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
	roles := map[int64]string{1: RoleMember, 2: RoleAdmin, 8: RoleViewer}
	roleOf := func(id int64) string { return roles[id] }
	base := NewTask{Name: "验证现场联动异常回退", OwnerID: 1, Start: date("2026-09-12"), End: date("2026-09-21")}

	cases := []struct {
		name string
		mut  func(*NewTask)
		want error
	}{
		{"合法草稿", func(*NewTask) {}, nil},
		{"名称为空", func(n *NewTask) { n.Name = "   " }, ErrTaskNameEmpty},
		{"名称超长", func(n *NewTask) { n.Name = string(make([]rune, 0)) + repeat("长", 201) }, ErrTaskNameTooLong},
		{"负责人不是项目成员", func(n *NewTask) { n.OwnerID = 99 }, ErrTaskOwnerNotEligible},
		{"负责人是访客", func(n *NewTask) { n.OwnerID = 8 }, ErrTaskOwnerNotEligible},
		{"截止早于开始", func(n *NewTask) { n.End = date("2026-09-01") }, ErrTaskPeriodInverted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := base
			tc.mut(&n)
			if got := ValidateNewTask(n, roleOf); !errors.Is(got, tc.want) {
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

// 权限矩阵 §3.4：创建任务——管理员／负责人／项目成员可建，访客与非成员不可。
func TestCanCreateTask(t *testing.T) {
	cases := []struct {
		name  string
		actor Actor
		want  bool
	}{
		{"项目管理员", Actor{Role: RoleAdmin}, true},
		{"项目负责人非成员", Actor{IsOwner: true}, true},
		{"项目成员", Actor{Role: RoleMember}, true},
		{"访客", Actor{Role: RoleViewer}, false},
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

