package mail

import "testing"

// #215：IPv6 字面量主机不能直接拼冒号，否则 net.Dialer 报 too many colons。
func TestSMTPAddr(t *testing.T) {
	for _, tc := range []struct {
		host string
		port int
		want string
	}{
		{"smtp.example.com", 587, "smtp.example.com:587"},
		{"127.0.0.1", 25, "127.0.0.1:25"},
		{"2001:db8::1", 465, "[2001:db8::1]:465"},
	} {
		if got := smtpAddr(tc.host, tc.port); got != tc.want {
			t.Errorf("smtpAddr(%q, %d) = %q, want %q", tc.host, tc.port, got, tc.want)
		}
	}
}
