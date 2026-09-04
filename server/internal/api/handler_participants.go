package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// 参与人（词汇表「参与人」；主 PRD §4.1、§9.2）。业务规则在 domain.participant，handler 只编排。
//
// 参与人不属关键字段（§5.2.B）：不进审批，
// 名单直接落库生效；留痕由写路径装饰器统一记一条「配置参与人」项目审计（§10.4）。

// SetTaskParticipants 配置参与人：整份名单覆盖式保存，空名单即清空。
func (s *Server) SetTaskParticipants(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64) {
	var req SetParticipantsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(currentUser(r), proj.OwnerID, proj.MyRole, proj.Visibility)
	task, facts, ok := s.fetchTask(w, r, projectId, taskId)
	if !ok {
		return
	}
	if !domain.CanManageParticipants(actor, uid, facts) {
		writeJSON(w, http.StatusForbidden, Error{Code: "forbidden", Message: "只有任务负责人、创建人或可编辑项目者可以配置参与人"})
		return
	}
	members, err := s.q.ListProjectMembers(r.Context(), projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	isMember := make(map[int64]bool, len(members))
	for _, m := range members {
		isMember[m.UserID] = true
	}
	ids := domain.NormalizeParticipants(req.UserIds)
	if err := domain.ValidateParticipants(task.OwnerID, ids, func(id int64) bool { return isMember[id] }); err != nil {
		code := "invalid_participants"
		if errors.Is(err, domain.ErrParticipantIsOwner) {
			code = "participant_is_owner"
		}
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: code, Message: err.Error()})
		return
	}
	// 覆盖式保存：清空与写入同事务，避免中途失败留下半份名单。
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	if _, err := qtx.ClearTaskParticipants(r.Context(), taskId); err != nil {
		writeInternalError(w, r, err)
		return
	}
	for _, id := range ids {
		if err := qtx.SetTaskParticipant(r.Context(), store.SetTaskParticipantParams{TaskID: taskId, UserID: id}); err != nil {
			writeInternalError(w, r, err)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w, r, err)
		return
	}
	s.writeTask(w, r, projectId, taskId, uid, actor)
}
