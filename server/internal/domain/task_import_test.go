package domain

import (
	"errors"
	"testing"
	"time"
)

// 任务批量导入（AC-02b、#107）：整批一次校验，任何一条不合规都不写。
// 权限沿用 CanEditProject——入口只对项目负责人与项目管理员开放。
func TestValidateTaskImport(t *testing.T) {
	roles := map[int64]string{1: RoleMember, 2: RoleAdmin, 8: RoleViewer}
	roleOf := func(id int64) string { return roles[id] }
	day := func(s string) time.Time {
		v, err := time.Parse("2006-01-02", s)
		if err != nil {
			panic(err)
		}
		return v
	}
	task := func(mut func(*ImportedTask)) ImportedTask {
		it := ImportedTask{
			Task: NewTask{Name: "盘点核心库对象", OwnerID: 1, Start: day("2026-03-09"), End: day("2026-03-26")},
		}
		if mut != nil {
			mut(&it)
		}
		return it
	}
	group := func(mut func(*TaskImportGroup)) TaskImportGroup {
		g := TaskImportGroup{KeyResultID: 7, Tasks: []ImportedTask{task(nil)}}
		if mut != nil {
			mut(&g)
		}
		return g
	}

	cases := []struct {
		name   string
		groups []TaskImportGroup
		want   error
	}{
		{"一组一条任务", []TaskImportGroup{group(nil)}, nil},
		{"多组多条任务", []TaskImportGroup{
			group(nil),
			group(func(g *TaskImportGroup) {
				g.KeyResultID = 9
				g.Tasks = []ImportedTask{task(nil), task(func(it *ImportedTask) { it.Task.OwnerID = 2 })}
			}),
		}, nil},
		{"带预期交付物", []TaskImportGroup{group(func(g *TaskImportGroup) {
			g.Tasks = []ImportedTask{task(func(it *ImportedTask) { it.ExpectedDeliverable = "不兼容对象清单" })}
		})}, nil},
		{"空批", nil, ErrTaskImportEmpty},
		{"组里没有任务", []TaskImportGroup{group(func(g *TaskImportGroup) { g.Tasks = nil })}, ErrTaskImportEmpty},
		{"没有所属 KR", []TaskImportGroup{group(func(g *TaskImportGroup) { g.KeyResultID = 0 })}, ErrTaskImportKrMissing},
		{"任务名为空", []TaskImportGroup{group(func(g *TaskImportGroup) {
			g.Tasks = []ImportedTask{task(func(it *ImportedTask) { it.Task.Name = " " })}
		})}, ErrTaskNameEmpty},
		{"负责人是访客", []TaskImportGroup{group(func(g *TaskImportGroup) {
			g.Tasks = []ImportedTask{task(func(it *ImportedTask) { it.Task.OwnerID = 8 })}
		})}, ErrTaskOwnerNotEligible},
		{"负责人非项目成员", []TaskImportGroup{group(func(g *TaskImportGroup) {
			g.Tasks = []ImportedTask{task(func(it *ImportedTask) { it.Task.OwnerID = 99 })}
		})}, ErrTaskOwnerNotEligible},
		{"截止早于开始", []TaskImportGroup{group(func(g *TaskImportGroup) {
			g.Tasks = []ImportedTask{task(func(it *ImportedTask) { it.Task.End = day("2026-03-01") })}
		})}, ErrTaskPeriodInverted},
		{"预期交付物名超长", []TaskImportGroup{group(func(g *TaskImportGroup) {
			g.Tasks = []ImportedTask{task(func(it *ImportedTask) {
				it.ExpectedDeliverable = "清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单清单一"
			})}
		})}, ErrDeliverableNameTooLong},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidateTaskImport(tc.groups, roleOf); !errors.Is(got, tc.want) {
				t.Fatalf("ValidateTaskImport = %v, want %v", got, tc.want)
			}
		})
	}
}

// 越权：任务批量导入的入口与项目结构编辑同一道边界（§3.4）。
func TestCanImportTasks(t *testing.T) {
	cases := []struct {
		name  string
		actor Actor
		want  bool
	}{
		{"项目管理员", Actor{Role: RoleAdmin}, true},
		{"项目负责人（非成员）", Actor{IsOwner: true}, true},
		{"项目成员", Actor{Role: RoleMember}, false},
		{"访客", Actor{Role: RoleViewer}, false},
		{"非成员且非负责人", Actor{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanImportTasks(tc.actor); got != tc.want {
				t.Fatalf("CanImportTasks(%+v) = %v, want %v", tc.actor, got, tc.want)
			}
		})
	}
}
