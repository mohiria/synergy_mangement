package domain

import (
	"testing"
	"time"
)

// R11-1（模块 PRD §4.2 规则 8、AC-16）：只有未就绪的必要输入进本页；
// 参考输入既不进「待我处理」也不进「等待他人」，也不计入徽标。
func TestMyWorkExcludesReferenceInputRequests(t *testing.T) {
	now := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)
	provider, owner := int64(1), int64(2)
	kr := i64(9)

	cases := []struct {
		name        string
		necessity   string
		wantPending int
		wantWaiting int
	}{
		{"必要输入两侧都进组", NecessityRequired, 1, 1},
		{"参考输入两侧都不进组", NecessityReference, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			facts := func(uid int64) MyWorkFacts {
				return MyWorkFacts{
					UserID: uid, Actor: Actor{Role: RoleMember}, Now: now,
					Tasks: []WorkTaskFact{
						{ID: 7, Name: "在办任务", DisplayStatus: TaskInProgress, OwnerID: owner, CreatorID: owner, KrOwnerID: kr},
					},
					InputRequests: []WorkInputRequestFact{{
						ID: 70, TaskID: 7, TaskName: "在办任务", InputName: "现场数据",
						Necessity: tc.necessity, ProviderID: provider, TaskOwnerID: owner,
						State: InputRequestPending, CreatedAt: now.AddDate(0, 0, -1), Notified: true,
					}},
				}
			}
			if got := countKind(MyWork(facts(provider)).Pending, "input_request"); got != tc.wantPending {
				t.Errorf("对接人视角待我处理 = %d，期望 %d", got, tc.wantPending)
			}
			if got := countKind(MyWork(facts(owner)).Waiting, "waiting_input_request"); got != tc.wantWaiting {
				t.Errorf("任务负责人视角等待他人 = %d，期望 %d", got, tc.wantWaiting)
			}
		})
	}
}

// R11-2（MW-14）：上游等待项随下游任务终态收口；来源任务已关闭时与「尚未交付」分开出文案，
// 且不给提醒入口——提醒已关闭任务的负责人交付没有意义。
func TestMyWorkUpstreamWaitingScope(t *testing.T) {
	now := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)
	me, upstreamOwner := int64(1), int64(2)
	kr := i64(9)

	cases := []struct {
		name         string
		targetStatus string
		sourceStatus string
		wantItems    int
		wantStage    string
		wantRemind   bool
	}{
		{"下游在办、上游在办：等待上游交付并可提醒", TaskInProgress, TaskInProgress, 1, WorkStageUpstreamWaiting, true},
		{"下游已关闭：等待项消失", TaskCancelled, TaskInProgress, 0, "", false},
		{"下游已完成：等待项消失", TaskCompleted, TaskInProgress, 0, "", false},
		{"上游已关闭：文案分开且不可提醒", TaskInProgress, TaskCancelled, 1, WorkStageUpstreamCancelled, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := MyWork(MyWorkFacts{
				UserID: me, Actor: Actor{Role: RoleMember}, Now: now,
				Tasks: []WorkTaskFact{
					{ID: 1, Name: "我的任务", DisplayStatus: tc.targetStatus, OwnerID: me, CreatorID: me, KrOwnerID: kr},
					{ID: 2, Name: "上游任务", DisplayStatus: tc.sourceStatus, OwnerID: upstreamOwner, CreatorID: upstreamOwner, KrOwnerID: kr},
				},
				Upstreams: []WorkUpstreamFact{{
					EdgeID: 50, TargetTaskID: 1, TargetName: "我的任务", TargetOwnerID: me,
					SourceTaskID: i64(2), SourceName: "上游任务", SourceOwnerID: upstreamOwner,
					SourceOwnerName: "李四", InputName: "接口口径", Ready: false, Necessity: NecessityRequired,
				}},
			})
			items := []WorkItem{}
			for _, it := range g.Waiting {
				if it.Kind == "upstream" {
					items = append(items, it)
				}
			}
			if len(items) != tc.wantItems {
				t.Fatalf("上游等待项 = %d 条，期望 %d 条：%+v", len(items), tc.wantItems, items)
			}
			if tc.wantItems == 0 {
				return
			}
			if items[0].Stage != tc.wantStage {
				t.Errorf("Stage = %q，期望 %q", items[0].Stage, tc.wantStage)
			}
			if items[0].CanRemind != tc.wantRemind {
				t.Errorf("CanRemind = %v，期望 %v", items[0].CanRemind, tc.wantRemind)
			}
		})
	}
}

// R12／AC-60：审批件的超期标红阈值取项目规则设置，与「审批超时」卡点同源；未配置回落默认 3 天。
func TestMyWorkApprovalTimeoutFromProjectSettings(t *testing.T) {
	now := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)
	me, submitter := int64(1), int64(2)
	submitted := now.AddDate(0, 0, -2) // 已等待 2 天

	cases := []struct {
		name        string
		timeoutDays int
		wantOverdue bool
	}{
		{"未配置回落默认 3 天，等待 2 天不标红", 0, false},
		{"阈值改为 1 天，等待 2 天标红", 1, true},
		{"阈值改为 5 天，等待 2 天不标红", 5, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := MyWork(MyWorkFacts{
				UserID: me, Actor: Actor{Role: RoleMember}, Now: now,
				ApprovalTimeoutDays: tc.timeoutDays,
				Tasks: []WorkTaskFact{
					{ID: 1, Name: "待入池任务", DisplayStatus: TaskDraft, OwnerID: submitter, CreatorID: submitter, KrOwnerID: &me},
				},
				PoolReviews: []WorkApprovalFact{{
					ID: 60, TaskID: 1, TaskName: "待入池任务", SubmittedBy: submitter,
					KrOwnerID: &me, KrOwnerName: "我", SubmittedAt: submitted,
				}},
			})
			if len(g.Approvals) != 1 {
				t.Fatalf("待我审批 = %d 条，期望 1 条", len(g.Approvals))
			}
			if g.Approvals[0].Overdue != tc.wantOverdue {
				t.Fatalf("Overdue = %v，期望 %v", g.Approvals[0].Overdue, tc.wantOverdue)
			}
		})
	}
}

// AC-62（Q4 裁决）：被指定为接收方的访客在「待我接收」看到待接收项并可确认接收，
// 这是其唯一写操作；接收方无审核权，因此不提供退回。
func TestMyWorkViewerReceiptStaysVisible(t *testing.T) {
	now := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)
	me := int64(4)
	g := MyWork(MyWorkFacts{
		UserID: me, Actor: Actor{Role: RoleViewer}, Now: now,
		Receipts: []ReceiptFact{
			{ID: 11, TaskID: 3, TaskName: "接口联调", UserID: me, UserName: "王五", GeneratedAt: now.AddDate(0, 0, -1)},
		},
	})
	if len(g.Receipts) != 1 {
		t.Fatalf("访客待我接收 = %d 条，期望 1 条", len(g.Receipts))
	}
	if g.Receipts[0].ActionLabel != WorkActionHandle {
		t.Fatalf("ActionLabel = %q，期望 %q", g.Receipts[0].ActionLabel, WorkActionHandle)
	}
	if err := CanConfirmReceipt(Actor{Role: RoleMember}, me, ReceiptFact{ID: 11, UserID: me}); err != nil {
		t.Fatalf("访客确认接收应放行，得到 %v", err)
	}
}

func countKind(items []WorkItem, kind string) int {
	n := 0
	for _, it := range items {
		if it.Kind == kind {
			n++
		}
	}
	return n
}
