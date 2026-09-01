package domain

// SelectTopBlocker 在一个 KR 的卡点里挑「风险最高」的一条（#122 风险队列副行）：
// 等级高者优先；同级按等待更久（Since 更早）者优先；再同则按 Key 字典序保证稳定。
// 无卡点返回 nil。
func SelectTopBlocker(blockers []Blocker) *Blocker {
	var top *Blocker
	for i := range blockers {
		b := &blockers[i]
		if top == nil {
			top = b
			continue
		}
		switch {
		case riskRank(b.Level) != riskRank(top.Level):
			if riskRank(b.Level) > riskRank(top.Level) {
				top = b
			}
		case !b.Since.Equal(top.Since):
			if b.Since.Before(top.Since) {
				top = b
			}
		case b.Key < top.Key:
			top = b
		}
	}
	return top
}
