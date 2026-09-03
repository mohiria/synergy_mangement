package domain

import "testing"

// 审批等待显示文案（AC-04、决策 34；#186）：面向用户统一显示“待{当前审批人姓名}审批”，
// 或签多人为“待{首位姓名}等N人审批”，无审批人时退化为“待审批”；
// 查看者本人即审批人时改显“待我审批”（多人含本人为“待我等N人审批”）。
func TestApprovalWaitingLabel(t *testing.T) {
	cases := []struct {
		name      string
		viewerID  int64
		approvers []Approver
		want      string
	}{
		{"单人显示姓名", 9, []Approver{{ID: 1, Name: "周宁"}}, "待周宁审批"},
		{"单人是本人显示待我审批", 1, []Approver{{ID: 1, Name: "周宁"}}, "待我审批"},
		{"或签多人显示首位加人数", 9, []Approver{{ID: 1, Name: "张三"}, {ID: 2, Name: "李四"}, {ID: 3, Name: "王五"}}, "待张三等3人审批"},
		{"两人也按首位加人数", 9, []Approver{{ID: 1, Name: "张三"}, {ID: 2, Name: "李四"}}, "待张三等2人审批"},
		{"多人含本人显示待我等N人审批", 2, []Approver{{ID: 1, Name: "张三"}, {ID: 2, Name: "李四"}}, "待我等2人审批"},
		{"多人首位是本人也显示待我等N人审批", 1, []Approver{{ID: 1, Name: "张三"}, {ID: 2, Name: "李四"}}, "待我等2人审批"},
		{"无审批人退化为待审批", 1, nil, "待审批"},
		{"空姓名退化为待审批", 9, []Approver{{ID: 1, Name: ""}}, "待审批"},
		{"空姓名但是本人仍显示待我审批", 1, []Approver{{ID: 1, Name: ""}}, "待我审批"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ApprovalWaitingLabel(tc.viewerID, tc.approvers); got != tc.want {
				t.Fatalf("ApprovalWaitingLabel(%d, %v) = %q, want %q", tc.viewerID, tc.approvers, got, tc.want)
			}
		})
	}
}

// ZipApprovers 把并行的 ID／姓名切片拼成审批人列表（handler 与 mywork 事实结构沿用并行切片）。
func TestZipApprovers(t *testing.T) {
	got := ZipApprovers([]int64{1, 2}, []string{"张三", "李四"})
	want := []Approver{{ID: 1, Name: "张三"}, {ID: 2, Name: "李四"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("ZipApprovers = %v, want %v", got, want)
	}
	// 长度不齐时缺的 ID 补零值（不影响姓名显示，零值不会命中任何查看者）。
	short := ZipApprovers([]int64{1}, []string{"张三", "李四"})
	if len(short) != 2 || short[1].ID != 0 || short[1].Name != "李四" {
		t.Fatalf("ZipApprovers(短ID) = %v", short)
	}
}

// 任务主状态显示文案（AC-04）：审核中按申请单环节取当前审批人姓名（裁决 13，#182），
// 其余为固定中文标签；裁决 11：终审人为项目管理员集合，多人为“待{首位姓名}等N人审批”。
func TestStatusLabel(t *testing.T) {
	final := []Approver{{ID: 1, Name: "周宁"}}
	finalTwo := []Approver{{ID: 1, Name: "周宁"}, {ID: 2, Name: "张三"}}
	cases := []struct {
		name           string
		status         string
		reviewStage    string
		viewerID       int64
		finalReviewers []Approver
		reviewers      []Approver
		want           string
	}{
		{"未开始", TaskNotStarted, "", 9, final, nil, "未开始"},
		{"等待输入", TaskWaitingInput, "", 9, final, nil, "等待输入"},
		{"进行中", TaskInProgress, "", 9, final, nil, "进行中"},
		{"审核中·或签环节单人", TaskInReview, CompletionIntermediate, 9, final, []Approver{{ID: 3, Name: "张三"}}, "待张三审批"},
		{"审核中·或签环节多人", TaskInReview, CompletionIntermediate, 9, final, []Approver{{ID: 3, Name: "张三"}, {ID: 4, Name: "李四"}}, "待张三等2人审批"},
		{"审核中·或签环节本人在审核组", TaskInReview, CompletionIntermediate, 3, final, []Approver{{ID: 3, Name: "张三"}}, "待我审批"},
		{"审核中·终审环节单管理员", TaskInReview, CompletionPendingFinal, 9, final, nil, "待周宁审批"},
		{"审核中·终审环节多管理员", TaskInReview, CompletionPendingFinal, 9, finalTwo, nil, "待周宁等2人审批"},
		{"审核中·终审环节本人是管理员之一", TaskInReview, CompletionPendingFinal, 2, finalTwo, nil, "待我等2人审批"},
		{"无管理员名单退化", TaskInReview, CompletionPendingFinal, 9, nil, nil, "待审批"},
		{"已完成", TaskCompleted, "", 9, final, nil, "已完成"},
		{"已关闭", TaskCancelled, "", 9, final, nil, "已关闭"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := StatusLabel(tc.status, tc.reviewStage, tc.viewerID, tc.finalReviewers, tc.reviewers); got != tc.want {
				t.Fatalf("StatusLabel(%q) = %q, want %q", tc.status, got, tc.want)
			}
		})
	}
}

// 审批单状态显示文案（AC-04）：等待状态按当前审批人姓名显示（裁决 10 后只剩完成申请一类）。
func TestReviewStateLabels(t *testing.T) {
	t.Run("完成申请", func(t *testing.T) {
		final := []Approver{{ID: 1, Name: "周宁"}}
		finalTwo := []Approver{{ID: 1, Name: "周宁"}, {ID: 2, Name: "张三"}}
		cases := []struct {
			name           string
			state          string
			viewerID       int64
			finalReviewers []Approver
			reviewers      []Approver
			want           string
		}{
			{"中间或签显示审核人", CompletionIntermediate, 9, final, []Approver{{ID: 3, Name: "张三"}, {ID: 4, Name: "李四"}}, "待张三等2人审批"},
			{"中间或签本人在审核组显示待我等N人审批", CompletionIntermediate, 4, final, []Approver{{ID: 3, Name: "张三"}, {ID: 4, Name: "李四"}}, "待我等2人审批"},
			{"待终审显示管理员集合（裁决 11）", CompletionPendingFinal, 9, finalTwo, nil, "待周宁等2人审批"},
			{"待终审本人是管理员显示待我等N人审批", CompletionPendingFinal, 1, finalTwo, nil, "待我等2人审批"},
			{"已通过", CompletionApproved, 9, final, nil, "已通过"},
			{"已退回", CompletionRejected, 9, final, nil, "已退回"},
		}
		for _, tc := range cases {
			if got := CompletionStateLabel(tc.state, tc.viewerID, tc.finalReviewers, tc.reviewers); got != tc.want {
				t.Fatalf("%s: CompletionStateLabel = %q, want %q", tc.name, got, tc.want)
			}
		}
	})
}

// 当前环节显示文案（AC-04、AC-31）：审批等待环节按当前审批人姓名显示，其余沿用环节名。
func TestStageLabel(t *testing.T) {
	final := []Approver{{ID: 1, Name: "周宁"}}
	finalTwo := []Approver{{ID: 1, Name: "周宁"}, {ID: 2, Name: "张三"}}
	cases := []struct {
		name           string
		stage          string
		viewerID       int64
		finalReviewers []Approver
		reviewers      []Approver
		want           string
	}{
		{"中间或签显示审核组", StageIntermediateReview, 9, final, []Approver{{ID: 3, Name: "张三"}, {ID: 4, Name: "李四"}}, "待张三等2人审批"},
		{"中间或签单人显示姓名", StageIntermediateReview, 9, final, []Approver{{ID: 3, Name: "张三"}}, "待张三审批"},
		{"中间或签单人是本人显示待我审批", StageIntermediateReview, 3, final, []Approver{{ID: 3, Name: "张三"}}, "待我审批"},
		{"终审显示管理员集合（裁决 11）", StageFinalReview, 9, finalTwo, nil, "待周宁等2人审批"},
		{"终审本人是管理员显示待我等N人审批", StageFinalReview, 2, finalTwo, nil, "待我等2人审批"},
		{"无管理员名单退化为待审批", StageFinalReview, 9, nil, nil, "待审批"},
		{"待开始执行沿用环节名", StageNotStarted, 9, final, nil, "待开始执行"},
		{"等待输入沿用环节名", StageWaitingInput, 9, final, nil, "等待输入"},
		{"任务执行沿用环节名", StageInProgress, 9, final, nil, "任务执行"},
		{"已闭环沿用环节名", StageCompleted, 9, final, nil, "已闭环"},
		{"已关闭沿用环节名", StageCancelled, 9, final, nil, "已关闭"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := StageLabel(tc.stage, tc.viewerID, tc.finalReviewers, tc.reviewers); got != tc.want {
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
