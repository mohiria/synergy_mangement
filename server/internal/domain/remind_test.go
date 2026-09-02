package domain

import (
	"strings"
	"testing"
	"time"
)

// MW-13：等待他人与卡点两组都能提醒当前待行动人；提醒目标不只是卡点。
func TestRemindTargets(t *testing.T) {
	due := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	kr := i64(7)
	f := RemindFacts{
		Blockers: []Blocker{{
			Key: "approval_timeout:final_review:31", Kind: BlockerApprovalTimeout,
			TaskID: 1, TaskName: "验收方案", Missing: "KR 终审处理", Reason: "已等待 5 天",
			ActionOwnerIDs: []int64{7}, ActionOwnerNames: []string{"李四"},
			TaskOwnerID: 5, KrOwnerID: kr,
		}},
		Waits: []RemindWaitFact{{
			Kind: "final_review", RefID: 32, TaskID: 2, TaskName: "接口口径",
			Missing: "KR 终审处理", Reason: "完成申请等待 KR 终审",
			ActionOwnerIDs: []int64{7}, ActionOwnerNames: []string{"李四"},
			TaskOwnerID: 5, KrOwnerID: kr,
		}},
		Tasks: map[int64]RemindTaskFact{
			1: {Name: "验收方案", OwnerID: 5, KrOwnerID: kr, End: &due, ImpactNote: "沿硬前置影响下游 2 项任务：A、B"},
			2: {Name: "接口口径", OwnerID: 5, KrOwnerID: kr, End: &due},
		},
	}
	targets := RemindTargets(f)
	if len(targets) != 2 {
		t.Fatalf("卡点与等待事项应各自可寻址: %+v", targets)
	}
	byKey := map[string]RemindTarget{}
	for _, tg := range targets {
		byKey[tg.Key] = tg
	}
	blocker, ok := byKey["approval_timeout:final_review:31"]
	if !ok {
		t.Fatalf("卡点目标应按卡点合成键寻址: %+v", byKey)
	}
	if blocker.Due == nil || !blocker.Due.Equal(due) || blocker.ImpactNote == "" {
		t.Fatalf("卡点目标应带任务截止时间与下游影响: %+v", blocker)
	}
	wait, ok := byKey[RemindWaitKey("final_review", 32)]
	if !ok {
		t.Fatalf("等待事项应按 wait 合成键寻址: %+v", byKey)
	}
	if wait.TaskID != 2 || wait.Due == nil || !wait.Due.Equal(due) {
		t.Fatalf("等待目标事实不对: %+v", wait)
	}
}

// 提醒权限：访客不可；待行动人不提醒自己；任务负责人／KR 负责人／可编辑项目者可提醒。
func TestCanRemind(t *testing.T) {
	kr := i64(7)
	target := RemindTarget{TaskID: 1, ActionOwnerIDs: []int64{9}, TaskOwnerID: 5, KrOwnerID: kr}
	if CanRemind(Actor{Role: RoleMember}, 9, target) {
		t.Fatal("待行动人不应提醒自己")
	}
	if !CanRemind(Actor{Role: RoleMember}, 5, target) {
		t.Fatal("任务负责人应可提醒")
	}
	if !CanRemind(Actor{Role: RoleMember}, 7, target) {
		t.Fatal("所属 KR 负责人应可提醒")
	}
	if !CanRemind(Actor{Role: RoleAdmin}, 3, target) {
		t.Fatal("项目管理员应可提醒")
	}
	if CanRemind(Actor{Role: RoleMember}, 3, target) {
		t.Fatal("无关成员不应可提醒")
	}
	if CanRemind(Actor{Role: RoleViewer}, 5, target) {
		t.Fatal("访客不应可提醒")
	}
}

// 提醒正文（模块 PRD §5.3）：自动带入任务、缺失输入、截止时间和下游影响。
func TestRemindContent(t *testing.T) {
	due := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	content := RemindContent(RemindTarget{
		TaskName: "验收方案", Missing: "现场数据包", Reason: "上游任务「数据采集」尚未交付当前内容",
		Due: &due, ImpactNote: "沿硬前置影响下游 2 项任务：A、B",
	})
	for _, want := range []string{"验收方案", "现场数据包", "数据采集", "2026-09-30", "下游 2 项任务"} {
		if !strings.Contains(content, want) {
			t.Fatalf("提醒正文缺「%s」: %s", want, content)
		}
	}
	// 没有截止时间与下游影响时不拼空字段
	bare := RemindContent(RemindTarget{TaskName: "无期任务", Missing: "按期完成任务", Reason: "已超期"})
	if strings.Contains(bare, "截止") || strings.HasSuffix(bare, "；") {
		t.Fatalf("缺字段时不应留空壳: %s", bare)
	}
}

// MW-13、AC-60 冷却：按（发起人、被提醒人、任务）三元组计当天次数，上限取项目规则设置。
func TestRemindAllowed(t *testing.T) {
	cases := []struct {
		name      string
		sentToday int
		limit     int
		want      bool
	}{
		{"今天还没提醒过", 0, 1, true},
		{"默认每天 1 次，第二次被拒", 1, 1, false},
		{"上限放到 3 次，第二次仍允许", 1, 3, true},
		{"上限放到 3 次，用满后被拒", 3, 3, false},
		{"上限缺失时按默认 1 次判定", 1, 0, false},
		{"上限缺失时首次仍允许", 0, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RemindAllowed(tc.sentToday, tc.limit); got != tc.want {
				t.Fatalf("RemindAllowed(%d, %d) = %v，期望 %v", tc.sentToday, tc.limit, got, tc.want)
			}
		})
	}
}

// MW-13 的另一半：尚未成卡点的等待事项，等待他人卡片也给提醒入口。
func TestMyWorkWaitingRemindWithoutBlocker(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	me := int64(5)
	krOther := i64(7)
	recent := now.AddDate(0, 0, -1) // 未达审批超时阈值，不会派生卡点
	g := MyWork(MyWorkFacts{
		UserID: me,
		Actor:  Actor{Role: RoleMember},
		Now:    now,
		Tasks: []WorkTaskFact{
			{ID: 1, Name: "我的任务", DisplayStatus: TaskInReview, OwnerID: me, CreatorID: me, KrOwnerID: krOther},
			{ID: 2, Name: "等输入的任务", DisplayStatus: TaskWaitingInput, OwnerID: me, CreatorID: me, KrOwnerID: krOther},
			{ID: 3, Name: "上游任务", DisplayStatus: TaskInProgress, OwnerID: 9, CreatorID: 9, KrOwnerID: krOther},
		},
		FinalReviewerIDs:   []int64{7},
		FinalReviewerNames: []string{"李四"},
		Completions: []WorkCompletionFact{
			{ID: 33, TaskID: 1, TaskName: "我的任务", SubmittedBy: me, TaskOwnerID: me,
				State: CompletionPendingFinal, SubmittedAt: recent},
		},
		Upstreams: []WorkUpstreamFact{
			{EdgeID: 51, TargetTaskID: 2, TargetName: "等输入的任务", TargetOwnerID: me,
				SourceTaskID: i64(3), SourceName: "上游任务", SourceOwnerID: 9, SourceOwnerName: "王五",
				InputName: "现场数据包", Ready: false, Necessity: NecessityRequired},
		},
		// 没有任何派生卡点
	})
	if len(g.Blockers) != 0 {
		t.Fatalf("本例不应有卡点: %+v", g.Blockers)
	}
	if len(g.Waiting) != 2 {
		t.Fatalf("等待他人应有两条: %+v", g.Waiting)
	}
	for _, it := range g.Waiting {
		if !it.CanRemind {
			t.Fatalf("尚未成卡点的等待事项也应可提醒: %+v", it)
		}
		if it.RefKey == "" || !strings.HasPrefix(it.RefKey, "wait:") {
			t.Fatalf("等待事项应按自己的提醒目标寻址: %+v", it)
		}
	}
}

// 不提醒本人：待行动人就是自己时不给提醒入口。
func TestMyWorkWaitingRemindNotSelf(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	me := int64(5)
	krMe := i64(5)
	recent := now.AddDate(0, 0, -1)
	g := MyWork(MyWorkFacts{
		UserID: me,
		Actor:  Actor{Role: RoleMember},
		Now:    now,
		Tasks: []WorkTaskFact{
			{ID: 1, Name: "我的任务", DisplayStatus: TaskInReview, OwnerID: me, CreatorID: me, KrOwnerID: krMe},
		},
		FinalReviewerIDs:   []int64{me},
		FinalReviewerNames: []string{"张三"},
		Completions: []WorkCompletionFact{
			{ID: 21, TaskID: 1, TaskName: "我的任务", SubmittedBy: 9, TaskOwnerID: me, State: CompletionPendingFinal, SubmittedAt: recent},
		},
	})
	for _, it := range g.Waiting {
		if it.CanRemind {
			t.Fatalf("待行动人是本人时不应给提醒入口: %+v", it)
		}
	}
}
