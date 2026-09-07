package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// 找回密码（模块 PRD §4；#214）：token 一次性、只落哈希、30 分钟有效；请求阶段统一回文防枚举。

// PasswordResetTTL 重置链接有效期。
const PasswordResetTTL = 30 * time.Minute

// PasswordResetRequestedMessage 无论账号是否存在、是否停用都返回的统一文案。
const PasswordResetRequestedMessage = "若账号存在，重置邮件已发送"

var (
	ErrResetTokenInvalid = errors.New("链接无效或已过期")
	ErrResetNotAvailable = errors.New("未开通找回密码")
)

// NewPasswordResetToken 生成 256 位随机 token（hex）；明文只进邮件，库里只存哈希。
func NewPasswordResetToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// HashPasswordResetToken token 的 SHA-256 hex，落库与比对都用它。
func HashPasswordResetToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

// ValidatePasswordResetToken token 可用性：查不到（篡改）、已用、到期（含恰好到期）、用户已停用
// 一律同一错误，不区分原因（模块 PRD §4.3）。
func ValidatePasswordResetToken(found bool, expiresAt time.Time, used bool, userDisabled bool, now time.Time) error {
	if !found || used || userDisabled || !now.Before(expiresAt) {
		return ErrResetTokenInvalid
	}
	return nil
}

// PasswordResetLink 重置链接只从系统设置的访问地址拼出（去尾部斜杠）。#215：不再用请求 Host 兜底——
// 请求阶段免登录，Host 可被伪造成攻击者域名，收件人点开就把 token 泄给对方。
func PasswordResetLink(baseURL, token string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/reset-password?token=" + token
}

// CanRecoverPassword 找回密码入口是否可用：邮件通道与访问地址都已配置才显示（模块 PRD §4.1；#215）。
func CanRecoverPassword(mailConfigured, baseURLConfigured bool) bool {
	return mailConfigured && baseURLConfigured
}
