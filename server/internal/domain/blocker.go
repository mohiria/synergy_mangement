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
//	上游未就绪 | 必要输入未就绪、已到开始时间且任务尚未开始 | 上游任务负责人 | 输入就绪或任务开始
//	任务超期   | 截止已过且未完成                 | 任务负责人                 | 完成或改期生效
//	审批超时   | 当前环节等待达到 N×24 小时       | 该审批人（或签为各审核人） | 审批处理或审批单关闭
//	硬依赖互锁 | 硬前置交付物边成环               | 环内各任务负责人           | 任一边被改掉
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
	ID        int64
	Name      string
	Status    string // 存储的生命周期状态，不是显示状态
	OwnerID   int64
	OwnerName string
	KrID      int64
	StartDate *time.Time
	EndDate   *time.Time
}

// BlockerInputFact 指向某任务的一条输入事实（交付物边 + 就绪判定结果）。
type BlockerInputFact struct {
	EdgeID          int64
	TargetTaskID    int64
	InputName       string
	Necessity       string
	Ready           bool
	SourceTaskID    *int64 // 来源为上游任务
	SourceTaskCode  string // 上游任务编号（#167，形如 T1.1.1）
	SourceTaskName  string
	SourceOwnerID   int64
	SourceOwnerName string
}

// BlockerApprovalFact 停在当前环节等待处理的审批件事实。
type BlockerApprovalFact struct {
	Kind          string // intermediate_review／final_review（裁决 10 后无 cancel_request）
	RefID         int64
	TaskID        int64
	StageSince    time.Time // 进入当前环节的时间（换环节重新计时）
	ApproverIDs   []int64
	ApproverNames []string
}

// BlockerFacts 派生卡点的全部输入事实。
type BlockerFacts struct {
	Now                 time.Time
	ApprovalTimeoutDays int // 项目级审批超时阈值 N（规则设置，AC-60），非正数时取默认值
	Tasks               []BlockerTaskFact
	Inputs              []BlockerInputFact
	Approvals           []BlockerApprovalFact
	RequiredEdges           []RequiredEdge
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
	// 上游任务事实（#167）：仅「上游未就绪」且来源为任务时有值——
	// 卡点条目按「编号＋标题＋负责人」展示，前端只消费不拼算。
	SourceTaskCode  string
	SourceTaskName  string
	SourceOwnerName string
}

// 裁决 10（#180）：关闭申请审批退场，审批超时只对保留的两个环节计时——成果审核、终审
// （裁决 11 #181：「KR 终审」更名「终审」，审批人为项目管理员集合）。
var approvalStageLabels = map[string]string{
	"intermediate_review": "成果审核",
	"final_review":        "终审",
}

// DeriveBlockers 由四类结构化事实派生当前全部卡点（AC-11）。
//
// 等级 PRD 未定义，按事实严重度定档：任务超期与硬依赖互锁恒为高风险；
// 上游未就绪在任务已超期时升为高风险，否则预警；审批超时等待达到 2N 天升为高风险，否则预警。
func DeriveBlockers(f BlockerFacts) []Blocker {
	n := f.ApprovalTimeoutDays
	if n <= 0 {
		n = DefaultApprovalTimeoutDays
	}
	taskByID := make(map[int64]BlockerTaskFact, len(f.Tasks))
	for _, t := range f.Tasks {
		taskByID[t.ID] = t
	}
	downstream := downstreamTaskNames(f.RequiredEdges, taskByID)

	out := []Blocker{}
	add := func(t BlockerTaskFact, b Blocker) {
		b.TaskID = t.ID
		b.TaskName = t.Name
		b.TaskOwnerID = t.OwnerID
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
		if t.Status != TaskNotStarted && t.Status != TaskWaitingInput {
			continue // AC-58：任务一旦开始，上游未就绪自动解除，下游可先行开工
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
		// #178 裁决：输入来源恒为上游任务（输入请求机制退场）。
		if in.SourceTaskID != nil {
			b.Reason = fmt.Sprintf("上游任务「%s」尚未交付当前内容", in.SourceTaskName)
			b.ActionOwnerIDs = []int64{in.SourceOwnerID}
			b.ActionOwnerNames = []string{in.SourceOwnerName}
			// #167：卡点条目按「编号＋标题＋负责人」展示上游任务。
			b.SourceTaskCode = in.SourceTaskCode
			b.SourceTaskName = in.SourceTaskName
			b.SourceOwnerName = in.SourceOwnerName
		} else {
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
	analysis := AnalyzeRequiredEdges(f.RequiredEdges, nil)
	for _, comp := range interlockComponents(f.RequiredEdges, analysis.Interlocked) {
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
	case TaskNotStarted, TaskWaitingInput, TaskInProgress, TaskInReview:
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
func interlockComponents(edges []RequiredEdge, interlocked map[int64]bool) [][]int64 {
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

// interlockActionOwners 环内各任务负责人去重后作为待行动人
// （裁决 12，#183：KR 无负责人，硬依赖互锁的待行动人改为环内各任务负责人）。
func interlockActionOwners(comp []int64, taskByID map[int64]BlockerTaskFact) ([]int64, []string) {
	ids := []int64{}
	names := []string{}
	seen := map[int64]bool{}
	for _, id := range comp {
		t, ok := taskByID[id]
		if !ok || seen[t.OwnerID] {
			continue
		}
		seen[t.OwnerID] = true
		ids = append(ids, t.OwnerID)
		names = append(names, t.OwnerName)
	}
	return ids, names
}

// RequiredDownstreamNotes 沿硬前置边汇总每个任务的下游影响说明（AC-11「定位影响」）；
// 提醒目标与卡点共用同一口径。
func RequiredDownstreamNotes(edges []RequiredEdge, tasks []BlockerTaskFact) map[int64]string {
	taskByID := make(map[int64]BlockerTaskFact, len(tasks))
	for _, t := range tasks {
		taskByID[t.ID] = t
	}
	return downstreamTaskNames(edges, taskByID)
}

// downstreamTaskNames 沿硬前置边汇总每个任务的下游影响说明（AC-11「定位影响」）。
func downstreamTaskNames(edges []RequiredEdge, taskByID map[int64]BlockerTaskFact) map[int64]string {
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
