package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// #172 裁决（第二刀）：关键字段修改不再走变更单审批——直接修改生效，
// 动作写入任务动态并站内通知所属 KR 负责人。
// 裁决 10（#180）：关闭申请审批机制整体退场，关闭改为项目管理员直接操作
// （原因必填、即时生效、写任务动态）；修改权限收归项目管理员。

var (
	ErrChangeEmpty      = errors.New("至少要修改一项字段")
	ErrChangeForbidden  = errors.New("无权修改该任务的关键字段")
	ErrChangeNotAllowed = errors.New("任务当前状态不允许修改关键字段")
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

// TaskEditRule 直接修改任务字段的权限判定（裁决 10，#180）：
// 仅项目管理员（含项目负责人），任务处于未开始／等待输入／进行中。
func TaskEditRule(a Actor, userID int64, t TaskFacts) error {
	if !CanEditTaskConfig(a, userID, t) {
		return ErrChangeForbidden
	}
	switch t.Status {
	case TaskNotStarted, TaskWaitingInput, TaskInProgress:
	default:
		return ErrChangeNotAllowed
	}
	return nil
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

var ErrCancelForbidden = errors.New("只有项目管理员可以关闭任务")

// ErrCancelPendingExists 任务上有任一未决审批单时不能关闭（AC-57 互斥口径顺延）。
var ErrCancelPendingExists = errors.New("任务上存在未决审批单，暂不能关闭")

// PendingApprovalOnTask 报告任务上是否存在任一未决审批单（AC-57；裁决 10 后只剩两类）：
// 完成审批体现在任务状态上，成果更新在审时任务状态仍是已完成、
// 未决事实只体现在成果更新进程上（AC-66）。
func PendingApprovalOnTask(t TaskFacts) bool {
	switch t.Status {
	case TaskPendingIntermediateReview, TaskPendingFinalReview:
		return true
	}
	return ResultUpdateReviewInFlight(t)
}

// CloseTaskRule 项目管理员直接关闭任务（AC-57、裁决 10，#180）：终态不可关闭；
// 任务上有任一未决审批单时互斥；仅可编辑项目者，无审批环节、即时生效。
// 关闭原因必填由 ValidateCancelReason 另行校验。
func CloseTaskRule(a Actor, t TaskFacts) error {
	if t.Status == TaskCompleted || t.Status == TaskCancelled {
		return ErrCannotCancel
	}
	if !CanEditProject(a) {
		return ErrCancelForbidden
	}
	if PendingApprovalOnTask(t) {
		return ErrCancelPendingExists
	}
	return nil
}

// CanCloseTask 关闭入口的派生动作标志：口径与 CloseTaskRule 同源。
func CanCloseTask(a Actor, t TaskFacts) bool {
	return CloseTaskRule(a, t) == nil
}
