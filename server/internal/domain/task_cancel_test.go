package domain

import (
	"errors"
	"testing"
)

// 裁决 10（#180）：关闭改为项目管理员直接操作——原因必填、即时生效、无审批环节；
// 任务负责人与 KR 负责人不再能发起关闭；终态与未决审批互斥口径保持。
func TestCloseTaskRule(t *testing.T) {
	facts := func(status string) TaskFacts {
		return TaskFacts{Status: status, CreatorID: 3, OwnerID: 5, KrOwnerID: i64(7)}
	}
	reviewing := facts(TaskCompleted)
	reviewing.ResultUpdate = ResultUpdateReviewing
	cases := []struct {
		name    string
		actor   Actor
		user    int64
		t       TaskFacts
		wantErr error
	}{
		{"项目管理员直接关闭", Actor{Role: RoleAdmin}, 9, facts(TaskInProgress), nil},
		{"项目负责人直接关闭", Actor{IsOwner: true}, 9, facts(TaskInProgress), nil},
		{"未开始也可关闭", Actor{Role: RoleAdmin}, 9, facts(TaskNotStarted), nil},
		{"任务负责人不可关闭", Actor{Role: RoleMember}, 5, facts(TaskInProgress), ErrCancelForbidden},
		{"KR 负责人不可关闭", Actor{Role: RoleMember}, 7, facts(TaskInProgress), ErrCancelForbidden},
		{"访客不可关闭", Actor{Role: RoleViewer}, 9, facts(TaskInProgress), ErrCancelForbidden},
		{"已完成不可关闭", Actor{Role: RoleAdmin}, 9, facts(TaskCompleted), ErrCannotCancel},
		{"已关闭不可再关闭", Actor{Role: RoleAdmin}, 9, facts(TaskCancelled), ErrCannotCancel},
		{"成果审核中互斥", Actor{Role: RoleAdmin}, 9, facts(TaskPendingIntermediateReview), ErrCancelPendingExists},
		{"终审中互斥", Actor{Role: RoleAdmin}, 9, facts(TaskPendingFinalReview), ErrCancelPendingExists},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := CloseTaskRule(tc.actor, tc.t); !errors.Is(err, tc.wantErr) {
				t.Fatalf("CloseTaskRule() err = %v, want %v", err, tc.wantErr)
			}
		})
	}
	// KR 无负责人不再挡关闭（审批环节退场，无「无人可审」问题）。
	noKr := facts(TaskInProgress)
	noKr.KrOwnerID = nil
	if err := CloseTaskRule(Actor{Role: RoleAdmin}, noKr); err != nil {
		t.Fatalf("KR 无负责人不应挡管理员关闭: %v", err)
	}
}

// 裁决 10：未决审批单判定收敛为完成申请与成果更新两类（关闭申请退场，去布尔参数）。
func TestPendingApprovalOnTaskNoCancel(t *testing.T) {
	if PendingApprovalOnTask(TaskFacts{Status: TaskInProgress}) {
		t.Fatal("进行中且无在途审批不应判定为有未决审批单")
	}
	if !PendingApprovalOnTask(TaskFacts{Status: TaskPendingIntermediateReview}) {
		t.Fatal("成果审核中应判定为有未决审批单")
	}
	if !PendingApprovalOnTask(TaskFacts{Status: TaskCompleted, ResultUpdate: ResultUpdateReviewing}) {
		t.Fatal("成果更新在审应判定为有未决审批单")
	}
}
