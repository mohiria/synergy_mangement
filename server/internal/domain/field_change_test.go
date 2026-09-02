package domain

import (
	"errors"
	"testing"
)

func sptr(s string) *string { return &s }

// §9.1（#172 修订）：直接修改任务字段——至少一项修改值；不再要求修改原因。
func TestValidateKeyFieldChanges(t *testing.T) {
	roles := map[int64]string{5: RoleMember, 8: RoleViewer}
	roleOf := func(id int64) string { return roles[id] }
	start := date("2026-09-01")

	cases := []struct {
		name string
		c    KeyFieldChanges
		want error
	}{
		{"合法改名", KeyFieldChanges{Name: sptr("新任务名")}, nil},
		{"空修改", KeyFieldChanges{}, ErrChangeEmpty},
		{"新名称为空", KeyFieldChanges{Name: sptr("   ")}, ErrTaskNameEmpty},
		{"新负责人非成员", KeyFieldChanges{OwnerID: i64(99)}, ErrTaskOwnerNotEligible},
		{"新负责人是访客", KeyFieldChanges{OwnerID: i64(8)}, ErrTaskOwnerNotEligible},
		{"新截止早于开始", KeyFieldChanges{EndDate: day("2026-08-20")}, ErrTaskPeriodInverted},
		{"新截止合法", KeyFieldChanges{EndDate: day("2026-09-15")}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidateKeyFieldChanges(tc.c, roleOf, start); !errors.Is(got, tc.want) {
				t.Fatalf("ValidateKeyFieldChanges() = %v, want %v", got, tc.want)
			}
		})
	}
}

// AC-57（#172 修订）：关闭申请只能由所属 KR 负责人处理，且仅待审批状态；
// 任务已终止时不得再批准；退回必须写清理由（MW-18）。
func TestDecideCancelRequestRule(t *testing.T) {
	krOwner := i64(7)
	inProgress := TaskFacts{Status: TaskInProgress, KrOwnerID: krOwner}
	if err := DecideCancelRequestRule(Actor{Role: RoleMember}, "pending", inProgress, 7, false, ""); !errors.Is(err, ErrRejectOpinionRequired) {
		t.Fatalf("退回不填理由应被拒: %v", err)
	}
	if err := DecideCancelRequestRule(Actor{Role: RoleMember}, "pending", inProgress, 7, false, "仍需执行"); err != nil {
		t.Fatalf("带理由退回应通过: %v", err)
	}
	if err := DecideCancelRequestRule(Actor{Role: RoleMember}, "pending", inProgress, 7, true, ""); err != nil {
		t.Fatalf("KR 负责人应可处理: %v", err)
	}
	if err := DecideCancelRequestRule(Actor{Role: RoleMember}, "pending", inProgress, 9, true, ""); !errors.Is(err, ErrNotKrOwner) {
		t.Fatalf("非 KR 负责人应被拒: %v", err)
	}
	if err := DecideCancelRequestRule(Actor{Role: RoleMember}, "approved", inProgress, 7, true, ""); !errors.Is(err, ErrCancelNotPending) {
		t.Fatalf("已处理关闭申请应冲突: %v", err)
	}
	if err := DecideCancelRequestRule(Actor{Role: RoleMember}, "pending", TaskFacts{Status: TaskInProgress}, 7, true, ""); !errors.Is(err, ErrNotKrOwner) {
		t.Fatalf("KR 无负责人时应被拒: %v", err)
	}
	for _, status := range []string{TaskCompleted, TaskCancelled} {
		if err := DecideCancelRequestRule(Actor{Role: RoleMember}, "pending", TaskFacts{Status: status, KrOwnerID: krOwner}, 7, true, ""); !errors.Is(err, ErrCancelTaskTerminal) {
			t.Fatalf("任务状态 %s 时应拒绝处理关闭申请: %v", status, err)
		}
	}
}

// 退回待处理事项：仅退回未处理，提交人或可编辑项目者可放弃。
func TestCanAbandonCancelRequest(t *testing.T) {
	cases := []struct {
		name      string
		actor     Actor
		user      int64
		submitter int64
		state     string
		resolved  bool
		want      bool
	}{
		{"提交人可放弃", Actor{Role: RoleMember}, 3, 3, "rejected", false, true},
		{"管理员可放弃", Actor{Role: RoleAdmin}, 9, 3, "rejected", false, true},
		{"他人不可放弃", Actor{Role: RoleMember}, 5, 3, "rejected", false, false},
		{"已处理不可再放弃", Actor{Role: RoleMember}, 3, 3, "rejected", true, false},
		{"待审批不可放弃", Actor{Role: RoleMember}, 3, 3, "pending", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanAbandonCancelRequest(tc.actor, tc.user, tc.submitter, tc.state, tc.resolved); got != tc.want {
				t.Fatalf("CanAbandonCancelRequest() = %v, want %v", got, tc.want)
			}
		})
	}
}
