package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// #172 裁决（第二刀）：关键字段修改不再走变更单审批——有编辑权限者直接修改生效，
// 动作写入任务动态并站内通知所属 KR 负责人。任务关闭的审批独立保留为「关闭申请」，
// 原因必填，仍由所属 KR 负责人处理（本人负责 KR 下免审）。

// 关闭申请状态（词汇表「关闭申请」；存储沿用 pending/approved/rejected）。
const (
	CancelRequestPendingState  = "pending"
	CancelRequestApprovedState = "approved"
	CancelRequestRejectedState = "rejected"
)

var (
	ErrChangeEmpty        = errors.New("至少要修改一项字段")
	ErrChangeForbidden    = errors.New("无权修改该任务的关键字段")
	ErrChangeNotAllowed   = errors.New("任务当前状态不允许修改关键字段")
	ErrCancelNotPending   = errors.New("关闭申请不在待审批状态")
	ErrCancelTaskTerminal = errors.New("任务已终止，关闭申请不能再处理")
)

// KeyFieldChanges 本次修改的字段值（nil 表示未修改）。
type KeyFieldChanges struct {
	Name               *string
	Description        *string
	CompletionCriteria *string
	OwnerID            *int64
	EndDate            *time.Time
}

// Empty 报告是否没有任何修改项。
func (c KeyFieldChanges) Empty() bool {
	return c.Name == nil && c.Description == nil && c.CompletionCriteria == nil &&
		c.OwnerID == nil && c.EndDate == nil
}

// ValidateKeyFieldChanges 校验修改值（§9.1：至少一项、名称与负责人与周期约束）。
func ValidateKeyFieldChanges(c KeyFieldChanges, roleOf func(int64) string, taskStart time.Time) error {
	if c.Empty() {
		return ErrChangeEmpty
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

// TaskEditRule 直接修改任务字段的权限判定（#172 裁决）：
// 负责人／KR 负责人／可编辑项目者，任务处于未开始／等待输入／进行中；
// 关闭申请审批期间不能修改（AC-57 互斥口径顺延）。
func TaskEditRule(a Actor, userID int64, t TaskFacts, hasPendingCancel bool) error {
	if !CanWriteProject(a) {
		return ErrChangeForbidden
	}
	switch t.Status {
	case TaskNotStarted, TaskWaitingInput, TaskInProgress:
	default:
		return ErrChangeNotAllowed
	}
	if hasPendingCancel {
		return ErrCancelBlocked
	}
	if userID == t.OwnerID || (t.KrOwnerID != nil && *t.KrOwnerID == userID) || CanEditProject(a) {
		return nil
	}
	return ErrChangeForbidden
}

// NotifyTaskFieldEdited 站内通知类型：任务字段直接修改生效（#172 裁决）。
const NotifyTaskFieldEdited = "task_field_edited"

// FieldEditNotifyTarget 字段修改生效后的站内通知对象：所属 KR 负责人；
// 修改人本人是 KR 负责人或 KR 无负责人时不发（与入池通知同模式，#162）。
func FieldEditNotifyTarget(editorID int64, krOwnerID *int64) *int64 {
	if krOwnerID == nil || *krOwnerID == editorID {
		return nil
	}
	id := *krOwnerID
	return &id
}

// FieldEditNotification 字段修改站内通知内容（派生文案，前端不拼算）。
func FieldEditNotification(editorName, taskName string, fieldLabels []string) string {
	return fmt.Sprintf("%s 修改了任务「%s」的%s", editorName, taskName, strings.Join(fieldLabels, "、"))
}

// DecideCancelRequestRule 关闭申请处理规则：仅所属 KR 负责人、仅待审批状态（管理员不可替代，§3.3）；
// 任务已进入终态时不得再处理。
func DecideCancelRequestRule(a Actor, state string, t TaskFacts, actorID int64, approve bool, opinion string) error {
	if state != CancelRequestPendingState {
		return ErrCancelNotPending
	}
	if t.Status == TaskCompleted || t.Status == TaskCancelled {
		return ErrCancelTaskTerminal
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

// CanAbandonCancelRequest 判定能否放弃已退回的关闭申请（词汇表「退回待处理事项」）。
func CanAbandonCancelRequest(a Actor, userID, submitterID int64, state string, resolved bool) bool {
	if !CanWriteProject(a) || state != CancelRequestRejectedState || resolved {
		return false
	}
	return userID == submitterID || CanEditProject(a)
}

// FieldChangeTypeCancel 关闭申请在存储表 field_change_requests 里的类型值
// （#172 裁决后表中仅剩此一类，历史键值保持不变）。
const FieldChangeTypeCancel = "cancel"

// CancelOutcome 关闭申请的路由结果。
type CancelOutcome int

const (
	// CancelExempt KR 负责人本人负责 KR 下关闭，免审即时生效并留痕。
	CancelExempt CancelOutcome = iota + 1
	// CancelPending 进入所属 KR 负责人审批。
	CancelPending
)

// CancelExemptOpinion 关闭免审即时生效时系统记录的说明（AC-57）。
const CancelExemptOpinion = "KR 负责人本人负责 KR 下关闭，免审即时生效"

var (
	ErrCancelForbidden     = errors.New("只有任务负责人与项目管理员可以发起关闭")
	ErrCancelPendingExists = errors.New("任务上存在未决审批单，暂不能发起关闭")
	ErrCancelBlocked       = errors.New("关闭申请审批期间不能提交其他审批单")
)

// PendingApprovalOnTask 报告任务上是否存在任一未决审批单（AC-57）：
// 完成审批体现在任务状态上，关闭申请体现在待审批单上（#172 裁决后仅剩这两类）。
func PendingApprovalOnTask(t TaskFacts, hasPendingCancel bool) bool {
	switch t.Status {
	case TaskPendingIntermediateReview, TaskPendingFinalReview:
		return true
	}
	// 成果更新在审时任务状态仍是已完成，未决事实只体现在成果更新进程上（AC-66）。
	if ResultUpdateReviewInFlight(t) {
		return true
	}
	return hasPendingCancel
}

// CancelRoute 路由关闭申请（AC-57、§5.1）：终态不可关闭；任务上有任一未决审批单时互斥；
// 发起人限任务负责人与项目管理员；KR 负责人在本人负责 KR 下免审即时生效，其余进入审批。
func CancelRoute(a Actor, userID int64, t TaskFacts, hasPendingCancel bool) (CancelOutcome, error) {
	if t.Status == TaskCompleted || t.Status == TaskCancelled {
		return 0, ErrCannotCancel
	}
	if !CanWriteProject(a) {
		return 0, ErrCancelForbidden
	}
	if PendingApprovalOnTask(t, hasPendingCancel) {
		return 0, ErrCancelPendingExists
	}
	if t.KrOwnerID == nil {
		return 0, ErrKrOwnerMissing
	}
	if *t.KrOwnerID == userID {
		return CancelExempt, nil
	}
	if userID != t.OwnerID && !CanEditProject(a) {
		return 0, ErrCancelForbidden
	}
	return CancelPending, nil
}
