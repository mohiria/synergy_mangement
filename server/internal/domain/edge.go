package domain

import "errors"

// 交付物边类型与必要性（词汇表「交付物边」；PRD §4.3）。
const (
	EdgeHardPrerequisite = "hard_prerequisite"
	EdgeInformation      = "information"
	EdgeHandover         = "handover"
	EdgeFeedback         = "feedback"

	NecessityRequired  = "required"
	NecessityReference = "reference"
)

var (
	ErrEdgeTypeInvalid   = errors.New("交付物边类型不合法")
	ErrNecessityInvalid  = errors.New("必要性不合法")
	ErrEdgeSelfLoop      = errors.New("不能以任务自身作为输入来源")
	ErrEdgeSourceMissing = errors.New("必须指定来源任务或对接成员")
)

// NewEdge 待建立的交付物边输入。名称不在这里——它由已有事实派生（EdgeDisplayName、#112）。
type NewEdge struct {
	EdgeType     string
	Necessity    string
	SourceTaskID *int64
	SourceUserID *int64
	TargetTaskID int64
}

// ValidateNewEdge 校验交付物边输入（AC-28、§4.4）；循环关系经多任务表达，自环禁止。
func ValidateNewEdge(e NewEdge) error {
	switch e.EdgeType {
	case EdgeHardPrerequisite, EdgeInformation, EdgeHandover, EdgeFeedback:
	default:
		return ErrEdgeTypeInvalid
	}
	switch e.Necessity {
	case NecessityRequired, NecessityReference:
	default:
		return ErrNecessityInvalid
	}
	if e.SourceTaskID == nil && e.SourceUserID == nil {
		return ErrEdgeSourceMissing
	}
	if e.SourceTaskID != nil && *e.SourceTaskID == e.TargetTaskID {
		return ErrEdgeSelfLoop
	}
	return nil
}

// CanConfigureInputs 判定能否配置任务输入（§3.4）：负责人／创建人／可编辑项目者，终态不可。
func CanConfigureInputs(a Actor, userID int64, t TaskFacts) bool {
	if !CanWriteProject(a) || t.Status == TaskCompleted || t.Status == TaskCancelled {
		return false
	}
	return userID == t.OwnerID || userID == t.CreatorID || CanEditProject(a)
}

// EdgeReady 关系就绪状态（AC-48）：只有已生效的当前内容使关系就绪；候选不提前满足输入，
// 已有当前内容时的候选更新不改变原有就绪状态。
func EdgeReady(hasCurrent, hasCandidate bool) bool {
	return hasCurrent
}

// DeriveDisplayStatus 页面主状态派生（§5.1、§4.4.7 等待输入；AC-58）：后台状态存真实值，
// 「等待输入」只在未开始上叠加——任务一旦进入进行中就不再叠加，下游凭部分提交、
// 中间版本或线下交付先行开工，必要输入未就绪在任何阶段都不阻断动作。
func DeriveDisplayStatus(stored string, hasUnmetRequiredInput bool) string {
	if hasUnmetRequiredInput && stored == TaskNotStarted {
		return TaskWaitingInput
	}
	return stored
}

var (
	ErrEdgeSourceDuplicated   = errors.New("来源任务不能重复选择")
	ErrDeliverableMultiSource = errors.New("指定交付物项时只能选择一个来源任务")
)

// NewTaskInputs 一次配置产生的多条「来源任务 → 目标任务」输入（AC-53：来源任务可多选）。
type NewTaskInputs struct {
	EdgeType       string
	Necessity      string
	SourceTaskIDs  []int64
	TargetTaskID   int64
	HasDeliverable bool
}

// ValidateNewTaskInputs 校验一次多选来源任务的输入配置（AC-53）：至少选一个、同一次不可重复，
// 每条来源仍按单边规则校验；交付物项挂在具体来源任务上，故只在单选时可指定。
func ValidateNewTaskInputs(in NewTaskInputs) error {
	if len(in.SourceTaskIDs) == 0 {
		return ErrEdgeSourceMissing
	}
	if in.HasDeliverable && len(in.SourceTaskIDs) > 1 {
		return ErrDeliverableMultiSource
	}
	seen := make(map[int64]struct{}, len(in.SourceTaskIDs))
	for _, id := range in.SourceTaskIDs {
		if _, dup := seen[id]; dup {
			return ErrEdgeSourceDuplicated
		}
		seen[id] = struct{}{}
		if err := ValidateNewEdge(NewEdge{
			EdgeType: in.EdgeType, Necessity: in.Necessity,
			SourceTaskID: &id, TargetTaskID: in.TargetTaskID,
		}); err != nil {
			return err
		}
	}
	return nil
}

// InputEdgeState 指向同一目标任务的一条输入的就绪事实（AC-53：多来源时每条边一项）。
type InputEdgeState struct {
	Name      string
	Necessity string
	Ready     bool
}

// FirstUnmetRequiredInput 取首条未就绪的必要输入名（AC-48、AC-53）：各边独立判定，
// 任一必要输入未就绪即整体等待输入；参考输入不参与。全部就绪时返回空串。
func FirstUnmetRequiredInput(inputs []InputEdgeState) string {
	for _, in := range inputs {
		if in.Necessity == NecessityRequired && !in.Ready {
			return in.Name
		}
	}
	return ""
}
