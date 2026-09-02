package domain

import (
	"errors"
	"testing"
	"time"
)

// AC-57（#172 修订）：关闭申请从变更单机制独立——发起人限任务负责人与项目管理员，
// KR 负责人在本人负责 KR 下免审即时生效，任务上有任一未决审批单时不能发起。
func TestCancelRoute(t *testing.T) {
	facts := func(status string) TaskFacts {
		return TaskFacts{Status: status, CreatorID: 3, OwnerID: 5, KrOwnerID: i64(7)}
	}
	noKrOwner := func(status string) TaskFacts {
		f := facts(status)
		f.KrOwnerID = nil
		return f
	}
	cases := []struct {
		name       string
		actor      Actor
		user       int64
		t          TaskFacts
		hasPending bool
		want       CancelOutcome
		wantErr    error
	}{
		{"负责人发起进审批", Actor{Role: RoleMember}, 5, facts(TaskInProgress), false, CancelPending, nil},
		{"项目管理员发起进审批", Actor{Role: RoleAdmin}, 9, facts(TaskInProgress), false, CancelPending, nil},
		{"KR 负责人本人免审", Actor{Role: RoleMember}, 7, facts(TaskInProgress), false, CancelExempt, nil},
		{"未开始也可发起", Actor{Role: RoleMember}, 5, facts(TaskNotStarted), false, CancelPending, nil},
		{"创建人不是发起人", Actor{Role: RoleMember}, 3, facts(TaskInProgress), false, 0, ErrCancelForbidden},
		{"访客不可发起", Actor{Role: RoleViewer}, 9, facts(TaskInProgress), false, 0, ErrCancelForbidden},
		{"已完成不可取消", Actor{Role: RoleMember}, 5, facts(TaskCompleted), false, 0, ErrCannotCancel},
		{"已关闭不可再取消", Actor{Role: RoleMember}, 5, facts(TaskCancelled), false, 0, ErrCannotCancel},
		{"成果审核中互斥", Actor{Role: RoleMember}, 5, facts(TaskPendingIntermediateReview), false, 0, ErrCancelPendingExists},
		{"终审中互斥", Actor{Role: RoleMember}, 5, facts(TaskPendingFinalReview), false, 0, ErrCancelPendingExists},
		{"已有待审批关闭申请互斥", Actor{Role: RoleMember}, 5, facts(TaskInProgress), true, 0, ErrCancelPendingExists},
		{"未决审批优先于免审", Actor{Role: RoleMember}, 7, facts(TaskInProgress), true, 0, ErrCancelPendingExists},
		{"KR 无负责人无人可审", Actor{Role: RoleMember}, 5, noKrOwner(TaskInProgress), false, 0, ErrKrOwnerMissing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CancelRoute(tc.actor, tc.user, tc.t, tc.hasPending)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("CancelRoute() err = %v, want %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("CancelRoute() = %v, want %v", got, tc.want)
			}
		})
	}
}

// AC-57 反向互斥（#172 修订）：待审批关闭申请存在时，任务字段修改不可进行。
func TestPendingCancelBlocksEdits(t *testing.T) {
	inProgress := TaskFacts{Status: TaskInProgress, CreatorID: 3, OwnerID: 5, KrOwnerID: i64(7)}
	if err := TaskEditRule(Actor{Role: RoleMember}, 5, inProgress, true); !errors.Is(err, ErrCancelBlocked) {
		t.Fatalf("有未决关闭申请时修改字段应被拒: %v", err)
	}
	// KR 负责人本人同样受未决关闭申请约束（否则会绕过互斥）。
	if err := TaskEditRule(Actor{Role: RoleMember}, 7, inProgress, true); !errors.Is(err, ErrCancelBlocked) {
		t.Fatalf("有未决关闭申请时 KR 负责人修改也应被拒: %v", err)
	}
}

// AC-57／MW（#172 修订）：关闭申请进入所属 KR 负责人的「待我审批」，标题带类型前缀。
func TestCancelRequestInMyWork(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	me := int64(5)
	g := MyWork(MyWorkFacts{
		UserID: me,
		Now:    now,
		Tasks: []WorkTaskFact{
			{ID: 22, Name: "待取消任务", DisplayStatus: TaskInProgress, OwnerID: 9, CreatorID: 9, KrOwnerID: i64(5)},
		},
		CancelRequests: []WorkApprovalFact{
			{ID: 23, TaskID: 22, TaskName: "待取消任务", SubmittedBy: 9, KrOwnerID: i64(5), SubmittedAt: now},
		},
	})
	if len(g.Approvals) != 1 || g.Approvals[0].Title != "[关闭申请] 待取消任务" {
		t.Fatalf("关闭申请未进待我审批或标题异常: %+v", g.Approvals)
	}
	if g.Approvals[0].Kind != "cancel_request" {
		t.Fatalf("关闭申请卡片类型应为 cancel_request: %+v", g.Approvals[0])
	}
}
