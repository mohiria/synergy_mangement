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

// unreadyRequiredInputs 汇总每个下游任务的必要输入未就绪注记（§5.1「等待输入」的唯一口径）：
// 值为「上游未就绪：缺 XX」，取首条未就绪的必要输入；成员来源按输入请求状态判定就绪。
func unreadyRequiredInputs(edges []store.ListEdgesByProjectRow, requests []store.ListInputRequestsByProjectRow) map[int64]string {
	stateByEdge := make(map[int64]string, len(requests))
	for _, ir := range requests {
		stateByEdge[ir.EdgeID] = ir.State
	}
	notes := map[int64]string{}
	for _, e := range edges {
		if e.Necessity != domain.NecessityRequired {
			continue
		}
		ready := domain.EdgeReady(e.CurrentFileID.Valid, e.HasCandidate)
		if state, ok := stateByEdge[e.ID]; ok {
			ready = domain.MemberEdgeReady(state)
		}
		if ready {
			continue
		}
		if _, seen := notes[e.TargetTaskID]; !seen {
			notes[e.TargetTaskID] = "上游未就绪：缺 " + e.Name
		}
	}
	return notes
}

// unreadyRequiredInputsByProject 取项目边与输入请求后汇总未就绪注记。
func (s *Server) unreadyRequiredInputsByProject(ctx context.Context, projectID int64) (map[int64]string, error) {
	edges, err := s.q.ListEdgesByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	requests, err := s.q.ListInputRequestsByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return unreadyRequiredInputs(edges, requests), nil
}

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
	requestRows, err := s.q.ListInputRequestsByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	requestByEdge := make(map[int64]store.ListInputRequestsByProjectRow, len(requestRows))
	for _, ir := range requestRows {
		requestByEdge[ir.EdgeID] = ir
	}
	// 硬依赖分析（AC-10）：循环互锁与关键路径。工期取任务计划天数（截止-开始+1）。
	taskRows, err := s.q.ListProjectTasks(ctx, projectID)
	if err != nil {
		return nil, err
	}
	durations := make(map[int64]int, len(taskRows))
	krOwnerNameByTask := make(map[int64]string, len(taskRows))
	for _, t := range taskRows {
		krOwnerNameByTask[t.ID] = t.KrOwnerName.String
		if t.StartDate.Valid && t.EndDate.Valid {
			d := int(t.EndDate.Time.Sub(t.StartDate.Time).Hours()/24) + 1
			if d < 1 {
				d = 1
			}
			durations[t.ID] = d
		}
	}
	// 来源任务状态显示文案（AC-04）：或签中任务取审核组姓名。
	reviewerRows, err := s.q.IntermediateReviewerNamesByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	reviewerNamesByTask := make(map[int64][]string)
	for _, rv := range reviewerRows {
		reviewerNamesByTask[rv.TaskID] = append(reviewerNamesByTask[rv.TaskID], rv.DisplayName)
	}
	hardEdges := []domain.HardEdge{}
	for _, e := range rows {
		if e.EdgeType == domain.EdgeHardPrerequisite && e.SourceTaskID.Valid {
			hardEdges = append(hardEdges, domain.HardEdge{ID: e.ID, Source: e.SourceTaskID.Int64, Target: e.TargetTaskID})
		}
	}
	analysis := domain.AnalyzeHardEdges(hardEdges, durations)
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
			sourceID := e.SourceTaskID.Int64
			label := domain.StatusLabel(e.SourceTaskStatus.String, krOwnerNameByTask[sourceID], reviewerNamesByTask[sourceID])
			item.SourceTaskStatusLabel = &label
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
		if e.EdgeType == domain.EdgeHardPrerequisite {
			interlock := analysis.Interlocked[e.ID]
			item.InterlockRisk = &interlock
			if analysis.CriticalPathAvailable {
				onPath := analysis.OnCriticalPath[e.ID]
				item.OnCriticalPath = &onPath
			}
		}
		// 成员来源：附输入请求，就绪按「已提供」判定（AC-30、词汇表「输入就绪」）。
		if ir, ok := requestByEdge[e.ID]; ok {
			view := s.inputRequestView(store.InputRequest{
				ID: ir.ID, EdgeID: ir.EdgeID, ProviderID: ir.ProviderID, ContentNote: ir.ContentNote,
				State: ir.State, NotifiedAt: ir.NotifiedAt, AcceptedAt: ir.AcceptedAt, ProvidedAt: ir.ProvidedAt,
				ProvidedText: ir.ProvidedText, FileName: ir.FileName, ObjectKey: ir.ObjectKey, CreatedAt: ir.CreatedAt,
			}, ir.ProviderName, userID)
			item.InputRequest = &view
			item.Ready = domain.MemberEdgeReady(ir.State)
		}
		out = append(out, item)
	}
	return out, nil
}
