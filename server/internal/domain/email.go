package domain

import (
	"errors"
	"regexp"
	"strings"
)

var (
	ErrEmailEmpty   = errors.New("邮箱不能为空")
	ErrEmailInvalid = errors.New("邮箱格式不正确")
	ErrEmailTaken   = errors.New("邮箱已被使用")
)

// emailPattern 只挡明显不是邮箱的输入：一个 @、两侧非空且无空白、域名含点。
// 不做邮箱验证（模块 PRD §9），所以不追求 RFC 5322 全量语法。
var emailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// maxEmailLength 邮箱总长上限（RFC 5321 路径上限 254）。
const maxEmailLength = 254

// NormalizeEmail 邮箱归一：去首尾空白、转小写。比对与存储都用归一后的值，
// 唯一索引建在 lower(email) 上（#202）。
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ValidateEmail 邮箱必填且格式合法（#202）；所有写邮箱的入口（useradd、建号、改资料）共用。
func ValidateEmail(email string) error {
	n := NormalizeEmail(email)
	if n == "" {
		return ErrEmailEmpty
	}
	if len(n) > maxEmailLength || !emailPattern.MatchString(n) {
		return ErrEmailInvalid
	}
	return nil
}
