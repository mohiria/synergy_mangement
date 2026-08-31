package domain

import (
	"errors"
	"testing"
)

// §5.4：成果审核人配置——非只读项目成员；配置调整在审核中与终态不可。
func TestValidateReviewers(t *testing.T) {
	roles := map[int64]string{3: RoleMember, 4: RoleAdmin, 5: RoleViewer}
	roleOf := func(id int64) string { return roles[id] }
	if err := ValidateReviewers([]int64{3, 4}, roleOf); err != nil {
		t.Fatalf("合法配置被拒: %v", err)
	}
	if err := ValidateReviewers(nil, roleOf); err != nil {
		t.Fatalf("空配置（不设成果审核）应合法: %v", err)
	}
	if err := ValidateReviewers([]int64{5}, roleOf); !errors.Is(err, ErrReviewerNotEligible) {
		t.Fatalf("访客应被拒: %v", err)
	}
	if err := ValidateReviewers([]int64{99}, roleOf); !errors.Is(err, ErrReviewerNotEligible) {
		t.Fatalf("非成员应被拒: %v", err)
	}
}

func TestCanManageReviewers(t *testing.T) {
	facts := TaskFacts{Status: TaskInProgress, CreatorID: 3, OwnerID: 5}
	if !CanManageReviewers(Actor{Role: RoleMember}, 5, facts) {
		t.Fatal("负责人应可配置成果审核人")
	}
	if !CanManageReviewers(Actor{Role: RoleAdmin}, 9, facts) {
		t.Fatal("管理员应可配置")
	}
	if CanManageReviewers(Actor{Role: RoleMember}, 9, facts) {
		t.Fatal("无关成员不应可配置")
	}
	if CanManageReviewers(Actor{Role: RoleMember}, 5, TaskFacts{Status: TaskPendingIntermediateReview, OwnerID: 5}) {
		t.Fatal("审核中不应可调整")
	}
	if CanManageReviewers(Actor{Role: RoleMember}, 5, TaskFacts{Status: TaskCompleted, OwnerID: 5}) {
		t.Fatal("终态不应可调整")
	}
}

// AC-13／AC-14 路由：无成果审核人直接待 KR 终审，有则进入中间或签。
func TestSubmitCompletionOutcome(t *testing.T) {
	state, status := SubmitCompletionOutcome(0, false)
	if state != CompletionPendingFinal || status != TaskPendingFinalReview {
		t.Fatalf("无审核人应直接待终审: %q %q", state, status)
	}
	state, status = SubmitCompletionOutcome(2, false)
	if state != CompletionIntermediate || status != TaskPendingIntermediateReview {
		t.Fatalf("有审核人应进入中间或签: %q %q", state, status)
	}
}

// AC-14／AC-24／AC-37：或签——任一审核人通过进入待 KR 终审；任一人退回整体退回（意见必填）；
// 非或签组成员（含 KR 负责人、管理员）不可处理。
func TestDecideIntermediateRule(t *testing.T) {
	facts := TaskFacts{Status: TaskPendingIntermediateReview, CreatorID: 3, OwnerID: 5, KrOwnerID: i64(7)}
	reviewers := map[int64]bool{11: true, 12: true}
	isReviewer := func(id int64) bool { return reviewers[id] }
	cases := []struct {
		name       string
		t          TaskFacts
		actor      int64
		approve    bool
		opinion    string
		wantTask   string
		wantReview string
		wantErr    error
	}{
		{"任一审核人通过进入待终审", facts, 11, true, "", TaskPendingFinalReview, CompletionPendingFinal, nil},
		{"任一审核人退回整体退回", facts, 12, false, "口径不一致", TaskInProgress, CompletionRejected, nil},
		{"退回意见必填", facts, 11, false, " ", "", "", ErrRejectOpinionRequired},
		{"KR 负责人非组员不可处理", facts, 7, true, "", "", "", ErrNotReviewer},
		{"任务负责人非组员不可处理", facts, 5, true, "", "", "", ErrNotReviewer},
		{"非成果审核状态冲突", TaskFacts{Status: TaskPendingFinalReview, KrOwnerID: i64(7)}, 11, true, "", "", "", ErrCompletionNotIntermediate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			taskStatus, reviewState, err := DecideIntermediateRule(Actor{Role: RoleMember}, tc.t, tc.actor, isReviewer, tc.approve, tc.opinion)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if taskStatus != tc.wantTask || reviewState != tc.wantReview {
				t.Fatalf("= (%q,%q), want (%q,%q)", taskStatus, reviewState, tc.wantTask, tc.wantReview)
			}
		})
	}
}
