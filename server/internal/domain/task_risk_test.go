package domain

import (
	"strings"
	"testing"
	"time"
)

// 裁决 14（#184）：任务级风险等级派生字段——
// 任务风险 = max(该任务未解除卡点的最高等级, 超期→高风险, 临期→预警)；
// 临期阈值取项目规则设置；已完成／已关闭任务恒为正常；临期不是卡点。
func TestDeriveTaskRisk(t *testing.T) {
	now := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	day := func(offset int) *time.Time {
		d := now.AddDate(0, 0, offset)
		return &d
	}
	task := func(status string, end *time.Time) RiskTaskFact {
		return RiskTaskFact{ID: 1, Name: "现场调研", Status: status, EndDate: end}
	}
	warnBlocker := Blocker{
		TaskID: 1, TaskName: "现场调研", Kind: BlockerUpstreamUnready,
		Level: RiskWarning, Reason: "上游任务「地质勘察」尚未交付当前内容",
	}
	highBlocker := Blocker{
		TaskID: 1, TaskName: "现场调研", Kind: BlockerInterlock,
		Level: RiskHighRisk, Reason: "任务 「现场调研」、「地质勘察」 的硬前置交付物边成环，需改掉其中一条依赖",
	}

	cases := []struct {
		name        string
		dueSoonDays int
		task        RiskTaskFact
		blockers    []Blocker
		wantLevel   string
		wantInNote  string // 空串＝期望无原因行
	}{
		{"无卡点不临期为正常", 3, task(TaskInProgress, day(10)), nil, RiskNormal, ""},
		{"临期为预警", 3, task(TaskInProgress, day(2)), nil, RiskWarning, "临期"},
		{"超期为高风险", 3, task(TaskInProgress, day(-1)), nil, RiskHighRisk, "超期"},
		{"预警卡点为预警", 3, task(TaskInProgress, day(10)), []Blocker{warnBlocker}, RiskWarning, "上游未就绪"},
		{"高风险卡点为高风险", 3, task(TaskInProgress, day(10)), []Blocker{highBlocker}, RiskHighRisk, "硬依赖互锁"},
		{"临期叠加高风险卡点取最大", 3, task(TaskInProgress, day(2)), []Blocker{highBlocker}, RiskHighRisk, "硬依赖互锁"},
		{"超期压过预警卡点", 3, task(TaskInProgress, day(-1)), []Blocker{warnBlocker}, RiskHighRisk, "超期"},
		{"同级时卡点原因优先", 3, task(TaskInProgress, day(2)), []Blocker{warnBlocker}, RiskWarning, "上游未就绪"},
		{"已完成不参与判定", 3, task(TaskCompleted, day(-10)), nil, RiskNormal, ""},
		{"已关闭不参与判定", 3, task(TaskCancelled, day(-10)), nil, RiskNormal, ""},
		{"阈值非正回落默认 3 天", 0, task(TaskInProgress, day(2)), nil, RiskWarning, "临期"},
		{"阈值放宽到 7 天", 7, task(TaskInProgress, day(5)), nil, RiskWarning, "临期"},
		{"未开始同样参与判定", 3, task(TaskNotStarted, day(-1)), nil, RiskHighRisk, "超期"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DeriveTaskRisk(now, tc.dueSoonDays, tc.task, tc.blockers)
			if got.Level != tc.wantLevel {
				t.Fatalf("Level = %q, want %q（note=%q）", got.Level, tc.wantLevel, got.Note)
			}
			if tc.wantInNote == "" {
				if got.Note != "" {
					t.Fatalf("正常态不应有原因行: %q", got.Note)
				}
				return
			}
			if !strings.Contains(got.Note, tc.wantInNote) {
				t.Fatalf("原因行缺「%s」: %q", tc.wantInNote, got.Note)
			}
		})
	}
}
