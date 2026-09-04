package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// 结构化卡点（AC-11）：四类结构化事实读时派生，没有上报与手动解除；
// 派生规则在 domain.DeriveBlockers，本文件只装配事实与编排提醒。

func (s *Server) ListBlockers(w http.ResponseWriter, r *http.Request, projectId int64) {
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(currentUser(r), proj.OwnerID, proj.MyRole, proj.Visibility)
	blockers, err := s.projectBlockers(r.Context(), projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	// #129：canRemind 显隐把当日配额算进去，任一待行动人还能提醒才显示按钮。
	remindCounts, err := s.remindCountsToday(r.Context(), uid)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, blockerViews(blockers, actor, uid, projectSettingsOf(proj).RemindDailyLimit, remindCounts))
}

// CreateReminder 一键提醒当前待行动人（AC-11、MW-13）。
// 提醒目标独立于卡点建模：卡点按卡点键寻址，尚未成卡点的等待事项按 wait:<类型>:<ID> 寻址。
func (s *Server) CreateReminder(w http.ResponseWriter, r *http.Request, projectId int64) {
	var req RemindRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(currentUser(r), proj.OwnerID, proj.MyRole, proj.Visibility)
	targets, err := s.projectRemindTargets(r.Context(), projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	key := strings.TrimSpace(req.TargetKey)
	var target *domain.RemindTarget
	for i := range targets {
		if targets[i].Key == key {
			target = &targets[i]
			break
		}
	}
	// 条件已消失的卡点自动解除、等待事项已被处理，一律按目标不存在处理。
	if target == nil {
		writeJSON(w, http.StatusNotFound, Error{Code: "remind_target_not_found", Message: "提醒目标已不存在或已处理"})
		return
	}
	if !domain.CanRemind(actor, uid, *target) {
		writeForbidden(w)
		return
	}
	// MW-13、AC-60 冷却：按（发起人、被提醒人、任务）三元组计当天次数，上限取项目规则设置；
	// 换一个被提醒人不受影响，故逐个待行动人判定，全部用满才算被冷却挡下。
	now := s.now()
	limit := projectSettingsOf(proj).RemindDailyLimit
	day := pgtype.Date{Time: now, Valid: true}
	recipients := make([]int64, 0, len(target.ActionOwnerIDs))
	for _, ownerID := range target.ActionOwnerIDs {
		sent, err := s.q.CountRemindsToday(r.Context(), store.CountRemindsTodayParams{
			TaskID: target.TaskID, SenderID: uid, RecipientID: ownerID, RemindDate: day,
		})
		if err != nil {
			writeInternalError(w, r, err)
			return
		}
		if domain.RemindAllowed(int(sent), limit) {
			recipients = append(recipients, ownerID)
		}
	}
	if len(recipients) == 0 {
		writeJSON(w, http.StatusConflict, Error{Code: "remind_cooldown", Message: domain.ErrRemindCooldown.Error()})
		return
	}
	content := domain.RemindContent(*target)
	for _, ownerID := range recipients {
		if _, err := s.q.CreateNotification(r.Context(), blockerRemindNotification(ownerID, projectId, target.TaskID, content)); err != nil {
			writeInternalError(w, r, err)
			return
		}
		if _, err := s.q.CreateRemindLog(r.Context(), store.CreateRemindLogParams{
			TaskID: target.TaskID, SenderID: uid, RecipientID: ownerID, TargetKey: key,
			RemindDate: day,
		}); err != nil {
			writeInternalError(w, r, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// projectRemindTargets 汇总项目内全部提醒目标：派生卡点，以及尚未成卡点的等待事项
// （停在当前环节的审批件、待对接人提供的输入请求、未交付的上游任务）。
func (s *Server) projectRemindTargets(ctx context.Context, projectID int64) ([]domain.RemindTarget, error) {
	facts, err := s.projectBlockerFacts(ctx, projectID)
	if err != nil {
		return nil, err
	}
	impact := domain.RequiredDownstreamNotes(facts.RequiredEdges, facts.Tasks)
	tasks := make(map[int64]domain.RemindTaskFact, len(facts.Tasks))
	for _, t := range facts.Tasks {
		tasks[t.ID] = domain.RemindTaskFact{
			Name: t.Name, OwnerID: t.OwnerID,
			End: t.EndDate, ImpactNote: impact[t.ID],
		}
	}
	waits := []domain.RemindWaitFact{}
	for _, a := range facts.Approvals {
		var days *int
		if !a.StageSince.IsZero() {
			d := int(facts.Now.Sub(a.StageSince).Hours() / 24)
			days = &d
		}
		waits = append(waits, domain.ApprovalWaitFact(a.Kind, a.RefID, a.TaskID, a.ApproverIDs, a.ApproverNames, days))
	}
	// #178 后输入来源恒为上游任务。
	for _, in := range facts.Inputs {
		if in.Ready || in.Necessity != domain.NecessityRequired || in.SourceTaskID == nil {
			continue
		}
		waits = append(waits, domain.UpstreamWaitFact(in.EdgeID, in.TargetTaskID, in.InputName, in.SourceTaskName, in.SourceOwnerID, in.SourceOwnerName))
	}
	return domain.RemindTargets(domain.RemindFacts{
		Blockers: domain.DeriveBlockers(facts), Waits: waits, Tasks: tasks,
	}), nil
}

// projectBlockers 装配四类结构化事实并派生本项目当前全部卡点。
func (s *Server) projectBlockers(ctx context.Context, projectID int64) ([]domain.Blocker, error) {
	// P1：同一次请求内、同一次提交之后的重复派生走缓存（见 blocker_cache.go）。
	cache := blockerCacheFrom(ctx)
	if cache != nil {
		if bs, ok := cache.get(projectID); ok {
			return bs, nil
		}
	}
	facts, err := s.projectBlockerFacts(ctx, projectID)
	if err != nil {
		return nil, err
	}
	bs := domain.DeriveBlockers(facts)
	if cache != nil {
		cache.put(projectID, bs)
	}
	return bs, nil
}

// projectBlockerFacts 装配卡点与提醒目标共用的项目结构化事实。
func (s *Server) projectBlockerFacts(ctx context.Context, projectID int64) (domain.BlockerFacts, error) {
	taskRows, err := s.q.ListProjectTasks(ctx, projectID)
	if err != nil {
		return domain.BlockerFacts{}, err
	}
	edgeRows, err := s.q.ListEdgesByProject(ctx, projectID)
	if err != nil {
		return domain.BlockerFacts{}, err
	}
	completionRows, err := s.q.LatestCompletionReviewsByProject(ctx, projectID)
	if err != nil {
		return domain.BlockerFacts{}, err
	}

	settings, err := s.projectSettings(ctx, projectID)
	if err != nil {
		return domain.BlockerFacts{}, err
	}
	facts := domain.BlockerFacts{Now: s.now(), ApprovalTimeoutDays: settings.ApprovalTimeoutDays}
	codeByTask := make(map[int64]string, len(taskRows))
	for _, t := range taskRows {
		codeByTask[t.ID] = domain.TaskCode(int(t.ObjectiveCodeSeq), int(t.KrCodeSeq), int(t.CodeSeq))
		tf := domain.BlockerTaskFact{
			ID: t.ID, Name: t.Name, Status: t.Status,
			OwnerID: t.OwnerID, OwnerName: t.OwnerName,
			KrID: t.KeyResultID,
		}
		if t.StartDate.Valid {
			start := t.StartDate.Time
			tf.StartDate = &start
		}
		if t.EndDate.Valid {
			end := t.EndDate.Time
			tf.EndDate = &end
		}
		facts.Tasks = append(facts.Tasks, tf)
	}

	// 输入就绪（裁决 #163／#178）：来源恒为上游任务，就绪只看来源任务已完成。
	for _, e := range edgeRows {
		in := domain.BlockerInputFact{
			EdgeID: e.ID, TargetTaskID: e.TargetTaskID, InputName: e.Name,
			Necessity: e.Necessity, Ready: domain.EdgeReady(e.SourceTaskStatus.String),
		}
		src := e.SourceTaskID
		in.SourceTaskID = &src
		in.SourceTaskCode = codeByTask[src]
		in.SourceTaskName = e.SourceTaskName.String
		in.SourceOwnerID = e.SourceOwnerID.Int64
		in.SourceOwnerName = e.SourceOwnerName.String
		facts.Inputs = append(facts.Inputs, in)
		// #173 裁决：互锁与下游影响沿「必要」边。
		if e.Necessity == domain.NecessityRequired {
			facts.RequiredEdges = append(facts.RequiredEdges, domain.RequiredEdge{
				ID: e.ID, Source: e.SourceTaskID, Target: e.TargetTaskID,
			})
		}
	}

	// 停在当前环节的审批件（裁决 10，#180：关闭申请退场，只剩中间或签、终审）。
	// 终审的待行动人为项目管理员集合（裁决 11，#181）。
	finalIDs, finalNames, err := s.projectFinalReviewers(ctx, projectID)
	if err != nil {
		return domain.BlockerFacts{}, err
	}
	for _, cr := range completionRows {
		switch cr.State {
		case domain.CompletionIntermediate:
			rvs, err := s.q.ListReviewReviewers(ctx, cr.ID)
			if err != nil {
				return domain.BlockerFacts{}, err
			}
			fact := domain.BlockerApprovalFact{
				Kind: "intermediate_review", RefID: cr.ID, TaskID: cr.TaskID,
				StageSince: cr.SubmittedAt.Time,
			}
			for _, rv := range rvs {
				fact.ApproverIDs = append(fact.ApproverIDs, rv.UserID)
				fact.ApproverNames = append(fact.ApproverNames, rv.DisplayName)
			}
			facts.Approvals = append(facts.Approvals, fact)
		case domain.CompletionPendingFinal:
			// 进入新环节重新计时：或签通过的时点即终审环节起点。
			since := cr.SubmittedAt.Time
			if cr.IntermediateAt.Valid {
				since = cr.IntermediateAt.Time
			}
			facts.Approvals = append(facts.Approvals, domain.BlockerApprovalFact{
				Kind: "final_review", RefID: cr.ID, TaskID: cr.TaskID, StageSince: since,
				ApproverIDs:   append([]int64(nil), finalIDs...),
				ApproverNames: append([]string(nil), finalNames...),
			})
		}
	}

	return facts, nil
}

func blockerViews(bs []domain.Blocker, actor domain.Actor, userID int64, remindLimit int, remindCounts func(recipientID, taskID int64) int) []Blocker {
	out := make([]Blocker, 0, len(bs))
	for _, b := range bs {
		out = append(out, blockerView(b, actor, userID, remindLimit, remindCounts))
	}
	return out
}

func blockerView(b domain.Blocker, actor domain.Actor, userID int64, remindLimit int, remindCounts func(recipientID, taskID int64) int) Blocker {
	// #129：权限之外再看当日配额，全部待行动人都用完就不显示按钮。
	canRemind := domain.CanRemindBlocker(actor, userID, b) &&
		domain.RemindQuotaLeft(domain.BlockerRemindTarget(b, nil), remindLimit, remindCounts)
	item := Blocker{
		Key:              b.Key,
		Kind:             BlockerKind(b.Kind),
		KindLabel:        domain.BlockerKindLabel(b.Kind),
		TaskId:           b.TaskID,
		TaskName:         b.TaskName,
		Missing:          b.Missing,
		Reason:           b.Reason,
		ActionOwnerIds:   append([]int64{}, b.ActionOwnerIDs...),
		ActionOwnerNames: append([]string{}, b.ActionOwnerNames...),
		Level:            RiskLevel(b.Level),
		LevelLabel:       optString(domain.RiskLevelLabel(b.Level)),
		Since:            b.Since,
		CanRemind:        &canRemind,
	}
	item.ImpactNote = optString(b.ImpactNote)
	// #167：「上游未就绪」条目按「编号＋标题＋负责人」展示上游任务。
	item.SourceTaskCode = optString(b.SourceTaskCode)
	item.SourceTaskName = optString(b.SourceTaskName)
	item.SourceOwnerName = optString(b.SourceOwnerName)
	return item
}

// remindCountsToday 当前用户今天的提醒计数（#129）：按（被提醒人、任务）寻址，
// 一次查询取回，供卡点列表与我的工作的 canRemind 显隐判定。
func (s *Server) remindCountsToday(ctx context.Context, senderID int64) (func(recipientID, taskID int64) int, error) {
	rows, err := s.q.ListRemindCountsToday(ctx, store.ListRemindCountsTodayParams{
		SenderID: senderID, RemindDate: pgtype.Date{Time: s.now(), Valid: true},
	})
	if err != nil {
		return nil, err
	}
	type key struct{ recipient, task int64 }
	m := make(map[key]int, len(rows))
	for _, row := range rows {
		m[key{row.RecipientID, row.TaskID}] = int(row.N)
	}
	return func(recipientID, taskID int64) int { return m[key{recipientID, taskID}] }, nil
}

func blockerRemindNotification(userID, projectID, taskID int64, content string) store.CreateNotificationParams {
	return store.CreateNotificationParams{
		UserID:    userID,
		Kind:      domain.NotifyBlockerRemind,
		Content:   content,
		ProjectID: pgtype.Int8{Int64: projectID, Valid: true},
		TaskID:    pgtype.Int8{Int64: taskID, Valid: true},
	}
}
