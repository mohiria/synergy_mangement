package domain

import "strconv"

// O／KR／任务的展示编号（AC-64）。编号由各级持久序号拼出：
// O 是自然数、KR 形如 KR1.1、任务形如 T1.1.1。序号在创建时分配后不再变动，
// 删除同级对象也不重排，因此同一个编号在会议和讨论里始终指向同一个对象。

// ObjectiveCode O 的展示编号。
func ObjectiveCode(seq int) string {
	if seq <= 0 {
		return ""
	}
	return "O" + strconv.Itoa(seq)
}

// KeyResultCode KR 的展示编号，形如 KR1.1。
func KeyResultCode(objectiveSeq, krSeq int) string {
	if objectiveSeq <= 0 || krSeq <= 0 {
		return ""
	}
	return "KR" + strconv.Itoa(objectiveSeq) + "." + strconv.Itoa(krSeq)
}

// TaskCode 任务的展示编号，形如 T1.1.1（#102）。
// T 前缀让任务编号与 O2、KR2.1 一样一眼可辨层级；序号语义不变。
func TaskCode(objectiveSeq, krSeq, taskSeq int) string {
	if objectiveSeq <= 0 || krSeq <= 0 || taskSeq <= 0 {
		return ""
	}
	return "T" + strconv.Itoa(objectiveSeq) + "." + strconv.Itoa(krSeq) + "." + strconv.Itoa(taskSeq)
}

// NextCodeSeq 同级下一个序号：取已有最大值加一，不复用被删对象的序号。
func NextCodeSeq(existing []int) int {
	max := 0
	for _, n := range existing {
		if n > max {
			max = n
		}
	}
	return max + 1
}
