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

// 任务创建邀请（AC-03）。业务规则在 domain，handler 仅编排。

func (s *Server) ListTaskInvites(w http.ResponseWriter, r *http.Request, projectId int64) {
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	resp, err := s.taskInviteList(r.Context(), projectId, uid, projectActor(uid, proj.OwnerID, proj.MyRole, proj.Visibility))
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) CreateTaskInvites(w http.ResponseWriter, r *http.Request, projectId int64) {
	var req CreateTaskInvitesRequest
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
	kr, err := s.q.GetKeyResultInProject(r.Context(), store.GetKeyResultInProjectParams{ID: req.KeyResultId, ProjectID: projectId})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_key_result", Message: "所属 KR 不存在"})
			return
		}
		writeInternalError(w, r, err)
		return
	}
	if !domain.CanInviteForKr(actor) {
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
	// 去重后校验受邀成员资格。
	seen := make(map[int64]bool, len(req.InviteeIds))
	invitees := make([]int64, 0, len(req.InviteeIds))
	for _, id := range req.InviteeIds {
		if !seen[id] {
			seen[id] = true
			invitees = append(invitees, id)
		}
	}
	if err := domain.ValidateInvitees(uid, invitees, func(id int64) string { return roleByID[id] }); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_invitees", Message: err.Error()})
		return
	}
	note := ""
	if req.Note != nil {
		note = strings.TrimSpace(*req.Note)
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	// AC-03：通知与邀请同事务——邀请进了「待我处理」却没有通知，受邀人不主动打开
	// 「我的工作」就不知道自己被邀请拆任务。撤回邀请不补发（与 #5 的撤回口径一致）。
	krCode := domain.KeyResultCode(int(kr.ObjectiveCodeSeq), int(kr.CodeSeq))
	inviteContent := domain.TaskInviteNotification(currentUser(r).DisplayName, krCode, kr.Description, note)
	for _, inviteeID := range invitees {
		if _, err := qtx.CreateTaskInvite(r.Context(), store.CreateTaskInviteParams{
			KeyResultID: req.KeyResultId,
			InviterID:   uid,
			InviteeID:   inviteeID,
			Note:        note,
		}); err != nil {
			writeInternalError(w, r, err)
			return
		}
		if _, err := qtx.CreateNotification(r.Context(), store.CreateNotificationParams{
			UserID:    inviteeID,
			Kind:      domain.NotifyTaskInvite,
			Content:   inviteContent,
			ProjectID: pgtype.Int8{Int64: projectId, Valid: true},
		}); err != nil {
			writeInternalError(w, r, err)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w, r, err)
		return
	}
	resp, err := s.taskInviteList(r.Context(), projectId, uid, actor)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) RevokeTaskInvite(w http.ResponseWriter, r *http.Request, projectId int64, inviteId int64) {
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(uid, proj.OwnerID, proj.MyRole, proj.Visibility)
	invite, err := s.q.GetTaskInviteInProject(r.Context(), store.GetTaskInviteInProjectParams{ID: inviteId, ProjectID: projectId})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "invite_not_found", Message: "邀请不存在"})
		} else {
			writeInternalError(w, r, err)
		}
		return
	}
	if invite.State != domain.TaskInvitePending {
		writeJSON(w, http.StatusConflict, Error{Code: "invite_state_conflict", Message: domain.ErrInviteNotPending.Error()})
		return
	}
	if !domain.CanRevokeInvite(actor, uid, invite.InviterID, invite.State) {
		writeForbidden(w)
		return
	}
	if _, err := s.q.UpdateTaskInviteState(r.Context(), store.UpdateTaskInviteStateParams{ID: inviteId, State: domain.TaskInviteRevoked}); err != nil {
		writeInternalError(w, r, err)
		return
	}
	list, err := s.taskInviteList(r.Context(), projectId, uid, actor)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	for _, item := range list {
		if item.Id == inviteId {
			writeJSON(w, http.StatusOK, item)
			return
		}
	}
	writeInternalError(w, r, err)
}

// taskInviteList 组装项目内全部邀请及派生动作标志。
func (s *Server) taskInviteList(ctx context.Context, projectID, userID int64, actor domain.Actor) ([]TaskInvite, error) {
	rows, err := s.q.ListProjectTaskInvites(ctx, projectID)
	if err != nil {
		return nil, err
	}
	resp := make([]TaskInvite, 0, len(rows))
	for _, iv := range rows {
		resp = append(resp, TaskInvite{
			Id:          iv.ID,
			KeyResultId: iv.KeyResultID,
			InviterId:   iv.InviterID,
			InviterName: iv.InviterName,
			InviteeId:   iv.InviteeID,
			InviteeName: iv.InviteeName,
			Note:        optString(iv.Note),
			State:       TaskInviteState(iv.State),
			CreatedAt:   iv.CreatedAt.Time,
			CanHandle:   domain.CanHandleInvite(userID, iv.InviteeID, iv.State),
			CanRevoke:   domain.CanRevokeInvite(actor, userID, iv.InviterID, iv.State),
		})
	}
	return resp, nil
}
