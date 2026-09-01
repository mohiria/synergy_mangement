package domain

import (
	"errors"
	"strings"
)

var (
	ErrProgressOutOfRange   = errors.New("进度百分比必须在 0～100 之间")
	ErrCannotStart          = errors.New("当前状态不能开始执行")
	ErrCannotCancel         = errors.New("已完成或已关闭的任务不能取消")
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

// ValidateCancelReason 关闭原因必填（PRD §5.1「任务不再执行并保留原因」；AC-57）。
// 能否取消由 CancelRoute 判定，本函数只管原因。
func ValidateCancelReason(reason string) error {
	if strings.TrimSpace(reason) == "" {
		return ErrCancelReasonRequired
	}
	return nil
}

// CanUpdateProgress 判定能否更新进度：负责人填写真实情况（§5.6），管理员／项目负责人可全局纠错；
// 仅执行中状态可改——未开始不产生进度，已完成由终审定论并锁定为 100（AC-63）。
func CanUpdateProgress(a Actor, userID int64, t TaskFacts) bool {
	if !CanWriteProject(a) || t.Status != TaskInProgress {
		return false
	}
	return userID == t.OwnerID || CanEditProject(a)
}

// ProgressCoverage 计算 KR 层进度汇总与数据覆盖度（AC-12、AC-63、§5.6）：
// 统计范围是未取消的任务，已关闭整体剔除（不进分子也不进分母）；
// 分母为该范围内全部任务、任务等权，未填按 0 计入，已完成一律按 100；
// FilledTasks 只数真实填写，用来说明这个平均值里有多少来自负责人填的值。
func ProgressCoverage(tasks []TaskProgressFact) ProgressSummaryFacts {
	out := ProgressSummaryFacts{}
	sum := 0
	for _, t := range tasks {
		if t.Status == TaskCancelled {
			continue
		}
		out.TotalTasks++
		if t.Status == TaskCompleted {
			// 完成即 100：任务只有一个进度事实，不看库里存了什么。
			sum += CompletedProgress()
			out.FilledTasks++
			continue
		}
		if t.Progress != nil {
			out.FilledTasks++
			sum += *t.Progress
		}
	}
	if out.TotalTasks > 0 {
		avg := (sum + out.TotalTasks/2) / out.TotalTasks
		out.AverageProgress = &avg
	}
	return out
}

// CanStartTask 判定能否开始执行（派生动作标志）：任务负责人或可编辑项目者，且状态允许。
func CanStartTask(a Actor, userID int64, t TaskFacts) bool {
	if !CanWriteProject(a) {
		return false
	}
	if _, err := StartTask(t.Status); err != nil {
		return false
	}
	return userID == t.OwnerID || CanEditProject(a)
}

// CanCancelTask 判定能否发起关闭申请（派生动作标志，AC-57）：口径与 CancelRoute 同源。
func CanCancelTask(a Actor, userID int64, t TaskFacts, hasPendingChange bool) bool {
	_, err := CancelRoute(a, userID, t, hasPendingChange)
	return err == nil
}

// CompletedProgress 完成终审通过时写入并锁定的进度（AC-63：完成即 100%）。
func CompletedProgress() int { return 100 }

// DisplayProgress 页面展示用的进度（AC-63、§5.6）：已完成一律 100；
// 其余状态原样返回负责人填的值，未填就是未填——不把汇总里的 0 显示成负责人填的值。
func DisplayProgress(status string, stored *int) *int {
	if status == TaskCompleted {
		v := CompletedProgress()
		return &v
	}
	return stored
}
