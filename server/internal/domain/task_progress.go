package domain

import (
	"errors"
	"strings"
)

var (
	ErrProgressOutOfRange   = errors.New("进度百分比必须在 0～100 之间")
	ErrCannotStart          = errors.New("当前状态不能开始执行")
	ErrCannotCancel         = errors.New("已完成或已取消的任务不能取消")
	ErrCancelReasonRequired = errors.New("取消任务需要填写原因")
)

// TaskProgressFact 覆盖度计算所需的任务事实。
type TaskProgressFact struct {
	Status   string
	Progress *int
}

// ProgressSummaryFacts KR 层进度数据覆盖度（词汇表「进度数据覆盖度」）。
type ProgressSummaryFacts struct {
	TotalTasks      int
	FilledTasks     int
	AverageProgress *int
}

// ValidateProgress 校验可选进度：未填写合法，填写时 0～100（AC-12）。
func ValidateProgress(p *int) error {
	if p != nil && (*p < 0 || *p > 100) {
		return ErrProgressOutOfRange
	}
	return nil
}

// StartTask 开始执行：未开始／等待输入 → 进行中（PRD §5.1 触发动作「点击开始」）。
func StartTask(status string) (string, error) {
	if status != TaskNotStarted && status != TaskWaitingInput {
		return "", ErrCannotStart
	}
	return TaskInProgress, nil
}

// CancelTask 取消任务：非终态可取消并保留原因（PRD §5.1「任务不再执行并保留原因」）。
func CancelTask(status, reason string) error {
	if status == TaskCompleted || status == TaskCancelled {
		return ErrCannotCancel
	}
	if strings.TrimSpace(reason) == "" {
		return ErrCancelReasonRequired
	}
	return nil
}

// CanUpdateProgress 判定能否更新进度：负责人填写真实情况（§5.6），管理员／项目负责人可全局纠错；
// 仅执行中状态可改（未开始不产生进度，完成态由终审定论）。
func CanUpdateProgress(a Actor, userID int64, t TaskFacts) bool {
	if t.Status != TaskInProgress {
		return false
	}
	return userID == t.OwnerID || CanEditProject(a)
}

// ProgressCoverage 计算 KR 层进度数据覆盖度：只统计已入池且未取消的任务，
// 平均值任务等权、只算已填任务、四舍五入；不为未填任务虚构百分比（AC-12）。
func ProgressCoverage(tasks []TaskProgressFact) ProgressSummaryFacts {
	out := ProgressSummaryFacts{}
	sum := 0
	for _, t := range tasks {
		switch t.Status {
		case TaskDraft, TaskPendingPoolReview, TaskCancelled:
			continue
		}
		out.TotalTasks++
		if t.Progress != nil {
			out.FilledTasks++
			sum += *t.Progress
		}
	}
	if out.FilledTasks > 0 {
		avg := (sum + out.FilledTasks/2) / out.FilledTasks
		out.AverageProgress = &avg
	}
	return out
}

// CanStartTask 判定能否开始执行（派生动作标志）：任务负责人或可编辑项目者，且状态允许。
func CanStartTask(a Actor, userID int64, t TaskFacts) bool {
	if _, err := StartTask(t.Status); err != nil {
		return false
	}
	return userID == t.OwnerID || CanEditProject(a)
}

// CanCancelTask 判定能否取消（派生动作标志）：负责人／创建人／可编辑项目者，非终态。
func CanCancelTask(a Actor, userID int64, t TaskFacts) bool {
	if t.Status == TaskCompleted || t.Status == TaskCancelled {
		return false
	}
	return userID == t.OwnerID || userID == t.CreatorID || CanEditProject(a)
}
