package domain

import (
	"strings"
	"time"
)

// 开放卡点相对统计范围的阶段（PRD §7.8）。
const (
	ReportPhaseNew     = "new"
	ReportPhaseCarried = "carried"
)

var reportPhaseLabels = map[string]string{
	ReportPhaseNew:     "本期新出现",
	ReportPhaseCarried: "上期遗留",
}

// ReportHorizonDays 下一步窗口天数，随范围变化（PRD §7.8）：
// 今天＝1、近 7 天＝7、近 30 天与项目整体＝30。
func ReportHorizonDays(rangeName string) int {
	switch rangeName {
	case "today":
		return 1
	case "week":
		return 7
	}
	return 30
}

// ReportBlockerPhase 开放卡点是本期新出现还是上期遗留：出现时刻不早于范围起点＝新出现；
// 项目整体（无起点）不区分；出现时刻未知按遗留处理。
func ReportBlockerPhase(since time.Time, from *time.Time) (phase, label string) {
	if from == nil {
		return "", ""
	}
	phase = ReportPhaseCarried
	if !since.IsZero() && !since.Before(*from) {
		phase = ReportPhaseNew
	}
	return phase, reportPhaseLabels[phase]
}

// DaysBetween a 到 b 的自然日差（项目时区）：跨零点即算一天；b 早于 a 为负。
func DaysBetween(a, b time.Time) int {
	da := dayStart(a.In(ProjectLocation))
	db := dayStart(b.In(ProjectLocation))
	return int(db.Sub(da).Hours() / 24)
}

// ReportDueWindow 截止日是否落在下一步窗口内（已超期或今天起 horizonDays 个自然日内到期），
// 以及超期天数（截止次日起算，与 Overdue 同口径）或距截止天数（0＝今天到期）。
func ReportDueWindow(end *time.Time, now time.Time, horizonDays int) (in bool, overdueDays, dueInDays int) {
	if end == nil {
		return false, 0, 0
	}
	days := DaysBetween(now, *end)
	if days < 0 {
		return true, -days, 0
	}
	if days >= horizonDays {
		return false, 0, 0
	}
	return true, 0, days
}

// ReportUpcomingStart 即将启动：尚未开始的任务，计划开始日在明天至窗口末。
func ReportUpcomingStart(start *time.Time, status string, now time.Time, horizonDays int) bool {
	if start == nil || status != "not_started" {
		return false
	}
	days := DaysBetween(now, *start)
	return days >= 1 && days < horizonDays
}

// ParseBlockerActivity 从卡点动态的合成键与定型文案还原类型与缺失项：
// 键形如 `<kind>:...`，文案形如「卡点解除：<类型> · 缺 <缺失项>」。
func ParseBlockerActivity(key, summary string) (kind, missing string) {
	kind, _, _ = strings.Cut(key, ":")
	if _, after, ok := strings.Cut(summary, " · 缺 "); ok {
		missing = after
	}
	return kind, missing
}
