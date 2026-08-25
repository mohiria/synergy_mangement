package domain

import "errors"

var ErrMemberRoleInvalid = errors.New("成员角色不合法")

// 成员角色四值枚举（词汇表「成员角色」；PRD §3.2 系统权限）。
const (
	RoleAdmin  = "admin"
	RoleEditor = "editor"
	RoleMember = "member"
	RoleViewer = "viewer"
)

// Actor 当前用户在某个项目内的身份事实：是否项目负责人、成员角色（非成员为空串）。
type Actor struct {
	IsOwner bool
	Role    string
}

var memberRoles = map[string]struct{}{
	RoleAdmin:  {},
	RoleEditor: {},
	RoleMember: {},
	RoleViewer: {},
}

// ValidateMemberRole 校验成员角色是否属于四值枚举。
func ValidateMemberRole(role string) error {
	if _, ok := memberRoles[role]; !ok {
		return ErrMemberRoleInvalid
	}
	return nil
}

// CanManageMembers 判定能否管理项目成员和权限：仅项目管理员（PRD §3.4）；
// 项目负责人为独立角色、享有同等权限（PRD §0.4 V4.4.2）。
func CanManageMembers(a Actor) bool {
	return a.IsOwner || a.Role == RoleAdmin
}

// CanEditProject 判定能否编辑项目基础信息与配置：骨架阶段与管理成员同一规则；
// 可编辑成员的「授权范围」编辑权待 O／KR 结构落地后再细化。
func CanEditProject(a Actor) bool {
	return a.IsOwner || a.Role == RoleAdmin
}
