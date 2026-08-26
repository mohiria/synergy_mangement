package api

import (
	"net/http"
	"sort"
	"strings"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"synergy/server/internal/domain"
)

// 项目报告（AC-19）：从同一份项目事实生成；范围解析在 domain，聚合在此。

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
	ctx := r.Context()

	// KR 进展：覆盖度 + 范围内终审通过任务数。
	objectives, err := s.okrList(ctx, projectId)
	if err != nil {
		writeInternalError(w)
		return Report{}, false
	}
	completionRows, err := s.q.LatestCompletionReviewsByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w)
		return Report{}, false
	}
	taskRows, err := s.q.ListProjectTasks(ctx, projectId)
	if err != nil {
		writeInternalError(w)
		return Report{}, false
	}
	krByTask := map[int64]int64{}
	taskNameByID := map[int64]string{}
	krOwnerNameByTask := map[int64]string{}
	for _, t := range taskRows {
		krByTask[t.ID] = t.KeyResultID
		taskNameByID[t.ID] = t.Name
		krOwnerNameByTask[t.ID] = t.KrOwnerName.String
	}
	// 必要输入未就绪的任务在报告里同样显示「等待输入」（§5.1；与任务列表、我的工作同口径）。
	unreadyNoteByTask, err := s.unreadyRequiredInputsByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w)
		return Report{}, false
	}
	// 下一步的状态显示文案（AC-04）：或签中任务取审核组姓名。
	reviewerRows, err := s.q.IntermediateReviewerNamesByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w)
		return Report{}, false
	}
	reviewerNamesByTask := map[int64][]string{}
	for _, rv := range reviewerRows {
		reviewerNamesByTask[rv.TaskID] = append(reviewerNamesByTask[rv.TaskID], rv.DisplayName)
	}
	completedByKr := map[int64]int{}
	for _, cr := range completionRows {
		if cr.State == domain.CompletionApproved && cr.DecidedAt.Valid && inRange(cr.DecidedAt.Time) {
			completedByKr[krByTask[cr.TaskID]]++
		}
	}
	krProgress := []ReportKrProgress{}
	for _, o := range objectives {
		for _, k := range o.KeyResults {
			item := ReportKrProgress{
				KeyResultId:      k.Id,
				Description:      k.Description,
				RiskLevel:        k.RiskLevel,
				CompletedInRange: completedByKr[k.Id],
			}
			if k.ProgressSummary != nil {
				item.TotalTasks = k.ProgressSummary.TotalTasks
				item.FilledTasks = k.ProgressSummary.FilledTasks
				item.AverageProgress = k.ProgressSummary.AverageProgress
			}
			krProgress = append(krProgress, item)
		}
	}

	// 完成成果：范围内生效的当前内容。
	files, err := s.q.ListDeliverableFilesByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w)
		return Report{}, false
	}
	deliverables, err := s.q.ListDeliverablesByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w)
		return Report{}, false
	}
	deliverableName := map[int64]string{}
	deliverableTask := map[int64]int64{}
	for _, d := range deliverables {
		deliverableName[d.ID] = d.Name
		deliverableTask[d.ID] = d.TaskID
	}
	completedDeliverables := []ReportDeliverable{}
	for _, f := range files {
		if f.State == domain.DeliverableCurrent && f.EffectiveAt.Valid && inRange(f.EffectiveAt.Time) {
			item := ReportDeliverable{
				TaskName:        taskNameByID[deliverableTask[f.DeliverableID]],
				DeliverableName: deliverableName[f.DeliverableID],
				FileName:        f.FileName,
			}
			eff := f.EffectiveAt.Time
			item.EffectiveAt = &eff
			completedDeliverables = append(completedDeliverables, item)
		}
	}

	// 风险卡点：当前派生的全部卡点（触发条件消失即自动解除，没有历史态可汇总）。
	derived, err := s.projectBlockers(ctx, projectId)
	if err != nil {
		writeInternalError(w)
		return Report{}, false
	}
	blockers := []ReportBlocker{}
	for _, b := range derived {
		item := ReportBlocker{
			TaskName:        b.TaskName,
			Kind:            BlockerKind(b.Kind),
			Missing:         b.Missing,
			Reason:          b.Reason,
			Level:           RiskLevel(b.Level),
			ActionOwnerName: optString(strings.Join(b.ActionOwnerNames, "、")),
		}
		since := b.Since
		item.Since = &since
		blockers = append(blockers, item)
	}

	// 待决策：停留在审批队列中的事项数。
	poolRows, err := s.q.LatestPoolReviewsByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w)
		return Report{}, false
	}
	changeRows, err := s.q.LatestFieldChangesByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w)
		return Report{}, false
	}
	pending := struct {
		Completions  int `json:"completions"`
		FieldChanges int `json:"fieldChanges"`
		PoolReviews  int `json:"poolReviews"`
	}{}
	for _, pr := range poolRows {
		if pr.Status == domain.PoolReviewPending {
			pending.PoolReviews++
		}
	}
	for _, fc := range changeRows {
		if fc.State == domain.FieldChangePendingState {
			pending.FieldChanges++
		}
	}
	for _, cr := range completionRows {
		if cr.State == domain.CompletionIntermediate || cr.State == domain.CompletionPendingFinal {
			pending.Completions++
		}
	}

	// 下一步：未完成任务中临近截止或已超期者（截止升序，前 10）。
	nextSteps := []ReportNextStep{}
	horizon := now.AddDate(0, 0, 7)
	for _, t := range taskRows {
		switch t.Status {
		case domain.TaskCompleted, domain.TaskCancelled, domain.TaskDraft:
			continue
		}
		if !t.EndDate.Valid || t.EndDate.Time.After(horizon) {
			continue
		}
		display := domain.DeriveDisplayStatus(t.Status, unreadyNoteByTask[t.ID] != "")
		item := ReportNextStep{
			TaskName:    t.Name,
			OwnerName:   t.OwnerName,
			Status:      TaskStatus(display),
			StatusLabel: domain.StatusLabel(display, krOwnerNameByTask[t.ID], reviewerNamesByTask[t.ID]),
		}
		d := openapi_types.Date{Time: t.EndDate.Time}
		item.EndDate = &d
		overdue := now.After(t.EndDate.Time.AddDate(0, 0, 1))
		item.Overdue = &overdue
		nextSteps = append(nextSteps, item)
	}
	sort.Slice(nextSteps, func(i, j int) bool {
		return nextSteps[i].EndDate.Time.Before(nextSteps[j].EndDate.Time)
	})
	if len(nextSteps) > 10 {
		nextSteps = nextSteps[:10]
	}

	return Report{
		Range:                 ReportRange(rangeName),
		GeneratedAt:           now,
		KrProgress:            krProgress,
		CompletedDeliverables: completedDeliverables,
		Blockers:              blockers,
		PendingApprovals:      pending,
		NextSteps:             nextSteps,
	}, true
}
