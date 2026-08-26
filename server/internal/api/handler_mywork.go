package api

import (
	"net/http"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"synergy/server/internal/domain"
)

// 我的工作五分组（AC-16）。事实装配在此，分组规则在 domain.MyWork。

func (s *Server) GetMyWork(w http.ResponseWriter, r *http.Request, projectId int64) {
	if _, ok := s.fetchProject(w, r, projectId); !ok {
		return
	}
	uid := currentUser(r).ID
	ctx := r.Context()

	facts := domain.MyWorkFacts{UserID: uid, Now: s.now()}

	// 交付物边与输入请求：上游事实、未就绪标记、对接人视角。
	edgeRows, err := s.q.ListEdgesByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w)
		return
	}
	requestRows, err := s.q.ListInputRequestsByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w)
		return
	}
	unreadyNoteByTask := unreadyRequiredInputs(edgeRows, requestRows)
	taskNameByID := map[int64]string{}

	// 任务事实（含显示状态与退回注记）。
	taskRows, err := s.q.ListProjectTasks(ctx, projectId)
	if err != nil {
		writeInternalError(w)
		return
	}
	poolRows, err := s.q.LatestPoolReviewsByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w)
		return
	}
	changeRows, err := s.q.LatestFieldChangesByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w)
		return
	}
	completionRows, err := s.q.LatestCompletionReviewsByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w)
		return
	}
	for _, t := range taskRows {
		taskNameByID[t.ID] = t.Name
		tf := domain.WorkTaskFact{
			ID:            t.ID,
			Name:          t.Name,
			DisplayStatus: domain.DeriveDisplayStatus(t.Status, unreadyNoteByTask[t.ID] != ""),
			OwnerID:       t.OwnerID,
			CreatorID:     t.CreatedBy,
			KrOwnerID:     fromPgInt8(t.KrOwnerID),
			UnreadyNote:   unreadyNoteByTask[t.ID],
		}
		if t.EndDate.Valid {
			end := t.EndDate.Time
			tf.EndDate = &end
		}
		facts.Tasks = append(facts.Tasks, tf)
	}
	// 退回注记与审批件事实。
	taskFactByID := map[int64]*domain.WorkTaskFact{}
	for i := range facts.Tasks {
		taskFactByID[facts.Tasks[i].ID] = &facts.Tasks[i]
	}
	krOwnerOf := func(taskID int64) *int64 {
		if tf, ok := taskFactByID[taskID]; ok {
			return tf.KrOwnerID
		}
		return nil
	}
	// 审批显示文案需要 KR 负责人姓名（AC-04）。
	krOwnerNameByTask := map[int64]string{}
	for _, t := range taskRows {
		krOwnerNameByTask[t.ID] = t.KrOwnerName.String
	}
	for _, pr := range poolRows {
		name := taskNameByID[pr.TaskID]
		switch pr.Status {
		case domain.PoolReviewPending:
			fact := domain.WorkApprovalFact{
				ID: pr.ID, TaskID: pr.TaskID, TaskName: name, SubmittedBy: pr.SubmittedBy,
				KrOwnerID: krOwnerOf(pr.TaskID), KrOwnerName: krOwnerNameByTask[pr.TaskID],
			}
			if pr.SubmittedAt.Valid {
				fact.SubmittedAt = pr.SubmittedAt.Time
			}
			if tf, ok := taskFactByID[pr.TaskID]; ok {
				fact.TaskEnd = tf.EndDate
			}
			facts.PoolReviews = append(facts.PoolReviews, fact)
		case domain.PoolReviewRejected:
			if tf, ok := taskFactByID[pr.TaskID]; ok && tf.DisplayStatus == domain.TaskDraft && pr.Opinion != "" {
				op := pr.Opinion
				tf.PoolRejected = &op
			}
		}
	}
	for _, fc := range changeRows {
		name := taskNameByID[fc.TaskID]
		switch fc.State {
		case domain.FieldChangePendingState:
			fact := domain.WorkApprovalFact{
				ID: fc.ID, TaskID: fc.TaskID, TaskName: name, SubmittedBy: fc.SubmittedBy,
				KrOwnerID: krOwnerOf(fc.TaskID), KrOwnerName: krOwnerNameByTask[fc.TaskID],
			}
			if fc.SubmittedAt.Valid {
				fact.SubmittedAt = fc.SubmittedAt.Time
			}
			if tf, ok := taskFactByID[fc.TaskID]; ok {
				fact.TaskEnd = tf.EndDate
			}
			facts.FieldChanges = append(facts.FieldChanges, fact)
		case domain.FieldChangeRejectedState:
			if !fc.Resolved && fc.SubmittedBy == uid {
				if tf, ok := taskFactByID[fc.TaskID]; ok && fc.Opinion != "" {
					op := "变更已退回：" + fc.Opinion
					tf.FieldChangeRejected = &op
				}
			}
		}
	}
	for _, cr := range completionRows {
		fact := domain.WorkCompletionFact{
			ID: cr.ID, TaskID: cr.TaskID, TaskName: cr.TaskName, SubmittedBy: cr.SubmittedBy,
			TaskOwnerID: cr.TaskOwnerID, KrOwnerID: fromPgInt8(cr.KrOwnerID), State: cr.State,
			KrOwnerName: krOwnerNameByTask[cr.TaskID],
		}
		if cr.SubmittedAt.Valid {
			fact.SubmittedAt = cr.SubmittedAt.Time
		}
		if tf, ok := taskFactByID[cr.TaskID]; ok {
			fact.TaskEnd = tf.EndDate
		}
		if cr.State == domain.CompletionIntermediate {
			rvs, err := s.q.ListReviewReviewers(ctx, cr.ID)
			if err != nil {
				writeInternalError(w)
				return
			}
			for _, rv := range rvs {
				fact.Reviewers = append(fact.Reviewers, rv.UserID)
				fact.ReviewerNames = append(fact.ReviewerNames, rv.DisplayName)
			}
		}
		if cr.State == domain.CompletionRejected && cr.TaskOwnerID == uid && cr.Opinion != "" {
			if tf, ok := taskFactByID[cr.TaskID]; ok && tf.DisplayStatus == domain.TaskInProgress {
				op := "完成申请已退回：" + cr.Opinion
				tf.CompletionRejected = &op
			}
		}
		facts.Completions = append(facts.Completions, fact)
	}

	// 输入请求与上游事实。
	edgeByID := map[int64]int{}
	for i, e := range edgeRows {
		edgeByID[e.ID] = i
	}
	for _, ir := range requestRows {
		fact := domain.WorkInputRequestFact{
			ID: ir.ID, TaskID: ir.TargetTaskID, TaskName: taskNameByID[ir.TargetTaskID],
			ProviderID: ir.ProviderID, State: ir.State, ContentNote: ir.ContentNote,
			Notified: ir.NotifiedAt.Valid,
		}
		if ir.CreatedAt.Valid {
			fact.CreatedAt = ir.CreatedAt.Time
		}
		if idx, ok := edgeByID[ir.EdgeID]; ok {
			e := edgeRows[idx]
			fact.InputName = e.Name
			if e.ExpectedDate.Valid {
				exp := e.ExpectedDate.Time
				fact.Expected = &exp
			}
		}
		if tf, ok := taskFactByID[ir.TargetTaskID]; ok {
			fact.TaskOwnerID = tf.OwnerID
		}
		facts.InputRequests = append(facts.InputRequests, fact)
	}
	for _, e := range edgeRows {
		if e.SourceTaskID.Valid {
			ready := domain.EdgeReady(e.CurrentFileID.Valid, e.HasCandidate)
			fact := domain.WorkUpstreamFact{
				EdgeID: e.ID, TargetTaskID: e.TargetTaskID, TargetName: e.TargetTaskName,
				SourceTaskID: &e.SourceTaskID.Int64, SourceName: e.SourceTaskName.String,
				InputName: e.Name, Ready: ready, Necessity: e.Necessity,
			}
			if tf, ok := taskFactByID[e.TargetTaskID]; ok {
				fact.TargetOwnerID = tf.OwnerID
			}
			facts.Upstreams = append(facts.Upstreams, fact)
		}
	}

	// 邀请与卡点。
	inviteRows, err := s.q.ListProjectTaskInvites(ctx, projectId)
	if err != nil {
		writeInternalError(w)
		return
	}
	krRows, err := s.q.ListKeyResultsByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w)
		return
	}
	krDescByID := map[int64]string{}
	for _, k := range krRows {
		krDescByID[k.ID] = k.Description
	}
	for _, iv := range inviteRows {
		facts.Invites = append(facts.Invites, domain.WorkInviteFact{
			ID: iv.ID, KrDescription: krDescByID[iv.KeyResultID], InviteeID: iv.InviteeID,
			State: iv.State, Note: iv.Note, CreatedAt: iv.CreatedAt.Time,
		})
	}
	blockerRows, err := s.q.ListBlockersByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w)
		return
	}
	for _, b := range blockerRows {
		facts.Blockers = append(facts.Blockers, domain.WorkBlockerFact{
			ID: b.ID, TaskID: b.TaskID, TaskName: b.TaskName, ActionOwnerID: b.ActionOwnerID,
			TaskOwnerID: b.TaskOwnerID, KrOwnerID: fromPgInt8(b.KrOwnerID), State: b.State,
			Kind: b.Kind, Missing: b.Missing, CreatedAt: b.CreatedAt.Time,
		})
	}

	groups := domain.MyWork(facts)
	writeJSON(w, http.StatusOK, MyWork{
		Pending:   toWorkItems(groups.Pending),
		Approvals: toWorkItems(groups.Approvals),
		Receipts:  toWorkItems(groups.Receipts),
		Waiting:   toWorkItems(groups.Waiting),
		Blockers:  toWorkItems(groups.Blockers),
	})
}

func toWorkItems(items []domain.WorkItem) []WorkItem {
	out := make([]WorkItem, 0, len(items))
	for _, it := range items {
		v := WorkItem{
			Kind:    it.Kind,
			Title:   it.Title,
			TaskId:  it.TaskID,
			RefId:   it.RefID,
			Overdue: &it.Overdue,
		}
		if it.TaskName != "" {
			v.TaskName = &it.TaskName
		}
		if it.Due != nil {
			v.DueDate = &openapi_types.Date{Time: *it.Due}
		}
		v.WaitingDays = it.WaitingDays
		if it.RejectedReason != "" {
			v.RejectedReason = &it.RejectedReason
		}
		if it.UnreadyNote != "" {
			v.UnreadyNote = &it.UnreadyNote
		}
		if it.Stage != "" {
			v.Stage = &it.Stage
		}
		if it.DrawerTab != "" {
			v.DrawerTab = &it.DrawerTab
		}
		out = append(out, v)
	}
	return out
}
