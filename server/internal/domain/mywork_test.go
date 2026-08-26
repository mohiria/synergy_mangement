package domain

import (
	"testing"
	"time"
)

// AC-16：五分组派生与判定顺序（模块 PRD §3～4）。
// 同时覆盖 MW-01（负责人视角）、MW-02（提交完成申请后移出待我处理）、MW-04（成员创建任务）、
// MW-05（入池退回回到创建人待我处理）、MW-06（变更单同时进两组）、MW-07／MW-08（或签与终审归属）、
// MW-10／MW-11（输入请求按通知与状态进组）、MW-12（卡点归组与同源去重）、MW-19（邀请退出条件）、
// MW-20（审批等待达阈值标超期）；MW-09 的待接收组在此断言为恒空——接收方尚未建模。
func TestMyWorkGrouping(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	me := int64(5)
	krOwnerMe := i64(5)
	krOwnerOther := i64(7)
	old := now.AddDate(0, 0, -5)
	recent := now.AddDate(0, 0, -1)

	facts := MyWorkFacts{
		UserID: me,
		Now:    now,
		Tasks: []WorkTaskFact{
			// 本人负责进行中 → 待我处理
			{ID: 1, Name: "执行任务", DisplayStatus: TaskInProgress, OwnerID: me, CreatorID: 3, KrOwnerID: krOwnerOther},
			// 本人负责等待输入 → 待我处理（带上游未就绪标记），上游进等待他人
			{ID: 2, Name: "被卡任务", DisplayStatus: TaskWaitingInput, OwnerID: me, CreatorID: me, KrOwnerID: krOwnerOther, UnreadyNote: "上游未就绪：缺 现场数据包"},
			// 本人创建、入池退回的草稿 → 待我处理（带理由）
			{ID: 3, Name: "退回草稿", DisplayStatus: TaskDraft, OwnerID: 9, CreatorID: me, KrOwnerID: krOwnerOther, PoolRejected: sptr("口径不清")},
			// 本人负责、完成申请审批中 → 只在等待他人（状态排除出待我处理）
			{ID: 4, Name: "终审中任务", DisplayStatus: TaskPendingFinalReview, OwnerID: me, CreatorID: me, KrOwnerID: krOwnerOther},
			// 他人的任务 → 不出现
			{ID: 6, Name: "别人的任务", DisplayStatus: TaskInProgress, OwnerID: 9, CreatorID: 9, KrOwnerID: krOwnerOther},
		},
		PoolReviews: []WorkApprovalFact{
			// 本人为 KR 负责人 → 待我审批
			{ID: 11, TaskID: 20, TaskName: "待入池任务", SubmittedBy: 9, KrOwnerID: krOwnerMe, SubmittedAt: old},
			// 他人 KR → 不出现；但提交人是我 → 等待他人
			{ID: 12, TaskID: 3, TaskName: "退回草稿重提", SubmittedBy: me, KrOwnerID: krOwnerOther, SubmittedAt: recent},
		},
		FieldChanges: []WorkApprovalFact{
			// 我提交的变更待审批 → 等待他人（任务本身仍在待我处理）
			{ID: 21, TaskID: 1, TaskName: "执行任务", SubmittedBy: me, KrOwnerID: krOwnerOther, SubmittedAt: recent},
			// 我是 KR 负责人 → 待我审批
			{ID: 22, TaskID: 21, TaskName: "改期任务", SubmittedBy: 9, KrOwnerID: krOwnerMe, SubmittedAt: recent},
		},
		Completions: []WorkCompletionFact{
			// 中间或签、我在组内 → 待我审批
			{ID: 31, TaskID: 22, TaskName: "或签任务", SubmittedBy: 9, TaskOwnerID: 9, KrOwnerID: krOwnerOther, State: CompletionIntermediate, Reviewers: []int64{me, 8}, SubmittedAt: recent},
			// KR 终审、我是 KR 负责人 → 待我审批（AC-16 明确）
			{ID: 32, TaskID: 23, TaskName: "终审任务", SubmittedBy: 9, TaskOwnerID: 9, KrOwnerID: krOwnerMe, State: CompletionPendingFinal, SubmittedAt: recent},
			// 我负责任务的完成申请在终审 → 等待他人
			{ID: 33, TaskID: 4, TaskName: "终审中任务", SubmittedBy: me, TaskOwnerID: me, KrOwnerID: krOwnerOther, State: CompletionPendingFinal, SubmittedAt: recent},
		},
		InputRequests: []WorkInputRequestFact{
			// 我是对接人、待接收、已通知 → 待我处理
			{ID: 41, TaskID: 30, TaskName: "下游任务", InputName: "接口口径", ProviderID: me, TaskOwnerID: 9, State: InputRequestPending, Notified: true, CreatedAt: old},
			// 我是对接人但未通知（任务未入池）→ 不出现
			{ID: 42, TaskID: 31, TaskName: "草稿下游", InputName: "评审意见", ProviderID: me, TaskOwnerID: 9, State: InputRequestPending, Notified: false, CreatedAt: recent},
			// 我任务上我发起的请求（对接人他人）→ 等待他人
			{ID: 43, TaskID: 2, TaskName: "被卡任务", InputName: "现场数据包", ProviderID: 9, TaskOwnerID: me, State: InputRequestAccepted, Notified: true, CreatedAt: recent},
		},
		Invites: []WorkInviteFact{
			// 我被邀请且待处理 → 待我处理
			{ID: 51, KrDescription: "上线自动验收", InviteeID: me, State: TaskInvitePending, CreatedAt: recent},
			// 已完成邀请 → 不出现
			{ID: 52, KrDescription: "上线自动验收", InviteeID: me, State: TaskInviteCompleted, CreatedAt: recent},
		},
		Blockers: []Blocker{
			// 我是待行动人 → 与我相关的卡点
			{Key: "task_overdue:40", TaskID: 40, TaskName: "上游任务", ActionOwnerIDs: []int64{me}, TaskOwnerID: me, KrOwnerID: krOwnerOther, Kind: BlockerTaskOverdue, Missing: "按期完成任务"},
			// 我负责的 KR 下的卡点 → 与我相关的卡点
			{Key: "interlock:41", TaskID: 41, TaskName: "KR 下任务", ActionOwnerIDs: []int64{9}, TaskOwnerID: 9, KrOwnerID: krOwnerMe, Kind: BlockerInterlock, Missing: "打破硬前置互锁"},
			// 「等我提供输入」与待我处理的输入请求同源 → 不进本组（任务 30 上我有待接收请求）
			{Key: "upstream_unready:edge:81", TaskID: 30, TaskName: "下游任务", ActionOwnerIDs: []int64{me}, InputProviderID: me, TaskOwnerID: 9, KrOwnerID: krOwnerOther, Kind: BlockerUpstreamUnready, Missing: "接口口径"},
			// 与我无关 → 不出现
			{Key: "task_overdue:44", TaskID: 44, TaskName: "他人任务", ActionOwnerIDs: []int64{9}, TaskOwnerID: 9, KrOwnerID: krOwnerOther, Kind: BlockerTaskOverdue, Missing: "按期完成任务"},
		},
		Upstreams: []WorkUpstreamFact{
			// 我任务的未就绪必要上游 → 等待他人
			{EdgeID: 71, TargetTaskID: 2, TargetOwnerID: me, SourceTaskID: i64(40), SourceName: "上游任务", InputName: "现场数据包", Ready: false, Necessity: NecessityRequired},
			// 参考输入不进等待他人
			{EdgeID: 72, TargetTaskID: 2, TargetOwnerID: me, SourceTaskID: i64(42), SourceName: "参考上游", InputName: "行业报告", Ready: false, Necessity: NecessityReference},
			// 已就绪不出现
			{EdgeID: 73, TargetTaskID: 1, TargetOwnerID: me, SourceTaskID: i64(43), SourceName: "已就绪上游", InputName: "规范", Ready: true, Necessity: NecessityRequired},
		},
	}

	g := MyWork(facts)

	kinds := func(items []WorkItem) map[string]int {
		m := map[string]int{}
		for _, it := range items {
			m[it.Kind]++
		}
		return m
	}

	// 待我处理：任务 1、2（带标记）、退回草稿 3、输入请求 41、邀请 51 = 5 条
	if len(g.Pending) != 5 {
		t.Fatalf("待我处理数量 = %d, want 5: %+v", len(g.Pending), kinds(g.Pending))
	}
	var foundUnready, foundRejected bool
	for _, it := range g.Pending {
		if it.TaskID != nil && *it.TaskID == 2 && it.UnreadyNote == "上游未就绪：缺 现场数据包" {
			foundUnready = true
		}
		if it.TaskID != nil && *it.TaskID == 3 && it.RejectedReason == "口径不清" {
			foundRejected = true
		}
		if it.TaskID != nil && *it.TaskID == 4 {
			t.Fatalf("完成审批中的任务不应在待我处理: %+v", it)
		}
	}
	if !foundUnready || !foundRejected {
		t.Fatalf("待我处理标记缺失: unready=%v rejected=%v", foundUnready, foundRejected)
	}

	// 待我审批：入池 11、变更 22、中间 31、KR 终审 32 = 4 条（AC-16：终审在本组）
	if len(g.Approvals) != 4 {
		t.Fatalf("待我审批数量 = %d, want 4: %+v", len(g.Approvals), kinds(g.Approvals))
	}
	var hasFinal bool
	for _, it := range g.Approvals {
		if it.Kind == "final_review" {
			hasFinal = true
		}
	}
	if !hasFinal {
		t.Fatal("KR 终审应归入待我审批")
	}

	// 等待他人：上游 71、我发起的输入请求 43、入池申请 12、变更 21、完成申请 33 = 5 条
	if len(g.Waiting) != 5 {
		t.Fatalf("等待他人数量 = %d, want 5: %+v", len(g.Waiting), kinds(g.Waiting))
	}

	// 与我相关的卡点：61、62（63 同源去重、64 已解除）= 2 条
	if len(g.Blockers) != 2 {
		t.Fatalf("卡点数量 = %d, want 2: %+v", len(g.Blockers), kinds(g.Blockers))
	}

	// 待接收：接收方建模未落地，恒为空数组
	if g.Receipts == nil || len(g.Receipts) != 0 {
		t.Fatalf("待我接收应为空数组: %+v", g.Receipts)
	}

	// 等待天数：入池审批 11 提交于 5 天前 → waitingDays=5、超期（阈值 3×24h）
	for _, it := range g.Approvals {
		if it.RefID != nil && *it.RefID == 11 {
			if it.WaitingDays == nil || *it.WaitingDays != 5 || !it.Overdue {
				t.Fatalf("等待天数/超期派生异常: %+v", it)
			}
		}
	}
}

// AC-04：等待他人卡片按当前审批人姓名显示“待{姓名}审批”（模块 PRD §8.2）。
func TestMyWorkWaitingApprovalCopy(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	me := int64(5)
	krOwnerOther := i64(7)
	recent := now.AddDate(0, 0, -1)

	facts := MyWorkFacts{
		UserID: me,
		Now:    now,
		PoolReviews: []WorkApprovalFact{
			{ID: 12, TaskID: 3, TaskName: "重提任务", SubmittedBy: me, KrOwnerID: krOwnerOther, KrOwnerName: "周宁", SubmittedAt: recent},
		},
		FieldChanges: []WorkApprovalFact{
			{ID: 21, TaskID: 1, TaskName: "执行任务", SubmittedBy: me, KrOwnerID: krOwnerOther, KrOwnerName: "周宁", SubmittedAt: recent},
		},
		Completions: []WorkCompletionFact{
			{ID: 33, TaskID: 4, TaskName: "终审中任务", SubmittedBy: me, TaskOwnerID: me, KrOwnerID: krOwnerOther, KrOwnerName: "周宁", State: CompletionPendingFinal, SubmittedAt: recent},
			{ID: 34, TaskID: 5, TaskName: "或签中任务", SubmittedBy: me, TaskOwnerID: me, KrOwnerID: krOwnerOther, KrOwnerName: "周宁", State: CompletionIntermediate, Reviewers: []int64{8, 9}, ReviewerNames: []string{"张三", "李四"}, SubmittedAt: recent},
		},
	}

	g := MyWork(facts)
	wantStage := map[int64]string{
		12: "待周宁审批",
		21: "待周宁审批",
		33: "待周宁审批",
		34: "待张三等2人审批",
	}
	if len(g.Waiting) != len(wantStage) {
		t.Fatalf("等待他人数量 = %d, want %d", len(g.Waiting), len(wantStage))
	}
	for _, it := range g.Waiting {
		if it.RefID == nil {
			t.Fatalf("等待他人卡片缺 RefID: %+v", it)
		}
		if want := wantStage[*it.RefID]; it.Stage != want {
			t.Fatalf("事项 %d Stage = %q, want %q", *it.RefID, it.Stage, want)
		}
	}
}

// AC-05：KR 风险一行原因——有开放卡点时取首个卡点事实，否则风险等级非正常时给通用说明。
func TestKrRiskNote(t *testing.T) {
	if got := KrRiskNote("normal", nil); got != "" {
		t.Fatalf("正常且无卡点不应有原因: %q", got)
	}
	if got := KrRiskNote("warning", nil); got != "存在待处理的风险因素" {
		t.Fatalf("预警无卡点应给通用说明: %q", got)
	}
	notes := []string{"缺 现场数据包：上游未交付"}
	if got := KrRiskNote("normal", notes); got != "缺 现场数据包：上游未交付" {
		t.Fatalf("有卡点应取首个卡点事实: %q", got)
	}
}

// AC-19：报告时间范围解析。
func TestReportRangeFrom(t *testing.T) {
	now := time.Date(2026, 9, 10, 15, 30, 0, 0, time.UTC)
	if from, err := ReportRangeFrom("today", now); err != nil || from == nil || !from.Equal(time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("today 解析异常: %v %v", from, err)
	}
	if from, err := ReportRangeFrom("week", now); err != nil || from == nil || !from.Equal(now.AddDate(0, 0, -7)) {
		t.Fatalf("week 解析异常: %v %v", from, err)
	}
	if from, err := ReportRangeFrom("month", now); err != nil || from == nil || !from.Equal(now.AddDate(0, 0, -30)) {
		t.Fatalf("month 解析异常: %v %v", from, err)
	}
	if from, err := ReportRangeFrom("all", now); err != nil || from != nil {
		t.Fatalf("all 应为无下界: %v %v", from, err)
	}
	if _, err := ReportRangeFrom("year", now); err == nil {
		t.Fatal("非法范围应报错")
	}
}

// 卡点卡片的环节文案用四类中文类型名，不把 kind 枚举原样透给界面（AC-11、MW-12）。
func TestMyWorkBlockerStageUsesKindLabel(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	me := int64(5)
	g := MyWork(MyWorkFacts{
		UserID: me,
		Now:    now,
		Blockers: []Blocker{{
			Key: "approval_timeout:pool_review:9", Kind: BlockerApprovalTimeout,
			TaskID: 1, TaskName: "现场调研", Missing: "入池审批",
			ActionOwnerIDs: []int64{me}, ActionOwnerNames: []string{"我"},
			Level: "high_risk", Since: now.AddDate(0, 0, -4),
		}},
	})
	if len(g.Blockers) != 1 {
		t.Fatalf("应有 1 条卡点事项，实际 %d", len(g.Blockers))
	}
	if g.Blockers[0].Stage != "审批超时" {
		t.Fatalf("卡点事项环节应为中文类型名，实际 %q", g.Blockers[0].Stage)
	}
}
