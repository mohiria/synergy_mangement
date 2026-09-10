package api

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// 项目报告（AC-19；PRD §7.8）：从同一份项目事实实时生成；范围与窗口判定在 domain，聚合在此。

func (s *Server) GetReport(w http.ResponseWriter, r *http.Request, projectId int64, params GetReportParams) {
	if _, ok := s.fetchProject(w, r, projectId); !ok {
		return
	}
	rangeName := "all"
	if params.Range != nil {
		rangeName = string(*params.Range)
	}
	report, ok := s.buildReport(w, r, projectId, rangeName)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// buildReport 聚合报告事实（GetReport 与导出共用）；失败时已写出响应并返回 false。
func (s *Server) buildReport(w http.ResponseWriter, r *http.Request, projectId int64, rangeName string) (Report, bool) {
	now := s.now()
	from, err := domain.ReportRangeFrom(rangeName, now)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_range", Message: err.Error()})
		return Report{}, false
	}
	inRange := func(t time.Time) bool { return from == nil || !t.Before(*from) }
	horizon := domain.ReportHorizonDays(rangeName)
	ctx := r.Context()
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return Report{}, false
	}
	uid := currentUser(r).ID
	actor := projectActor(currentUser(r), proj.OwnerID, proj.MyRole, proj.Visibility)

	objectives, err := s.okrList(ctx, projectId, actor, uid)
	if err != nil {
		writeInternalError(w, r, err)
		return Report{}, false
	}
	completionRows, err := s.q.LatestCompletionReviewsByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return Report{}, false
	}
	taskRows, err := s.q.ListProjectTasks(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return Report{}, false
	}
	sort.SliceStable(taskRows, func(i, j int) bool { return taskRows[i].CodeSeq < taskRows[j].CodeSeq })
	taskByID := map[int64]store.ListProjectTasksRow{}
	for _, t := range taskRows {
		taskByID[t.ID] = t
	}
	taskCode := func(t store.ListProjectTasksRow) string {
		return domain.TaskCode(int(t.ObjectiveCodeSeq), int(t.KrCodeSeq), int(t.CodeSeq))
	}
	krCode := func(t store.ListProjectTasksRow) string {
		return domain.KeyResultCode(int(t.ObjectiveCodeSeq), int(t.KrCodeSeq))
	}
	krDescription := map[int64]string{}
	for _, o := range objectives {
		for _, k := range o.KeyResults {
			krDescription[k.Id] = k.Description
		}
	}
	// 终审人集合（裁决 11，#181）：待终审的显示文案取项目管理员姓名。
	finalIDs, finalNames, err := s.projectFinalReviewers(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return Report{}, false
	}
	finalReviewers := domain.ZipApprovers(finalIDs, finalNames)
	// 审核中任务的当前环节从完成申请单读取（裁决 13，#182）。
	reviewStageByTask, err := s.pendingReviewStageByTask(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return Report{}, false
	}
	// 必要输入未就绪的任务在报告里同样显示「等待输入」（§5.1；与任务列表、我的工作同口径）。
	unreadyNoteByTask, err := s.unreadyRequiredInputsByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return Report{}, false
	}
	// 状态显示文案（AC-04）：或签中任务取审核组。
	reviewersByTask, err := s.intermediateReviewersByTask(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return Report{}, false
	}
	displayOf := func(t store.ListProjectTasksRow) (TaskStatus, string) {
		display := domain.DeriveDisplayStatus(t.Status, unreadyNoteByTask[t.ID] != "")
		return TaskStatus(display), domain.StatusLabel(display, reviewStageByTask[t.ID], uid, finalReviewers, reviewersByTask[t.ID])
	}

	// 一、本期成果：范围内终审通过的任务 ∪ 范围内有当前交付内容生效的任务，按 O → KR → 任务归组。
	completedAt := map[int64]time.Time{}
	completedByKr := map[int64]int{}
	pendingCompletions := 0
	for _, cr := range completionRows {
		switch {
		case cr.State == domain.CompletionApproved && cr.DecidedAt.Valid && inRange(cr.DecidedAt.Time):
			completedAt[cr.TaskID] = cr.DecidedAt.Time
			completedByKr[taskByID[cr.TaskID].KeyResultID]++
		case cr.State == domain.CompletionIntermediate || cr.State == domain.CompletionPendingFinal:
			pendingCompletions++
		}
	}
	files, err := s.q.ListDeliverableFilesByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return Report{}, false
	}
	deliverables, err := s.q.ListDeliverablesByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return Report{}, false
	}
	deliverableName := map[int64]string{}
	deliverableTask := map[int64]int64{}
	for _, d := range deliverables {
		deliverableName[d.ID] = d.Name
		deliverableTask[d.ID] = d.TaskID
	}
	filesByTask := map[int64][]ReportFile{}
	effectiveFiles := 0
	for _, f := range files {
		if f.State != domain.DeliverableCurrent || !f.EffectiveAt.Valid || !inRange(f.EffectiveAt.Time) {
			continue
		}
		tid := deliverableTask[f.DeliverableID]
		filesByTask[tid] = append(filesByTask[tid], ReportFile{
			DeliverableName: deliverableName[f.DeliverableID],
			FileName:        f.FileName,
			EffectiveAt:     f.EffectiveAt.Time,
		})
		effectiveFiles++
	}
	deliveryByKr := map[int64][]ReportDeliveryTask{}
	for _, t := range taskRows {
		done, isDone := completedAt[t.ID]
		fs := filesByTask[t.ID]
		if !isDone && len(fs) == 0 {
			continue
		}
		sort.SliceStable(fs, func(i, j int) bool { return fs[i].EffectiveAt.Before(fs[j].EffectiveAt) })
		status, label := displayOf(t)
		item := ReportDeliveryTask{
			TaskId:      t.ID,
			Code:        taskCode(t),
			Name:        t.Name,
			OwnerName:   t.OwnerName,
			Status:      status,
			StatusLabel: label,
			Files:       append([]ReportFile{}, fs...),
		}
		if isDone {
			d := done
			item.CompletedAt = &d
		} else if t.Progress.Valid {
			p := int(t.Progress.Int32)
			item.Progress = &p
		}
		deliveryByKr[t.KeyResultID] = append(deliveryByKr[t.KeyResultID], item)
	}
	deliveryObjectives := []ReportDeliveryObjective{}
	okrProgress := []ReportObjectiveProgress{}
	for _, o := range objectives {
		dObj := ReportDeliveryObjective{ObjectiveId: o.Id, Code: o.Code, Title: o.Title, KeyResults: []ReportDeliveryKr{}}
		pObj := ReportObjectiveProgress{ObjectiveId: o.Id, Code: o.Code, Title: o.Title, KeyResults: []ReportKrProgress{}}
		for _, k := range o.KeyResults {
			progress := ReportKrProgress{
				KeyResultId:      k.Id,
				Code:             k.Code,
				Description:      k.Description,
				RiskLevel:        k.RiskLevel,
				CompletedInRange: completedByKr[k.Id],
			}
			if k.ProgressSummary != nil {
				progress.TotalTasks = k.ProgressSummary.TotalTasks
				progress.FilledTasks = k.ProgressSummary.FilledTasks
				progress.AverageProgress = k.ProgressSummary.AverageProgress
			}
			pObj.KeyResults = append(pObj.KeyResults, progress)
			if tasks := deliveryByKr[k.Id]; len(tasks) > 0 {
				dObj.KeyResults = append(dObj.KeyResults, ReportDeliveryKr{
					KeyResultId:     k.Id,
					Code:            k.Code,
					Description:     k.Description,
					AverageProgress: progress.AverageProgress,
					Tasks:           tasks,
				})
			}
		}
		if len(dObj.KeyResults) > 0 {
			deliveryObjectives = append(deliveryObjectives, dObj)
		}
		okrProgress = append(okrProgress, pObj)
	}

	// 二、风险与卡点：当前开放卡点（按出现时间升序，标本期新出现／上期遗留）＋ 范围内解除的卡点（来自动态）。
	derived, err := s.projectBlockers(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return Report{}, false
	}
	sort.SliceStable(derived, func(i, j int) bool { return derived[i].Since.Before(derived[j].Since) })
	open := []ReportBlocker{}
	newInRange := 0
	for _, b := range derived {
		item := ReportBlocker{
			TaskId:          b.TaskID,
			Code:            taskCode(taskByID[b.TaskID]),
			TaskName:        b.TaskName,
			Kind:            BlockerKind(b.Kind),
			KindLabel:       domain.BlockerKindLabel(b.Kind),
			Missing:         b.Missing,
			Reason:          b.Reason,
			Level:           RiskLevel(b.Level),
			ActionOwnerName: optString(strings.Join(b.ActionOwnerNames, "、")),
			StayDays:        domain.DaysBetween(b.Since, now),
		}
		since := b.Since
		item.Since = &since
		if phase, label := domain.ReportBlockerPhase(b.Since, from); phase != "" {
			p := ReportBlockerPhase(phase)
			item.Phase = &p
			item.PhaseLabel = optString(label)
			if phase == domain.ReportPhaseNew {
				newInRange++
			}
		}
		open = append(open, item)
	}
	var since pgtype.Timestamptz
	if from != nil {
		since = pgtype.Timestamptz{Time: *from, Valid: true}
	}
	resolvedRows, err := s.q.ListResolvedBlockerActivitiesByProject(ctx, store.ListResolvedBlockerActivitiesByProjectParams{ProjectID: projectId, Since: since})
	if err != nil {
		writeInternalError(w, r, err)
		return Report{}, false
	}
	resolved := []ReportResolvedBlocker{}
	for _, row := range resolvedRows {
		kind, missing := domain.ParseBlockerActivity(row.BlockerKey.String, row.Summary)
		openedAt := row.ResolvedAt.Time
		if row.OpenedAt.Valid {
			openedAt = row.OpenedAt.Time
		}
		resolved = append(resolved, ReportResolvedBlocker{
			TaskId:       row.TaskID,
			Code:         domain.TaskCode(int(row.ObjectiveCodeSeq), int(row.KrCodeSeq), int(row.CodeSeq)),
			TaskName:     row.TaskName,
			Kind:         BlockerKind(kind),
			KindLabel:    domain.BlockerKindLabel(kind),
			Missing:      missing,
			OpenedAt:     openedAt,
			ResolvedAt:   row.ResolvedAt.Time,
			DurationDays: domain.DaysBetween(openedAt, row.ResolvedAt.Time),
		})
	}

	// 三、下一步：窗口内到期或已超期的未完成任务（截止升序）＋ 即将启动（开始日升序）。
	due := []ReportNextStep{}
	upcoming := []ReportUpcomingTask{}
	for _, t := range taskRows {
		switch t.Status {
		case domain.TaskCompleted, domain.TaskCancelled:
			continue
		}
		var end, start *time.Time
		if t.EndDate.Valid {
			v := t.EndDate.Time
			end = &v
		}
		if t.StartDate.Valid {
			v := t.StartDate.Time
			start = &v
		}
		if in, overdueDays, dueInDays := domain.ReportDueWindow(end, now, horizon); in {
			status, label := displayOf(t)
			item := ReportNextStep{
				TaskId:               t.ID,
				Code:                 taskCode(t),
				TaskName:             t.Name,
				KeyResultCode:        optString(krCode(t)),
				KeyResultDescription: optString(krDescription[t.KeyResultID]),
				OwnerName:            t.OwnerName,
				Status:               status,
				StatusLabel:          label,
				EndDate:              openapi_types.Date{Time: *end},
				UnreadyNote:          optString(unreadyNoteByTask[t.ID]),
			}
			if overdueDays > 0 {
				item.OverdueDays = &overdueDays
			} else {
				item.DueInDays = &dueInDays
			}
			due = append(due, item)
		}
		if domain.ReportUpcomingStart(start, t.Status, now, horizon) {
			status, label := displayOf(t)
			upcoming = append(upcoming, ReportUpcomingTask{
				TaskId:               t.ID,
				Code:                 taskCode(t),
				TaskName:             t.Name,
				KeyResultCode:        optString(krCode(t)),
				KeyResultDescription: optString(krDescription[t.KeyResultID]),
				OwnerName:            t.OwnerName,
				Status:               status,
				StatusLabel:          label,
				StartDate:            openapi_types.Date{Time: *start},
				UnreadyNote:          optString(unreadyNoteByTask[t.ID]),
			})
		}
	}
	sort.SliceStable(due, func(i, j int) bool { return due[i].EndDate.Time.Before(due[j].EndDate.Time) })
	sort.SliceStable(upcoming, func(i, j int) bool { return upcoming[i].StartDate.Time.Before(upcoming[j].StartDate.Time) })

	return Report{
		Range:       ReportRange(rangeName),
		From:        from,
		GeneratedAt: now,
		Deliveries: ReportDeliveries{
			CompletedTasks: len(completedAt),
			EffectiveFiles: effectiveFiles,
			Objectives:     deliveryObjectives,
		},
		Blockers: ReportBlockers{
			Open:               open,
			Resolved:           resolved,
			NewInRange:         newInRange,
			ResolvedInRange:    len(resolved),
			PendingCompletions: pendingCompletions,
		},
		NextSteps:   ReportNextSteps{HorizonDays: horizon, Due: due, Upcoming: upcoming},
		OkrProgress: okrProgress,
	}, true
}
