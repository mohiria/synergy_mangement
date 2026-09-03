package domain

import "fmt"

// 审批等待显示文案（AC-04、决策 34）：面向用户统一显示“待{当前审批人姓名}审批”，
// 内部状态名不变，仅显示层转换。

// Approver 一名当前审批人（#186）：ID 用于与查看者比对，Name 用于显示。
type Approver struct {
	ID   int64
	Name string
}

// ZipApprovers 把并行的 ID／姓名切片拼成审批人列表（handler 查询与 mywork 事实
// 结构沿用并行切片，收口处转一次）。长度不齐时缺的 ID 补零值。
func ZipApprovers(ids []int64, names []string) []Approver {
	out := make([]Approver, 0, len(names))
	for i, n := range names {
		var id int64
		if i < len(ids) {
			id = ids[i]
		}
		out = append(out, Approver{ID: id, Name: n})
	}
	return out
}

// ApprovalWaitingLabel 组装“待{审批人姓名}审批”：单人显示姓名，或签多人显示
// “待{首位姓名}等N人审批”，无审批人（或姓名缺失）退化为“待审批”；
// 查看者本人在审批人名单中时显示“待我审批”（多人为“待我等N人审批”，#186）。
func ApprovalWaitingLabel(viewerID int64, approvers []Approver) string {
	// 本人在名单中时即使姓名缺失也算有效条目——“待我审批”不依赖姓名。
	valid := make([]Approver, 0, len(approvers))
	viewerIn := false
	for _, a := range approvers {
		isViewer := viewerID != 0 && a.ID == viewerID
		if a.Name == "" && !isViewer {
			continue
		}
		valid = append(valid, a)
		viewerIn = viewerIn || isViewer
	}
	switch len(valid) {
	case 0:
		return "待审批"
	case 1:
		if viewerIn {
			return "待我审批"
		}
		return "待" + valid[0].Name + "审批"
	}
	if viewerIn {
		return fmt.Sprintf("待我等%d人审批", len(valid))
	}
	return fmt.Sprintf("待%s等%d人审批", valid[0].Name, len(valid))
}

// StatusLabel 派生任务主状态的面向用户显示文案：审核中按申请单环节取当前审批人姓名
// （裁决 13，#182：中间或签取审核组，待终审取项目管理员集合），其余为固定中文标签。
func StatusLabel(status, reviewStage string, viewerID int64, finalReviewers, reviewers []Approver) string {
	switch status {
	case TaskNotStarted:
		return "未开始"
	case TaskWaitingInput:
		return "等待输入"
	case TaskInProgress:
		return "进行中"
	case TaskInReview:
		if reviewStage == CompletionIntermediate {
			return ApprovalWaitingLabel(viewerID, reviewers)
		}
		return ApprovalWaitingLabel(viewerID, finalReviewers)
	case TaskCompleted:
		return "已完成"
	case TaskCancelled:
		return "已关闭"
	}
	return status
}

// CompletionStateLabel 完成申请显示文案：中间或签取审核组姓名，
// 待终审显示项目管理员集合（裁决 11）。
func CompletionStateLabel(state string, viewerID int64, finalReviewers, reviewers []Approver) string {
	switch state {
	case CompletionIntermediate:
		return ApprovalWaitingLabel(viewerID, reviewers)
	case CompletionPendingFinal:
		return ApprovalWaitingLabel(viewerID, finalReviewers)
	case CompletionApproved:
		return "已通过"
	case CompletionRejected:
		return "已退回"
	}
	return state
}

// 当前环节名（词汇表「当前环节」；CurrentStage 的取值域）。
const (
	StageNotStarted         = "待开始执行"
	StageWaitingInput       = "等待输入"
	StageInProgress         = "任务执行"
	StageIntermediateReview = "成果审核（或签）"
	// StageFinalReview 裁决 11（#181）：终审人改项目管理员集合，词条「KR 终审」更名「终审」。
	StageFinalReview = "终审"
	StageCompleted   = "已闭环"
	StageCancelled   = "已关闭"
)

// StageLabel 当前环节的面向用户显示文案（AC-04）：审批等待环节按当前审批人姓名显示，
// 其余环节沿用环节名。
func StageLabel(stage string, viewerID int64, finalReviewers, reviewers []Approver) string {
	switch stage {
	case StageFinalReview:
		return ApprovalWaitingLabel(viewerID, finalReviewers)
	case StageIntermediateReview:
		return ApprovalWaitingLabel(viewerID, reviewers)
	}
	return stage
}

// 枚举显示文案统一在 domain 派生（F1）：与 StatusLabel／BlockerKindLabel 同惯例，
// 前端不再各写一份映射表，免得同一枚举在不同页面出现不同说法。

var riskLevelLabels = map[string]string{
	RiskNormal:   "正常",
	RiskWarning:  "预警",
	RiskHighRisk: "高风险",
}

// RiskLevelLabel 风险等级显示文案。
func RiskLevelLabel(level string) string {
	if label, ok := riskLevelLabels[level]; ok {
		return label
	}
	return "正常"
}

var necessityLabels = map[string]string{
	NecessityRequired:  "必要",
	NecessityReference: "参考",
}

// NecessityLabel 输入必要性显示文案（#173 裁决：关系类型删除，只留必要性）。
func NecessityLabel(necessity string) string {
	if label, ok := necessityLabels[necessity]; ok {
		return label
	}
	return "协作关系"
}

var projectStatusLabels = map[string]string{
	"not_started": "未开始",
	"in_progress": "进行中",
	"completed":   "已完成",
	"archived":    "已归档",
}

// ProjectStatusLabel 项目状态显示文案。
func ProjectStatusLabel(status string) string {
	if label, ok := projectStatusLabels[status]; ok {
		return label
	}
	return "未开始"
}

var memberRoleLabels = map[string]string{
	RoleAdmin:  "项目管理员",
	RoleMember: "项目成员",
	RoleViewer: "访客",
}

// MemberRoleLabel 成员角色显示文案。
func MemberRoleLabel(role string) string {
	if label, ok := memberRoleLabels[role]; ok {
		return label
	}
	return role
}
