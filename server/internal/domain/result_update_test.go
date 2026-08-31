package domain

import (
	"errors"
	"testing"
)

// 成果更新的发起规则（AC-66、§5.1、§5.3）：已完成任务唯一接受的审批单，
// 发起人限任务负责人与可编辑项目者，与其他未决审批单互斥，已关闭任务永不可发起。
func TestStartResultUpdateRule(t *testing.T) {
	krOwner := int64(7)
	completed := TaskFacts{Status: TaskCompleted, CreatorID: 3, OwnerID: 5, KrOwnerID: &krOwner}
	cases := []struct {
		name             string
		actor            Actor
		user             int64
		facts            TaskFacts
		hasPendingChange bool
		want             error
	}{
		{"负责人可发起", Actor{Role: RoleMember}, 5, completed, false, nil},
		{"管理员可代发起", Actor{Role: RoleAdmin}, 9, completed, false, nil},
		{"项目负责人可代发起", Actor{IsOwner: true}, 9, completed, false, nil},
		{"无关成员不可发起", Actor{Role: RoleMember}, 9, completed, false, ErrResultUpdateForbidden},
		{"访客不可发起", Actor{Role: RoleViewer}, 5, completed, false, ErrResultUpdateForbidden},
		{"创建人不是负责人也不可发起", Actor{Role: RoleMember}, 3, completed, false, ErrResultUpdateForbidden},
		{"进行中任务不可发起", Actor{Role: RoleMember}, 5, TaskFacts{Status: TaskInProgress, OwnerID: 5, KrOwnerID: &krOwner}, false, ErrResultUpdateNotCompleted},
		{"已关闭任务不可发起", Actor{Role: RoleMember}, 5, TaskFacts{Status: TaskCancelled, OwnerID: 5, KrOwnerID: &krOwner}, false, ErrResultUpdateNotCompleted},
		{"已有未提交的成果更新不可再发起", Actor{Role: RoleMember}, 5, withResultUpdate(completed, ResultUpdateOpen), false, ErrResultUpdateExists},
		{"审批中的成果更新不可再发起", Actor{Role: RoleMember}, 5, withResultUpdate(completed, ResultUpdateReviewing), false, ErrResultUpdateExists},
		{"存在未决变更单时互斥", Actor{Role: RoleMember}, 5, completed, true, ErrResultUpdatePendingExists},
		{"KR 无负责人时无人终审", Actor{Role: RoleMember}, 5, TaskFacts{Status: TaskCompleted, OwnerID: 5}, false, ErrKrOwnerMissing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := StartResultUpdateRule(tc.actor, tc.user, tc.facts, tc.hasPendingChange)
			if !errors.Is(err, tc.want) {
				t.Fatalf("StartResultUpdateRule = %v, want %v", err, tc.want)
			}
			if got := CanStartResultUpdate(tc.actor, tc.user, tc.facts, tc.hasPendingChange); got != (tc.want == nil) {
				t.Fatalf("CanStartResultUpdate = %v, want %v", got, tc.want == nil)
			}
		})
	}
}

// 已完成任务的候选上传：只在存在未提交的成果更新时放行（AC-66；审核期间整批候选锁定，§5.3）。
func TestCanUploadCandidateUnderResultUpdate(t *testing.T) {
	completed := TaskFacts{Status: TaskCompleted, CreatorID: 3, OwnerID: 5}
	cases := []struct {
		name  string
		user  int64
		facts TaskFacts
		want  bool
	}{
		{"未发起成果更新不可传", 5, completed, false},
		{"已发起成果更新可传", 5, withResultUpdate(completed, ResultUpdateOpen), true},
		{"成果更新审核中不可另传", 5, withResultUpdate(completed, ResultUpdateReviewing), false},
		{"非负责人不可传", 9, withResultUpdate(completed, ResultUpdateOpen), false},
		{"已关闭任务不可传", 5, withResultUpdate(TaskFacts{Status: TaskCancelled, OwnerID: 5}, ResultUpdateOpen), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanUploadCandidate(Actor{Role: RoleMember}, tc.user, tc.facts); got != tc.want {
				t.Fatalf("CanUploadCandidate = %v, want %v", got, tc.want)
			}
		})
	}
}

// 成果更新走同一道完成审批：提交时任务状态保持已完成，不回退生命周期（§5.1、AC-66）。
func TestSubmitCompletionUnderResultUpdate(t *testing.T) {
	krOwner := int64(7)
	completed := TaskFacts{Status: TaskCompleted, OwnerID: 5, KrOwnerID: &krOwner}
	if err := SubmitCompletionRule(withResultUpdate(completed, ResultUpdateOpen), 1, "更新说明"); err != nil {
		t.Fatalf("已发起成果更新应可提交完成申请: %v", err)
	}
	if err := SubmitCompletionRule(completed, 1, "更新说明"); !errors.Is(err, ErrCompletionNotInProgress) {
		t.Fatalf("未发起成果更新不应可提交: %v", err)
	}
	if err := SubmitCompletionRule(withResultUpdate(completed, ResultUpdateReviewing), 1, "更新说明"); !errors.Is(err, ErrCompletionNotInProgress) {
		t.Fatalf("审核中不应可重复提交: %v", err)
	}
	if err := SubmitCompletionRule(withResultUpdate(completed, ResultUpdateOpen), 0, "更新说明"); !errors.Is(err, ErrNoCandidates) {
		t.Fatalf("无候选内容不应可提交: %v", err)
	}
	cases := []struct {
		name         string
		reviewers    int
		resultUpdate bool
		wantReview   string
		wantStatus   string
	}{
		{"首次定稿无成果审核", 0, false, CompletionPendingFinal, TaskPendingFinalReview},
		{"首次定稿有成果审核", 2, false, CompletionIntermediate, TaskPendingIntermediateReview},
		{"成果更新无成果审核仍为已完成", 0, true, CompletionPendingFinal, TaskCompleted},
		{"成果更新有成果审核仍为已完成", 2, true, CompletionIntermediate, TaskCompleted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			review, status := SubmitCompletionOutcome(tc.reviewers, tc.resultUpdate)
			if review != tc.wantReview || status != tc.wantStatus {
				t.Fatalf("SubmitCompletionOutcome = (%s, %s), want (%s, %s)", review, status, tc.wantReview, tc.wantStatus)
			}
		})
	}
}

// 成果更新的终审与或签：处理人口径不变，处理结果不改变任务生命周期状态（AC-66）。
func TestDecideCompletionUnderResultUpdate(t *testing.T) {
	krOwner := int64(7)
	reviewing := withResultUpdate(TaskFacts{Status: TaskCompleted, OwnerID: 5, KrOwnerID: &krOwner}, ResultUpdateReviewing)
	admin := Actor{Role: RoleAdmin}
	member := Actor{Role: RoleMember}

	status, err := DecideCompletionRule(member, reviewing, krOwner, true, "")
	if err != nil || status != TaskCompleted {
		t.Fatalf("成果更新终审通过应保持已完成: (%s, %v)", status, err)
	}
	status, err = DecideCompletionRule(member, reviewing, krOwner, false, "内容不完整")
	if err != nil || status != TaskCompleted {
		t.Fatalf("成果更新终审退回应保持已完成: (%s, %v)", status, err)
	}
	if _, err := DecideCompletionRule(admin, reviewing, 9, true, ""); !errors.Is(err, ErrNotKrOwner) {
		t.Fatalf("非 KR 负责人不应可终审: %v", err)
	}
	if _, err := DecideCompletionRule(member, TaskFacts{Status: TaskCompleted, OwnerID: 5, KrOwnerID: &krOwner}, krOwner, true, ""); !errors.Is(err, ErrCompletionNotPending) {
		t.Fatalf("没有在审的成果更新时不应可终审: %v", err)
	}

	isReviewer := func(id int64) bool { return id == 11 }
	taskStatus, reviewState, err := DecideIntermediateRule(member, reviewing, 11, isReviewer, true, "")
	if err != nil || taskStatus != TaskCompleted || reviewState != CompletionPendingFinal {
		t.Fatalf("成果更新或签通过 = (%s, %s, %v)", taskStatus, reviewState, err)
	}
	taskStatus, reviewState, err = DecideIntermediateRule(member, reviewing, 11, isReviewer, false, "重做")
	if err != nil || taskStatus != TaskCompleted || reviewState != CompletionRejected {
		t.Fatalf("成果更新或签退回 = (%s, %s, %v)", taskStatus, reviewState, err)
	}
}

// 审核中的成果更新属未决审批单：与其他审批单互斥（§5.1）。
func TestPendingApprovalCountsResultUpdate(t *testing.T) {
	completed := TaskFacts{Status: TaskCompleted, OwnerID: 5}
	if PendingApprovalOnTask(completed, false) {
		t.Fatal("已完成且无成果更新时不应算未决审批")
	}
	if !PendingApprovalOnTask(withResultUpdate(completed, ResultUpdateReviewing), false) {
		t.Fatal("成果更新审核中应算未决审批")
	}
}

func withResultUpdate(t TaskFacts, state string) TaskFacts {
	t.ResultUpdate = state
	return t
}
