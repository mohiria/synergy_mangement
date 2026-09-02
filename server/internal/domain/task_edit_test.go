package domain

import (
	"errors"
	"testing"
)

// 裁决 10（#180）：关键字段直接修改收归项目管理员（含项目负责人）；
// 可编辑状态：未开始／等待输入／进行中；关闭申请机制退场后无审批互斥。
func TestTaskEditRule(t *testing.T) {
	facts := func(status string) TaskFacts {
		return TaskFacts{Status: status, CreatorID: 3, OwnerID: 5}
	}
	cases := []struct {
		name  string
		actor Actor
		user  int64
		t     TaskFacts
		want  error
	}{
		{"管理员直接修改", Actor{Role: RoleAdmin}, 9, facts(TaskNotStarted), nil},
		{"项目负责人直接修改", Actor{IsOwner: true}, 9, facts(TaskInProgress), nil},
		{"等待输入可修改", Actor{Role: RoleAdmin}, 9, facts(TaskWaitingInput), nil},
		{"任务负责人禁止", Actor{Role: RoleMember}, 5, facts(TaskInProgress), ErrChangeForbidden},
		{"KR 负责人禁止", Actor{Role: RoleMember}, 7, facts(TaskInProgress), ErrChangeForbidden},
		{"无关成员禁止", Actor{Role: RoleMember}, 9, facts(TaskInProgress), ErrChangeForbidden},
		{"访客禁止", Actor{Role: RoleViewer}, 5, facts(TaskInProgress), ErrChangeForbidden},
		{"完成审核中不可修改", Actor{Role: RoleAdmin}, 9, facts(TaskInReview), ErrChangeNotAllowed},
		{"已完成不可修改", Actor{Role: RoleAdmin}, 9, facts(TaskCompleted), ErrChangeNotAllowed},
		{"已关闭不可修改", Actor{Role: RoleAdmin}, 9, facts(TaskCancelled), ErrChangeNotAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := TaskEditRule(tc.actor, tc.user, tc.t); !errors.Is(got, tc.want) {
				t.Fatalf("TaskEditRule() = %v, want %v", got, tc.want)
			}
		})
	}
}

// 裁决 12（#183）：KR 负责人退场，原 #172 字段修改站内通知机制删除——
// 修改事实只体现在任务动态（ActivityFieldEdited），此处不再有通知派生可测。
