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
// 内部状态名不变，仅显示层转换。
func TestStatusLabel(t *testing.T) {
	cases := []struct {
		name      string
		status    string
		krOwner   string
		reviewers []string
		want      string
	}{
		{"草稿", TaskDraft, "周宁", nil, "草稿"},
		{"待入池审批显示 KR 负责人", TaskPendingPoolReview, "周宁", nil, "待周宁审批"},
		{"未开始", TaskNotStarted, "周宁", nil, "未开始"},
		{"等待输入", TaskWaitingInput, "周宁", nil, "等待输入"},
		{"进行中", TaskInProgress, "周宁", nil, "进行中"},
		{"中间或签单人", TaskPendingIntermediateReview, "周宁", []string{"张三"}, "待张三审批"},
		{"中间或签多人", TaskPendingIntermediateReview, "周宁", []string{"张三", "李四"}, "待张三等2人审批"},
		{"终审显示 KR 负责人", TaskPendingFinalReview, "周宁", nil, "待周宁审批"},
		{"KR 无负责人时退化", TaskPendingFinalReview, "", nil, "待审批"},
		{"已完成", TaskCompleted, "周宁", nil, "已完成"},
		{"已关闭", TaskCancelled, "周宁", nil, "已关闭"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := StatusLabel(tc.status, tc.krOwner, tc.reviewers); got != tc.want {
				t.Fatalf("StatusLabel(%q) = %q, want %q", tc.status, got, tc.want)
			}
		})
	}
}

// 三道审批单状态显示文案（AC-04）：等待状态按当前审批人姓名显示。
func TestReviewStateLabels(t *testing.T) {
	t.Run("入池审批单", func(t *testing.T) {
		cases := []struct {
			name    string
			state   string
			exempt  bool
			krOwner string
			want    string
		}{
			{"待审批显示 KR 负责人", PoolReviewPending, false, "周宁", "待周宁审批"},
			{"免审", PoolReviewApproved, true, "周宁", "免审通过"},
			{"已通过", PoolReviewApproved, false, "周宁", "已通过"},
			{"已退回", PoolReviewRejected, false, "周宁", "已退回"},
		}
		for _, tc := range cases {
			if got := PoolReviewStateLabel(tc.state, tc.exempt, tc.krOwner); got != tc.want {
				t.Fatalf("%s: PoolReviewStateLabel = %q, want %q", tc.name, got, tc.want)
			}
		}
	})
	t.Run("关键字段变更单", func(t *testing.T) {
		cases := []struct {
			name    string
			state   string
			exempt  bool
			krOwner string
			want    string
		}{
			{"待审批显示 KR 负责人", FieldChangePendingState, false, "周宁", "待周宁审批"},
			{"免审即时生效", FieldChangeApprovedState, true, "周宁", "免审生效"},
			{"已通过", FieldChangeApprovedState, false, "周宁", "已通过"},
			{"已退回", FieldChangeRejectedState, false, "周宁", "已退回"},
		}
		for _, tc := range cases {
			if got := FieldChangeStateLabel(tc.state, tc.exempt, tc.krOwner); got != tc.want {
				t.Fatalf("%s: FieldChangeStateLabel = %q, want %q", tc.name, got, tc.want)
			}
		}
	})
	t.Run("完成申请", func(t *testing.T) {
		cases := []struct {
			name      string
			state     string
			krOwner   string
			reviewers []string
			want      string
		}{
			{"中间或签显示审核人", CompletionIntermediate, "周宁", []string{"张三", "李四"}, "待张三等2人审批"},
			{"待终审显示 KR 负责人", CompletionPendingFinal, "周宁", nil, "待周宁审批"},
			{"已通过", CompletionApproved, "周宁", nil, "已通过"},
			{"已退回", CompletionRejected, "周宁", nil, "已退回"},
		}
		for _, tc := range cases {
			if got := CompletionStateLabel(tc.state, tc.krOwner, tc.reviewers); got != tc.want {
				t.Fatalf("%s: CompletionStateLabel = %q, want %q", tc.name, got, tc.want)
			}
		}
	})
}

// 当前环节显示文案（AC-04、AC-31）：审批等待环节按当前审批人姓名显示，其余沿用环节名。
func TestStageLabel(t *testing.T) {
	cases := []struct {
		name      string
		stage     string
		krOwner   string
		reviewers []string
		want      string
	}{
		{"入池审批显示 KR 负责人", StagePoolReview, "周宁", nil, "待周宁审批"},
		{"中间或签显示审核组", StageIntermediateReview, "周宁", []string{"张三", "李四"}, "待张三等2人审批"},
		{"中间或签单人显示姓名", StageIntermediateReview, "周宁", []string{"张三"}, "待张三审批"},
		{"KR 终审显示 KR 负责人", StageFinalReview, "周宁", nil, "待周宁审批"},
		{"KR 无负责人退化为待审批", StageFinalReview, "", nil, "待审批"},
		{"草稿完善沿用环节名", StageDraft, "周宁", nil, "草稿完善"},
		{"待开始执行沿用环节名", StageNotStarted, "周宁", nil, "待开始执行"},
		{"等待输入沿用环节名", StageWaitingInput, "周宁", nil, "等待输入"},
		{"任务执行沿用环节名", StageInProgress, "周宁", nil, "任务执行"},
		{"已闭环沿用环节名", StageCompleted, "周宁", nil, "已闭环"},
		{"已关闭沿用环节名", StageCancelled, "周宁", nil, "已关闭"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := StageLabel(tc.stage, tc.krOwner, tc.reviewers); got != tc.want {
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
		{EdgeTypeLabel(EdgeHardPrerequisite), "硬前置交付"},
		{EdgeTypeLabel(EdgeFeedback), "迭代／反馈"},
		{EdgeTypeLabel("unknown"), "协作关系"},
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
