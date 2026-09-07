package domain

import (
	"errors"
	"testing"
)

// #211：按内容探测出的类型判定（不信文件名与声明的类型），SVG 与非图片一律拒绝；大小上限 512KB。
func TestValidateLogo(t *testing.T) {
	cases := []struct {
		name string
		typ  string
		size int64
		want error
	}{
		{"PNG 合法", "image/png", 1024, nil},
		{"JPEG 合法", "image/jpeg", 1024, nil},
		{"WebP 合法", "image/webp", 1024, nil},
		{"恰好 512KB", "image/png", MaxLogoBytes, nil},
		{"超过 512KB", "image/png", MaxLogoBytes + 1, ErrLogoTooLarge},
		{"SVG 拒绝", "image/svg+xml", 100, ErrLogoTypeUnsupported},
		{"GIF 拒绝", "image/gif", 100, ErrLogoTypeUnsupported},
		{"文本拒绝", "text/plain; charset=utf-8", 100, ErrLogoTypeUnsupported},
		{"空文件", "image/png", 0, ErrLogoEmpty},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ValidateLogo(c.typ, c.size); !errors.Is(got, c.want) {
				t.Fatalf("ValidateLogo(%q, %d) = %v, want %v", c.typ, c.size, got, c.want)
			}
		})
	}
}
