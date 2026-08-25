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

// 必要输入与交付物边（AC-07、AC-28、AC-48）。业务规则在 domain，handler 仅编排。

// CreateTaskInput 新增输入要求：自动建立「来源任务 → 目标任务」的交付物边（AC-28）。
func (s *Server) CreateTaskInput(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64) {
	var req CreateTaskInputRequest
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
	_, facts, ok := s.fetchTask(w, r, projectId, taskId)
	if !ok {
		return
	}
	if !domain.CanConfigureInputs(actor, uid, facts) {
		writeForbidden(w)
		return
	}
	edge := domain.NewEdge{
		Name:         strings.TrimSpace(req.Name),
		EdgeType:     string(req.EdgeType),
		Necessity:    string(req.Necessity),
		SourceTaskID: &req.SourceTaskId,
		TargetTaskID: taskId,
	}
	if err := domain.ValidateNewEdge(edge); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_edge", Message: err.Error()})
		return
	}
	// 来源任务必须属于本项目；指定交付物项时必须挂在来源任务上。
	if _, err := s.q.GetTaskInProject(r.Context(), store.GetTaskInProjectParams{ID: req.SourceTaskId, ProjectID: projectId}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_source_task", Message: "来源任务不存在"})
			return
		}
		writeInternalError(w)
		return
	}
	if req.DeliverableId != nil {
		d, err := s.q.GetDeliverableInProject(r.Context(), store.GetDeliverableInProjectParams{ID: *req.DeliverableId, ID_2: req.SourceTaskId, ProjectID: projectId})
		if err != nil || d.TaskID != req.SourceTaskId {
			writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_deliverable", Message: "交付物项不属于来源任务"})
			return
		}
	}
	created, err := s.q.CreateEdge(r.Context(), store.CreateEdgeParams{
		TargetTaskID: taskId,
		SourceTaskID: pgtype.Int8{Int64: req.SourceTaskId, Valid: true},
		DeliverableID: func() pgtype.Int8 {
			if req.DeliverableId == nil {
				return pgtype.Int8{}
			}
			return pgtype.Int8{Int64: *req.DeliverableId, Valid: true}
		}(),
		Name:         edge.Name,
		EdgeType:     edge.EdgeType,
		Necessity:    edge.Necessity,
		ExpectedDate: toPgDate(req.ExpectedDate),
		CreatedBy:    uid,
	})
	if err != nil {
		writeInternalError(w)
		return
	}
	views, err := s.edgeViews(r.Context(), projectId, uid, actor)
	if err != nil {
		writeInternalError(w)
		return
	}
	for _, v := range views {
		if v.Id == created.ID {
			writeJSON(w, http.StatusCreated, v)
			return
		}
	}
	writeInternalError(w)
}

func (s *Server) RemoveEdge(w http.ResponseWriter, r *http.Request, projectId int64, edgeId int64) {
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(uid, proj.OwnerID, proj.MyRole)
	edge, err := s.q.GetEdgeInProject(r.Context(), store.GetEdgeInProjectParams{ID: edgeId, ProjectID: projectId})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "edge_not_found", Message: "交付物边不存在"})
		} else {
			writeInternalError(w)
		}
		return
	}
	facts := domain.TaskFacts{Status: edge.TargetStatus, CreatorID: edge.TargetCreatedBy, OwnerID: edge.TargetOwnerID}
	if !domain.CanConfigureInputs(actor, uid, facts) {
		writeForbidden(w)
		return
	}
	if _, err := s.q.DeleteEdge(r.Context(), edgeId); err != nil {
		writeInternalError(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) ListEdges(w http.ResponseWriter, r *http.Request, projectId int64) {
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	views, err := s.edgeViews(r.Context(), projectId, uid, projectActor(uid, proj.OwnerID, proj.MyRole))
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, views)
}

// edgeViews 组装项目全部交付物边（就绪状态与动作标志派生）。
func (s *Server) edgeViews(ctx context.Context, projectID, userID int64, actor domain.Actor) ([]DeliverableEdge, error) {
	rows, err := s.q.ListEdgesByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]DeliverableEdge, 0, len(rows))
	for _, e := range rows {
		hasCurrent := e.CurrentFileID.Valid
		facts := domain.TaskFacts{Status: "", CreatorID: e.TargetCreatedBy, OwnerID: e.TargetOwnerID}
		// 解除权限沿目标任务判定；状态从行内不可得时按非终态处理（列表行含 target 状态即可）。
		facts.Status = domain.TaskInProgress
		canRemove := domain.CanConfigureInputs(actor, userID, facts)
		item := DeliverableEdge{
			Id:           e.ID,
			Name:         e.Name,
			EdgeType:     EdgeType(e.EdgeType),
			Necessity:    Necessity(e.Necessity),
			TargetTaskId: e.TargetTaskID,
			Ready:        domain.EdgeReady(hasCurrent, e.HasCandidate),
			HasCandidate: e.HasCandidate,
			CanRemove:    &canRemove,
		}
		item.TargetTaskName = optString(e.TargetTaskName)
		item.SourceTaskId = fromPgInt8(e.SourceTaskID)
		item.SourceTaskName = fromPgText(e.SourceTaskName)
		if e.SourceTaskStatus.Valid {
			st := TaskStatus(e.SourceTaskStatus.String)
			item.SourceTaskStatus = &st
		}
		if e.SourceOwnerName.Valid {
			item.SourceOwnerName = &e.SourceOwnerName.String
		} else if e.SourceUserName.Valid {
			item.SourceOwnerName = &e.SourceUserName.String
		}
		item.DeliverableId = fromPgInt8(e.DeliverableID)
		item.DeliverableName = fromPgText(e.DeliverableName)
		if e.CurrentFileID.Valid {
			item.CurrentFileId = &e.CurrentFileID.Int64
			item.CurrentFileName = fromPgText(e.CurrentFileName)
		}
		item.ExpectedDate = fromPgDate(e.ExpectedDate)
		out = append(out, item)
	}
	return out, nil
}
