package domain

import "errors"

// 项目规则设置（词汇表「项目规则设置」；主 PRD §7.9、我的工作 PRD §8.8；AC-60）。
// 按项目生效、仅项目管理员可改，均有默认值；阈值不写死在代码里，卡点派生、我的工作与
// 一键提醒冷却读同一份值。

// 规则设置默认值。
const (
	DefaultApprovalTimeoutDays = 3
	DefaultDueSoonDays         = 3
	DefaultRemindDailyLimit    = 1
)

// 规则设置取值范围（契约 UpdateProjectSettingsRequest）。
const (
	maxApprovalTimeoutDays = 30
	maxDueSoonDays         = 30
	maxRemindDailyLimit    = 20
)

var (
	ErrApprovalTimeoutDaysRange = errors.New("审批超时阈值取值须在 1～30 天之间")
	ErrDueSoonDaysRange         = errors.New("临期阈值取值须在 1～30 天之间")
	ErrRemindDailyLimitRange    = errors.New("提醒冷却次数须在 1～20 次之间")
)

// ProjectSettings 一个项目的规则设置。
type ProjectSettings struct {
	ApprovalTimeoutDays int
	DueSoonDays         int
	RemindDailyLimit    int
}

// DefaultProjectSettings 新建项目的规则设置。
func DefaultProjectSettings() ProjectSettings {
	return ProjectSettings{
		ApprovalTimeoutDays: DefaultApprovalTimeoutDays,
		DueSoonDays:         DefaultDueSoonDays,
		RemindDailyLimit:    DefaultRemindDailyLimit,
	}
}

// NormalizeProjectSettings 把非正数逐项补成默认值，供读路径兜底：
// 阈值缺值时若原样使用会把全部审批件判成超时，宁可回落默认。
func NormalizeProjectSettings(s ProjectSettings) ProjectSettings {
	d := DefaultProjectSettings()
	if s.ApprovalTimeoutDays <= 0 {
		s.ApprovalTimeoutDays = d.ApprovalTimeoutDays
	}
	if s.DueSoonDays <= 0 {
		s.DueSoonDays = d.DueSoonDays
	}
	if s.RemindDailyLimit <= 0 {
		s.RemindDailyLimit = d.RemindDailyLimit
	}
	return s
}

// ValidateProjectSettings 校验三项取值范围（写路径，不做回落）。
func ValidateProjectSettings(s ProjectSettings) error {
	if s.ApprovalTimeoutDays < 1 || s.ApprovalTimeoutDays > maxApprovalTimeoutDays {
		return ErrApprovalTimeoutDaysRange
	}
	if s.DueSoonDays < 1 || s.DueSoonDays > maxDueSoonDays {
		return ErrDueSoonDaysRange
	}
	if s.RemindDailyLimit < 1 || s.RemindDailyLimit > maxRemindDailyLimit {
		return ErrRemindDailyLimitRange
	}
	return nil
}

// CanEditProjectSettings 判定能否修改规则设置：仅项目管理员（主 PRD §7.9）；
// 项目负责人为独立角色、享有同等权限。
func CanEditProjectSettings(a Actor) bool { return CanEditProject(a) }
