package domain

import (
	"testing"
)

// #172 裁决：结构变更（输入、输入源、接收方）与标量关键字段同口径直接生效，
// 权限与状态判定共用 TaskEditRule（见 TestTaskEditRule）；本处只验证动作枚举登记。
func TestStructureOps(t *testing.T) {
	for _, op := range []string{StructureAddTaskInput, StructureAddMemberInput,
		StructureRemoveEdge, StructureSetReceivers} {
		if !ValidStructureOp(op) {
			t.Fatalf("未登记的结构变更动作 %q", op)
		}
	}
}

// 差异行字段名对齐 §5.2.B 的关键字段用词；未知动作不执行也不伪造字段名。
func TestStructureFieldLabel(t *testing.T) {
	cases := map[string]string{
		StructureAddTaskInput:   "任务输入",
		StructureAddMemberInput: "输入源",
		StructureRemoveEdge:     "输入源",
		StructureSetReceivers:   "接收方",
	}
	if ValidStructureOp("add_deliverable") {
		t.Fatal("输出已移出关键字段（裁决 H1），add_deliverable 不应再是合法动作")
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
