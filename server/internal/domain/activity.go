package domain

import "time"

// 任务动态类型（词汇表「任务动态」；ADR 0002）。
const (
	ActivityPoolSubmitted        = "pool_submitted"
	ActivityPoolApproved         = "pool_approved"
	ActivityPoolRejected         = "pool_rejected"
	ActivityFieldChangeSubmitted = "field_change_submitted"
	ActivityFieldChangeApproved  = "field_change_approved"
	ActivityFieldChangeRejected  = "field_change_rejected"
	ActivityFieldChangeAbandoned = "field_change_abandoned"
	ActivityCompletionSubmitted  = "completion_submitted"
	ActivityCompletionApproved   = "completion_approved"
	ActivityCompletionRejected   = "completion_rejected"
	ActivityBlockerOpened        = "blocker_opened"
	ActivityBlockerResolved      = "blocker_resolved"
)

var activityKindLabels = map[string]string{
	ActivityPoolSubmitted:        "提交入池审批",
	ActivityPoolApproved:         "入池审批通过",
	ActivityPoolRejected:         "入池审批退回",
	ActivityFieldChangeSubmitted: "提交关键字段修改",
	ActivityFieldChangeApproved:  "关键字段修改生效",
	ActivityFieldChangeRejected:  "关键字段修改退回",
	ActivityFieldChangeAbandoned: "放弃关键字段修改",
	ActivityCompletionSubmitted:  "提交完成申请",
	ActivityCompletionApproved:   "完成审核通过",
	ActivityCompletionRejected:   "完成审核退回",
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

func blockerActivity(b Blocker, kind string, now time.Time) TaskActivity {
	return TaskActivity{
		TaskID:     b.TaskID,
		Kind:       kind,
		Summary:    ActivityKindLabel(kind) + "：" + BlockerKindLabel(b.Kind) + " · 缺 " + b.Missing,
		OccurredAt: now,
	}
}
