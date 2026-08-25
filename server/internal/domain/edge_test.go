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
		{"合法硬前置", NewEdge{Name: "现场数据包", EdgeType: EdgeHardPrerequisite, Necessity: NecessityRequired, SourceTaskID: i64(2), TargetTaskID: 1}, nil},
		{"合法参考信息输入", NewEdge{Name: "行业报告", EdgeType: EdgeInformation, Necessity: NecessityReference, SourceTaskID: i64(2), TargetTaskID: 1}, nil},
		{"名称为空", NewEdge{Name: " ", EdgeType: EdgeInformation, Necessity: NecessityRequired, SourceTaskID: i64(2), TargetTaskID: 1}, ErrEdgeNameEmpty},
		{"类型非法", NewEdge{Name: "x", EdgeType: "loop", Necessity: NecessityRequired, SourceTaskID: i64(2), TargetTaskID: 1}, ErrEdgeTypeInvalid},
		{"必要性非法", NewEdge{Name: "x", EdgeType: EdgeInformation, Necessity: "optional", SourceTaskID: i64(2), TargetTaskID: 1}, ErrNecessityInvalid},
		{"自环禁止", NewEdge{Name: "x", EdgeType: EdgeInformation, Necessity: NecessityRequired, SourceTaskID: i64(1), TargetTaskID: 1}, ErrEdgeSelfLoop},
		{"无来源禁止", NewEdge{Name: "x", EdgeType: EdgeInformation, Necessity: NecessityRequired, TargetTaskID: 1}, ErrEdgeSourceMissing},
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

// §4.4.7／§5.1：必要输入未就绪时页面显示「等待输入」；参考输入不影响；执行外状态不改写。
func TestDeriveDisplayStatus(t *testing.T) {
	cases := []struct {
		name      string
		stored    string
		hasUnmet  bool
		want      string
	}{
		{"未开始且必要输入未到", TaskNotStarted, true, TaskWaitingInput},
		{"进行中且必要输入未到", TaskInProgress, true, TaskWaitingInput},
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
