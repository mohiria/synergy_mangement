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

// 任务取消申请（AC-57）：复用关键字段变更单的一种变更类型，规则在 domain，handler 仅编排。

// RequestTaskCancellation 发起取消申请：KR 负责人在本人负责 KR 下免审即时生效，其余进入其待我审批。
func (s *Server) RequestTaskCancellation(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64) {
	var req TaskCancellationRequest
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
	reason := strings.TrimSpace(req.Reason)
	if err := domain.ValidateCancelReason(reason); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "cancel_reason_required", Message: err.Error()})
		return
	}

	// 互斥判定与写入同事务：先锁任务行再读未决单，避免两个并发请求各自看到「没有未决单」（R2／R3）。
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
	hasPending, err := qtx.HasPendingFieldChange(r.Context(), taskId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	outcome, err := domain.CancelRoute(actor, uid, facts, hasPending)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrCancelForbidden):
			writeForbidden(w)
		case errors.Is(err, domain.ErrCannotCancel), errors.Is(err, domain.ErrCancelPendingExists):
			writeJSON(w, http.StatusConflict, Error{Code: "task_state_conflict", Message: err.Error()})
		case errors.Is(err, domain.ErrKrOwnerMissing):
			writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "kr_owner_missing", Message: err.Error()})
		default:
			writeInternalError(w, r, err)
		}
		return
	}
	switch outcome {
	case domain.FieldChangeExempt:
		if _, err := qtx.CreateFieldChange(r.Context(), cancelChangeParams(taskId, facts.Status, uid, reason,
			domain.FieldChangeApprovedState, true, domain.CancelExemptOpinion,
			pgtype.Int8{Int64: uid, Valid: true}, pgtype.Timestamptz{Time: s.now(), Valid: true})); err != nil {
			writeInternalError(w, r, err)
			return
		}
		if _, err := qtx.UpdateTaskStatusWithReason(r.Context(), store.UpdateTaskStatusWithReasonParams{
			ID: taskId, Status: domain.TaskCancelled, CancelReason: reason,
		}); err != nil {
			writeInternalError(w, r, err)
			return
		}
	default:
		if _, err := qtx.CreateFieldChange(r.Context(), cancelChangeParams(taskId, facts.Status, uid, reason,
			domain.FieldChangePendingState, false, "", pgtype.Int8{}, pgtype.Timestamptz{})); err != nil {
			writeInternalError(w, r, err)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w, r, err)
		return
	}
	if outcome == domain.FieldChangeExempt {
		s.actionActivity(r.Context(), taskId, domain.ActivityCancelApproved, uid, domain.CancelExemptOpinion)
	} else {
		s.actionActivity(r.Context(), taskId, domain.ActivityCancelRequested, uid, reason)
	}
	s.writeTask(w, r, projectId, taskId, uid, actor)
}

// cancelChangeParams 组装取消单：变更字段＝任务状态，旧值＝当前状态，拟议值＝已取消（PRD §5.2.B）。
func cancelChangeParams(taskID int64, currentStatus string, uid int64, reason, state string,
	exempt bool, opinion string, decidedBy pgtype.Int8, decidedAt pgtype.Timestamptz,
) store.CreateFieldChangeParams {
	return store.CreateFieldChangeParams{
		TaskID: taskID, SubmittedBy: uid, Reason: reason, State: state,
		Exempt: exempt, Opinion: opinion, DecidedBy: decidedBy, DecidedAt: decidedAt,
		ChangeType: domain.FieldChangeTypeCancel,
		OldStatus:  pgtype.Text{String: currentStatus, Valid: true},
		NewStatus:  pgtype.Text{String: domain.TaskCancelled, Valid: true},
	}
}
