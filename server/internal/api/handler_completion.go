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
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	// 无中间审核人（#11 引入或签），直接待 KR 终审。
	review, err := qtx.CreateCompletionReview(r.Context(), store.CreateCompletionReviewParams{
		TaskID: taskId, SubmittedBy: uid, Note: note, State: domain.CompletionPendingFinal,
	})
	if err != nil {
		writeInternalError(w)
		return
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
	if _, err := qtx.UpdateTaskStatus(r.Context(), store.UpdateTaskStatusParams{ID: taskId, Status: domain.TaskPendingFinalReview}); err != nil {
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
	if review.State != domain.CompletionPendingFinal {
		writeJSON(w, http.StatusConflict, Error{Code: "review_state_conflict", Message: domain.ErrCompletionNotPending.Error()})
		return
	}
	opinion := ""
	if req.Opinion != nil {
		opinion = strings.TrimSpace(*req.Opinion)
	}
	approve := req.Decision == CompletionDecisionRequestDecisionApproved
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
		_, decideErr := domain.DecideCompletionRule(facts, userID, true, "")
		canDecide := cr.State == domain.CompletionPendingFinal && decideErr == nil
		item := CompletionReview{
			Id:              cr.ID,
			State:           CompletionReviewState(cr.State),
			Note:            cr.Note,
			Opinion:         optString(cr.Opinion),
			Items:           views,
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
		out = append(out, item)
	}
	return out, nil
}
