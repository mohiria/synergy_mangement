package domain

import (
	"errors"
	"testing"
)

// #201：系统设置（含用户管理列表）只对系统管理员开放；项目角色与之无关。
func TestCanAccessSystemSettings(t *testing.T) {
	cases := []struct {
		name          string
		isSystemAdmin bool
		want          error
	}{
		{"系统管理员可访问", true, nil},
		{"普通用户拒绝", false, ErrSystemAdminRequired},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CanAccessSystemSettings(c.isSystemAdmin); !errors.Is(got, c.want) {
				t.Fatalf("CanAccessSystemSettings(%v) = %v, want %v", c.isSystemAdmin, got, c.want)
			}
		})
	}
}
