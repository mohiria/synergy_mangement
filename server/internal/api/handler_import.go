package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
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
	// AC-68：这一次导入无论成败都要留一条记录。成功的一条随导入同事务写入，
	// 失败的一条在事务回滚之后单独写——回滚会把同事务里的记录一并抹掉，
	// 而「失败不留痕」正是本票要修的问题。
	sourceFileName := ""
	if req.SourceFileName != nil {
		sourceFileName = strings.TrimSpace(*req.SourceFileName)
	}
	failImport := func(status int, code, msg string) {
		s.recordImportFailure(r.Context(), projectId, uid, sourceFileName, msg)
		writeJSON(w, status, Error{Code: code, Message: msg})
	}
	failImportInternal := func(err error) {
		s.recordImportFailure(r.Context(), projectId, uid, sourceFileName, "服务端错误，导入未生效")
		writeInternalError(w, r, err)
	}
	members, err := s.q.ListProjectMembers(r.Context(), projectId)
	if err != nil {
		failImportInternal(err)
		return
	}
	roleByID := make(map[int64]string, len(members))
	for _, m := range members {
		roleByID[m.UserID] = m.Role
	}
	roleOf := func(id int64) string { return roleByID[id] }

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
						if err := domain.ValidateNewTask(nt, roleOf); err != nil {
							failImport(http.StatusUnprocessableEntity, "invalid_task", err.Error())
							return
						}
						if tk.ExpectedDeliverable != nil && strings.TrimSpace(*tk.ExpectedDeliverable) != "" {
							if err := domain.ValidateDeliverableName(strings.TrimSpace(*tk.ExpectedDeliverable)); err != nil {
								failImport(http.StatusUnprocessableEntity, "invalid_deliverable", err.Error())
								return
							}
						}
					}
				}
			}
		}
		okrItems = append(okrItems, oi)
	}
	if err := domain.ValidateOkrBatch(okrItems, roleOf); err != nil {
		failImport(http.StatusUnprocessableEntity, "invalid_okr", err.Error())
		return
	}
	for _, item := range req.Items {
		if item.ObjectiveId == nil {
			continue
		}
		if _, err := s.q.GetObjective(r.Context(), store.GetObjectiveParams{ID: *item.ObjectiveId, ProjectID: projectId}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				failImport(http.StatusUnprocessableEntity, "invalid_objective", "所属 O 不存在")
				return
			}
			failImportInternal(err)
			return
		}
	}

	// 整批一个事务：O → KR → 任务草稿（含预期交付物项）。
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		failImportInternal(err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	// 计数取本次真实写入量，不是请求里报的条数（AC-68）。
	counts := domain.ImportCounts{}
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
				failImportInternal(err)
				return
			}
			objectiveID = o.ID
			counts.Objectives++
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
			})
			if err != nil {
				failImportInternal(err)
				return
			}
			counts.KeyResults++
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
					failImportInternal(err)
					return
				}
				counts.Tasks++
				if tk.ExpectedDeliverable != nil {
					if dn := strings.TrimSpace(*tk.ExpectedDeliverable); dn != "" {
						if _, err := qtx.CreateDeliverable(r.Context(), store.CreateDeliverableParams{TaskID: task.ID, Name: dn, CreatedBy: uid}); err != nil {
							failImportInternal(err)
							return
						}
					}
				}
			}
		}
	}
	// AC-68：记录与导入同事务——导入回滚则记录不留，不会出现「记了成功但什么都没建」。
	if _, err := qtx.CreateImportRecord(r.Context(), store.CreateImportRecordParams{
		ProjectID: projectId, OperatorID: uid, SourceFileName: sourceFileName,
		ObjectiveCount: int32(counts.Objectives), KeyResultCount: int32(counts.KeyResults),
		TaskCount: int32(counts.Tasks), Result: domain.DeriveImportOutcome(counts, ""),
	}); err != nil {
		failImportInternal(err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		failImportInternal(err)
		return
	}
	// 到这里导入已经落库：后面只是回读展示数据，出错不改变「本次导入成功」这个事实。
	objectives, err := s.okrList(r.Context(), projectId, actor, uid)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	tasks, err := s.taskList(r.Context(), projectId, uid, actor)
	if err != nil {
		writeInternalError(w, r, err)
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
		hasPending, err := s.q.HasPendingFieldChange(r.Context(), id)
		if err != nil {
			writeInternalError(w, r, err)
			return
		}
		if err := domain.SubmitPoolReview(facts, hasPending); err != nil {
			if errors.Is(err, domain.ErrTaskNotDraft) || errors.Is(err, domain.ErrCancelBlocked) {
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
		writeInternalError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	for _, tg := range targets {
		if _, err := qtx.UpdateTaskStatus(r.Context(), store.UpdateTaskStatusParams{ID: tg.id, Status: domain.TaskPendingPoolReview}); err != nil {
			writeInternalError(w, r, err)
			return
		}
		if _, err := qtx.CreatePoolReview(r.Context(), store.CreatePoolReviewParams{TaskID: tg.id, SubmittedBy: uid, Status: domain.PoolReviewPending}); err != nil {
			writeInternalError(w, r, err)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w, r, err)
		return
	}
	resp, err := s.taskList(r.Context(), projectId, uid, actor)
	if err != nil {
		writeInternalError(w, r, err)
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
	// 规则与写入同事务：逐个锁任务行后再重读事实，整批要么一起生效要么一起回滚（R2）。
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	targets := []target{}
	for _, id := range req.TaskIds {
		task, facts, ok := lockTaskFacts(r, w, qtx, projectId, id)
		if !ok {
			return
		}
		newStatus, err := domain.DecidePoolReview(actor, facts, uid, approve, opinion)
		if err != nil {
			switch {
			case errors.Is(err, domain.ErrPoolReviewNotPending):
				writeJSON(w, http.StatusConflict, Error{Code: "task_state_conflict", Message: task.Name + "：" + err.Error()})
			case errors.Is(err, domain.ErrRejectOpinionRequired):
				writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "opinion_required", Message: err.Error()})
			default:
				writeJSON(w, http.StatusForbidden, Error{Code: "forbidden", Message: task.Name + "：" + err.Error()})
			}
			return
		}
		review, err := qtx.GetLatestPoolReview(r.Context(), id)
		if err != nil || review.Status != domain.PoolReviewPending {
			writeInternalError(w, r, err)
			return
		}
		targets = append(targets, target{taskID: id, reviewID: review.ID, newStatus: newStatus})
	}
	reviewStatus := domain.PoolReviewRejected
	if approve {
		reviewStatus = domain.PoolReviewApproved
	}
	for _, tg := range targets {
		if _, err := qtx.DecidePoolReview(r.Context(), store.DecidePoolReviewParams{
			ID: tg.reviewID, Status: reviewStatus, Opinion: opinion, DecidedBy: pgtype.Int8{Int64: uid, Valid: true},
		}); err != nil {
			writeInternalError(w, r, err)
			return
		}
		if _, err := qtx.UpdateTaskStatus(r.Context(), store.UpdateTaskStatusParams{ID: tg.taskID, Status: tg.newStatus}); err != nil {
			writeInternalError(w, r, err)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w, r, err)
		return
	}
	// 入池通过的任务补发输入请求通知（与单条路径一致）。
	if approve {
		for _, id := range req.TaskIds {
			if task, err := s.q.GetTaskInProject(r.Context(), store.GetTaskInProjectParams{ID: id, ProjectID: projectId}); err == nil {
				s.notifyPendingInputRequests(r, projectId, task.ID, task.Name)
			}
		}
	}
	resp, err := s.taskList(r.Context(), projectId, uid, actor)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// recordImportFailure 记一条失败的导入（AC-68）：失败绝不写成功，也不能不留痕。
// 用后台上下文单独写：调用它时业务事务已经回滚或即将回滚，跟着回滚就等于没记。
func (s *Server) recordImportFailure(ctx context.Context, projectID, operatorID int64, fileName, summary string) {
	if _, err := s.q.CreateImportRecord(context.WithoutCancel(ctx), store.CreateImportRecordParams{
		ProjectID: projectID, OperatorID: operatorID, SourceFileName: fileName,
		Result: domain.DeriveImportOutcome(domain.ImportCounts{}, summary), FailureSummary: summary,
	}); err != nil {
		log.Printf("import record: 记录导入失败未成功: project=%d err=%v", projectID, err)
	}
}

// ListImportRecords 项目设置的「导入记录」分节（§7.9、AC-68；仅项目管理员，只读）。
func (s *Server) ListImportRecords(w http.ResponseWriter, r *http.Request, projectId int64) {
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	if !domain.CanEditProject(projectActor(uid, proj.OwnerID, proj.MyRole)) {
		writeForbidden(w)
		return
	}
	rows, err := s.q.ListImportRecordsByProject(r.Context(), projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	out := make([]ImportRecord, 0, len(rows))
	for _, row := range rows {
		item := ImportRecord{
			Id:             row.ID,
			OperatorName:   row.OperatorName,
			SourceFileName: optString(row.SourceFileName),
			ObjectiveCount: int(row.ObjectiveCount),
			KeyResultCount: int(row.KeyResultCount),
			TaskCount:      int(row.TaskCount),
			Result:         ImportRecordResult(row.Result),
			ResultLabel:    domain.ImportOutcomeLabel(row.Result),
			FailureSummary: optString(row.FailureSummary),
		}
		if row.ImportedAt.Valid {
			item.ImportedAt = row.ImportedAt.Time
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, out)
}
