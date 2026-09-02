package domain

// 结构变更（输入、输入源、接收方）：#172 裁决后不再走变更单审批——
// 有编辑权限者直接修改生效（TaskEditRule 同口径），动作写入任务动态并通知所属 KR 负责人。
// 本文件保留动作枚举与中文字段名，供动态摘要与通知文案使用。

// 结构变更动作。
// 裁决 H1（#141）：「输出（交付物项）」已从关键字段清单移除，增删不再走结构变更审批
// （见 DeliverableStructureRule），此处不再有 add_deliverable 动作。
const (
	StructureAddTaskInput   = "add_task_input"
	StructureAddMemberInput = "add_member_input"
	StructureRemoveEdge     = "remove_edge"
	StructureSetReceivers   = "set_receivers"
)

// structureFieldLabels 差异行的中文字段名，对齐 §5.2.B 的关键字段用词。
var structureFieldLabels = map[string]string{
	StructureAddTaskInput:   "任务输入",
	StructureAddMemberInput: "输入源",
	StructureRemoveEdge:     "输入源",
	StructureSetReceivers:   "接收方",
}

// StructureFieldLabel 结构变更在变更单里显示的字段名（派生字段）。
func StructureFieldLabel(op string) string {
	if label, ok := structureFieldLabels[op]; ok {
		return label
	}
	return "任务结构"
}

// ValidStructureOp 报告是否为已知的结构变更动作；未知动作一律拒绝执行，
// 避免旧数据或伪造 payload 在审批通过时触发意料外的写入。
func ValidStructureOp(op string) bool {
	_, ok := structureFieldLabels[op]
	return ok
}

