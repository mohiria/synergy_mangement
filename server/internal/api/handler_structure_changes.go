package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// 结构变更（输入、输入源、接收方）：#172 裁决后不再走变更单审批——
// 有编辑权限者直接修改生效（TaskEditRule 同口径），动作写入任务动态并通知所属 KR 负责人。

// structurePayload 一次结构变更的展示事实（差异行）+ 原样请求体；
// #172 后不再落库，仅用于当场执行、动态摘要与通知文案。
type structurePayload struct {
	Op       string          `json:"op"`
	Label    string          `json:"label"`
	OldValue string          `json:"oldValue"`
	NewValue string          `json:"newValue"`
	Request  json.RawMessage `json:"request"`
}

var errStructureUnknownOp = errors.New("未知的结构变更动作")

// routeStructureChange 判定本次结构变更能否直接执行，并给出统一的错误回报。
// 返回 ok=false 时响应已写出。
func (s *Server) routeStructureChange(w http.ResponseWriter, r *http.Request, taskID int64,
	actor domain.Actor, uid int64, facts domain.TaskFacts,
) bool {
	if err := domain.TaskEditRule(actor, uid, facts); err != nil {
		switch {
		case errors.Is(err, domain.ErrChangeForbidden):
			writeForbidden(w)
		case errors.Is(err, domain.ErrChangeNotAllowed):
			writeJSON(w, http.StatusConflict, Error{Code: "task_state_conflict", Message: err.Error()})
		default:
			writeInternalError(w, r, err)
		}
		return false
	}
	return true
}

// commitStructureChange 直接落地一次结构变更：执行写入并通知所属 KR 负责人，整个过程一个事务。
func (s *Server) commitStructureChange(w http.ResponseWriter, r *http.Request, projectID, taskID, uid int64,
	p structurePayload,
) bool {
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return false
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	if !s.applyStructureOrFail(w, r, qtx, projectID, taskID, uid, p) {
		return false
	}
	// 站内通知所属 KR 负责人（#172 裁决；本人修改不另发），与写入同事务。
	task, err := qtx.GetTaskInProject(r.Context(), store.GetTaskInProjectParams{ID: taskID, ProjectID: projectID})
	if err != nil {
		writeInternalError(w, r, err)
		return false
	}
	if target := domain.FieldEditNotifyTarget(uid, fromPgInt8(task.KrOwnerID)); target != nil {
		if _, err := qtx.CreateNotification(r.Context(), store.CreateNotificationParams{
			UserID: *target,
			Kind:   domain.NotifyTaskFieldEdited,
			Content: domain.FieldEditNotification(
				currentUser(r).DisplayName, task.Name, []string{p.Label}),
			ProjectID: pgtype.Int8{Int64: projectID, Valid: true},
			TaskID:    pgtype.Int8{Int64: taskID, Valid: true},
		}); err != nil {
			writeInternalError(w, r, err)
			return false
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w, r, err)
		return false
	}
	s.actionActivity(r.Context(), taskID, domain.ActivityFieldEdited, uid, p.Label)
	return true
}

func (s *Server) applyStructureOrFail(w http.ResponseWriter, r *http.Request, qtx *store.Queries,
	projectID, taskID, uid int64, p structurePayload,
) bool {
	if err := s.applyStructureChange(r.Context(), qtx, projectID, taskID, uid, p); err != nil {
		if errors.Is(err, errStructureUnknownOp) {
			writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_field_change", Message: err.Error()})
		} else {
			writeInternalError(w, r, err)
		}
		return false
	}
	return true
}

// applyStructureChange 真正写入一次结构变更（直改、免审与审批通过共用同一条路径）。
func (s *Server) applyStructureChange(ctx context.Context, qtx *store.Queries,
	projectID, taskID, uid int64, p structurePayload,
) error {
	if !domain.ValidStructureOp(p.Op) {
		return errStructureUnknownOp
	}
	switch p.Op {
	case domain.StructureAddTaskInput:
		var req CreateTaskInputRequest
		if err := json.Unmarshal(p.Request, &req); err != nil {
			return err
		}
		return applyAddTaskInput(ctx, qtx, projectID, taskID, uid, req)
	case domain.StructureRemoveEdge:
		var req struct {
			EdgeID int64 `json:"edgeId"`
		}
		if err := json.Unmarshal(p.Request, &req); err != nil {
			return err
		}
		_, err := qtx.DeleteEdge(ctx, req.EdgeID)
		return err
	}
	var req SetReceiversRequest
	if err := json.Unmarshal(p.Request, &req); err != nil {
		return err
	}
	return applySetReceivers(ctx, qtx, taskID, req)
}

func applyAddTaskInput(ctx context.Context, qtx *store.Queries, projectID, taskID, uid int64, req CreateTaskInputRequest) error {
	for _, sourceID := range req.SourceTaskIds {
		// name 列保留（历史值不动），建边时写一份当时的快照；带编号的完整标识读时现算（#112）。
		display := ""
		if src, err := qtx.GetTaskInProject(ctx, store.GetTaskInProjectParams{ID: sourceID, ProjectID: projectID}); err == nil {
			display = domain.EdgeDisplayName("", src.Name)
		}
		// #174 裁决：边不维护期望时间，展示与超期判断取上游任务截止日期。
		if _, err := qtx.CreateEdge(ctx, store.CreateEdgeParams{
			TargetTaskID: taskID,
			SourceTaskID: sourceID,
			Name:         display,
			Necessity:    string(req.Necessity),
			CreatedBy:    uid,
		}); err != nil {
			return err
		}
	}
	return nil
}

func applySetReceivers(ctx context.Context, qtx *store.Queries, taskID int64, req SetReceiversRequest) error {
	scope := string(req.Scope)
	if _, err := qtx.SetTaskReceiverScope(ctx, store.SetTaskReceiverScopeParams{ID: taskID, ReceiverScope: scope}); err != nil {
		return err
	}
	if _, err := qtx.ClearTaskReceivers(ctx, taskID); err != nil {
		return err
	}
	if scope != domain.ReceiverScopeMembers || req.UserIds == nil {
		return nil
	}
	seen := map[int64]bool{}
	for _, id := range *req.UserIds {
		if seen[id] {
			continue
		}
		seen[id] = true
		if err := qtx.SetTaskReceiver(ctx, store.SetTaskReceiverParams{TaskID: taskID, UserID: id}); err != nil {
			return err
		}
	}
	return nil
}

