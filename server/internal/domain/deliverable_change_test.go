package domain

import (
	"errors"
	"testing"
)

// 裁决 H1（#141）：提交完成申请之前，交付物项的新增／删除完全自由，不走关键字段审批；
// 完成申请提交后到审结前冻结；已发布（有当前内容）的项不可删，必须走成果更新。
func TestDeliverableStructureRule(t *testing.T) {
	const me, other = int64(5), int64(9)
	member := Actor{Role: RoleMember}
	facts := func(status string) TaskFacts {
		return TaskFacts{Status: status, CreatorID: other, OwnerID: me}
	}
	cases := []struct {
		name    string
		actor   Actor
		userID  int64
		facts   TaskFacts
		wantErr error
	}{
		{"负责人新增即时生效，不进审批", member, me, facts(TaskInProgress), nil},
		{"未开始状态负责人可调整", member, me, facts(TaskNotStarted), nil},
		{"等待输入状态负责人可调整", member, me, facts(TaskWaitingInput), nil},
		{"非负责人的普通成员无权调整", member, other, facts(TaskInProgress), ErrDeliverableChangeForbidden},
		{"隐式访客无权调整", Actor{Role: RoleViewer, Implicit: true}, me, facts(TaskInProgress), ErrDeliverableChangeForbidden},
		{"成果审核中冻结", member, me, facts(TaskInReview), ErrDeliverableFrozen},
		{"终审中冻结", member, me, facts(TaskInReview), ErrDeliverableFrozen},
		{"已完成不可增删", member, me, facts(TaskCompleted), ErrDeliverableStateNotAllowed},
		{"已关闭不可增删", member, me, facts(TaskCancelled), ErrDeliverableStateNotAllowed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := DeliverableStructureRule(c.actor, c.userID, c.facts)
			if c.wantErr == nil {
				if err != nil {
					t.Fatalf("DeliverableStructureRule = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("DeliverableStructureRule = %v, want %v", err, c.wantErr)
			}
		})
	}
}

func TestDeleteDeliverableRule(t *testing.T) {
	const me = int64(5)
	member := Actor{Role: RoleMember}
	facts := func(status string) TaskFacts {
		return TaskFacts{Status: status, CreatorID: me, OwnerID: me}
	}
	cases := []struct {
		name       string
		actor      Actor
		userID     int64
		facts      TaskFacts
		hasCurrent bool
		wantErr    error
	}{
		{"未发布的项可自由删除", member, me, facts(TaskInProgress), false, nil},
		{"有当前内容的项不可删，走成果更新", member, me, facts(TaskInProgress), true, ErrDeliverableHasCurrent},
		{"已完成任务上已发布的项，提示走成果更新而非状态错误", member, me, facts(TaskCompleted), true, ErrDeliverableHasCurrent},
		{"完成申请在审期间不可删", member, me, facts(TaskInReview), false, ErrDeliverableFrozen},
		{"非负责人的普通成员无权删除", member, int64(9), facts(TaskInProgress), false, ErrDeliverableChangeForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := DeleteDeliverableRule(c.actor, c.userID, c.facts, c.hasCurrent)
			if c.wantErr == nil {
				if err != nil {
					t.Fatalf("DeleteDeliverableRule = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("DeleteDeliverableRule = %v, want %v", err, c.wantErr)
			}
		})
	}
}
