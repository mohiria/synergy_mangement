package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// 找回密码（#214，模块 PRD §4）：两个接口都免登录、POST + JSON。
// 请求阶段：挂登录限速、统一回文、不记审计；成功重置：作废 token、清「须改密码」、踢掉全部会话、
// 记一条系统级审计（目标用户）。

// RequestPasswordReset 输入用户名或邮箱；无论账号是否存在、是否停用都返回同一文案。
func (s *Server) RequestPasswordReset(w http.ResponseWriter, r *http.Request) {
	var req PasswordResetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	identifier := strings.TrimSpace(req.Identifier)
	if identifier == "" {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请输入用户名或邮箱"})
		return
	}
	ms, err := s.q.GetMailSettings(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	if !domain.CanRecoverPassword(domain.MailChannelConfigured(ms.Host, ms.FromAddress)) {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "reset_not_available", Message: domain.ErrResetNotAvailable.Error()})
		return
	}
	now := s.now()
	ip := clientIP(r)
	throttleKey := "reset:" + strings.ToLower(identifier)
	if !s.throttle.Allow(throttleKey, ip, now) {
		writeRateLimited(w, s.throttle.RetryAfter(throttleKey, ip, now))
		return
	}
	// 每次请求都计一次：同一标识 + IP 在锁定窗口内最多尝试上限次数。
	s.throttle.RecordFailure(throttleKey, ip, now)

	user, err := s.q.GetUserByUsernameOrEmail(r.Context(), identifier)
	if err == nil && !user.DisabledAt.Valid {
		if err := s.issuePasswordReset(r, user); err != nil {
			log.Printf("password reset: issue for user %d failed: %v", user.ID, err)
		}
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, PasswordResetRequested{Message: domain.PasswordResetRequestedMessage})
}

// issuePasswordReset 生成一次性 token（只落哈希）、作废旧 token、把重置链接经 outbox 发出。
func (s *Server) issuePasswordReset(r *http.Request, user store.User) error {
	ctx := r.Context()
	token, err := domain.NewPasswordResetToken()
	if err != nil {
		return err
	}
	if err := s.q.InvalidatePasswordResetTokens(ctx, user.ID); err != nil {
		return err
	}
	if _, err := s.q.CreatePasswordResetToken(ctx, store.CreatePasswordResetTokenParams{
		UserID: user.ID, TokenHash: domain.HashPasswordResetToken(token),
		ExpiresAt: pgtype.Timestamptz{Time: s.now().Add(domain.PasswordResetTTL), Valid: true},
	}); err != nil {
		return err
	}
	st, err := s.q.GetSystemSettings(ctx)
	if err != nil {
		return err
	}
	link := domain.PasswordResetLink(st.BaseUrl, r.Host, token)
	body := user.DisplayName + "（" + user.Username + "），你好：\n\n请在 30 分钟内打开以下链接设置新密码；链接只能使用一次，若非本人操作请忽略。\n\n" + link
	_, err = s.enqueueMail(ctx, user.Email, "["+st.SystemName+"] 找回密码", body, domain.MailEventPasswordReset)
	return err
}

// ConfirmPasswordReset 用 token 设置新密码。
func (s *Server) ConfirmPasswordReset(w http.ResponseWriter, r *http.Request) {
	var req PasswordResetConfirm
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	ctx := r.Context()
	row, err := s.q.GetPasswordResetToken(ctx, domain.HashPasswordResetToken(req.Token))
	found := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeInternalError(w, r, err)
		return
	}
	var user store.User
	if found {
		user, err = s.q.GetUserByID(ctx, row.UserID)
		if err != nil {
			writeInternalError(w, r, err)
			return
		}
	}
	if err := domain.ValidatePasswordResetToken(found, row.ExpiresAt.Time, row.UsedAt.Valid, user.DisabledAt.Valid, s.now()); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "reset_token_invalid", Message: err.Error()})
		return
	}
	if err := domain.ValidatePasswordLength(req.Password); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_password", Message: err.Error()})
		return
	}
	hash, err := domain.HashPassword(req.Password)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)
	// UpdateUserPassword 同时清除「须改密码」标记（#203）。
	if err := qtx.UpdateUserPassword(ctx, store.UpdateUserPasswordParams{ID: user.ID, PasswordHash: hash}); err != nil {
		writeInternalError(w, r, err)
		return
	}
	if err := qtx.MarkPasswordResetTokenUsed(ctx, row.ID); err != nil {
		writeInternalError(w, r, err)
		return
	}
	if _, err := qtx.DeleteUserSessions(ctx, user.ID); err != nil {
		writeInternalError(w, r, err)
		return
	}
	// 只记成功重置：系统级审计，目标用户、时间；操作者为空（本人未登录）。
	if err := qtx.CreateAuditLog(ctx, store.CreateAuditLogParams{
		Action: "找回密码重置", Method: http.MethodPost, Route: "/auth/password-reset/confirm",
		ObjectType: "users", ObjectID: toPgInt8(&user.ID),
	}); err != nil {
		log.Printf("password reset: audit failed: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		writeInternalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
