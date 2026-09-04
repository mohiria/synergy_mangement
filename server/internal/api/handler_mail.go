package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"synergy/server/internal/domain"
	"synergy/server/internal/mail"
	"synergy/server/internal/secrets"
	"synergy/server/internal/store"
)

// 邮件通道（#212，模块 PRD §10.1～10.2）：配置落库（密码 AES-GCM 密文）、所有邮件先进 outbox、
// 进程内后台协程取出发送、失败按 domain.MailRetry 退避；API 请求不等待 SMTP。

// MailOutboxInterval 后台协程轮询间隔。
const MailOutboxInterval = 10 * time.Second

// mailOutboxBatch 每轮最多处理的待发件数。
const mailOutboxBatch = 20

// ConfigureMail 注入应用密钥与发送器；main 用真实 SMTP，测试用记录器。
func (s *Server) ConfigureMail(secretKey []byte, sender mail.Sender) {
	s.secretKey = secretKey
	s.mailer = sender
}

// GetMailSettings 通道配置（仅系统管理员）；密码永不回显，只给「已设置」。
func (s *Server) GetMailSettings(w http.ResponseWriter, r *http.Request) {
	if !requireSystemAdmin(w, r) {
		return
	}
	ms, err := s.q.GetMailSettings(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toMailSettings(ms))
}

// UpdateMailSettings 保存通道配置：密码留空表示保持原值；给了就用应用密钥加密后落库。
func (s *Server) UpdateMailSettings(w http.ResponseWriter, r *http.Request) {
	if !requireSystemAdmin(w, r) {
		return
	}
	var req MailSettingsInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	in, err := domain.ValidateMailSettings(domain.MailSettingsInput{
		Host: req.Host, Port: req.Port, Encryption: string(req.Encryption), Username: req.Username,
		FromName: req.FromName, FromAddress: req.FromAddress,
	})
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_mail_settings", Message: err.Error()})
		return
	}
	if req.Password != nil && *req.Password != "" {
		if len(s.secretKey) == 0 {
			writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "secret_key_missing", Message: secrets.ErrKeyMissing.Error()})
			return
		}
		enc, err := secrets.Encrypt(s.secretKey, []byte(*req.Password))
		if err != nil {
			writeInternalError(w, r, err)
			return
		}
		if err := s.q.SetMailPassword(r.Context(), enc); err != nil {
			writeInternalError(w, r, err)
			return
		}
	}
	ms, err := s.q.UpdateMailSettings(r.Context(), store.UpdateMailSettingsParams{
		Host: in.Host, Port: int32(in.Port), Encryption: in.Encryption, Username: in.Username,
		FromName: in.FromName, FromAddress: in.FromAddress,
	})
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toMailSettings(ms))
}

// SendTestMail 发送测试邮件：发到我绑定的邮箱，或手填地址；只入队，202 立即返回。
func (s *Server) SendTestMail(w http.ResponseWriter, r *http.Request) {
	if !requireSystemAdmin(w, r) {
		return
	}
	var req TestMailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	ms, err := s.q.GetMailSettings(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	if !domain.MailChannelConfigured(ms.Host, ms.FromAddress) {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "mail_not_configured", Message: domain.ErrMailNotConfigured.Error()})
		return
	}
	to := currentUser(r).Email
	if req.Target == Custom {
		addr := ""
		if req.Address != nil {
			addr = *req.Address
		}
		if err := domain.ValidateEmail(addr); err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_email", Message: domain.ErrMailTestTargetInvalid.Error()})
			return
		}
		to = domain.NormalizeEmail(addr)
	}
	item, err := s.enqueueMail(r.Context(), to, "测试邮件", "这是一封来自「系统设置 → 通知设置」的测试邮件。收到即表示邮件通道配置正确。", domain.MailEventTest)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, item)
}

// ListMailOutbox 最近发送记录（最近 50 条）。
func (s *Server) ListMailOutbox(w http.ResponseWriter, r *http.Request) {
	if !requireSystemAdmin(w, r) {
		return
	}
	rows, err := s.q.ListMailOutbox(r.Context(), 50)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	out := make([]MailOutboxItem, 0, len(rows))
	for _, x := range rows {
		out = append(out, MailOutboxItem{
			Id: x.ID, ToAddress: x.ToAddress, Subject: x.Subject, Event: x.Event, EventLabel: domain.MailEventLabel(x.Event),
			Status: MailOutboxItemStatus(x.Status), StatusLabel: domain.MailStatusLabel(x.Status), Attempts: int(x.Attempts),
			LastError: optString(x.LastError), CreatedAt: x.CreatedAt.Time, SentAt: fromPgTime(x.SentAt),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// enqueueMail 写一条待发邮件；找回密码（#214）与站内通知同步（#213）复用。
func (s *Server) enqueueMail(ctx context.Context, to, subject, body, event string) (MailOutboxItem, error) {
	x, err := s.q.EnqueueMail(ctx, store.EnqueueMailParams{ToAddress: to, Subject: subject, Body: body, Event: event})
	if err != nil {
		return MailOutboxItem{}, err
	}
	return MailOutboxItem{
		Id: x.ID, ToAddress: x.ToAddress, Subject: x.Subject, Event: x.Event, EventLabel: domain.MailEventLabel(x.Event),
		Status: MailOutboxItemStatus(x.Status), StatusLabel: domain.MailStatusLabel(x.Status), Attempts: int(x.Attempts),
		CreatedAt: x.CreatedAt.Time,
	}, nil
}

// ProcessMailOutbox 处理一轮到期待发件，返回处理条数。发送失败按 domain.MailRetry 退避或标记失败。
func (s *Server) ProcessMailOutbox(ctx context.Context) int {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		log.Printf("mail outbox: begin failed: %v", err)
		return 0
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)
	items, err := qtx.ClaimDueMail(ctx, mailOutboxBatch)
	if err != nil {
		log.Printf("mail outbox: claim failed: %v", err)
		return 0
	}
	if len(items) == 0 {
		return 0
	}
	ms, err := qtx.GetMailSettings(ctx)
	if err != nil {
		log.Printf("mail outbox: settings failed: %v", err)
		return 0
	}
	cfg, cfgErr := s.mailConfig(ms)
	for _, item := range items {
		var sendErr error
		switch {
		case cfgErr != nil:
			sendErr = cfgErr
		case s.mailer == nil:
			sendErr = errors.New("未配置发送器")
		default:
			sendErr = s.mailer.Send(ctx, cfg, mail.Message{To: item.ToAddress, Subject: item.Subject, Body: item.Body})
		}
		if sendErr == nil {
			if err := qtx.MarkMailSent(ctx, item.ID); err != nil {
				log.Printf("mail outbox: mark sent %d failed: %v", item.ID, err)
			}
			continue
		}
		msg := sendErr.Error()
		if len(msg) > 500 {
			msg = msg[:500]
		}
		next, failed := domain.MailRetry(int(item.Attempts)+1, s.now())
		if failed {
			err = qtx.MarkMailFailed(ctx, store.MarkMailFailedParams{ID: item.ID, LastError: msg})
		} else {
			err = qtx.MarkMailRetry(ctx, store.MarkMailRetryParams{ID: item.ID, LastError: msg, NextAttemptAt: pgtype.Timestamptz{Time: next, Valid: true}})
		}
		if err != nil {
			log.Printf("mail outbox: mark retry %d failed: %v", item.ID, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		log.Printf("mail outbox: commit failed: %v", err)
		return 0
	}
	return len(items)
}

// StartMailOutboxWorker 进程内单协程按间隔处理 outbox；返回停止函数。
func (s *Server) StartMailOutboxWorker(ctx context.Context, interval time.Duration) func() {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.ProcessMailOutbox(ctx)
			}
		}
	}()
	return cancel
}

// mailConfig 把落库配置解密成发送配置；通道未配置或密钥缺失时给出可读错误（进 last_error）。
func (s *Server) mailConfig(ms store.MailSetting) (mail.Config, error) {
	if !domain.MailChannelConfigured(ms.Host, ms.FromAddress) {
		return mail.Config{}, domain.ErrMailNotConfigured
	}
	cfg := mail.Config{
		Host: ms.Host, Port: int(ms.Port), Encryption: ms.Encryption, Username: ms.Username,
		FromName: ms.FromName, FromAddress: ms.FromAddress,
	}
	if ms.PasswordEnc != "" {
		if len(s.secretKey) == 0 {
			return mail.Config{}, secrets.ErrKeyMissing
		}
		plain, err := secrets.Decrypt(s.secretKey, ms.PasswordEnc)
		if err != nil {
			return mail.Config{}, err
		}
		cfg.Password = string(plain)
	}
	return cfg, nil
}

func toMailSettings(ms store.MailSetting) MailSettings {
	return MailSettings{
		Host: ms.Host, Port: int(ms.Port), Encryption: MailSettingsEncryption(ms.Encryption), Username: ms.Username,
		FromName: ms.FromName, FromAddress: ms.FromAddress,
		PasswordSet: strings.TrimSpace(ms.PasswordEnc) != "",
		Configured:  domain.MailChannelConfigured(ms.Host, ms.FromAddress),
		Notify:      toNotifySwitches(systemSwitches(ms), nil),
		UpdatedAt:   ms.UpdatedAt.Time,
	}
}

// systemSwitches 系统级开关（#213）。
func systemSwitches(ms store.MailSetting) domain.MailSwitches {
	return domain.MailSwitches{Enabled: ms.NotifyEnabled, Events: map[string]bool{
		domain.NotifyDiscussionMention:    ms.NotifyDiscussionMention,
		domain.NotifyDiscussionOwner:      ms.NotifyDiscussionOwner,
		domain.NotifyTaskInvite:           ms.NotifyTaskInvite,
		domain.NotifyUpstreamTaskAssigned: ms.NotifyUpstreamTaskAssigned,
		domain.NotifyBlockerRemind:        ms.NotifyBlockerRemind,
	}}
}

// userSwitches 个人偏好；无行即全开。
func userSwitches(p store.UserMailPref, found bool) domain.MailSwitches {
	if !found {
		return domain.AllOn()
	}
	return domain.MailSwitches{Enabled: p.Enabled, Events: map[string]bool{
		domain.NotifyDiscussionMention:    p.NotifyDiscussionMention,
		domain.NotifyDiscussionOwner:      p.NotifyDiscussionOwner,
		domain.NotifyTaskInvite:           p.NotifyTaskInvite,
		domain.NotifyUpstreamTaskAssigned: p.NotifyUpstreamTaskAssigned,
		domain.NotifyBlockerRemind:        p.NotifyBlockerRemind,
	}}
}

// toNotifySwitches 转契约；system 非空时给每个事件附带系统级是否启用（个人页置灰用）。
func toNotifySwitches(sw domain.MailSwitches, system *domain.MailSwitches) MailNotifySwitches {
	out := MailNotifySwitches{Enabled: sw.Enabled, Events: make([]MailEventSwitch, 0, len(domain.MailNotifyKinds))}
	for _, k := range domain.MailNotifyKinds {
		on, ok := sw.Events[k]
		item := MailEventSwitch{Kind: k, Label: domain.MailNotifyKindLabel(k), Enabled: !ok || on}
		if system != nil {
			se := system.Enabled && system.Events[k]
			item.SystemEnabled = &se
		}
		out.Events = append(out.Events, item)
	}
	return out
}

func fromNotifySwitches(in MailNotifySwitches) domain.MailSwitches {
	sw := domain.AllOn()
	sw.Enabled = in.Enabled
	for _, e := range in.Events {
		if _, ok := sw.Events[e.Kind]; ok {
			sw.Events[e.Kind] = e.Enabled
		}
	}
	return sw
}

// UpdateMailNotify 系统级开关（仅系统管理员，#213）；进系统级审计。
func (s *Server) UpdateMailNotify(w http.ResponseWriter, r *http.Request) {
	if !requireSystemAdmin(w, r) {
		return
	}
	var req MailNotifySwitches
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	sw := fromNotifySwitches(req)
	ms, err := s.q.UpdateMailNotifySwitches(r.Context(), store.UpdateMailNotifySwitchesParams{
		NotifyEnabled: sw.Enabled, NotifyDiscussionMention: sw.Events[domain.NotifyDiscussionMention],
		NotifyDiscussionOwner: sw.Events[domain.NotifyDiscussionOwner], NotifyTaskInvite: sw.Events[domain.NotifyTaskInvite],
		NotifyUpstreamTaskAssigned: sw.Events[domain.NotifyUpstreamTaskAssigned], NotifyBlockerRemind: sw.Events[domain.NotifyBlockerRemind],
	})
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toMailSettings(ms))
}

// GetMyMailPreferences 个人通知偏好（#213）：本人开关 + 系统级开关（置灰用）。
func (s *Server) GetMyMailPreferences(w http.ResponseWriter, r *http.Request) {
	prefs, ok := s.loadMailPreferences(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

// UpdateMyMailPreferences 保存本人偏好（#213）；不进审计（/me 不在 /system 下）。
func (s *Server) UpdateMyMailPreferences(w http.ResponseWriter, r *http.Request) {
	var req MailNotifySwitches
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	sw := fromNotifySwitches(req)
	if _, err := s.q.UpsertUserMailPrefs(r.Context(), store.UpsertUserMailPrefsParams{
		UserID: currentUser(r).ID, Enabled: sw.Enabled,
		NotifyDiscussionMention: sw.Events[domain.NotifyDiscussionMention], NotifyDiscussionOwner: sw.Events[domain.NotifyDiscussionOwner],
		NotifyTaskInvite: sw.Events[domain.NotifyTaskInvite], NotifyUpstreamTaskAssigned: sw.Events[domain.NotifyUpstreamTaskAssigned],
		NotifyBlockerRemind: sw.Events[domain.NotifyBlockerRemind],
	}); err != nil {
		writeInternalError(w, r, err)
		return
	}
	prefs, ok := s.loadMailPreferences(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

func (s *Server) loadMailPreferences(w http.ResponseWriter, r *http.Request) (MailPreferences, bool) {
	ms, err := s.q.GetMailSettings(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return MailPreferences{}, false
	}
	system := systemSwitches(ms)
	row, err := s.q.GetUserMailPrefs(r.Context(), currentUser(r).ID)
	found := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeInternalError(w, r, err)
		return MailPreferences{}, false
	}
	sw := toNotifySwitches(userSwitches(row, found), &system)
	return MailPreferences{Enabled: sw.Enabled, Events: sw.Events, SystemEnabled: system.Enabled}, true
}

// notify 站内通知的唯一写入口（#213）：落通知，再按系统开关 × 个人偏好 × 停用状态决定是否同时入 outbox。
// 邮件失败不影响通知；q 为当前事务的 Queries，通知与邮件同事务落库。
func (s *Server) notify(ctx context.Context, q *store.Queries, params store.CreateNotificationParams) error {
	if _, err := q.CreateNotification(ctx, params); err != nil {
		return err
	}
	ms, err := q.GetMailSettings(ctx)
	if err != nil {
		log.Printf("notify mail: settings failed: %v", err)
		return nil
	}
	if !domain.MailChannelConfigured(ms.Host, ms.FromAddress) {
		return nil
	}
	user, err := q.GetUserByID(ctx, params.UserID)
	if err != nil {
		log.Printf("notify mail: user %d failed: %v", params.UserID, err)
		return nil
	}
	row, perr := q.GetUserMailPrefs(ctx, params.UserID)
	if !domain.ShouldMailNotification(params.Kind, systemSwitches(ms), userSwitches(row, perr == nil), user.DisabledAt.Valid) {
		return nil
	}
	st, err := q.GetSystemSettings(ctx)
	if err != nil {
		log.Printf("notify mail: system settings failed: %v", err)
		return nil
	}
	subject := "[" + st.SystemName + "] " + domain.MailNotifyKindLabel(params.Kind)
	body := params.Content + "\n\n请登录系统查看"
	if st.BaseUrl != "" {
		body += "：" + st.BaseUrl
	}
	if _, err := q.EnqueueMail(ctx, store.EnqueueMailParams{ToAddress: user.Email, Subject: subject, Body: body, Event: params.Kind}); err != nil {
		log.Printf("notify mail: enqueue failed: %v", err)
	}
	return nil
}

func fromPgTime(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}
