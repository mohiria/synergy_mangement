package domain

import "testing"

// 当前环节与待行动人派生（词汇表「当前环节」「待行动人」；AC-31 基础信息）。
func TestCurrentStage(t *testing.T) {
	facts := func(status string) TaskFacts {
		return TaskFacts{Status: status, CreatorID: 3, OwnerID: 5, KrOwnerID: i64(7)}
	}
	cases := []struct {
		name      string
		t         TaskFacts
		wantStage string
		wantActor *int64
	}{
		{"草稿由创建人完善", facts(TaskDraft), "草稿完善", i64(3)},
		{"待入池审批停在 KR 负责人", facts(TaskPendingPoolReview), "创建入池审批", i64(7)},
		{"未开始等负责人开始", facts(TaskNotStarted), "待开始执行", i64(5)},
		{"等待输入停在负责人", facts(TaskWaitingInput), "等待输入", i64(5)},
		{"进行中为任务执行", facts(TaskInProgress), "任务执行", i64(5)},
		{"待中间审核", facts(TaskPendingIntermediateReview), "中间或签审核", nil},
		{"待 KR 终审停在 KR 负责人", facts(TaskPendingFinalReview), "KR 终审", i64(7)},
		{"已完成即已闭环", facts(TaskCompleted), "已闭环", nil},
		{"已关闭无待行动人", facts(TaskCancelled), "已关闭", nil},
		{"KR 无负责人时终审待行动人为空", TaskFacts{Status: TaskPendingFinalReview, OwnerID: 5}, "KR 终审", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stage, actor := CurrentStage(tc.t)
			if stage != tc.wantStage {
				t.Fatalf("CurrentStage() stage = %q, want %q", stage, tc.wantStage)
			}
			switch {
			case tc.wantActor == nil && actor != nil:
				t.Fatalf("actor = %v, want nil", *actor)
			case tc.wantActor != nil && (actor == nil || *actor != *tc.wantActor):
				t.Fatalf("actor = %v, want %d", actor, *tc.wantActor)
			}
		})
	}
}
