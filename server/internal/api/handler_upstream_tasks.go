package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// #178 裁决（裁决 8）：替指定成员创建上游任务——上游任务不存在时由其完成，
// 输入请求机制退场，关系回归任务与任务之间。业务规则在 domain，handler 仅编排：
// 新任务走 #162 口径直接入池（入池动态留痕；裁决 12 #183 后无 KR 负责人、无入池通知），另通知新任务负责人；
// 自动建立「新上游任务 → 当前任务」的必要输入边（配置输入源权限口径同 #172 直改）。
func (s *Server) CreateUpstreamTask(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64) {
	var req CreateUpstreamTaskRequest
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
	// 输入源属关键字段：权限与状态判定沿 #172 直改口径。
	if !s.routeStructureChange(w, r, taskId, actor, uid, facts) {
		return
	}
	if _, err := s.q.GetKeyResultInProject(r.Context(), store.GetKeyResultInProjectParams{ID: req.KeyResultId, ProjectID: projectId}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_key_result", Message: "所属 KR 不存在"})
			return
		}
		writeInternalError(w, r, err)
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
	nt := domain.NewTask{Name: strings.TrimSpace(req.Name), OwnerID: req.OwnerId, Start: req.StartDate.Time, End: req.EndDate.Time}
	if err := domain.ValidateNewTask(nt, func(id int64) string { return roleByID[id] }); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_task", Message: err.Error()})
		return
	}

	// 创建、建边与全部通知一个事务。
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	params := store.CreateTaskParams{
		KeyResultID: req.KeyResultId,
		Name:        nt.Name,
		OwnerID:     req.OwnerId,
		StartDate:   pgtype.Date{Time: req.StartDate.Time, Valid: true},
		EndDate:     pgtype.Date{Time: req.EndDate.Time, Valid: true},
		Status:      domain.TaskNotStarted,
		CreatedBy:   uid,
	}
	if req.Description != nil {
		params.Description = strings.TrimSpace(*req.Description)
	}
	if req.CompletionCriteria != nil {
		params.CompletionCriteria = strings.TrimSpace(*req.CompletionCriteria)
	}
	upstream, err := qtx.CreateTask(r.Context(), params)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	// 通知新任务负责人（无认领确认环节；本人替自己创建不另发）。
	if target := domain.UpstreamTaskNotifyTarget(uid, req.OwnerId); target != nil {
		if err := s.notify(r.Context(), qtx, store.CreateNotificationParams{
			UserID:    *target,
			Kind:      domain.NotifyUpstreamTaskAssigned,
			Content:   domain.UpstreamTaskNotification(currentUser(r).DisplayName, upstream.Name, task.Name),
			ProjectID: pgtype.Int8{Int64: projectId, Valid: true},
			TaskID:    pgtype.Int8{Int64: upstream.ID, Valid: true},
		}); err != nil {
			writeInternalError(w, r, err)
			return
		}
	}
	// 自动建立「新上游任务 → 当前任务」的必要输入边（就绪按来源任务已完成判定，#163）。
	if _, err := qtx.CreateEdge(r.Context(), store.CreateEdgeParams{
		TargetTaskID: taskId,
		SourceTaskID: upstream.ID,
		Name:         domain.EdgeDisplayName("", upstream.Name),
		Necessity:    domain.NecessityRequired,
		CreatedBy:    uid,
	}); err != nil {
		writeInternalError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w, r, err)
		return
	}
	// 留痕：新任务记「任务入池」，当前任务记一次输入源修改。
	s.actionActivity(r.Context(), upstream.ID, domain.ActivityPoolEntered, uid, "")
	s.actionActivity(r.Context(), taskId, domain.ActivityFieldEdited, uid,
		"输入源：新建上游任务「"+upstream.Name+"」")
	s.writeTask(w, r, projectId, taskId, uid, actor)
}
