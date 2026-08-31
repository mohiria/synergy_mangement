package domain

import (
	"errors"
	"testing"
)

// AC-28／§4.4：输入要求校验——名称必填、类型与必要性合法、不能以自身为来源（循环经多任务表达）。
func TestValidateNewEdge(t *testing.T) {
	cases := []struct {
		name string
		e    NewEdge
		want error
	}{
		{"合法硬前置", NewEdge{EdgeType: EdgeHardPrerequisite, Necessity: NecessityRequired, SourceTaskID: i64(2), TargetTaskID: 1}, nil},
		{"合法参考信息输入", NewEdge{EdgeType: EdgeInformation, Necessity: NecessityReference, SourceTaskID: i64(2), TargetTaskID: 1}, nil},
		{"类型非法", NewEdge{EdgeType: "loop", Necessity: NecessityRequired, SourceTaskID: i64(2), TargetTaskID: 1}, ErrEdgeTypeInvalid},
		{"必要性非法", NewEdge{EdgeType: EdgeInformation, Necessity: "optional", SourceTaskID: i64(2), TargetTaskID: 1}, ErrNecessityInvalid},
		{"自环禁止", NewEdge{EdgeType: EdgeInformation, Necessity: NecessityRequired, SourceTaskID: i64(1), TargetTaskID: 1}, ErrEdgeSelfLoop},
		{"无来源禁止", NewEdge{EdgeType: EdgeInformation, Necessity: NecessityRequired, TargetTaskID: 1}, ErrEdgeSourceMissing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidateNewEdge(tc.e); !errors.Is(got, tc.want) {
				t.Fatalf("ValidateNewEdge() = %v, want %v", got, tc.want)
			}
		})
	}
}

// 配置输入权限：目标任务负责人／创建人／可编辑项目者（§3.4 配置输入、输出、依赖）。
func TestCanConfigureInputs(t *testing.T) {
	facts := TaskFacts{Status: TaskInProgress, CreatorID: 3, OwnerID: 5}
	if !CanConfigureInputs(Actor{Role: RoleMember}, 5, facts) {
		t.Fatal("负责人应可配置输入")
	}
	if !CanConfigureInputs(Actor{Role: RoleAdmin}, 9, facts) {
		t.Fatal("管理员应可配置输入")
	}
	if CanConfigureInputs(Actor{Role: RoleMember}, 9, facts) {
		t.Fatal("无关成员不应可配置")
	}
	if CanConfigureInputs(Actor{Role: RoleMember}, 5, TaskFacts{Status: TaskCompleted, OwnerID: 5}) {
		t.Fatal("已完成任务不应可配置")
	}
}

// AC-48：关系就绪——仅当前内容生效时就绪；候选不提前满足；有当前又有候选仍按当前就绪。
func TestEdgeReady(t *testing.T) {
	if EdgeReady(true, false) != true {
		t.Fatal("有当前内容应就绪")
	}
	if EdgeReady(false, true) != false {
		t.Fatal("仅候选不应就绪")
	}
	if EdgeReady(true, true) != true {
		t.Fatal("当前+候选更新应继续就绪")
	}
	if EdgeReady(false, false) != false {
		t.Fatal("无内容不应就绪")
	}
}

// §4.4.7／§5.1、AC-58：「等待输入」只在未开始阶段叠加，任务一进入进行中即消失；
// 参考输入不影响；执行外状态不改写。
func TestDeriveDisplayStatus(t *testing.T) {
	cases := []struct {
		name     string
		stored   string
		hasUnmet bool
		want     string
	}{
		{"未开始且必要输入未到", TaskNotStarted, true, TaskWaitingInput},
		{"进行中不再叠加等待输入", TaskInProgress, true, TaskInProgress},
		{"进行中输入已就绪", TaskInProgress, false, TaskInProgress},
		{"草稿不改写", TaskDraft, true, TaskDraft},
		{"待终审不改写", TaskPendingFinalReview, true, TaskPendingFinalReview},
		{"已完成不改写", TaskCompleted, true, TaskCompleted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DeriveDisplayStatus(tc.stored, tc.hasUnmet); got != tc.want {
				t.Fatalf("DeriveDisplayStatus(%q, %v) = %q, want %q", tc.stored, tc.hasUnmet, got, tc.want)
			}
		})
	}
}

// AC-53：一次配置可多选来源任务——至少一个、不可重复、逐条沿用单边校验；指定交付物项时只能单选。
func TestValidateNewTaskInputs(t *testing.T) {
	base := NewTaskInputs{EdgeType: EdgeHardPrerequisite, Necessity: NecessityRequired, SourceTaskIDs: []int64{2, 3, 4}, TargetTaskID: 1}
	cases := []struct {
		name string
		mut  func(*NewTaskInputs)
		want error
	}{
		{"多来源合法", func(*NewTaskInputs) {}, nil},
		{"单来源合法", func(in *NewTaskInputs) { in.SourceTaskIDs = []int64{2} }, nil},
		{"未选来源", func(in *NewTaskInputs) { in.SourceTaskIDs = nil }, ErrEdgeSourceMissing},
		{"来源重复", func(in *NewTaskInputs) { in.SourceTaskIDs = []int64{2, 3, 2} }, ErrEdgeSourceDuplicated},
		{"含自身", func(in *NewTaskInputs) { in.SourceTaskIDs = []int64{2, 1} }, ErrEdgeSelfLoop},
		{"类型非法", func(in *NewTaskInputs) { in.EdgeType = "loop" }, ErrEdgeTypeInvalid},
		{"必要性非法", func(in *NewTaskInputs) { in.Necessity = "optional" }, ErrNecessityInvalid},
		{"单来源可指定交付物项", func(in *NewTaskInputs) { in.SourceTaskIDs = []int64{2}; in.HasDeliverable = true }, nil},
		{"多来源不可指定交付物项", func(in *NewTaskInputs) { in.HasDeliverable = true }, ErrDeliverableMultiSource},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := base
			tc.mut(&in)
			if got := ValidateNewTaskInputs(in); !errors.Is(got, tc.want) {
				t.Fatalf("ValidateNewTaskInputs() = %v, want %v", got, tc.want)
			}
		})
	}
}

// AC-53：多来源各边独立参与就绪判定——任一必要输入未就绪即整体等待输入；参考输入不参与。
func TestFirstUnmetRequiredInput(t *testing.T) {
	cases := []struct {
		name   string
		inputs []InputEdgeState
		want   string
	}{
		{"无输入", nil, ""},
		{"多来源全部就绪", []InputEdgeState{
			{Name: "A", Necessity: NecessityRequired, Ready: true},
			{Name: "B", Necessity: NecessityRequired, Ready: true},
		}, ""},
		{"一条未就绪即等待", []InputEdgeState{
			{Name: "A", Necessity: NecessityRequired, Ready: true},
			{Name: "B", Necessity: NecessityRequired, Ready: false},
		}, "B"},
		{"取首条未就绪", []InputEdgeState{
			{Name: "A", Necessity: NecessityRequired, Ready: false},
			{Name: "B", Necessity: NecessityRequired, Ready: false},
		}, "A"},
		{"参考输入不参与", []InputEdgeState{
			{Name: "A", Necessity: NecessityReference, Ready: false},
			{Name: "B", Necessity: NecessityRequired, Ready: true},
		}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FirstUnmetRequiredInput(tc.inputs); got != tc.want {
				t.Fatalf("FirstUnmetRequiredInput() = %q, want %q", got, tc.want)
			}
		})
	}
}
