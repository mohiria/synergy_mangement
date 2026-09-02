package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// #172 裁决（第二刀）：关键字段修改直接生效（无变更单、无修改原因），
// 动作写入任务动态并站内通知所属 KR 负责人；任务关闭审批独立保留为「关闭申请」。
// 业务规则在 domain，handler 仅编排。

// EditTaskFields 直接修改任务关键字段：立即生效、动态留痕、通知 KR 负责人（本人修改不另发）。
func (s *Server) EditTaskFields(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64) {
	var req EditTaskFieldsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(uid, proj.OwnerID, proj.MyRole, proj.Visibility)
	task, facts, ok := s.fetchTask(w, r, projectId, taskId)
	if !ok {
		return
	}
	hasPendingCancel, err := s.q.HasPendingFieldChange(r.Context(), taskId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	if err := domain.TaskEditRule(actor, uid, facts, hasPendingCancel); err != nil {
		switch {
		case errors.Is(err, domain.ErrChangeForbidden):
			writeForbidden(w)
		case errors.Is(err, domain.ErrChangeNotAllowed), errors.Is(err, domain.ErrCancelBlocked):
			writeJSON(w, http.StatusConflict, Error{Code: "task_state_conflict", Message: err.Error()})
		default:
			writeInternalError(w, r, err)
		}
		return
	}
	changes := toKeyFieldChanges(req)
	members, err := s.q.ListProjectMembers(r.Context(), projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	roleByID := make(map[int64]string, len(members))
	for _, m := range members {
		roleByID[m.UserID] = m.Role
	}
	if err := domain.ValidateKeyFieldChanges(changes,
		func(id int64) string { return roleByID[id] }, task.StartDate.Time); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_field_change", Message: err.Error()})
		return
	}
	labels := changedFieldLabels(changes)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	if _, err := qtx.ApplyTaskKeyFields(r.Context(), applyParams(taskId, changes)); err != nil {
		writeInternalError(w, r, err)
		return
	}
	// 站内通知所属 KR 负责人（#172 裁决；本人是 KR 负责人时不另发），与写入同事务。
	if target := domain.FieldEditNotifyTarget(uid, facts.KrOwnerID); target != nil {
		if _, err := qtx.CreateNotification(r.Context(), store.CreateNotificationParams{
			UserID: *target,
			Kind:   domain.NotifyTaskFieldEdited,
			Content: domain.FieldEditNotification(
				currentUser(r).DisplayName, task.Name, labels),
			ProjectID: pgtype.Int8{Int64: projectId, Valid: true},
			TaskID:    pgtype.Int8{Int64: taskId, Valid: true},
		}); err != nil {
			writeInternalError(w, r, err)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w, r, err)
		return
	}
	s.actionActivity(r.Context(), taskId, domain.ActivityFieldEdited, uid, strings.Join(labels, "、"))
	s.writeTask(w, r, projectId, taskId, uid, actor)
}

// DecideCancelRequest 所属 KR 负责人通过或退回关闭申请（AC-57）。
func (s *Server) DecideCancelRequest(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64, requestId int64) {
	var req CancelRequestDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !req.Decision.Valid() {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(uid, proj.OwnerID, proj.MyRole, proj.Visibility)
	opinion := ""
	if req.Opinion != nil {
		opinion = strings.TrimSpace(*req.Opinion)
	}
	approve := req.Decision == CancelRequestDecisionRequestDecisionApproved

	// 规则与写入同事务：先锁任务行再重读事实与申请单，避免批准落在已终止的任务上（R2／R3）。
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	_, facts, ok := lockTaskFacts(r, w, qtx, projectId, taskId)
	if !ok {
		return
	}
	fc, err := qtx.GetFieldChange(r.Context(), store.GetFieldChangeParams{ID: requestId, TaskID: taskId})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "cancel_request_not_found", Message: "关闭申请不存在"})
		} else {
			writeInternalError(w, r, err)
		}
		return
	}
	if err := domain.DecideCancelRequestRule(actor, fc.State, facts, uid, approve, opinion); err != nil {
		if errors.Is(err, domain.ErrCancelNotPending) {
			writeJSON(w, http.StatusConflict, Error{Code: "change_state_conflict", Message: err.Error()})
		} else if errors.Is(err, domain.ErrCancelTaskTerminal) {
			writeJSON(w, http.StatusConflict, Error{Code: "task_state_conflict", Message: err.Error()})
		} else if errors.Is(err, domain.ErrRejectOpinionRequired) {
			writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "opinion_required", Message: err.Error()})
		} else {
			writeJSON(w, http.StatusForbidden, Error{Code: "forbidden", Message: err.Error()})
		}
		return
	}
	newState := domain.CancelRequestRejectedState
	if approve {
		newState = domain.CancelRequestApprovedState
	}
	if _, err := qtx.DecideFieldChange(r.Context(), store.DecideFieldChangeParams{
		ID: requestId, State: newState, Opinion: opinion, DecidedBy: pgtype.Int8{Int64: uid, Valid: true},
	}); err != nil {
		writeInternalError(w, r, err)
		return
	}
	if approve {
		// AC-57：关闭申请通过后任务进入已关闭并保留原因。
		if _, err := qtx.UpdateTaskStatusWithReason(r.Context(), store.UpdateTaskStatusWithReasonParams{
			ID: taskId, Status: domain.TaskCancelled, CancelReason: fc.Reason,
		}); err != nil {
			writeInternalError(w, r, err)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w, r, err)
		return
	}
	if approve {
		s.actionActivity(r.Context(), taskId, domain.ActivityCancelApproved, uid, opinion)
	} else {
		s.actionActivity(r.Context(), taskId, domain.ActivityCancelRejected, uid, opinion)
	}
	s.writeTask(w, r, projectId, taskId, uid, actor)
}

// AbandonCancelRequest 放弃已退回的关闭申请（清除退回待处理事项）。
func (s *Server) AbandonCancelRequest(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64, requestId int64) {
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(uid, proj.OwnerID, proj.MyRole, proj.Visibility)
	if _, _, ok := s.fetchTask(w, r, projectId, taskId); !ok {
		return
	}
	fc, err := s.q.GetFieldChange(r.Context(), store.GetFieldChangeParams{ID: requestId, TaskID: taskId})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "cancel_request_not_found", Message: "关闭申请不存在"})
		} else {
			writeInternalError(w, r, err)
		}
		return
	}
	if fc.State != domain.CancelRequestRejectedState || fc.Resolved {
		writeJSON(w, http.StatusConflict, Error{Code: "change_state_conflict", Message: "只有退回未处理的关闭申请可以放弃"})
		return
	}
	if !domain.CanAbandonCancelRequest(actor, uid, fc.SubmittedBy, fc.State, fc.Resolved) {
		writeForbidden(w)
		return
	}
	if _, err := s.q.ResolveFieldChange(r.Context(), requestId); err != nil {
		writeInternalError(w, r, err)
		return
	}
	s.actionActivity(r.Context(), taskId, domain.ActivityFieldChangeAbandoned, uid, "")
	s.writeTask(w, r, projectId, taskId, uid, actor)
}

// toKeyFieldChanges 把契约请求映射为 domain 修改值（去除首尾空白）。
func toKeyFieldChanges(req EditTaskFieldsRequest) domain.KeyFieldChanges {
	out := domain.KeyFieldChanges{OwnerID: req.OwnerId, EndDate: toTimePtr(req.EndDate)}
	if req.Name != nil {
		v := strings.TrimSpace(*req.Name)
		out.Name = &v
	}
	if req.Description != nil {
		v := strings.TrimSpace(*req.Description)
		out.Description = &v
	}
	if req.CompletionCriteria != nil {
		v := strings.TrimSpace(*req.CompletionCriteria)
		out.CompletionCriteria = &v
	}
	return out
}

// changedFieldLabels 本次修改涉及字段的中文名（动态摘要与通知共用，对齐 §5.2.B 用词）。
func changedFieldLabels(c domain.KeyFieldChanges) []string {
	labels := []string{}
	if c.Name != nil {
		labels = append(labels, "任务名称")
	}
	if c.Description != nil {
		labels = append(labels, "任务说明")
	}
	if c.CompletionCriteria != nil {
		labels = append(labels, "完成标准")
	}
	if c.OwnerID != nil {
		labels = append(labels, "任务负责人")
	}
	if c.EndDate != nil {
		labels = append(labels, "截止时间")
	}
	return labels
}

func applyParams(taskID int64, c domain.KeyFieldChanges) store.ApplyTaskKeyFieldsParams {
	return store.ApplyTaskKeyFieldsParams{
		ID:                 taskID,
		Name:               toPgTextPtr(c.Name),
		Description:        toPgTextPtr(c.Description),
		CompletionCriteria: toPgTextPtr(c.CompletionCriteria),
		OwnerID:            toPgInt8(c.OwnerID),
		EndDate:            toPgDateFromTime(c.EndDate),
	}
}

func toPgTextPtr(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}

// cancelRequestView 组装契约 CancelRequest（含派生动作标志）。
func (s *Server) cancelRequestView(ctx context.Context, fc store.FieldChangeRequest, submittedByName string, decidedByName pgtype.Text,
	facts domain.TaskFacts, actor domain.Actor, userID int64,
) CancelRequest {
	canDecide := domain.DecideCancelRequestRule(actor, fc.State, facts, userID, true, "") == nil
	canAbandon := domain.CanAbandonCancelRequest(actor, userID, fc.SubmittedBy, fc.State, fc.Resolved)
	// AC-04：待审批显示「待{所属 KR 负责人姓名}审批」。
	krOwnerName := ""
	if facts.KrOwnerID != nil {
		if u, err := s.q.GetUserByID(ctx, *facts.KrOwnerID); err == nil {
			krOwnerName = u.DisplayName
		}
	}
	out := CancelRequest{
		Id:              fc.ID,
		State:           CancelRequestState(fc.State),
		StateLabel:      domain.CancelRequestStateLabel(fc.State, fc.Exempt, krOwnerName),
		Reason:          fc.Reason,
		Opinion:         optString(fc.Opinion),
		Resolved:        fc.Resolved,
		Exempt:          fc.Exempt,
		SubmittedById:   &fc.SubmittedBy,
		SubmittedByName: optString(submittedByName),
		DecidedByName:   fromPgText(decidedByName),
		CanDecide:       &canDecide,
		CanAbandon:      &canAbandon,
	}
	if fc.SubmittedAt.Valid {
		out.SubmittedAt = &fc.SubmittedAt.Time
	}
	if fc.DecidedAt.Valid {
		out.DecidedAt = &fc.DecidedAt.Time
	}
	return out
}
