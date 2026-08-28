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
}

// Empty 报告是否没有任何拟议值。
func (c KeyFieldChanges) Empty() bool {
	return c.Name == nil && c.Description == nil && c.CompletionCriteria == nil && c.OwnerID == nil && c.EndDate == nil
}

// FieldChangeOutcome 提交关键字段修改的路由结果。
type FieldChangeOutcome int

const (
	// FieldChangeDirect 草稿直接完善，不生成变更单。
	FieldChangeDirect FieldChangeOutcome = iota + 1
	// FieldChangeExempt KR 负责人本人修改，免审即时生效并留痕。
	FieldChangeExempt
	// FieldChangePending 进入审批，旧值继续生效。
	FieldChangePending
)

// ValidateKeyFieldChanges 校验拟议值（§9.1：至少一项拟议值、修改原因；草稿直接完善不要求原因）。
func ValidateKeyFieldChanges(c KeyFieldChanges, reason string, reasonRequired bool, isMember func(int64) bool, taskStart time.Time) error {
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
	if c.OwnerID != nil && !isMember(*c.OwnerID) {
		return ErrTaskOwnerNotMember
	}
	if c.EndDate != nil && c.EndDate.Before(taskStart) {
		return ErrTaskPeriodInverted
	}
	return nil
}

// FieldChangeRoute 路由提交结果（AC-23、§5.2.B）：
// 草稿由创建人／负责人／可编辑项目者直接完善；已入池任务 KR 负责人本人免审即时生效，
// 其余进入审批（同一任务最多一张待审批变更单）；审批中／审核中／终态不可修改。
func FieldChangeRoute(a Actor, userID int64, t TaskFacts, hasPending bool) (FieldChangeOutcome, error) {
	editorAllowed := userID == t.CreatorID || userID == t.OwnerID || CanEditProject(a)
	switch t.Status {
	case TaskDraft:
		if !editorAllowed {
			return 0, ErrChangeForbidden
		}
		return FieldChangeDirect, nil
	case TaskNotStarted, TaskWaitingInput, TaskInProgress:
		if hasPending {
			return 0, ErrChangePendingExists
		}
		if t.KrOwnerID != nil && *t.KrOwnerID == userID {
			return FieldChangeExempt, nil
		}
		if !editorAllowed {
			return 0, ErrChangeForbidden
		}
		return FieldChangePending, nil
	default:
		return 0, ErrChangeNotAllowed
	}
}

// DecideFieldChangeRule 变更单处理规则：仅所属 KR 负责人、仅待审批状态（管理员不可替代，§3.3）；
// 任务已进入终态时变更单不得再被处理。
func DecideFieldChangeRule(state string, t TaskFacts, actorID int64, approve bool, opinion string) error {
	if state != FieldChangePendingState {
		return ErrChangeNotPending
	}
	if t.Status == TaskCompleted || t.Status == TaskCancelled {
		return ErrChangeTaskTerminal
	}
	if t.KrOwnerID == nil || *t.KrOwnerID != actorID {
		return ErrNotKrOwner
	}
	// MW-18：退回必须写清理由，与入池审批、完成审核同口径。
	if !approve && strings.TrimSpace(opinion) == "" {
		return ErrRejectOpinionRequired
	}
	return nil
}

// CanAbandonFieldChange 判定能否放弃已退回的变更（词汇表「退回待处理事项」）。
func CanAbandonFieldChange(a Actor, userID, submitterID int64, state string, resolved bool) bool {
	if state != FieldChangeRejectedState || resolved {
		return false
	}
	return userID == submitterID || CanEditProject(a)
}
