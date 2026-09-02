package domain

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// O／KR 的编辑、删除与成员移出前的职责检查（AC-61、AC-65；PRD §7.2、§3.4）。

var (
	ErrObjectiveHasKeyResults = errors.New("该 O 下还有 KR，请先处理下级再删除")
	ErrKeyResultHasTasks      = errors.New("该 KR 下还有任务（含已完成、已关闭），请先处理下级再删除")
	ErrOkrDeleteForbidden     = errors.New("只有项目管理员可以删除 O 与 KR")
	ErrMemberHasDuties        = errors.New("该成员仍在承担项目职责，请先交接后再移出项目")
)

// CanEditObjective 判定能否编辑 O（AC-65、§7.2）：只有项目管理员（含项目负责人）。
func CanEditObjective(a Actor) bool {
	return CanEditProject(a)
}

// CanEditKeyResult 判定能否编辑 KR（AC-65；裁决 12，#183）：仅项目管理员（含项目负责人）。
func CanEditKeyResult(a Actor) bool {
	return CanEditProject(a)
}

// CanDeleteObjective 判定能否删除 O（AC-65）：仅项目管理员，且 O 下没有 KR。
func CanDeleteObjective(a Actor, keyResultCount int) bool {
	return DeleteObjectiveRule(a, keyResultCount) == nil
}

// DeleteObjectiveRule 删除 O 的规则，错误文案即页面提示。
func DeleteObjectiveRule(a Actor, keyResultCount int) error {
	if !CanEditProject(a) {
		return ErrOkrDeleteForbidden
	}
	if keyResultCount > 0 {
		return ErrObjectiveHasKeyResults
	}
	return nil
}

// CanDeleteKeyResult 判定能否删除 KR（AC-65）：仅项目管理员，且 KR 下没有任务
// （含已完成与已关闭——它们仍是项目事实，不能随 KR 一起消失）。
func CanDeleteKeyResult(a Actor, taskCount int) bool {
	return DeleteKeyResultRule(a, taskCount) == nil
}

// DeleteKeyResultRule 删除 KR 的规则，错误文案即页面提示。
func DeleteKeyResultRule(a Actor, taskCount int) error {
	if !CanEditProject(a) {
		return ErrOkrDeleteForbidden
	}
	if taskCount > 0 {
		return ErrKeyResultHasTasks
	}
	return nil
}

// KeyResultUpdate 一次 KR 编辑的拟议值（nil 表示不改）。
// 裁决 12（#183）：KR 只剩结构字段——描述与量化指标。
type KeyResultUpdate struct {
	Description *string
	Metric      *string
}

// Empty 报告本次编辑是否什么都没改。
func (u KeyResultUpdate) Empty() bool {
	return u.Description == nil && u.Metric == nil
}

// ValidateKeyResultUpdate 校验 KR 编辑（AC-65）：描述非空且不超长、量化指标不超长。
func ValidateKeyResultUpdate(u KeyResultUpdate) error {
	if u.Description != nil {
		desc := strings.TrimSpace(*u.Description)
		if desc == "" {
			return ErrKrDescriptionEmpty
		}
		if utf8.RuneCountInString(desc) > 200 {
			return ErrKrDescriptionTooLong
		}
	}
	if u.Metric != nil && utf8.RuneCountInString(strings.TrimSpace(*u.Metric)) > 100 {
		return ErrKrMetricTooLong
	}
	return nil
}

// ObjectiveUpdate 一次 O 编辑的拟议值（nil 表示不改）。
type ObjectiveUpdate struct {
	Title       *string
	Description *string
}

// Empty 报告本次编辑是否什么都没改。
func (u ObjectiveUpdate) Empty() bool {
	return u.Title == nil && u.Description == nil
}

// ValidateObjectiveUpdate 校验 O 编辑（AC-65）：标题非空且不超长、说明不超长。
func ValidateObjectiveUpdate(u ObjectiveUpdate) error {
	if u.Title != nil {
		title := strings.TrimSpace(*u.Title)
		if title == "" {
			return ErrObjectiveTitleEmpty
		}
		if utf8.RuneCountInString(title) > 100 {
			return ErrObjectiveTitleTooLong
		}
	}
	if u.Description != nil && utf8.RuneCountInString(strings.TrimSpace(*u.Description)) > 500 {
		return ErrObjectiveDescTooLong
	}
	return nil
}

// MemberDuties 一名成员在项目里仍占着的职责（AC-21、AC-61）。
// 判定只比对 ID 是不够的：人被移出后这些职责会变成无人可处理的死锁，所以移出前必须先交接。
// 裁决 12（#183）：KR 无负责人，职责检查删除 KR 项（#178 后也无「输入对接人」）。
type MemberDuties struct {
	Tasks     []string // 仍在担任负责人的任务名
	Reviewers []string // 仍在成果审核组里的任务名
	Receivers []string // 仍是接收方的任务名
}

// Empty 报告是否没有任何职责占位。
func (d MemberDuties) Empty() bool {
	return len(d.Tasks) == 0 && len(d.Reviewers) == 0 && len(d.Receivers) == 0
}

// RemoveMemberRule 移出成员的规则：仍有职责占位时不能移出（AC-21、AC-61）。
func RemoveMemberRule(d MemberDuties) error {
	if d.Empty() {
		return nil
	}
	return ErrMemberHasDuties
}

// MemberDutiesSummary 待交接清单的一行说明，直接作为 409 的可读原因返回。
func MemberDutiesSummary(d MemberDuties) string {
	parts := []string{}
	add := func(label string, items []string) {
		if len(items) > 0 {
			parts = append(parts, label+"："+strings.Join(items, "、"))
		}
	}
	add("任务负责人", d.Tasks)
	add("成果审核人", d.Reviewers)
	add("接收方", d.Receivers)
	return strings.Join(parts, "；")
}
