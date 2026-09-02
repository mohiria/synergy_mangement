package domain

import (
	"errors"
	"testing"
)

// AC-13／§9.1：提交完成申请——负责人在进行中提交，须有候选内容与提交说明；
// 裁决 11（#181）：终审人为项目管理员集合，「KR 无负责人则无人可审批」校验退场。
func TestSubmitCompletionRule(t *testing.T) {
	facts := func(status string) TaskFacts {
		return TaskFacts{Status: status, CreatorID: 3, OwnerID: 5, KrOwnerID: i64(7)}
	}
	cases := []struct {
		name       string
		t          TaskFacts
		candidates int
		note       string
		want       error
	}{
		{"进行中有候选可提交", facts(TaskInProgress), 2, "第一批成果", nil},
		{"无候选不可提交", facts(TaskInProgress), 0, "x", ErrNoCandidates},
		{"提交说明必填", facts(TaskInProgress), 1, "  ", ErrCompletionNoteRequired},
		{"未开始不可提交", facts(TaskNotStarted), 1, "x", ErrCompletionNotInProgress},
		{"已在终审不可重复提交", facts(TaskPendingFinalReview), 1, "x", ErrCompletionNotInProgress},
		{"KR 无负责人也可提交（裁决 11）", TaskFacts{Status: TaskInProgress, OwnerID: 5}, 1, "x", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SubmitCompletionRule(tc.t, tc.candidates, tc.note); !errors.Is(got, tc.want) {
				t.Fatalf("SubmitCompletionRule() = %v, want %v", got, tc.want)
			}
		})
	}
}

// 提交人：任务负责人（管理员／项目负责人可纠错）。
func TestCanSubmitCompletion(t *testing.T) {
	facts := TaskFacts{Status: TaskInProgress, CreatorID: 3, OwnerID: 5, KrOwnerID: i64(7)}
	if !CanSubmitCompletion(Actor{Role: RoleMember}, 5, facts, 1) {
		t.Fatal("负责人应可提交")
	}
	if CanSubmitCompletion(Actor{Role: RoleMember}, 3, facts, 1) {
		t.Fatal("创建人非负责人不应可提交")
	}
	if !CanSubmitCompletion(Actor{Role: RoleAdmin}, 9, facts, 1) {
		t.Fatal("管理员应可提交（纠错）")
	}
	if CanSubmitCompletion(Actor{Role: RoleMember}, 5, facts, 0) {
		t.Fatal("无候选不应可提交")
	}
}

// AC-15／38（裁决 11 #181 修订）：终审人为项目管理员集合（含项目负责人）或签，
// 任一人通过→已完成，退回→进行中且退回意见必填；KR 负责人与任务负责人不再可终审。
func TestDecideCompletionRule(t *testing.T) {
	pending := TaskFacts{Status: TaskPendingFinalReview, CreatorID: 3, OwnerID: 5, KrOwnerID: i64(7)}
	admin := Actor{Role: RoleAdmin}
	owner := Actor{IsOwner: true}
	member := Actor{Role: RoleMember}
	cases := []struct {
		name       string
		actor      Actor
		t          TaskFacts
		approve    bool
		opinion    string
		wantStatus string
		wantErr    error
	}{
		{"项目管理员通过（意见选填）", admin, pending, true, "", TaskCompleted, nil},
		{"项目负责人通过", owner, pending, true, "", TaskCompleted, nil},
		{"管理员退回需意见", admin, pending, false, "", "", ErrRejectOpinionRequired},
		{"管理员退回回进行中", admin, pending, false, "验收样例不足", TaskInProgress, nil},
		{"KR 负责人（成员）不可终审", member, pending, true, "", "", ErrNotFinalReviewer},
		{"任务负责人不可自审", member, pending, true, "", "", ErrNotFinalReviewer},
		{"访客不可终审", Actor{Role: RoleViewer}, pending, true, "", "", ErrNotFinalReviewer},
		{"非待终审状态冲突", admin, TaskFacts{Status: TaskInProgress, KrOwnerID: i64(7)}, true, "", "", ErrCompletionNotPending},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, err := DecideCompletionRule(tc.actor, tc.t, tc.approve, tc.opinion)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("DecideCompletionRule() err = %v, want %v", err, tc.wantErr)
			}
			if status != tc.wantStatus {
				t.Fatalf("DecideCompletionRule() status = %q, want %q", status, tc.wantStatus)
			}
		})
	}
}
