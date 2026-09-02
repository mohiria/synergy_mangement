package domain

import (
	"errors"
	"strings"
)

// 完成申请状态（词汇表「完成申请」）。
const (
	CompletionIntermediate = "intermediate_review"
	CompletionPendingFinal = "pending_final"
	CompletionApproved     = "approved"
	CompletionRejected     = "rejected"
)

var (
	ErrCompletionNotInProgress = errors.New("只有进行中的任务可以提交完成申请")
	ErrNoCandidates            = errors.New("没有候选交付物内容，请先上传")
	ErrCompletionNoteRequired  = errors.New("提交完成申请需要填写提交说明")
	ErrCompletionNotPending    = errors.New("完成申请不在待终审状态")
	ErrRejectOpinionRequired   = errors.New("退回意见必填")
	ErrNotFinalReviewer        = errors.New("只能由项目管理员终审")
)

// SubmitCompletionRule 校验提交完成申请（AC-13、§9.1）：
// 进行中、至少一项候选内容、提交说明必填。
// 裁决 11（#181）：终审人为项目管理员集合（项目负责人恒存在），无「无人可审批」情形。
func SubmitCompletionRule(t TaskFacts, candidateCount int, note string) error {
	// 成果更新走同一道完成审批：已完成任务在成果更新已发起、尚未提交时同样可提交（AC-66）。
	if t.Status != TaskInProgress && !(t.Status == TaskCompleted && t.ResultUpdate == ResultUpdateOpen) {
		return ErrCompletionNotInProgress
	}
	if candidateCount == 0 {
		return ErrNoCandidates
	}
	if strings.TrimSpace(note) == "" {
		return ErrCompletionNoteRequired
	}
	return nil
}

// CanSubmitCompletion 判定提交完成申请的动作人：任务负责人（管理员／项目负责人纠错）。
func CanSubmitCompletion(a Actor, userID int64, t TaskFacts, candidateCount int) bool {
	if !CanWriteProject(a) {
		return false
	}
	if SubmitCompletionRule(t, candidateCount, "占位") != nil {
		return false
	}
	return userID == t.OwnerID || CanEditProject(a)
}

// DecideCompletionRule 终审规则（AC-15、AC-38；裁决 11，#181）：终审人为项目管理员集合
// （含项目负责人）或签，审批人按处理时点动态解析角色、不快照；
// 任一人通过→任务完成（意见选填），退回→意见必填、任务回到进行中。
func DecideCompletionRule(a Actor, t TaskFacts, approve bool, opinion string) (string, error) {
	// 成果更新的终审在任务已完成的前提下进行，处理结果不改变生命周期状态（AC-66）。
	inResultUpdate := ResultUpdateReviewInFlight(t)
	if t.Status != TaskPendingFinalReview && !inResultUpdate {
		return "", ErrCompletionNotPending
	}
	if !CanEditProject(a) {
		return "", ErrNotFinalReviewer
	}
	if approve {
		return TaskCompleted, nil
	}
	if strings.TrimSpace(opinion) == "" {
		return "", ErrRejectOpinionRequired
	}
	if inResultUpdate {
		return TaskCompleted, nil
	}
	return TaskInProgress, nil
}
