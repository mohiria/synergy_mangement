package domain

import (
	"errors"
	"testing"
)

// AC-18：成果包——名称必填、勾选项非空且须有已生效当前内容；仅管理员/项目负责人可创建。
func TestValidatePackage(t *testing.T) {
	hasCurrent := map[int64]bool{1: true, 2: true, 3: false}
	check := func(id int64) bool { return hasCurrent[id] }
	if err := ValidatePackage("联调阶段成果", []int64{1, 2}, check); err != nil {
		t.Fatalf("合法成果包被拒: %v", err)
	}
	if err := ValidatePackage(" ", []int64{1}, check); !errors.Is(err, ErrPackageNameEmpty) {
		t.Fatalf("空名称应被拒: %v", err)
	}
	if err := ValidatePackage("x", nil, check); !errors.Is(err, ErrPackageEmpty) {
		t.Fatalf("空目录应被拒: %v", err)
	}
	if err := ValidatePackage("x", []int64{3}, check); !errors.Is(err, ErrPackageItemNoCurrent) {
		t.Fatalf("无当前内容的项应被拒: %v", err)
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
