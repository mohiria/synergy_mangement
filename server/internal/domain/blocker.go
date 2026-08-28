package domain

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// 结构化卡点（词汇表「结构化卡点」；主 PRD §8.4、我的工作 PRD §8.7）。
//
// V4.5 起卡点全部由结构化事实读时派生：没有人工上报、协调人、手动解除，
// 触发条件消失即自动解除。四类触发与待行动人：
//
//	上游未就绪 | 必要输入未就绪且任务已到开始时间 | 上游任务负责人／输入对接人 | 输入就绪
//	任务超期   | 截止已过且未完成                 | 任务负责人                 | 完成或改期生效
//	审批超时   | 当前环节等待达到 N×24 小时       | 该审批人（或签为各审核人） | 审批处理或审批单关闭
//	硬依赖互锁 | 硬前置交付物边成环               | 环内各任务所属 KR 负责人   | 任一边被改掉
const (
	BlockerUpstreamUnready = "upstream_unready"
	BlockerTaskOverdue     = "task_overdue"
	BlockerApprovalTimeout = "approval_timeout"
	BlockerInterlock       = "interlock"
)

// NotifyBlockerRemind 站内通知类型（一键提醒待行动人）。
const NotifyBlockerRemind = "blocker_remind"

// BlockerTaskFact 派生卡点所需的任务事实。
type BlockerTaskFact struct {
	ID          int64
	Name        string
	Status      string // 存储的生命周期状态，不是显示状态
	OwnerID     int64
	OwnerName   string
	KrID        int64
	KrOwnerID   *int64
	KrOwnerName string
	StartDate   *time.Time
	EndDate     *time.Time
}

// BlockerInputFact 指向某任务的一条输入事实（交付物边 + 就绪判定结果）。
type BlockerInputFact struct {
	EdgeID          int64
	TargetTaskID    int64
	InputName       string
	Necessity       string
	Ready           bool
	SourceTaskID    *int64 // 来源为上游任务
	SourceTaskName  string
	SourceOwnerID   int64
	SourceOwnerName string
	ProviderID      int64 // 来源为指定成员时的对接人
	ProviderName    string
	RequestID       int64 // 来源为指定成员时的输入请求 ID（提醒目标寻址用）
}

// BlockerApprovalFact 停在当前环节等待处理的审批件事实。
type BlockerApprovalFact struct {
	Kind          string // pool_review／field_change／intermediate_review／final_review
	RefID         int64
	TaskID        int64
	StageSince    time.Time // 进入当前环节的时间（换环节重新计时）
	ApproverIDs   []int64
	ApproverNames []string
}

// BlockerFacts 派生卡点的全部输入事实。
type BlockerFacts struct {
	Now                 time.Time
	ApprovalTimeoutDays int // 项目级审批超时阈值 N，非正数时取默认值
	Tasks               []BlockerTaskFact
	Inputs              []BlockerInputFact
	Approvals           []BlockerApprovalFact
	HardEdges           []HardEdge
}

// Blocker 一条派生卡点。Key 是合成键（卡点不落库），一键提醒按 Key 寻址。
type Blocker struct {
	Key              string
	Kind             string
	TaskID           int64
	TaskName         string
	Missing          string // 缺失的输入或条件
	Reason           string // 阻塞原因
	ActionOwnerIDs   []int64
	ActionOwnerNames []string
	Level            string
	Since            time.Time
	OccurredAt       time.Time // 真实发生时刻（ADR 0001；ticker 补记与写触发 diff 共用同一时间戳）
	ImpactNote       string
	// 以下为分组与权限判定用的任务事实，不出现在 API 契约里。
	TaskOwnerID int64
	KrOwnerID   *int64
	// InputProviderID 上游未就绪且来源为指定成员时的对接人 ID；其余为 0。
	// 我的工作用它剔除「等我提供输入」类（与输入请求同源，见模块 PRD §3.2.E）。
	InputProviderID int64
}

var approvalStageLabels = map[string]string{
	"pool_review":         "入池审批",
	"field_change":        "关键字段变更审批",
	"intermediate_review": "中间审核",
	"final_review":        "KR 终审",
}

// DeriveBlockers 由四类结构化事实派生当前全部卡点（AC-11）。
//
// 等级 PRD 未定义，按事实严重度定档：任务超期与硬依赖互锁恒为高风险；
// 上游未就绪在任务已超期时升为高风险，否则预警；审批超时等待达到 2N 天升为高风险，否则预警。
func DeriveBlockers(f BlockerFacts) []Blocker {
	n := f.ApprovalTimeoutDays
	if n <= 0 {
		n = ApprovalTimeoutDays
	}
	taskByID := make(map[int64]BlockerTaskFact, len(f.Tasks))
	for _, t := range f.Tasks {
		taskByID[t.ID] = t
	}
	downstream := downstreamTaskNames(f.HardEdges, taskByID)

	out := []Blocker{}
	add := func(t BlockerTaskFact, b Blocker) {
		b.TaskID = t.ID
		b.TaskName = t.Name
		b.TaskOwnerID = t.OwnerID
		b.KrOwnerID = t.KrOwnerID
		b.ImpactNote = downstream[t.ID]
		out = append(out, b)
	}

	// —— 上游未就绪 ——
	for _, in := range f.Inputs {
		t, ok := taskByID[in.TargetTaskID]
		if !ok || !blockerTaskInExecution(t.Status) {
			continue
		}
		if in.Ready || in.Necessity != NecessityRequired {
			continue
		}
		if !Started(t.StartDate, f.Now) {
			continue // 未到开始时间的输入缺失只是风险信号（模块 PRD §8.7）
		}
		b := Blocker{
			Key:        fmt.Sprintf("%s:edge:%d", BlockerUpstreamUnready, in.EdgeID),
			Kind:       BlockerUpstreamUnready,
			Missing:    in.InputName,
			Level:      blockerLevel(Overdue(t.EndDate, f.Now)),
			Since:      *t.StartDate,
			OccurredAt: *t.StartDate,
		}
		switch {
		case in.SourceTaskID != nil:
			b.Reason = fmt.Sprintf("上游任务「%s」尚未交付当前内容", in.SourceTaskName)
			b.ActionOwnerIDs = []int64{in.SourceOwnerID}
			b.ActionOwnerNames = []string{in.SourceOwnerName}
		case in.ProviderID != 0:
			b.Reason = "输入对接人尚未提供内容"
			b.ActionOwnerIDs = []int64{in.ProviderID}
			b.ActionOwnerNames = []string{in.ProviderName}
			b.InputProviderID = in.ProviderID
		default:
			b.Reason = "必要输入尚未指定来源"
		}
		add(t, b)
	}

	// —— 任务超期 ——
	for _, t := range f.Tasks {
		if !blockerTaskInExecution(t.Status) || !Overdue(t.EndDate, f.Now) {
			continue
		}
		add(t, Blocker{
			Key:              fmt.Sprintf("%s:%d", BlockerTaskOverdue, t.ID),
			Kind:             BlockerTaskOverdue,
			Missing:          "按期完成任务",
			Reason:           fmt.Sprintf("截止时间 %s 已过，任务仍未完成", t.EndDate.Format("2006-01-02")),
			ActionOwnerIDs:   []int64{t.OwnerID},
			ActionOwnerNames: []string{t.OwnerName},
			Level:            "high_risk",
			Since:            *t.EndDate,
			OccurredAt:       *t.EndDate,
		})
	}

	// —— 审批超时 ——
	threshold := time.Duration(n) * 24 * time.Hour
	for _, a := range f.Approvals {
		t, ok := taskByID[a.TaskID]
		if !ok || a.StageSince.IsZero() {
			continue
		}
		waited := f.Now.Sub(a.StageSince)
		if waited < threshold {
			continue // 未达阈值只是等待，不成卡点
		}
		stage := approvalStageLabels[a.Kind]
		if stage == "" {
			stage = "审批"
		}
		add(t, Blocker{
			Key:              fmt.Sprintf("%s:%s:%d", BlockerApprovalTimeout, a.Kind, a.RefID),
			Kind:             BlockerApprovalTimeout,
			Missing:          stage + "处理",
			Reason:           fmt.Sprintf("%s已等待 %d 天，超过阈值 %d 天", stage, int(waited.Hours()/24), n),
			ActionOwnerIDs:   append([]int64(nil), a.ApproverIDs...),
			ActionOwnerNames: append([]string(nil), a.ApproverNames...),
			Level:            blockerLevel(waited >= 2*threshold),
			Since:            a.StageSince,
			// 审批超时在「进入环节 + N×24h」那一刻发生，与本次派生时刻无关（ADR 0001）。
			OccurredAt: a.StageSince.Add(threshold),
		})
	}

	// —— 硬依赖互锁 ——
	analysis := AnalyzeHardEdges(f.HardEdges, nil)
	for _, comp := range interlockComponents(f.HardEdges, analysis.Interlocked) {
		ownerIDs, ownerNames := interlockActionOwners(comp, taskByID)
		names := make([]string, 0, len(comp))
		for _, id := range comp {
			if t, ok := taskByID[id]; ok {
				names = append(names, "「"+t.Name+"」")
			}
		}
		reason := "任务 " + strings.Join(names, "、") + " 的硬前置交付物边成环，需改掉其中一条依赖"
		for _, id := range comp {
			t, ok := taskByID[id]
			if !ok {
				continue
			}
			add(t, Blocker{
				Key:              fmt.Sprintf("%s:%d", BlockerInterlock, t.ID),
				Kind:             BlockerInterlock,
				Missing:          "打破硬前置互锁",
				Reason:           reason,
				ActionOwnerIDs:   ownerIDs,
				ActionOwnerNames: ownerNames,
				Level:            "high_risk",
				Since:            f.Now, // 互锁没有独立发生时点，取本次派生时间
				OccurredAt:       f.Now,
			})
		}
	}

	// 高风险在前，其余按任务与键稳定排序。
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].Level == "high_risk") != (out[j].Level == "high_risk") {
			return out[i].Level == "high_risk"
		}
		if out[i].TaskID != out[j].TaskID {
			return out[i].TaskID < out[j].TaskID
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// CanRemindBlocker 卡点卡片的一键提醒判定；与等待他人事项共用同一套提醒目标规则（MW-13）。
func CanRemindBlocker(a Actor, userID int64, b Blocker) bool {
	return CanRemind(a, userID, BlockerRemindTarget(b, nil))
}

// blockerTaskInExecution 判定任务是否处于会产生卡点的执行区间：已入池且未终态。
func blockerTaskInExecution(status string) bool {
	switch status {
	case TaskNotStarted, TaskWaitingInput, TaskInProgress,
		TaskPendingIntermediateReview, TaskPendingFinalReview:
		return true
	}
	return false
}

func blockerLevel(severe bool) string {
	if severe {
		return "high_risk"
	}
	return "warning"
}

// interlockComponents 把互锁边聚成连通分量，每个分量是一个互锁环所涉及的任务集合。
func interlockComponents(edges []HardEdge, interlocked map[int64]bool) [][]int64 {
	parent := map[int64]int64{}
	var find func(int64) int64
	find = func(x int64) int64 {
		if p, ok := parent[x]; ok && p != x {
			parent[x] = find(p)
			return parent[x]
		}
		return x
	}
	touch := func(x int64) {
		if _, ok := parent[x]; !ok {
			parent[x] = x
		}
	}
	for _, e := range edges {
		if !interlocked[e.ID] {
			continue
		}
		touch(e.Source)
		touch(e.Target)
		a, b := find(e.Source), find(e.Target)
		if a != b {
			parent[a] = b
		}
	}
	groups := map[int64][]int64{}
	for n := range parent {
		r := find(n)
		groups[r] = append(groups[r], n)
	}
	out := make([][]int64, 0, len(groups))
	for _, g := range groups {
		sort.Slice(g, func(i, j int) bool { return g[i] < g[j] })
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

// interlockActionOwners 环内各任务所属 KR 负责人去重后作为待行动人。
func interlockActionOwners(comp []int64, taskByID map[int64]BlockerTaskFact) ([]int64, []string) {
	ids := []int64{}
	names := []string{}
	seen := map[int64]bool{}
	for _, id := range comp {
		t, ok := taskByID[id]
		if !ok || t.KrOwnerID == nil || seen[*t.KrOwnerID] {
			continue
		}
		seen[*t.KrOwnerID] = true
		ids = append(ids, *t.KrOwnerID)
		names = append(names, t.KrOwnerName)
	}
	return ids, names
}

// HardDownstreamNotes 沿硬前置边汇总每个任务的下游影响说明（AC-11「定位影响」）；
// 提醒目标与卡点共用同一口径。
func HardDownstreamNotes(edges []HardEdge, tasks []BlockerTaskFact) map[int64]string {
	taskByID := make(map[int64]BlockerTaskFact, len(tasks))
	for _, t := range tasks {
		taskByID[t.ID] = t
	}
	return downstreamTaskNames(edges, taskByID)
}

// downstreamTaskNames 沿硬前置边汇总每个任务的下游影响说明（AC-11「定位影响」）。
func downstreamTaskNames(edges []HardEdge, taskByID map[int64]BlockerTaskFact) map[int64]string {
	if len(edges) == 0 {
		return map[int64]string{}
	}
	next := map[int64][]int64{}
	for _, e := range edges {
		next[e.Source] = append(next[e.Source], e.Target)
	}
	notes := map[int64]string{}
	for from := range next {
		seen := map[int64]bool{from: true}
		queue := []int64{from}
		reached := []string{}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			for _, nx := range next[cur] {
				if seen[nx] {
					continue
				}
				seen[nx] = true
				queue = append(queue, nx)
				if t, ok := taskByID[nx]; ok {
					reached = append(reached, t.Name)
				}
			}
		}
		if len(reached) == 0 {
			continue
		}
		sort.Strings(reached)
		notes[from] = fmt.Sprintf("沿硬前置影响下游 %d 项任务：%s", len(reached), strings.Join(reached, "、"))
	}
	return notes
}

var blockerKindLabels = map[string]string{
	BlockerUpstreamUnready: "上游未就绪",
	BlockerTaskOverdue:     "任务超期",
	BlockerApprovalTimeout: "审批超时",
	BlockerInterlock:       "硬依赖互锁",
}

// BlockerKindLabel 四类卡点的中文类型名（我的工作 PRD §8.7）。
// 行级显示消费本派生字段，前端不再按枚举拼文案（AC-11）。
func BlockerKindLabel(kind string) string {
	if label, ok := blockerKindLabels[kind]; ok {
		return label
	}
	return "卡点"
}
