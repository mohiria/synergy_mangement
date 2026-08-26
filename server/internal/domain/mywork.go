package domain

import "time"

// 我的工作五分组（词汇表「我的工作事项」；AC-16；模块 PRD §3～5）。
// 本文件只做纯派生：输入为项目事实切片，输出为五组卡片事实。

// ApprovalTimeoutDays 审批超时阈值 N（默认 3；模块 PRD §5.4）。
const ApprovalTimeoutDays = 3

type WorkTaskFact struct {
	ID                  int64
	Name                string
	DisplayStatus       string
	OwnerID             int64
	CreatorID           int64
	KrOwnerID           *int64
	EndDate             *time.Time
	UnreadyNote         string
	PoolRejected        *string
	FieldChangeRejected *string
	CompletionRejected  *string
}

type WorkApprovalFact struct {
	ID          int64
	TaskID      int64
	TaskName    string
	SubmittedBy int64
	KrOwnerID   *int64
	KrOwnerName string
	SubmittedAt time.Time
	TaskEnd     *time.Time
	Summary     string
}

type WorkCompletionFact struct {
	ID            int64
	TaskID        int64
	TaskName      string
	SubmittedBy   int64
	TaskOwnerID   int64
	KrOwnerID     *int64
	KrOwnerName   string
	State         string
	Reviewers     []int64
	ReviewerNames []string
	SubmittedAt   time.Time
	TaskEnd       *time.Time
}

type WorkInputRequestFact struct {
	ID          int64
	TaskID      int64
	TaskName    string
	InputName   string
	ContentNote string
	ProviderID  int64
	TaskOwnerID int64
	State       string
	Expected    *time.Time
	CreatedAt   time.Time
	Notified    bool
}

type WorkInviteFact struct {
	ID            int64
	KrDescription string
	InviteeID     int64
	State         string
	Note          string
	CreatedAt     time.Time
}

type WorkBlockerFact struct {
	ID            int64
	TaskID        int64
	TaskName      string
	ActionOwnerID int64
	TaskOwnerID   int64
	KrOwnerID     *int64
	State         string
	Kind          string
	Missing       string
	CreatedAt     time.Time
}

type WorkUpstreamFact struct {
	EdgeID        int64
	TargetTaskID  int64
	TargetName    string
	TargetOwnerID int64
	SourceTaskID  *int64
	SourceName    string
	InputName     string
	Ready         bool
	Necessity     string
}

type MyWorkFacts struct {
	UserID        int64
	Now           time.Time
	Tasks         []WorkTaskFact
	PoolReviews   []WorkApprovalFact
	FieldChanges  []WorkApprovalFact
	Completions   []WorkCompletionFact
	InputRequests []WorkInputRequestFact
	Invites       []WorkInviteFact
	Blockers      []WorkBlockerFact
	Upstreams     []WorkUpstreamFact
}

// WorkItem 单张卡片事实。
type WorkItem struct {
	Kind           string
	Title          string
	TaskID         *int64
	TaskName       string
	RefID          *int64
	Due            *time.Time
	WaitingDays    *int
	Overdue        bool
	RejectedReason string
	UnreadyNote    string
	Stage          string
	DrawerTab      string
}

type MyWorkGroups struct {
	Pending   []WorkItem
	Approvals []WorkItem
	Receipts  []WorkItem
	Waiting   []WorkItem
	Blockers  []WorkItem
}

// MyWork 按模块 PRD §4.1 判定顺序派生五分组；同一事项只归一组，同一任务可因不同事项多组出现。
func MyWork(f MyWorkFacts) MyWorkGroups {
	g := MyWorkGroups{
		Pending:   []WorkItem{},
		Approvals: []WorkItem{},
		Receipts:  []WorkItem{}, // 待接收项随接收方建模落地（当前恒空）
		Waiting:   []WorkItem{},
		Blockers:  []WorkItem{},
	}
	me := f.UserID

	waitingDays := func(since time.Time) (*int, bool) {
		d := int(f.Now.Sub(since).Hours() / 24)
		return &d, d >= ApprovalTimeoutDays
	}
	tid := func(v int64) *int64 { return &v }

	// —— 待我审批（判定顺序 Q1）——
	for _, pr := range f.PoolReviews {
		if pr.KrOwnerID != nil && *pr.KrOwnerID == me {
			days, overdue := waitingDays(pr.SubmittedAt)
			g.Approvals = append(g.Approvals, WorkItem{
				Kind: "pool_review", Title: "[入池审批] " + pr.TaskName,
				TaskID: tid(pr.TaskID), TaskName: pr.TaskName, RefID: tid(pr.ID),
				Due: pr.TaskEnd, WaitingDays: days, Overdue: overdue, DrawerTab: "audit",
			})
		}
	}
	for _, fc := range f.FieldChanges {
		if fc.KrOwnerID != nil && *fc.KrOwnerID == me {
			days, overdue := waitingDays(fc.SubmittedAt)
			g.Approvals = append(g.Approvals, WorkItem{
				Kind: "field_change", Title: "[关键字段修改] " + fc.TaskName,
				TaskID: tid(fc.TaskID), TaskName: fc.TaskName, RefID: tid(fc.ID),
				Due: fc.TaskEnd, WaitingDays: days, Overdue: overdue, DrawerTab: "audit",
			})
		}
	}
	for _, cr := range f.Completions {
		switch cr.State {
		case CompletionIntermediate:
			for _, rv := range cr.Reviewers {
				if rv == me {
					days, overdue := waitingDays(cr.SubmittedAt)
					g.Approvals = append(g.Approvals, WorkItem{
						Kind: "intermediate_review", Title: "[中间审核] " + cr.TaskName,
						TaskID: tid(cr.TaskID), TaskName: cr.TaskName, RefID: tid(cr.ID),
						Due: cr.TaskEnd, WaitingDays: days, Overdue: overdue,
						Stage: "中间或签审核", DrawerTab: "audit",
					})
					break
				}
			}
		case CompletionPendingFinal:
			// AC-16：KR 终审归入待我审批（终审永不豁免）。
			if cr.KrOwnerID != nil && *cr.KrOwnerID == me {
				days, overdue := waitingDays(cr.SubmittedAt)
				g.Approvals = append(g.Approvals, WorkItem{
					Kind: "final_review", Title: "[KR 终审] " + cr.TaskName,
					TaskID: tid(cr.TaskID), TaskName: cr.TaskName, RefID: tid(cr.ID),
					Due: cr.TaskEnd, WaitingDays: days, Overdue: overdue,
					Stage: "KR 终审", DrawerTab: "audit",
				})
			}
		}
	}

	// —— 待我处理（Q3：输入对接人；及本人任务）——
	providerPendingByTask := map[int64]bool{}
	for _, ir := range f.InputRequests {
		if ir.ProviderID == me && ir.Notified && (ir.State == InputRequestPending || ir.State == InputRequestAccepted) {
			providerPendingByTask[ir.TaskID] = true
			days, _ := waitingDays(ir.CreatedAt)
			overdue := ir.Expected != nil && f.Now.After(*ir.Expected)
			g.Pending = append(g.Pending, WorkItem{
				Kind: "input_request", Title: "[输入请求] " + ir.InputName + " → " + ir.TaskName,
				TaskID: tid(ir.TaskID), TaskName: ir.TaskName, RefID: tid(ir.ID),
				Due: ir.Expected, WaitingDays: days, Overdue: overdue, DrawerTab: "overview",
			})
		}
	}
	for _, iv := range f.Invites {
		if iv.InviteeID == me && iv.State == TaskInvitePending {
			g.Pending = append(g.Pending, WorkItem{
				Kind: "invite", Title: "[任务创建邀请] " + iv.KrDescription,
				RefID: tid(iv.ID), Stage: iv.Note, DrawerTab: "overview",
			})
		}
	}
	for _, tk := range f.Tasks {
		switch {
		case tk.OwnerID == me &&
			(tk.DisplayStatus == TaskNotStarted || tk.DisplayStatus == TaskInProgress || tk.DisplayStatus == TaskWaitingInput):
			item := WorkItem{
				Kind: "task", Title: tk.Name, TaskID: tid(tk.ID), TaskName: tk.Name,
				Due: tk.EndDate, UnreadyNote: tk.UnreadyNote, DrawerTab: "overview",
				Overdue: tk.EndDate != nil && f.Now.After(*tk.EndDate),
			}
			// 被退回事项回到提交人的待我处理，卡片带「已退回：理由」（补充规则 3）。
			switch {
			case tk.CompletionRejected != nil:
				item.RejectedReason = *tk.CompletionRejected
			case tk.FieldChangeRejected != nil:
				item.RejectedReason = *tk.FieldChangeRejected
			}
			g.Pending = append(g.Pending, item)
		case tk.CreatorID == me && tk.DisplayStatus == TaskDraft && tk.PoolRejected != nil:
			g.Pending = append(g.Pending, WorkItem{
				Kind: "task_rejected", Title: tk.Name, TaskID: tid(tk.ID), TaskName: tk.Name,
				Due: tk.EndDate, RejectedReason: *tk.PoolRejected, DrawerTab: "audit",
			})
		}
	}

	// —— 等待他人（Q5：停在他人队列；及我任务的上游）——
	for _, up := range f.Upstreams {
		if up.TargetOwnerID == me && !up.Ready && up.Necessity == NecessityRequired && up.SourceTaskID != nil {
			g.Waiting = append(g.Waiting, WorkItem{
				Kind: "upstream", Title: "[上游任务] " + up.SourceName + " → " + up.InputName,
				TaskID: up.SourceTaskID, TaskName: up.SourceName,
				RefID: tid(up.EdgeID), Stage: "等待上游交付", DrawerTab: "overview",
			})
		}
	}
	for _, ir := range f.InputRequests {
		if ir.TaskOwnerID == me && ir.ProviderID != me && (ir.State == InputRequestPending || ir.State == InputRequestAccepted) {
			days, _ := waitingDays(ir.CreatedAt)
			overdue := ir.Expected != nil && f.Now.After(*ir.Expected)
			g.Waiting = append(g.Waiting, WorkItem{
				Kind: "waiting_input_request", Title: "[输入请求] " + ir.InputName + " → " + ir.TaskName,
				TaskID: tid(ir.TaskID), TaskName: ir.TaskName, RefID: tid(ir.ID),
				Due: ir.Expected, WaitingDays: days, Overdue: overdue,
				Stage: "等待对接人提供", DrawerTab: "overview",
			})
		}
	}
	for _, pr := range f.PoolReviews {
		if pr.SubmittedBy == me && !(pr.KrOwnerID != nil && *pr.KrOwnerID == me) {
			days, overdue := waitingDays(pr.SubmittedAt)
			// AC-04：等待他人卡片按当前审批人姓名显示。
			g.Waiting = append(g.Waiting, WorkItem{
				Kind: "waiting_pool", Title: "[入池申请] " + pr.TaskName,
				TaskID: tid(pr.TaskID), TaskName: pr.TaskName, RefID: tid(pr.ID),
				WaitingDays: days, Overdue: overdue,
				Stage: ApprovalWaitingLabel([]string{pr.KrOwnerName}), DrawerTab: "audit",
			})
		}
	}
	for _, fc := range f.FieldChanges {
		if fc.SubmittedBy == me && !(fc.KrOwnerID != nil && *fc.KrOwnerID == me) {
			days, overdue := waitingDays(fc.SubmittedAt)
			g.Waiting = append(g.Waiting, WorkItem{
				Kind: "waiting_field_change", Title: "[关键字段变更] " + fc.TaskName,
				TaskID: tid(fc.TaskID), TaskName: fc.TaskName, RefID: tid(fc.ID),
				WaitingDays: days, Overdue: overdue,
				Stage: ApprovalWaitingLabel([]string{fc.KrOwnerName}), DrawerTab: "audit",
			})
		}
	}
	for _, cr := range f.Completions {
		if cr.TaskOwnerID != me && cr.SubmittedBy != me {
			continue
		}
		if cr.State != CompletionIntermediate && cr.State != CompletionPendingFinal {
			continue
		}
		// 我同时是终审人的情形已在待我审批，避免重复。
		if cr.State == CompletionPendingFinal && cr.KrOwnerID != nil && *cr.KrOwnerID == me {
			continue
		}
		stage := ApprovalWaitingLabel([]string{cr.KrOwnerName})
		if cr.State == CompletionIntermediate {
			stage = ApprovalWaitingLabel(cr.ReviewerNames)
		}
		days, overdue := waitingDays(cr.SubmittedAt)
		g.Waiting = append(g.Waiting, WorkItem{
			Kind: "waiting_completion", Title: "[完成申请] " + cr.TaskName,
			TaskID: tid(cr.TaskID), TaskName: cr.TaskName, RefID: tid(cr.ID),
			WaitingDays: days, Overdue: overdue, Stage: stage, DrawerTab: "audit",
		})
	}

	// —— 与我相关的卡点（Q7）——
	for _, b := range f.Blockers {
		if b.State != BlockerOpen {
			continue
		}
		related := b.ActionOwnerID == me || b.TaskOwnerID == me || (b.KrOwnerID != nil && *b.KrOwnerID == me)
		if !related {
			continue
		}
		// 「等我提供输入」类与待我处理的输入请求同源，不重复计数（模块 PRD §3.2.E）。
		if b.ActionOwnerID == me && providerPendingByTask[b.TaskID] {
			continue
		}
		days, _ := waitingDays(b.CreatedAt)
		g.Blockers = append(g.Blockers, WorkItem{
			Kind: "blocker", Title: "[卡点] " + b.TaskName + "：缺 " + b.Missing,
			TaskID: tid(b.TaskID), TaskName: b.TaskName, RefID: tid(b.ID),
			WaitingDays: days, Stage: b.Kind, DrawerTab: "overview",
		})
	}

	return g
}

// KrRiskNote 派生 KR 行的一行风险原因（AC-05）：优先取 KR 下任务的首个开放卡点事实；
// 无卡点但风险等级非正常时给通用说明。
func KrRiskNote(riskLevel string, blockerNotes []string) string {
	if len(blockerNotes) > 0 {
		return blockerNotes[0]
	}
	if riskLevel != "normal" {
		return "存在待处理的风险因素"
	}
	return ""
}

// ErrReportRangeInvalid 报告时间范围非法。
var ErrReportRangeInvalid = errReportRange{}

type errReportRange struct{}

func (errReportRange) Error() string { return "报告时间范围不合法" }

// ReportRangeFrom 解析报告时间范围下界（AC-19）：today＝当日零点、week＝近 7 天、
// month＝近 30 天、all＝项目整体（无下界）。
func ReportRangeFrom(name string, now time.Time) (*time.Time, error) {
	switch name {
	case "today":
		from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		return &from, nil
	case "week":
		from := now.AddDate(0, 0, -7)
		return &from, nil
	case "month":
		from := now.AddDate(0, 0, -30)
		return &from, nil
	case "all", "":
		return nil, nil
	}
	return nil, ErrReportRangeInvalid
}
