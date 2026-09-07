package domain

import (
	"errors"
	"testing"
	"time"
)

// #214：token 过期、已用、篡改（查不到）、用户已停用一律「链接无效或已过期」；在有效期内且未用才通过。
func TestValidatePasswordResetToken(t *testing.T) {
	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		found    bool
		expires  time.Time
		used     bool
		disabled bool
		want     error
	}{
		{"有效", true, now.Add(10 * time.Minute), false, false, nil},
		{"恰好到期视为过期", true, now, false, false, ErrResetTokenInvalid},
		{"已过期", true, now.Add(-time.Second), false, false, ErrResetTokenInvalid},
		{"已使用", true, now.Add(10 * time.Minute), true, false, ErrResetTokenInvalid},
		{"查不到（篡改）", false, time.Time{}, false, false, ErrResetTokenInvalid},
		{"用户已停用", true, now.Add(10 * time.Minute), false, true, ErrResetTokenInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ValidatePasswordResetToken(c.found, c.expires, c.used, c.disabled, now); !errors.Is(got, c.want) {
				t.Fatalf("ValidatePasswordResetToken() = %v, want %v", got, c.want)
			}
		})
	}
}

// 重置链接只从系统设置的访问地址拼出（#215：不再用请求 Host 兜底，Host 可被未登录请求伪造）；token 进查询串。
func TestPasswordResetLink(t *testing.T) {
	if got := PasswordResetLink("http://203.0.113.10", "abc"); got != "http://203.0.113.10/reset-password?token=abc" {
		t.Fatalf("got %q", got)
	}
	if got := PasswordResetLink(" https://x.example/ ", "t"); got != "https://x.example/reset-password?token=t" {
		t.Fatalf("got %q", got)
	}
}

// 找回密码入口：邮件通道与访问地址都已配置才开通（#215：没有访问地址就拼不出可信链接）。
func TestCanRecoverPassword(t *testing.T) {
	if !CanRecoverPassword(true, true) {
		t.Fatal("通道与访问地址齐全应开通")
	}
	if CanRecoverPassword(false, true) {
		t.Fatal("通道未配置不应开通")
	}
	if CanRecoverPassword(true, false) {
		t.Fatal("访问地址未配置不应开通")
	}
}

func TestPasswordResetTokenHash(t *testing.T) {
	tok, err := NewPasswordResetToken()
	if err != nil || len(tok) != 64 {
		t.Fatalf("token = %q, %v", tok, err)
	}
	if HashPasswordResetToken(tok) == tok || HashPasswordResetToken(tok) != HashPasswordResetToken(" "+tok+" ") {
		t.Fatal("哈希应与明文不同且忽略首尾空白")
	}
}
