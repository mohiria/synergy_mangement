package domain

import (
	"errors"
	"testing"
)

// AC-13／§9.1：提交完成申请——负责人在进行中提交，须有候选内容与提交说明，KR 须有负责人。
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
		{"KR 无负责人不可提交", TaskFacts{Status: TaskInProgress, OwnerID: 5}, 1, "x", ErrKrOwnerMissing},
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

// AC-15／38：终审仅 KR 负责人处理；通过→已完成，退回→进行中且退回意见必填。
func TestDecideCompletionRule(t *testing.T) {
	pending := TaskFacts{Status: TaskPendingFinalReview, CreatorID: 3, OwnerID: 5, KrOwnerID: i64(7)}
	cases := []struct {
		name       string
		t          TaskFacts
		actor      int64
		approve    bool
		opinion    string
		wantStatus string
		wantErr    error
	}{
		{"KR 负责人通过（意见选填）", pending, 7, true, "", TaskCompleted, nil},
		{"KR 负责人退回需意见", pending, 7, false, "", "", ErrRejectOpinionRequired},
		{"KR 负责人退回回进行中", pending, 7, false, "验收样例不足", TaskInProgress, nil},
		{"非 KR 负责人不可终审", pending, 9, true, "", "", ErrNotKrOwner},
		{"任务负责人不可自审", pending, 5, true, "", "", ErrNotKrOwner},
		{"非待终审状态冲突", TaskFacts{Status: TaskInProgress, KrOwnerID: i64(7)}, 7, true, "", "", ErrCompletionNotPending},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, err := DecideCompletionRule(Actor{Role: RoleMember}, tc.t, tc.actor, tc.approve, tc.opinion)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("DecideCompletionRule() err = %v, want %v", err, tc.wantErr)
			}
			if status != tc.wantStatus {
				t.Fatalf("DecideCompletionRule() status = %q, want %q", status, tc.wantStatus)
			}
		})
	}
}
