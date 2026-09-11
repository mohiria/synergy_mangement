package api

import (
	"strings"
	"testing"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"synergy/server/internal/store"
)

// 导出模板与 Report 结构同步（PRD §7.8）：三段正文 + 附录都能渲染，可选字段缺省不报错。
func TestRenderReportHTML(t *testing.T) {
	now := time.Date(2026, 9, 10, 15, 0, 0, 0, time.UTC)
	from := now.AddDate(0, 0, -7)
	i := func(v int) *int { return &v }
	s := func(v string) *string { return &v }
	phase := New
	report := Report{
		Range: ReportRangeWeek, From: &from, GeneratedAt: now,
		Deliveries: ReportDeliveries{CompletedTasks: 1, EffectiveFiles: 1, Objectives: []ReportDeliveryObjective{{
			ObjectiveId: 1, Code: "O1", Title: "提升交付质量",
			KeyResults: []ReportDeliveryKr{{KeyResultId: 2, Code: "KR1.1", Description: "上线自动验收", AverageProgress: i(80), Tasks: []ReportDeliveryTask{
				{TaskId: 3, Code: "T1.1.1", Name: "输出验收方案", OwnerName: "李四", Status: TaskStatusCompleted, StatusLabel: "已完成", CompletedAt: &now,
					Files: []ReportFile{{DeliverableName: "验收方案.docx", FileName: "验收方案V1.docx", EffectiveAt: now}}},
				{TaskId: 4, Code: "T1.1.2", Name: "进行中任务", OwnerName: "李四", Status: TaskStatusInProgress, StatusLabel: "进行中", Progress: i(40), Files: []ReportFile{}},
			}}},
		}}},
		Blockers: ReportBlockers{
			Open: []ReportBlocker{{TaskId: 5, Code: "T1.1.3", TaskName: "临近截止任务", Kind: UpstreamUnready, KindLabel: "上游未就绪", Missing: "上游未完成任务",
				Reason: "必要输入未就绪", Level: Warning, ActionOwnerName: s("李四"), Since: &from, StayDays: 7, Phase: &phase, PhaseLabel: s("本期新出现")}},
			Resolved: []ReportResolvedBlocker{{TaskId: 6, Code: "T1.1.4", TaskName: "已解除任务", Kind: TaskOverdue, KindLabel: "任务超期", Missing: "完成",
				OpenedAt: from, ResolvedAt: now, DurationDays: 7}},
			NewInRange: 1, ResolvedInRange: 1, PendingCompletions: 2,
		},
		NextSteps: ReportNextSteps{HorizonDays: 7,
			Due: []ReportNextStep{
				{TaskId: 5, Code: "T1.1.3", TaskName: "临近截止任务", KeyResultCode: s("KR1.1"), KeyResultDescription: s("上线自动验收"), OwnerName: "李四",
					Status: TaskStatusWaitingInput, StatusLabel: "等待输入", EndDate: openapi_types.Date{Time: now.AddDate(0, 0, 2)}, DueInDays: i(2), UnreadyNote: s("上游未就绪：缺 上游未完成任务")},
				{TaskId: 7, Code: "T1.1.5", TaskName: "超期任务", OwnerName: "张三", Status: TaskStatusInProgress, StatusLabel: "进行中",
					EndDate: openapi_types.Date{Time: now.AddDate(0, 0, -3)}, OverdueDays: i(3)},
			},
			Upcoming: []ReportUpcomingTask{{TaskId: 8, Code: "T1.1.6", TaskName: "下周启动", OwnerName: "张三", Status: TaskStatusNotStarted, StatusLabel: "未开始",
				StartDate: openapi_types.Date{Time: now.AddDate(0, 0, 4)}}},
		},
		OkrProgress: []ReportObjectiveProgress{{ObjectiveId: 1, Code: "O1", Title: "提升交付质量", KeyResults: []ReportKrProgress{
			{KeyResultId: 2, Code: "KR1.1", Description: "上线自动验收", RiskLevel: Normal, TotalTasks: 3, FilledTasks: 2, AverageProgress: i(80), CompletedInRange: 1},
			{KeyResultId: 9, Code: "KR1.2", Description: "空 KR", RiskLevel: HighRisk, TotalTasks: 0, FilledTasks: 0, CompletedInRange: 0},
		}}},
	}
	html, err := renderReportHTML(store.GetProjectRow{Name: "报告试点"}, report)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	for _, want := range []string{
		"报告试点 · 项目报告", "近 7 天（", "任务完成 · 1 份交付物生效", "本期新增 1 · 解除 1", "其中超期 1 项",
		"一、本期成果", "交付物：验收方案V1.docx", "进度 40%", "二、风险与卡点", "另有 2 件完成审核仍在审批队列",
		"本期新出现", "<b>7</b> 天", "本期解除 1 项", "任务超期，2026-09-10 解除）",
		"三、下一步", "到期／超期", "超期 3 天", "即将启动", "附、O／KR 进展", "高风险", "未填",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("导出 HTML 缺少 %q", want)
		}
	}
	// 面向领导的精简版（PRD §7.8）：不再输出文件生效日期、卡点原因／缺失项、KR 行、未就绪注记与已填计数。
	for _, gone := range []string{"生效 2026", "必要输入未就绪", "上游未完成任务", "上游未就绪：", "KR1.1 · 上线自动验收", "已填"} {
		if strings.Contains(html, gone) {
			t.Fatalf("导出 HTML 仍含精简后应去掉的 %q", gone)
		}
	}

	// 项目整体：无起点、不区分阶段、不显示本期新增／解除计数。
	report.Range, report.From = ReportRangeAll, nil
	report.Blockers.Open[0].Phase, report.Blockers.Open[0].PhaseLabel = nil, nil
	html, err = renderReportHTML(store.GetProjectRow{Name: "报告试点"}, report)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if strings.Contains(html, "本期新增") || strings.Contains(html, "本期新出现") || !strings.Contains(html, "项目整体 · 生成时间") {
		t.Fatalf("项目整体范围的头部与阶段标签异常")
	}
}
