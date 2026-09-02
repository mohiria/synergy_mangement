package domain

import (
	"errors"
	"testing"
)

// 裁决 10（#180）：任务配置类判定收归项目管理员（含项目负责人），裁决 D2（#137）四角色口径作废；
// 负责人保留的动作只剩交付物项、任务文件与成果审核人配置（上传链路），KR 负责人与创建人全部出局。
func TestTaskEditPermissionConvergence(t *testing.T) {
	kr := int64(7)
	pooled := TaskFacts{Status: TaskInProgress, CreatorID: 3, OwnerID: 5, KrOwnerID: &kr}
	member := Actor{Role: RoleMember}
	admin := Actor{Role: RoleAdmin}
	owner := Actor{IsOwner: true}

	// 配置收归组：仅项目管理员。
	adminOnly := map[string]func(Actor, int64, TaskFacts) bool{
		"CanConfigureInputs":    CanConfigureInputs,
		"CanManageParticipants": CanManageParticipants,
		"CanConfigureReceivers": CanConfigureReceivers,
	}
	// 负责人保留组：负责人本人或项目管理员（裁决 10 上传链路）。
	ownerKept := map[string]func(Actor, int64, TaskFacts) bool{
		"CanManageDeliverables": CanManageDeliverables,
		"CanManageTaskFiles":    CanManageTaskFiles,
		"CanManageReviewers":    CanManageReviewers,
	}
	cases := []struct {
		name          string
		actor         Actor
		uid           int64
		wantAdminOnly bool
		wantOwnerKept bool
	}{
		{"创建人（非负责人）不可操作", member, 3, false, false},
		{"所属 KR 负责人不可操作", member, 7, false, false},
		{"任务负责人仅保留上传链路配置", member, 5, false, true},
		{"项目管理员可操作", admin, 9, true, true},
		{"项目负责人可操作", owner, 9, true, true},
		{"无关成员不可操作", member, 8, false, false},
	}
	for name, judge := range adminOnly {
		for _, c := range cases {
			t.Run(name+"/"+c.name, func(t *testing.T) {
				if got := judge(c.actor, c.uid, pooled); got != c.wantAdminOnly {
					t.Fatalf("%s = %v, want %v", name, got, c.wantAdminOnly)
				}
			})
		}
	}
	for name, judge := range ownerKept {
		for _, c := range cases {
			t.Run(name+"/"+c.name, func(t *testing.T) {
				if got := judge(c.actor, c.uid, pooled); got != c.wantOwnerKept {
					t.Fatalf("%s = %v, want %v", name, got, c.wantOwnerKept)
				}
			})
		}
	}
}

// 裁决 10（#180）：关键字段直接修改收归项目管理员——负责人与 KR 负责人不再可改。
func TestTaskEditRuleAdminOnly(t *testing.T) {
	kr := int64(7)
	pooled := TaskFacts{Status: TaskInProgress, CreatorID: 3, OwnerID: 5, KrOwnerID: &kr}
	if err := TaskEditRule(Actor{Role: RoleMember}, 5, pooled); !errors.Is(err, ErrChangeForbidden) {
		t.Fatalf("负责人修改字段应被拒: %v", err)
	}
	if err := TaskEditRule(Actor{Role: RoleMember}, 7, pooled); !errors.Is(err, ErrChangeForbidden) {
		t.Fatalf("KR 负责人修改字段应被拒: %v", err)
	}
	if err := TaskEditRule(Actor{Role: RoleAdmin}, 9, pooled); err != nil {
		t.Fatalf("项目管理员应可直接修改: %v", err)
	}
}
