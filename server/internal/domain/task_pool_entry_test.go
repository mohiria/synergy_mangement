package domain

import "testing"

// 裁决 #162：取消入池审批，任务创建／导入直接入池。
// 补偿机制：入池写任务动态，并站内通知所属 KR 负责人（本人创建不另发）。

func TestPoolEntryNotifyTarget(t *testing.T) {
	owner := int64(7)
	cases := []struct {
		name      string
		creatorID int64
		krOwnerID *int64
		want      *int64
	}{
		{"成员创建通知 KR 负责人", 3, &owner, &owner},
		{"KR 负责人本人创建不另发", 7, &owner, nil},
		{"KR 未指定负责人无人可通知", 3, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PoolEntryNotifyTarget(tc.creatorID, tc.krOwnerID)
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("PoolEntryNotifyTarget() = %v, want nil", *got)
			case tc.want != nil && (got == nil || *got != *tc.want):
				t.Fatalf("PoolEntryNotifyTarget() = %v, want %d", got, *tc.want)
			}
		})
	}
}

func TestPoolEntryNotification(t *testing.T) {
	got := PoolEntryNotification("张三", "输出验收报告", "KR1.1", "完成验收标准制定")
	want := "张三创建的任务「输出验收报告」已进入 KR1.1「完成验收标准制定」任务池，初始状态未开始"
	if got != want {
		t.Fatalf("PoolEntryNotification() = %q, want %q", got, want)
	}
}

func TestPoolEnteredActivityLabel(t *testing.T) {
	if got := ActivityKindLabel(ActivityPoolEntered); got != "任务入池" {
		t.Fatalf("ActivityKindLabel(pool_entered) = %q, want 任务入池", got)
	}
}
