package domain

import "fmt"

// 审批等待显示文案（AC-04、决策 34）：面向用户统一显示“待{当前审批人姓名}审批”，
// 内部状态名不变，仅显示层转换。

// ApprovalWaitingLabel 组装“待{审批人姓名}审批”：单人显示姓名，或签多人显示
// “待{首位姓名}等N人审批”，无审批人（或姓名缺失）退化为“待审批”。
func ApprovalWaitingLabel(names []string) string {
	valid := make([]string, 0, len(names))
	for _, n := range names {
		if n != "" {
			valid = append(valid, n)
		}
	}
	switch len(valid) {
	case 0:
		return "待审批"
	case 1:
		return "待" + valid[0] + "审批"
	}
	return fmt.Sprintf("待%s等%d人审批", valid[0], len(valid))
}

// StatusLabel 派生任务主状态的面向用户显示文案：审批等待状态按当前审批人姓名显示
// （入池与终审的审批人是所属 KR 负责人，中间或签取审核组），其余为固定中文标签。
func StatusLabel(status, krOwnerName string, reviewerNames []string) string {
	switch status {
	case TaskDraft:
		return "草稿"
	case TaskPendingPoolReview:
		return ApprovalWaitingLabel([]string{krOwnerName})
	case TaskNotStarted:
		return "未开始"
	case TaskWaitingInput:
		return "等待输入"
	case TaskInProgress:
		return "进行中"
	case TaskPendingIntermediateReview:
		return ApprovalWaitingLabel(reviewerNames)
	case TaskPendingFinalReview:
		return ApprovalWaitingLabel([]string{krOwnerName})
	case TaskCompleted:
		return "已完成"
	case TaskCancelled:
		return "已关闭"
	}
	return status
}

// PoolReviewStateLabel 入池审批单显示文案：待审批显示所属 KR 负责人，免审为“免审通过”。
func PoolReviewStateLabel(state string, exempt bool, krOwnerName string) string {
	switch {
	case state == PoolReviewPending:
		return ApprovalWaitingLabel([]string{krOwnerName})
	case exempt:
		return "免审通过"
	case state == PoolReviewApproved:
		return "已通过"
	case state == PoolReviewRejected:
		return "已退回"
	}
	return state
}

// FieldChangeStateLabel 关键字段变更单显示文案：待审批显示所属 KR 负责人，免审为“免审生效”。
func FieldChangeStateLabel(state string, exempt bool, krOwnerName string) string {
	switch {
	case state == FieldChangePendingState:
		return ApprovalWaitingLabel([]string{krOwnerName})
	case exempt:
		return "免审生效"
	case state == FieldChangeApprovedState:
		return "已通过"
	case state == FieldChangeRejectedState:
		return "已退回"
	}
	return state
}

// CompletionStateLabel 完成申请显示文案：中间或签取审核组姓名，待终审显示所属 KR 负责人。
func CompletionStateLabel(state, krOwnerName string, reviewerNames []string) string {
	switch state {
	case CompletionIntermediate:
		return ApprovalWaitingLabel(reviewerNames)
	case CompletionPendingFinal:
		return ApprovalWaitingLabel([]string{krOwnerName})
	case CompletionApproved:
		return "已通过"
	case CompletionRejected:
		return "已退回"
	}
	return state
}

// 当前环节名（词汇表「当前环节」；CurrentStage 的取值域）。
const (
	StageDraft              = "草稿完善"
	StagePoolReview         = "创建入池审批"
	StageNotStarted         = "待开始执行"
	StageWaitingInput       = "等待输入"
	StageInProgress         = "任务执行"
	StageIntermediateReview = "中间或签审核"
	StageFinalReview        = "KR 终审"
	StageCompleted          = "已闭环"
	StageCancelled          = "已关闭"
)

// StageLabel 当前环节的面向用户显示文案（AC-04）：审批等待环节按当前审批人姓名显示，
// 其余环节沿用环节名。
func StageLabel(stage, krOwnerName string, reviewerNames []string) string {
	switch stage {
	case StagePoolReview, StageFinalReview:
		return ApprovalWaitingLabel([]string{krOwnerName})
	case StageIntermediateReview:
		return ApprovalWaitingLabel(reviewerNames)
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

var edgeTypeLabels = map[string]string{
	EdgeHardPrerequisite: "硬前置交付",
	EdgeInformation:      "信息输入",
	EdgeHandover:         "正式成果接收",
	EdgeFeedback:         "迭代／反馈",
}

// EdgeTypeLabel 交付物边类型显示文案。
func EdgeTypeLabel(edgeType string) string {
	if label, ok := edgeTypeLabels[edgeType]; ok {
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
