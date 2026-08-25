package domain

import (
	"errors"
	"testing"
)

func TestValidateMemberRole(t *testing.T) {
	cases := []struct {
		name    string
		role    string
		wantErr error
	}{
		{"项目管理员", "admin", nil},
		{"可编辑成员", "editor", nil},
		{"普通成员", "member", nil},
		{"只读成员", "viewer", nil},
		{"空角色", "", ErrMemberRoleInvalid},
		{"未知角色", "superuser", ErrMemberRoleInvalid},
		{"大小写不匹配", "Admin", ErrMemberRoleInvalid},
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
		{"项目负责人兼只读成员：负责人权限不因成员角色降级", Actor{IsOwner: true, Role: RoleViewer}, true},
		{"可编辑成员", Actor{Role: RoleEditor}, false},
		{"普通成员", Actor{Role: RoleMember}, false},
		{"只读成员", Actor{Role: RoleViewer}, false},
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
		{"可编辑成员暂无项目配置编辑权", Actor{Role: RoleEditor}, false},
		{"普通成员", Actor{Role: RoleMember}, false},
		{"只读成员", Actor{Role: RoleViewer}, false},
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
