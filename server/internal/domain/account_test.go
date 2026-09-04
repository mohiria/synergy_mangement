package domain

import (
	"errors"
	"strings"
	"testing"
)

// #203：管理员建号的字段规则——用户名小写字母／数字／._-，3～32；显示名非空 ≤50。
func TestValidateUsername(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want error
	}{
		{"合法", "alice", nil},
		{"合法带点下划线连字符与数字", "li.si_01-x", nil},
		{"恰好 3 位", "abc", nil},
		{"恰好 32 位", strings.Repeat("a", 32), nil},
		{"空", "", ErrUsernameEmpty},
		{"仅空白", "  ", ErrUsernameEmpty},
		{"2 位过短", "ab", ErrUsernameInvalid},
		{"33 位过长", strings.Repeat("a", 33), ErrUsernameInvalid},
		{"大写字母", "Alice", ErrUsernameInvalid},
		{"含空格", "ali ce", ErrUsernameInvalid},
		{"含中文", "张三", ErrUsernameInvalid},
		{"含 @", "a@b", ErrUsernameInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ValidateUsername(c.in); !errors.Is(got, c.want) {
				t.Fatalf("ValidateUsername(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestValidateDisplayName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want error
	}{
		{"合法", "张三", nil},
		{"恰好 50 字", strings.Repeat("名", 50), nil},
		{"空", "", ErrDisplayNameEmpty},
		{"仅空白", "  ", ErrDisplayNameEmpty},
		{"51 字过长", strings.Repeat("名", 51), ErrDisplayNameTooLong},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ValidateDisplayName(c.in); !errors.Is(got, c.want) {
				t.Fatalf("ValidateDisplayName(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// #203：「须改密码」为真时，只放行登录、登出、修改密码、读当前用户（模块 PRD §5.3、§11），
// 其余接口一律 403 password_change_required。品牌读取接口（#210）到时加入放行表。
func TestPasswordChangeRequiredAllows(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/api/v1/auth/login", true},
		{"/api/v1/auth/logout", true},
		{"/api/v1/auth/change-password", true},
		{"/api/v1/auth/me", true},
		{"/api/v1/healthz", true},
		{"/api/v1/projects", false},
		{"/api/v1/projects/1", false},
		{"/api/v1/system/users", false},
		{"/api/v1/users", false},
		{"/api/v1/notifications", false},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			if got := PasswordChangeRequiredAllows(c.path); got != c.want {
				t.Fatalf("PasswordChangeRequiredAllows(%q) = %v, want %v", c.path, got, c.want)
			}
		})
	}
}
