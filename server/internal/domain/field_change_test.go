package domain

import (
	"errors"
	"testing"
)

func sptr(s string) *string { return &s }

// §9.1：提交关键字段修改——至少一项拟议值；进入审批时修改原因必填。
func TestValidateKeyFieldChanges(t *testing.T) {
	roles := map[int64]string{5: RoleMember, 8: RoleViewer}
	roleOf := func(id int64) string { return roles[id] }
	start := date("2026-09-01")

	cases := []struct {
		name    string
		c       KeyFieldChanges
		reason  string
		needRsn bool
		want    error
	}{
		{"合法改名", KeyFieldChanges{Name: sptr("新任务名")}, "口径调整", true, nil},
		{"空变更", KeyFieldChanges{}, "x", true, ErrChangeEmpty},
		{"审批路径原因必填", KeyFieldChanges{Name: sptr("新名")}, "  ", true, ErrChangeReasonRequired},
		{"草稿完善可不填原因", KeyFieldChanges{Name: sptr("新名")}, "", false, nil},
		{"新名称为空", KeyFieldChanges{Name: sptr("   ")}, "x", true, ErrTaskNameEmpty},
		{"新负责人非成员", KeyFieldChanges{OwnerID: i64(99)}, "x", true, ErrTaskOwnerNotEligible},
		{"新负责人是只读成员", KeyFieldChanges{OwnerID: i64(8)}, "x", true, ErrTaskOwnerNotEligible},
		{"新截止早于开始", KeyFieldChanges{EndDate: day("2026-08-20")}, "x", true, ErrTaskPeriodInverted},
		{"新截止合法", KeyFieldChanges{EndDate: day("2026-09-15")}, "x", true, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ValidateKeyFieldChanges(tc.c, tc.reason, tc.needRsn, roleOf, start)
			if !errors.Is(got, tc.want) {
				t.Fatalf("ValidateKeyFieldChanges() = %v, want %v", got, tc.want)
			}
		})
	}
}

// AC-23 路由：草稿直接完善；KR 负责人本人免审即时生效；已入池任务进入审批；
// 待入池审批／审核中／终态不可修改；同一任务最多一张待审批变更单。
func TestFieldChangeRoute(t *testing.T) {
	facts := func(status string) TaskFacts {
		return TaskFacts{Status: status, CreatorID: 3, OwnerID: 5, KrOwnerID: i64(7)}
	}
	cases := []struct {
		name       string
		actor      Actor
		user       int64
		t          TaskFacts
		hasPending bool
		want       FieldChangeOutcome
		wantErr    error
	}{
		{"草稿创建人直接完善", Actor{Role: RoleMember}, 3, facts(TaskDraft), false, FieldChangeDirect, nil},
		{"草稿负责人直接完善", Actor{Role: RoleMember}, 5, facts(TaskDraft), false, FieldChangeDirect, nil},
		{"草稿无关成员禁止", Actor{Role: RoleMember}, 9, facts(TaskDraft), false, 0, ErrChangeForbidden},
		{"KR 负责人本人免审", Actor{Role: RoleMember}, 7, facts(TaskInProgress), false, FieldChangeExempt, nil},
		{"负责人提交进入审批", Actor{Role: RoleMember}, 5, facts(TaskInProgress), false, FieldChangePending, nil},
		{"管理员提交进入审批", Actor{Role: RoleAdmin}, 9, facts(TaskNotStarted), false, FieldChangePending, nil},
		{"等待输入可提交", Actor{Role: RoleMember}, 5, facts(TaskWaitingInput), false, FieldChangePending, nil},
		{"无关成员禁止", Actor{Role: RoleMember}, 9, facts(TaskInProgress), false, 0, ErrChangeForbidden},
		{"已有待审批变更单冲突", Actor{Role: RoleMember}, 5, facts(TaskInProgress), true, 0, ErrChangePendingExists},
		{"待入池审批不可修改", Actor{Role: RoleMember}, 5, facts(TaskPendingPoolReview), false, 0, ErrChangeNotAllowed},
		{"完成审核中不可修改", Actor{Role: RoleMember}, 5, facts(TaskPendingFinalReview), false, 0, ErrChangeNotAllowed},
		{"已完成不可修改", Actor{Role: RoleMember}, 5, facts(TaskCompleted), false, 0, ErrChangeNotAllowed},
		{"已取消不可修改", Actor{Role: RoleAdmin}, 9, facts(TaskCancelled), false, 0, ErrChangeNotAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := FieldChangeRoute(tc.actor, tc.user, tc.t, tc.hasPending)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("FieldChangeRoute() err = %v, want %v", err, tc.wantErr)
			}
			if err == nil && got != tc.want {
				t.Fatalf("FieldChangeRoute() = %v, want %v", got, tc.want)
			}
		})
	}
}

// AC-23：变更单只能由所属 KR 负责人处理，且仅待审批状态；任务已终止时不得再批准（回归：R3）。
func TestDecideFieldChangeRule(t *testing.T) {
	krOwner := i64(7)
	inProgress := TaskFacts{Status: TaskInProgress, KrOwnerID: krOwner}
	// MW-18：变更单退回同样必须写清理由。
	if err := DecideFieldChangeRule(Actor{Role: RoleMember}, "pending", inProgress, 7, false, ""); !errors.Is(err, ErrRejectOpinionRequired) {
		t.Fatalf("退回不填理由应被拒: %v", err)
	}
	if err := DecideFieldChangeRule(Actor{Role: RoleMember}, "pending", inProgress, 7, false, "口径不符"); err != nil {
		t.Fatalf("带理由退回应通过: %v", err)
	}
	if err := DecideFieldChangeRule(Actor{Role: RoleMember}, "pending", inProgress, 7, true, ""); err != nil {
		t.Fatalf("KR 负责人应可处理: %v", err)
	}
	if err := DecideFieldChangeRule(Actor{Role: RoleMember}, "pending", inProgress, 9, true, ""); !errors.Is(err, ErrNotKrOwner) {
		t.Fatalf("非 KR 负责人应被拒: %v", err)
	}
	if err := DecideFieldChangeRule(Actor{Role: RoleMember}, "approved", inProgress, 7, true, ""); !errors.Is(err, ErrChangeNotPending) {
		t.Fatalf("已处理变更单应冲突: %v", err)
	}
	if err := DecideFieldChangeRule(Actor{Role: RoleMember}, "pending", TaskFacts{Status: TaskInProgress}, 7, true, ""); !errors.Is(err, ErrNotKrOwner) {
		t.Fatalf("KR 无负责人时应被拒: %v", err)
	}
	// 终态任务的未决变更单不得再被批准：批准会改写已完成／已取消任务的名称、负责人与截止时间。
	for _, status := range []string{TaskCompleted, TaskCancelled} {
		if err := DecideFieldChangeRule(Actor{Role: RoleMember}, "pending", TaskFacts{Status: status, KrOwnerID: krOwner}, 7, true, ""); !errors.Is(err, ErrChangeTaskTerminal) {
			t.Fatalf("任务状态 %s 时应拒绝处理变更单: %v", status, err)
		}
	}
}

// 退回待处理事项：仅退回未处理，提交人或可编辑项目者可放弃。
func TestCanAbandonFieldChange(t *testing.T) {
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
			if got := CanAbandonFieldChange(tc.actor, tc.user, tc.submitter, tc.state, tc.resolved); got != tc.want {
				t.Fatalf("CanAbandonFieldChange() = %v, want %v", got, tc.want)
			}
		})
	}
}
