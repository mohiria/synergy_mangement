package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// 接收方与接收记录（词汇表「接收方」「接收记录」；模块 PRD §8.6；MW-09）。
// 业务规则在 domain.receiver，handler 仅编排。

// SetTaskReceivers 配置接收方：接收方属关键字段（§5.2.B），与输入／输出同口径走变更审批。
func (s *Server) SetTaskReceivers(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64) {
	var req SetReceiversRequest
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
	currentReceivers, err := s.q.ListTaskReceivers(r.Context(), taskId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	currentReceiverNames := make([]string, 0, len(currentReceivers))
	for _, rv := range currentReceivers {
		currentReceiverNames = append(currentReceiverNames, rv.DisplayName)
	}
	members, err := s.q.ListProjectMembers(r.Context(), projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	isMember := make(map[int64]bool, len(members))
	for _, m := range members {
		isMember[m.UserID] = true
	}
	scope := string(req.Scope)
	ids := []int64{}
	seen := map[int64]bool{}
	if req.UserIds != nil {
		for _, id := range *req.UserIds {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	if err := domain.ValidateReceivers(scope, ids, func(id int64) bool { return isMember[id] }); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_receivers", Message: err.Error()})
		return
	}
	if scope != domain.ReceiverScopeMembers {
		ids = nil
	}
	// 接收方是关键字段（§5.2.B）：已入池任务改名单要经所属 KR 负责人审批。
	outcome, ok := s.routeStructureChange(w, r, taskId, actor, uid, facts)
	if !ok {
		return
	}
	nameByID := make(map[int64]string, len(members))
	for _, m := range members {
		nameByID[m.UserID] = m.DisplayName
	}
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		names = append(names, nameByID[id])
	}
	raw, err := json.Marshal(SetReceiversRequest{Scope: ReceiverScope(scope), UserIds: &ids})
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	payload := structurePayload{
		Op:       domain.StructureSetReceivers,
		Label:    domain.StructureFieldLabel(domain.StructureSetReceivers),
		OldValue: receiverScopeSummary(task.ReceiverScope, currentReceiverNames),
		NewValue: receiverScopeSummary(scope, names),
		Request:  raw,
	}
	if !s.commitStructureChange(w, r, projectId, taskId, uid, outcome, payload,
		"接收方改为"+receiverScopeSummary(scope, names)) {
		return
	}
	s.writeTask(w, r, projectId, taskId, uid, actor)
}

// receiverScopeSummary 接收方配置的展示文案（变更单差异行用）。
func receiverScopeSummary(scope string, names []string) string {
	if scope == domain.ReceiverScopeMembers {
		return domain.ReceiverScopeLabel(scope) + "：" + strings.Join(names, "、")
	}
	return domain.ReceiverScopeLabel(scope)
}

// ConfirmTaskReceipt 接收方确认接收：待接收项退出「待我接收」并成为接收记录，动作进任务动态。
func (s *Server) ConfirmTaskReceipt(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64) {
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(uid, proj.OwnerID, proj.MyRole, proj.Visibility)
	if _, _, ok := s.fetchTask(w, r, projectId, taskId); !ok {
		return
	}
	row, err := s.q.GetTaskReceipt(r.Context(), store.GetTaskReceiptParams{TaskID: taskId, UserID: uid})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "receipt_not_found", Message: "没有属于本人的待接收项"})
		} else {
			writeInternalError(w, r, err)
		}
		return
	}
	fact := domain.ReceiptFact{ID: row.ID, TaskID: row.TaskID, UserID: row.UserID, UserName: row.DisplayName}
	if row.ConfirmedAt.Valid {
		at := row.ConfirmedAt.Time
		fact.ConfirmedAt = &at
	}
	if err := domain.CanConfirmReceipt(actor, uid, fact); err != nil {
		switch {
		case errors.Is(err, domain.ErrReceiptConfirmed):
			writeJSON(w, http.StatusConflict, Error{Code: "receipt_confirmed", Message: err.Error()})
		default:
			writeJSON(w, http.StatusForbidden, Error{Code: "forbidden", Message: err.Error()})
		}
		return
	}
	if _, err := s.q.ConfirmTaskReceipt(r.Context(), row.ID); err != nil {
		writeInternalError(w, r, err)
		return
	}
	s.actionActivity(r.Context(), taskId, domain.ActivityReceiptConfirmed, uid, row.DisplayName)
	s.writeTask(w, r, projectId, taskId, uid, actor)
}

// generateReceipts 终审通过时按当时接收方名单逐人生成待接收项（模块 PRD §8.6）。
// 与业务写在同一事务内，重复终审由唯一键保证不重复记账。
func generateReceipts(ctx context.Context, q *store.Queries, projectID, taskID int64, scope string) error {
	if scope == domain.ReceiverScopeNone {
		return nil
	}
	var memberIDs []int64
	if scope == domain.ReceiverScopeAll {
		members, err := q.ListProjectMembers(ctx, projectID)
		if err != nil {
			return err
		}
		for _, m := range members {
			memberIDs = append(memberIDs, m.UserID)
		}
	}
	var receiverIDs []int64
	if scope == domain.ReceiverScopeMembers {
		rows, err := q.ListTaskReceivers(ctx, taskID)
		if err != nil {
			return err
		}
		for _, rv := range rows {
			receiverIDs = append(receiverIDs, rv.UserID)
		}
	}
	for _, id := range domain.ReceiptTargets(scope, receiverIDs, memberIDs) {
		if err := q.CreateTaskReceipt(ctx, store.CreateTaskReceiptParams{TaskID: taskID, UserID: id}); err != nil {
			return err
		}
	}
	return nil
}

// receiptList 任务的待接收项与接收记录视图。
func (s *Server) receiptList(ctx context.Context, taskID int64) ([]TaskReceipt, error) {
	rows, err := s.q.ListTaskReceipts(ctx, taskID)
	if err != nil {
		return nil, err
	}
	out := make([]TaskReceipt, 0, len(rows))
	for _, rc := range rows {
		item := TaskReceipt{Id: rc.ID, UserId: rc.UserID, DisplayName: rc.DisplayName}
		if rc.GeneratedAt.Valid {
			item.GeneratedAt = rc.GeneratedAt.Time
		}
		if rc.ConfirmedAt.Valid {
			at := rc.ConfirmedAt.Time
			item.ConfirmedAt = &at
		}
		out = append(out, item)
	}
	return out, nil
}
