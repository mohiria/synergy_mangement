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
)

// SubmitCompletionRule 校验提交完成申请（AC-13、§9.1）：
// 进行中、至少一项候选内容、提交说明必填、KR 已指定负责人（终审人存在）。
func SubmitCompletionRule(t TaskFacts, candidateCount int, note string) error {
	if t.Status != TaskInProgress {
		return ErrCompletionNotInProgress
	}
	if candidateCount == 0 {
		return ErrNoCandidates
	}
	if strings.TrimSpace(note) == "" {
		return ErrCompletionNoteRequired
	}
	if t.KrOwnerID == nil {
		return ErrKrOwnerMissing
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

// DecideCompletionRule 终审规则（AC-15、AC-38）：仅所属 KR 负责人处理待终审申请；
// 通过→任务完成（意见选填），退回→意见必填、任务回到进行中。
func DecideCompletionRule(a Actor, t TaskFacts, actorID int64, approve bool, opinion string) (string, error) {
	if t.Status != TaskPendingFinalReview {
		return "", ErrCompletionNotPending
	}
	if !CanWriteProject(a) || t.KrOwnerID == nil || *t.KrOwnerID != actorID {
		return "", ErrNotKrOwner
	}
	if approve {
		return TaskCompleted, nil
	}
	if strings.TrimSpace(opinion) == "" {
		return "", ErrRejectOpinionRequired
	}
	return TaskInProgress, nil
}
