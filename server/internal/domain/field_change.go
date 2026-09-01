package domain

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// 关键字段变更单状态（词汇表「关键字段变更单」）。
const (
	FieldChangePendingState  = "pending"
	FieldChangeApprovedState = "approved"
	FieldChangeRejectedState = "rejected"
)

// FieldChangeExemptOpinion 免审即时生效时系统记录的说明（PRD §5.2.B）。
const FieldChangeExemptOpinion = "KR 负责人本人修改，免审即时生效"

var (
	ErrChangeEmpty          = errors.New("至少要有一项拟议值")
	ErrChangeReasonRequired = errors.New("提交关键字段修改需要填写修改原因")
	ErrChangeForbidden      = errors.New("无权修改该任务的关键字段")
	ErrChangeNotAllowed     = errors.New("任务当前状态不允许修改关键字段")
	ErrChangePendingExists  = errors.New("已有待审批的关键字段修改，处理后才能再次提交")
	ErrChangeNotPending     = errors.New("变更单不在待审批状态")
	ErrChangeTaskTerminal   = errors.New("任务已终止，变更单不能再处理")
)

// KeyFieldChanges 拟议的关键字段值（nil 表示未修改）。
type KeyFieldChanges struct {
	Name               *string
	Description        *string
	CompletionCriteria *string
	OwnerID            *int64
	EndDate            *time.Time
	// Status 仅用于关闭申请：拟议值恒为「已关闭」（PRD §5.2.B）。
	Status *string
}

// Empty 报告是否没有任何拟议值。
func (c KeyFieldChanges) Empty() bool {
	return c.Name == nil && c.Description == nil && c.CompletionCriteria == nil &&
		c.OwnerID == nil && c.EndDate == nil && c.Status == nil
}

// FieldChangeOutcome 提交关键字段修改的路由结果。
type FieldChangeOutcome int

const (
	// FieldChangeExempt KR 负责人本人修改，免审即时生效并留痕。
	FieldChangeExempt FieldChangeOutcome = iota + 1
	// FieldChangePending 进入审批，旧值继续生效。
	FieldChangePending
)

// FieldEditMode 就地编辑保存路由的对外表达（#138 裁决 E1）：exempt／approval。
func FieldEditMode(o FieldChangeOutcome) string {
	switch o {
	case FieldChangeExempt:
		return "exempt"
	case FieldChangePending:
		return "approval"
	}
	return ""
}

// ValidateKeyFieldChanges 校验拟议值（§9.1：至少一项拟议值、修改原因）。
func ValidateKeyFieldChanges(c KeyFieldChanges, reason string, reasonRequired bool, roleOf func(int64) string, taskStart time.Time) error {
	if c.Empty() {
		return ErrChangeEmpty
	}
	if reasonRequired && strings.TrimSpace(reason) == "" {
		return ErrChangeReasonRequired
	}
	if c.Name != nil {
		name := strings.TrimSpace(*c.Name)
		if name == "" {
			return ErrTaskNameEmpty
		}
		if utf8.RuneCountInString(name) > 200 {
			return ErrTaskNameTooLong
		}
	}
	if c.OwnerID != nil && !eligibleOwner(roleOf(*c.OwnerID)) {
		return ErrTaskOwnerNotEligible
	}
	if c.EndDate != nil && c.EndDate.Before(taskStart) {
		return ErrTaskPeriodInverted
	}
	return nil
}

// FieldChangeRoute 路由提交结果（AC-23、§5.2.B）：
// KR 负责人本人免审即时生效，其余进入审批（同一任务最多一张待审批变更单）；
// 审批中／审核中／终态不可修改。
func FieldChangeRoute(a Actor, userID int64, t TaskFacts, hasPending bool) (FieldChangeOutcome, error) {
	if !CanWriteProject(a) {
		return 0, ErrChangeForbidden
	}
	switch t.Status {
	case TaskNotStarted, TaskWaitingInput, TaskInProgress:
		if hasPending {
			return 0, ErrChangePendingExists
		}
		if t.KrOwnerID != nil && *t.KrOwnerID == userID {
			return FieldChangeExempt, nil
		}
		// 负责人／可编辑项目者（KR 负责人已在上方免审路径）。
		if userID != t.OwnerID && !CanEditProject(a) {
			return 0, ErrChangeForbidden
		}
		return FieldChangePending, nil
	default:
		return 0, ErrChangeNotAllowed
	}
}

// DecideFieldChangeRule 变更单处理规则：仅所属 KR 负责人、仅待审批状态（管理员不可替代，§3.3）；
// 任务已进入终态时变更单不得再被处理。
func DecideFieldChangeRule(a Actor, state string, t TaskFacts, actorID int64, approve bool, opinion string) error {
	if state != FieldChangePendingState {
		return ErrChangeNotPending
	}
	if t.Status == TaskCompleted || t.Status == TaskCancelled {
		return ErrChangeTaskTerminal
	}
	if !CanWriteProject(a) || t.KrOwnerID == nil || *t.KrOwnerID != actorID {
		return ErrNotKrOwner
	}
	// MW-18：退回必须写清理由，与完成审核同口径。
	if !approve && strings.TrimSpace(opinion) == "" {
		return ErrRejectOpinionRequired
	}
	return nil
}

// CanAbandonFieldChange 判定能否放弃已退回的变更（词汇表「退回待处理事项」）。
func CanAbandonFieldChange(a Actor, userID, submitterID int64, state string, resolved bool) bool {
	if !CanWriteProject(a) || state != FieldChangeRejectedState || resolved {
		return false
	}
	return userID == submitterID || CanEditProject(a)
}

// 变更类型（PRD §5.2.B）：关键字段修改，或复用同一张变更单的任务关闭申请。
const (
	FieldChangeTypeKeyFields = "key_fields"
	FieldChangeTypeCancel    = "cancel"
)

// CancelExemptOpinion 关闭免审即时生效时系统记录的说明（AC-57）。
const CancelExemptOpinion = "KR 负责人本人负责 KR 下关闭，免审即时生效"

var (
	ErrCancelForbidden     = errors.New("只有任务负责人与项目管理员可以发起关闭")
	ErrCancelPendingExists = errors.New("任务上存在未决审批单，暂不能发起关闭")
	ErrCancelBlocked       = errors.New("关闭申请审批期间不能提交其他审批单")
)

// CancelChange 关闭申请的拟议值：任务状态 → 已关闭（旧值由任务当前状态快照）。
func CancelChange() KeyFieldChanges {
	cancelled := TaskCancelled
	return KeyFieldChanges{Status: &cancelled}
}

// PendingApprovalOnTask 报告任务上是否存在任一未决审批单：
// 完成审批体现在任务状态上，关键字段变更／取消体现在待审批变更单上（AC-57）。
func PendingApprovalOnTask(t TaskFacts, hasPendingChange bool) bool {
	switch t.Status {
	case TaskPendingIntermediateReview, TaskPendingFinalReview:
		return true
	}
	// 成果更新在审时任务状态仍是已完成，未决事实只体现在成果更新进程上（AC-66）。
	if ResultUpdateReviewInFlight(t) {
		return true
	}
	return hasPendingChange
}

// CancelRoute 路由关闭申请（AC-57、§5.1）：终态不可取消；任务上有任一未决审批单时互斥；
// 发起人限任务负责人与项目管理员；KR 负责人在本人负责 KR 下免审即时生效，其余进入审批。
func CancelRoute(a Actor, userID int64, t TaskFacts, hasPendingChange bool) (FieldChangeOutcome, error) {
	if t.Status == TaskCompleted || t.Status == TaskCancelled {
		return 0, ErrCannotCancel
	}
	if !CanWriteProject(a) {
		return 0, ErrCancelForbidden
	}
	if PendingApprovalOnTask(t, hasPendingChange) {
		return 0, ErrCancelPendingExists
	}
	if t.KrOwnerID == nil {
		return 0, ErrKrOwnerMissing
	}
	if *t.KrOwnerID == userID {
		return FieldChangeExempt, nil
	}
	if userID != t.OwnerID && !CanEditProject(a) {
		return 0, ErrCancelForbidden
	}
	return FieldChangePending, nil
}
