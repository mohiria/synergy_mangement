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
	actor := projectActor(uid, proj.OwnerID, proj.MyRole)
	blockers, err := s.projectBlockers(r.Context(), projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, blockerViews(blockers, actor, uid))
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
	actor := projectActor(uid, proj.OwnerID, proj.MyRole)
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
	impact := domain.HardDownstreamNotes(facts.HardEdges, facts.Tasks)
	tasks := make(map[int64]domain.RemindTaskFact, len(facts.Tasks))
	for _, t := range facts.Tasks {
		tasks[t.ID] = domain.RemindTaskFact{
			Name: t.Name, OwnerID: t.OwnerID, KrOwnerID: t.KrOwnerID,
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
	for _, in := range facts.Inputs {
		if in.Ready || in.Necessity != domain.NecessityRequired {
			continue
		}
		switch {
		case in.RequestID != 0:
			waits = append(waits, domain.InputRequestWaitFact(in.RequestID, in.TargetTaskID, in.InputName, in.ProviderID, in.ProviderName))
		case in.SourceTaskID != nil:
			waits = append(waits, domain.UpstreamWaitFact(in.EdgeID, in.TargetTaskID, in.InputName, in.SourceTaskName, in.SourceOwnerID, in.SourceOwnerName))
		}
	}
	return domain.RemindTargets(domain.RemindFacts{
		Blockers: domain.DeriveBlockers(facts), Waits: waits, Tasks: tasks,
	}), nil
}

// projectBlockers 装配四类结构化事实并派生本项目当前全部卡点。
func (s *Server) projectBlockers(ctx context.Context, projectID int64) ([]domain.Blocker, error) {
	facts, err := s.projectBlockerFacts(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return domain.DeriveBlockers(facts), nil
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
	requestRows, err := s.q.ListInputRequestsByProject(ctx, projectID)
	if err != nil {
		return domain.BlockerFacts{}, err
	}
	poolRows, err := s.q.LatestPoolReviewsByProject(ctx, projectID)
	if err != nil {
		return domain.BlockerFacts{}, err
	}
	changeRows, err := s.q.LatestFieldChangesByProject(ctx, projectID)
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
	krOwnerNameByTask := make(map[int64]string, len(taskRows))
	for _, t := range taskRows {
		krOwnerNameByTask[t.ID] = t.KrOwnerName.String
		tf := domain.BlockerTaskFact{
			ID: t.ID, Name: t.Name, Status: t.Status,
			OwnerID: t.OwnerID, OwnerName: t.OwnerName,
			KrID: t.KeyResultID, KrOwnerID: fromPgInt8(t.KrOwnerID), KrOwnerName: t.KrOwnerName.String,
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

	// 输入就绪：来源为上游任务时看当前内容是否生效，来源为指定成员时看输入请求是否已提供。
	requestByEdge := make(map[int64]store.ListInputRequestsByProjectRow, len(requestRows))
	for _, ir := range requestRows {
		requestByEdge[ir.EdgeID] = ir
	}
	for _, e := range edgeRows {
		in := domain.BlockerInputFact{
			EdgeID: e.ID, TargetTaskID: e.TargetTaskID, InputName: e.Name,
			Necessity: e.Necessity, Ready: domain.EdgeReady(e.CurrentFileID.Valid, e.HasCandidate),
		}
		if e.SourceTaskID.Valid {
			src := e.SourceTaskID.Int64
			in.SourceTaskID = &src
			in.SourceTaskName = e.SourceTaskName.String
			in.SourceOwnerID = e.SourceOwnerID.Int64
			in.SourceOwnerName = e.SourceOwnerName.String
		}
		if ir, ok := requestByEdge[e.ID]; ok {
			in.Ready = domain.MemberEdgeReady(ir.State)
			in.ProviderID = ir.ProviderID
			in.ProviderName = ir.ProviderName
			in.RequestID = ir.ID
		}
		facts.Inputs = append(facts.Inputs, in)
		if e.EdgeType == domain.EdgeHardPrerequisite && e.SourceTaskID.Valid {
			facts.HardEdges = append(facts.HardEdges, domain.HardEdge{
				ID: e.ID, Source: e.SourceTaskID.Int64, Target: e.TargetTaskID,
			})
		}
	}

	// 停在当前环节的审批件：入池、关键字段变更、中间或签、KR 终审。
	for _, pr := range poolRows {
		if pr.Status != domain.PoolReviewPending {
			continue
		}
		facts.Approvals = append(facts.Approvals, domain.BlockerApprovalFact{
			Kind: "pool_review", RefID: pr.ID, TaskID: pr.TaskID,
			StageSince:  pr.SubmittedAt.Time,
			ApproverIDs: approverIDs(taskRows, pr.TaskID), ApproverNames: []string{krOwnerNameByTask[pr.TaskID]},
		})
	}
	for _, fc := range changeRows {
		if fc.State != domain.FieldChangePendingState {
			continue
		}
		facts.Approvals = append(facts.Approvals, domain.BlockerApprovalFact{
			Kind: "field_change", RefID: fc.ID, TaskID: fc.TaskID,
			StageSince:  fc.SubmittedAt.Time,
			ApproverIDs: approverIDs(taskRows, fc.TaskID), ApproverNames: []string{krOwnerNameByTask[fc.TaskID]},
		})
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
				ApproverIDs: approverIDs(taskRows, cr.TaskID), ApproverNames: []string{krOwnerNameByTask[cr.TaskID]},
			})
		}
	}

	return facts, nil
}

// approverIDs 取任务所属 KR 负责人（入池、关键字段变更与终审的审批人）。
func approverIDs(tasks []store.ListProjectTasksRow, taskID int64) []int64 {
	for _, t := range tasks {
		if t.ID == taskID && t.KrOwnerID.Valid {
			return []int64{t.KrOwnerID.Int64}
		}
	}
	return nil
}

func blockerViews(bs []domain.Blocker, actor domain.Actor, userID int64) []Blocker {
	out := make([]Blocker, 0, len(bs))
	for _, b := range bs {
		out = append(out, blockerView(b, actor, userID))
	}
	return out
}

func blockerView(b domain.Blocker, actor domain.Actor, userID int64) Blocker {
	canRemind := domain.CanRemindBlocker(actor, userID, b)
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
		Since:            b.Since,
		CanRemind:        &canRemind,
	}
	item.ImpactNote = optString(b.ImpactNote)
	return item
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
