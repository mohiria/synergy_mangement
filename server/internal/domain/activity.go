package domain

import "time"

// 任务动态类型（词汇表「任务动态」；ADR 0002）。
const (
	ActivityPoolEntered = "pool_entered"
	// ActivityFieldEdited #172 裁决：关键字段（含结构字段）直接修改生效的动态。
	ActivityFieldEdited = "field_edited"
	// field_change_* 为 #172 前的变更单动态类型：不再产生新记录，仅供历史回显。
	ActivityFieldChangeSubmitted = "field_change_submitted"
	ActivityFieldChangeApproved  = "field_change_approved"
	ActivityFieldChangeRejected  = "field_change_rejected"
	ActivityFieldChangeAbandoned = "field_change_abandoned"
	// ActivityTaskClosed 裁决 10（#180）：项目管理员直接关闭任务的动态。
	ActivityTaskClosed = "task_closed"
	// cancel_* 为裁决 10 前关闭申请机制的动态类型：不再产生新记录，仅供历史回显。
	ActivityCancelRequested = "cancel_requested"
	ActivityCancelApproved  = "cancel_approved"
	ActivityCancelRejected  = "cancel_rejected"
	ActivityResultUpdateStarted  = "result_update_started"
	ActivityCompletionSubmitted  = "completion_submitted"
	ActivityCompletionApproved   = "completion_approved"
	ActivityCompletionRejected   = "completion_rejected"
	ActivityReceiptConfirmed     = "receipt_confirmed"
	ActivityBlockerOpened        = "blocker_opened"
	ActivityBlockerResolved      = "blocker_resolved"
)

var activityKindLabels = map[string]string{
	ActivityPoolEntered:          "任务入池",
	ActivityFieldEdited:          "任务字段修改",
	ActivityFieldChangeSubmitted: "提交关键字段修改",
	ActivityFieldChangeApproved:  "关键字段修改生效",
	ActivityFieldChangeRejected:  "关键字段修改退回",
	ActivityFieldChangeAbandoned: "放弃关键字段修改",
	ActivityTaskClosed:           "任务关闭",
	ActivityCancelRequested:      "发起任务关闭申请",
	ActivityCancelApproved:       "任务关闭生效",
	ActivityCancelRejected:       "任务关闭退回",
	ActivityResultUpdateStarted:  "发起成果更新",
	ActivityCompletionSubmitted:  "提交完成申请",
	ActivityCompletionApproved:   "完成审核通过",
	ActivityCompletionRejected:   "完成审核退回",
	ActivityReceiptConfirmed:     "确认接收",
	ActivityBlockerOpened:        "卡点出现",
	ActivityBlockerResolved:      "卡点解除",
}

// TaskActivity 任务动态的一条事实：已经发生、不可撤销，只记录不派生。
type TaskActivity struct {
	TaskID     int64
	Kind       string
	ActorID    *int64 // 系统派生事件为空
	Summary    string
	OccurredAt time.Time
	BlockerKey string // 卡点类动态的合成键（落库唯一键用，业务动作为空）
}

// ActivityKindLabel 动态类型的中文名（派生字段）；未知类型退化为通用词，不返回枚举原文。
func ActivityKindLabel(kind string) string {
	if label, ok := activityKindLabels[kind]; ok {
		return label
	}
	return "任务动态"
}

// BlockerActivityDiff 比较业务写操作前后的派生卡点集合，产出出现与解除两组动态
// （ADR 0001／0002；模块 PRD §8.7）。卡点没有持久身份，只能按合成键认同一条：
// 键在 after 里新出现记「卡点出现」，在 after 里消失记「卡点解除」；
// 键两侧都在的（哪怕等级变了）不是新事实，不记。出现在前、解除在后，各自保持入参顺序。
func BlockerActivityDiff(before, after []Blocker, now time.Time) []TaskActivity {
	beforeKeys := make(map[string]struct{}, len(before))
	for _, b := range before {
		beforeKeys[b.Key] = struct{}{}
	}
	afterKeys := make(map[string]struct{}, len(after))
	for _, b := range after {
		afterKeys[b.Key] = struct{}{}
	}
	out := []TaskActivity{}
	for _, b := range after {
		if _, existed := beforeKeys[b.Key]; !existed {
			out = append(out, blockerActivity(b, ActivityBlockerOpened, now))
		}
	}
	for _, b := range before {
		if _, still := afterKeys[b.Key]; !still {
			out = append(out, blockerActivity(b, ActivityBlockerResolved, now))
		}
	}
	return out
}

// blockerActivity 一条卡点动态。出现取卡点的真实发生时刻——写触发的 diff 与每小时 ticker
// 因此对同一条卡点产出完全相同的一行，落库唯一键即可挡住重复记账（ADR 0001）；
// 解除没有可计算的发生时刻，取本次比对时刻。
func blockerActivity(b Blocker, kind string, now time.Time) TaskActivity {
	at := now
	if kind == ActivityBlockerOpened && !b.OccurredAt.IsZero() {
		at = b.OccurredAt
	}
	return TaskActivity{
		TaskID:     b.TaskID,
		Kind:       kind,
		Summary:    ActivityKindLabel(kind) + "：" + BlockerKindLabel(b.Kind) + " · 缺 " + b.Missing,
		OccurredAt: at,
		BlockerKey: b.Key,
	}
}

// IsTimeTriggeredBlocker 判定是否时间型卡点（ADR 0001）：审批超时与任务超期只随时间流逝出现，
// 没有对应的业务写操作，因此写触发的 diff 抓不到它们的「出现」。
func IsTimeTriggeredBlocker(kind string) bool {
	return kind == BlockerApprovalTimeout || kind == BlockerTaskOverdue
}

// TimeTriggeredBlockerActivities 每小时 ticker 扫描活跃项目后要补记的「卡点出现」动态。
// 只补时间型卡点：其余三类都有写操作可依附，由 BlockerActivityDiff 记账。
// 解除同理不在这里补——时间型卡点的解除（完成、改期、审批处理）一定伴随写操作。
func TimeTriggeredBlockerActivities(bs []Blocker) []TaskActivity {
	out := []TaskActivity{}
	for _, b := range bs {
		if !IsTimeTriggeredBlocker(b.Kind) {
			continue
		}
		out = append(out, blockerActivity(b, ActivityBlockerOpened, b.OccurredAt))
	}
	return out
}
