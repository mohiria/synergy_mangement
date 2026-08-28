package domain

import (
	"fmt"
	"time"
)

// 风险等级三值（词汇表「风险等级」；主 PRD §5.7）。
const (
	RiskNormal   = "normal"
	RiskWarning  = "warning"
	RiskHighRisk = "high_risk"
)

// RiskTaskFact 派生 KR 风险等级所需的任务事实。
type RiskTaskFact struct {
	ID      int64
	Name    string
	Status  string
	EndDate *time.Time
}

// KrRisk 一个 KR 的风险表达：等级与原因行同源派生，正常态原因行为空串。
type KrRisk struct {
	Level string
	Note  string
}

// DeriveKrRisk 读时派生一个 KR 的风险等级与一行原因（AC-05、主 PRD §5.7）。
//
// 风险等级不落库、无写入路径：
//
//	任务风险 = max(该任务未解除卡点的最高等级, 超期判定, 临期判定)
//	KR 风险  = max(下级任务风险)
//
// 只有处于执行区间（已入池、未终态）的任务参与临期与超期判定——已完成、已取消任务不构成
// 风险；卡点本身已由 DeriveBlockers 按同一执行区间口径派生。blockers 传该 KR 下任务的卡点。
func DeriveKrRisk(now time.Time, dueSoonDays int, tasks []RiskTaskFact, blockers []Blocker) KrRisk {
	if dueSoonDays <= 0 {
		dueSoonDays = DefaultDueSoonDays
	}
	out := KrRisk{Level: RiskNormal}
	raise := func(level, note string) {
		if riskRank(level) > riskRank(out.Level) {
			out.Level, out.Note = level, note
		}
	}
	// 卡点先行：同等级下卡点原因比日期原因更具体，严格大于保证它不被后来的同级事实顶掉。
	for _, b := range blockers {
		raise(b.Level, BlockerRiskNote(b))
	}
	for _, t := range tasks {
		if !blockerTaskInExecution(t.Status) {
			continue
		}
		switch {
		case Overdue(t.EndDate, now):
			raise(RiskHighRisk, fmt.Sprintf("「%s」超期：截止 %s 已过", t.Name, t.EndDate.Format("2006-01-02")))
		case DueSoon(t.EndDate, now, dueSoonDays):
			raise(RiskWarning, fmt.Sprintf("「%s」临期：截止 %s，不足 %d 天", t.Name, t.EndDate.Format("2006-01-02"), dueSoonDays))
		}
	}
	return out
}

// BlockerRiskNote 把一条卡点压成风险原因行（KR 行、风险队列共用同一文案）。
func BlockerRiskNote(b Blocker) string {
	return fmt.Sprintf("「%s」%s：%s", b.TaskName, BlockerKindLabel(b.Kind), b.Reason)
}

// riskRank 风险等级的严重度序，用于取最大值。
func riskRank(level string) int {
	switch level {
	case RiskHighRisk:
		return 2
	case RiskWarning:
		return 1
	}
	return 0
}
