package domain

import (
	"testing"
	"time"
)

// 下一步窗口随范围变化（PRD §7.8）：今天＝1、近 7 天＝7、近 30 天与项目整体＝30。
func TestReportHorizonDays(t *testing.T) {
	cases := map[string]int{"today": 1, "week": 7, "month": 30, "all": 30, "": 30}
	for name, want := range cases {
		if got := ReportHorizonDays(name); got != want {
			t.Fatalf("ReportHorizonDays(%q) = %d, want %d", name, got, want)
		}
	}
}

// 开放卡点相对统计范围的阶段：出现时刻落在范围内＝本期新出现，否则上期遗留；项目整体不区分。
func TestReportBlockerPhase(t *testing.T) {
	from := cst(2026, 9, 3, 0)
	cases := []struct {
		name  string
		since time.Time
		from  *time.Time
		want  string
		label string
	}{
		{"范围内出现", cst(2026, 9, 5, 10), &from, ReportPhaseNew, "本期新出现"},
		{"恰在范围起点出现", from, &from, ReportPhaseNew, "本期新出现"},
		{"范围前出现", cst(2026, 9, 2, 23), &from, ReportPhaseCarried, "上期遗留"},
		{"项目整体不区分", cst(2026, 9, 5, 10), nil, "", ""},
		{"出现时刻未知按遗留", time.Time{}, &from, ReportPhaseCarried, "上期遗留"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, label := ReportBlockerPhase(tc.since, tc.from)
			if got != tc.want || label != tc.label {
				t.Fatalf("ReportBlockerPhase = (%q, %q), want (%q, %q)", got, label, tc.want, tc.label)
			}
		})
	}
}

// 自然日差按项目时区计算：跨零点即算一天，不足 24 小时也算；同一天为 0。
func TestDaysBetween(t *testing.T) {
	cases := []struct {
		name string
		a, b time.Time
		want int
	}{
		{"同一天", cst(2026, 9, 5, 8), cst(2026, 9, 5, 23), 0},
		{"跨零点不足 24 小时", cst(2026, 9, 5, 23), cst(2026, 9, 6, 1), 1},
		{"整三天", cst(2026, 9, 2, 10), cst(2026, 9, 5, 10), 3},
		{"UTC 表示的同一时刻", time.Date(2026, 9, 4, 16, 30, 0, 0, time.UTC), cst(2026, 9, 6, 9), 1},
		{"b 早于 a 为负", cst(2026, 9, 6, 0), cst(2026, 9, 5, 0), -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DaysBetween(tc.a, tc.b); got != tc.want {
				t.Fatalf("DaysBetween = %d, want %d", got, tc.want)
			}
		})
	}
}

// 下一步的「到期／超期」：截止日在今天起 horizon 个自然日内（含今天，今天＝1 只看今天），或已超期。
// 超期天数从截止次日起算（与 Overdue 同口径），未超期给距截止天数（0＝今天到期）。
func TestReportDueWindow(t *testing.T) {
	now := cst(2026, 9, 10, 15)
	day := func(d int) *time.Time {
		v := time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC) // pgx 扫 DATE 的形态
		return &v
	}
	cases := []struct {
		name        string
		end         *time.Time
		horizon     int
		in          bool
		overdueDays int
		dueInDays   int
	}{
		{"今天到期", day(10), 1, true, 0, 0},
		{"明天到期，窗口 1 天不含", day(11), 1, false, 0, 0},
		{"明天到期，窗口 7 天", day(11), 7, true, 0, 1},
		{"窗口末日（今天起第 7 天）", day(16), 7, true, 0, 6},
		{"窗口外", day(17), 7, false, 0, 0},
		{"超期三天", day(7), 1, true, 3, 0},
		{"昨天截止算超期一天", day(9), 7, true, 1, 0},
		{"无截止日不入窗口", nil, 30, false, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in, od, di := ReportDueWindow(tc.end, now, tc.horizon)
			if in != tc.in || od != tc.overdueDays || di != tc.dueInDays {
				t.Fatalf("ReportDueWindow = (%v, %d, %d), want (%v, %d, %d)",
					in, od, di, tc.in, tc.overdueDays, tc.dueInDays)
			}
		})
	}
}

// 即将启动：尚未开始的任务，计划开始日在明天至窗口末（今天起第 horizon 天）。
func TestReportUpcomingStart(t *testing.T) {
	now := cst(2026, 9, 10, 15)
	day := func(d int) *time.Time {
		v := time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC)
		return &v
	}
	cases := []struct {
		name    string
		start   *time.Time
		status  string
		horizon int
		want    bool
	}{
		{"明天启动", day(11), "not_started", 7, true},
		{"窗口末日启动", day(16), "not_started", 7, true},
		{"窗口外", day(17), "not_started", 7, false},
		{"今天已到开始日不算即将", day(10), "not_started", 7, false},
		{"过去的开始日", day(1), "not_started", 7, false},
		{"窗口 1 天时明天不含", day(11), "not_started", 1, false},
		{"已开始的任务不算", day(12), "in_progress", 7, false},
		{"等待输入的任务不算", day(12), "waiting_input", 7, false},
		{"无开始日", nil, "not_started", 7, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReportUpcomingStart(tc.start, tc.status, now, tc.horizon); got != tc.want {
				t.Fatalf("ReportUpcomingStart = %v, want %v", got, tc.want)
			}
		})
	}
}

// 已解除卡点只剩动态记录：类型取合成键前缀，缺失项取定型文案里「缺 」之后的部分。
func TestParseBlockerActivity(t *testing.T) {
	cases := []struct {
		name, key, summary, kind, missing string
	}{
		{"上游未就绪", "upstream_unready:edge:12", "卡点解除：上游未就绪 · 缺 方案初稿", BlockerUpstreamUnready, "方案初稿"},
		{"审批超时", "approval_timeout:completion:3", "卡点出现：审批超时 · 缺 终审决定", BlockerApprovalTimeout, "终审决定"},
		{"缺失项含分隔符", "task_overdue:7", "卡点解除：任务超期 · 缺 缺 A · B", BlockerTaskOverdue, "缺 A · B"},
		{"无缺失段", "interlock:9", "卡点解除：硬依赖互锁", BlockerInterlock, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kind, missing := ParseBlockerActivity(tc.key, tc.summary)
			if kind != tc.kind || missing != tc.missing {
				t.Fatalf("ParseBlockerActivity = (%q, %q), want (%q, %q)", kind, missing, tc.kind, tc.missing)
			}
		})
	}
}
