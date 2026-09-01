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

// 任务创建与直接入池（裁决 #162）。业务规则在 domain，handler 仅编排。

func (s *Server) ListTasks(w http.ResponseWriter, r *http.Request, projectId int64, params ListTasksParams) {
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	resp, err := s.taskList(r.Context(), projectId, uid, projectActor(uid, proj.OwnerID, proj.MyRole, proj.Visibility))
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	// 服务端裁剪（P1）：大项目不必整表下发。派生仍按整个项目算——
	// 卡点、就绪度与风险都依赖跨 KR 的关系，先裁剪会算错。
	out := make([]Task, 0, len(resp))
	for _, t := range resp {
		if params.KrId != nil && t.KeyResultId != *params.KrId {
			continue
		}
		if params.IncludeCompleted != nil && !*params.IncludeCompleted && t.Status == TaskStatusCompleted {
			continue
		}
		out = append(out, t)
		if params.Limit != nil && len(out) >= *params.Limit {
			break
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) CreateTaskBatch(w http.ResponseWriter, r *http.Request, projectId int64) {
	var req CreateTaskBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Items) == 0 {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析或为空"})
		return
	}
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(uid, proj.OwnerID, proj.MyRole, proj.Visibility)
	if !domain.CanCreateTask(actor) {
		writeForbidden(w)
		return
	}
	members, err := s.q.ListProjectMembers(r.Context(), projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	roleByID := make(map[int64]string, len(members))
	for _, m := range members {
		roleByID[m.UserID] = m.Role
	}
	// 通过任务创建邀请响应时（AC-03）：邀请必须待处理、发给本人，且本批含指定 KR 的任务。
	var invite store.GetTaskInviteInProjectRow
	if req.TaskInviteId != nil {
		invite, err = s.q.GetTaskInviteInProject(r.Context(), store.GetTaskInviteInProjectParams{ID: *req.TaskInviteId, ProjectID: projectId})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, Error{Code: "invite_not_found", Message: "邀请不存在"})
				return
			}
			writeInternalError(w, r, err)
			return
		}
		itemKrIDs := make([]int64, 0, len(req.Items))
		for _, item := range req.Items {
			itemKrIDs = append(itemKrIDs, item.KeyResultId)
		}
		if err := domain.FulfillInvite(invite.State, invite.InviteeID, uid, invite.KeyResultID, itemKrIDs); err != nil {
			switch {
			case errors.Is(err, domain.ErrInviteNotPending):
				writeJSON(w, http.StatusConflict, Error{Code: "invite_state_conflict", Message: err.Error()})
			case errors.Is(err, domain.ErrInviteNotInvitee):
				writeJSON(w, http.StatusForbidden, Error{Code: "forbidden", Message: err.Error()})
			default:
				writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_invite_response", Message: err.Error()})
			}
			return
		}
	}
	// 逐项校验：所属 KR 属于本项目、最小骨架合法。
	krs := make([]store.GetKeyResultInProjectRow, len(req.Items))
	for i, item := range req.Items {
		kr, err := s.q.GetKeyResultInProject(r.Context(), store.GetKeyResultInProjectParams{ID: item.KeyResultId, ProjectID: projectId})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_key_result", Message: "所属 KR 不存在"})
				return
			}
			writeInternalError(w, r, err)
			return
		}
		krs[i] = kr
		nt := domain.NewTask{Name: strings.TrimSpace(item.Name), OwnerID: item.OwnerId, Start: item.StartDate.Time, End: item.EndDate.Time}
		if err := domain.ValidateNewTask(nt, func(id int64) string { return roleByID[id] }); err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_task", Message: err.Error()})
			return
		}
		// 裁决 #164：五个选填字段沿用各配置项既有的校验器。
		isMember := func(id int64) bool { return roleByID[id] != "" }
		if item.ParticipantIds != nil {
			if err := domain.ValidateParticipants(item.OwnerId, *item.ParticipantIds, isMember); err != nil {
				writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_participants", Message: err.Error()})
				return
			}
		}
		if item.ReviewerIds != nil {
			if err := domain.ValidateReviewers(*item.ReviewerIds, func(id int64) string { return roleByID[id] }); err != nil {
				writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_reviewers", Message: err.Error()})
				return
			}
		}
		if item.ReceiverScope != nil {
			ids := []int64{}
			if item.ReceiverIds != nil {
				ids = *item.ReceiverIds
			}
			if err := domain.ValidateReceivers(string(*item.ReceiverScope), ids, isMember); err != nil {
				writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_receivers", Message: err.Error()})
				return
			}
		}
	}
	// 整批一个事务：全部成功或全部失败。裁决 #162：创建即入正式任务池，初始状态未开始；
	// 补偿机制——入池通知所属 KR 负责人（本人创建不另发），通知与创建同事务。
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	createdIDs := make([]int64, 0, len(req.Items))
	for i, item := range req.Items {
		params := store.CreateTaskParams{
			KeyResultID: item.KeyResultId,
			Name:        strings.TrimSpace(item.Name),
			OwnerID:     item.OwnerId,
			StartDate:   pgtype.Date{Time: item.StartDate.Time, Valid: true},
			EndDate:     pgtype.Date{Time: item.EndDate.Time, Valid: true},
			Status:      domain.TaskNotStarted,
			CreatedBy:   uid,
		}
		if item.Description != nil {
			params.Description = strings.TrimSpace(*item.Description)
		}
		if item.CompletionCriteria != nil {
			params.CompletionCriteria = strings.TrimSpace(*item.CompletionCriteria)
		}
		task, err := qtx.CreateTask(r.Context(), params)
		if err != nil {
			writeInternalError(w, r, err)
			return
		}
		createdIDs = append(createdIDs, task.ID)
		if target := domain.PoolEntryNotifyTarget(uid, fromPgInt8(krs[i].OwnerID)); target != nil {
			krCode := domain.KeyResultCode(int(krs[i].ObjectiveCodeSeq), int(krs[i].CodeSeq))
			if _, err := qtx.CreateNotification(r.Context(), store.CreateNotificationParams{
				UserID:    *target,
				Kind:      domain.NotifyTaskPoolEntered,
				Content:   domain.PoolEntryNotification(currentUser(r).DisplayName, task.Name, krCode, krs[i].Description),
				ProjectID: pgtype.Int8{Int64: projectId, Valid: true},
				TaskID:    pgtype.Int8{Int64: task.ID, Valid: true},
			}); err != nil {
				writeInternalError(w, r, err)
				return
			}
		}
		// 裁决 #164：参与人、成果审核人与接收方随创建一并落库（选填）。
		if item.ParticipantIds != nil {
			for _, id := range domain.NormalizeParticipants(*item.ParticipantIds) {
				if err := qtx.SetTaskParticipant(r.Context(), store.SetTaskParticipantParams{TaskID: task.ID, UserID: id}); err != nil {
					writeInternalError(w, r, err)
					return
				}
			}
		}
		if item.ReviewerIds != nil {
			for _, id := range domain.NormalizeParticipants(*item.ReviewerIds) {
				if err := qtx.SetTaskReviewer(r.Context(), store.SetTaskReviewerParams{TaskID: task.ID, UserID: id}); err != nil {
					writeInternalError(w, r, err)
					return
				}
			}
		}
		if item.ReceiverScope != nil && *item.ReceiverScope != ReceiverScopeNone {
			if _, err := qtx.SetTaskReceiverScope(r.Context(), store.SetTaskReceiverScopeParams{ID: task.ID, ReceiverScope: string(*item.ReceiverScope)}); err != nil {
				writeInternalError(w, r, err)
				return
			}
			if *item.ReceiverScope == ReceiverScopeMembers && item.ReceiverIds != nil {
				for _, id := range domain.NormalizeParticipants(*item.ReceiverIds) {
					if err := qtx.SetTaskReceiver(r.Context(), store.SetTaskReceiverParams{TaskID: task.ID, UserID: id}); err != nil {
						writeInternalError(w, r, err)
						return
					}
				}
			}
		}
	}
	if req.TaskInviteId != nil {
		// 邀请随本批创建一并完成（词汇表：受邀人通过该邀请创建关联任务后结束）。
		if _, err := qtx.UpdateTaskInviteState(r.Context(), store.UpdateTaskInviteStateParams{ID: invite.ID, State: domain.TaskInviteCompleted}); err != nil {
			writeInternalError(w, r, err)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w, r, err)
		return
	}
	// 入池留痕：每个新任务记一条「任务入池」动态（裁决 #162 补偿机制）。
	for _, id := range createdIDs {
		s.actionActivity(r.Context(), id, domain.ActivityPoolEntered, uid, "")
	}
	resp, err := s.taskList(r.Context(), projectId, uid, actor)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

// UpdateTaskStatus 手工流转：开始执行（AC-12；完成走三道审批，取消走 RequestTaskCancellation）。
func (s *Server) UpdateTaskStatus(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64) {
	var req UpdateTaskStatusRequest
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
	switch req.Status {
	case UpdateTaskStatusRequestStatusInProgress:
		if uid != task.OwnerID && !domain.CanEditProject(actor) {
			writeForbidden(w)
			return
		}
		newStatus, err := domain.StartTask(facts.Status)
		if err != nil {
			writeJSON(w, http.StatusConflict, Error{Code: "task_state_conflict", Message: err.Error()})
			return
		}
		if _, err := s.q.UpdateTaskStatus(r.Context(), store.UpdateTaskStatusParams{ID: taskId, Status: newStatus}); err != nil {
			writeInternalError(w, r, err)
			return
		}
	default:
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "不支持的目标状态"})
		return
	}
	s.writeTask(w, r, projectId, taskId, uid, actor)
}

// UpdateTaskProgress 更新或清除可选进度（AC-12）。
func (s *Server) UpdateTaskProgress(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64) {
	var req UpdateTaskProgressRequest
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
	if err := domain.ValidateProgress(req.Progress); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_progress", Message: err.Error()})
		return
	}
	if !domain.CanUpdateProgress(actor, uid, facts) {
		if facts.Status == domain.TaskCompleted {
			// AC-63：完成终审通过后进度锁定为 100%，不再接受写入。
			writeJSON(w, http.StatusConflict, Error{Code: "task_state_conflict", Message: "任务已完成，进度锁定为 100%"})
			return
		}
		if facts.Status != domain.TaskInProgress {
			writeJSON(w, http.StatusConflict, Error{Code: "task_state_conflict", Message: "仅进行中的任务可以更新进度"})
			return
		}
		writeForbidden(w)
		return
	}
	if _, err := s.q.UpdateTaskProgress(r.Context(), store.UpdateTaskProgressParams{ID: taskId, Progress: toPgInt4(req.Progress)}); err != nil {
		writeInternalError(w, r, err)
		return
	}
	s.writeTask(w, r, projectId, taskId, uid, actor)
}

func toPgInt4(v *int) pgtype.Int4 {
	if v == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: int32(*v), Valid: true}
}

func fromPgInt4(v pgtype.Int4) *int {
	if !v.Valid {
		return nil
	}
	n := int(v.Int32)
	return &n
}

// GetTaskDetail 任务详情抽屉（AC-31／AC-34）：全体项目成员可查看，动作按派生标志。
func (s *Server) GetTaskDetail(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64) {
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(uid, proj.OwnerID, proj.MyRole, proj.Visibility)
	task, taskFacts, ok := s.fetchTask(w, r, projectId, taskId)
	if !ok {
		return
	}
	kr, err := s.q.GetKeyResultInProject(r.Context(), store.GetKeyResultInProjectParams{ID: task.KeyResultID, ProjectID: projectId})
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	obj, err := s.q.GetObjective(r.Context(), store.GetObjectiveParams{ID: kr.ObjectiveID, ProjectID: projectId})
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	changeRows, err := s.q.ListFieldChangesByTask(r.Context(), taskId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	deliverables, err := s.deliverableList(r.Context(), taskId, actor, uid, taskFacts)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	// 过程文件与重要外部材料（§7.7）：与交付物并列展示，但不进审批、不作正式输入。
	taskFiles, err := s.taskFileList(r.Context(), taskId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	discussions, err := s.discussionList(r.Context(), taskId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	activities, err := s.activityList(r.Context(), taskId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	factsForReviews := domain.TaskFacts{Status: task.Status, CreatorID: task.CreatedBy, OwnerID: task.OwnerID,
		KrOwnerID: fromPgInt8(task.KrOwnerID), ResultUpdate: task.ResultUpdate}
	completions, err := s.completionReviewList(r.Context(), taskId, factsForReviews, actor, uid)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	allEdges, err := s.edgeViews(r.Context(), projectId, uid, actor)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	reviewerRows, err := s.q.ListTaskReviewers(r.Context(), taskId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	reviewerViews := make([]ReviewerInfo, 0, len(reviewerRows))
	for _, rv := range reviewerRows {
		reviewerViews = append(reviewerViews, ReviewerInfo{UserId: rv.UserID, DisplayName: rv.DisplayName})
	}
	allBlockers, err := s.projectBlockers(r.Context(), projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	// #129：抽屉卡点行的提醒按钮显隐同样把当日配额算进去。
	remindCounts, err := s.remindCountsToday(r.Context(), uid)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	taskBlockers := []Blocker{}
	for _, b := range allBlockers {
		if b.TaskID == taskId {
			taskBlockers = append(taskBlockers, blockerView(b, actor, uid, projectSettingsOf(proj).RemindDailyLimit, remindCounts))
		}
	}
	inputs, outputs := []DeliverableEdge{}, []DeliverableEdge{}
	for _, e := range allEdges {
		if e.TargetTaskId == taskId {
			inputs = append(inputs, e)
		}
		if e.SourceTaskId != nil && *e.SourceTaskId == taskId {
			outputs = append(outputs, e)
		}
	}
	list, err := s.taskList(r.Context(), projectId, uid, actor)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	var item *Task
	for i := range list {
		if list[i].Id == taskId {
			item = &list[i]
		}
	}
	if item == nil {
		writeInternalError(w, r, err)
		return
	}
	facts := domain.TaskFacts{Status: string(item.Status), CreatorID: task.CreatedBy, OwnerID: task.OwnerID,
		KrOwnerID: fromPgInt8(task.KrOwnerID), ResultUpdate: task.ResultUpdate}
	fcs := make([]FieldChange, 0, len(changeRows))
	for _, fc := range changeRows {
		fcs = append(fcs, s.fieldChangeView(r.Context(), store.FieldChangeRequest{
			ID: fc.ID, TaskID: fc.TaskID, SubmittedBy: fc.SubmittedBy, Reason: fc.Reason,
			State: fc.State, Exempt: fc.Exempt, Opinion: fc.Opinion, Resolved: fc.Resolved,
			ChangeType: fc.ChangeType, OldStatus: fc.OldStatus, NewStatus: fc.NewStatus, Payload: fc.Payload,
			OldName: fc.OldName, NewName: fc.NewName,
			OldDescription: fc.OldDescription, NewDescription: fc.NewDescription,
			OldCompletionCriteria: fc.OldCompletionCriteria, NewCompletionCriteria: fc.NewCompletionCriteria,
			OldOwnerID: fc.OldOwnerID, NewOwnerID: fc.NewOwnerID,
			OldEndDate: fc.OldEndDate, NewEndDate: fc.NewEndDate,
			SubmittedAt: fc.SubmittedAt, DecidedBy: fc.DecidedBy, DecidedAt: fc.DecidedAt,
		}, fc.SubmittedByName, fc.DecidedByName, facts, actor, uid))
	}
	// 协作关系摘要（AC-50、词汇表「协作关系摘要」）：分组与合并规则在 domain，
	// 这里只把对方任务的展示事实（名称、所属 KR、负责人、状态文案）贴上去。
	relFacts := make([]domain.RelationEdgeFact, 0, len(allEdges))
	for _, e := range allEdges {
		relFacts = append(relFacts, domain.RelationEdgeFact{
			EdgeType: string(e.EdgeType), SourceTaskID: e.SourceTaskId,
			TargetTaskID: e.TargetTaskId, Ready: e.Ready,
		})
	}
	upRefs, downRefs := domain.RelationSummary(taskId, relFacts)
	krDescByID := map[int64]string{}
	if krRows, err := s.q.ListKeyResultsByProject(r.Context(), projectId); err == nil {
		for _, k := range krRows {
			krDescByID[k.ID] = k.Description
		}
	}
	taskByID := make(map[int64]*Task, len(list))
	for i := range list {
		taskByID[list[i].Id] = &list[i]
	}
	relationViews := func(refs []domain.TaskRelationRef) []TaskRelation {
		out := make([]TaskRelation, 0, len(refs))
		for _, ref := range refs {
			other, ok := taskByID[ref.TaskID]
			if !ok {
				continue
			}
			out = append(out, TaskRelation{
				TaskId:          other.Id,
				TaskName:        other.Name,
				KrDescription:   krDescByID[other.KeyResultId],
				EdgeType:        EdgeType(ref.EdgeType),
				EdgeTypeLabel:   optString(domain.EdgeTypeLabel(ref.EdgeType)),
				OwnerName:       other.OwnerName,
				TaskStatusLabel: other.StatusLabel,
				Ready:           ref.Ready,
			})
		}
		return out
	}
	// 受影响 O／KR（协作关系 PRD §8.1）：只沿下游硬前置边推导，规则在 domain。
	objectiveRows, err := s.q.ListObjectives(r.Context(), projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	objectiveTitleByID := make(map[int64]string, len(objectiveRows))
	for _, o := range objectiveRows {
		objectiveTitleByID[o.ID] = o.Title
	}
	krOwnerObjective := map[int64]int64{}
	if krRows, err := s.q.ListKeyResultsByProject(r.Context(), projectId); err == nil {
		for _, k := range krRows {
			krOwnerObjective[k.ID] = k.ObjectiveID
		}
	}
	impactFacts := make(map[int64]domain.ImpactTaskFact, len(list))
	for i := range list {
		t := list[i]
		objID := krOwnerObjective[t.KeyResultId]
		impactFacts[t.Id] = domain.ImpactTaskFact{
			TaskID: t.Id, KeyResultID: t.KeyResultId, KrDescription: krDescByID[t.KeyResultId],
			ObjectiveID: objID, ObjectiveTitle: objectiveTitleByID[objID],
		}
	}
	impactEdges := make([]domain.ImpactEdgeFact, 0, len(allEdges))
	for _, e := range allEdges {
		impactEdges = append(impactEdges, domain.ImpactEdgeFact{
			SourceTaskID: e.SourceTaskId, TargetTaskID: e.TargetTaskId, EdgeType: string(e.EdgeType),
		})
	}
	impacted := make([]ImpactedTarget, 0)
	for _, it := range domain.ImpactedObjectives(taskId, impactFacts, impactEdges) {
		impacted = append(impacted, ImpactedTarget{
			KeyResultId: it.KeyResultID, KrDescription: it.KrDescription,
			ObjectiveId: it.ObjectiveID, ObjectiveTitle: it.ObjectiveTitle,
		})
	}
	receipts, err := s.receiptList(r.Context(), taskId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	// F1：未决审批计数由后端派生，前端不再把审批单各自过滤后相加。
	pendingCount := 0
	for _, fc := range fcs {
		if fc.State == FieldChangeStatePending {
			pendingCount++
		}
	}
	for _, cr := range completions {
		if cr.State == CompletionReviewStateIntermediateReview || cr.State == CompletionReviewStatePendingFinal {
			pendingCount++
		}
	}
	item.PendingReviewCount = &pendingCount
	writeJSON(w, http.StatusOK, TaskDetail{
		Task:              *item,
		ObjectiveTitle:    obj.Title,
		KrDescription:     kr.Description,
		FieldChanges:      fcs,
		Deliverables:      deliverables,
		Files:             &taskFiles,
		Discussions:       discussions,
		CompletionReviews: completions,
		Reviewers:         reviewerViews,
		Blockers:          taskBlockers,
		Inputs:            inputs,
		Outputs:           outputs,
		ImpactedTargets:   impacted,
		Upstream:          relationViews(upRefs),
		Downstream:        relationViews(downRefs),
		Activities:        activities,
		Receipts:          receipts,
	})
}

// fetchTask 读取任务与流转判定所需事实；不存在时已写出 404 并返回 false。
func (s *Server) fetchTask(w http.ResponseWriter, r *http.Request, projectID, taskID int64) (store.GetTaskInProjectRow, domain.TaskFacts, bool) {
	task, err := s.q.GetTaskInProject(r.Context(), store.GetTaskInProjectParams{ID: taskID, ProjectID: projectID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "task_not_found", Message: "任务不存在"})
		} else {
			writeInternalError(w, r, err)
		}
		return store.GetTaskInProjectRow{}, domain.TaskFacts{}, false
	}
	facts := domain.TaskFacts{
		Status:       task.Status,
		CreatorID:    task.CreatedBy,
		OwnerID:      task.OwnerID,
		KrOwnerID:    fromPgInt8(task.KrOwnerID),
		ResultUpdate: task.ResultUpdate,
	}
	return task, facts, true
}

// lockTaskFacts 在事务内对任务行加写锁并重读事实：三道审批的决策必须在锁内重跑规则，
// 否则并发决策会各自基于过期状态写库（或签通过与退回同时发生即可产出「已完成但无当前交付物」）。
func lockTaskFacts(r *http.Request, w http.ResponseWriter, qtx *store.Queries, projectID, taskID int64) (store.LockTaskInProjectRow, domain.TaskFacts, bool) {
	task, err := qtx.LockTaskInProject(r.Context(), store.LockTaskInProjectParams{ID: taskID, ProjectID: projectID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "task_not_found", Message: "任务不存在"})
		} else {
			writeInternalError(w, r, err)
		}
		return store.LockTaskInProjectRow{}, domain.TaskFacts{}, false
	}
	facts := domain.TaskFacts{
		Status:       task.Status,
		CreatorID:    task.CreatedBy,
		OwnerID:      task.OwnerID,
		KrOwnerID:    fromPgInt8(task.KrOwnerID),
		ResultUpdate: task.ResultUpdate,
	}
	return task, facts, true
}

// taskList 组装项目全部任务及派生字段（负责人姓名、动作标志）。
func (s *Server) taskList(ctx context.Context, projectID, userID int64, actor domain.Actor) ([]Task, error) {
	rows, err := s.q.ListProjectTasks(ctx, projectID)
	if err != nil {
		return nil, err
	}
	// 每个任务最近一张需要关注的变更单（待审批＝拟议值标示，退回未处理＝退回待处理事项）。
	changeRows, err := s.q.LatestFieldChangesByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	changeByTask := make(map[int64]store.LatestFieldChangesByProjectRow, len(changeRows))
	for _, fc := range changeRows {
		changeByTask[fc.TaskID] = fc
	}
	// 必要输入未就绪的任务显示「等待输入」（AC-48、§5.1）；成员来源按输入请求状态判定就绪。
	unreadyNoteByTask, err := s.unreadyRequiredInputsByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	// 派生卡点数量（列表徽标用）。
	blockers, err := s.projectBlockers(ctx, projectID)
	if err != nil {
		return nil, err
	}
	openBlockersByTask := map[int64]int{}
	for _, b := range blockers {
		openBlockersByTask[b.TaskID]++
	}
	// 候选内容数量（提交完成申请的派生标志用）。
	candidateRows, err := s.q.CandidateCountsByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	candidatesByTask := make(map[int64]int, len(candidateRows))
	for _, c := range candidateRows {
		candidatesByTask[c.TaskID] = int(c.N)
	}
	// 「预期交付物」列：任务的交付物项名称。
	deliverableRows, err := s.q.ListDeliverableNamesByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	namesByTask := make(map[int64][]string)
	for _, d := range deliverableRows {
		namesByTask[d.TaskID] = append(namesByTask[d.TaskID], d.Name)
	}
	// 接收方名单与本人待接收项（MW-09、模块 PRD §8.6）。
	receiverRows, err := s.q.ListReceiversByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	receiversByTask := make(map[int64][]ReviewerInfo)
	for _, rv := range receiverRows {
		receiversByTask[rv.TaskID] = append(receiversByTask[rv.TaskID], ReviewerInfo{UserId: rv.UserID, DisplayName: rv.DisplayName})
	}
	// 参与人名单（词汇表「参与人」；§9.2 按需字段，只作展示与检索）。
	participantRows, err := s.q.ListParticipantsByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	participantsByTask := make(map[int64][]ReviewerInfo)
	for _, pt := range participantRows {
		participantsByTask[pt.TaskID] = append(participantsByTask[pt.TaskID], ReviewerInfo{UserId: pt.UserID, DisplayName: pt.DisplayName})
	}
	receiptRows, err := s.q.ListReceiptsByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	pendingReceiptByTask := map[int64]bool{}
	for _, rc := range receiptRows {
		if rc.UserID == userID && !rc.ConfirmedAt.Valid {
			pendingReceiptByTask[rc.TaskID] = true
		}
	}
	// 或签中任务的当前审核组姓名（AC-04 显示文案）。
	reviewerRows, err := s.q.IntermediateReviewerNamesByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	reviewerNamesByTask := make(map[int64][]string)
	for _, rv := range reviewerRows {
		reviewerNamesByTask[rv.TaskID] = append(reviewerNamesByTask[rv.TaskID], rv.DisplayName)
	}
	resp := make([]Task, 0, len(rows))
	for _, t := range rows {
		facts := domain.TaskFacts{Status: t.Status, CreatorID: t.CreatedBy, OwnerID: t.OwnerID,
			KrOwnerID: fromPgInt8(t.KrOwnerID), ResultUpdate: t.ResultUpdate}
		// 待审批变更单（含关闭单）决定编辑与取消入口是否可用（AC-23、AC-57 互斥）。
		fc, hasChange := changeByTask[t.ID]
		hasPending := hasChange && fc.State == domain.FieldChangePendingState
		item := Task{
			Id:                 t.ID,
			KeyResultId:        t.KeyResultID,
			Code:               domain.TaskCode(int(t.ObjectiveCodeSeq), int(t.KrCodeSeq), int(t.CodeSeq)),
			Name:               t.Name,
			OwnerId:            t.OwnerID,
			OwnerName:          t.OwnerName,
			StartDate:          *fromPgDate(t.StartDate),
			EndDate:            *fromPgDate(t.EndDate),
			UpdatedAt:          t.UpdatedAt.Time,
			Status:             TaskStatus(t.Status),
			Description:        optString(t.Description),
			CompletionCriteria: optString(t.CompletionCriteria),
			Progress:           domain.DisplayProgress(t.Status, fromPgInt4(t.Progress)),
			CancelReason:       optString(t.CancelReason),
			CanStart:           domain.CanStartTask(actor, userID, facts),
			CanUpdateProgress:  domain.CanUpdateProgress(actor, userID, facts),
			CanCancel:          domain.CanCancelTask(actor, userID, facts, hasPending),
		}
		// 页面主状态汇总：必要输入未到显示「等待输入」（存储态保持真实执行状态）。
		displayFacts := facts
		displayFacts.Status = domain.DeriveDisplayStatus(t.Status, unreadyNoteByTask[t.ID] != "")
		item.Status = TaskStatus(displayFacts.Status)
		// AC-04：审批等待状态面向用户显示「待{审批人姓名}审批」。
		item.StatusLabel = domain.StatusLabel(displayFacts.Status, t.KrOwnerName.String, reviewerNamesByTask[t.ID])
		// 当前环节与待行动人（AC-31 基础信息；名字按身份就近解析）。
		// 环节文案同样按 AC-04 收口：审批等待环节显示当前审批人姓名。
		stage, actorID := domain.CurrentStage(displayFacts)
		item.CurrentStage = domain.StageLabel(stage, t.KrOwnerName.String, reviewerNamesByTask[t.ID])
		if actorID != nil {
			item.PendingActorId = actorID
			switch {
			case *actorID == t.OwnerID:
				item.PendingActorName = &t.OwnerName
			case *actorID == t.CreatedBy:
				item.PendingActorName = &t.CreatorName
			case t.KrOwnerID.Valid && *actorID == t.KrOwnerID.Int64:
				item.PendingActorName = fromPgText(t.KrOwnerName)
			}
		}
		// 编辑／关键字段修改动作标志与需要关注的变更单（AC-23）。
		if hasChange {
			// 任务终止后退回待处理事项随之结束（词汇表）。
			terminal := t.Status == domain.TaskCompleted || t.Status == domain.TaskCancelled
			if hasPending || !terminal {
				view := s.fieldChangeView(ctx, store.FieldChangeRequest{
					ID: fc.ID, TaskID: fc.TaskID, SubmittedBy: fc.SubmittedBy, Reason: fc.Reason,
					State: fc.State, Exempt: fc.Exempt, Opinion: fc.Opinion, Resolved: fc.Resolved,
					ChangeType: fc.ChangeType, OldStatus: fc.OldStatus, NewStatus: fc.NewStatus, Payload: fc.Payload,
					OldName: fc.OldName, NewName: fc.NewName,
					OldDescription: fc.OldDescription, NewDescription: fc.NewDescription,
					OldCompletionCriteria: fc.OldCompletionCriteria, NewCompletionCriteria: fc.NewCompletionCriteria,
					OldOwnerID: fc.OldOwnerID, NewOwnerID: fc.NewOwnerID,
					OldEndDate: fc.OldEndDate, NewEndDate: fc.NewEndDate,
					SubmittedAt: fc.SubmittedAt, DecidedBy: fc.DecidedBy, DecidedAt: fc.DecidedAt,
				}, fc.SubmittedByName, fc.DecidedByName, facts, actor, userID)
				item.FieldChange = &view
			}
		}
		routeOutcome, routeErr := domain.FieldChangeRoute(actor, userID, facts, hasPending)
		item.CanProposeFieldChange = routeErr == nil
		// #138 就地编辑的保存路由（裁决 E1）：前端只消费，不复算规则。
		if routeErr == nil {
			mode := TaskFieldEditMode(domain.FieldEditMode(routeOutcome))
			item.FieldEditMode = &mode
		}
		canDiscuss := domain.CanDiscuss(actor)
		item.CanDiscuss = &canDiscuss
		canManageDeliverables := domain.CanManageDeliverables(actor, userID, facts)
		canUploadCandidate := domain.CanUploadCandidate(actor, userID, facts)
		item.CanManageDeliverables = &canManageDeliverables
		item.CanUploadCandidate = &canUploadCandidate
		canSubmitCompletion := domain.CanSubmitCompletion(actor, userID, facts, candidatesByTask[t.ID])
		item.CanSubmitCompletion = &canSubmitCompletion
		canManageReviewers := domain.CanManageReviewers(actor, userID, facts)
		item.CanManageReviewers = &canManageReviewers
		// 接收方与确认接收（MW-09）：名单只在「指定成员」时有意义，其余取值返回空数组。
		item.ReceiverScope = ReceiverScope(t.ReceiverScope)
		receivers := receiversByTask[t.ID]
		if receivers == nil || t.ReceiverScope != domain.ReceiverScopeMembers {
			receivers = []ReviewerInfo{}
		}
		item.Receivers = &receivers
		canManageReceivers := domain.CanConfigureReceivers(actor, userID, facts)
		item.CanManageReceivers = &canManageReceivers
		canConfirmReceipt := pendingReceiptByTask[t.ID]
		item.CanConfirmReceipt = &canConfirmReceipt
		// 参与人：不进任何判定，只随任务事实一起返回；未配置时给空数组，前端不必区分 null。
		participants := participantsByTask[t.ID]
		if participants == nil {
			participants = []ReviewerInfo{}
		}
		item.Participants = &participants
		canManageParticipants := domain.CanManageParticipants(actor, userID, facts)
		item.CanManageParticipants = &canManageParticipants
		// 成果更新（AC-66）：进程与发起入口都由后端派生，前端不复算生命周期规则。
		ru := resultUpdateState(t.ResultUpdate)
		item.ResultUpdate = &ru
		canStartResultUpdate := domain.CanStartResultUpdate(actor, userID, facts, hasPending)
		item.CanStartResultUpdate = &canStartResultUpdate
		if n := openBlockersByTask[t.ID]; n > 0 {
			item.OpenBlockerCount = &n
		}
		if names := namesByTask[t.ID]; len(names) > 0 {
			item.DeliverableNames = &names
		}
		resp = append(resp, item)
	}
	return resp, nil
}

// writeTask 输出单个任务的最新事实（提交、审批处理后的响应）。
func (s *Server) writeTask(w http.ResponseWriter, r *http.Request, projectID, taskID, userID int64, actor domain.Actor) {
	list, err := s.taskList(r.Context(), projectID, userID, actor)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	for _, t := range list {
		if t.Id == taskID {
			writeJSON(w, http.StatusOK, t)
			return
		}
	}
	writeInternalError(w, r, err)
}

