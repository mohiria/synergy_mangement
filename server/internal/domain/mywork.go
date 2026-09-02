package domain

import (
	"sort"
	"strings"
	"time"
)

// 我的工作五分组（词汇表「我的工作事项」；AC-16；模块 PRD §3～5）。
// 本文件只做纯派生：输入为项目事实切片，输出为五组卡片事实。

type WorkTaskFact struct {
	ID                  int64
	Name                string
	DisplayStatus string
	OwnerID       int64
	CreatorID     int64
	KrOwnerID     *int64
	EndDate       *time.Time
	UnreadyNote   string
	// CancelRejected #172 裁决：退回注记只剩关闭申请（关键字段修改不再有退回）。
	CancelRejected     *string
	CompletionRejected *string
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

type WorkInviteFact struct {
	ID            int64
	KrDescription string
	InviteeID     int64
	State         string
	Note          string
	CreatedAt     time.Time
}

type WorkUpstreamFact struct {
	EdgeID          int64
	TargetTaskID    int64
	TargetName      string
	TargetOwnerID   int64
	SourceTaskID    *int64
	SourceName      string
	SourceOwnerID   int64
	SourceOwnerName string
	InputName       string
	Ready           bool
	Necessity       string
	// SourceEndDate #174 裁决：上游等待条目按上游任务截止日期展示与判定超期。
	SourceEndDate *time.Time
}

type MyWorkFacts struct {
	UserID int64
	Actor  Actor
	Now    time.Time
	// ApprovalTimeoutDays 审批超时阈值 N，取项目规则设置（AC-60）；非正数时回落默认值。
	// 与「审批超时」卡点同源，见 BlockerFacts.ApprovalTimeoutDays。
	ApprovalTimeoutDays int
	// 一键提醒当日配额（#129）：canRemind = 权限 && 任一待行动人配额未用完。
	// RemindSentToday 返回（当前用户、被提醒人、任务）三元组今日已发次数；nil 按不限处理。
	RemindDailyLimit int
	RemindSentToday  func(recipientID, taskID int64) int
	Tasks []WorkTaskFact
	// CancelRequests #172 裁决：审批件里的变更单只剩关闭申请。
	CancelRequests []WorkApprovalFact
	Completions    []WorkCompletionFact
	Invites        []WorkInviteFact
	Blockers      []Blocker
	Upstreams     []WorkUpstreamFact
	Receipts      []ReceiptFact
}

// WorkItem 单张卡片事实。
type WorkItem struct {
	Kind           string
	Title          string
	TaskID         *int64
	TaskName       string
	RefID          *int64
	RefKey         string // 提醒目标合成键（卡点为卡点键，等待他人为 wait:<类型>:<ID>）
	Due            *time.Time
	WaitingDays    *int
	Overdue        bool
	RejectedReason string
	UnreadyNote    string
	Stage          string
	DrawerTab      string
	ActionLabel    string
	CanRemind      bool
}

// 上游等待项的阶段文案（MW-14、模块 PRD §4.2 规则 6）：
// 来源任务已关闭时与「尚未交付」分开出文案，提醒该来源的负责人已无意义。
const (
	WorkStageUpstreamWaiting   = "等待上游交付"
	WorkStageUpstreamCancelled = "上游已关闭"
)

// 卡片动作文案（模块 PRD §5.3；AC-55 只用文字按钮）。
// #168（#14 反馈）：只读组（等待他人、卡点）不再派生文字按钮，动作文案为空，点条目行打开抽屉。
const WorkActionHandle = "去处理"

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
		Receipts:  []WorkItem{},
		Waiting:   []WorkItem{},
		Blockers:  []WorkItem{},
	}
	me := f.UserID

	// MW-14：任务关闭后该任务的审批件与输入请求一并消失（卡点侧由「执行中才派生」自然排除）。
	terminal := make(map[int64]bool, len(f.Tasks))
	for _, t := range f.Tasks {
		if t.DisplayStatus == TaskCancelled || t.DisplayStatus == TaskCompleted {
			terminal[t.ID] = true
		}
	}

	taskByID := make(map[int64]WorkTaskFact, len(f.Tasks))
	for _, t := range f.Tasks {
		taskByID[t.ID] = t
	}
	// 等待他人事项的提醒目标（MW-13）：目标是事项本身，不必先成卡点；
	// 任务负责人／KR 负责人与截止时间由所在任务补齐，没有可寻址待行动人时不给提醒入口。
	setWaitRemind := func(item *WorkItem, w RemindWaitFact) {
		if t, ok := taskByID[w.TaskID]; ok {
			w.TaskName = t.Name
			w.TaskOwnerID = t.OwnerID
			w.KrOwnerID = t.KrOwnerID
			w.Due = t.EndDate
		}
		target := WaitRemindTarget(w)
		item.RefKey = target.Key
		item.CanRemind = CanRemind(f.Actor, me, target) &&
			RemindQuotaLeft(target, f.RemindDailyLimit, f.RemindSentToday)
	}

	timeoutDays := f.ApprovalTimeoutDays
	if timeoutDays <= 0 {
		timeoutDays = DefaultApprovalTimeoutDays
	}
	waitingDays := func(since time.Time) (*int, bool) {
		d := int(f.Now.Sub(since).Hours() / 24)
		return &d, d >= timeoutDays
	}
	tid := func(v int64) *int64 { return &v }

	// —— 待我审批（判定顺序 Q1）——
	// #172 裁决：关键字段修改直接生效不再进审批，本组变更类只剩关闭申请。
	for _, fc := range f.CancelRequests {
		if terminal[fc.TaskID] {
			continue
		}
		if fc.KrOwnerID != nil && *fc.KrOwnerID == me {
			days, overdue := waitingDays(fc.SubmittedAt)
			g.Approvals = append(g.Approvals, WorkItem{
				Kind: "cancel_request", Title: "[关闭申请] " + fc.TaskName,
				TaskID: tid(fc.TaskID), TaskName: fc.TaskName, RefID: tid(fc.ID),
				Due: fc.TaskEnd, WaitingDays: days, Overdue: overdue, DrawerTab: "audit",
			})
		}
	}
	for _, cr := range f.Completions {
		if terminal[cr.TaskID] {
			continue
		}
		switch cr.State {
		case CompletionIntermediate:
			for _, rv := range cr.Reviewers {
				if rv == me {
					days, overdue := waitingDays(cr.SubmittedAt)
					g.Approvals = append(g.Approvals, WorkItem{
						Kind: "intermediate_review", Title: "[成果审核] " + cr.TaskName,
						TaskID: tid(cr.TaskID), TaskName: cr.TaskName, RefID: tid(cr.ID),
						Due: cr.TaskEnd, WaitingDays: days, Overdue: overdue,
						Stage: "成果审核（或签）", DrawerTab: "audit",
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

	// —— 待我处理（本人任务与邀请；#178 裁决：输入请求机制退场，「输」类条目删除）——
	for _, iv := range f.Invites {
		if iv.InviteeID == me && iv.State == TaskInvitePending {
			g.Pending = append(g.Pending, WorkItem{
				Kind: "invite", Title: "[任务创建邀请] " + iv.KrDescription,
				RefID: tid(iv.ID), Stage: iv.Note, DrawerTab: "overview",
			})
		}
	}
	for _, tk := range f.Tasks {
		if tk.OwnerID == me &&
			(tk.DisplayStatus == TaskNotStarted || tk.DisplayStatus == TaskInProgress || tk.DisplayStatus == TaskWaitingInput) {
			item := WorkItem{
				Kind: "task", Title: tk.Name, TaskID: tid(tk.ID), TaskName: tk.Name,
				Due: tk.EndDate, UnreadyNote: tk.UnreadyNote, DrawerTab: "overview",
				Overdue: Overdue(tk.EndDate, f.Now),
			}
			// 被退回事项回到提交人的待我处理，卡片带「已退回：理由」（补充规则 3）。
			switch {
			case tk.CompletionRejected != nil:
				item.RejectedReason = *tk.CompletionRejected
			case tk.CancelRejected != nil:
				item.RejectedReason = *tk.CancelRejected
			}
			g.Pending = append(g.Pending, item)
		}
	}

	// —— 待我接收（Q2：本人是接收方且文件已交付、未确认；模块 PRD §3.2.C、§8.6）——
	// 待接收项在终审通过时按当时接收方名单落库，本处只按「本人且未确认」筛选，不再重算名单。
	for _, rc := range f.Receipts {
		if rc.UserID != me || rc.ConfirmedAt != nil {
			continue
		}
		days, _ := waitingDays(rc.GeneratedAt)
		g.Receipts = append(g.Receipts, WorkItem{
			Kind: "receipt", Title: "[待接收] " + rc.TaskName,
			TaskID: tid(rc.TaskID), TaskName: rc.TaskName, RefID: tid(rc.ID),
			WaitingDays: days, Stage: "确认接收", DrawerTab: "overview",
		})
	}

	// —— 等待他人（Q5：停在他人队列；及我任务的上游）——
	for _, up := range f.Upstreams {
		if terminal[up.TargetTaskID] {
			continue // MW-14：下游任务进终态后本人不再等待任何上游
		}
		if up.TargetOwnerID == me && !up.Ready && up.Necessity == NecessityRequired && up.SourceTaskID != nil {
			// #174 裁决：期望时间取消，上游等待条目按上游任务截止日期展示并判定超期。
			item := WorkItem{
				Kind: "upstream", Title: "[上游任务] " + up.SourceName + " → " + up.InputName,
				TaskID: up.SourceTaskID, TaskName: up.SourceName,
				Due: up.SourceEndDate, Overdue: Overdue(up.SourceEndDate, f.Now),
				RefID: tid(up.EdgeID), Stage: WorkStageUpstreamWaiting, DrawerTab: "overview",
			}
			if src, ok := taskByID[*up.SourceTaskID]; ok && src.DisplayStatus == TaskCancelled {
				// 来源已关闭：输入仍未就绪，但催上游负责人交付已无意义，只留卡片说明该改指来源
				// （模块 PRD §4.2 规则 6）。
				item.Stage = WorkStageUpstreamCancelled
			} else {
				// 提醒目标针对被卡住的下游任务（与「上游未就绪」卡点同口径），待行动人是上游任务负责人。
				setWaitRemind(&item, UpstreamWaitFact(up.EdgeID, up.TargetTaskID, up.InputName,
					up.SourceName, up.SourceOwnerID, up.SourceOwnerName))
			}
			g.Waiting = append(g.Waiting, item)
		}
	}
	for _, fc := range f.CancelRequests {
		if terminal[fc.TaskID] {
			continue
		}
		if fc.SubmittedBy == me && !(fc.KrOwnerID != nil && *fc.KrOwnerID == me) {
			days, overdue := waitingDays(fc.SubmittedAt)
			item := WorkItem{
				Kind: "waiting_cancel_request", Title: "[关闭申请] " + fc.TaskName,
				TaskID: tid(fc.TaskID), TaskName: fc.TaskName, RefID: tid(fc.ID),
				WaitingDays: days, Overdue: overdue,
				Stage: ApprovalWaitingLabel([]string{fc.KrOwnerName}), DrawerTab: "audit",
			}
			setWaitRemind(&item, ApprovalWaitFact("cancel_request", fc.ID, fc.TaskID, singleApprover(fc.KrOwnerID), []string{fc.KrOwnerName}, days))
			g.Waiting = append(g.Waiting, item)
		}
	}
	for _, cr := range f.Completions {
		if terminal[cr.TaskID] {
			continue
		}
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
		item := WorkItem{
			Kind: "waiting_completion", Title: "[完成申请] " + cr.TaskName,
			TaskID: tid(cr.TaskID), TaskName: cr.TaskName, RefID: tid(cr.ID),
			WaitingDays: days, Overdue: overdue, Stage: stage, DrawerTab: "audit",
		}
		wait := ApprovalWaitFact("final_review", cr.ID, cr.TaskID, singleApprover(cr.KrOwnerID), []string{cr.KrOwnerName}, days)
		if cr.State == CompletionIntermediate {
			wait = ApprovalWaitFact("intermediate_review", cr.ID, cr.TaskID,
				append([]int64(nil), cr.Reviewers...), cr.ReviewerNames, days)
		}
		setWaitRemind(&item, wait)
		g.Waiting = append(g.Waiting, item)
	}

	// —— 与我相关的卡点（Q7）——
	for _, b := range f.Blockers {
		isActionOwner := false
		for _, id := range b.ActionOwnerIDs {
			if id == me {
				isActionOwner = true
				break
			}
		}
		if !isActionOwner && b.TaskOwnerID != me && !(b.KrOwnerID != nil && *b.KrOwnerID == me) {
			continue
		}
		days, _ := waitingDays(b.Since)
		// #168（#15 反馈）：卡点条目补所属任务截止，参与超期标红与组内排序。
		var due *time.Time
		if t, ok := taskByID[b.TaskID]; ok {
			due = t.EndDate
		}
		g.Blockers = append(g.Blockers, WorkItem{
			Kind: "blocker", Title: "[卡点] " + b.TaskName + "：缺 " + b.Missing,
			TaskID: tid(b.TaskID), TaskName: b.TaskName, RefKey: b.Key,
			Due: due, Overdue: Overdue(due, f.Now),
			WaitingDays: days, Stage: BlockerKindLabel(b.Kind), DrawerTab: "overview",
		})
	}

	sortWorkGroups(f.Now, &g)
	decorateWorkCards(f, &g)
	return g
}

// sortWorkGroups 五组共用的固定排序（模块 PRD §7.1、MW-15）：
// 超期最前，其次今天到期，再按截止／期望时间升序；没有时间字段的事项排在最后并按已等待
// 时长降序；同一紧急层级内，被阻塞（上游未就绪）的事项沉一档。不提供用户自选排序。
func sortWorkGroups(now time.Time, g *MyWorkGroups) {
	for _, group := range [][]WorkItem{g.Pending, g.Approvals, g.Receipts, g.Waiting, g.Blockers} {
		items := group
		sort.SliceStable(items, func(i, j int) bool {
			bi, bj := workUrgency(now, items[i]), workUrgency(now, items[j])
			if bi != bj {
				return bi < bj
			}
			// 同一紧急层级内被阻塞的沉一档
			si, sj := workBlocked(items[i]), workBlocked(items[j])
			if si != sj {
				return !si
			}
			if bi == workUrgencyUndated {
				return workWaitingDays(items[i]) > workWaitingDays(items[j])
			}
			if items[i].Due != nil && items[j].Due != nil && !items[i].Due.Equal(*items[j].Due) {
				return items[i].Due.Before(*items[j].Due)
			}
			return false
		})
	}
}

// 紧急层级：0 超期、1 今天到期、2 有时间、3 无时间。
const (
	workUrgencyOverdue = iota
	workUrgencyToday
	workUrgencyDated
	workUrgencyUndated
)

func workUrgency(now time.Time, it WorkItem) int {
	if it.Overdue {
		return workUrgencyOverdue
	}
	if it.Due == nil {
		return workUrgencyUndated
	}
	if DueToday(it.Due, now) {
		return workUrgencyToday
	}
	return workUrgencyDated
}

func workBlocked(it WorkItem) bool { return it.UnreadyNote != "" }

func workWaitingDays(it WorkItem) int {
	if it.WaitingDays == nil {
		return 0
	}
	return *it.WaitingDays
}

// decorateWorkCards 按分组补齐卡片动作（模块 PRD §5.3、MW-13；#168 调整）：
// 待我处理／待我审批／待我接收是本人要办的事，用「去处理」；等待他人与卡点只读、
// 不派生文字按钮（动作文案为空，点条目行打开抽屉）。
// 提醒事实在各组派生时已按各自的提醒目标定好：等待他人指向事项本身，卡点指向卡点。
func decorateWorkCards(f MyWorkFacts, g *MyWorkGroups) {
	byKey := make(map[string]Blocker, len(f.Blockers))
	for _, b := range f.Blockers {
		byKey[b.Key] = b
	}
	for _, group := range [][]WorkItem{g.Pending, g.Approvals, g.Receipts} {
		for i := range group {
			group[i].ActionLabel = WorkActionHandle
		}
	}
	for i := range g.Blockers {
		if b, ok := byKey[g.Blockers[i].RefKey]; ok {
			g.Blockers[i].CanRemind = CanRemindBlocker(f.Actor, f.UserID, b) &&
				RemindQuotaLeft(BlockerRemindTarget(b, nil), f.RemindDailyLimit, f.RemindSentToday)
		}
	}
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

// singleApprover 单审批人环节的待行动人列表（未指定 KR 负责人时为空，不给提醒入口）。
func singleApprover(id *int64) []int64 {
	if id == nil {
		return nil
	}
	return []int64{*id}
}

// 身份卡「当前职责」（模块 PRD §3.1）：回答「我凭什么会收到这些事项」。
// 事实用的是移出成员时那份职责占位清单（MemberDuties），两处口径同源——
// 会挡住移出的职责，正是会给人派活的职责。

// workResponsibilityOrder 职责显示顺序：从项目级到事项级，横排一行读得下来。
// #178 裁决：输入请求机制退场，「输入对接人」职责随之删除。
var workResponsibilityOrder = []string{
	"项目负责人", "KR 负责人", "任务负责人", "成果审核人", "接收方", "被邀请人",
}

// WorkResponsibilities 派生当前用户在本项目承担的职责标签（读时派生，不落库）。
func WorkResponsibilities(d MemberDuties, isProjectOwner, hasPendingInvite bool) []string {
	held := map[string]bool{
		"项目负责人":  isProjectOwner,
		"KR 负责人": len(d.KeyResults) > 0,
		"任务负责人":  len(d.Tasks) > 0,
		"成果审核人":  len(d.Reviewers) > 0,
		"接收方":    len(d.Receivers) > 0,
		"被邀请人":   hasPendingInvite,
	}
	out := []string{}
	for _, label := range workResponsibilityOrder {
		if held[label] {
			out = append(out, label)
		}
	}
	return out
}

// WorkResponsibilitiesLabel 职责一行文案；一项不担也要说清楚，不留空白。
func WorkResponsibilitiesLabel(list []string) string {
	if len(list) == 0 {
		return "当前未承担行动职责"
	}
	return strings.Join(list, "、")
}

// WorkIdentityRoleLabel 身份卡的身份文案：优先成员角色；项目负责人可以不在成员表里
// （CanReadProject 认负责人身份），那时不回显空串。
func WorkIdentityRoleLabel(role string, isProjectOwner bool) string {
	if label, ok := memberRoleLabels[role]; ok {
		return label
	}
	if isProjectOwner {
		return "项目负责人"
	}
	return "非项目成员"
}
