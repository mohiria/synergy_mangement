package domain

import (
	"errors"
	"testing"
)

// AC-23／§5.2.B：四类结构变更（输入、输入源、输出、接收方）乘三条路由。
func TestStructureChangeRoute(t *testing.T) {
	facts := func(status string) TaskFacts {
		return TaskFacts{Status: status, CreatorID: 3, OwnerID: 5, KrOwnerID: i64(7)}
	}
	ops := []string{StructureAddTaskInput, StructureAddMemberInput, StructureRemoveEdge,
		StructureAddDeliverable, StructureSetReceivers}
	cases := []struct {
		name       string
		actor      Actor
		user       int64
		t          TaskFacts
		hasPending bool
		want       FieldChangeOutcome
		wantErr    error
	}{
		{"草稿负责人直接生效", Actor{Role: RoleMember}, 5, facts(TaskDraft), false, FieldChangeDirect, nil},
		{"草稿创建人直接生效", Actor{Role: RoleMember}, 3, facts(TaskDraft), false, FieldChangeDirect, nil},
		{"草稿无关成员禁止", Actor{Role: RoleMember}, 9, facts(TaskDraft), false, 0, ErrChangeForbidden},
		{"KR 负责人本人免审", Actor{Role: RoleMember}, 7, facts(TaskInProgress), false, FieldChangeExempt, nil},
		{"已入池负责人进审批", Actor{Role: RoleMember}, 5, facts(TaskInProgress), false, FieldChangePending, nil},
		{"未开始也进审批", Actor{Role: RoleMember}, 5, facts(TaskNotStarted), false, FieldChangePending, nil},
		{"项目管理员进审批", Actor{Role: RoleAdmin}, 9, facts(TaskInProgress), false, FieldChangePending, nil},
		{"访客禁止", Actor{Role: RoleViewer}, 9, facts(TaskInProgress), false, 0, ErrChangeForbidden},
		{"已有待审批单互斥", Actor{Role: RoleMember}, 5, facts(TaskInProgress), true, 0, ErrChangePendingExists},
		{"待入池审批期间不可改", Actor{Role: RoleMember}, 5, facts(TaskPendingPoolReview), false, 0, ErrChangeNotAllowed},
		{"终审中不可改", Actor{Role: RoleMember}, 5, facts(TaskPendingFinalReview), false, 0, ErrChangeNotAllowed},
		{"已完成不可改", Actor{Role: RoleMember}, 5, facts(TaskCompleted), false, 0, ErrChangeNotAllowed},
		{"已关闭不可改", Actor{Role: RoleMember}, 5, facts(TaskCancelled), false, 0, ErrChangeNotAllowed},
	}
	for _, op := range ops {
		for _, tc := range cases {
			t.Run(op+"/"+tc.name, func(t *testing.T) {
				if !ValidStructureOp(op) {
					t.Fatalf("未登记的结构变更动作 %q", op)
				}
				got, err := StructureChangeRoute(tc.actor, tc.user, tc.t, tc.hasPending)
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("StructureChangeRoute() err = %v, want %v", err, tc.wantErr)
				}
				if got != tc.want {
					t.Fatalf("StructureChangeRoute() = %v, want %v", got, tc.want)
				}
			})
		}
	}
}

// 差异行字段名对齐 §5.2.B 的关键字段用词；未知动作不执行也不伪造字段名。
func TestStructureFieldLabel(t *testing.T) {
	cases := map[string]string{
		StructureAddTaskInput:   "任务输入",
		StructureAddMemberInput: "输入源",
		StructureRemoveEdge:     "输入源",
		StructureAddDeliverable: "预期交付物",
		StructureSetReceivers:   "接收方",
	}
	for op, want := range cases {
		if got := StructureFieldLabel(op); got != want {
			t.Fatalf("StructureFieldLabel(%q) = %q, want %q", op, got, want)
		}
	}
	if ValidStructureOp("drop_all_tasks") {
		t.Fatal("未知动作不应被认为合法")
	}
	if got := StructureFieldLabel("drop_all_tasks"); got != "任务结构" {
		t.Fatalf("未知动作字段名 = %q", got)
	}
}
