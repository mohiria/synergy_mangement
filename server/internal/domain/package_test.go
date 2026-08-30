package domain

import (
	"errors"
	"testing"
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
