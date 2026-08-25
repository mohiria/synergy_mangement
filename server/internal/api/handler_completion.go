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

// 完成申请与 KR 终审（AC-13、AC-15、AC-38～40）。业务规则在 domain，handler 仅编排。

// SubmitCompletion 提交完成申请：纳入任务全部候选内容，无中间审核直接进入待 KR 终审（AC-13）。
func (s *Server) SubmitCompletion(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64) {
	var req SubmitCompletionRequest
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
	if uid != task.OwnerID && !domain.CanEditProject(actor) {
		writeForbidden(w)
		return
	}
	candidates, err := s.q.ListCandidateFilesByTask(r.Context(), taskId)
	if err != nil {
		writeInternalError(w)
		return
	}
	note := strings.TrimSpace(req.Note)
	if err := domain.SubmitCompletionRule(facts, len(candidates), note); err != nil {
		switch {
		case errors.Is(err, domain.ErrCompletionNotInProgress):
			writeJSON(w, http.StatusConflict, Error{Code: "task_state_conflict", Message: err.Error()})
		default:
			writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_completion", Message: err.Error()})
		}
		return
	}
	reviewers, err := s.q.ListTaskReviewers(r.Context(), taskId)
	if err != nil {
		writeInternalError(w)
		return
	}
	// AC-13/14：无中间审核人直接待 KR 终审；配置了则进入中间或签（配置快照进申请）。
	reviewState, taskStatus := domain.SubmitCompletionOutcome(len(reviewers))
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	review, err := qtx.CreateCompletionReview(r.Context(), store.CreateCompletionReviewParams{
		TaskID: taskId, SubmittedBy: uid, Note: note, State: reviewState,
	})
	if err != nil {
		writeInternalError(w)
		return
	}
	for _, rv := range reviewers {
		if err := qtx.CreateReviewReviewer(r.Context(), store.CreateReviewReviewerParams{ReviewID: review.ID, UserID: rv.UserID}); err != nil {
			writeInternalError(w)
			return
		}
	}
	for _, c := range candidates {
		if err := qtx.CreateCompletionReviewItem(r.Context(), store.CreateCompletionReviewItemParams{
			ReviewID:        review.ID,
			DeliverableID:   c.DeliverableID,
			DeliverableName: c.DeliverableName,
			FileName:        c.FileName,
			FileID:          pgtype.Int8{Int64: c.ID, Valid: true},
		}); err != nil {
			writeInternalError(w)
			return
		}
	}
	if _, err := qtx.UpdateTaskStatus(r.Context(), store.UpdateTaskStatusParams{ID: taskId, Status: taskStatus}); err != nil {
		writeInternalError(w)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w)
		return
	}
	s.writeTask(w, r, projectId, taskId, uid, actor)
}

// DecideCompletion KR 负责人终审：通过则候选整体覆盖对应当前内容并永久删除旧文件（AC-15/39），
// 退回则删除本次候选文件、任务回到进行中（AC-40）。
func (s *Server) DecideCompletion(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64, reviewId int64) {
	var req CompletionDecisionRequest
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
	review, err := s.q.GetCompletionReview(r.Context(), store.GetCompletionReviewParams{ID: reviewId, TaskID: taskId})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "review_not_found", Message: "完成申请不存在"})
		} else {
			writeInternalError(w)
		}
		return
	}
	opinion := ""
	if req.Opinion != nil {
		opinion = strings.TrimSpace(*req.Opinion)
	}
	approve := req.Decision == CompletionDecisionRequestDecisionApproved
	// 中间或签阶段（AC-14/24/37）：仅或签组成员可处理。
	if review.State == domain.CompletionIntermediate {
		s.decideIntermediate(w, r, projectId, taskId, review, facts, uid, actor, approve, opinion)
		return
	}
	if review.State != domain.CompletionPendingFinal {
		writeJSON(w, http.StatusConflict, Error{Code: "review_state_conflict", Message: domain.ErrCompletionNotPending.Error()})
		return
	}
	newStatus, err := domain.DecideCompletionRule(facts, uid, approve, opinion)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrCompletionNotPending):
			writeJSON(w, http.StatusConflict, Error{Code: "review_state_conflict", Message: err.Error()})
		case errors.Is(err, domain.ErrRejectOpinionRequired):
			writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "opinion_required", Message: err.Error()})
		default:
			writeJSON(w, http.StatusForbidden, Error{Code: "forbidden", Message: err.Error()})
		}
		return
	}
	items, err := s.q.ListCompletionReviewItems(r.Context(), reviewId)
	if err != nil {
		writeInternalError(w)
		return
	}
	reviewState := domain.CompletionRejected
	if approve {
		reviewState = domain.CompletionApproved
	}
	// 收集需要从 MinIO 删除的对象（事务提交后再删，避免误删）。
	var removeKeys []string
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	for _, item := range items {
		if !item.FileID.Valid {
			continue
		}
		if approve {
			// AC-39：候选分别覆盖对应当前内容；被覆盖旧文件永久删除、不可恢复。
			if old, err := qtx.GetCurrentFile(r.Context(), item.DeliverableID); err == nil {
				if _, err := qtx.DeleteDeliverableFile(r.Context(), old.ID); err != nil {
					writeInternalError(w)
					return
				}
				removeKeys = append(removeKeys, old.ObjectKey)
			}
			if _, err := qtx.PromoteCandidateToCurrent(r.Context(), item.FileID.Int64); err != nil {
				writeInternalError(w)
				return
			}
		} else {
			// AC-40：退回删除本次候选文件，原当前交付物保持不变。
			if f, err := qtx.GetCandidateFile(r.Context(), item.DeliverableID); err == nil && f.ID == item.FileID.Int64 {
				if _, err := qtx.DeleteDeliverableFile(r.Context(), f.ID); err != nil {
					writeInternalError(w)
					return
				}
				removeKeys = append(removeKeys, f.ObjectKey)
			}
		}
	}
	if _, err := qtx.DecideCompletionReview(r.Context(), store.DecideCompletionReviewParams{
		ID: reviewId, State: reviewState, Opinion: opinion, DecidedBy: pgtype.Int8{Int64: uid, Valid: true},
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
	for _, key := range removeKeys {
		_ = s.files.Remove(r.Context(), key)
	}
	s.writeTask(w, r, projectId, taskId, uid, actor)
}

// decideIntermediate 或签处理：任一人通过→待 KR 终审（留痕）；任一人退回→整体退回并删除候选。
func (s *Server) decideIntermediate(w http.ResponseWriter, r *http.Request, projectId, taskId int64,
	review store.CompletionReview, facts domain.TaskFacts, uid int64, actor domain.Actor, approve bool, opinion string,
) {
	reviewerRows, err := s.q.ListReviewReviewers(r.Context(), review.ID)
	if err != nil {
		writeInternalError(w)
		return
	}
	reviewerSet := make(map[int64]bool, len(reviewerRows))
	for _, rv := range reviewerRows {
		reviewerSet[rv.UserID] = true
	}
	newTaskStatus, newReviewState, err := domain.DecideIntermediateRule(facts, uid, func(id int64) bool { return reviewerSet[id] }, approve, opinion)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrCompletionNotIntermediate):
			writeJSON(w, http.StatusConflict, Error{Code: "review_state_conflict", Message: err.Error()})
		case errors.Is(err, domain.ErrRejectOpinionRequired):
			writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "opinion_required", Message: err.Error()})
		default:
			writeJSON(w, http.StatusForbidden, Error{Code: "forbidden", Message: err.Error()})
		}
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	var removeKeys []string
	if approve {
		if _, err := qtx.RecordIntermediateApproval(r.Context(), store.RecordIntermediateApprovalParams{
			ID: review.ID, State: newReviewState, IntermediateBy: pgtype.Int8{Int64: uid, Valid: true}, IntermediateOpinion: opinion,
		}); err != nil {
			writeInternalError(w)
			return
		}
	} else {
		// AC-24：整体退回——删除本次候选文件，原当前交付物保持不变。
		items, err := qtx.ListCompletionReviewItems(r.Context(), review.ID)
		if err != nil {
			writeInternalError(w)
			return
		}
		for _, item := range items {
			if !item.FileID.Valid {
				continue
			}
			if f, err := qtx.GetCandidateFile(r.Context(), item.DeliverableID); err == nil && f.ID == item.FileID.Int64 {
				if _, err := qtx.DeleteDeliverableFile(r.Context(), f.ID); err != nil {
					writeInternalError(w)
					return
				}
				removeKeys = append(removeKeys, f.ObjectKey)
			}
		}
		if _, err := qtx.DecideCompletionReview(r.Context(), store.DecideCompletionReviewParams{
			ID: review.ID, State: newReviewState, Opinion: opinion, DecidedBy: pgtype.Int8{Int64: uid, Valid: true},
		}); err != nil {
			writeInternalError(w)
			return
		}
	}
	if _, err := qtx.UpdateTaskStatus(r.Context(), store.UpdateTaskStatusParams{ID: taskId, Status: newTaskStatus}); err != nil {
		writeInternalError(w)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w)
		return
	}
	for _, key := range removeKeys {
		_ = s.files.Remove(r.Context(), key)
	}
	s.writeTask(w, r, projectId, taskId, uid, actor)
}

// SetTaskReviewers 调整任务级中间审核人配置（非关键字段，直接调整；§5.2.B）。
func (s *Server) SetTaskReviewers(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64) {
	var req SetReviewersRequest
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
	if !domain.CanManageReviewers(actor, uid, facts) {
		switch facts.Status {
		case domain.TaskPendingIntermediateReview, domain.TaskPendingFinalReview, domain.TaskCompleted, domain.TaskCancelled:
			writeJSON(w, http.StatusConflict, Error{Code: "task_state_conflict", Message: "审核期间或终态不能调整中间审核人"})
		default:
			writeForbidden(w)
		}
		return
	}
	members, err := s.q.ListProjectMembers(r.Context(), projectId)
	if err != nil {
		writeInternalError(w)
		return
	}
	roleByID := make(map[int64]string, len(members))
	for _, m := range members {
		roleByID[m.UserID] = m.Role
	}
	seen := map[int64]bool{}
	ids := []int64{}
	for _, id := range req.UserIds {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if err := domain.ValidateReviewers(ids, func(id int64) string { return roleByID[id] }); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_reviewers", Message: err.Error()})
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	if _, err := qtx.ClearTaskReviewers(r.Context(), taskId); err != nil {
		writeInternalError(w)
		return
	}
	for _, id := range ids {
		if err := qtx.SetTaskReviewer(r.Context(), store.SetTaskReviewerParams{TaskID: taskId, UserID: id}); err != nil {
			writeInternalError(w)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w)
		return
	}
	rows, err := s.q.ListTaskReviewers(r.Context(), taskId)
	if err != nil {
		writeInternalError(w)
		return
	}
	resp := make([]ReviewerInfo, 0, len(rows))
	for _, rv := range rows {
		resp = append(resp, ReviewerInfo{UserId: rv.UserID, DisplayName: rv.DisplayName})
	}
	writeJSON(w, http.StatusOK, resp)
}

// completionReviewList 组装完成申请记录（含项快照与派生动作标志）。
func (s *Server) completionReviewList(ctx context.Context, taskID int64, facts domain.TaskFacts, userID int64) ([]CompletionReview, error) {
	rows, err := s.q.ListCompletionReviewsByTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	out := make([]CompletionReview, 0, len(rows))
	for _, cr := range rows {
		items, err := s.q.ListCompletionReviewItems(ctx, cr.ID)
		if err != nil {
			return nil, err
		}
		views := make([]CompletionReviewItem, 0, len(items))
		for _, it := range items {
			v := CompletionReviewItem{DeliverableId: it.DeliverableID, DeliverableName: it.DeliverableName, FileName: it.FileName}
			// 仅当文件仍在库（候选待审或已成为当前内容）时提供下载入口。
			if it.FileID.Valid {
				v.FileId = &it.FileID.Int64
			}
			views = append(views, v)
		}
		reviewerRows, err := s.q.ListReviewReviewers(ctx, cr.ID)
		if err != nil {
			return nil, err
		}
		reviewerViews := make([]ReviewerInfo, 0, len(reviewerRows))
		reviewerSet := make(map[int64]bool, len(reviewerRows))
		for _, rv := range reviewerRows {
			reviewerViews = append(reviewerViews, ReviewerInfo{UserId: rv.UserID, DisplayName: rv.DisplayName})
			reviewerSet[rv.UserID] = true
		}
		var canDecide bool
		switch cr.State {
		case domain.CompletionIntermediate:
			_, _, err := domain.DecideIntermediateRule(facts, userID, func(id int64) bool { return reviewerSet[id] }, true, "")
			canDecide = err == nil
		case domain.CompletionPendingFinal:
			_, err := domain.DecideCompletionRule(facts, userID, true, "")
			canDecide = err == nil
		}
		item := CompletionReview{
			Id:              cr.ID,
			State:           CompletionReviewState(cr.State),
			Note:            cr.Note,
			Opinion:         optString(cr.Opinion),
			Items:           views,
			Reviewers:       &reviewerViews,
			SubmittedByName: optString(cr.SubmittedByName),
			DecidedByName:   fromPgText(cr.DecidedByName),
			CanDecide:       &canDecide,
		}
		if cr.SubmittedAt.Valid {
			item.SubmittedAt = &cr.SubmittedAt.Time
		}
		if cr.DecidedAt.Valid {
			item.DecidedAt = &cr.DecidedAt.Time
		}
		if cr.IntermediateBy.Valid {
			if u, err := s.q.GetUserByID(ctx, cr.IntermediateBy.Int64); err == nil {
				item.IntermediateByName = &u.DisplayName
			}
			item.IntermediateOpinion = optString(cr.IntermediateOpinion)
			if cr.IntermediateAt.Valid {
				item.IntermediateAt = &cr.IntermediateAt.Time
			}
		}
		out = append(out, item)
	}
	return out, nil
}
