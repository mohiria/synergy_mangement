package domain

import (
	"errors"
	"testing"
	"time"
)

// AC-57：关闭申请复用关键字段变更单——发起人限任务负责人与项目管理员，
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
		want       FieldChangeOutcome
		wantErr    error
	}{
		{"负责人发起进审批", Actor{Role: RoleMember}, 5, facts(TaskInProgress), false, FieldChangePending, nil},
		{"项目管理员发起进审批", Actor{Role: RoleAdmin}, 9, facts(TaskInProgress), false, FieldChangePending, nil},
		{"KR 负责人本人免审", Actor{Role: RoleMember}, 7, facts(TaskInProgress), false, FieldChangeExempt, nil},
		{"未开始也可发起", Actor{Role: RoleMember}, 5, facts(TaskNotStarted), false, FieldChangePending, nil},
		{"草稿也可发起", Actor{Role: RoleMember}, 5, facts(TaskDraft), false, FieldChangePending, nil},
		{"创建人不是发起人", Actor{Role: RoleMember}, 3, facts(TaskInProgress), false, 0, ErrCancelForbidden},
		{"访客不可发起", Actor{Role: RoleViewer}, 9, facts(TaskInProgress), false, 0, ErrCancelForbidden},
		{"已完成不可取消", Actor{Role: RoleMember}, 5, facts(TaskCompleted), false, 0, ErrCannotCancel},
		{"已关闭不可再取消", Actor{Role: RoleMember}, 5, facts(TaskCancelled), false, 0, ErrCannotCancel},
		{"入池审批中互斥", Actor{Role: RoleMember}, 5, facts(TaskPendingPoolReview), false, 0, ErrCancelPendingExists},
		{"成果审核中互斥", Actor{Role: RoleMember}, 5, facts(TaskPendingIntermediateReview), false, 0, ErrCancelPendingExists},
		{"终审中互斥", Actor{Role: RoleMember}, 5, facts(TaskPendingFinalReview), false, 0, ErrCancelPendingExists},
		{"有待审批变更单互斥", Actor{Role: RoleMember}, 5, facts(TaskInProgress), true, 0, ErrCancelPendingExists},
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

// AC-57 反向互斥：待审批关闭单存在时，其他审批单不能提交。
func TestPendingCancelBlocksOtherApprovals(t *testing.T) {
	inProgress := TaskFacts{Status: TaskInProgress, CreatorID: 3, OwnerID: 5, KrOwnerID: i64(7)}
	if _, err := FieldChangeRoute(Actor{Role: RoleMember}, 5, inProgress, true); !errors.Is(err, ErrChangePendingExists) {
		t.Fatalf("有未决关闭单时提交关键字段修改应被拒: %v", err)
	}
	// KR 负责人本人的免审通道同样受未决单约束（否则免审会绕过互斥）。
	if _, err := FieldChangeRoute(Actor{Role: RoleMember}, 7, inProgress, true); !errors.Is(err, ErrChangePendingExists) {
		t.Fatalf("有未决关闭单时 KR 负责人免审也应被拒: %v", err)
	}
	draft := TaskFacts{Status: TaskDraft, CreatorID: 3, OwnerID: 5, KrOwnerID: i64(7)}
	if err := SubmitPoolReview(draft, true); !errors.Is(err, ErrCancelBlocked) {
		t.Fatalf("有未决关闭单时提交入池应被拒: %v", err)
	}
	if err := SubmitPoolReview(draft, false); err != nil {
		t.Fatalf("无未决单时提交入池应通过: %v", err)
	}
}

// AC-57：关闭单进入所属 KR 负责人的待我审批，处理规则与关键字段变更同源。
func TestDecideCancelRequest(t *testing.T) {
	inProgress := TaskFacts{Status: TaskInProgress, CreatorID: 3, OwnerID: 5, KrOwnerID: i64(7)}
	if err := DecideFieldChangeRule(Actor{Role: RoleMember}, FieldChangePendingState, inProgress, 7, true, ""); err != nil {
		t.Fatalf("KR 负责人应可通过关闭单: %v", err)
	}
	if err := DecideFieldChangeRule(Actor{Role: RoleMember}, FieldChangePendingState, inProgress, 5, true, ""); !errors.Is(err, ErrNotKrOwner) {
		t.Fatalf("非 KR 负责人不能处理关闭单: %v", err)
	}
	if err := DecideFieldChangeRule(Actor{Role: RoleMember}, FieldChangePendingState, inProgress, 7, false, "  "); !errors.Is(err, ErrRejectOpinionRequired) {
		t.Fatalf("退回关闭单必须写理由: %v", err)
	}
}

// AC-57：关闭单的拟议值固定为「已关闭」，旧值由任务当前状态快照。
func TestCancelChange(t *testing.T) {
	c := CancelChange()
	if c.Status == nil || *c.Status != TaskCancelled {
		t.Fatalf("拟议值应为已关闭，got %v", c.Status)
	}
	if c.Empty() {
		t.Fatal("关闭单不应被判为空变更")
	}
}

// AC-57／MW：关闭单进入所属 KR 负责人的「待我审批」，标题与关键字段修改区分开。
func TestCancelRequestInMyWork(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	me := int64(5)
	g := MyWork(MyWorkFacts{
		UserID: me,
		Now:    now,
		Tasks: []WorkTaskFact{
			{ID: 21, Name: "改期任务", DisplayStatus: TaskInProgress, OwnerID: 9, CreatorID: 9, KrOwnerID: i64(5)},
			{ID: 22, Name: "待取消任务", DisplayStatus: TaskInProgress, OwnerID: 9, CreatorID: 9, KrOwnerID: i64(5)},
		},
		FieldChanges: []WorkApprovalFact{
			{ID: 22, TaskID: 21, TaskName: "改期任务", SubmittedBy: 9, KrOwnerID: i64(5), SubmittedAt: now,
				ChangeType: FieldChangeTypeKeyFields},
			{ID: 23, TaskID: 22, TaskName: "待取消任务", SubmittedBy: 9, KrOwnerID: i64(5), SubmittedAt: now,
				ChangeType: FieldChangeTypeCancel},
		},
	})
	titles := map[string]bool{}
	for _, it := range g.Approvals {
		titles[it.Title] = true
	}
	if !titles["[关键字段修改] 改期任务"] {
		t.Fatalf("关键字段修改标题丢失: %+v", g.Approvals)
	}
	if !titles["[关闭申请] 待取消任务"] {
		t.Fatalf("关闭单未进待我审批或标题未区分: %+v", g.Approvals)
	}
}
