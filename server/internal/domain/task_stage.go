package domain

// CurrentStage 派生任务的当前环节与待行动人（词汇表「当前环节」「待行动人」）。
// 中间或签审核的待行动人在 #11 引入审核组后细化；已完成／已取消无待行动人。
func CurrentStage(t TaskFacts) (string, *int64) {
	owner := t.OwnerID
	switch t.Status {
	case TaskDraft:
		creator := t.CreatorID
		return StageDraft, &creator
	case TaskPendingPoolReview:
		return StagePoolReview, t.KrOwnerID
	case TaskNotStarted:
		return StageNotStarted, &owner
	case TaskWaitingInput:
		return StageWaitingInput, &owner
	case TaskInProgress:
		return StageInProgress, &owner
	case TaskPendingIntermediateReview:
		return StageIntermediateReview, nil
	case TaskPendingFinalReview:
		return StageFinalReview, t.KrOwnerID
	case TaskCompleted:
		return StageCompleted, nil
	case TaskCancelled:
		return StageCancelled, nil
	}
	return StageInProgress, &owner
}
