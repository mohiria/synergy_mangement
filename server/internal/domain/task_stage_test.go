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
		{"未开始等负责人开始", facts(TaskNotStarted), "待开始执行", i64(5)},
		{"等待输入停在负责人", facts(TaskWaitingInput), "等待输入", i64(5)},
		{"进行中为任务执行", facts(TaskInProgress), "任务执行", i64(5)},
		{"待成果审核", facts(TaskPendingIntermediateReview), "成果审核（或签）", nil},
		// 裁决 11（#181）：终审人为项目管理员集合，无单一待行动人（与或签同口径）。
		{"待终审无单一待行动人", facts(TaskPendingFinalReview), "终审", nil},
		{"已完成即已闭环", facts(TaskCompleted), "已闭环", nil},
		{"已关闭无待行动人", facts(TaskCancelled), "已关闭", nil},
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
