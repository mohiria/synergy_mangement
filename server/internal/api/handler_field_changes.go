package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// 关键字段修改审批（AC-23）。业务规则在 domain，handler 仅编排。

// SubmitFieldChange 编辑任务／提交关键字段修改：草稿直接完善，KR 负责人本人免审即时生效，
// 其余进入审批且旧值继续生效。
func (s *Server) SubmitFieldChange(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64) {
	var req SubmitFieldChangeRequest
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
	task, facts, ok := s.fetchTask(w, r, projectId, taskId)
	if !ok {
		return
	}
	hasPending, err := s.q.HasPendingFieldChange(r.Context(), taskId)
	if err != nil {
		writeInternalError(w)
		return
	}
	outcome, err := domain.FieldChangeRoute(actor, uid, facts, hasPending)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrChangeForbidden):
			writeForbidden(w)
		case errors.Is(err, domain.ErrChangePendingExists), errors.Is(err, domain.ErrChangeNotAllowed):
			writeJSON(w, http.StatusConflict, Error{Code: "task_state_conflict", Message: err.Error()})
		default:
			writeInternalError(w)
		}
		return
	}
	changes := toKeyFieldChanges(req.Changes)
	reason := ""
	if req.Reason != nil {
		reason = strings.TrimSpace(*req.Reason)
	}
	members, err := s.q.ListProjectMembers(r.Context(), projectId)
	if err != nil {
		writeInternalError(w)
		return
	}
	memberSet := make(map[int64]bool, len(members))
	for _, m := range members {
		memberSet[m.UserID] = true
	}
	if err := domain.ValidateKeyFieldChanges(changes, reason, outcome != domain.FieldChangeDirect,
		func(id int64) bool { return memberSet[id] }, task.StartDate.Time); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_field_change", Message: err.Error()})
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	switch outcome {
	case domain.FieldChangeDirect:
		// 草稿完善：不生成变更单。
		if _, err := qtx.ApplyTaskKeyFields(r.Context(), applyParams(taskId, changes)); err != nil {
			writeInternalError(w)
			return
		}
	case domain.FieldChangeExempt:
		if _, err := qtx.CreateFieldChange(r.Context(), createFieldChangeParams(task, uid, reason, changes,
			domain.FieldChangeApprovedState, true, domain.FieldChangeExemptOpinion,
			pgtype.Int8{Int64: uid, Valid: true}, pgtype.Timestamptz{Time: s.now(), Valid: true})); err != nil {
			writeInternalError(w)
			return
		}
		if _, err := qtx.ApplyTaskKeyFields(r.Context(), applyParams(taskId, changes)); err != nil {
			writeInternalError(w)
			return
		}
	case domain.FieldChangePending:
		// 重新提交时清除本人此前的退回待处理事项。
		if _, err := qtx.ResolveRejectedFieldChanges(r.Context(), store.ResolveRejectedFieldChangesParams{TaskID: taskId, SubmittedBy: uid}); err != nil {
			writeInternalError(w)
			return
		}
		if _, err := qtx.CreateFieldChange(r.Context(), createFieldChangeParams(task, uid, reason, changes,
			domain.FieldChangePendingState, false, "", pgtype.Int8{}, pgtype.Timestamptz{})); err != nil {
			writeInternalError(w)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w)
		return
	}
	s.writeTask(w, r, projectId, taskId, uid, actor)
}

func (s *Server) DecideFieldChange(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64, changeId int64) {
	var req FieldChangeDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !req.Decision.Valid() {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(uid, proj.OwnerID, proj.MyRole)
	_, facts, ok := s.fetchTask(w, r, projectId, taskId)
	if !ok {
		return
	}
	fc, err := s.q.GetFieldChange(r.Context(), store.GetFieldChangeParams{ID: changeId, TaskID: taskId})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "change_not_found", Message: "变更单不存在"})
		} else {
			writeInternalError(w)
		}
		return
	}
	if err := domain.DecideFieldChangeRule(fc.State, facts.KrOwnerID, uid); err != nil {
		if errors.Is(err, domain.ErrChangeNotPending) {
			writeJSON(w, http.StatusConflict, Error{Code: "change_state_conflict", Message: err.Error()})
		} else {
			writeJSON(w, http.StatusForbidden, Error{Code: "forbidden", Message: err.Error()})
		}
		return
	}
	opinion := ""
	if req.Opinion != nil {
		opinion = strings.TrimSpace(*req.Opinion)
	}
	approve := req.Decision == FieldChangeDecisionRequestDecisionApproved
	newState := domain.FieldChangeRejectedState
	if approve {
		newState = domain.FieldChangeApprovedState
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	if _, err := qtx.DecideFieldChange(r.Context(), store.DecideFieldChangeParams{
		ID: changeId, State: newState, Opinion: opinion, DecidedBy: pgtype.Int8{Int64: uid, Valid: true},
	}); err != nil {
		writeInternalError(w)
		return
	}
	if approve {
		// AC-23：通过后拟议值成为当前值。
		if _, err := qtx.ApplyTaskKeyFields(r.Context(), store.ApplyTaskKeyFieldsParams{
			ID:                 taskId,
			Name:               fc.NewName,
			Description:        fc.NewDescription,
			CompletionCriteria: fc.NewCompletionCriteria,
			OwnerID:            fc.NewOwnerID,
			EndDate:            fc.NewEndDate,
		}); err != nil {
			writeInternalError(w)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w)
		return
	}
	s.writeTask(w, r, projectId, taskId, uid, actor)
}

func (s *Server) AbandonFieldChange(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64, changeId int64) {
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(uid, proj.OwnerID, proj.MyRole)
	if _, _, ok := s.fetchTask(w, r, projectId, taskId); !ok {
		return
	}
	fc, err := s.q.GetFieldChange(r.Context(), store.GetFieldChangeParams{ID: changeId, TaskID: taskId})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "change_not_found", Message: "变更单不存在"})
		} else {
			writeInternalError(w)
		}
		return
	}
	if fc.State != domain.FieldChangeRejectedState || fc.Resolved {
		writeJSON(w, http.StatusConflict, Error{Code: "change_state_conflict", Message: "只有退回未处理的变更可以放弃"})
		return
	}
	if !domain.CanAbandonFieldChange(actor, uid, fc.SubmittedBy, fc.State, fc.Resolved) {
		writeForbidden(w)
		return
	}
	if _, err := s.q.ResolveFieldChange(r.Context(), changeId); err != nil {
		writeInternalError(w)
		return
	}
	s.writeTask(w, r, projectId, taskId, uid, actor)
}

// toKeyFieldChanges 把契约请求映射为 domain 拟议值（去除首尾空白）。
func toKeyFieldChanges(c struct {
	CompletionCriteria *string             `json:"completionCriteria,omitempty"`
	Description        *string             `json:"description,omitempty"`
	EndDate            *openapi_types.Date `json:"endDate,omitempty"`
	Name               *string             `json:"name,omitempty"`
	OwnerId            *int64              `json:"ownerId,omitempty"`
}) domain.KeyFieldChanges {
	out := domain.KeyFieldChanges{OwnerID: c.OwnerId, EndDate: toTimePtr(c.EndDate)}
	if c.Name != nil {
		v := strings.TrimSpace(*c.Name)
		out.Name = &v
	}
	if c.Description != nil {
		v := strings.TrimSpace(*c.Description)
		out.Description = &v
	}
	if c.CompletionCriteria != nil {
		v := strings.TrimSpace(*c.CompletionCriteria)
		out.CompletionCriteria = &v
	}
	return out
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

// createFieldChangeParams 组装变更单：新值取拟议项，旧值快照取当前任务。
func createFieldChangeParams(task store.GetTaskInProjectRow, uid int64, reason string, c domain.KeyFieldChanges,
	state string, exempt bool, opinion string, decidedBy pgtype.Int8, decidedAt pgtype.Timestamptz,
) store.CreateFieldChangeParams {
	p := store.CreateFieldChangeParams{
		TaskID: task.ID, SubmittedBy: uid, Reason: reason, State: state,
		Exempt: exempt, Opinion: opinion, DecidedBy: decidedBy, DecidedAt: decidedAt,
		NewName:               toPgTextPtr(c.Name),
		NewDescription:        toPgTextPtr(c.Description),
		NewCompletionCriteria: toPgTextPtr(c.CompletionCriteria),
		NewOwnerID:            toPgInt8(c.OwnerID),
		NewEndDate:            toPgDateFromTime(c.EndDate),
	}
	if c.Name != nil {
		p.OldName = pgtype.Text{String: task.Name, Valid: true}
	}
	if c.Description != nil {
		p.OldDescription = pgtype.Text{String: task.Description, Valid: true}
	}
	if c.CompletionCriteria != nil {
		p.OldCompletionCriteria = pgtype.Text{String: task.CompletionCriteria, Valid: true}
	}
	if c.OwnerID != nil {
		p.OldOwnerID = pgtype.Int8{Int64: task.OwnerID, Valid: true}
	}
	if c.EndDate != nil {
		p.OldEndDate = task.EndDate
	}
	return p
}

func toPgTextPtr(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}

// fieldChangeView 组装契约 FieldChange（含差异展示与派生动作标志）。
func (s *Server) fieldChangeView(ctx context.Context, fc store.FieldChangeRequest, submittedByName string, decidedByName pgtype.Text,
	facts domain.TaskFacts, actor domain.Actor, userID int64,
) FieldChange {
	nameOf := func(id pgtype.Int8) string {
		if !id.Valid {
			return ""
		}
		if u, err := s.q.GetUserByID(ctx, id.Int64); err == nil {
			return u.DisplayName
		}
		return ""
	}
	diffs := []FieldChangeDiff{}
	addText := func(field, label string, oldV, newV pgtype.Text) {
		if newV.Valid {
			diffs = append(diffs, FieldChangeDiff{Field: field, Label: label, OldValue: oldV.String, NewValue: newV.String})
		}
	}
	addText("name", "任务名称", fc.OldName, fc.NewName)
	addText("description", "任务说明", fc.OldDescription, fc.NewDescription)
	addText("completionCriteria", "完成标准", fc.OldCompletionCriteria, fc.NewCompletionCriteria)
	if fc.NewOwnerID.Valid {
		diffs = append(diffs, FieldChangeDiff{Field: "ownerId", Label: "任务负责人", OldValue: nameOf(fc.OldOwnerID), NewValue: nameOf(fc.NewOwnerID)})
	}
	if fc.NewEndDate.Valid {
		old := ""
		if fc.OldEndDate.Valid {
			old = fc.OldEndDate.Time.Format("2006-01-02")
		}
		diffs = append(diffs, FieldChangeDiff{Field: "endDate", Label: "截止时间", OldValue: old, NewValue: fc.NewEndDate.Time.Format("2006-01-02")})
	}
	canDecide := domain.DecideFieldChangeRule(fc.State, facts.KrOwnerID, userID) == nil
	canAbandon := domain.CanAbandonFieldChange(actor, userID, fc.SubmittedBy, fc.State, fc.Resolved)
	// AC-04：待审批显示「待{所属 KR 负责人姓名}审批」。
	krOwnerName := ""
	if facts.KrOwnerID != nil {
		krOwnerName = nameOf(pgtype.Int8{Int64: *facts.KrOwnerID, Valid: true})
	}
	out := FieldChange{
		Id:              fc.ID,
		State:           FieldChangeState(fc.State),
		StateLabel:      domain.FieldChangeStateLabel(fc.State, fc.Exempt, krOwnerName),
		Reason:          fc.Reason,
		Opinion:         optString(fc.Opinion),
		Resolved:        fc.Resolved,
		Exempt:          fc.Exempt,
		Changes:         diffs,
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
