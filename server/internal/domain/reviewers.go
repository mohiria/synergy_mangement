package domain

import (
	"errors"
	"strings"
)

var (
	ErrReviewerNotEligible       = errors.New("中间审核人必须是非只读的项目成员")
	ErrNotReviewer               = errors.New("只有或签组内的中间审核人可以处理")
	ErrCompletionNotIntermediate = errors.New("完成申请不在中间审核状态")
)

// ValidateReviewers 校验中间审核组：非只读项目成员；空配置＝不设中间审核（§5.4）。
func ValidateReviewers(userIDs []int64, roleOf func(int64) string) error {
	for _, id := range userIDs {
		if role := roleOf(id); role != RoleAdmin && role != RoleMember {
			return ErrReviewerNotEligible
		}
	}
	return nil
}

// CanManageReviewers 判定能否调整中间审核人配置：负责人／创建人／可编辑项目者；
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
// 未配置中间审核人直接待 KR 终审，配置了则进入中间或签。
func SubmitCompletionOutcome(reviewerCount int) (string, string) {
	if reviewerCount > 0 {
		return CompletionIntermediate, TaskPendingIntermediateReview
	}
	return CompletionPendingFinal, TaskPendingFinalReview
}

// DecideIntermediateRule 或签处理（AC-14/24/37）：仅或签组成员可处理（KR 负责人与管理员
// 不能替代）；任一人通过→待 KR 终审并关闭其余待办，任一人退回→整体退回（意见必填）。
func DecideIntermediateRule(a Actor, t TaskFacts, actorID int64, isReviewer func(int64) bool, approve bool, opinion string) (string, string, error) {
	if t.Status != TaskPendingIntermediateReview {
		return "", "", ErrCompletionNotIntermediate
	}
	if !CanWriteProject(a) || !isReviewer(actorID) {
		return "", "", ErrNotReviewer
	}
	if approve {
		return TaskPendingFinalReview, CompletionPendingFinal, nil
	}
	if strings.TrimSpace(opinion) == "" {
		return "", "", ErrRejectOpinionRequired
	}
	return TaskInProgress, CompletionRejected, nil
}
