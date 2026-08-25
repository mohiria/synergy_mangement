package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// 任务讨论与站内通知（AC-35、AC-36）。业务规则在 domain，handler 仅编排。

func (s *Server) CreateDiscussion(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64) {
	var req CreateDiscussionRequest
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
	if !domain.CanDiscuss(actor) {
		writeForbidden(w)
		return
	}
	task, _, ok := s.fetchTask(w, r, projectId, taskId)
	if !ok {
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
	content := strings.TrimSpace(req.Content)
	mentions := []int64{}
	if req.MentionUserIds != nil {
		seen := map[int64]bool{}
		for _, id := range *req.MentionUserIds {
			if !seen[id] {
				seen[id] = true
				mentions = append(mentions, id)
			}
		}
	}
	if err := domain.ValidateDiscussion(content, mentions, func(id int64) bool { return memberSet[id] }); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_discussion", Message: err.Error()})
		return
	}
	author := currentUser(r)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	d, err := qtx.CreateDiscussion(r.Context(), store.CreateDiscussionParams{TaskID: taskId, AuthorID: uid, Content: content})
	if err != nil {
		writeInternalError(w)
		return
	}
	mentionSet := make(map[int64]bool, len(mentions))
	for _, id := range mentions {
		mentionSet[id] = true
		if err := qtx.CreateDiscussionMention(r.Context(), store.CreateDiscussionMentionParams{DiscussionID: d.ID, UserID: id}); err != nil {
			writeInternalError(w)
			return
		}
	}
	// AC-36：只通知任务负责人和被 @ 成员，通知可直达讨论 Tab（携带 taskId）。
	for _, target := range domain.DiscussionNotifyTargets(uid, task.OwnerID, mentions) {
		kind := domain.NotifyDiscussionOwner
		text := fmt.Sprintf("%s 在任务「%s」的讨论中发表了意见", author.DisplayName, task.Name)
		if mentionSet[target] {
			kind = domain.NotifyDiscussionMention
			text = fmt.Sprintf("%s 在任务「%s」的讨论中提到了你", author.DisplayName, task.Name)
		}
		if _, err := qtx.CreateNotification(r.Context(), store.CreateNotificationParams{
			UserID:    target,
			Kind:      kind,
			Content:   text,
			ProjectID: pgtype.Int8{Int64: projectId, Valid: true},
			TaskID:    pgtype.Int8{Int64: taskId, Valid: true},
		}); err != nil {
			writeInternalError(w)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w)
		return
	}
	// 返回带派生字段的完整意见（含 @ 姓名）。
	list, err := s.discussionList(r.Context(), taskId)
	if err != nil {
		writeInternalError(w)
		return
	}
	for _, item := range list {
		if item.Id == d.ID {
			writeJSON(w, http.StatusCreated, item)
			return
		}
	}
	writeInternalError(w)
}

func (s *Server) ListNotifications(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListNotificationsByUser(r.Context(), currentUser(r).ID)
	if err != nil {
		writeInternalError(w)
		return
	}
	resp := make([]Notification, 0, len(rows))
	for _, n := range rows {
		item := Notification{Id: n.ID, Kind: n.Kind, Content: n.Content, CreatedAt: n.CreatedAt.Time}
		item.ProjectId = fromPgInt8(n.ProjectID)
		item.TaskId = fromPgInt8(n.TaskID)
		if n.ReadAt.Valid {
			item.ReadAt = &n.ReadAt.Time
		}
		resp = append(resp, item)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) MarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	if _, err := s.q.MarkAllNotificationsRead(r.Context(), currentUser(r).ID); err != nil {
		writeInternalError(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) discussionList(ctx context.Context, taskID int64) ([]Discussion, error) {
	rows, err := s.q.ListDiscussionsByTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	out := make([]Discussion, 0, len(rows))
	for _, d := range rows {
		item := Discussion{
			Id:         d.ID,
			TaskId:     d.TaskID,
			AuthorId:   d.AuthorID,
			AuthorName: d.AuthorName,
			Content:    d.Content,
			CreatedAt:  d.CreatedAt.Time,
		}
		if len(d.MentionNames) > 0 {
			names := d.MentionNames
			item.MentionNames = &names
		}
		out = append(out, item)
	}
	return out, nil
}
