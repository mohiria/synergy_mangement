package domain

import (
	"errors"
	"strings"
)

// 结构化卡点（词汇表「结构化卡点」）。
const (
	BlockerInputMissing    = "input_missing"
	BlockerApprovalWaiting = "approval_waiting"
	BlockerResource        = "resource"
	BlockerOther           = "other"

	BlockerOpen     = "open"
	BlockerResolved = "resolved"
)

// NotifyBlockerRemind 站内通知类型。
const NotifyBlockerRemind = "blocker_remind"

var (
	ErrBlockerKindInvalid        = errors.New("卡点类型不合法")
	ErrBlockerMissingRequired    = errors.New("请填写缺失的输入或条件")
	ErrBlockerReasonRequired     = errors.New("请填写阻塞原因")
	ErrBlockerActionOwnerInvalid = errors.New("希望行动人必须是项目成员")
	ErrBlockerLevelInvalid       = errors.New("卡点等级只能是预警或高风险")
	ErrBlockerNotOpen            = errors.New("卡点不在开放状态")
)

// NewBlocker 上报卡点的输入（§9.1）。
type NewBlocker struct {
	Kind          string
	Missing       string
	Reason        string
	ActionOwnerID int64
	Level         string
}

// BlockerFacts 卡点动作判定所需事实。
type BlockerFacts struct {
	State         string
	CreatedBy     int64
	ActionOwnerID int64
	TaskOwnerID   int64
}

// ValidateBlocker 校验上报卡点（AC-11、§9.1：类型、缺失输入／条件、阻塞原因、希望行动人必填）；
// 结构化卡点只有预警或高风险两级（词汇表）。
func ValidateBlocker(b NewBlocker, isMember func(int64) bool) error {
	switch b.Kind {
	case BlockerInputMissing, BlockerApprovalWaiting, BlockerResource, BlockerOther:
	default:
		return ErrBlockerKindInvalid
	}
	if strings.TrimSpace(b.Missing) == "" {
		return ErrBlockerMissingRequired
	}
	if strings.TrimSpace(b.Reason) == "" {
		return ErrBlockerReasonRequired
	}
	if !isMember(b.ActionOwnerID) {
		return ErrBlockerActionOwnerInvalid
	}
	if b.Level != "warning" && b.Level != "high_risk" {
		return ErrBlockerLevelInvalid
	}
	return nil
}

// CanReportBlocker 执行者上报（§8.4）：负责人／创建人／可编辑项目者，已入池非终态。
func CanReportBlocker(a Actor, userID int64, t TaskFacts) bool {
	switch t.Status {
	case TaskNotStarted, TaskWaitingInput, TaskInProgress, TaskPendingIntermediateReview, TaskPendingFinalReview:
	default:
		return false
	}
	return userID == t.OwnerID || userID == t.CreatorID || CanEditProject(a)
}

// CanRemindBlocker 一键提醒：开放中，上报人／任务负责人／可编辑项目者；行动人本人无需提醒自己。
func CanRemindBlocker(a Actor, userID int64, b BlockerFacts) bool {
	if b.State != BlockerOpen || userID == b.ActionOwnerID {
		return false
	}
	return userID == b.CreatedBy || userID == b.TaskOwnerID || CanEditProject(a)
}

// CanResolveBlocker 解除：开放中，上报人／希望行动人／任务负责人／可编辑项目者。
func CanResolveBlocker(a Actor, userID int64, b BlockerFacts) bool {
	if b.State != BlockerOpen {
		return false
	}
	return userID == b.CreatedBy || userID == b.ActionOwnerID || userID == b.TaskOwnerID || CanEditProject(a)
}
