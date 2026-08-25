package domain

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrProjectNameEmpty     = errors.New("项目名称不能为空")
	ErrProjectNameTooLong   = errors.New("项目名称不能超过 100 字")
	ErrProjectStatusInvalid = errors.New("项目状态不合法")
	ErrProjectStageTooLong  = errors.New("项目阶段不能超过 50 字")
	ErrProjectPlanInverted  = errors.New("计划完成日期不能早于计划开始日期")
)

// DefaultProjectStatus 新建项目的初始状态（词汇表：项目状态）。
const DefaultProjectStatus = "not_started"

// projectStatuses 项目状态四值枚举：未开始／进行中／已完成／已归档（PRD §0.4 V4.4.2）。
var projectStatuses = map[string]struct{}{
	"not_started": {},
	"in_progress": {},
	"completed":   {},
	"archived":    {},
}

// ValidateProjectName 校验项目名称：去除首尾空白后非空且不超过 100 字（契约 CreateProjectRequest）。
func ValidateProjectName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ErrProjectNameEmpty
	}
	if utf8.RuneCountInString(trimmed) > 100 {
		return ErrProjectNameTooLong
	}
	return nil
}

// ValidateProjectStatus 校验项目状态是否属于四值枚举。
func ValidateProjectStatus(status string) error {
	if _, ok := projectStatuses[status]; !ok {
		return ErrProjectStatusInvalid
	}
	return nil
}

// ValidateProjectStage 校验项目阶段：选填，非空时不超过 50 字。
func ValidateProjectStage(stage string) error {
	if utf8.RuneCountInString(strings.TrimSpace(stage)) > 50 {
		return ErrProjectStageTooLong
	}
	return nil
}

// ValidateProjectPlan 校验计划周期：开始、完成日期均选填；两者都填时完成不得早于开始。
func ValidateProjectPlan(start, end *time.Time) error {
	if start != nil && end != nil && end.Before(*start) {
		return ErrProjectPlanInverted
	}
	return nil
}
