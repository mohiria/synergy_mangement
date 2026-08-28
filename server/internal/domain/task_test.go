package domain

import (
	"errors"
	"testing"
	"time"
)

// date 复用 okr_test 的 day 帮助函数取值形式，返回非指针日期。
func date(s string) time.Time { return *day(s) }

// AC-04／AC-26 前置：任务草稿最小骨架校验（PRD §9.1：名称、所属 KR、负责人、开始／截止时间）。
func TestValidateNewTask(t *testing.T) {
	members := map[int64]bool{1: true, 2: true}
	isMember := func(id int64) bool { return members[id] }
	base := NewTask{Name: "验证现场联动异常回退", OwnerID: 1, Start: date("2026-09-12"), End: date("2026-09-21")}

	cases := []struct {
		name string
		mut  func(*NewTask)
		want error
	}{
		{"合法草稿", func(*NewTask) {}, nil},
		{"名称为空", func(n *NewTask) { n.Name = "   " }, ErrTaskNameEmpty},
		{"名称超长", func(n *NewTask) { n.Name = string(make([]rune, 0)) + repeat("长", 201) }, ErrTaskNameTooLong},
		{"负责人不是项目成员", func(n *NewTask) { n.OwnerID = 99 }, ErrTaskOwnerNotMember},
		{"截止早于开始", func(n *NewTask) { n.End = date("2026-09-01") }, ErrTaskPeriodInverted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := base
			tc.mut(&n)
			if got := ValidateNewTask(n, isMember); !errors.Is(got, tc.want) {
				t.Fatalf("ValidateNewTask() = %v, want %v", got, tc.want)
			}
		})
	}
}

func repeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}

// 权限矩阵 §3.4：创建任务——管理员／负责人／普通成员可建，只读成员与非成员不可。
func TestCanCreateTask(t *testing.T) {
	cases := []struct {
		name  string
		actor Actor
		want  bool
	}{
		{"项目管理员", Actor{Role: RoleAdmin}, true},
		{"项目负责人非成员", Actor{IsOwner: true}, true},
		{"普通成员", Actor{Role: RoleMember}, true},
		{"只读成员", Actor{Role: RoleViewer}, false},
		{"非成员", Actor{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanCreateTask(tc.actor); got != tc.want {
				t.Fatalf("CanCreateTask(%+v) = %v, want %v", tc.actor, got, tc.want)
			}
		})
	}
}

// AC-26：KR 负责人在本人负责的 KR 下创建任务免审，保存后直接进入未开始；其余创建为草稿。
func TestTaskCreationOutcome(t *testing.T) {
	cases := []struct {
		name       string
		creator    int64
		krOwner    *int64
		wantStatus string
		wantExempt bool
	}{
		{"KR 负责人本人创建免审", 7, i64(7), TaskNotStarted, true},
		{"普通成员创建为草稿", 3, i64(7), TaskDraft, false},
		{"KR 未指定负责人时创建为草稿", 7, nil, TaskDraft, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, exempt := TaskCreationOutcome(tc.creator, tc.krOwner)
			if status != tc.wantStatus || exempt != tc.wantExempt {
				t.Fatalf("TaskCreationOutcome() = (%q, %v), want (%q, %v)", status, exempt, tc.wantStatus, tc.wantExempt)
			}
		})
	}
}

// AC-04：提交入池——仅草稿可提交，且所属 KR 必须已指定负责人。
func TestSubmitPoolReview(t *testing.T) {
	cases := []struct {
		name string
		t    TaskFacts
		want error
	}{
		{"草稿可提交", TaskFacts{Status: TaskDraft, KrOwnerID: i64(7)}, nil},
		{"待审批不可重复提交", TaskFacts{Status: TaskPendingPoolReview, KrOwnerID: i64(7)}, ErrTaskNotDraft},
		{"未开始不可提交", TaskFacts{Status: TaskNotStarted, KrOwnerID: i64(7)}, ErrTaskNotDraft},
		{"KR 未指定负责人不可提交", TaskFacts{Status: TaskDraft}, ErrKrOwnerMissing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SubmitPoolReview(tc.t, false); !errors.Is(got, tc.want) {
				t.Fatalf("SubmitPoolReview() = %v, want %v", got, tc.want)
			}
		})
	}
}

// 提交入池的动作人：创建人、任务负责人或可编辑项目者（§3.4「具有创建职责时」）。
func TestCanSubmitPoolReview(t *testing.T) {
	facts := TaskFacts{Status: TaskDraft, CreatorID: 3, OwnerID: 5, KrOwnerID: i64(7)}
	cases := []struct {
		name  string
		actor Actor
		user  int64
		t     TaskFacts
		want  bool
	}{
		{"创建人可提交", Actor{Role: RoleMember}, 3, facts, true},
		{"任务负责人可提交", Actor{Role: RoleMember}, 5, facts, true},
		{"项目管理员可提交", Actor{Role: RoleAdmin}, 9, facts, true},
		{"无关成员不可提交", Actor{Role: RoleMember}, 9, facts, false},
		{"非草稿不可提交", Actor{Role: RoleMember}, 3, TaskFacts{Status: TaskPendingPoolReview, CreatorID: 3, KrOwnerID: i64(7)}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanSubmitPoolReview(tc.actor, tc.user, tc.t, false); got != tc.want {
				t.Fatalf("CanSubmitPoolReview() = %v, want %v", got, tc.want)
			}
		})
	}
}

// AC-04：入池审批由所属 KR 负责人处理；通过→未开始，退回→草稿；
// §3.3：项目管理员不能因管理权限替代 KR 负责人。
func TestDecidePoolReview(t *testing.T) {
	pending := TaskFacts{Status: TaskPendingPoolReview, CreatorID: 3, OwnerID: 5, KrOwnerID: i64(7)}
	cases := []struct {
		name       string
		t          TaskFacts
		actor      int64
		approve    bool
		opinion    string
		wantStatus string
		wantErr    error
	}{
		{"KR 负责人通过后进入未开始", pending, 7, true, "", TaskNotStarted, nil},
		{"KR 负责人带理由退回后回到草稿", pending, 7, false, "范围与 KR 不匹配", TaskDraft, nil},
		{"非 KR 负责人（含管理员）不可处理", pending, 9, true, "", "", ErrNotKrOwner},
		{"任务创建人不可自审", pending, 3, true, "", "", ErrNotKrOwner},
		{"非待审批状态不可处理", TaskFacts{Status: TaskDraft, KrOwnerID: i64(7)}, 7, true, "", "", ErrPoolReviewNotPending},
		// MW-18：退回必须带理由，与完成审核同口径；否则任务回到草稿后不在任何分组里。
		{"退回不填理由被拒", pending, 7, false, "", "", ErrRejectOpinionRequired},
		{"退回理由只有空白被拒", pending, 7, false, "   ", "", ErrRejectOpinionRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, err := DecidePoolReview(tc.t, tc.actor, tc.approve, tc.opinion)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("DecidePoolReview() err = %v, want %v", err, tc.wantErr)
			}
			if status != tc.wantStatus {
				t.Fatalf("DecidePoolReview() status = %q, want %q", status, tc.wantStatus)
			}
		})
	}
}

// 派生动作标志：仅所属 KR 负责人在待审批状态下可处理。
func TestCanDecidePoolReview(t *testing.T) {
	pending := TaskFacts{Status: TaskPendingPoolReview, KrOwnerID: i64(7)}
	cases := []struct {
		name string
		user int64
		t    TaskFacts
		want bool
	}{
		{"KR 负责人待审批可处理", 7, pending, true},
		{"其他成员不可处理", 9, pending, false},
		{"草稿状态不可处理", 7, TaskFacts{Status: TaskDraft, KrOwnerID: i64(7)}, false},
		{"KR 无负责人不可处理", 7, TaskFacts{Status: TaskPendingPoolReview}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanDecidePoolReview(tc.user, tc.t); got != tc.want {
				t.Fatalf("CanDecidePoolReview() = %v, want %v", got, tc.want)
			}
		})
	}
}
