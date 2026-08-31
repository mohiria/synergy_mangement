package domain

import (
	"errors"
	"strings"
)

var (
	ErrReviewerNotEligible       = errors.New("成果审核人必须是非只读的项目成员")
	ErrNotReviewer               = errors.New("只有或签组内的成果审核人可以处理")
	ErrCompletionNotIntermediate = errors.New("完成申请不在成果审核状态")
)

// ValidateReviewers 校验成果审核组：非只读项目成员；空配置＝不设成果审核（§5.4）。
func ValidateReviewers(userIDs []int64, roleOf func(int64) string) error {
	for _, id := range userIDs {
		if role := roleOf(id); role != RoleAdmin && role != RoleMember {
			return ErrReviewerNotEligible
		}
	}
	return nil
}

// CanManageReviewers 判定能否调整成果审核人配置：负责人／创建人／可编辑项目者；
// 审核期间与终态不可调整（配置随提交已快照进申请）。
func CanManageReviewers(a Actor, userID int64, t TaskFacts) bool {
	if !CanWriteProject(a) {
		return false
	}
	switch t.Status {
	case TaskPendingIntermediateReview, TaskPendingFinalReview, TaskCompleted, TaskCancelled:
		return false
	}
	return userID == t.OwnerID || userID == t.CreatorID || CanEditProject(a)
}

// SubmitCompletionOutcome 提交完成申请的路由（AC-13/14）：
// 未配置成果审核人直接待 KR 终审，配置了则进入中间或签。
// 成果更新走同一道审批链，但任务状态保持已完成不回退（AC-66、§5.1）。
func SubmitCompletionOutcome(reviewerCount int, resultUpdate bool) (string, string) {
	reviewState := CompletionPendingFinal
	taskStatus := TaskPendingFinalReview
	if reviewerCount > 0 {
		reviewState = CompletionIntermediate
		taskStatus = TaskPendingIntermediateReview
	}
	if resultUpdate {
		taskStatus = TaskCompleted
	}
	return reviewState, taskStatus
}

// DecideIntermediateRule 或签处理（AC-14/24/37）：仅或签组成员可处理（KR 负责人与管理员
// 不能替代）；任一人通过→待 KR 终审并关闭其余待办，任一人退回→整体退回（意见必填）。
func DecideIntermediateRule(a Actor, t TaskFacts, actorID int64, isReviewer func(int64) bool, approve bool, opinion string) (string, string, error) {
	// 成果更新的或签同样在任务已完成的前提下进行，处理结果不改变生命周期状态（AC-66）。
	inResultUpdate := ResultUpdateReviewInFlight(t)
	if t.Status != TaskPendingIntermediateReview && !inResultUpdate {
		return "", "", ErrCompletionNotIntermediate
	}
	if !CanWriteProject(a) || !isReviewer(actorID) {
		return "", "", ErrNotReviewer
	}
	if approve {
		if inResultUpdate {
			return TaskCompleted, CompletionPendingFinal, nil
		}
		return TaskPendingFinalReview, CompletionPendingFinal, nil
	}
	if strings.TrimSpace(opinion) == "" {
		return "", "", ErrRejectOpinionRequired
	}
	if inResultUpdate {
		return TaskCompleted, CompletionRejected, nil
	}
	return TaskInProgress, CompletionRejected, nil
}
