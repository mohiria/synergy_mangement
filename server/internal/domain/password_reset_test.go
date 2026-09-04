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

// 重置链接：有访问地址用访问地址，否则用请求 Host 兜底（http）；token 进查询串。
func TestPasswordResetLink(t *testing.T) {
	if got := PasswordResetLink("http://203.0.113.10", "ignored:8080", "abc"); got != "http://203.0.113.10/reset-password?token=abc" {
		t.Fatalf("got %q", got)
	}
	if got := PasswordResetLink("", "203.0.113.10:80", "abc"); got != "http://203.0.113.10:80/reset-password?token=abc" {
		t.Fatalf("got %q", got)
	}
	if got := PasswordResetLink("https://x.example/app/", "h", "t"); got != "https://x.example/app/reset-password?token=t" {
		t.Fatalf("got %q", got)
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
