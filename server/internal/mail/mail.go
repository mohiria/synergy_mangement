// Package mail SMTP 出口（#212）：只负责把一封纯文本邮件按通道配置送出去；
// 排队、重试与开关判定在 api 层的 outbox 与 domain 规则里。
package mail

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"sync"
	"time"
)

// 加密方式（词汇表「邮件通道」）。
const (
	EncryptionNone     = "none"
	EncryptionStartTLS = "starttls"
	EncryptionSSL      = "ssl"
)

// Config 一次发送用到的通道配置（密码已解密）。
type Config struct {
	Host        string
	Port        int
	Encryption  string
	Username    string
	Password    string
	FromName    string
	FromAddress string
}

// Message 一封纯文本邮件。
type Message struct {
	To      string
	Subject string
	Body    string
}

// Sender 发送器：真实 SMTP 与测试用的记录器都实现它。
type Sender interface {
	Send(ctx context.Context, cfg Config, msg Message) error
}

// SMTPSender net/smtp 实现：none 明文、starttls 先明文再升级、ssl 直接 TLS。连接超时 15 秒。
type SMTPSender struct {
	Timeout time.Duration
}

func (s SMTPSender) Send(ctx context.Context, cfg Config, msg Message) error {
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	dialer := &net.Dialer{Timeout: timeout}
	var conn net.Conn
	var err error
	if cfg.Encryption == EncryptionSSL {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: cfg.Host})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("连接 %s 失败: %w", addr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	c, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return fmt.Errorf("SMTP 握手失败: %w", err)
	}
	defer c.Close()
	if cfg.Encryption == EncryptionStartTLS {
		if err := c.StartTLS(&tls.Config{ServerName: cfg.Host}); err != nil {
			return fmt.Errorf("STARTTLS 失败: %w", err)
		}
	}
	if cfg.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
			return fmt.Errorf("SMTP 认证失败: %w", err)
		}
	}
	if err := c.Mail(cfg.FromAddress); err != nil {
		return fmt.Errorf("MAIL FROM 失败: %w", err)
	}
	if err := c.Rcpt(msg.To); err != nil {
		return fmt.Errorf("RCPT TO 失败: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("DATA 失败: %w", err)
	}
	if _, err := w.Write([]byte(Render(cfg, msg))); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// Render 组装 RFC 5322 文本：From／To／Subject／Date 与 UTF-8 纯文本正文。
func Render(cfg Config, msg Message) string {
	from := cfg.FromAddress
	if cfg.FromName != "" {
		from = fmt.Sprintf("=?UTF-8?B?%s?= <%s>", b64(cfg.FromName), cfg.FromAddress)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", msg.To)
	fmt.Fprintf(&b, "Subject: =?UTF-8?B?%s?=\r\n", b64(msg.Subject))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(strings.ReplaceAll(msg.Body, "\n", "\r\n"))
	b.WriteString("\r\n")
	return b.String()
}

// Recorder 测试用发送器：记录发出的邮件；Err 非空时每次都失败。
type Recorder struct {
	mu   sync.Mutex
	Err  error
	Sent []Message
}

func (r *Recorder) Send(_ context.Context, _ Config, msg Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Err != nil {
		return r.Err
	}
	r.Sent = append(r.Sent, msg)
	return nil
}

// Messages 已发出邮件的副本。
func (r *Recorder) Messages() []Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Message(nil), r.Sent...)
}
