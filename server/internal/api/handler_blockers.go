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

// 结构化卡点（AC-11）：四类结构化事实读时派生，没有上报与手动解除；
// 派生规则在 domain.DeriveBlockers，本文件只装配事实与编排提醒。

func (s *Server) ListBlockers(w http.ResponseWriter, r *http.Request, projectId int64) {
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(uid, proj.OwnerID, proj.MyRole)
	blockers, err := s.projectBlockers(r.Context(), projectId)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, blockerViews(blockers, actor, uid))
}

func (s *Server) RemindBlocker(w http.ResponseWriter, r *http.Request, projectId int64) {
	var req RemindBlockerRequest
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
	blockers, err := s.projectBlockers(r.Context(), projectId)
	if err != nil {
		writeInternalError(w)
		return
	}
	key := strings.TrimSpace(req.Key)
	var target *domain.Blocker
	for i := range blockers {
		if blockers[i].Key == key {
			target = &blockers[i]
			break
		}
	}
	// 触发条件已消失的卡点自动解除，按不存在处理。
	if target == nil {
		writeJSON(w, http.StatusNotFound, Error{Code: "blocker_not_found", Message: "卡点已不存在或已自动解除"})
		return
	}
	if !domain.CanRemindBlocker(actor, uid, *target) {
		writeForbidden(w)
		return
	}
	// AC-11：提醒带上任务、缺失条件与影响范围。
	content := fmt.Sprintf("任务「%s」的卡点提醒：缺「%s」（%s）", target.TaskName, target.Missing, target.Reason)
	if target.ImpactNote != "" {
		content += "；" + target.ImpactNote
	}
	for _, ownerID := range target.ActionOwnerIDs {
		if _, err := s.q.CreateNotification(r.Context(), blockerRemindNotification(ownerID, projectId, target.TaskID, content)); err != nil {
			writeInternalError(w)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// projectBlockers 装配四类结构化事实并派生本项目当前全部卡点。
func (s *Server) projectBlockers(ctx context.Context, projectID int64) ([]domain.Blocker, error) {
	taskRows, err := s.q.ListProjectTasks(ctx, projectID)
	if err != nil {
		return nil, err
	}
	edgeRows, err := s.q.ListEdgesByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	requestRows, err := s.q.ListInputRequestsByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	poolRows, err := s.q.LatestPoolReviewsByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	changeRows, err := s.q.LatestFieldChangesByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	completionRows, err := s.q.LatestCompletionReviewsByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}

	facts := domain.BlockerFacts{Now: s.now()}
	krOwnerNameByTask := make(map[int64]string, len(taskRows))
	for _, t := range taskRows {
		krOwnerNameByTask[t.ID] = t.KrOwnerName.String
		tf := domain.BlockerTaskFact{
			ID: t.ID, Name: t.Name, Status: t.Status,
			OwnerID: t.OwnerID, OwnerName: t.OwnerName,
			KrID: t.KeyResultID, KrOwnerID: fromPgInt8(t.KrOwnerID), KrOwnerName: t.KrOwnerName.String,
		}
		if t.StartDate.Valid {
			start := t.StartDate.Time
			tf.StartDate = &start
		}
		if t.EndDate.Valid {
			end := t.EndDate.Time
			tf.EndDate = &end
		}
		facts.Tasks = append(facts.Tasks, tf)
	}

	// 输入就绪：来源为上游任务时看当前内容是否生效，来源为指定成员时看输入请求是否已提供。
	requestByEdge := make(map[int64]store.ListInputRequestsByProjectRow, len(requestRows))
	for _, ir := range requestRows {
		requestByEdge[ir.EdgeID] = ir
	}
	for _, e := range edgeRows {
		in := domain.BlockerInputFact{
			EdgeID: e.ID, TargetTaskID: e.TargetTaskID, InputName: e.Name,
			Necessity: e.Necessity, Ready: domain.EdgeReady(e.CurrentFileID.Valid, e.HasCandidate),
		}
		if e.SourceTaskID.Valid {
			src := e.SourceTaskID.Int64
			in.SourceTaskID = &src
			in.SourceTaskName = e.SourceTaskName.String
			in.SourceOwnerID = e.SourceOwnerID.Int64
			in.SourceOwnerName = e.SourceOwnerName.String
		}
		if ir, ok := requestByEdge[e.ID]; ok {
			in.Ready = domain.MemberEdgeReady(ir.State)
			in.ProviderID = ir.ProviderID
			in.ProviderName = ir.ProviderName
		}
		facts.Inputs = append(facts.Inputs, in)
		if e.EdgeType == domain.EdgeHardPrerequisite && e.SourceTaskID.Valid {
			facts.HardEdges = append(facts.HardEdges, domain.HardEdge{
				ID: e.ID, Source: e.SourceTaskID.Int64, Target: e.TargetTaskID,
			})
		}
	}

	// 停在当前环节的审批件：入池、关键字段变更、中间或签、KR 终审。
	for _, pr := range poolRows {
		if pr.Status != domain.PoolReviewPending {
			continue
		}
		facts.Approvals = append(facts.Approvals, domain.BlockerApprovalFact{
			Kind: "pool_review", RefID: pr.ID, TaskID: pr.TaskID,
			StageSince:  pr.SubmittedAt.Time,
			ApproverIDs: approverIDs(taskRows, pr.TaskID), ApproverNames: []string{krOwnerNameByTask[pr.TaskID]},
		})
	}
	for _, fc := range changeRows {
		if fc.State != domain.FieldChangePendingState {
			continue
		}
		facts.Approvals = append(facts.Approvals, domain.BlockerApprovalFact{
			Kind: "field_change", RefID: fc.ID, TaskID: fc.TaskID,
			StageSince:  fc.SubmittedAt.Time,
			ApproverIDs: approverIDs(taskRows, fc.TaskID), ApproverNames: []string{krOwnerNameByTask[fc.TaskID]},
		})
	}
	for _, cr := range completionRows {
		switch cr.State {
		case domain.CompletionIntermediate:
			rvs, err := s.q.ListReviewReviewers(ctx, cr.ID)
			if err != nil {
				return nil, err
			}
			fact := domain.BlockerApprovalFact{
				Kind: "intermediate_review", RefID: cr.ID, TaskID: cr.TaskID,
				StageSince: cr.SubmittedAt.Time,
			}
			for _, rv := range rvs {
				fact.ApproverIDs = append(fact.ApproverIDs, rv.UserID)
				fact.ApproverNames = append(fact.ApproverNames, rv.DisplayName)
			}
			facts.Approvals = append(facts.Approvals, fact)
		case domain.CompletionPendingFinal:
			// 进入新环节重新计时：或签通过的时点即终审环节起点。
			since := cr.SubmittedAt.Time
			if cr.IntermediateAt.Valid {
				since = cr.IntermediateAt.Time
			}
			facts.Approvals = append(facts.Approvals, domain.BlockerApprovalFact{
				Kind: "final_review", RefID: cr.ID, TaskID: cr.TaskID, StageSince: since,
				ApproverIDs: approverIDs(taskRows, cr.TaskID), ApproverNames: []string{krOwnerNameByTask[cr.TaskID]},
			})
		}
	}

	return domain.DeriveBlockers(facts), nil
}

// approverIDs 取任务所属 KR 负责人（入池、关键字段变更与终审的审批人）。
func approverIDs(tasks []store.ListProjectTasksRow, taskID int64) []int64 {
	for _, t := range tasks {
		if t.ID == taskID && t.KrOwnerID.Valid {
			return []int64{t.KrOwnerID.Int64}
		}
	}
	return nil
}

func blockerViews(bs []domain.Blocker, actor domain.Actor, userID int64) []Blocker {
	out := make([]Blocker, 0, len(bs))
	for _, b := range bs {
		out = append(out, blockerView(b, actor, userID))
	}
	return out
}

func blockerView(b domain.Blocker, actor domain.Actor, userID int64) Blocker {
	canRemind := domain.CanRemindBlocker(actor, userID, b)
	item := Blocker{
		Key:              b.Key,
		Kind:             BlockerKind(b.Kind),
		TaskId:           b.TaskID,
		TaskName:         b.TaskName,
		Missing:          b.Missing,
		Reason:           b.Reason,
		ActionOwnerIds:   append([]int64{}, b.ActionOwnerIDs...),
		ActionOwnerNames: append([]string{}, b.ActionOwnerNames...),
		Level:            RiskLevel(b.Level),
		Since:            b.Since,
		CanRemind:        &canRemind,
	}
	item.ImpactNote = optString(b.ImpactNote)
	return item
}

func blockerRemindNotification(userID, projectID, taskID int64, content string) store.CreateNotificationParams {
	return store.CreateNotificationParams{
		UserID:    userID,
		Kind:      domain.NotifyBlockerRemind,
		Content:   content,
		ProjectID: pgtype.Int8{Int64: projectID, Valid: true},
		TaskID:    pgtype.Int8{Int64: taskID, Valid: true},
	}
}
