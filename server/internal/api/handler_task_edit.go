package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// #172 裁决（第二刀）：关键字段修改直接生效（无变更单、无修改原因），
// 动作写入任务动态并站内通知所属 KR 负责人；裁决 10（#180）：修改权限收归项目管理员，
// 关闭申请机制退场（关闭见 handler_task_cancel.go）。业务规则在 domain，handler 仅编排。

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
	if err := domain.TaskEditRule(actor, uid, facts); err != nil {
		switch {
		case errors.Is(err, domain.ErrChangeForbidden):
			writeForbidden(w)
		case errors.Is(err, domain.ErrChangeNotAllowed):
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

