package domain

import (
	"errors"
	"testing"
)

func ip(v int) *int { return &v }

// AC-12：进度百分比可空，范围 0～100。
func TestValidateProgress(t *testing.T) {
	cases := []struct {
		name string
		p    *int
		want error
	}{
		{"未填写合法", nil, nil},
		{"0 合法", ip(0), nil},
		{"100 合法", ip(100), nil},
		{"负数非法", ip(-1), ErrProgressOutOfRange},
		{"超过 100 非法", ip(101), ErrProgressOutOfRange},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidateProgress(tc.p); !errors.Is(got, tc.want) {
				t.Fatalf("ValidateProgress() = %v, want %v", got, tc.want)
			}
		})
	}
}

// §5.1：开始执行——未开始／等待输入 → 进行中；其余状态不可开始。
func TestStartTask(t *testing.T) {
	cases := []struct {
		name    string
		status  string
		want    string
		wantErr error
	}{
		{"未开始可开始", TaskNotStarted, TaskInProgress, nil},
		{"等待输入可开始", TaskWaitingInput, TaskInProgress, nil},
		{"草稿不可开始", TaskDraft, "", ErrCannotStart},
		{"待入池审批不可开始", TaskPendingPoolReview, "", ErrCannotStart},
		{"进行中不可重复开始", TaskInProgress, "", ErrCannotStart},
		{"已完成不可开始", TaskCompleted, "", ErrCannotStart},
		{"已取消不可开始", TaskCancelled, "", ErrCannotStart},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := StartTask(tc.status)
			if !errors.Is(err, tc.wantErr) || got != tc.want {
				t.Fatalf("StartTask(%q) = (%q, %v), want (%q, %v)", tc.status, got, err, tc.want, tc.wantErr)
			}
		})
	}
}

// §5.1／AC-57：取消原因必填。
func TestValidateCancelReason(t *testing.T) {
	if err := ValidateCancelReason("需求变更不再执行"); err != nil {
		t.Fatalf("填了原因不应报错: %v", err)
	}
	if err := ValidateCancelReason("  "); !errors.Is(err, ErrCancelReasonRequired) {
		t.Fatalf("原因必填: %v", err)
	}
}

// §5.6：进度由任务负责人填写；管理员／项目负责人可全局纠错；仅执行类状态可改。
func TestCanUpdateProgress(t *testing.T) {
	facts := TaskFacts{Status: TaskInProgress, OwnerID: 5, CreatorID: 3}
	cases := []struct {
		name  string
		actor Actor
		user  int64
		t     TaskFacts
		want  bool
	}{
		{"负责人可更新", Actor{Role: RoleMember}, 5, facts, true},
		{"管理员可更新", Actor{Role: RoleAdmin}, 9, facts, true},
		{"其他成员不可更新", Actor{Role: RoleMember}, 3, facts, false},
		{"未开始不可更新进度", Actor{Role: RoleMember}, 5, TaskFacts{Status: TaskNotStarted, OwnerID: 5}, false},
		{"已完成不可更新进度", Actor{Role: RoleMember}, 5, TaskFacts{Status: TaskCompleted, OwnerID: 5}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanUpdateProgress(tc.actor, tc.user, tc.t); got != tc.want {
				t.Fatalf("CanUpdateProgress() = %v, want %v", got, tc.want)
			}
		})
	}
}

// AC-12：KR 覆盖度——只统计已入池且未取消的任务；平均值任务等权、只算已填任务、四舍五入；
// 系统不为未填任务虚构百分比。
func TestProgressCoverage(t *testing.T) {
	cases := []struct {
		name       string
		tasks      []TaskProgressFact
		wantTotal  int
		wantFilled int
		wantAvg    *int
	}{
		{"无任务", nil, 0, 0, nil},
		{"草稿与待审批不计入", []TaskProgressFact{
			{Status: TaskDraft}, {Status: TaskPendingPoolReview, Progress: ip(50)},
		}, 0, 0, nil},
		{"已取消不计入", []TaskProgressFact{
			{Status: TaskCancelled, Progress: ip(80)}, {Status: TaskNotStarted},
		}, 1, 0, nil},
		{"未填任务不虚构百分比", []TaskProgressFact{
			{Status: TaskInProgress}, {Status: TaskNotStarted},
		}, 2, 0, nil},
		{"等权平均且四舍五入", []TaskProgressFact{
			{Status: TaskInProgress, Progress: ip(30)},
			{Status: TaskInProgress, Progress: ip(45)},
			{Status: TaskNotStarted},
		}, 3, 2, ip(38)},
		{"完成态计入覆盖度", []TaskProgressFact{
			{Status: TaskCompleted, Progress: ip(100)},
			{Status: TaskPendingFinalReview, Progress: ip(90)},
		}, 2, 2, ip(95)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ProgressCoverage(tc.tasks)
			if got.TotalTasks != tc.wantTotal || got.FilledTasks != tc.wantFilled {
				t.Fatalf("ProgressCoverage() = %+v, want total %d filled %d", got, tc.wantTotal, tc.wantFilled)
			}
			switch {
			case tc.wantAvg == nil && got.AverageProgress != nil:
				t.Fatalf("AverageProgress = %v, want nil", *got.AverageProgress)
			case tc.wantAvg != nil && (got.AverageProgress == nil || *got.AverageProgress != *tc.wantAvg):
				t.Fatalf("AverageProgress = %v, want %d", got.AverageProgress, *tc.wantAvg)
			}
		})
	}
}

// 派生动作标志：开始（负责人/可编辑项目者）、取消（另含创建人）。
func TestCanStartAndCancelFlags(t *testing.T) {
	facts := TaskFacts{Status: TaskNotStarted, OwnerID: 5, CreatorID: 3, KrOwnerID: i64(7)}
	if !CanStartTask(Actor{Role: RoleMember}, 5, facts) {
		t.Fatal("负责人应可开始")
	}
	if CanStartTask(Actor{Role: RoleMember}, 3, facts) {
		t.Fatal("创建人非负责人不应可开始")
	}
	if !CanStartTask(Actor{Role: RoleAdmin}, 9, facts) {
		t.Fatal("管理员应可开始")
	}
	if CanStartTask(Actor{Role: RoleMember}, 5, TaskFacts{Status: TaskInProgress, OwnerID: 5}) {
		t.Fatal("进行中不应再显示开始")
	}
	if !CanCancelTask(Actor{Role: RoleMember}, 5, facts, false) {
		t.Fatal("负责人应可发起取消")
	}
	if CanCancelTask(Actor{Role: RoleMember}, 3, facts, false) {
		t.Fatal("创建人不再是取消发起人（AC-57）")
	}
	if CanCancelTask(Actor{Role: RoleMember}, 9, facts, false) {
		t.Fatal("无关成员不应可取消")
	}
	if CanCancelTask(Actor{Role: RoleMember}, 5, facts, true) {
		t.Fatal("有未决审批单时取消入口应关闭")
	}
	if CanCancelTask(Actor{Role: RoleAdmin}, 9, TaskFacts{Status: TaskCompleted, OwnerID: 5, KrOwnerID: i64(7)}, false) {
		t.Fatal("已完成不应可取消")
	}
}
