package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// 必要输入与交付物边（AC-07、AC-28、AC-48）。业务规则在 domain，handler 仅编排。

// unreadyRequiredInputs 汇总每个下游任务的必要输入未就绪注记（§5.1「等待输入」的唯一口径）：
// 值为「上游未就绪：缺 XX」，取首条未就绪的必要输入；成员来源按输入请求状态判定就绪。
// 同一任务的多条输入（AC-53 多来源）各自独立判定，任一必要输入未就绪即等待输入。
func unreadyRequiredInputs(edges []store.ListEdgesByProjectRow, requests []store.ListInputRequestsByProjectRow) map[int64]string {
	stateByEdge := make(map[int64]string, len(requests))
	noteByEdge := make(map[int64]string, len(requests))
	for _, ir := range requests {
		stateByEdge[ir.EdgeID] = ir.State
		noteByEdge[ir.EdgeID] = ir.ContentNote
	}
	targets := []int64{}
	byTarget := map[int64][]domain.InputEdgeState{}
	for _, e := range edges {
		ready := domain.EdgeReady(e.SourceTaskStatus.String)
		if state, ok := stateByEdge[e.ID]; ok {
			ready = domain.MemberEdgeReady(state)
		}
		if _, seen := byTarget[e.TargetTaskID]; !seen {
			targets = append(targets, e.TargetTaskID)
		}
		// 缺哪一项按派生标识说（#112）：注记里不带任务编号，读作「缺 <来源任务>」或「缺 <所需内容摘要>」。
		byTarget[e.TargetTaskID] = append(byTarget[e.TargetTaskID],
			domain.InputEdgeState{
				Name:      domain.EdgeDisplayName("", e.SourceTaskName.String, noteByEdge[e.ID]),
				Necessity: e.Necessity, Ready: ready,
			})
	}
	notes := map[int64]string{}
	for _, taskID := range targets {
		if missing := domain.FirstUnmetRequiredInput(byTarget[taskID]); missing != "" {
			notes[taskID] = "上游未就绪：缺 " + missing
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

// CreateTaskInput 新增输入要求：为每个选中的来源任务分别建立「来源任务 → 目标任务」交付物边
// （AC-28；AC-53 一次可多选来源任务，多条边一并建立或一并失败）。
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
	actor := projectActor(uid, proj.OwnerID, proj.MyRole, proj.Visibility)
	_, facts, ok := s.fetchTask(w, r, projectId, taskId)
	if !ok {
		return
	}
	inputs := domain.NewTaskInputs{
		Necessity:     string(req.Necessity),
		SourceTaskIDs: req.SourceTaskIds,
		TargetTaskID:  taskId,
	}
	if err := domain.ValidateNewTaskInputs(inputs); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_edge", Message: err.Error()})
		return
	}
	// 每个来源任务都必须属于本项目。
	for _, sourceID := range inputs.SourceTaskIDs {
		if _, err := s.q.GetTaskInProject(r.Context(), store.GetTaskInProjectParams{ID: sourceID, ProjectID: projectId}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_source_task", Message: "来源任务不存在"})
				return
			}
			writeInternalError(w, r, err)
			return
		}
	}
	// 输入与输入源是关键字段（§5.2.B；#172 裁决改直接生效）。
	if !s.routeStructureChange(w, r, taskId, actor, uid, facts) {
		return
	}
	sourceNames := make([]string, 0, len(inputs.SourceTaskIDs))
	for _, sourceID := range inputs.SourceTaskIDs {
		if src, err := s.q.GetTaskInProject(r.Context(), store.GetTaskInProjectParams{ID: sourceID, ProjectID: projectId}); err == nil {
			sourceNames = append(sourceNames, src.Name)
		}
	}
	raw, err := json.Marshal(req)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	payload := structurePayload{
		Op:       domain.StructureAddTaskInput,
		Label:    domain.StructureFieldLabel(domain.StructureAddTaskInput),
		OldValue: "—",
		NewValue: fmt.Sprintf("新增输入源，来源任务：%s", strings.Join(sourceNames, "、")),
		Request:  raw,
	}
	if !s.commitStructureChange(w, r, projectId, taskId, uid, payload) {
		return
	}
	s.writeTask(w, r, projectId, taskId, uid, actor)
}

func (s *Server) RemoveEdge(w http.ResponseWriter, r *http.Request, projectId int64, edgeId int64) {
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(uid, proj.OwnerID, proj.MyRole, proj.Visibility)
	edge, err := s.q.GetEdgeInProject(r.Context(), store.GetEdgeInProjectParams{ID: edgeId, ProjectID: projectId})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "edge_not_found", Message: "交付物边不存在"})
		} else {
			writeInternalError(w, r, err)
		}
		return
	}
	facts := domain.TaskFacts{
		Status: edge.TargetStatus, CreatorID: edge.TargetCreatedBy, OwnerID: edge.TargetOwnerID,
		KrOwnerID: fromPgInt8(edge.TargetKrOwnerID),
	}
	// 解除输入源同样是关键字段变更（§5.5；#172 裁决改直接生效）。
	if !s.routeStructureChange(w, r, edge.TargetTaskID, actor, uid, facts) {
		return
	}
	raw, err := json.Marshal(struct {
		EdgeID int64 `json:"edgeId"`
	}{EdgeID: edgeId})
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	payload := structurePayload{
		Op:       domain.StructureRemoveEdge,
		Label:    domain.StructureFieldLabel(domain.StructureRemoveEdge),
		OldValue: edge.Name,
		NewValue: "解除该输入关系",
		Request:  raw,
	}
	if !s.commitStructureChange(w, r, projectId, edge.TargetTaskID, uid, payload) {
		return
	}
	s.writeTask(w, r, projectId, edge.TargetTaskID, uid, actor)
}

func (s *Server) ListEdges(w http.ResponseWriter, r *http.Request, projectId int64, params ListEdgesParams) {
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	views, err := s.edgeViews(r.Context(), projectId, uid, projectActor(uid, proj.OwnerID, proj.MyRole, proj.Visibility))
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	if params.KrId == nil {
		writeJSON(w, http.StatusOK, views)
		return
	}
	// 服务端裁剪（P1）：只留两端任一端落在该 KR 下的边——
	// 与图谱的 KR 任务关系层同口径，跨 KR 的边照样保留，关系不会被裁断。
	tasks, err := s.q.ListProjectTasks(r.Context(), projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	inKr := map[int64]bool{}
	for _, t := range tasks {
		if t.KeyResultID == *params.KrId {
			inKr[t.ID] = true
		}
	}
	out := make([]DeliverableEdge, 0, len(views))
	for _, e := range views {
		if inKr[e.TargetTaskId] || (e.SourceTaskId != nil && inKr[*e.SourceTaskId]) {
			out = append(out, e)
		}
	}
	writeJSON(w, http.StatusOK, out)
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
	// 成员来源的输入源标识取「所需内容」摘要（#112），按边先索引一份。
	noteByEdge := make(map[int64]string, len(requestRows))
	for _, ir := range requestRows {
		requestByEdge[ir.EdgeID] = ir
		noteByEdge[ir.EdgeID] = ir.ContentNote
	}
	// 硬依赖分析（AC-10）：循环互锁与关键路径。工期取任务计划天数（截止-开始+1）。
	taskRows, err := s.q.ListProjectTasks(ctx, projectID)
	if err != nil {
		return nil, err
	}
	durations := make(map[int64]int, len(taskRows))
	krOwnerNameByTask := make(map[int64]string, len(taskRows))
	// 来源任务编号（#101）：编号是持久字段，由 O／KR／任务三级序号拼出，前端不再自己拼。
	codeByTask := make(map[int64]string, len(taskRows))
	for _, t := range taskRows {
		krOwnerNameByTask[t.ID] = t.KrOwnerName.String
		codeByTask[t.ID] = domain.TaskCode(int(t.ObjectiveCodeSeq), int(t.KrCodeSeq), int(t.CodeSeq))
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
	// 裁决 J1（#142）＋裁决 #163：「当前交付物」列按来源任务列出全部已生效当前内容
	// （一项显示「类型 · 大小」，多项显示「N 项」）。
	currentFileRows, err := s.q.ListCurrentFilesByProjectTask(ctx, projectID)
	if err != nil {
		return nil, err
	}
	currentFilesByTask := make(map[int64][]EdgeCurrentFile)
	for _, f := range currentFileRows {
		currentFilesByTask[f.TaskID] = append(currentFilesByTask[f.TaskID], EdgeCurrentFile{
			FileId:        f.FileID,
			FileName:      f.FileName,
			FileTypeLabel: domain.FileTypeLabel(f.FileName),
			FileSize:      f.FileSize,
		})
	}
	// #173 裁决：互锁与关键路径沿「必要」边分析。
	requiredEdges := []domain.RequiredEdge{}
	for _, e := range rows {
		if e.Necessity == domain.NecessityRequired && e.SourceTaskID.Valid {
			requiredEdges = append(requiredEdges, domain.RequiredEdge{ID: e.ID, Source: e.SourceTaskID.Int64, Target: e.TargetTaskID})
		}
	}
	analysis := domain.AnalyzeRequiredEdges(requiredEdges, durations)
	out := make([]DeliverableEdge, 0, len(rows))
	for _, e := range rows {
		facts := domain.TaskFacts{Status: "", CreatorID: e.TargetCreatedBy, OwnerID: e.TargetOwnerID}
		// 解除权限沿目标任务判定；状态从行内不可得时按非终态处理（列表行含 target 状态即可）。
		facts.Status = domain.TaskInProgress
		canRemove := domain.CanConfigureInputs(actor, userID, facts)
		item := DeliverableEdge{
			Id:             e.ID,
			Name:           e.Name,
			Necessity:      Necessity(e.Necessity),
			NecessityLabel: domain.NecessityLabel(e.Necessity),
			TargetTaskId:   e.TargetTaskID,
			Ready:          domain.EdgeReady(e.SourceTaskStatus.String),
			CanRemove:      &canRemove,
		}
		item.TargetTaskName = optString(e.TargetTaskName)
		item.SourceTaskId = fromPgInt8(e.SourceTaskID)
		item.SourceTaskName = fromPgText(e.SourceTaskName)
		sourceCode := ""
		if e.SourceTaskID.Valid {
			if code := codeByTask[e.SourceTaskID.Int64]; code != "" {
				sourceCode = code
				item.SourceTaskCode = &code
			}
		}
		// 输入源标识读时现算（裁决 F1、#112）：库里的 name 只是建边当时的快照，
		// 来源任务改名后要跟着变，成员来源则取「所需内容」摘要。
		item.Name = domain.EdgeDisplayName(sourceCode, e.SourceTaskName.String, noteByEdge[e.ID])
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
		if e.SourceTaskID.Valid {
			if files := currentFilesByTask[e.SourceTaskID.Int64]; len(files) > 0 {
				item.SourceCurrentFiles = &files
			}
		}
		item.ExpectedDate = fromPgDate(e.ExpectedDate)
		if e.Necessity == domain.NecessityRequired {
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
