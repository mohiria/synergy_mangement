package domain

// 结构变更：PRD §5.2.B 把「输入、输入源、输出、接收方」列为关键字段，§5.5 明写已入池任务
// 更换来源任务或对接人仍执行关键字段修改审批。这类变更改的是关系与名单而不是标量字段，
// 因此复用同一张变更单（变更类型 structure），差异与待执行动作存 payload，
// 路由沿用 FieldChangeRoute 的三条：草稿直改、KR 负责人本人免审、其余进审批。

// FieldChangeTypeStructure 变更类型：结构变更。
const FieldChangeTypeStructure = "structure"

// 结构变更动作。
const (
	StructureAddTaskInput   = "add_task_input"
	StructureAddMemberInput = "add_member_input"
	StructureRemoveEdge     = "remove_edge"
	StructureAddDeliverable = "add_deliverable"
	StructureSetReceivers   = "set_receivers"
)

// structureFieldLabels 差异行的中文字段名，对齐 §5.2.B 的关键字段用词。
var structureFieldLabels = map[string]string{
	StructureAddTaskInput:   "任务输入",
	StructureAddMemberInput: "输入源",
	StructureRemoveEdge:     "输入源",
	StructureAddDeliverable: "预期交付物",
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

// StructureChangeRoute 路由结构变更（AC-23、§5.2.B）：与关键字段修改完全同源——
// 草稿由创建人／负责人／可编辑项目者直接生效，已入池任务的 KR 负责人本人免审即时生效，
// 其余进入审批（同一任务最多一张待审批变更单，与关闭单互斥）。
func StructureChangeRoute(a Actor, userID int64, t TaskFacts, hasPending bool) (FieldChangeOutcome, error) {
	return FieldChangeRoute(a, userID, t, hasPending)
}
