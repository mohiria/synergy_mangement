package domain

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// 邮件通道（词汇表「邮件通道」；模块 PRD §10；#212）。

// 加密方式三值枚举。
const (
	MailEncryptionNone     = "none"
	MailEncryptionStartTLS = "starttls"
	MailEncryptionSSL      = "ssl"
)

// outbox 状态。
const (
	MailPending = "pending"
	MailSent    = "sent"
	MailFailed  = "failed"
)

// 邮件事件（outbox 的 event 字段）。
const (
	MailEventTest          = "test"
	MailEventPasswordReset = "password_reset"
)

var (
	ErrMailHostEmpty         = errors.New("SMTP 主机不能为空")
	ErrMailPortInvalid       = errors.New("SMTP 端口须在 1～65535")
	ErrMailEncryptionInvalid = errors.New("加密方式须为无、STARTTLS 或 SSL")
	ErrMailFromInvalid       = errors.New("发件人地址格式不正确")
	ErrMailFromNameTooLong   = errors.New("发件人显示名不能超过 50 字")
	ErrMailNotConfigured     = errors.New("邮件通道尚未配置")
	ErrMailTestTargetInvalid = errors.New("测试邮件收件地址格式不正确")
)

// MailSettingsInput 通道配置（不含密码；密码单独处理，永不回显）。
type MailSettingsInput struct {
	Host        string
	Port        int
	Encryption  string
	Username    string
	FromName    string
	FromAddress string
}

var mailEncryptions = map[string]struct{}{MailEncryptionNone: {}, MailEncryptionStartTLS: {}, MailEncryptionSSL: {}}

// ValidateMailSettings 校验并归一通道配置（#212）：主机必填、端口 1～65535、加密方式三值、
// 发件人地址按邮箱规则并小写归一、显示名 ≤50；密码不在此处（单独加密落库，永不回显）。
func ValidateMailSettings(in MailSettingsInput) (MailSettingsInput, error) {
	out := MailSettingsInput{
		Host: strings.TrimSpace(in.Host), Port: in.Port, Encryption: strings.ToLower(strings.TrimSpace(in.Encryption)),
		Username: strings.TrimSpace(in.Username), FromName: strings.TrimSpace(in.FromName), FromAddress: NormalizeEmail(in.FromAddress),
	}
	if out.Host == "" {
		return out, ErrMailHostEmpty
	}
	if out.Port < 1 || out.Port > 65535 {
		return out, ErrMailPortInvalid
	}
	if _, ok := mailEncryptions[out.Encryption]; !ok {
		return out, ErrMailEncryptionInvalid
	}
	if err := ValidateEmail(out.FromAddress); err != nil {
		return out, ErrMailFromInvalid
	}
	if utf8.RuneCountInString(out.FromName) > 50 {
		return out, ErrMailFromNameTooLong
	}
	return out, nil
}

// MailChannelConfigured 主机与发件人地址齐全即视为已配置：找回密码入口与邮件通知据此判定。
func MailChannelConfigured(host, fromAddress string) bool {
	return strings.TrimSpace(host) != "" && strings.TrimSpace(fromAddress) != ""
}

// MailRetryDelays 失败退避：第 1、2、3 次失败后分别等 1、5、15 分钟再试，第 4 次失败标记失败。
var MailRetryDelays = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}

// MailRetry 第 attempts 次失败后的处置：还有退避档位就给出下次尝试时间，否则标记失败不再重试。
func MailRetry(attempts int, now time.Time) (time.Time, bool) {
	if attempts < 1 || attempts > len(MailRetryDelays) {
		return now, true
	}
	return now.Add(MailRetryDelays[attempts-1]), false
}

var mailEventLabels = map[string]string{
	MailEventTest:          "测试邮件",
	MailEventPasswordReset: "找回密码",
}

// MailEventLabel 邮件事件显示文案（派生字段）。
func MailEventLabel(event string) string {
	if l, ok := mailEventLabels[event]; ok {
		return l
	}
	return event
}

var mailStatusLabels = map[string]string{
	MailPending: "待发送",
	MailSent:    "已发送",
	MailFailed:  "失败",
}

// MailStatusLabel outbox 状态显示文案（派生字段）。
func MailStatusLabel(status string) string {
	if l, ok := mailStatusLabels[status]; ok {
		return l
	}
	return status
}
