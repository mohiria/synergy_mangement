package domain

import (
	"errors"
	"testing"
)

// 交付物项名称校验（PRD §9.1 预期交付物为选填；建立时非空）。
func TestValidateDeliverableName(t *testing.T) {
	if err := ValidateDeliverableName("任务成果记录"); err != nil {
		t.Fatalf("合法名称被拒: %v", err)
	}
	if err := ValidateDeliverableName("   "); !errors.Is(err, ErrDeliverableNameEmpty) {
		t.Fatalf("空名称应被拒: %v", err)
	}
	if err := ValidateDeliverableName(repeat("长", 101)); !errors.Is(err, ErrDeliverableNameTooLong) {
		t.Fatalf("超长名称应被拒: %v", err)
	}
}

// 配置输出：任务负责人／创建人／可编辑项目者；终态任务不可再配置。
func TestCanManageDeliverables(t *testing.T) {
	facts := TaskFacts{Status: TaskInProgress, CreatorID: 3, OwnerID: 5}
	cases := []struct {
		name  string
		actor Actor
		user  int64
		t     TaskFacts
		want  bool
	}{
		{"负责人可配置", Actor{Role: RoleMember}, 5, facts, true},
		{"创建人可配置", Actor{Role: RoleMember}, 3, facts, true},
		{"管理员可配置", Actor{Role: RoleAdmin}, 9, facts, true},
		{"无关成员不可配置", Actor{Role: RoleMember}, 9, facts, false},
		{"草稿可配置", Actor{Role: RoleMember}, 5, TaskFacts{Status: TaskDraft, OwnerID: 5}, true},
		{"已完成不可配置", Actor{Role: RoleAdmin}, 9, TaskFacts{Status: TaskCompleted, OwnerID: 5}, false},
		{"已取消不可配置", Actor{Role: RoleMember}, 5, TaskFacts{Status: TaskCancelled, OwnerID: 5}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanManageDeliverables(tc.actor, tc.user, tc.t); got != tc.want {
				t.Fatalf("CanManageDeliverables() = %v, want %v", got, tc.want)
			}
		})
	}
}

// 候选内容登记：任务负责人（管理员纠错），执行类状态；审核期间与终态不可另传。
func TestCanUploadCandidate(t *testing.T) {
	cases := []struct {
		name  string
		actor Actor
		user  int64
		t     TaskFacts
		want  bool
	}{
		{"负责人进行中可传", Actor{Role: RoleMember}, 5, TaskFacts{Status: TaskInProgress, OwnerID: 5}, true},
		{"负责人未开始可先备", Actor{Role: RoleMember}, 5, TaskFacts{Status: TaskNotStarted, OwnerID: 5}, true},
		{"等待输入可传", Actor{Role: RoleMember}, 5, TaskFacts{Status: TaskWaitingInput, OwnerID: 5}, true},
		{"管理员可传", Actor{Role: RoleAdmin}, 9, TaskFacts{Status: TaskInProgress, OwnerID: 5}, true},
		{"创建人非负责人不可传", Actor{Role: RoleMember}, 3, TaskFacts{Status: TaskInProgress, OwnerID: 5, CreatorID: 3}, false},
		{"草稿不可传", Actor{Role: RoleMember}, 5, TaskFacts{Status: TaskDraft, OwnerID: 5}, false},
		{"完成审核中不可另传", Actor{Role: RoleMember}, 5, TaskFacts{Status: TaskPendingFinalReview, OwnerID: 5}, false},
		{"已完成不可传", Actor{Role: RoleMember}, 5, TaskFacts{Status: TaskCompleted, OwnerID: 5}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanUploadCandidate(tc.actor, tc.user, tc.t); got != tc.want {
				t.Fatalf("CanUploadCandidate() = %v, want %v", got, tc.want)
			}
		})
	}
}

// 文件名校验。
func TestValidateCandidateFileName(t *testing.T) {
	if err := ValidateCandidateFileName("现场验收记录.xlsx"); err != nil {
		t.Fatalf("合法文件名被拒: %v", err)
	}
	if err := ValidateCandidateFileName(" "); !errors.Is(err, ErrFileNameEmpty) {
		t.Fatalf("空文件名应被拒: %v", err)
	}
	if err := ValidateCandidateFileName(repeat("长", 201)); !errors.Is(err, ErrFileNameTooLong) {
		t.Fatalf("超长文件名应被拒: %v", err)
	}
}
