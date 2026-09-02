package domain

import (
	"errors"
	"testing"
)

// AC-28／§4.4（#173 裁决：关系类型删除）：输入要求校验——必要性合法、
// 不能以自身为来源（循环经多任务表达）。
func TestValidateNewEdge(t *testing.T) {
	cases := []struct {
		name string
		e    NewEdge
		want error
	}{
		{"合法必要输入", NewEdge{Necessity: NecessityRequired, SourceTaskID: i64(2), TargetTaskID: 1}, nil},
		{"合法参考输入", NewEdge{Necessity: NecessityReference, SourceTaskID: i64(2), TargetTaskID: 1}, nil},
		{"必要性非法", NewEdge{Necessity: "optional", SourceTaskID: i64(2), TargetTaskID: 1}, ErrNecessityInvalid},
		{"自环禁止", NewEdge{Necessity: NecessityRequired, SourceTaskID: i64(1), TargetTaskID: 1}, ErrEdgeSelfLoop},
		{"无来源禁止", NewEdge{Necessity: NecessityRequired, TargetTaskID: 1}, ErrEdgeSourceMissing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidateNewEdge(tc.e); !errors.Is(got, tc.want) {
				t.Fatalf("ValidateNewEdge() = %v, want %v", got, tc.want)
			}
		})
	}
}

// 配置输入权限（裁决 10，#180）：仅项目管理员（含项目负责人），终态不可。
func TestCanConfigureInputs(t *testing.T) {
	facts := TaskFacts{Status: TaskInProgress, CreatorID: 3, OwnerID: 5}
	if CanConfigureInputs(Actor{Role: RoleMember}, 5, facts) {
		t.Fatal("负责人不再可配置输入（裁决 10）")
	}
	if !CanConfigureInputs(Actor{Role: RoleAdmin}, 9, facts) {
		t.Fatal("管理员应可配置输入")
	}
	if CanConfigureInputs(Actor{Role: RoleMember}, 9, facts) {
		t.Fatal("无关成员不应可配置")
	}
	if CanConfigureInputs(Actor{Role: RoleAdmin}, 9, TaskFacts{Status: TaskCompleted, OwnerID: 5}) {
		t.Fatal("已完成任务不应可配置")
	}
}

// 裁决 #163（AC-48 修订）：任务来源边的就绪只看「来源任务已完成」——完成必然产生
// 已终审生效的当前成果，语义等价；未完成时无论是否已上传文件一律未就绪。
func TestEdgeReady(t *testing.T) {
	cases := []struct {
		name         string
		sourceStatus string
		want         bool
	}{
		{"来源已完成即就绪", TaskCompleted, true},
		{"进行中未就绪", TaskInProgress, false},
		{"未开始未就绪", TaskNotStarted, false},
		{"终审中未就绪", TaskInReview, false},
		{"已关闭未就绪", TaskCancelled, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EdgeReady(tc.sourceStatus); got != tc.want {
				t.Fatalf("EdgeReady(%q) = %v, want %v", tc.sourceStatus, got, tc.want)
			}
		})
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
		{"待终审不改写", TaskInReview, true, TaskInReview},
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

// AC-53：一次配置可多选来源任务——至少一个、不可重复、逐条沿用单边校验（裁决 #163：不再挂交付物项）。
func TestValidateNewTaskInputs(t *testing.T) {
	base := NewTaskInputs{Necessity: NecessityRequired, SourceTaskIDs: []int64{2, 3, 4}, TargetTaskID: 1}
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
		{"必要性非法", func(in *NewTaskInputs) { in.Necessity = "optional" }, ErrNecessityInvalid},
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
