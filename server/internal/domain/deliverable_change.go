package domain

import "errors"

// 交付物项增删规则（裁决 H1，#141）：提交完成申请之前，新增／重传／删除完全自由，
// 「输出」不再是关键字段、不生成结构变更单；完成申请提交后到审结前冻结；
// 已发布（有当前内容）的项不可删——删已发布成果必须走成果更新重传（AC-66 不变）。

var (
	ErrDeliverableChangeForbidden = errors.New("无权调整交付物项")
	ErrDeliverableFrozen          = errors.New("完成申请审核期间交付物项不可调整")
	ErrDeliverableStateNotAllowed = errors.New("任务当前状态不能调整交付物项")
	ErrDeliverableHasCurrent      = errors.New("已发布的交付物项不可删除，请发起成果更新重传内容")
)

// DeliverableStructureRule 判定能否增删交付物项：执行类状态放行（裁决 10，#180：
// 负责人／项目管理员——上传链路负责人保留），完成申请在审冻结，终态一律不可。
func DeliverableStructureRule(a Actor, userID int64, t TaskFacts) error {
	switch t.Status {
	case TaskNotStarted, TaskWaitingInput, TaskInProgress:
	case TaskPendingIntermediateReview, TaskPendingFinalReview:
		return ErrDeliverableFrozen
	default:
		return ErrDeliverableStateNotAllowed
	}
	if !OwnerOrProjectAdmin(a, userID, t) {
		return ErrDeliverableChangeForbidden
	}
	return nil
}

// DeleteDeliverableRule 在增删规则之上再挡一条：有当前内容（已终审发布）的项不可删。
// 未发布的项（空／仅候选）可自由删，候选对象文件由调用方同步清理。
// hasCurrent 先于状态判定：已发布项的宿主任务通常已完成，回报要指向成果更新而不是状态错误。
func DeleteDeliverableRule(a Actor, userID int64, t TaskFacts, hasCurrent bool) error {
	if hasCurrent {
		return ErrDeliverableHasCurrent
	}
	return DeliverableStructureRule(a, userID, t)
}
