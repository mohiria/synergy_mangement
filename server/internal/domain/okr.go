package domain

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrOkrBatchEmpty         = errors.New("O／KR 批量创建内容不能为空")
	ErrOkrItemAmbiguous      = errors.New("每一项必须在新建 O（title）和已有 O（objectiveId）中二选一")
	ErrOkrExistingNoKRs      = errors.New("向已有 O 追加时至少要有一条 KR")
	ErrObjectiveTitleEmpty   = errors.New("O 标题不能为空")
	ErrObjectiveTitleTooLong = errors.New("O 标题不能超过 100 字")
	ErrObjectiveDescTooLong  = errors.New("O 说明不能超过 500 字")
	ErrKrDescriptionEmpty    = errors.New("KR 描述不能为空")
	ErrKrDescriptionTooLong  = errors.New("KR 描述不能超过 200 字")
	ErrKrMetricTooLong       = errors.New("量化指标不能超过 100 字")
	ErrKrOwnerNotEligible    = errors.New("KR 负责人必须是非只读的项目成员")
	ErrKrPeriodInverted      = errors.New("KR 截止日期不能早于开始日期")
)

// NewKeyResult 待创建的 KR 输入（词汇表：KR）。
type NewKeyResult struct {
	Description string
	Metric      string
	OwnerID     *int64
	Start       *time.Time
	End         *time.Time
}

// OkrBatchItem 表格式批量创建中的一项：
// 新建 O（Title 非空，可带 Description 与 KeyResults），
// 或向已有 O 追加 KR（ObjectiveID 非空，KeyResults 至少一条）。
type OkrBatchItem struct {
	ObjectiveID *int64
	Title       string
	Description string
	KeyResults  []NewKeyResult
}

// eligibleOwner 报告某个项目角色能否承担负责人职责：访客与非成员不可（§3.4、S2）。
func eligibleOwner(role string) bool {
	return role == RoleAdmin || role == RoleMember
}

// ValidateOkrBatch 校验整批 O／KR 创建输入（AC-01）。
// roleOf 返回某用户在本项目的角色，用于 KR 负责人校验（§7.2 匹配现有项目成员；§3.4 排除只读）。
func ValidateOkrBatch(items []OkrBatchItem, roleOf func(int64) string) error {
	if len(items) == 0 {
		return ErrOkrBatchEmpty
	}
	for _, item := range items {
		hasTitle := strings.TrimSpace(item.Title) != ""
		switch {
		case item.ObjectiveID != nil && item.Title != "":
			return ErrOkrItemAmbiguous
		case item.ObjectiveID == nil && item.Title == "":
			return ErrOkrItemAmbiguous
		case item.ObjectiveID != nil:
			if len(item.KeyResults) == 0 {
				return ErrOkrExistingNoKRs
			}
		default:
			if !hasTitle {
				return ErrObjectiveTitleEmpty
			}
			if utf8.RuneCountInString(strings.TrimSpace(item.Title)) > 100 {
				return ErrObjectiveTitleTooLong
			}
			if utf8.RuneCountInString(strings.TrimSpace(item.Description)) > 500 {
				return ErrObjectiveDescTooLong
			}
		}
		for _, k := range item.KeyResults {
			if err := validateNewKeyResult(k, roleOf); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateNewKeyResult(k NewKeyResult, roleOf func(int64) string) error {
	desc := strings.TrimSpace(k.Description)
	if desc == "" {
		return ErrKrDescriptionEmpty
	}
	if utf8.RuneCountInString(desc) > 200 {
		return ErrKrDescriptionTooLong
	}
	if utf8.RuneCountInString(strings.TrimSpace(k.Metric)) > 100 {
		return ErrKrMetricTooLong
	}
	if k.OwnerID != nil && !eligibleOwner(roleOf(*k.OwnerID)) {
		return ErrKrOwnerNotEligible
	}
	if k.Start != nil && k.End != nil && k.End.Before(*k.Start) {
		return ErrKrPeriodInverted
	}
	return nil
}
