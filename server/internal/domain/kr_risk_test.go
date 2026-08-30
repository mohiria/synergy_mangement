package domain

import (
	"testing"
	"time"
)

func riskDay(y int, m time.Month, d int) *time.Time {
	t := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return &t
}

// TestDeriveKrRisk 覆盖 AC-05：KR 风险等级是读时派生值，
// 由「下级任务卡点的最高等级、超期、临期」取最大值得到，正常态原因行为空串。
func TestDeriveKrRisk(t *testing.T) {
	now := time.Date(2026, 8, 28, 10, 0, 0, 0, ProjectLocation)
	inExec := func(end *time.Time) RiskTaskFact {
		return RiskTaskFact{ID: 1, Name: "现场勘察", Status: TaskInProgress, EndDate: end}
	}
	overdueBlocker := Blocker{
		Kind: BlockerTaskOverdue, TaskID: 1, TaskName: "现场勘察",
		Missing: "按期完成任务", Reason: "截止时间 2026-08-23 已过，任务仍未完成", Level: "high_risk",
	}
	unreadyBlocker := Blocker{
		Kind: BlockerUpstreamUnready, TaskID: 1, TaskName: "现场勘察",
		Missing: "现场数据包", Reason: "上游任务「测绘」尚未交付当前内容", Level: "warning",
	}

	cases := []struct {
		name      string
		dueSoon   int
		tasks     []RiskTaskFact
		blockers  []Blocker
		wantLevel string
		wantNote  string
	}{
		{
			name:      "无任务无卡点为正常且不给原因行",
			dueSoon:   3,
			wantLevel: RiskNormal,
			wantNote:  "",
		},
		{
			name:      "任务在执行但远未到期为正常",
			dueSoon:   3,
			tasks:     []RiskTaskFact{inExec(riskDay(2026, 9, 30))},
			wantLevel: RiskNormal,
			wantNote:  "",
		},
		{
			name:      "临期任务派生预警",
			dueSoon:   3,
			tasks:     []RiskTaskFact{inExec(riskDay(2026, 8, 30))},
			wantLevel: RiskWarning,
			wantNote:  "「现场勘察」临期：截止 2026-08-30，不足 3 天",
		},
		{
			name:      "临期阈值取项目设置",
			dueSoon:   7,
			tasks:     []RiskTaskFact{inExec(riskDay(2026, 9, 2))},
			wantLevel: RiskWarning,
			wantNote:  "「现场勘察」临期：截止 2026-09-02，不足 7 天",
		},
		{
			name:    "已完成与已取消任务不参与风险汇总",
			dueSoon: 3,
			tasks: []RiskTaskFact{
				{ID: 2, Name: "已完成任务", Status: TaskCompleted, EndDate: riskDay(2026, 8, 20)},
				{ID: 3, Name: "已取消任务", Status: TaskCancelled, EndDate: riskDay(2026, 8, 20)},
			},
			wantLevel: RiskNormal,
			wantNote:  "",
		},
		{
			name:      "超期任务派生高风险",
			dueSoon:   3,
			tasks:     []RiskTaskFact{inExec(riskDay(2026, 8, 23))},
			wantLevel: RiskHighRisk,
			wantNote:  "「现场勘察」超期：截止 2026-08-23 已过",
		},
		{
			name:      "预警级卡点派生预警并取卡点原因",
			dueSoon:   3,
			tasks:     []RiskTaskFact{inExec(riskDay(2026, 9, 30))},
			blockers:  []Blocker{unreadyBlocker},
			wantLevel: RiskWarning,
			wantNote:  "「现场勘察」上游未就绪：上游任务「测绘」尚未交付当前内容",
		},
		{
			name:      "高风险卡点压过临期",
			dueSoon:   3,
			tasks:     []RiskTaskFact{inExec(riskDay(2026, 8, 23))},
			blockers:  []Blocker{overdueBlocker},
			wantLevel: RiskHighRisk,
			wantNote:  "「现场勘察」任务超期：截止时间 2026-08-23 已过，任务仍未完成",
		},
		{
			name:      "同为预警时卡点原因优先于临期",
			dueSoon:   3,
			tasks:     []RiskTaskFact{inExec(riskDay(2026, 8, 30))},
			blockers:  []Blocker{unreadyBlocker},
			wantLevel: RiskWarning,
			wantNote:  "「现场勘察」上游未就绪：上游任务「测绘」尚未交付当前内容",
		},
		{
			name:      "阈值缺值回落默认 3 天",
			dueSoon:   0,
			tasks:     []RiskTaskFact{inExec(riskDay(2026, 8, 30))},
			wantLevel: RiskWarning,
			wantNote:  "「现场勘察」临期：截止 2026-08-30，不足 3 天",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DeriveKrRisk(now, c.dueSoon, c.tasks, c.blockers)
			if got.Level != c.wantLevel {
				t.Errorf("Level = %q, want %q", got.Level, c.wantLevel)
			}
			if got.Note != c.wantNote {
				t.Errorf("Note = %q, want %q", got.Note, c.wantNote)
			}
		})
	}
}

// TestDueSoon 锁定临期判定的边界：超期不再算临期，阈值天数按项目时区自然日计。
func TestDueSoon(t *testing.T) {
	now := time.Date(2026, 8, 28, 23, 0, 0, 0, ProjectLocation)
	cases := []struct {
		name string
		due  *time.Time
		days int
		want bool
	}{
		{"无截止日不临期", nil, 3, false},
		{"今天到期算临期", riskDay(2026, 8, 28), 3, true},
		{"阈值内最后一天算临期", riskDay(2026, 8, 30), 3, true},
		{"刚好够阈值天数不算临期", riskDay(2026, 8, 31), 3, false},
		{"已超期不算临期", riskDay(2026, 8, 27), 3, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DueSoon(c.due, now, c.days); got != c.want {
				t.Errorf("DueSoon = %v, want %v", got, c.want)
			}
		})
	}
}

// AC-59 的「与 O」这一半（#82）：O 只取下级 KR 风险的最大值，
// 不叠加 O 自身的临期与超期（Q-02：max(卡点, 临期, 超期) 只适用于任务级对象）。
func TestDeriveObjectiveRisk(t *testing.T) {
	cases := []struct {
		name string
		krs  []KrRisk
		want KrRisk
	}{
		{"没有 KR 时为正常", nil, KrRisk{Level: RiskNormal}},
		{"全部正常时为正常", []KrRisk{{Level: RiskNormal}, {Level: RiskNormal}}, KrRisk{Level: RiskNormal}},
		{"含预警取预警并带原因行", []KrRisk{{Level: RiskNormal}, {Level: RiskWarning, Note: "「联调」临期：截止 2026-09-02，不足 3 天"}},
			KrRisk{Level: RiskWarning, Note: "「联调」临期：截止 2026-09-02，不足 3 天"}},
		{"含高风险取高风险", []KrRisk{{Level: RiskWarning, Note: "临期"}, {Level: RiskHighRisk, Note: "「验收」超期：截止 2026-08-01 已过"}},
			KrRisk{Level: RiskHighRisk, Note: "「验收」超期：截止 2026-08-01 已过"}},
		{"同级取先出现的原因行", []KrRisk{{Level: RiskWarning, Note: "先"}, {Level: RiskWarning, Note: "后"}},
			KrRisk{Level: RiskWarning, Note: "先"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DeriveObjectiveRisk(c.krs)
			if got.Level != c.want.Level || got.Note != c.want.Note {
				t.Fatalf("DeriveObjectiveRisk = %+v, want %+v", got, c.want)
			}
		})
	}
}
