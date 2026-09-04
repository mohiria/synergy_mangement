package domain

import "errors"

// ErrProjectVisibilityInvalid 项目可见性取值不在二值枚举内。
var ErrProjectVisibilityInvalid = errors.New("项目可见性不合法")

// 项目可见性（词汇表「项目可见性」；裁决 D1、#111）：默认私有，公开只放开读。
const (
	VisibilityPrivate = "private"
	VisibilityPublic  = "public"
)

var projectVisibilities = map[string]struct{}{
	VisibilityPrivate: {},
	VisibilityPublic:  {},
}

// ValidateProjectVisibility 校验项目可见性是否属于二值枚举。
func ValidateProjectVisibility(v string) error {
	if _, ok := projectVisibilities[v]; !ok {
		return ErrProjectVisibilityInvalid
	}
	return nil
}

var visibilityLabels = map[string]string{
	VisibilityPrivate: "私有项目",
	VisibilityPublic:  "公开项目",
}

// VisibilityLabel 项目可见性显示文案（派生字段，前端不按枚举拼文案）。
func VisibilityLabel(v string) string {
	if label, ok := visibilityLabels[v]; ok {
		return label
	}
	return v
}

// ProjectIdentity 当前用户对某项目的有效身份（#111）：这是全系统唯一的身份判定入口，
// handler 不各写一遍。顺序是显式身份优先——项目负责人 → 显式管理员 → 系统管理员（#200）→
// 成员角色 → 公开项目的隐式访客 → 无身份。隐式访客只在项目公开且此人没有任何显式身份时产生，
// 读权限与显式访客相同，写动作一律不放行（含发表讨论），也不能被指定为接收方，差别都靠
// Implicit 这一位表达。
//
// 系统管理员对任意项目视同项目管理员（可查看、编辑、进项目设置、管成员），权限判定不低于
// 其显式成员角色；SystemAdmin 位只说明这份管理员权限来自系统标记。它不是成员表里的身份：
// 审批链、成员列表、人员选择器与统计都只读成员表，因此隐式身份天然不进审批链（ADR 0003）。
func ProjectIdentity(userID, ownerID int64, memberRole, visibility string, systemAdmin bool) Actor {
	if userID == ownerID {
		return Actor{IsOwner: true}
	}
	if memberRole == RoleAdmin {
		return Actor{Role: RoleAdmin}
	}
	if systemAdmin {
		return Actor{Role: RoleAdmin, SystemAdmin: true}
	}
	if _, ok := memberRoles[memberRole]; ok {
		return Actor{Role: memberRole}
	}
	if visibility == VisibilityPublic {
		return Actor{Role: RoleViewer, Implicit: true}
	}
	return Actor{}
}
