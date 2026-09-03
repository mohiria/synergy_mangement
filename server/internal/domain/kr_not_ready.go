package domain

// KrInputFact 一条指向 KR 下任务的输入边就绪事实（#150 风险队列未就绪摘要）。
type KrInputFact struct {
	TargetStatus string
	Ready        bool
	Necessity    string
}

// unreadyOnLiveReceiver 报告一条输入边是否「未就绪且接收方还在等」：
// 已完成、已取消的接收方不再等待输入，其输入边不计——
// 两种计数因此都不随「显示已完成」开关摆动（§7.2）。
func unreadyOnLiveReceiver(in KrInputFact) bool {
	return !in.Ready && in.TargetStatus != TaskCompleted && in.TargetStatus != TaskCancelled
}

// CountNotReadyInputs 统计 KR 的「未就绪摘要」（模块 PRD §5.2、§6.1、CR-22；
// 裁决 15 #185 修订）：只计未就绪的**必要**交付物边——参考＝不影响完成，
// 退出风险识别，不再计入。
func CountNotReadyInputs(inputs []KrInputFact) int {
	n := 0
	for _, in := range inputs {
		if unreadyOnLiveReceiver(in) && in.Necessity == NecessityRequired {
			n++
		}
	}
	return n
}

// CountUnreadyReminders 统计 KR 的「提醒」计数（裁决 15，#185；词汇表「提醒」）：
// 上游未就绪但不构成卡点的中性提示——必要与参考的未就绪边都计，
// 风险队列第二档「KR 编号 · 上游未就绪」由它派生（有卡点时第一档优先，规则在前端消费处按档取用）。
func CountUnreadyReminders(inputs []KrInputFact) int {
	n := 0
	for _, in := range inputs {
		if unreadyOnLiveReceiver(in) {
			n++
		}
	}
	return n
}
