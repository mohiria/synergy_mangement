package api

import (
	"net/http"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"synergy/server/internal/domain"
)

// 我的工作五分组（AC-16）。事实装配在此，分组规则在 domain.MyWork。

func (s *Server) GetMyWork(w http.ResponseWriter, r *http.Request, projectId int64) {
	me := currentUser(r)
	uid := me.ID
	ctx := r.Context()

	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	// 一键提醒当日配额（#129）：一次取回本人今天的提醒计数，canRemind 显隐把配额算进去。
	remindCounts, err := s.remindCountsToday(ctx, uid)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	// 审批件超期标红的阈值取项目规则设置，与「审批超时」卡点同源（R12、AC-60）。
	facts := domain.MyWorkFacts{
		UserID: uid, Actor: projectActor(uid, proj.OwnerID, proj.MyRole, proj.Visibility), Now: s.now(),
		ApprovalTimeoutDays: projectSettingsOf(proj).ApprovalTimeoutDays,
		RemindDailyLimit:    projectSettingsOf(proj).RemindDailyLimit,
		RemindSentToday:     remindCounts,
	}

	// 交付物边与输入请求：上游事实、未就绪标记、对接人视角。
	edgeRows, err := s.q.ListEdgesByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	requestRows, err := s.q.ListInputRequestsByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	unreadyNoteByTask := unreadyRequiredInputs(edgeRows, requestRows)
	taskNameByID := map[int64]string{}

	// 任务事实（含显示状态与退回注记）。
	taskRows, err := s.q.ListProjectTasks(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	changeRows, err := s.q.LatestFieldChangesByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	completionRows, err := s.q.LatestCompletionReviewsByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
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
	for _, fc := range changeRows {
		name := taskNameByID[fc.TaskID]
		switch fc.State {
		case domain.FieldChangePendingState:
			fact := domain.WorkApprovalFact{
				ID: fc.ID, TaskID: fc.TaskID, TaskName: name, SubmittedBy: fc.SubmittedBy,
				KrOwnerID: krOwnerOf(fc.TaskID), KrOwnerName: krOwnerNameByTask[fc.TaskID],
				ChangeType: fc.ChangeType,
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
				writeInternalError(w, r, err)
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
		// 必要性取所在交付物边：参考输入不进任何分组、不计入徽标（模块 PRD §4.2 规则 8）。
		if ir.CreatedAt.Valid {
			fact.CreatedAt = ir.CreatedAt.Time
		}
		if idx, ok := edgeByID[ir.EdgeID]; ok {
			e := edgeRows[idx]
			fact.InputName = e.Name
			fact.Necessity = e.Necessity
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
			ready := domain.EdgeReady(e.SourceTaskStatus.String)
			fact := domain.WorkUpstreamFact{
				EdgeID: e.ID, TargetTaskID: e.TargetTaskID, TargetName: e.TargetTaskName,
				SourceTaskID: &e.SourceTaskID.Int64, SourceName: e.SourceTaskName.String,
				SourceOwnerID: e.SourceOwnerID.Int64, SourceOwnerName: e.SourceOwnerName.String,
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
		writeInternalError(w, r, err)
		return
	}
	krRows, err := s.q.ListKeyResultsByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	krDescByID := map[int64]string{}
	for _, k := range krRows {
		krDescByID[k.ID] = k.Description
	}
	hasPendingInvite := false
	for _, iv := range inviteRows {
		if iv.InviteeID == uid && iv.State == domain.TaskInvitePending {
			hasPendingInvite = true
		}
		facts.Invites = append(facts.Invites, domain.WorkInviteFact{
			ID: iv.ID, KrDescription: krDescByID[iv.KeyResultID], InviteeID: iv.InviteeID,
			State: iv.State, Note: iv.Note, CreatedAt: iv.CreatedAt.Time,
		})
	}
	// 待接收项与接收记录（MW-09）：分组只按「本人且未确认」筛选，名单在终审通过时已落库。
	receiptRows, err := s.q.ListReceiptsByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	for _, rc := range receiptRows {
		fact := domain.ReceiptFact{
			ID: rc.ID, TaskID: rc.TaskID, TaskName: rc.TaskName,
			UserID: rc.UserID, UserName: rc.DisplayName,
		}
		if rc.GeneratedAt.Valid {
			fact.GeneratedAt = rc.GeneratedAt.Time
		}
		if rc.ConfirmedAt.Valid {
			at := rc.ConfirmedAt.Time
			fact.ConfirmedAt = &at
		}
		facts.Receipts = append(facts.Receipts, fact)
	}
	blockers, err := s.projectBlockers(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	facts.Blockers = blockers

	// 身份卡（模块 PRD §3.1）：职责事实与「移出成员前必须交接」同源，见 memberDuties。
	duties, err := s.memberDuties(ctx, projectId, uid)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	responsibilities := domain.WorkResponsibilities(duties, proj.OwnerID == uid, hasPendingInvite)
	identity := WorkIdentity{
		UserId:                uid,
		Username:              me.Username,
		DisplayName:           me.DisplayName,
		RoleLabel:             domain.WorkIdentityRoleLabel(proj.MyRole.String, proj.OwnerID == uid),
		Responsibilities:      responsibilities,
		ResponsibilitiesLabel: domain.WorkResponsibilitiesLabel(responsibilities),
	}
	if proj.MyRole.Valid {
		role := MemberRole(proj.MyRole.String)
		identity.Role = &role
	}

	groups := domain.MyWork(facts)
	writeJSON(w, http.StatusOK, MyWork{
		Identity:  identity,
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
			Kind:        it.Kind,
			Title:       it.Title,
			TaskId:      it.TaskID,
			RefId:       it.RefID,
			Overdue:     &it.Overdue,
			ActionLabel: it.ActionLabel,
			CanRemind:   it.CanRemind,
		}
		if it.TaskName != "" {
			v.TaskName = &it.TaskName
		}
		if it.Due != nil {
			v.DueDate = &openapi_types.Date{Time: *it.Due}
		}
		if it.RefKey != "" {
			v.RefKey = &it.RefKey
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
