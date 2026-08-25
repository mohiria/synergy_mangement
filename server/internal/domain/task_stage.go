package domain

// CurrentStage 派生任务的当前环节与待行动人（词汇表「当前环节」「待行动人」）。
// 中间或签审核的待行动人在 #11 引入审核组后细化；已完成／已取消无待行动人。
func CurrentStage(t TaskFacts) (string, *int64) {
	owner := t.OwnerID
	switch t.Status {
	case TaskDraft:
		creator := t.CreatorID
		return "草稿完善", &creator
	case TaskPendingPoolReview:
		return "创建入池审批", t.KrOwnerID
	case TaskNotStarted:
		return "待开始执行", &owner
	case TaskWaitingInput:
		return "等待输入", &owner
	case TaskInProgress:
		return "任务执行", &owner
	case TaskPendingIntermediateReview:
		return "中间或签审核", nil
	case TaskPendingFinalReview:
		return "KR 终审", t.KrOwnerID
	case TaskCompleted:
		return "已闭环", nil
	case TaskCancelled:
		return "已取消", nil
	}
	return "任务执行", &owner
}
