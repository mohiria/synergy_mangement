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
		writeInternalError(w)
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
		writeInternalError(w)
		return
	}
	memberSet := make(map[int64]bool, len(members))
	for _, m := range members {
		memberSet[m.UserID] = true
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
			writeInternalError(w)
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
			writeInternalError(w)
			return
		}
		krOwners[i] = fromPgInt8(kr.OwnerID)
		nt := domain.NewTask{Name: strings.TrimSpace(item.Name), OwnerID: item.OwnerId, Start: item.StartDate.Time, End: item.EndDate.Time}
		if err := domain.ValidateNewTask(nt, func(id int64) bool { return memberSet[id] }); err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_task", Message: err.Error()})
			return
		}
		if _, exempt := domain.TaskCreationOutcome(uid, krOwners[i]); !exempt && req.SubmitForReview {
			if err := domain.SubmitPoolReview(domain.TaskFacts{Status: domain.TaskDraft, KrOwnerID: krOwners[i]}); err != nil {
				writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "kr_owner_missing", Message: err.Error()})
				return
			}
		}
	}
	// 整批一个事务：全部成功或全部失败。
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
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
			writeInternalError(w)
			return
		}
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
			writeInternalError(w)
			return
		}
	}
	if req.TaskInviteId != nil {
		// 邀请随本批提交一并完成（词汇表：受邀人通过该邀请提交关联任务的入池申请后结束）。
		if _, err := qtx.UpdateTaskInviteState(r.Context(), store.UpdateTaskInviteStateParams{ID: invite.ID, State: domain.TaskInviteCompleted}); err != nil {
			writeInternalError(w)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w)
		return
	}
	resp, err := s.taskList(r.Context(), projectId, uid, actor)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
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
	if err := domain.SubmitPoolReview(facts); err != nil {
		if errors.Is(err, domain.ErrTaskNotDraft) {
			writeJSON(w, http.StatusConflict, Error{Code: "task_state_conflict", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "kr_owner_missing", Message: err.Error()})
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	if _, err := qtx.UpdateTaskStatus(r.Context(), store.UpdateTaskStatusParams{ID: taskId, Status: domain.TaskPendingPoolReview}); err != nil {
		writeInternalError(w)
		return
	}
	if _, err := qtx.CreatePoolReview(r.Context(), store.CreatePoolReviewParams{
		TaskID:      taskId,
		SubmittedBy: uid,
		Status:      domain.PoolReviewPending,
	}); err != nil {
		writeInternalError(w)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w)
		return
	}
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
	_, facts, ok := s.fetchTask(w, r, projectId, taskId)
	if !ok {
		return
	}
	approve := req.Decision == PoolReviewDecisionRequestDecisionApproved
	newStatus, err := domain.DecidePoolReview(facts, uid, approve)
	if err != nil {
		if errors.Is(err, domain.ErrPoolReviewNotPending) {
			writeJSON(w, http.StatusConflict, Error{Code: "task_state_conflict", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusForbidden, Error{Code: "forbidden", Message: err.Error()})
		return
	}
	review, err := s.q.GetLatestPoolReview(r.Context(), taskId)
	if err != nil || review.Status != domain.PoolReviewPending {
		writeInternalError(w)
		return
	}
	opinion := ""
	if req.Opinion != nil {
		opinion = strings.TrimSpace(*req.Opinion)
	}
	reviewStatus := domain.PoolReviewApproved
	if !approve {
		reviewStatus = domain.PoolReviewRejected
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	if _, err := qtx.DecidePoolReview(r.Context(), store.DecidePoolReviewParams{
		ID:        review.ID,
		Status:    reviewStatus,
		Opinion:   opinion,
		DecidedBy: pgtype.Int8{Int64: uid, Valid: true},
	}); err != nil {
		writeInternalError(w)
		return
	}
	if _, err := qtx.UpdateTaskStatus(r.Context(), store.UpdateTaskStatusParams{ID: taskId, Status: newStatus}); err != nil {
		writeInternalError(w)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w)
		return
	}
	s.writeTask(w, r, projectId, taskId, uid, actor)
}

// fetchTask 读取任务与流转判定所需事实；不存在时已写出 404 并返回 false。
func (s *Server) fetchTask(w http.ResponseWriter, r *http.Request, projectID, taskID int64) (store.GetTaskInProjectRow, domain.TaskFacts, bool) {
	task, err := s.q.GetTaskInProject(r.Context(), store.GetTaskInProjectParams{ID: taskID, ProjectID: projectID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "task_not_found", Message: "任务不存在"})
		} else {
			writeInternalError(w)
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
	resp := make([]Task, 0, len(rows))
	for _, t := range rows {
		facts := domain.TaskFacts{Status: t.Status, CreatorID: t.CreatedBy, OwnerID: t.OwnerID, KrOwnerID: fromPgInt8(t.KrOwnerID)}
		item := Task{
			Id:                  t.ID,
			KeyResultId:         t.KeyResultID,
			Name:                t.Name,
			OwnerId:             t.OwnerID,
			OwnerName:           t.OwnerName,
			StartDate:           *fromPgDate(t.StartDate),
			EndDate:             *fromPgDate(t.EndDate),
			Status:              TaskStatus(t.Status),
			CanSubmitPoolReview: domain.CanSubmitPoolReview(actor, userID, facts),
			CanDecidePoolReview: domain.CanDecidePoolReview(userID, facts),
		}
		if pr, ok := reviewByTask[t.ID]; ok {
			item.PoolReview = toPoolReview(pr)
		}
		resp = append(resp, item)
	}
	return resp, nil
}

// writeTask 输出单个任务的最新事实（提交、审批处理后的响应）。
func (s *Server) writeTask(w http.ResponseWriter, r *http.Request, projectID, taskID, userID int64, actor domain.Actor) {
	list, err := s.taskList(r.Context(), projectID, userID, actor)
	if err != nil {
		writeInternalError(w)
		return
	}
	for _, t := range list {
		if t.Id == taskID {
			writeJSON(w, http.StatusOK, t)
			return
		}
	}
	writeInternalError(w)
}

func toPoolReview(pr store.LatestPoolReviewsByProjectRow) *PoolReview {
	out := &PoolReview{
		Status:          PoolReviewStatus(pr.Status),
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
