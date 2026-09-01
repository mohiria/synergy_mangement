package domain

import (
	"errors"
	"testing"
)

// 裁决 D2（#137）：任务编辑权限收敛为四角色——任务负责人、所属 KR 负责人、项目管理员、
// 项目负责人（CanEditProject 覆盖后两者）；创建人只在草稿期保留编辑权；
// 其他项目成员（含参与人）与访客只读。六个配置判定一个口径。
func TestTaskEditPermissionConvergence(t *testing.T) {
	kr := int64(7)
	draft := TaskFacts{Status: TaskDraft, CreatorID: 3, OwnerID: 5, KrOwnerID: &kr}
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
		{"创建人草稿期可编辑", member, 3, draft, true},
		{"创建人入池后不可编辑", member, 3, pooled, false},
		{"所属 KR 负责人入池后可编辑", member, 7, pooled, true},
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

// 裁决 D2：关键字段修改同口径——创建人只在草稿期直接完善，入池后无权提交变更；
// KR 负责人免审即时生效路径不变。
func TestFieldChangeRouteCreatorOnlyDraft(t *testing.T) {
	kr := int64(7)
	pooled := TaskFacts{Status: TaskInProgress, CreatorID: 3, OwnerID: 5, KrOwnerID: &kr}
	if _, err := FieldChangeRoute(Actor{Role: RoleMember}, 3, pooled, false); !errors.Is(err, ErrChangeForbidden) {
		t.Fatalf("入池后创建人提交变更应被拒: %v", err)
	}
	if out, err := FieldChangeRoute(Actor{Role: RoleMember}, 3, TaskFacts{Status: TaskDraft, CreatorID: 3, OwnerID: 5, KrOwnerID: &kr}, false); err != nil || out != FieldChangeDirect {
		t.Fatalf("草稿期创建人应可直接完善: %v %v", out, err)
	}
	if out, err := FieldChangeRoute(Actor{Role: RoleMember}, 7, pooled, false); err != nil || out != FieldChangeExempt {
		t.Fatalf("KR 负责人免审路径应不变: %v %v", out, err)
	}
}
