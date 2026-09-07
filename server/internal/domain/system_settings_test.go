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

// #219：SystemSettings.canEdit 只对系统管理员为 true，前端据此决定字段是否可点击编辑。
func TestCanEditSystemSettings(t *testing.T) {
	cases := []struct {
		name          string
		isSystemAdmin bool
		want          bool
	}{
		{"系统管理员可编辑", true, true},
		{"普通用户只读", false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CanEditSystemSettings(c.isSystemAdmin); got != c.want {
				t.Fatalf("CanEditSystemSettings(%v) = %v, want %v", c.isSystemAdmin, got, c.want)
			}
		})
	}
}
