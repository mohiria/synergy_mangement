package domain

import (
	"errors"
	"testing"
)

// 裁决 D2（#137）：任务编辑权限收敛为四角色——任务负责人、所属 KR 负责人、项目管理员、
// 项目负责人（CanEditProject 覆盖后两者）；裁决 #162 后无草稿期，创建人不再有编辑权；
// 其他项目成员（含参与人）与访客只读。六个配置判定一个口径。
func TestTaskEditPermissionConvergence(t *testing.T) {
	kr := int64(7)
	pooled := TaskFacts{Status: TaskInProgress, CreatorID: 3, OwnerID: 5, KrOwnerID: &kr}
	member := Actor{Role: RoleMember}
	admin := Actor{Role: RoleAdmin}

	judges := map[string]func(Actor, int64, TaskFacts) bool{
		"CanManageDeliverables": CanManageDeliverables,
		"CanConfigureInputs":    CanConfigureInputs,
		"CanManageParticipants": CanManageParticipants,
		"CanManageTaskFiles":    CanManageTaskFiles,
		"CanManageReviewers":    CanManageReviewers,
		"CanConfigureReceivers": CanConfigureReceivers,
	}
	cases := []struct {
		name  string
		actor Actor
		uid   int64
		facts TaskFacts
		want  bool
	}{
		{"创建人（非负责人）不可编辑", member, 3, pooled, false},
		{"所属 KR 负责人可编辑", member, 7, pooled, true},
		{"任务负责人可编辑", member, 5, pooled, true},
		{"项目管理员可编辑", admin, 9, pooled, true},
		{"参与人（无关成员）不可编辑", member, 8, pooled, false},
	}
	for name, judge := range judges {
		for _, c := range cases {
			t.Run(name+"/"+c.name, func(t *testing.T) {
				if got := judge(c.actor, c.uid, c.facts); got != c.want {
					t.Fatalf("%s = %v, want %v", name, got, c.want)
				}
			})
		}
	}
}

// 裁决 D2（#172 修订）：关键字段直接修改同口径——创建人（非负责人）无权修改；
// KR 负责人与负责人可直接修改。
func TestTaskEditRuleCreatorNoEdit(t *testing.T) {
	kr := int64(7)
	pooled := TaskFacts{Status: TaskInProgress, CreatorID: 3, OwnerID: 5, KrOwnerID: &kr}
	if err := TaskEditRule(Actor{Role: RoleMember}, 3, pooled, false); !errors.Is(err, ErrChangeForbidden) {
		t.Fatalf("创建人修改字段应被拒: %v", err)
	}
	if err := TaskEditRule(Actor{Role: RoleMember}, 7, pooled, false); err != nil {
		t.Fatalf("KR 负责人应可直接修改: %v", err)
	}
}
