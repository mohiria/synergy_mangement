package domain

import "testing"

// 裁决 #162：取消入池审批，任务创建／导入直接入池，入池写任务动态。
// 裁决 12（#183）：KR 负责人退场，原入池站内通知机制删除，只剩动态留痕可测。

func TestPoolEnteredActivityLabel(t *testing.T) {
	if got := ActivityKindLabel(ActivityPoolEntered); got != "任务入池" {
		t.Fatalf("ActivityKindLabel(pool_entered) = %q, want 任务入池", got)
	}
}
