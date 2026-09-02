package domain

import (
	"testing"
	"time"
)

// 我的工作卡片动作与提醒（模块 PRD §5.3、AC-55、MW-21、MW-13；#168 调整）：
// 待我处理／待我审批／待我接收用「去处理」；等待他人与卡点只读、不再派生「查看详情」
// 文字按钮（动作文案为空，点条目行打开抽屉）；
// 提醒按各自的提醒目标寻址：卡点用卡点键，等待他人用事项自身的 wait 键；待行动人是本人或访客时不可提醒。
func TestMyWorkCardActionAndRemind(t *testing.T) {
	now := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
	end := now.AddDate(0, 0, -3)
	me := int64(1)
	other := int64(2)

	base := func(actor Actor, blockers []Blocker) MyWorkFacts {
		return MyWorkFacts{
			UserID: me, Now: now, Actor: actor,
			Tasks: []WorkTaskFact{
				{ID: 10, Name: "联调验证", DisplayStatus: TaskInProgress, OwnerID: me, CreatorID: me, EndDate: &end},
			},
			// 裁决 10：关闭申请退场，等待他人用完成申请（终审中）事实。
			Completions: []WorkCompletionFact{
				{ID: 7, TaskID: 10, TaskName: "联调验证", SubmittedBy: me, TaskOwnerID: me, KrOwnerID: &other, KrOwnerName: "周宁", State: CompletionPendingFinal, SubmittedAt: now.AddDate(0, 0, -5), TaskEnd: &end},
			},
			Blockers: blockers,
		}
	}
	overdueBlocker := Blocker{
		Key: "task_overdue:10", Kind: BlockerTaskOverdue, TaskID: 10, TaskName: "联调验证",
		Missing: "按期完成任务", ActionOwnerIDs: []int64{other}, ActionOwnerNames: []string{"周宁"},
		TaskOwnerID: me, Level: "high_risk", Since: end,
	}

	t.Run("五组动作文案按分组派生", func(t *testing.T) {
		g := MyWork(base(Actor{Role: RoleMember}, nil))
		for _, c := range []struct {
			group string
			items []WorkItem
			want  string
		}{
			{"待我处理", g.Pending, WorkActionHandle},
			{"待我审批", g.Approvals, WorkActionHandle},
			{"待我接收", g.Receipts, WorkActionHandle},
			{"等待他人", g.Waiting, ""},
			{"与我相关的卡点", g.Blockers, ""},
		} {
			for _, it := range c.items {
				if it.ActionLabel != c.want {
					t.Errorf("%s 的 %s 卡片动作文案 = %q, want %q", c.group, it.Kind, it.ActionLabel, c.want)
				}
			}
		}
		if len(g.Pending) == 0 || len(g.Waiting) == 0 {
			t.Fatalf("用例前提失效：待我处理 %d 条、等待他人 %d 条", len(g.Pending), len(g.Waiting))
		}
	})

	// #168（#15 反馈）：卡点条目补所属任务的截止日期，参与超期标红与组内排序。
	t.Run("卡点条目带任务截止并参与超期", func(t *testing.T) {
		g := MyWork(base(Actor{Role: RoleMember}, []Blocker{overdueBlocker}))
		if len(g.Blockers) != 1 {
			t.Fatalf("卡点组条数 = %d, want 1", len(g.Blockers))
		}
		it := g.Blockers[0]
		if it.Due == nil || !it.Due.Equal(end) {
			t.Errorf("卡点条目应带所属任务截止 %v: %+v", end, it.Due)
		}
		if !it.Overdue {
			t.Errorf("任务已超期，卡点条目应标超期: %+v", it)
		}
	})

	t.Run("卡点卡片可提醒非本人的待行动人", func(t *testing.T) {
		g := MyWork(base(Actor{Role: RoleMember}, []Blocker{overdueBlocker}))
		if len(g.Blockers) != 1 {
			t.Fatalf("卡点组条数 = %d, want 1", len(g.Blockers))
		}
		if !g.Blockers[0].CanRemind || g.Blockers[0].RefKey != overdueBlocker.Key {
			t.Errorf("卡点卡片应可提醒并带卡点键: %+v", g.Blockers[0])
		}
	})

	t.Run("待行动人是本人时不可提醒", func(t *testing.T) {
		mine := overdueBlocker
		mine.ActionOwnerIDs = []int64{me}
		mine.ActionOwnerNames = []string{"我"}
		g := MyWork(base(Actor{Role: RoleMember}, []Blocker{mine}))
		if len(g.Blockers) != 1 {
			t.Fatalf("卡点组条数 = %d, want 1", len(g.Blockers))
		}
		if g.Blockers[0].CanRemind {
			t.Errorf("不应提醒本人: %+v", g.Blockers[0])
		}
	})

	t.Run("访客不可提醒", func(t *testing.T) {
		g := MyWork(base(Actor{Role: RoleViewer}, []Blocker{overdueBlocker}))
		for _, it := range append(append([]WorkItem{}, g.Waiting...), g.Blockers...) {
			if it.CanRemind {
				t.Errorf("访客不应可提醒: %+v", it)
			}
		}
	})

	t.Run("等待他人卡片按事项自身的提醒目标寻址", func(t *testing.T) {
		g := MyWork(base(Actor{Role: RoleMember}, []Blocker{overdueBlocker}))
		if len(g.Waiting) != 1 {
			t.Fatalf("等待他人条数 = %d, want 1", len(g.Waiting))
		}
		if !g.Waiting[0].CanRemind || g.Waiting[0].RefKey != RemindWaitKey("final_review", 7) {
			t.Errorf("等待他人卡片应带自身 wait 键并可提醒: %+v", g.Waiting[0])
		}
	})

	// MW-13 的另一半：审批件尚未达到超时阈值、没有派生卡点时，等待他人同样可提醒。
	t.Run("尚未成卡点的等待事项同样可提醒", func(t *testing.T) {
		g := MyWork(base(Actor{Role: RoleMember}, nil))
		if len(g.Waiting) != 1 {
			t.Fatalf("等待他人条数 = %d, want 1", len(g.Waiting))
		}
		if !g.Waiting[0].CanRemind || g.Waiting[0].RefKey != RemindWaitKey("final_review", 7) {
			t.Errorf("尚未成卡点的等待事项也应可提醒: %+v", g.Waiting[0])
		}
	})
}
