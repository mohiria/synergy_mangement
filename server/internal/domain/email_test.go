package domain

import (
	"errors"
	"strings"
	"testing"
)

// #202：邮箱必填、格式合法、大小写不敏感（比对与存储都按小写归一）。不做邮箱验证。
func TestValidateEmail(t *testing.T) {
	cases := []struct {
		name  string
		email string
		want  error
	}{
		{"合法", "alice@example.com", nil},
		{"首尾空白与大小写归一后合法", "  Alice@Example.COM ", nil},
		{"子域名合法", "a.b@mail.example.co", nil},
		{"空", "", ErrEmailEmpty},
		{"仅空白", "   ", ErrEmailEmpty},
		{"缺 @", "aliceexample.com", ErrEmailInvalid},
		{"缺域名", "alice@", ErrEmailInvalid},
		{"域名无点", "alice@localhost", ErrEmailInvalid},
		{"两个 @", "a@b@example.com", ErrEmailInvalid},
		{"含空格", "ali ce@example.com", ErrEmailInvalid},
		{"超过 254 字符", strings.Repeat("a", 250) + "@x.io", ErrEmailInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ValidateEmail(c.email); !errors.Is(got, c.want) {
				t.Fatalf("ValidateEmail(%q) = %v, want %v", c.email, got, c.want)
			}
		})
	}
}

func TestNormalizeEmail(t *testing.T) {
	if got := NormalizeEmail("  Alice@Example.COM "); got != "alice@example.com" {
		t.Fatalf("NormalizeEmail() = %q, want alice@example.com", got)
	}
}
