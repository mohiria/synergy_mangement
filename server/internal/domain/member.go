package domain

import "errors"

var ErrMemberRoleInvalid = errors.New("成员角色不合法")

// 成员角色三值枚举（词汇表「成员角色」；PRD §3.2 系统权限，V4.4.3 取消可编辑成员）。
const (
	RoleAdmin  = "admin"
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
	RoleMember: {},
	RoleViewer: {},
}

// ValidateMemberRole 校验成员角色是否属于三值枚举。
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

// CanEditProject 判定能否编辑项目基础信息与配置：与管理成员同一规则（PRD §3.4）。
func CanEditProject(a Actor) bool {
	return a.IsOwner || a.Role == RoleAdmin
}

// CanWriteProject 判定当前身份是否具备写入资格：项目负责人、管理员或项目成员。
// 访客与已被移出项目的人一律不可写（§3.4 权限矩阵「只读列全部为 —」）。
// 这是全部 Can／Decide 判定的前置：任务负责人、KR 负责人、中间审核人这些职责
// 只在人还是非只读项目成员时才生效——成员被移除或降为只读后，职责随之失效（S2）。
// 唯一例外是「确认接收」，访客被指定为接收方时可以确认（AC-62）。
func CanWriteProject(a Actor) bool {
	return a.IsOwner || a.Role == RoleAdmin || a.Role == RoleMember
}

// CanReadProject 判定能否读取项目内容：项目内成员或项目负责人（PRD §3.3）。
// 非成员一律不可读，读越权与写越权同一道边界。
func CanReadProject(a Actor) bool {
	if a.IsOwner {
		return true
	}
	_, ok := memberRoles[a.Role]
	return ok
}
