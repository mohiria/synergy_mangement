package domain

import (
	"errors"
	"strings"
	"testing"
)

// #210：基本信息四项——名称必填 ≤10、副标题 ≤16 可空、提示语 ≤60 可空、访问地址为完整 URL 或空；
// 全部按 Unicode 字符计并去首尾空白。
func TestValidateSystemSettings(t *testing.T) {
	ok := SystemSettingsInput{SystemName: "协同管理工具", Subtitle: "O／KR／任务协同推进", LoginHint: "账号由管理员分配", BaseURL: "http://203.0.113.10"}
	cases := []struct {
		name string
		in   SystemSettingsInput
		want error
	}{
		{"默认值合法", ok, nil},
		{"名称恰好 10 字", SystemSettingsInput{SystemName: strings.Repeat("名", 10)}, nil},
		{"名称 11 字过长", SystemSettingsInput{SystemName: strings.Repeat("名", 11)}, ErrSystemNameTooLong},
		{"名称为空", SystemSettingsInput{SystemName: "  "}, ErrSystemNameEmpty},
		{"副标题恰好 16 字", SystemSettingsInput{SystemName: "x", Subtitle: strings.Repeat("副", 16)}, nil},
		{"副标题 17 字过长", SystemSettingsInput{SystemName: "x", Subtitle: strings.Repeat("副", 17)}, ErrSubtitleTooLong},
		{"提示语恰好 60 字", SystemSettingsInput{SystemName: "x", LoginHint: strings.Repeat("提", 60)}, nil},
		{"提示语 61 字过长", SystemSettingsInput{SystemName: "x", LoginHint: strings.Repeat("提", 61)}, ErrLoginHintTooLong},
		{"副标题与提示语可空", SystemSettingsInput{SystemName: "x"}, nil},
		{"访问地址 https 合法", SystemSettingsInput{SystemName: "x", BaseURL: "https://example.com/app"}, nil},
		{"访问地址缺协议", SystemSettingsInput{SystemName: "x", BaseURL: "example.com"}, ErrBaseURLInvalid},
		{"访问地址协议不对", SystemSettingsInput{SystemName: "x", BaseURL: "ftp://example.com"}, ErrBaseURLInvalid},
		{"访问地址无主机", SystemSettingsInput{SystemName: "x", BaseURL: "http://"}, ErrBaseURLInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, got := ValidateSystemSettings(c.in); !errors.Is(got, c.want) {
				t.Fatalf("ValidateSystemSettings(%+v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
	// 归一：去首尾空白，访问地址去尾部斜杠。
	got, err := ValidateSystemSettings(SystemSettingsInput{SystemName: " 名称 ", Subtitle: " 副 ", LoginHint: " 提 ", BaseURL: "http://a.b/"})
	if err != nil || got.SystemName != "名称" || got.Subtitle != "副" || got.LoginHint != "提" || got.BaseURL != "http://a.b" {
		t.Fatalf("归一结果异常: %+v %v", got, err)
	}
}
