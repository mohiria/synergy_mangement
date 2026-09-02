package domain

import "testing"

// 审批等待显示文案（AC-04、决策 34）：面向用户统一显示“待{当前审批人姓名}审批”，
// 或签多人为“待{首位姓名}等N人审批”，无审批人时退化为“待审批”。
func TestApprovalWaitingLabel(t *testing.T) {
	cases := []struct {
		name  string
		names []string
		want  string
	}{
		{"单人显示姓名", []string{"周宁"}, "待周宁审批"},
		{"或签多人显示首位加人数", []string{"张三", "李四", "王五"}, "待张三等3人审批"},
		{"两人也按首位加人数", []string{"张三", "李四"}, "待张三等2人审批"},
		{"无审批人退化为待审批", nil, "待审批"},
		{"空姓名退化为待审批", []string{""}, "待审批"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ApprovalWaitingLabel(tc.names); got != tc.want {
				t.Fatalf("ApprovalWaitingLabel(%v) = %q, want %q", tc.names, got, tc.want)
			}
		})
	}
}

// 任务主状态显示文案（AC-04）：审批等待状态按当前审批人姓名显示，其余为固定中文标签；
// 裁决 11（#181）：终审人为项目管理员集合，多人为“待{首位姓名}等N人审批”。
func TestStatusLabel(t *testing.T) {
	cases := []struct {
		name           string
		status         string
		finalReviewers []string
		reviewers      []string
		want           string
	}{
		{"未开始", TaskNotStarted, []string{"周宁"}, nil, "未开始"},
		{"等待输入", TaskWaitingInput, []string{"周宁"}, nil, "等待输入"},
		{"进行中", TaskInProgress, []string{"周宁"}, nil, "进行中"},
		{"中间或签单人", TaskPendingIntermediateReview, []string{"周宁"}, []string{"张三"}, "待张三审批"},
		{"中间或签多人", TaskPendingIntermediateReview, []string{"周宁"}, []string{"张三", "李四"}, "待张三等2人审批"},
		{"终审单管理员显示姓名", TaskPendingFinalReview, []string{"周宁"}, nil, "待周宁审批"},
		{"终审多管理员显示首位加人数", TaskPendingFinalReview, []string{"周宁", "张三"}, nil, "待周宁等2人审批"},
		{"无管理员名单退化", TaskPendingFinalReview, nil, nil, "待审批"},
		{"已完成", TaskCompleted, []string{"周宁"}, nil, "已完成"},
		{"已关闭", TaskCancelled, []string{"周宁"}, nil, "已关闭"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := StatusLabel(tc.status, tc.finalReviewers, tc.reviewers); got != tc.want {
				t.Fatalf("StatusLabel(%q) = %q, want %q", tc.status, got, tc.want)
			}
		})
	}
}

// 审批单状态显示文案（AC-04）：等待状态按当前审批人姓名显示（裁决 10 后只剩完成申请一类）。
func TestReviewStateLabels(t *testing.T) {
	t.Run("完成申请", func(t *testing.T) {
		cases := []struct {
			name           string
			state          string
			finalReviewers []string
			reviewers      []string
			want           string
		}{
			{"中间或签显示审核人", CompletionIntermediate, []string{"周宁"}, []string{"张三", "李四"}, "待张三等2人审批"},
			{"待终审显示管理员集合（裁决 11）", CompletionPendingFinal, []string{"周宁", "张三"}, nil, "待周宁等2人审批"},
			{"已通过", CompletionApproved, []string{"周宁"}, nil, "已通过"},
			{"已退回", CompletionRejected, []string{"周宁"}, nil, "已退回"},
		}
		for _, tc := range cases {
			if got := CompletionStateLabel(tc.state, tc.finalReviewers, tc.reviewers); got != tc.want {
				t.Fatalf("%s: CompletionStateLabel = %q, want %q", tc.name, got, tc.want)
			}
		}
	})
}

// 当前环节显示文案（AC-04、AC-31）：审批等待环节按当前审批人姓名显示，其余沿用环节名。
func TestStageLabel(t *testing.T) {
	cases := []struct {
		name           string
		stage          string
		finalReviewers []string
		reviewers      []string
		want           string
	}{
		{"中间或签显示审核组", StageIntermediateReview, []string{"周宁"}, []string{"张三", "李四"}, "待张三等2人审批"},
		{"中间或签单人显示姓名", StageIntermediateReview, []string{"周宁"}, []string{"张三"}, "待张三审批"},
		{"终审显示管理员集合（裁决 11）", StageFinalReview, []string{"周宁", "张三"}, nil, "待周宁等2人审批"},
		{"无管理员名单退化为待审批", StageFinalReview, nil, nil, "待审批"},
		{"待开始执行沿用环节名", StageNotStarted, []string{"周宁"}, nil, "待开始执行"},
		{"等待输入沿用环节名", StageWaitingInput, []string{"周宁"}, nil, "等待输入"},
		{"任务执行沿用环节名", StageInProgress, []string{"周宁"}, nil, "任务执行"},
		{"已闭环沿用环节名", StageCompleted, []string{"周宁"}, nil, "已闭环"},
		{"已关闭沿用环节名", StageCancelled, []string{"周宁"}, nil, "已关闭"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := StageLabel(tc.stage, tc.finalReviewers, tc.reviewers); got != tc.want {
				t.Fatalf("StageLabel(%q) = %q, want %q", tc.stage, got, tc.want)
			}
		})
	}
}

// F1：四组枚举文案统一由 domain 派生，未知取值退化为安全默认，不回显枚举原文。
func TestEnumLabels(t *testing.T) {
	cases := []struct {
		got  string
		want string
	}{
		{RiskLevelLabel(RiskNormal), "正常"},
		{RiskLevelLabel(RiskWarning), "预警"},
		{RiskLevelLabel(RiskHighRisk), "高风险"},
		{RiskLevelLabel("unknown"), "正常"},
		{NecessityLabel(NecessityRequired), "必要"},
		{NecessityLabel(NecessityReference), "参考"},
		{NecessityLabel("unknown"), "协作关系"},
		{ProjectStatusLabel("in_progress"), "进行中"},
		{ProjectStatusLabel("archived"), "已归档"},
		{ProjectStatusLabel("unknown"), "未开始"},
		{MemberRoleLabel(RoleAdmin), "项目管理员"},
		{MemberRoleLabel(RoleViewer), "访客"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Fatalf("label = %q, want %q", tc.got, tc.want)
		}
	}
}
