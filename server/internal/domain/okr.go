package domain

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrOkrBatchEmpty          = errors.New("O／KR 批量创建内容不能为空")
	ErrOkrItemAmbiguous       = errors.New("每一项必须在新建 O（title）和已有 O（objectiveId）中二选一")
	ErrOkrExistingNoKRs       = errors.New("向已有 O 追加时至少要有一条 KR")
	ErrObjectiveTitleEmpty    = errors.New("O 标题不能为空")
	ErrObjectiveTitleTooLong  = errors.New("O 标题不能超过 100 字")
	ErrObjectiveDescTooLong   = errors.New("O 说明不能超过 500 字")
	ErrKrDescriptionEmpty     = errors.New("KR 描述不能为空")
	ErrKrDescriptionTooLong   = errors.New("KR 描述不能超过 200 字")
	ErrKrMetricTooLong        = errors.New("量化指标不能超过 100 字")
	ErrKrOwnerNotMember       = errors.New("KR 负责人必须是项目成员")
	ErrKrPeriodInverted       = errors.New("KR 截止日期不能早于开始日期")
)

// DefaultKrRiskLevel 新建 KR 的初始风险等级（词汇表：风险等级；创建弹窗不提供设置入口）。
const DefaultKrRiskLevel = "normal"

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

// ValidateOkrBatch 校验整批 O／KR 创建输入（AC-01）。
// isMember 报告某用户是否为项目成员，用于 KR 负责人校验（PRD §7.2：匹配现有项目成员）。
func ValidateOkrBatch(items []OkrBatchItem, isMember func(int64) bool) error {
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
			if err := validateNewKeyResult(k, isMember); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateNewKeyResult(k NewKeyResult, isMember func(int64) bool) error {
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
	if k.OwnerID != nil && !isMember(*k.OwnerID) {
		return ErrKrOwnerNotMember
	}
	if k.Start != nil && k.End != nil && k.End.Before(*k.Start) {
		return ErrKrPeriodInverted
	}
	return nil
}
