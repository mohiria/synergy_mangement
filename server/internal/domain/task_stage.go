package domain

// CurrentStage 派生任务的当前环节与待行动人（词汇表「当前环节」「待行动人」）。
// 成果审核（或签）的待行动人在 #11 引入审核组后细化；已完成／已关闭无待行动人。
func CurrentStage(t TaskFacts) (string, *int64) {
	owner := t.OwnerID
	switch t.Status {
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
