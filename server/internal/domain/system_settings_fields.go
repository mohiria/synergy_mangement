package domain

import (
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"
)

// 系统设置「基本信息」字段（模块 PRD §7.1；#210）。上限按 Unicode 字符计：
// 侧栏 224px、标题 16px 一行约 8 汉字 → 系统名称 ≤10；副标题 12px 约 11 汉字 → ≤16；提示语 ≤60。
const (
	MaxSystemNameLength = 10
	MaxSubtitleLength   = 16
	MaxLoginHintLength  = 60
	MaxBaseURLLength    = 254

	DefaultSystemName = "协同管理工具"
	DefaultSubtitle   = "O／KR／任务协同推进"
	DefaultLoginHint  = "账号由管理员分配"
)

var (
	ErrSystemNameEmpty   = errors.New("系统名称不能为空")
	ErrSystemNameTooLong = errors.New("系统名称不能超过 10 个字符")
	ErrSubtitleTooLong   = errors.New("副标题不能超过 16 个字符")
	ErrLoginHintTooLong  = errors.New("登录页提示语不能超过 60 个字符")
	ErrBaseURLInvalid    = errors.New("访问地址须为 http:// 或 https:// 开头的完整地址")
)

// SystemSettingsInput 基本信息四项（去首尾空白后）。
type SystemSettingsInput struct {
	SystemName string
	Subtitle   string
	LoginHint  string
	BaseURL    string
}

// ValidateSystemSettings 校验并归一基本信息四项（#210）：去首尾空白；名称必填且不超上限；
// 副标题、提示语可空但有上限；访问地址为空或 http(s):// 开头且带主机的完整地址，去尾部斜杠。
func ValidateSystemSettings(in SystemSettingsInput) (SystemSettingsInput, error) {
	out := SystemSettingsInput{
		SystemName: strings.TrimSpace(in.SystemName),
		Subtitle:   strings.TrimSpace(in.Subtitle),
		LoginHint:  strings.TrimSpace(in.LoginHint),
		BaseURL:    strings.TrimRight(strings.TrimSpace(in.BaseURL), "/"),
	}
	if out.SystemName == "" {
		return out, ErrSystemNameEmpty
	}
	if utf8.RuneCountInString(out.SystemName) > MaxSystemNameLength {
		return out, ErrSystemNameTooLong
	}
	if utf8.RuneCountInString(out.Subtitle) > MaxSubtitleLength {
		return out, ErrSubtitleTooLong
	}
	if utf8.RuneCountInString(out.LoginHint) > MaxLoginHintLength {
		return out, ErrLoginHintTooLong
	}
	if out.BaseURL != "" {
		u, err := url.Parse(out.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(out.BaseURL) > MaxBaseURLLength {
			return out, ErrBaseURLInvalid
		}
	}
	return out, nil
}
