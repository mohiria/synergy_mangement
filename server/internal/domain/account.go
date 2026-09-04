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
	ErrAccountDisabled        = errors.New("账号已停用，请联系管理员")
	ErrCannotDisableSelf      = errors.New("不能停用自己")
	ErrCannotRevokeOwnAdmin   = errors.New("不能撤销自己的系统管理员")
)

// CanRevokeSystemAdmin 设／撤系统管理员的规则（#205）：不能撤销自己，防止管理员全部锁死；
// 应急恢复只走 CLI usermod（ADR 0003）。
func CanRevokeSystemAdmin(actorID, targetID int64, makeAdmin bool) error {
	if !makeAdmin && actorID == targetID {
		return ErrCannotRevokeOwnAdmin
	}
	return nil
}

// LoginOutcome 登录判定结果（#204）。
type LoginOutcome int

const (
	LoginOK LoginOutcome = iota
	LoginInvalidCredentials
	LoginDisabled
)

// DecideLogin 登录判定（#204，模块 PRD §3.2）：账号不存在或密码错误一律「用户名或密码错误」；
// 只有用户名与密码都正确时，停用账号才提示「已停用」——公网部署下不让人靠登录探测账号存在或状态。
func DecideLogin(found, passwordOK, disabled bool) LoginOutcome {
	if !found || !passwordOK {
		return LoginInvalidCredentials
	}
	if disabled {
		return LoginDisabled
	}
	return LoginOK
}

// CanDisableUser 停用规则：不能停用自己（#204）。
func CanDisableUser(actorID, targetID int64) error {
	if actorID == targetID {
		return ErrCannotDisableSelf
	}
	return nil
}

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
