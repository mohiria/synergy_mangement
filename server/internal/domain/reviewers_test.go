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
	if CanManageReviewers(Actor{Role: RoleMember}, 5, TaskFacts{Status: TaskInReview, OwnerID: 5}) {
		t.Fatal("审核中不应可调整")
	}
	if CanManageReviewers(Actor{Role: RoleMember}, 5, TaskFacts{Status: TaskCompleted, OwnerID: 5}) {
		t.Fatal("终态不应可调整")
	}
}

// AC-13／AC-14 路由：无成果审核人直接待 KR 终审，有则进入中间或签。
func TestSubmitCompletionOutcome(t *testing.T) {
	state, status := SubmitCompletionOutcome(0, false)
	if state != CompletionPendingFinal || status != TaskInReview {
		t.Fatalf("无审核人应直接待终审: %q %q", state, status)
	}
	state, status = SubmitCompletionOutcome(2, false)
	if state != CompletionIntermediate || status != TaskInReview {
		t.Fatalf("有审核人应进入中间或签: %q %q", state, status)
	}
}

// AC-14／AC-24／AC-37：或签——任一审核人通过进入待终审；任一人退回整体退回（意见必填）；
// 非或签组成员（含 KR 负责人、管理员）不可处理。
// 裁决 C2（#136，裁决 11 #181 改写）：或签组**包含项目管理员**时，管理员通过即视同终审通过
// 一次闭环；其他审核人通过仍进待终审；退回路径不变。
func TestDecideIntermediateRule(t *testing.T) {
	facts := TaskFacts{Status: TaskInReview, CreatorID: 3, OwnerID: 5}
	member := Actor{Role: RoleMember}
	admin := Actor{Role: RoleAdmin}
	defaultGroup := map[int64]bool{11: true, 12: true}
	withAdmin := map[int64]bool{9: true, 11: true}
	onlyAdmin := map[int64]bool{9: true}
	cases := []struct {
		name       string
		actor      Actor
		t          TaskFacts
		group      map[int64]bool
		actorID    int64
		approve    bool
		opinion    string
		wantTask   string
		wantReview string
		wantErr    error
	}{
		{"任一审核人通过进入待终审", member, facts, nil, 11, true, "", TaskInReview, CompletionPendingFinal, nil},
		{"任一审核人退回整体退回", member, facts, nil, 12, false, "口径不一致", TaskInProgress, CompletionRejected, nil},
		{"退回意见必填", member, facts, nil, 11, false, " ", "", "", ErrRejectOpinionRequired},
		{"KR 负责人非组员不可处理", member, facts, nil, 7, true, "", "", "", ErrNotReviewer},
		{"任务负责人非组员不可处理", member, facts, nil, 5, true, "", "", "", ErrNotReviewer},
		{"管理员非组员不可处理", admin, facts, nil, 9, true, "", "", "", ErrNotReviewer},
		// 裁决 13：环节由申请单把关，状态冲突用非审核中状态验证。
		{"非审核中状态冲突", member, TaskFacts{Status: TaskInProgress}, nil, 11, true, "", "", "", ErrCompletionNotIntermediate},
		{"KR 负责人在组内通过仅进待终审（C2 改写）", member, facts, map[int64]bool{7: true, 11: true}, 7, true, "", TaskInReview, CompletionPendingFinal, nil},
		{"组含管理员：其通过即视同终审通过", admin, facts, withAdmin, 9, true, "", TaskCompleted, CompletionApproved, nil},
		{"组含管理员：其他审核人通过仍待终审", member, facts, withAdmin, 11, true, "", TaskInReview, CompletionPendingFinal, nil},
		{"组含管理员：其退回仍整体退回", admin, facts, withAdmin, 9, false, "口径不一致", TaskInProgress, CompletionRejected, nil},
		{"组只有管理员：一次通过即闭环", admin, facts, onlyAdmin, 9, true, "", TaskCompleted, CompletionApproved, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			group := tc.group
			if group == nil {
				group = defaultGroup
			}
			isReviewer := func(id int64) bool { return group[id] }
			taskStatus, reviewState, err := DecideIntermediateRule(tc.actor, tc.t, tc.actorID, isReviewer, tc.approve, tc.opinion)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if taskStatus != tc.wantTask || reviewState != tc.wantReview {
				t.Fatalf("= (%q,%q), want (%q,%q)", taskStatus, reviewState, tc.wantTask, tc.wantReview)
			}
		})
	}
}
