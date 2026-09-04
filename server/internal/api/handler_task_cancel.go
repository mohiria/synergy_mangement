package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// 任务关闭（AC-57、裁决 10，#180）：项目管理员直接操作——原因必填、即时生效、
// 写任务动态，无审批环节。规则在 domain，handler 仅编排。

// CloseTask 项目管理员直接关闭任务。
func (s *Server) CloseTask(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64) {
	var req CloseTaskRequest
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
	reason := strings.TrimSpace(req.Reason)
	if err := domain.ValidateCancelReason(reason); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "cancel_reason_required", Message: err.Error()})
		return
	}

	// 规则与写入同事务：先锁任务行再判定，避免与并发的审批提交互踩（R2／R3）。
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
	if err := domain.CloseTaskRule(actor, facts); err != nil {
		switch {
		case errors.Is(err, domain.ErrCancelForbidden):
			writeForbidden(w)
		case errors.Is(err, domain.ErrCannotCancel), errors.Is(err, domain.ErrCancelPendingExists):
			writeJSON(w, http.StatusConflict, Error{Code: "task_state_conflict", Message: err.Error()})
		default:
			writeInternalError(w, r, err)
		}
		return
	}
	if _, err := qtx.UpdateTaskStatusWithReason(r.Context(), store.UpdateTaskStatusWithReasonParams{
		ID: taskId, Status: domain.TaskCancelled, CancelReason: reason,
	}); err != nil {
		writeInternalError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w, r, err)
		return
	}
	s.actionActivity(r.Context(), taskId, domain.ActivityTaskClosed, uid, reason)
	s.writeTask(w, r, projectId, taskId, uid, actor)
}
