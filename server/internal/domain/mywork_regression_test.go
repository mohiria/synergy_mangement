package domain

import (
	"testing"
	"time"
)

// MW-14（#178 修订：输入请求机制退场）：任务关闭后，该任务相关的审批件与卡点卡片
// 全部从我的工作消失。卡点侧本来就按「执行中才派生」排除了取消任务。
func TestMyWorkDropsCancelledTaskItems(t *testing.T) {
	now := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
	me := int64(1)
	other := int64(2)
	end := now.AddDate(0, 0, 5)

	facts := MyWorkFacts{
		UserID: me, Now: now, Actor: Actor{Role: RoleMember},
		Tasks: []WorkTaskFact{
			{ID: 10, Name: "已关闭任务", DisplayStatus: TaskCancelled, OwnerID: me, CreatorID: me, KrOwnerID: &me, EndDate: &end},
			{ID: 11, Name: "在办任务", DisplayStatus: TaskInProgress, OwnerID: me, CreatorID: me, KrOwnerID: &me, EndDate: &end},
		},
		FinalReviewerIDs:   []int64{me},
		FinalReviewerNames: []string{"我"},
		Completions: []WorkCompletionFact{
			{ID: 90, TaskID: 10, TaskName: "已关闭任务", SubmittedBy: other, TaskOwnerID: other, State: CompletionPendingFinal, SubmittedAt: now.AddDate(0, 0, -1), TaskEnd: &end},
			{ID: 91, TaskID: 11, TaskName: "在办任务", SubmittedBy: other, TaskOwnerID: other, State: CompletionPendingFinal, SubmittedAt: now.AddDate(0, 0, -1), TaskEnd: &end},
		},
	}
	g := MyWork(facts)

	for _, it := range g.Approvals {
		if it.TaskID != nil && *it.TaskID == 10 {
			t.Errorf("已关闭任务的审批件不应出现在待我审批: %+v", it)
		}
	}
	// 在办任务的同类事项仍在，确认不是被一刀切掉的
	if len(g.Approvals) != 1 || g.Approvals[0].TaskID == nil || *g.Approvals[0].TaskID != 11 {
		t.Fatalf("在办任务的审批件应保留: %+v", g.Approvals)
	}
}

// MW-15／模块 PRD §7.1：五组共用固定排序——超期最前，其次今天到期，再按截止／期望时间升序；
// 无时间字段的事项按已等待时长降序；同一紧急层级内被阻塞（上游未就绪）的任务沉一档。
func TestMyWorkSorting(t *testing.T) {
	// 时刻取项目时区的工作日中段；日期型字段按「当天零点」构造，与 pgx 扫 DATE 的形态一致，
	// 这样「今天到期」这一层级才真的被断言覆盖（回归 R7：此前 due 带时分秒，掩盖了时区差）。
	now := time.Date(2026, 8, 26, 10, 0, 0, 0, ProjectLocation)
	me := int64(1)
	day := func(d int) *time.Time {
		v := time.Date(2026, 8, 26+d, 0, 0, 0, 0, time.UTC)
		return &v
	}
	task := func(id int64, name string, endDelta int, unready string) WorkTaskFact {
		return WorkTaskFact{
			ID: id, Name: name, DisplayStatus: TaskInProgress, OwnerID: me, CreatorID: me,
			EndDate: day(endDelta), UnreadyNote: unready,
		}
	}
	facts := MyWorkFacts{
		UserID: me, Now: now, Actor: Actor{Role: RoleMember},
		Tasks: []WorkTaskFact{
			task(1, "三天后到期", 3, ""),
			task(2, "已超期", -2, ""),
			task(3, "今天到期", 0, ""),
			task(4, "今天到期但被阻塞", 0, "上游未就绪：缺 接口清单"),
			task(5, "明天到期", 1, ""),
		},
	}
	g := MyWork(facts)
	got := make([]string, 0, len(g.Pending))
	for _, it := range g.Pending {
		got = append(got, it.TaskName)
	}
	// 只有真正过了截止日的才算超期：截止日当天仍有一整天工期（回归 R7）。
	// 仅看排序结果无法区分——今天到期的两条即便被误判为超期，位次也不变。
	for _, it := range g.Pending {
		if want := it.TaskName == "已超期"; it.Overdue != want {
			t.Fatalf("任务「%s」Overdue = %v, want %v", it.TaskName, it.Overdue, want)
		}
	}
	want := []string{"已超期", "今天到期", "今天到期但被阻塞", "明天到期", "三天后到期"}
	if len(got) != len(want) {
		t.Fatalf("待我处理条数 = %d, want %d（got %v）", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("排序 = %v, want %v", got, want)
		}
	}
}

// MW-15 第 4 条：没有时间字段的事项按已等待时长降序，越久越前。
func TestMyWorkSortingWithoutDueDate(t *testing.T) {
	now := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
	me := int64(1)
	other := int64(2)
	facts := MyWorkFacts{
		UserID: me, Now: now, Actor: Actor{Role: RoleMember},
		FinalReviewerIDs:   []int64{other},
		FinalReviewerNames: []string{"周宁"},
		Completions: []WorkCompletionFact{
			{ID: 1, TaskID: 101, TaskName: "等 1 天", SubmittedBy: me, TaskOwnerID: me, State: CompletionPendingFinal, SubmittedAt: now.AddDate(0, 0, -1)},
			{ID: 2, TaskID: 102, TaskName: "等 6 天", SubmittedBy: me, TaskOwnerID: me, State: CompletionPendingFinal, SubmittedAt: now.AddDate(0, 0, -6)},
			{ID: 3, TaskID: 103, TaskName: "等 3 天", SubmittedBy: me, TaskOwnerID: me, State: CompletionPendingFinal, SubmittedAt: now.AddDate(0, 0, -3)},
		},
	}
	g := MyWork(facts)
	got := make([]string, 0, len(g.Waiting))
	for _, it := range g.Waiting {
		got = append(got, it.TaskName)
	}
	want := []string{"等 6 天", "等 3 天", "等 1 天"}
	if len(got) != len(want) {
		t.Fatalf("等待他人条数 = %d, want %d（got %v）", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("无时间事项排序 = %v, want %v", got, want)
		}
	}
}
