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

// F-10、AC-18、§7.7：成果包目录项的归一口径——交付物项解析到当前内容，任务文件项就是自己；
// 任务文件被删除后条目不消失，按快照保留名称与所属任务、标注「来源文件已删除」，且不进包内。
func TestResolvePackageItem(t *testing.T) {
	at := time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC)
	id := func(v int64) *int64 { return &v }

	cases := []struct {
		name          string
		facts         PackageItemFacts
		wantName      string
		wantTaskName  string
		wantKind      string
		wantFileID    *int64
		wantFileName  string
		wantDeleted   bool
		wantManifest  string
	}{
		{
			name: "交付物项解析到已生效当前内容",
			facts: PackageItemFacts{
				DeliverableID: id(7), DeliverableName: "接口联调报告", DeliverableTaskName: "完成资金模块驱动切换",
				CurrentFileID: id(70), CurrentFileName: "报告v3.pdf", CurrentObjectKey: "obj/70", EffectiveAt: &at,
			},
			wantName: "接口联调报告", wantTaskName: "完成资金模块驱动切换",
			wantFileID: id(70), wantFileName: "报告v3.pdf",
			wantManifest: "完成资金模块驱动切换 / 接口联调报告 → 报告v3.pdf",
		},
		{
			name: "交付物项没有当前内容",
			facts: PackageItemFacts{
				DeliverableID: id(8), DeliverableName: "验收用例", DeliverableTaskName: "补齐适配后的回归用例",
			},
			wantName: "验收用例", wantTaskName: "补齐适配后的回归用例",
			wantManifest: "补齐适配后的回归用例 / 验收用例 →（暂无已生效当前内容）",
		},
		{
			name: "任务文件项来源仍在",
			facts: PackageItemFacts{
				TaskFileID: id(12), TaskFileName: "过程记录.txt", TaskFileKind: TaskFileProcess,
				TaskFileObjectKey: "obj/12", TaskFileTaskName: "梳理数据库权限",
				SourceTaskName: "梳理数据库权限", SourceFileName: "过程记录.txt", SourceFileKind: TaskFileProcess,
			},
			wantName: "过程记录.txt", wantTaskName: "梳理数据库权限", wantKind: TaskFileProcess,
			wantFileID: id(12), wantFileName: "过程记录.txt",
			wantManifest: "梳理数据库权限 / 过程记录.txt（过程文件） → 过程记录.txt",
		},
		{
			name: "任务文件被删除后条目按快照保留",
			facts: PackageItemFacts{
				SourceTaskName: "梳理数据库权限", SourceFileName: "过程记录.txt", SourceFileKind: TaskFileProcess,
			},
			wantName: "过程记录.txt", wantTaskName: "梳理数据库权限", wantKind: TaskFileProcess,
			wantDeleted:  true,
			wantManifest: "梳理数据库权限 / 过程记录.txt（过程文件） →（来源文件已删除）",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolvePackageItem(c.facts)
			if got.Name != c.wantName || got.TaskName != c.wantTaskName {
				t.Errorf("名称／任务 = %q / %q, want %q / %q", got.Name, got.TaskName, c.wantName, c.wantTaskName)
			}
			if got.FileKind != c.wantKind {
				t.Errorf("FileKind = %q, want %q", got.FileKind, c.wantKind)
			}
			if (got.FileID == nil) != (c.wantFileID == nil) || (got.FileID != nil && *got.FileID != *c.wantFileID) {
				t.Errorf("FileID = %v, want %v", got.FileID, c.wantFileID)
			}
			if got.FileName != c.wantFileName {
				t.Errorf("FileName = %q, want %q", got.FileName, c.wantFileName)
			}
			if got.SourceDeleted != c.wantDeleted {
				t.Errorf("SourceDeleted = %v, want %v", got.SourceDeleted, c.wantDeleted)
			}
			if line := PackageManifestLine(got); line != c.wantManifest {
				t.Errorf("清单行 = %q, want %q", line, c.wantManifest)
			}
		})
	}
}

// 来源已删除的条目不进 zip：包内文件按 FileID 取，删除后 FileID 为空。
func TestResolvePackageItemDeletedSourceHasNoContent(t *testing.T) {
	got := ResolvePackageItem(PackageItemFacts{
		SourceTaskName: "梳理数据库权限", SourceFileName: "外部材料.pdf", SourceFileKind: TaskFileExternal,
	})
	if got.FileID != nil || got.ObjectKey != "" {
		t.Fatalf("来源已删除的条目不应带内容：FileID=%v ObjectKey=%q", got.FileID, got.ObjectKey)
	}
	if !got.SourceDeleted {
		t.Fatal("SourceDeleted 应为真")
	}
}
