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

// 表格导入与批量入池（AC-02、AC-25）。字段映射/人员匹配/结构预览在前端完成，
// 此处接收结构化结果；校验与规则复用 domain。

func (s *Server) ImportTable(w http.ResponseWriter, r *http.Request, projectId int64) {
	var req ImportRequest
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
	if !domain.CanEditProject(actor) {
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
	isMember := func(id int64) bool { return memberSet[id] }

	// 预校验：O/KR 结构复用 OKR 批量规则；任务复用任务草稿规则。
	okrItems := make([]domain.OkrBatchItem, 0, len(req.Items))
	for _, item := range req.Items {
		oi := domain.OkrBatchItem{ObjectiveID: item.ObjectiveId}
		if item.Title != nil {
			oi.Title = strings.TrimSpace(*item.Title)
		}
		if item.Description != nil {
			oi.Description = strings.TrimSpace(*item.Description)
		}
		if item.KeyResults != nil {
			for _, k := range *item.KeyResults {
				kr := domain.NewKeyResult{
					Description: strings.TrimSpace(k.Description),
					OwnerID:     k.OwnerId,
					Start:       toTimePtr(k.StartDate),
					End:         toTimePtr(k.EndDate),
				}
				if k.Metric != nil {
					kr.Metric = strings.TrimSpace(*k.Metric)
				}
				oi.KeyResults = append(oi.KeyResults, kr)
				if k.Tasks != nil {
					for _, tk := range *k.Tasks {
						nt := domain.NewTask{Name: strings.TrimSpace(tk.Name), OwnerID: tk.OwnerId, Start: tk.StartDate.Time, End: tk.EndDate.Time}
						if err := domain.ValidateNewTask(nt, isMember); err != nil {
							writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_task", Message: err.Error()})
							return
						}
						if tk.ExpectedDeliverable != nil && strings.TrimSpace(*tk.ExpectedDeliverable) != "" {
							if err := domain.ValidateDeliverableName(strings.TrimSpace(*tk.ExpectedDeliverable)); err != nil {
								writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_deliverable", Message: err.Error()})
								return
							}
						}
					}
				}
			}
		}
		okrItems = append(okrItems, oi)
	}
	if err := domain.ValidateOkrBatch(okrItems, isMember); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_okr", Message: err.Error()})
		return
	}
	for _, item := range req.Items {
		if item.ObjectiveId == nil {
			continue
		}
		if _, err := s.q.GetObjective(r.Context(), store.GetObjectiveParams{ID: *item.ObjectiveId, ProjectID: projectId}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_objective", Message: "所属 O 不存在"})
				return
			}
			writeInternalError(w)
			return
		}
	}

	// 整批一个事务：O → KR → 任务草稿（含预期交付物项）。
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	for _, item := range req.Items {
		objectiveID := int64(0)
		if item.ObjectiveId != nil {
			objectiveID = *item.ObjectiveId
		} else {
			title := ""
			if item.Title != nil {
				title = strings.TrimSpace(*item.Title)
			}
			desc := ""
			if item.Description != nil {
				desc = strings.TrimSpace(*item.Description)
			}
			o, err := qtx.CreateObjective(r.Context(), store.CreateObjectiveParams{ProjectID: projectId, Title: title, Description: desc})
			if err != nil {
				writeInternalError(w)
				return
			}
			objectiveID = o.ID
		}
		if item.KeyResults == nil {
			continue
		}
		for _, k := range *item.KeyResults {
			metric := ""
			if k.Metric != nil {
				metric = strings.TrimSpace(*k.Metric)
			}
			kr, err := qtx.CreateKeyResult(r.Context(), store.CreateKeyResultParams{
				ObjectiveID: objectiveID,
				Description: strings.TrimSpace(k.Description),
				Metric:      metric,
				OwnerID:     toPgInt8(k.OwnerId),
				StartDate:   toPgDate(k.StartDate),
				EndDate:     toPgDate(k.EndDate),
				RiskLevel:   domain.DefaultKrRiskLevel,
			})
			if err != nil {
				writeInternalError(w)
				return
			}
			if k.Tasks == nil {
				continue
			}
			for _, tk := range *k.Tasks {
				task, err := qtx.CreateTask(r.Context(), store.CreateTaskParams{
					KeyResultID: kr.ID,
					Name:        strings.TrimSpace(tk.Name),
					OwnerID:     tk.OwnerId,
					StartDate:   pgtype.Date{Time: tk.StartDate.Time, Valid: true},
					EndDate:     pgtype.Date{Time: tk.EndDate.Time, Valid: true},
					Status:      domain.TaskDraft,
					CreatedBy:   uid,
				})
				if err != nil {
					writeInternalError(w)
					return
				}
				if tk.ExpectedDeliverable != nil {
					if dn := strings.TrimSpace(*tk.ExpectedDeliverable); dn != "" {
						if _, err := qtx.CreateDeliverable(r.Context(), store.CreateDeliverableParams{TaskID: task.ID, Name: dn, CreatedBy: uid}); err != nil {
							writeInternalError(w)
							return
						}
					}
				}
			}
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w)
		return
	}
	objectives, err := s.okrList(r.Context(), projectId)
	if err != nil {
		writeInternalError(w)
		return
	}
	tasks, err := s.taskList(r.Context(), projectId, uid, actor)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, ImportResult{Objectives: objectives, Tasks: tasks})
}

// BatchSubmitPool 批量提交入池（AC-25）：整批一个事务，任一任务不满足则全部失败。
func (s *Server) BatchSubmitPool(w http.ResponseWriter, r *http.Request, projectId int64) {
	var req BatchPoolSubmitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.TaskIds) == 0 {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析或为空"})
		return
	}
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(uid, proj.OwnerID, proj.MyRole)
	type target struct{ id int64 }
	targets := []target{}
	for _, id := range req.TaskIds {
		task, err := s.q.GetTaskInProject(r.Context(), store.GetTaskInProjectParams{ID: id, ProjectID: projectId})
		if err != nil {
			writeJSON(w, http.StatusNotFound, Error{Code: "task_not_found", Message: "任务不存在"})
			return
		}
		facts := domain.TaskFacts{Status: task.Status, CreatorID: task.CreatedBy, OwnerID: task.OwnerID, KrOwnerID: fromPgInt8(task.KrOwnerID)}
		if uid != task.CreatedBy && uid != task.OwnerID && !domain.CanEditProject(actor) {
			writeForbidden(w)
			return
		}
		if err := domain.SubmitPoolReview(facts); err != nil {
			if errors.Is(err, domain.ErrTaskNotDraft) {
				writeJSON(w, http.StatusConflict, Error{Code: "task_state_conflict", Message: task.Name + "：" + err.Error()})
			} else {
				writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "kr_owner_missing", Message: task.Name + "：" + err.Error()})
			}
			return
		}
		targets = append(targets, target{id: id})
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	for _, tg := range targets {
		if _, err := qtx.UpdateTaskStatus(r.Context(), store.UpdateTaskStatusParams{ID: tg.id, Status: domain.TaskPendingPoolReview}); err != nil {
			writeInternalError(w)
			return
		}
		if _, err := qtx.CreatePoolReview(r.Context(), store.CreatePoolReviewParams{TaskID: tg.id, SubmittedBy: uid, Status: domain.PoolReviewPending}); err != nil {
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
	writeJSON(w, http.StatusOK, resp)
}

// BatchDecidePool 批量通过/退回入池审批（AC-25）：每个任务须由其所属 KR 负责人处理。
func (s *Server) BatchDecidePool(w http.ResponseWriter, r *http.Request, projectId int64) {
	var req BatchPoolDecisionRequest
	// decision 必须落在枚举内：与其余三处 decide 端点同口径，避免非法值被当作「退回」执行。
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !req.Decision.Valid() || len(req.TaskIds) == 0 {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析或为空"})
		return
	}
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(uid, proj.OwnerID, proj.MyRole)
	approve := req.Decision == BatchPoolDecisionRequestDecisionApproved
	opinion := ""
	if req.Opinion != nil {
		opinion = strings.TrimSpace(*req.Opinion)
	}
	type target struct {
		taskID    int64
		reviewID  int64
		newStatus string
	}
	targets := []target{}
	for _, id := range req.TaskIds {
		task, err := s.q.GetTaskInProject(r.Context(), store.GetTaskInProjectParams{ID: id, ProjectID: projectId})
		if err != nil {
			writeJSON(w, http.StatusNotFound, Error{Code: "task_not_found", Message: "任务不存在"})
			return
		}
		facts := domain.TaskFacts{Status: task.Status, CreatorID: task.CreatedBy, OwnerID: task.OwnerID, KrOwnerID: fromPgInt8(task.KrOwnerID)}
		newStatus, err := domain.DecidePoolReview(facts, uid, approve)
		if err != nil {
			if errors.Is(err, domain.ErrPoolReviewNotPending) {
				writeJSON(w, http.StatusConflict, Error{Code: "task_state_conflict", Message: task.Name + "：" + err.Error()})
			} else {
				writeJSON(w, http.StatusForbidden, Error{Code: "forbidden", Message: task.Name + "：" + err.Error()})
			}
			return
		}
		review, err := s.q.GetLatestPoolReview(r.Context(), id)
		if err != nil || review.Status != domain.PoolReviewPending {
			writeInternalError(w)
			return
		}
		targets = append(targets, target{taskID: id, reviewID: review.ID, newStatus: newStatus})
	}
	reviewStatus := domain.PoolReviewRejected
	if approve {
		reviewStatus = domain.PoolReviewApproved
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	for _, tg := range targets {
		if _, err := qtx.DecidePoolReview(r.Context(), store.DecidePoolReviewParams{
			ID: tg.reviewID, Status: reviewStatus, Opinion: opinion, DecidedBy: pgtype.Int8{Int64: uid, Valid: true},
		}); err != nil {
			writeInternalError(w)
			return
		}
		if _, err := qtx.UpdateTaskStatus(r.Context(), store.UpdateTaskStatusParams{ID: tg.taskID, Status: tg.newStatus}); err != nil {
			writeInternalError(w)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w)
		return
	}
	// 入池通过的任务补发输入请求通知（与单条路径一致）。
	if approve {
		for _, id := range req.TaskIds {
			if task, err := s.q.GetTaskInProject(r.Context(), store.GetTaskInProjectParams{ID: id, ProjectID: projectId}); err == nil {
				s.notifyPendingInputRequests(r, projectId, task)
			}
		}
	}
	resp, err := s.taskList(r.Context(), projectId, uid, actor)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
