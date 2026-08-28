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

// 任务与入池审批（AC-04、AC-26）。业务规则在 domain，handler 仅编排。

func (s *Server) ListTasks(w http.ResponseWriter, r *http.Request, projectId int64) {
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	resp, err := s.taskList(r.Context(), projectId, uid, projectActor(uid, proj.OwnerID, proj.MyRole))
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
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
	actor := projectActor(uid, proj.OwnerID, proj.MyRole)
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
	// 通过任务创建邀请响应时（AC-03）：邀请必须待处理、发给本人，且本批含指定 KR 的任务并提交入池。
	var invite store.GetTaskInviteInProjectRow
	if req.TaskInviteId != nil {
		if !req.SubmitForReview {
			writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_invite_response", Message: "通过邀请创建的任务需要一并提交入池"})
			return
		}
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
	// 逐项校验：所属 KR 属于本项目、最小骨架合法；提交入池时 KR 必须有负责人（免审除外）。
	krOwners := make([]*int64, len(req.Items))
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
		krOwners[i] = fromPgInt8(kr.OwnerID)
		nt := domain.NewTask{Name: strings.TrimSpace(item.Name), OwnerID: item.OwnerId, Start: item.StartDate.Time, End: item.EndDate.Time}
		if err := domain.ValidateNewTask(nt, func(id int64) string { return roleByID[id] }); err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_task", Message: err.Error()})
			return
		}
		if item.ExpectedDeliverable != nil && strings.TrimSpace(*item.ExpectedDeliverable) != "" {
			if err := domain.ValidateDeliverableName(strings.TrimSpace(*item.ExpectedDeliverable)); err != nil {
				writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_deliverable", Message: err.Error()})
				return
			}
		}
		if _, exempt := domain.TaskCreationOutcome(uid, krOwners[i]); !exempt && req.SubmitForReview {
			if err := domain.SubmitPoolReview(domain.TaskFacts{Status: domain.TaskDraft, KrOwnerID: krOwners[i]}, false); err != nil {
				writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "kr_owner_missing", Message: err.Error()})
				return
			}
		}
	}
	// 整批一个事务：全部成功或全部失败。
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	created := make([]createdTask, 0, len(req.Items))
	for i, item := range req.Items {
		status, exempt := domain.TaskCreationOutcome(uid, krOwners[i])
		if !exempt && req.SubmitForReview {
			status = domain.TaskPendingPoolReview
		}
		task, err := qtx.CreateTask(r.Context(), store.CreateTaskParams{
			KeyResultID: item.KeyResultId,
			Name:        strings.TrimSpace(item.Name),
			OwnerID:     item.OwnerId,
			StartDate:   pgtype.Date{Time: item.StartDate.Time, Valid: true},
			EndDate:     pgtype.Date{Time: item.EndDate.Time, Valid: true},
			Status:      status,
			CreatedBy:   uid,
		})
		if err != nil {
			writeInternalError(w, r, err)
			return
		}
		created = append(created, createdTask{id: task.ID, exempt: exempt, submitted: req.SubmitForReview})
		switch {
		case exempt:
			// AC-26：免审生成一条已通过并标记免审的审批单，记录免审原因。
			_, err = qtx.CreatePoolReview(r.Context(), store.CreatePoolReviewParams{
				TaskID:      task.ID,
				SubmittedBy: uid,
				Status:      domain.PoolReviewApproved,
				Exempt:      true,
				Opinion:     domain.PoolExemptOpinion,
				DecidedBy:   pgtype.Int8{Int64: uid, Valid: true},
				DecidedAt:   pgtype.Timestamptz{Time: s.now(), Valid: true},
			})
		case req.SubmitForReview:
			_, err = qtx.CreatePoolReview(r.Context(), store.CreatePoolReviewParams{
				TaskID:      task.ID,
				SubmittedBy: uid,
				Status:      domain.PoolReviewPending,
			})
		}
		if err != nil {
			writeInternalError(w, r, err)
			return
		}
		// 预期交付物（原型创建弹窗列）：随任务建立对应交付物项。
		if item.ExpectedDeliverable != nil {
			if dn := strings.TrimSpace(*item.ExpectedDeliverable); dn != "" {
				if _, err := qtx.CreateDeliverable(r.Context(), store.CreateDeliverableParams{TaskID: task.ID, Name: dn, CreatedBy: uid}); err != nil {
					writeInternalError(w, r, err)
					return
				}
			}
		}
	}
	if req.TaskInviteId != nil {
		// 邀请随本批提交一并完成（词汇表：受邀人通过该邀请提交关联任务的入池申请后结束）。
		if _, err := qtx.UpdateTaskInviteState(r.Context(), store.UpdateTaskInviteStateParams{ID: invite.ID, State: domain.TaskInviteCompleted}); err != nil {
			writeInternalError(w, r, err)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w, r, err)
		return
	}
	// 新建任务的入池留痕：免审记「入池审批通过」并带免审原因，提交待审记「提交入池审批」。
	// 存草稿不产生审批事实，也就没有可留痕的动态。
	for _, c := range created {
		switch {
		case c.exempt:
			s.actionActivity(r.Context(), c.id, domain.ActivityPoolApproved, uid, domain.PoolExemptOpinion)
		case c.submitted:
			s.actionActivity(r.Context(), c.id, domain.ActivityPoolSubmitted, uid, "")
		}
	}
	resp, err := s.taskList(r.Context(), projectId, uid, actor)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

// createdTask 本批新建任务的入池留痕所需事实。
type createdTask struct {
	id        int64
	exempt    bool
	submitted bool
}

func (s *Server) SubmitTaskPoolReview(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64) {
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(uid, proj.OwnerID, proj.MyRole)
	task, facts, ok := s.fetchTask(w, r, projectId, taskId)
	if !ok {
		return
	}
	if uid != task.CreatedBy && uid != task.OwnerID && !domain.CanEditProject(actor) {
		writeForbidden(w)
		return
	}
	blockersBefore := s.blockerSnapshot(r.Context(), projectId)
	hasPending, err := s.q.HasPendingFieldChange(r.Context(), taskId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	if err := domain.SubmitPoolReview(facts, hasPending); err != nil {
		switch {
		case errors.Is(err, domain.ErrTaskNotDraft), errors.Is(err, domain.ErrCancelBlocked):
			writeJSON(w, http.StatusConflict, Error{Code: "task_state_conflict", Message: err.Error()})
		default:
			writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "kr_owner_missing", Message: err.Error()})
		}
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	if _, err := qtx.UpdateTaskStatus(r.Context(), store.UpdateTaskStatusParams{ID: taskId, Status: domain.TaskPendingPoolReview}); err != nil {
		writeInternalError(w, r, err)
		return
	}
	if _, err := qtx.CreatePoolReview(r.Context(), store.CreatePoolReviewParams{
		TaskID:      taskId,
		SubmittedBy: uid,
		Status:      domain.PoolReviewPending,
	}); err != nil {
		writeInternalError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w, r, err)
		return
	}
	s.actionActivity(r.Context(), taskId, domain.ActivityPoolSubmitted, uid, "")
	s.recordBlockerChanges(r.Context(), projectId, blockersBefore)
	s.writeTask(w, r, projectId, taskId, uid, actor)
}

func (s *Server) DecideTaskPoolReview(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64) {
	var req PoolReviewDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !req.Decision.Valid() {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(uid, proj.OwnerID, proj.MyRole)
	approve := req.Decision == PoolReviewDecisionRequestDecisionApproved
	blockersBefore := s.blockerSnapshot(r.Context(), projectId)
	opinion := ""
	if req.Opinion != nil {
		opinion = strings.TrimSpace(*req.Opinion)
	}
	// 规则与写入同事务：先锁任务行再重读事实，避免并发决策各自基于过期状态写库（R2）。
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	task, facts, ok := lockTaskFacts(r, w, qtx, projectId, taskId)
	if !ok {
		return
	}
	newStatus, err := domain.DecidePoolReview(actor, facts, uid, approve, opinion)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrPoolReviewNotPending):
			writeJSON(w, http.StatusConflict, Error{Code: "task_state_conflict", Message: err.Error()})
		case errors.Is(err, domain.ErrRejectOpinionRequired):
			writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "opinion_required", Message: err.Error()})
		default:
			writeJSON(w, http.StatusForbidden, Error{Code: "forbidden", Message: err.Error()})
		}
		return
	}
	review, err := qtx.GetLatestPoolReview(r.Context(), taskId)
	if err != nil || review.Status != domain.PoolReviewPending {
		writeInternalError(w, r, err)
		return
	}
	reviewStatus := domain.PoolReviewApproved
	if !approve {
		reviewStatus = domain.PoolReviewRejected
	}
	if _, err := qtx.DecidePoolReview(r.Context(), store.DecidePoolReviewParams{
		ID:        review.ID,
		Status:    reviewStatus,
		Opinion:   opinion,
		DecidedBy: pgtype.Int8{Int64: uid, Valid: true},
	}); err != nil {
		writeInternalError(w, r, err)
		return
	}
	if _, err := qtx.UpdateTaskStatus(r.Context(), store.UpdateTaskStatusParams{ID: taskId, Status: newStatus}); err != nil {
		writeInternalError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w, r, err)
		return
	}
	// 退回类动态带上一句话理由（MW-18）。
	if approve {
		s.actionActivity(r.Context(), taskId, domain.ActivityPoolApproved, uid, opinion)
		// AC-29：首次入池通过后补发指定成员的输入请求通知。
		s.notifyPendingInputRequests(r, projectId, task.ID, task.Name)
	} else {
		s.actionActivity(r.Context(), taskId, domain.ActivityPoolRejected, uid, opinion)
	}
	s.recordBlockerChanges(r.Context(), projectId, blockersBefore)
	s.writeTask(w, r, projectId, taskId, uid, actor)
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
	actor := projectActor(uid, proj.OwnerID, proj.MyRole)
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
	actor := projectActor(uid, proj.OwnerID, proj.MyRole)
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
	actor := projectActor(uid, proj.OwnerID, proj.MyRole)
	task, _, ok := s.fetchTask(w, r, projectId, taskId)
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
	reviews, err := s.q.ListPoolReviewsByTask(r.Context(), taskId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	changeRows, err := s.q.ListFieldChangesByTask(r.Context(), taskId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	deliverables, err := s.deliverableList(r.Context(), taskId)
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
	factsForReviews := domain.TaskFacts{Status: task.Status, CreatorID: task.CreatedBy, OwnerID: task.OwnerID, KrOwnerID: fromPgInt8(task.KrOwnerID)}
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
	taskBlockers := []Blocker{}
	for _, b := range allBlockers {
		if b.TaskID == taskId {
			taskBlockers = append(taskBlockers, blockerView(b, actor, uid))
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
	// 审批显示文案需要所属 KR 负责人姓名（AC-04）。
	krOwnerName := ""
	if kr.OwnerID.Valid {
		if ku, err := s.q.GetUserByID(r.Context(), kr.OwnerID.Int64); err == nil {
			krOwnerName = ku.DisplayName
		}
	}
	prs := make([]PoolReview, 0, len(reviews))
	for _, pr := range reviews {
		prs = append(prs, *toPoolReview(store.LatestPoolReviewsByProjectRow{
			ID: pr.ID, TaskID: pr.TaskID, SubmittedBy: pr.SubmittedBy, Status: pr.Status,
			Exempt: pr.Exempt, Opinion: pr.Opinion, SubmittedAt: pr.SubmittedAt,
			DecidedBy: pr.DecidedBy, DecidedAt: pr.DecidedAt,
			SubmittedByName: pr.SubmittedByName, DecidedByName: pr.DecidedByName,
		}, krOwnerName))
	}
	facts := domain.TaskFacts{Status: string(item.Status), CreatorID: task.CreatedBy, OwnerID: task.OwnerID, KrOwnerID: fromPgInt8(task.KrOwnerID)}
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
	// F1：未决审批计数由后端派生，前端不再把三类审批单各自过滤后相加。
	pendingCount := 0
	for _, pr := range prs {
		if pr.Status == PoolReviewStatusPending {
			pendingCount++
		}
	}
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
		PoolReviews:       prs,
		FieldChanges:      fcs,
		Deliverables:      deliverables,
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
		Status:    task.Status,
		CreatorID: task.CreatedBy,
		OwnerID:   task.OwnerID,
		KrOwnerID: fromPgInt8(task.KrOwnerID),
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
		Status:    task.Status,
		CreatorID: task.CreatedBy,
		OwnerID:   task.OwnerID,
		KrOwnerID: fromPgInt8(task.KrOwnerID),
	}
	return task, facts, true
}

// taskList 组装项目全部任务及派生字段（负责人姓名、动作标志、最近一次入池审批单）。
func (s *Server) taskList(ctx context.Context, projectID, userID int64, actor domain.Actor) ([]Task, error) {
	rows, err := s.q.ListProjectTasks(ctx, projectID)
	if err != nil {
		return nil, err
	}
	reviews, err := s.q.LatestPoolReviewsByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	reviewByTask := make(map[int64]store.LatestPoolReviewsByProjectRow, len(reviews))
	for _, pr := range reviews {
		reviewByTask[pr.TaskID] = pr
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
		facts := domain.TaskFacts{Status: t.Status, CreatorID: t.CreatedBy, OwnerID: t.OwnerID, KrOwnerID: fromPgInt8(t.KrOwnerID)}
		// 待审批变更单（含取消单）决定编辑、取消与提交入池三处入口是否可用（AC-23、AC-57 互斥）。
		fc, hasChange := changeByTask[t.ID]
		hasPending := hasChange && fc.State == domain.FieldChangePendingState
		item := Task{
			Id:                  t.ID,
			KeyResultId:         t.KeyResultID,
			Code:                domain.TaskCode(int(t.ObjectiveCodeSeq), int(t.KrCodeSeq), int(t.CodeSeq)),
			Name:                t.Name,
			OwnerId:             t.OwnerID,
			OwnerName:           t.OwnerName,
			StartDate:           *fromPgDate(t.StartDate),
			EndDate:             *fromPgDate(t.EndDate),
			UpdatedAt:           t.UpdatedAt.Time,
			Status:              TaskStatus(t.Status),
			Description:         optString(t.Description),
			CompletionCriteria:  optString(t.CompletionCriteria),
			Progress:            domain.DisplayProgress(t.Status, fromPgInt4(t.Progress)),
			CancelReason:        optString(t.CancelReason),
			CanStart:            domain.CanStartTask(actor, userID, facts),
			CanUpdateProgress:   domain.CanUpdateProgress(actor, userID, facts),
			CanCancel:           domain.CanCancelTask(actor, userID, facts, hasPending),
			CanSubmitPoolReview: domain.CanSubmitPoolReview(actor, userID, facts, hasPending),
			CanDecidePoolReview: domain.CanDecidePoolReview(actor, userID, facts),
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
		if pr, ok := reviewByTask[t.ID]; ok {
			item.PoolReview = toPoolReview(pr, t.KrOwnerName.String)
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
		_, routeErr := domain.FieldChangeRoute(actor, userID, facts, hasPending)
		item.CanProposeFieldChange = routeErr == nil
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

func toPoolReview(pr store.LatestPoolReviewsByProjectRow, krOwnerName string) *PoolReview {
	out := &PoolReview{
		Status:          PoolReviewStatus(pr.Status),
		StatusLabel:     domain.PoolReviewStateLabel(pr.Status, pr.Exempt, krOwnerName),
		Exempt:          pr.Exempt,
		Opinion:         optString(pr.Opinion),
		SubmittedByName: optString(pr.SubmittedByName),
		DecidedByName:   fromPgText(pr.DecidedByName),
	}
	if pr.SubmittedAt.Valid {
		out.SubmittedAt = &pr.SubmittedAt.Time
	}
	if pr.DecidedAt.Valid {
		out.DecidedAt = &pr.DecidedAt.Time
	}
	return out
}
