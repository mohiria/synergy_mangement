package domain

import (
	"strings"
	"testing"
)

// #178 裁决（裁决 8）：「指定项目成员提供」改为替他人创建上游任务——
// 新任务直接入池（#162 口径），另通知新任务负责人；本人替自己创建时不另发。
func TestUpstreamTaskNotify(t *testing.T) {
	if got := UpstreamTaskNotifyTarget(3, 5); got == nil || *got != 5 {
		t.Fatalf("替他人创建应通知新任务负责人: %v", got)
	}
	if got := UpstreamTaskNotifyTarget(5, 5); got != nil {
		t.Fatalf("替自己创建不另发: %v", got)
	}
	msg := UpstreamTaskNotification("张三", "补充现场口径说明", "回归验证分析")
	for _, want := range []string{"张三", "补充现场口径说明", "回归验证分析", "负责人"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("通知内容缺少 %q: %q", want, msg)
		}
	}
}
