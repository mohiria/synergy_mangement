package domain

import (
	"testing"
	"time"
)

// 时间型卡点的真实发生时刻（ADR 0001）：审批超时在「进入环节 + N×24h」那一刻发生，
// 任务超期在截止时间那一刻发生；两者都与派生时刻无关，重复派生得到同一个时间戳。
func TestBlockerOccurredAt(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	since := time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC)
	facts := func(now time.Time) BlockerFacts {
		return BlockerFacts{
			Now:                 now,
			ApprovalTimeoutDays: 3,
			Tasks: []BlockerTaskFact{
				{ID: 1, Name: "验收方案", Status: TaskInProgress, OwnerID: 5, OwnerName: "王五", StartDate: &start, EndDate: &end},
			},
			Approvals: []BlockerApprovalFact{
				{Kind: "final_review", RefID: 31, TaskID: 1, StageSince: since, ApproverIDs: []int64{7}, ApproverNames: []string{"李四"}},
			},
		}
	}
	byKind := func(now time.Time) map[string]Blocker {
		out := map[string]Blocker{}
		for _, b := range DeriveBlockers(facts(now)) {
			out[b.Kind] = b
		}
		return out
	}
	first := byKind(time.Date(2026, 9, 11, 6, 0, 0, 0, time.UTC))
	later := byKind(time.Date(2026, 9, 15, 18, 30, 0, 0, time.UTC))

	wantTimeout := since.Add(3 * 24 * time.Hour)
	if got := first[BlockerApprovalTimeout].OccurredAt; !got.Equal(wantTimeout) {
		t.Fatalf("审批超时发生时刻 = %v, want %v", got, wantTimeout)
	}
	if got := first[BlockerTaskOverdue].OccurredAt; !got.Equal(end) {
		t.Fatalf("任务超期发生时刻 = %v, want %v", got, end)
	}
	for _, kind := range []string{BlockerApprovalTimeout, BlockerTaskOverdue} {
		if !first[kind].OccurredAt.Equal(later[kind].OccurredAt) {
			t.Fatalf("%s 的发生时刻不应随派生时刻变化: %v vs %v", kind, first[kind].OccurredAt, later[kind].OccurredAt)
		}
	}
}

// ticker 补记（ADR 0001）：只补时间型卡点，时间戳取真实发生时刻，
// 文案与写触发的 diff 一致，并带上合成键供落库去重。
func TestTimeTriggeredBlockerActivities(t *testing.T) {
	occurred := time.Date(2026, 9, 5, 9, 0, 0, 0, time.UTC)
	bs := []Blocker{
		{Key: "upstream_unready:edge:1", Kind: BlockerUpstreamUnready, TaskID: 7, TaskName: "联调", Missing: "接口清单", OccurredAt: occurred},
		{Key: "approval_timeout:final_review:31", Kind: BlockerApprovalTimeout, TaskID: 7, TaskName: "联调", Missing: "终审处理", OccurredAt: occurred},
		{Key: "task_overdue:7", Kind: BlockerTaskOverdue, TaskID: 7, TaskName: "联调", Missing: "按期完成任务", OccurredAt: occurred},
		{Key: "interlock:7", Kind: BlockerInterlock, TaskID: 7, TaskName: "联调", Missing: "打破硬前置互锁", OccurredAt: occurred},
	}
	got := TimeTriggeredBlockerActivities(bs)
	if len(got) != 2 {
		t.Fatalf("只应补记审批超时与任务超期两类: %+v", got)
	}
	for _, a := range got {
		if a.Kind != ActivityBlockerOpened {
			t.Fatalf("补记的只有「卡点出现」: %+v", a)
		}
		if !a.OccurredAt.Equal(occurred) {
			t.Fatalf("时间戳应取真实发生时刻: %+v", a)
		}
		if a.BlockerKey == "" {
			t.Fatalf("补记应带卡点合成键以便落库去重: %+v", a)
		}
		if a.ActorID != nil {
			t.Fatalf("系统派生事件不应带行动人: %+v", a)
		}
	}
	if got[0].Summary != "卡点出现：审批超时 · 缺 终审处理" {
		t.Fatalf("文案应与写触发口径一致: %q", got[0].Summary)
	}
}

// 写触发的 diff 与 ticker 补记指向同一条事实：出现动态的时间戳都取真实发生时刻，
// 合成键一致，落库唯一键因此能挡住重复记账。
func TestBlockerActivityDiffUsesOccurredAt(t *testing.T) {
	now := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	occurred := time.Date(2026, 9, 5, 9, 0, 0, 0, time.UTC)
	b := Blocker{
		Key: "task_overdue:7", Kind: BlockerTaskOverdue, TaskID: 7, TaskName: "联调",
		Missing: "按期完成任务", OccurredAt: occurred,
	}
	opened := BlockerActivityDiff(nil, []Blocker{b}, now)
	if len(opened) != 1 || !opened[0].OccurredAt.Equal(occurred) || opened[0].BlockerKey != b.Key {
		t.Fatalf("出现动态应取真实发生时刻并带合成键: %+v", opened)
	}
	if same := TimeTriggeredBlockerActivities([]Blocker{b}); len(same) != 1 ||
		!same[0].OccurredAt.Equal(opened[0].OccurredAt) || same[0].BlockerKey != opened[0].BlockerKey ||
		same[0].Summary != opened[0].Summary {
		t.Fatalf("两条路径应产出同一条事实: %+v vs %+v", same, opened)
	}
	// 解除没有可计算的发生时刻，取本次比对时刻。
	resolved := BlockerActivityDiff([]Blocker{b}, nil, now)
	if len(resolved) != 1 || !resolved[0].OccurredAt.Equal(now) {
		t.Fatalf("解除动态应取比对时刻: %+v", resolved)
	}
}
