package domain

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// 交付内容状态（词汇表「当前交付物」「候选交付物」）。
const (
	DeliverableCurrent   = "current"
	DeliverableCandidate = "candidate"
	// DeliverableUploading 已建记录、文件尚未确认写入对象存储；不参与就绪与审核（R4 两阶段提交）。
	DeliverableUploading = "uploading"
)

var (
	ErrDeliverableNameEmpty   = errors.New("交付物名称不能为空")
	ErrDeliverableNameTooLong = errors.New("交付物名称不能超过 100 字")
	// ErrDeliverableNameDuplicate 同一任务下已有同名交付物项：新增要挡下，更新内容请走已有项（裁决 G1）。
	ErrDeliverableNameDuplicate = errors.New("同名交付物项已存在，请在该项上更新内容")
	ErrFileTooLarge           = errors.New("单个文件不能超过 20MB")
	ErrFileEmpty              = errors.New("文件内容为空")
	ErrFileNameEmpty          = errors.New("文件名不能为空")
	ErrFileNameTooLong        = errors.New("文件名不能超过 200 字")
)

// deliverableNameRunes 交付物项名长度上限。
const deliverableNameRunes = 100

// ValidateDeliverableName 校验交付物项名称。
func ValidateDeliverableName(name string) error {
	n := strings.TrimSpace(name)
	if n == "" {
		return ErrDeliverableNameEmpty
	}
	if utf8.RuneCountInString(n) > deliverableNameRunes {
		return ErrDeliverableNameTooLong
	}
	return nil
}

// CanManageDeliverables 判定能否配置任务输出：负责人／项目管理员，终态不可
// （裁决 10，#180：交付物项是上传交付物的入口，负责人保留）。
func CanManageDeliverables(a Actor, userID int64, t TaskFacts) bool {
	if t.Status == TaskCompleted || t.Status == TaskCancelled {
		return false
	}
	return OwnerOrProjectAdmin(a, userID, t)
}

// CanUploadCandidate 判定能否登记候选内容：任务负责人（管理员纠错），执行类状态；
// 完成审核期间整批候选已锁定（§5.3），不可另传。已完成任务只在成果更新已发起、
// 尚未随完成申请提交时放行（AC-66）——这是「已生效 · 有更新审核中」的唯一入口。
func CanUploadCandidate(a Actor, userID int64, t TaskFacts) bool {
	if !CanWriteProject(a) {
		return false
	}
	switch t.Status {
	case TaskNotStarted, TaskWaitingInput, TaskInProgress:
	case TaskCompleted:
		if t.ResultUpdate != ResultUpdateOpen {
			return false
		}
	default:
		return false
	}
	return userID == t.OwnerID || CanEditProject(a)
}

// MaxUploadSize 单个上传文件的大小上限（与前端提示一致的 20MB）。
const MaxUploadSize int64 = 20 << 20

// ValidateUploadSize 校验对象存储中的真实文件大小。
func ValidateUploadSize(size int64) error {
	if size <= 0 {
		return ErrFileEmpty
	}
	if size > MaxUploadSize {
		return ErrFileTooLarge
	}
	return nil
}

// ValidateCandidateFileName 校验候选内容文件名。
func ValidateCandidateFileName(name string) error {
	n := strings.TrimSpace(name)
	if n == "" {
		return ErrFileNameEmpty
	}
	if utf8.RuneCountInString(n) > 200 {
		return ErrFileNameTooLong
	}
	return nil
}

// 归档视角的交付物内容状态（AC-17；词汇表「当前交付物」「候选交付物」）。
// 成果归档列表按项列出，一行要同时说清「现在能用的是什么」和「有没有在审的更新」，
// 因此已生效之上叠加候选单列一档，不与纯审核中混为一谈。
const (
	ContentStateEmpty         = "empty"
	ContentStatePendingSubmit = "pending_submit"
	ContentStateReviewing     = "reviewing"
	ContentStateEffective     = "effective"
	ContentStateUpdating      = "updating"
)

// contentStateLabels 内容状态的中文显示文案。
var contentStateLabels = map[string]string{
	ContentStateEmpty:         "未提交",
	ContentStatePendingSubmit: "待提交审核",
	ContentStateReviewing:     "审核中",
	ContentStateEffective:     "已生效",
	ContentStateUpdating:      "已生效 · 有更新审核中",
}

// DeriveContentState 由当前内容、候选内容与未决完成申请派生内容状态（读时派生，不落库）。
// 「在审」以存在未决完成申请为准，不以候选文件在不在为准（§5.3、AC-33、AC-67）：
// 传了没提交只是「待提交审核」，说成审核中会让 KR 负责人去找根本不存在的待办审批件。
func DeriveContentState(hasCurrent, hasCandidate, hasPendingReview bool) string {
	switch {
	case hasCandidate && !hasPendingReview:
		return ContentStatePendingSubmit
	case hasCurrent && hasCandidate:
		return ContentStateUpdating
	case hasCurrent:
		return ContentStateEffective
	case hasCandidate:
		return ContentStateReviewing
	default:
		return ContentStateEmpty
	}
}

// ContentStateLabel 内容状态显示文案（派生字段）；未知取值不回显枚举原文。
func ContentStateLabel(state string) string {
	if label, ok := contentStateLabels[state]; ok {
		return label
	}
	return contentStateLabels[ContentStateEmpty]
}
