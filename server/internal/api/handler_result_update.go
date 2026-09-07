package api

import (
	"errors"
	"net/http"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// 成果更新（AC-66、§5.3）：已完成任务重新开放候选交付物上传并走同一道完成审批。
// 规则在 domain，handler 仅编排；任务生命周期状态全程保持已完成。

// StartResultUpdate 发起成果更新。
func (s *Server) StartResultUpdate(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64) {
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(currentUser(r), proj.OwnerID, proj.MyRole, proj.Visibility)

	// 先锁任务行再判定，避免两个并发请求各自看到「没有在途更新」（R2／R3）。
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
	if err := domain.StartResultUpdateRule(actor, uid, facts); err != nil {
		switch {
		case errors.Is(err, domain.ErrResultUpdateForbidden):
			writeForbidden(w)
		default:
			writeJSON(w, http.StatusConflict, Error{Code: "task_state_conflict", Message: err.Error()})
		}
		return
	}
	if _, err := qtx.SetTaskResultUpdate(r.Context(), store.SetTaskResultUpdateParams{
		ID: taskId, ResultUpdate: domain.ResultUpdateOpen,
	}); err != nil {
		writeInternalError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w, r, err)
		return
	}
	s.actionActivity(r.Context(), taskId, domain.ActivityResultUpdateStarted, uid, "")
	s.writeTask(w, r, projectId, taskId, uid, actor)
}

// resultUpdateState 成果更新进程的接口取值：domain 的空串在契约上是 none。
func resultUpdateState(state string) ResultUpdateState {
	switch state {
	case domain.ResultUpdateOpen:
		return ResultUpdateStateOpen
	case domain.ResultUpdateReviewing:
		return ResultUpdateStateReviewing
	}
	return ResultUpdateStateNone
}
