package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// 结构变更（输入、输入源、输出、接收方）走关键字段修改审批（AC-23、§5.2.B）。
// 三条路由与标量关键字段同源：草稿直接生效、KR 负责人本人免审即时生效、其余进入审批。
// 待审批期间结构不变，通过时才由 applyStructureChange 真正写入。

// structurePayload 变更单里存的结构变更：展示用差异行 + 原样保留的请求体。
// 请求体在提交时已校验过一遍；审批通过时按 Op 重新执行。
type structurePayload struct {
	Op       string          `json:"op"`
	Label    string          `json:"label"`
	OldValue string          `json:"oldValue"`
	NewValue string          `json:"newValue"`
	Request  json.RawMessage `json:"request"`
}

var errStructureUnknownOp = errors.New("变更单记录了未知的结构变更动作")

// routeStructureChange 判定本次结构变更走哪条路由，并给出统一的错误回报。
// 返回 ok=false 时响应已写出。
func (s *Server) routeStructureChange(w http.ResponseWriter, r *http.Request, taskID int64,
	actor domain.Actor, uid int64, facts domain.TaskFacts,
) (domain.FieldChangeOutcome, bool) {
	hasPending, err := s.q.HasPendingFieldChange(r.Context(), taskID)
	if err != nil {
		writeInternalError(w, r, err)
		return 0, false
	}
	outcome, err := domain.StructureChangeRoute(actor, uid, facts, hasPending)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrChangeForbidden):
			writeForbidden(w)
		case errors.Is(err, domain.ErrChangePendingExists), errors.Is(err, domain.ErrChangeNotAllowed):
			writeJSON(w, http.StatusConflict, Error{Code: "task_state_conflict", Message: err.Error()})
		default:
			writeInternalError(w, r, err)
		}
		return 0, false
	}
	return outcome, true
}

// commitStructureChange 按路由落地一次结构变更：直改与免审当场执行，
// 进入审批时只落一张待审批变更单。整个过程一个事务。
func (s *Server) commitStructureChange(w http.ResponseWriter, r *http.Request, projectID, taskID, uid int64,
	outcome domain.FieldChangeOutcome, p structurePayload, reason string,
) bool {
	raw, err := json.Marshal(p)
	if err != nil {
		writeInternalError(w, r, err)
		return false
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return false
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	switch outcome {
	case domain.FieldChangeDirect:
		if !s.applyStructureOrFail(w, r, qtx, projectID, taskID, uid, p) {
			return false
		}
	case domain.FieldChangeExempt:
		if _, err := qtx.CreateFieldChange(r.Context(), structureChangeParams(taskID, uid, reason, raw,
			domain.FieldChangeApprovedState, true, domain.FieldChangeExemptOpinion,
			pgtype.Int8{Int64: uid, Valid: true}, pgtype.Timestamptz{Time: s.now(), Valid: true})); err != nil {
			writeInternalError(w, r, err)
			return false
		}
		if !s.applyStructureOrFail(w, r, qtx, projectID, taskID, uid, p) {
			return false
		}
	default:
		// 重新提交时清除本人此前的退回待处理事项，与标量关键字段同口径。
		if _, err := qtx.ResolveRejectedFieldChanges(r.Context(),
			store.ResolveRejectedFieldChangesParams{TaskID: taskID, SubmittedBy: uid}); err != nil {
			writeInternalError(w, r, err)
			return false
		}
		if _, err := qtx.CreateFieldChange(r.Context(), structureChangeParams(taskID, uid, reason, raw,
			domain.FieldChangePendingState, false, "", pgtype.Int8{}, pgtype.Timestamptz{})); err != nil {
			writeInternalError(w, r, err)
			return false
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w, r, err)
		return false
	}
	switch outcome {
	case domain.FieldChangeExempt:
		s.actionActivity(r.Context(), taskID, domain.ActivityFieldChangeApproved, uid, domain.FieldChangeExemptOpinion)
	case domain.FieldChangePending:
		s.actionActivity(r.Context(), taskID, domain.ActivityFieldChangeSubmitted, uid, reason)
	}
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
		return applyAddTaskInput(ctx, qtx, taskID, uid, req)
	case domain.StructureAddMemberInput:
		var req CreateMemberInputRequest
		if err := json.Unmarshal(p.Request, &req); err != nil {
			return err
		}
		return s.applyAddMemberInput(ctx, qtx, projectID, taskID, uid, req)
	case domain.StructureRemoveEdge:
		var req struct {
			EdgeID int64 `json:"edgeId"`
		}
		if err := json.Unmarshal(p.Request, &req); err != nil {
			return err
		}
		_, err := qtx.DeleteEdge(ctx, req.EdgeID)
		return err
	case domain.StructureAddDeliverable:
		var req CreateDeliverableRequest
		if err := json.Unmarshal(p.Request, &req); err != nil {
			return err
		}
		_, err := qtx.CreateDeliverable(ctx, store.CreateDeliverableParams{
			TaskID: taskID, Name: strings.TrimSpace(req.Name), CreatedBy: uid,
		})
		return err
	}
	var req SetReceiversRequest
	if err := json.Unmarshal(p.Request, &req); err != nil {
		return err
	}
	return applySetReceivers(ctx, qtx, taskID, req)
}

func applyAddTaskInput(ctx context.Context, qtx *store.Queries, taskID, uid int64, req CreateTaskInputRequest) error {
	deliverable := pgtype.Int8{}
	if req.DeliverableId != nil {
		deliverable = pgtype.Int8{Int64: *req.DeliverableId, Valid: true}
	}
	for _, sourceID := range req.SourceTaskIds {
		if _, err := qtx.CreateEdge(ctx, store.CreateEdgeParams{
			TargetTaskID:  taskID,
			SourceTaskID:  pgtype.Int8{Int64: sourceID, Valid: true},
			DeliverableID: deliverable,
			Name:          strings.TrimSpace(req.Name),
			EdgeType:      string(req.EdgeType),
			Necessity:     string(req.Necessity),
			ExpectedDate:  toPgDate(req.ExpectedDate),
			CreatedBy:     uid,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) applyAddMemberInput(ctx context.Context, qtx *store.Queries, projectID, taskID, uid int64, req CreateMemberInputRequest) error {
	task, err := qtx.GetTaskInProject(ctx, store.GetTaskInProjectParams{ID: taskID, ProjectID: projectID})
	if err != nil {
		return err
	}
	// 草稿与待入池审批阶段不提前打扰对接人（§7.3）。
	pooled := task.Status != domain.TaskDraft && task.Status != domain.TaskPendingPoolReview
	name := strings.TrimSpace(req.Name)
	note := strings.TrimSpace(req.ContentNote)
	for _, providerID := range req.ProviderIds {
		edge, err := qtx.CreateEdge(ctx, store.CreateEdgeParams{
			TargetTaskID: taskID,
			SourceUserID: pgtype.Int8{Int64: providerID, Valid: true},
			Name:         name,
			EdgeType:     domain.EdgeInformation,
			Necessity:    string(req.Necessity),
			ExpectedDate: pgtype.Date{Time: req.ExpectedDate.Time, Valid: true},
			CreatedBy:    uid,
		})
		if err != nil {
			return err
		}
		notified := pgtype.Timestamptz{}
		if pooled {
			notified = pgtype.Timestamptz{Time: s.now(), Valid: true}
		}
		if _, err := qtx.CreateInputRequest(ctx, store.CreateInputRequestParams{
			EdgeID: edge.ID, ProviderID: providerID, ContentNote: note, NotifiedAt: notified,
		}); err != nil {
			return err
		}
		if pooled {
			// AC-29：带上下文的站内通知，每名对接人各发一条。
			if _, err := qtx.CreateNotification(ctx, store.CreateNotificationParams{
				UserID:    providerID,
				Kind:      domain.NotifyInputRequest,
				Content:   fmt.Sprintf("请你为任务「%s」提供输入「%s」：%s", task.Name, name, note),
				ProjectID: pgtype.Int8{Int64: projectID, Valid: true},
				TaskID:    pgtype.Int8{Int64: taskID, Valid: true},
			}); err != nil {
				return err
			}
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

// structureChangeParams 组装结构变更单：变更字段与差异行都在 payload 里。
func structureChangeParams(taskID, uid int64, reason string, payload []byte, state string,
	exempt bool, opinion string, decidedBy pgtype.Int8, decidedAt pgtype.Timestamptz,
) store.CreateFieldChangeParams {
	return store.CreateFieldChangeParams{
		TaskID: taskID, SubmittedBy: uid, Reason: reason, State: state,
		Exempt: exempt, Opinion: opinion, DecidedBy: decidedBy, DecidedAt: decidedAt,
		ChangeType: domain.FieldChangeTypeStructure,
		Payload:    payload,
	}
}
