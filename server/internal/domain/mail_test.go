package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// #212：通道配置——主机必填、端口 1～65535、加密方式三值、发件人地址合法、显示名 ≤50；归一去空白与小写地址。
func TestValidateMailSettings(t *testing.T) {
	ok := MailSettingsInput{Host: "smtp.example.com", Port: 587, Encryption: MailEncryptionStartTLS, Username: "bot", FromName: "协同", FromAddress: "bot@example.com"}
	cases := []struct {
		name string
		in   MailSettingsInput
		want error
	}{
		{"合法", ok, nil},
		{"无认证账号合法", MailSettingsInput{Host: "h", Port: 25, Encryption: MailEncryptionNone, FromAddress: "a@b.co"}, nil},
		{"SSL 合法", MailSettingsInput{Host: "h", Port: 465, Encryption: MailEncryptionSSL, FromAddress: "a@b.co"}, nil},
		{"主机为空", MailSettingsInput{Port: 25, Encryption: MailEncryptionNone, FromAddress: "a@b.co"}, ErrMailHostEmpty},
		{"端口 0", MailSettingsInput{Host: "h", Port: 0, Encryption: MailEncryptionNone, FromAddress: "a@b.co"}, ErrMailPortInvalid},
		{"端口 65536", MailSettingsInput{Host: "h", Port: 65536, Encryption: MailEncryptionNone, FromAddress: "a@b.co"}, ErrMailPortInvalid},
		{"加密方式非法", MailSettingsInput{Host: "h", Port: 25, Encryption: "tls", FromAddress: "a@b.co"}, ErrMailEncryptionInvalid},
		{"发件人地址非法", MailSettingsInput{Host: "h", Port: 25, Encryption: MailEncryptionNone, FromAddress: "nope"}, ErrMailFromInvalid},
		{"显示名过长", MailSettingsInput{Host: "h", Port: 25, Encryption: MailEncryptionNone, FromAddress: "a@b.co", FromName: strings.Repeat("名", 51)}, ErrMailFromNameTooLong},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, got := ValidateMailSettings(c.in); !errors.Is(got, c.want) {
				t.Fatalf("ValidateMailSettings(%+v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
	got, err := ValidateMailSettings(MailSettingsInput{Host: " Smtp.Example.com ", Port: 25, Encryption: MailEncryptionNone, FromAddress: " Bot@Example.com ", FromName: " 协同 "})
	if err != nil || got.Host != "Smtp.Example.com" || got.FromAddress != "bot@example.com" || got.FromName != "协同" {
		t.Fatalf("归一异常: %+v %v", got, err)
	}
}

func TestMailChannelConfigured(t *testing.T) {
	if MailChannelConfigured("", "a@b.co") || MailChannelConfigured("h", "") {
		t.Fatal("主机或发件人为空即未配置")
	}
	if !MailChannelConfigured("h", "a@b.co") {
		t.Fatal("主机与发件人齐全即已配置")
	}
}

// 失败退避：第 1／2／3 次失败后 1／5／15 分钟再试；第 4 次失败标记失败不再重试。
func TestMailRetry(t *testing.T) {
	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		attempts int
		wantNext time.Time
		wantFail bool
	}{
		{1, now.Add(time.Minute), false},
		{2, now.Add(5 * time.Minute), false},
		{3, now.Add(15 * time.Minute), false},
		{4, now, true},
		{9, now, true},
	}
	for _, c := range cases {
		next, failed := MailRetry(c.attempts, now)
		if failed != c.wantFail || (!failed && !next.Equal(c.wantNext)) {
			t.Fatalf("MailRetry(%d) = %v %v, want %v %v", c.attempts, next, failed, c.wantNext, c.wantFail)
		}
	}
}
