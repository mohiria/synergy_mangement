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

// 上传大小上限（R4／E2）：预签名直传绕过服务端，前端的 20MB 限制改一个 fetch 即可绕过，
// 服务端在两阶段提交的确认步骤按对象存储的真实大小兜底。
func TestValidateUploadSize(t *testing.T) {
	cases := []struct {
		name    string
		size    int64
		wantErr error
	}{
		{"正常文件", 5 << 20, nil},
		{"刚好到上限", MaxUploadSize, nil},
		{"超出上限", MaxUploadSize + 1, ErrFileTooLarge},
		{"空文件", 0, ErrFileEmpty},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidateUploadSize(tc.size); !errors.Is(got, tc.wantErr) {
				t.Fatalf("ValidateUploadSize(%d) = %v, want %v", tc.size, got, tc.wantErr)
			}
		})
	}
}

// AC-17／AC-33／AC-67：归档视角列表层必须能同时看到已生效当前内容与审核中的候选（#68），
// 且「在审」以存在未决完成申请为准，不以候选文件在不在为准（#81）——
// 传了没提交是「待提交审核」，不得声称在审。
func TestDeriveContentState(t *testing.T) {
	cases := []struct {
		name             string
		hasCurrent       bool
		hasCandidate     bool
		hasPendingReview bool
		want             string
		wantLabel        string
	}{
		{"三者皆无为未提交", false, false, false, ContentStateEmpty, "未提交"},
		{"候选已传未提交为待提交审核", false, true, false, ContentStatePendingSubmit, "待提交审核"},
		{"候选随完成申请提交后才是审核中", false, true, true, ContentStateReviewing, "审核中"},
		{"只有当前内容为已生效", true, false, false, ContentStateEffective, "已生效"},
		{"已生效之上候选未提交仍为待提交审核", true, true, false, ContentStatePendingSubmit, "待提交审核"},
		{"已生效之上候选在审为有更新审核中", true, true, true, ContentStateUpdating, "已生效 · 有更新审核中"},
		{"无候选时未决审批单不影响内容状态", true, false, true, ContentStateEffective, "已生效"},
		{"无候选无当前内容时未决审批单不影响内容状态", false, false, true, ContentStateEmpty, "未提交"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DeriveContentState(c.hasCurrent, c.hasCandidate, c.hasPendingReview)
			if got != c.want {
				t.Fatalf("DeriveContentState(%v,%v,%v) = %q, want %q", c.hasCurrent, c.hasCandidate, c.hasPendingReview, got, c.want)
			}
			if label := ContentStateLabel(got); label != c.wantLabel {
				t.Fatalf("ContentStateLabel(%q) = %q, want %q", got, label, c.wantLabel)
			}
		})
	}
}

// 未知取值不回显枚举原文，与其余显示文案同口径。
func TestContentStateLabelUnknown(t *testing.T) {
	if label := ContentStateLabel("bogus"); label != "未提交" {
		t.Fatalf("ContentStateLabel(bogus) = %q, want 未提交", label)
	}
}
