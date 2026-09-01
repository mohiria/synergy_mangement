package domain

import (
	"errors"
	"strings"
)

// 输入请求状态（词汇表「输入请求」）。
const (
	InputRequestPending  = "pending"
	InputRequestAccepted = "accepted"
	InputRequestProvided = "provided"
	// InputRequestUploading 已登记待上传：附件确认写入对象存储后才转 provided（R4 两阶段提交）。
	InputRequestUploading = "uploading"
)

// NotifyInputRequest 站内通知类型。
const NotifyInputRequest = "input_request"

var (
	ErrProviderNotEligible  = errors.New("对接人必须是非只读的项目成员")
	ErrContentNoteRequired  = errors.New("请填写所需内容说明")
	ErrExpectedDateRequired = errors.New("请填写期望时间")
	ErrNotProvider          = errors.New("只有指定的对接人可以处理该输入请求")
	ErrInputStateConflict   = errors.New("输入请求不在可处理状态")
	ErrInputNotAccepted     = errors.New("请先同意接收，再提交内容")
	ErrInputContentRequired = errors.New("请提交文字内容或文件")
)

// MemberInput 指定项目成员输入的请求输入（§9.1）。
type MemberInput struct {
	Necessity       string
	ProviderID      int64
	ContentNote     string
	HasExpectedDate bool
}

// ValidateMemberInput 校验指定项目成员输入（AC-29、§9.1：必要性、对接人、所需内容、期望时间必填）。
// 名称不再由用户填写，它由「所需内容」摘要派生（EdgeDisplayName、#112）。
func ValidateMemberInput(m MemberInput, roleOf func(int64) string) error {
	switch m.Necessity {
	case NecessityRequired, NecessityReference:
	default:
		return ErrNecessityInvalid
	}
	if role := roleOf(m.ProviderID); role != RoleAdmin && role != RoleMember {
		return ErrProviderNotEligible
	}
	if strings.TrimSpace(m.ContentNote) == "" {
		return ErrContentNoteRequired
	}
	if !m.HasExpectedDate {
		return ErrExpectedDateRequired
	}
	return nil
}

// AcceptInputRule 同意接收（AC-30）：仅对接人本人、仅待接收状态；接收只表示承担责任。
func AcceptInputRule(state string, providerID, actorID int64) error {
	if state != InputRequestPending {
		return ErrInputStateConflict
	}
	if actorID != providerID {
		return ErrNotProvider
	}
	return nil
}

// ProvideInputRule 提交内容（AC-30）：仅对接人、须先同意接收、内容或文件至少其一。
func ProvideInputRule(state string, providerID, actorID int64, hasContent bool) error {
	switch state {
	case InputRequestAccepted:
	case InputRequestPending:
		if actorID != providerID {
			return ErrNotProvider
		}
		return ErrInputNotAccepted
	default:
		return ErrInputStateConflict
	}
	if actorID != providerID {
		return ErrNotProvider
	}
	if !hasContent {
		return ErrInputContentRequired
	}
	return nil
}

// MemberEdgeReady 成员来源的交付物边就绪判定（词汇表「输入就绪」）：已提供才就绪。
func MemberEdgeReady(state string) bool {
	return state == InputRequestProvided
}

var (
	ErrProvidersEmpty     = errors.New("请至少选择一名对接人")
	ErrProviderDuplicated = errors.New("对接人不能重复选择")
)

// MemberInputs 一次配置产生的多条「项目成员 → 目标任务」输入（AC-53：对接人可多选）。
type MemberInputs struct {
	Necessity       string
	ProviderIDs     []int64
	ContentNote     string
	HasExpectedDate bool
}

// ValidateMemberInputs 校验一次多选对接人的输入配置（AC-53）：至少选一名、同一次不可重复，
// 每名对接人仍按单人规则校验（非只读项目成员、所需内容与期望时间必填）。
func ValidateMemberInputs(m MemberInputs, roleOf func(int64) string) error {
	if len(m.ProviderIDs) == 0 {
		return ErrProvidersEmpty
	}
	seen := make(map[int64]struct{}, len(m.ProviderIDs))
	for _, id := range m.ProviderIDs {
		if _, dup := seen[id]; dup {
			return ErrProviderDuplicated
		}
		seen[id] = struct{}{}
		if err := ValidateMemberInput(MemberInput{
			Necessity: m.Necessity, ProviderID: id,
			ContentNote: m.ContentNote, HasExpectedDate: m.HasExpectedDate,
		}, roleOf); err != nil {
			return err
		}
	}
	return nil
}
