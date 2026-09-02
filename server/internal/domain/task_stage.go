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
	case TaskInReview:
		// 裁决 13（#182）：当前环节从完成申请单读取（中间或签／待终审）；
		// 裁决 11：终审人为项目管理员集合，两个审批环节都无单一待行动人。
		if t.ReviewStage == CompletionIntermediate {
			return StageIntermediateReview, nil
		}
		return StageFinalReview, nil
	case TaskCompleted:
		return StageCompleted, nil
	case TaskCancelled:
		return StageCancelled, nil
	}
	return StageInProgress, &owner
}
