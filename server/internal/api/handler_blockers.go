package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// 结构化卡点与一键提醒（AC-11）。业务规则在 domain，handler 仅编排。

func (s *Server) CreateBlocker(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64) {
	var req CreateBlockerRequest
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
	if !domain.CanReportBlocker(actor, uid, facts) {
		switch facts.Status {
		case domain.TaskDraft, domain.TaskPendingPoolReview, domain.TaskCompleted, domain.TaskCancelled:
			writeJSON(w, http.StatusConflict, Error{Code: "task_state_conflict", Message: "任务当前状态不能上报卡点"})
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
	memberSet := make(map[int64]bool, len(members))
	for _, m := range members {
		memberSet[m.UserID] = true
	}
	nb := domain.NewBlocker{
		Kind:          string(req.Kind),
		Missing:       strings.TrimSpace(req.Missing),
		Reason:        strings.TrimSpace(req.Reason),
		ActionOwnerID: req.ActionOwnerId,
		Level:         string(req.Level),
	}
	if err := domain.ValidateBlocker(nb, func(id int64) bool { return memberSet[id] }); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_blocker", Message: err.Error()})
		return
	}
	created, err := s.q.CreateBlocker(r.Context(), store.CreateBlockerParams{
		TaskID:               taskId,
		Kind:                 nb.Kind,
		Missing:              nb.Missing,
		Reason:               nb.Reason,
		ActionOwnerID:        nb.ActionOwnerID,
		Level:                nb.Level,
		ExpectedRecoveryDate: toPgDate(req.ExpectedRecoveryDate),
		CreatedBy:            uid,
	})
	if err != nil {
		writeInternalError(w)
		return
	}
	views, err := s.blockerViews(r.Context(), projectId, uid, actor)
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

func (s *Server) ListBlockers(w http.ResponseWriter, r *http.Request, projectId int64) {
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	views, err := s.blockerViews(r.Context(), projectId, uid, projectActor(uid, proj.OwnerID, proj.MyRole))
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, views)
}

func (s *Server) RemindBlocker(w http.ResponseWriter, r *http.Request, projectId int64, blockerId int64) {
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(uid, proj.OwnerID, proj.MyRole)
	b, err := s.q.GetBlockerInProject(r.Context(), store.GetBlockerInProjectParams{ID: blockerId, ProjectID: projectId})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "blocker_not_found", Message: "卡点不存在"})
		} else {
			writeInternalError(w)
		}
		return
	}
	facts := domain.BlockerFacts{State: b.State, CreatedBy: b.CreatedBy, ActionOwnerID: b.ActionOwnerID, TaskOwnerID: b.TaskOwnerID}
	if !domain.CanRemindBlocker(actor, uid, facts) {
		if b.State != domain.BlockerOpen {
			writeJSON(w, http.StatusConflict, Error{Code: "blocker_state_conflict", Message: domain.ErrBlockerNotOpen.Error()})
			return
		}
		writeForbidden(w)
		return
	}
	// AC-11：一键提醒——带卡点上下文的定向站内通知。
	if _, err := s.q.CreateNotification(r.Context(), store.CreateNotificationParams{
		UserID:    b.ActionOwnerID,
		Kind:      domain.NotifyBlockerRemind,
		Content:   fmt.Sprintf("任务「%s」的卡点提醒：缺失「%s」（%s），请尽快处理", b.TaskName, b.Missing, b.Reason),
		ProjectID: pgtype.Int8{Int64: projectId, Valid: true},
		TaskID:    pgtype.Int8{Int64: b.TaskID, Valid: true},
	}); err != nil {
		writeInternalError(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) ResolveBlocker(w http.ResponseWriter, r *http.Request, projectId int64, blockerId int64) {
	var req ResolveBlockerRequest
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
	b, err := s.q.GetBlockerInProject(r.Context(), store.GetBlockerInProjectParams{ID: blockerId, ProjectID: projectId})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "blocker_not_found", Message: "卡点不存在"})
		} else {
			writeInternalError(w)
		}
		return
	}
	facts := domain.BlockerFacts{State: b.State, CreatedBy: b.CreatedBy, ActionOwnerID: b.ActionOwnerID, TaskOwnerID: b.TaskOwnerID}
	if !domain.CanResolveBlocker(actor, uid, facts) {
		if b.State != domain.BlockerOpen {
			writeJSON(w, http.StatusConflict, Error{Code: "blocker_state_conflict", Message: domain.ErrBlockerNotOpen.Error()})
			return
		}
		writeForbidden(w)
		return
	}
	note := ""
	if req.Note != nil {
		note = strings.TrimSpace(*req.Note)
	}
	if _, err := s.q.ResolveBlocker(r.Context(), store.ResolveBlockerParams{ID: blockerId, ResolvedNote: note}); err != nil {
		writeInternalError(w)
		return
	}
	views, err := s.blockerViews(r.Context(), projectId, uid, actor)
	if err != nil {
		writeInternalError(w)
		return
	}
	for _, v := range views {
		if v.Id == blockerId {
			writeJSON(w, http.StatusOK, v)
			return
		}
	}
	writeInternalError(w)
}

// blockerViews 组装项目全部卡点（派生动作标志）。
func (s *Server) blockerViews(ctx context.Context, projectID, userID int64, actor domain.Actor) ([]Blocker, error) {
	rows, err := s.q.ListBlockersByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]Blocker, 0, len(rows))
	for _, b := range rows {
		facts := domain.BlockerFacts{State: b.State, CreatedBy: b.CreatedBy, ActionOwnerID: b.ActionOwnerID, TaskOwnerID: b.TaskOwnerID}
		canRemind := domain.CanRemindBlocker(actor, userID, facts)
		canResolve := domain.CanResolveBlocker(actor, userID, facts)
		item := Blocker{
			Id:              b.ID,
			TaskId:          b.TaskID,
			Kind:            BlockerKind(b.Kind),
			Missing:         b.Missing,
			Reason:          b.Reason,
			ActionOwnerId:   &b.ActionOwnerID,
			ActionOwnerName: optString(b.ActionOwnerName),
			Level:           RiskLevel(b.Level),
			State:           BlockerState(b.State),
			CreatedByName:   optString(b.CreatedByName),
			CreatedAt:       b.CreatedAt.Time,
			ResolvedNote:    optString(b.ResolvedNote),
			CanRemind:       &canRemind,
			CanResolve:      &canResolve,
		}
		item.ExpectedRecoveryDate = fromPgDate(b.ExpectedRecoveryDate)
		if b.ResolvedAt.Valid {
			item.ResolvedAt = &b.ResolvedAt.Time
		}
		out = append(out, item)
	}
	return out, nil
}
