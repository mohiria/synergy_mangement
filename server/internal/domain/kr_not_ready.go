package domain

// KrInputFact 一条指向 KR 下任务的输入边就绪事实（#150 风险队列未就绪摘要）。
type KrInputFact struct {
	TargetStatus string
	Ready        bool
}

// CountNotReadyInputs 统计 KR 的「未就绪摘要」（模块 PRD §5.2、§6.1、CR-22）：
// 计 KR 下未关闭任务作为接收方的全部未就绪交付物边——参考型输入一并计入
// （摘要提示口径，与硬前置卡任务的 required 口径无关）；已完成、已取消的接收方
// 不再等待输入，其输入边不计，计数因此不随「显示已完成」开关摆动（§7.2）。
func CountNotReadyInputs(inputs []KrInputFact) int {
	n := 0
	for _, in := range inputs {
		if in.Ready || in.TargetStatus == TaskCompleted || in.TargetStatus == TaskCancelled {
			continue
		}
		n++
	}
	return n
}
