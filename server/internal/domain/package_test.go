package domain

import (
	"errors"
	"testing"
	"time"
)

// AC-18：成果包——名称必填、勾选项非空、交付物项须有已生效当前内容；
// 过程文件与重要外部材料也可按需选进（§7.7 边界表第三列，#79）。
func TestValidatePackage(t *testing.T) {
	hasCurrent := map[int64]bool{1: true, 2: true, 3: false}
	check := func(id int64) bool { return hasCurrent[id] }
	taskFiles := map[int64]bool{11: true}
	fileCheck := func(id int64) bool { return taskFiles[id] }
	if err := ValidatePackage("联调阶段成果", []int64{1, 2}, nil, check, fileCheck); err != nil {
		t.Fatalf("合法成果包被拒: %v", err)
	}
	if err := ValidatePackage("联调过程留痕", nil, []int64{11}, check, fileCheck); err != nil {
		t.Fatalf("只勾过程文件也应成包: %v", err)
	}
	if err := ValidatePackage("混合", []int64{1}, []int64{11}, check, fileCheck); err != nil {
		t.Fatalf("当前成果与过程文件可同包: %v", err)
	}
	if err := ValidatePackage(" ", []int64{1}, nil, check, fileCheck); !errors.Is(err, ErrPackageNameEmpty) {
		t.Fatalf("空名称应被拒: %v", err)
	}
	if err := ValidatePackage("x", nil, nil, check, fileCheck); !errors.Is(err, ErrPackageEmpty) {
		t.Fatalf("空目录应被拒: %v", err)
	}
	if err := ValidatePackage("x", []int64{3}, nil, check, fileCheck); !errors.Is(err, ErrPackageItemNoCurrent) {
		t.Fatalf("无当前内容的项应被拒: %v", err)
	}
	if err := ValidatePackage("x", nil, []int64{99}, check, fileCheck); !errors.Is(err, ErrPackageFileNotFound) {
		t.Fatalf("不属于本项目的任务文件应被拒: %v", err)
	}
}

func TestCanCreatePackage(t *testing.T) {
	if !CanCreatePackage(Actor{Role: RoleAdmin}) || !CanCreatePackage(Actor{IsOwner: true}) {
		t.Fatal("管理员/项目负责人应可创建成果包")
	}
	if CanCreatePackage(Actor{Role: RoleMember}) || CanCreatePackage(Actor{Role: RoleViewer}) {
		t.Fatal("普通/只读成员只可查看下载")
	}
}

// §7.7 统一归档的「时间」筛选维（AC-17、#86）：按日期闭区间裁剪，端点当天算在内；
// 没有时间的项在给了区间后不返回——它无法证明自己落在区间里。
func TestInArchiveWindow(t *testing.T) {
	day := func(s string) time.Time {
		v, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatalf("bad date %q: %v", s, err)
		}
		return v
	}
	at := func(s string) *time.Time {
		v := day(s)
		return &v
	}
	from, to := day("2026-09-10"), day("2026-09-20")
	cases := []struct {
		name string
		at   *time.Time
		from *time.Time
		to   *time.Time
		want bool
	}{
		{"不给区间时一律通过", at("2026-01-01"), nil, nil, true},
		{"无时间且不给区间也通过", nil, nil, nil, true},
		{"区间内", at("2026-09-15"), &from, &to, true},
		{"起点当天算在内", at("2026-09-10"), &from, &to, true},
		{"终点当天算在内", at("2026-09-20"), &from, &to, true},
		{"终点当天的晚些时刻也算在内", func() *time.Time { v := day("2026-09-20").Add(23 * time.Hour); return &v }(), &from, &to, true},
		{"早于起点", at("2026-09-09"), &from, &to, false},
		{"晚于终点", at("2026-09-21"), &from, &to, false},
		{"只给起点", at("2026-12-01"), &from, nil, true},
		{"只给终点", at("2026-12-01"), nil, &to, false},
		{"无时间的项在给了区间后不返回", nil, &from, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := InArchiveWindow(c.at, c.from, c.to); got != c.want {
				t.Fatalf("InArchiveWindow = %v, want %v", got, c.want)
			}
		})
	}
}
