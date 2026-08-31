package domain

import "testing"

// AC-64：O 编号为自然数、KR 编号形如 KR1.1、任务编号形如 T1.1.1（#102 加 T 前缀）；
// 编号创建时分配并持久保存，删除同级对象后其余编号不变。
func TestEntityCodes(t *testing.T) {
	if got := ObjectiveCode(2); got != "O2" {
		t.Fatalf("ObjectiveCode(2) = %q", got)
	}
	if got := KeyResultCode(1, 1); got != "KR1.1" {
		t.Fatalf("KeyResultCode(1,1) = %q", got)
	}
	if got := KeyResultCode(3, 12); got != "KR3.12" {
		t.Fatalf("KeyResultCode(3,12) = %q", got)
	}
	if got := TaskCode(1, 1, 1); got != "T1.1.1" {
		t.Fatalf("TaskCode(1,1,1) = %q", got)
	}
	if got := TaskCode(2, 3, 10); got != "T2.3.10" {
		t.Fatalf("TaskCode(2,3,10) = %q", got)
	}
	// 序号缺失（存量数据未回填）时不编造编号，返回空串让界面退化显示。
	if got := TaskCode(0, 1, 1); got != "" {
		t.Fatalf("缺 O 序号时不应编号: %q", got)
	}
	if got := KeyResultCode(1, 0); got != "" {
		t.Fatalf("缺 KR 序号时不应编号: %q", got)
	}
	if got := ObjectiveCode(0); got != "" {
		t.Fatalf("缺 O 序号时不应编号: %q", got)
	}
}

// AC-64：新序号取同级最大值加一，不复用被删对象的序号——
// 删除 O2 后新建的 O 是 O3，原 O3 编号不变。
func TestNextCodeSeq(t *testing.T) {
	if got := NextCodeSeq(nil); got != 1 {
		t.Fatalf("空集合应从 1 开始，got %d", got)
	}
	if got := NextCodeSeq([]int{1, 3}); got != 4 {
		t.Fatalf("应取最大值加一（不复用被删的 2），got %d", got)
	}
	if got := NextCodeSeq([]int{2}); got != 3 {
		t.Fatalf("NextCodeSeq([2]) = %d", got)
	}
}
