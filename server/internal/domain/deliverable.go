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
	ErrFileTooLarge           = errors.New("单个文件不能超过 20MB")
	ErrFileEmpty              = errors.New("文件内容为空")
	ErrFileNameEmpty          = errors.New("文件名不能为空")
	ErrFileNameTooLong        = errors.New("文件名不能超过 200 字")
)

// ValidateDeliverableName 校验交付物项名称。
func ValidateDeliverableName(name string) error {
	n := strings.TrimSpace(name)
	if n == "" {
		return ErrDeliverableNameEmpty
	}
	if utf8.RuneCountInString(n) > 100 {
		return ErrDeliverableNameTooLong
	}
	return nil
}

// CanManageDeliverables 判定能否配置任务输出：负责人／创建人／可编辑项目者，终态不可（§3.4 配置输出）。
func CanManageDeliverables(a Actor, userID int64, t TaskFacts) bool {
	if t.Status == TaskCompleted || t.Status == TaskCancelled {
		return false
	}
	return userID == t.OwnerID || userID == t.CreatorID || CanEditProject(a)
}

// CanUploadCandidate 判定能否登记候选内容：任务负责人（管理员纠错），执行类状态；
// 完成审核期间整批候选已锁定（§5.3），不可另传。
func CanUploadCandidate(a Actor, userID int64, t TaskFacts) bool {
	switch t.Status {
	case TaskNotStarted, TaskWaitingInput, TaskInProgress:
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
