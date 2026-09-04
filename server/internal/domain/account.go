package domain

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	ErrUsernameEmpty          = errors.New("用户名不能为空")
	ErrUsernameInvalid        = errors.New("用户名只能包含小写字母、数字、点、下划线与连字符，长度 3～32")
	ErrUsernameTaken          = errors.New("用户名已被使用")
	ErrDisplayNameEmpty       = errors.New("显示名不能为空")
	ErrDisplayNameTooLong     = errors.New("显示名不能超过 50 字")
	ErrPasswordChangeRequired = errors.New("首次登录请先设置新密码")
)

// usernamePattern 登录用户名：小写字母、数字、点、下划线、连字符，3～32 位（#203）。
var usernamePattern = regexp.MustCompile(`^[a-z0-9._-]{3,32}$`)

// ValidateUsername 管理员建号时的用户名规则；重复由唯一约束兜底（ErrUsernameTaken）。
func ValidateUsername(username string) error {
	trimmed := strings.TrimSpace(username)
	if trimmed == "" {
		return ErrUsernameEmpty
	}
	if !usernamePattern.MatchString(trimmed) {
		return ErrUsernameInvalid
	}
	return nil
}

// ValidateDisplayName 显示名：去首尾空白后非空且不超过 50 字。
func ValidateDisplayName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ErrDisplayNameEmpty
	}
	if utf8.RuneCountInString(trimmed) > 50 {
		return ErrDisplayNameTooLong
	}
	return nil
}

// passwordChangeRequiredAllowlist 「须改密码」为真时仍放行的路由后缀（模块 PRD §5.3、§11）：
// 登录、登出、修改密码、读当前用户，以及无需会话的健康检查；品牌读取（#210）到时加入。
var passwordChangeRequiredAllowlist = []string{"/auth/login", "/auth/logout", "/auth/change-password", "/auth/me", "/healthz"}

// PasswordChangeRequiredAllows 判定某路径在「须改密码」状态下是否放行；其余一律 403。
func PasswordChangeRequiredAllows(path string) bool {
	for _, suffix := range passwordChangeRequiredAllowlist {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}
