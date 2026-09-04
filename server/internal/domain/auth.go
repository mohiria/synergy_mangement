package domain

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"unicode/utf8"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	// SessionTTL 会话有效期，7 天滑动过期（ADR-0001）。
	SessionTTL = 7 * 24 * time.Hour
	// RenewSlack 滑动续期间隔：距上次续期超过该时长才写库，避免每个请求都更新。
	RenewSlack = time.Hour
	// MaxLoginFailures 连续失败次数上限，达到后在锁定窗口内拒绝尝试。
	MaxLoginFailures = 5
	// LoginLockWindow 登录失败锁定窗口。
	LoginLockWindow = 15 * time.Minute
)

// SessionRenewal 判定会话是否需要滑动续期。返回新的过期时间与是否需要写库。
func SessionRenewal(now, expiresAt time.Time) (time.Time, bool) {
	newExpiry := now.Add(SessionTTL)
	if newExpiry.Sub(expiresAt) >= RenewSlack {
		return newExpiry, true
	}
	return expiresAt, false
}

// LoginThrottle 进程内登录失败限速（ADR-0001：进程内缓存替代 Redis）。
// 计数维度是 (用户名, 来源 IP)：只按用户名计数时，任何人对着别人的账号连打几次错密码
// 就能把真实用户锁在门外（S3）。同一 (用户名, IP) 连续失败达上限后在锁定窗口内拒绝继续尝试；
// 距上次失败超过锁定窗口的旧记录不再累计，并由 Sweep 清出 map。
type LoginThrottle struct {
	mu    sync.Mutex
	state map[throttleKey]*failState
}

// throttleKey 限速计数的维度。
type throttleKey struct {
	username string
	ip       string
}

type failState struct {
	failures    int
	lastFailure time.Time
}

func NewLoginThrottle() *LoginThrottle {
	return &LoginThrottle{state: map[throttleKey]*failState{}}
}

// Allow 判定该用户名当前是否允许尝试登录。
func (t *LoginThrottle) Allow(username, ip string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.state[throttleKey{username: username, ip: ip}]
	if !ok {
		return true
	}
	if now.Sub(s.lastFailure) > LoginLockWindow {
		return true
	}
	return s.failures < MaxLoginFailures
}

// RecordFailure 记录一次登录失败。
func (t *LoginThrottle) RecordFailure(username, ip string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	k := throttleKey{username: username, ip: ip}
	s, ok := t.state[k]
	if !ok || now.Sub(s.lastFailure) > LoginLockWindow {
		t.state[k] = &failState{failures: 1, lastFailure: now}
		return
	}
	s.failures++
	s.lastFailure = now
}

// RecordSuccess 登录成功后清零失败计数。
func (t *LoginThrottle) RecordSuccess(username, ip string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.state, throttleKey{username: username, ip: ip})
}

// Sweep 清理超出锁定窗口的失败记录，返回清掉的条数。
func (t *LoginThrottle) Sweep(now time.Time) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := 0
	for k, s := range t.state {
		if now.Sub(s.lastFailure) > LoginLockWindow {
			delete(t.state, k)
			n++
		}
	}
	return n
}

// Size 当前保留的失败记录条数。
func (t *LoginThrottle) Size() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.state)
}

// MinPasswordLength 密码最小长度（S3：内网自用，只挡住明显过短的密码）。
const MinPasswordLength = 8

// MaxPasswordLength 密码最大长度（#193，按 Unicode 字符计）。
const MaxPasswordLength = 32

var (
	ErrPasswordTooLong   = errors.New("新密码最多 32 位")
	ErrPasswordTooShort  = errors.New("新密码至少 8 位")
	ErrPasswordUnchanged = errors.New("新密码不能与当前密码相同")
	ErrPasswordWrong     = errors.New("当前密码不正确")
)

// bcryptMaxBytes bcrypt 只取前 72 字节，更长的部分会被静默截断；
// 32 个汉字按字符计不超上限但有 96 字节，必须一并挡下（模块 PRD §5.2）。
const bcryptMaxBytes = 72

// ValidatePasswordLength 密码长度规则（#193）：8～32 位按 Unicode 字符计，
// 且 UTF-8 不超过 72 字节。所有密码入口（改密、建号、重置、找回）共用。
func ValidatePasswordLength(password string) error {
	trimmed := strings.TrimSpace(password)
	if utf8.RuneCountInString(trimmed) < MinPasswordLength {
		return ErrPasswordTooShort
	}
	if utf8.RuneCountInString(password) > MaxPasswordLength || len(password) > bcryptMaxBytes {
		return ErrPasswordTooLong
	}
	return nil
}

// ValidatePasswordChange 校验一次改密码（S3）：新密码满足长度规则且不能与当前密码相同。
// 当前密码是否正确由调用方比对哈希后判定，规则本身不碰哈希。
func ValidatePasswordChange(current, next string) error {
	if err := ValidatePasswordLength(next); err != nil {
		return err
	}
	if next == current {
		return ErrPasswordUnchanged
	}
	return nil
}

// HashPassword 生成 bcrypt 哈希（ADR-0001）。
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// VerifyPassword 校验明文密码与 bcrypt 哈希是否匹配。
func VerifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// NewSessionToken 生成 256 位随机会话 token（hex 编码）。
func NewSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
