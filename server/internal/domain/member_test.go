package domain

import (
	"errors"
	"slices"
	"testing"
)

func TestValidateMemberRole(t *testing.T) {
	cases := []struct {
		name    string
		role    string
		wantErr error
	}{
		{"项目管理员", "admin", nil},
		{"项目成员", "member", nil},
		{"访客", "viewer", nil},
		{"空角色", "", ErrMemberRoleInvalid},
		{"未知角色", "superuser", ErrMemberRoleInvalid},
		{"大小写不匹配", "Admin", ErrMemberRoleInvalid},
		{"已取消的可编辑成员角色", "editor", ErrMemberRoleInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidateMemberRole(tc.role); !errors.Is(got, tc.wantErr) {
				t.Fatalf("ValidateMemberRole(%q) = %v, want %v", tc.role, got, tc.wantErr)
			}
		})
	}
}

// 权限规则依据：PRD §3.4「管理成员和权限」仅项目管理员 ✓；
// §0.4 V4.4.2：项目负责人与管理员独立同权，负责人不自动成为管理员。
func TestCanManageMembers(t *testing.T) {
	cases := []struct {
		name  string
		actor Actor
		want  bool
	}{
		{"项目管理员成员", Actor{Role: RoleAdmin}, true},
		{"项目负责人（非成员）享有同等权限", Actor{IsOwner: true}, true},
		{"项目负责人兼访客：负责人权限不因成员角色降级", Actor{IsOwner: true, Role: RoleViewer}, true},
		{"项目成员", Actor{Role: RoleMember}, false},
		{"访客", Actor{Role: RoleViewer}, false},
		{"非成员且非负责人", Actor{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanManageMembers(tc.actor); got != tc.want {
				t.Fatalf("CanManageMembers(%+v) = %v, want %v", tc.actor, got, tc.want)
			}
		})
	}
}

func TestCanEditProject(t *testing.T) {
	cases := []struct {
		name  string
		actor Actor
		want  bool
	}{
		{"项目管理员成员", Actor{Role: RoleAdmin}, true},
		{"项目负责人（非成员）", Actor{IsOwner: true}, true},
		{"项目成员", Actor{Role: RoleMember}, false},
		{"访客", Actor{Role: RoleViewer}, false},
		{"非成员且非负责人", Actor{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanEditProject(tc.actor); got != tc.want {
				t.Fatalf("CanEditProject(%+v) = %v, want %v", tc.actor, got, tc.want)
			}
		})
	}
}

// 读边界依据：PRD §3.3 项目内容仅对「所有项目内成员」可见；
// §0.4 V4.4.2：项目负责人为独立角色，即便未登记为成员也可读。
func TestCanReadProject(t *testing.T) {
	cases := []struct {
		name  string
		actor Actor
		want  bool
	}{
		{"项目管理员成员", Actor{Role: RoleAdmin}, true},
		{"项目成员", Actor{Role: RoleMember}, true},
		{"访客", Actor{Role: RoleViewer}, true},
		{"项目负责人（未登记为成员）", Actor{IsOwner: true}, true},
		{"非成员且非负责人", Actor{}, false},
		{"角色为未知值时不放行", Actor{Role: "superuser"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanReadProject(tc.actor); got != tc.want {
				t.Fatalf("CanReadProject(%+v) = %v, want %v", tc.actor, got, tc.want)
			}
		})
	}
}

// 批量加入成员（#93、AC-21 口径延伸）：一次提交为所选全部用户建立成员关系，
// 已在项目内或用户不存在的按人跳过，不让整批失败。
func TestPlanAddMembers(t *testing.T) {
	// 系统内已有的用户；项目现有成员是 2 与 7。
	known := []int64{1, 2, 3, 5, 7}
	existing := []int64{2, 7}

	cases := []struct {
		name        string
		role        string
		requested   []int64
		wantAdd     []int64
		wantSkipped []SkippedMember
		wantErr     error
	}{
		{
			name:      "全部成功",
			role:      RoleMember,
			requested: []int64{1, 3, 5},
			wantAdd:   []int64{1, 3, 5},
		},
		{
			name:        "部分已在项目内：跳过已有成员，其余照加",
			role:        RoleViewer,
			requested:   []int64{1, 2, 7},
			wantAdd:     []int64{1},
			wantSkipped: []SkippedMember{{UserID: 2, Reason: SkipAlreadyMember}, {UserID: 7, Reason: SkipAlreadyMember}},
		},
		{
			name:        "名单里有不存在的用户：按人跳过",
			role:        RoleMember,
			requested:   []int64{1, 99},
			wantAdd:     []int64{1},
			wantSkipped: []SkippedMember{{UserID: 99, Reason: SkipUserNotFound}},
		},
		{
			name:      "重复选中同一人只加一次",
			role:      RoleMember,
			requested: []int64{3, 3, 1, 3},
			wantAdd:   []int64{3, 1},
		},
		{
			name:        "全部已在项目内：不报错，全部跳过",
			role:        RoleMember,
			requested:   []int64{2, 7},
			wantSkipped: []SkippedMember{{UserID: 2, Reason: SkipAlreadyMember}, {UserID: 7, Reason: SkipAlreadyMember}},
		},
		{name: "空名单", role: RoleMember, requested: nil, wantErr: ErrNoMembersSelected},
		{name: "名单为空数组", role: RoleMember, requested: []int64{}, wantErr: ErrNoMembersSelected},
		{name: "角色不合法", role: "editor", requested: []int64{1}, wantErr: ErrMemberRoleInvalid},
		{name: "角色为空", role: "", requested: []int64{1}, wantErr: ErrMemberRoleInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			add, skipped, err := PlanAddMembers(tc.role, tc.requested, known, existing)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("PlanAddMembers err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			if !slices.Equal(add, tc.wantAdd) {
				t.Fatalf("add = %v, want %v", add, tc.wantAdd)
			}
			if !slices.Equal(skipped, tc.wantSkipped) {
				t.Fatalf("skipped = %v, want %v", skipped, tc.wantSkipped)
			}
		})
	}
}

// 跳过原因是派生文案的唯一来源（前端不按枚举拼文案）。
func TestSkipReasonLabel(t *testing.T) {
	cases := []struct {
		reason string
		want   string
	}{
		{SkipAlreadyMember, "已在项目内"},
		{SkipUserNotFound, "用户不存在"},
		{"unknown", "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.reason, func(t *testing.T) {
			if got := SkipReasonLabel(tc.reason); got != tc.want {
				t.Fatalf("SkipReasonLabel(%q) = %q, want %q", tc.reason, got, tc.want)
			}
		})
	}
}
