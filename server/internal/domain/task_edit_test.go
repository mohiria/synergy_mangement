package domain

import (
	"errors"
	"strings"
	"testing"
)

// #172 裁决（第二刀）：关键字段修改不再走变更单——有编辑权限者直接修改生效。
// 编辑权限与原提交权限同口径：负责人／KR 负责人／可编辑项目者；
// 可编辑状态：未开始／等待输入／进行中；关闭申请审批期间不能修改。
func TestTaskEditRule(t *testing.T) {
	facts := func(status string) TaskFacts {
		return TaskFacts{Status: status, CreatorID: 3, OwnerID: 5, KrOwnerID: i64(7)}
	}
	cases := []struct {
		name             string
		actor            Actor
		user             int64
		t                TaskFacts
		hasPendingCancel bool
		want             error
	}{
		{"负责人直接修改", Actor{Role: RoleMember}, 5, facts(TaskInProgress), false, nil},
		{"KR 负责人直接修改", Actor{Role: RoleMember}, 7, facts(TaskInProgress), false, nil},
		{"管理员直接修改", Actor{Role: RoleAdmin}, 9, facts(TaskNotStarted), false, nil},
		{"等待输入可修改", Actor{Role: RoleMember}, 5, facts(TaskWaitingInput), false, nil},
		{"无关成员禁止", Actor{Role: RoleMember}, 9, facts(TaskInProgress), false, ErrChangeForbidden},
		{"访客禁止", Actor{Role: RoleViewer}, 5, facts(TaskInProgress), false, ErrChangeForbidden},
		{"完成审核中不可修改", Actor{Role: RoleMember}, 5, facts(TaskPendingFinalReview), false, ErrChangeNotAllowed},
		{"已完成不可修改", Actor{Role: RoleMember}, 5, facts(TaskCompleted), false, ErrChangeNotAllowed},
		{"已关闭不可修改", Actor{Role: RoleAdmin}, 9, facts(TaskCancelled), false, ErrChangeNotAllowed},
		{"关闭申请审批期间不可修改", Actor{Role: RoleMember}, 5, facts(TaskInProgress), true, ErrCancelBlocked},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := TaskEditRule(tc.actor, tc.user, tc.t, tc.hasPendingCancel); !errors.Is(got, tc.want) {
				t.Fatalf("TaskEditRule() = %v, want %v", got, tc.want)
			}
		})
	}
}

// #172：字段修改直接生效后站内通知所属 KR 负责人（本人修改时不另发），与入池通知同模式。
func TestFieldEditNotify(t *testing.T) {
	kr := i64(7)
	if got := FieldEditNotifyTarget(5, kr); got == nil || *got != 7 {
		t.Fatalf("他人修改应通知 KR 负责人: %v", got)
	}
	if got := FieldEditNotifyTarget(7, kr); got != nil {
		t.Fatalf("KR 负责人本人修改不另发: %v", got)
	}
	if got := FieldEditNotifyTarget(5, nil); got != nil {
		t.Fatalf("KR 无负责人时不发: %v", got)
	}
	msg := FieldEditNotification("张三", "现场数据采集", []string{"任务名称", "截止时间"})
	for _, want := range []string{"张三", "现场数据采集", "任务名称", "截止时间"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("通知内容缺少 %q: %q", want, msg)
		}
	}
}
