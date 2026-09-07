package domain

import "errors"

// ErrSystemAdminRequired 非系统管理员访问系统设置。
var ErrSystemAdminRequired = errors.New("仅系统管理员可访问系统设置")

// CanAccessSystemSettings 系统设置（基本信息、通知设置、用户管理、操作审计）读写的准入（#201）：
// 只看系统管理员标记，与任何项目角色无关（模块 PRD §7、§11）。
func CanAccessSystemSettings(isSystemAdmin bool) error {
	if !isSystemAdmin {
		return ErrSystemAdminRequired
	}
	return nil
}

// CanEditSystemSettings 系统设置基本信息能否逐字段编辑（#219）：派生字段 SystemSettings.canEdit，
// 只看系统管理员标记；前端只消费该字段，不判断角色。
func CanEditSystemSettings(isSystemAdmin bool) bool {
	return isSystemAdmin
}
