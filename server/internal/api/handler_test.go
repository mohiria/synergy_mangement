package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 限速要按真实来源 IP 计数（S3）。部署形态是 Caddy 反代 app（web/Caddyfile），
// 所有请求的 RemoteAddr 都是 Caddy 容器地址，必须读 X-Forwarded-For；
// 且客户端可以自带伪造的 XFF，Caddy 只在其尾部追加真实对端，所以取最右一段。
func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		want       string
	}{
		{"直连取 RemoteAddr 的 host", "10.0.0.7:51234", "", "10.0.0.7"},
		{"RemoteAddr 无端口时原样取用", "10.0.0.7", "", "10.0.0.7"},
		{"IPv6 直连去掉端口与方括号", "[::1]:51234", "", "::1"},
		{"反代单段取该值", "172.18.0.3:40000", "10.0.0.7", "10.0.0.7"},
		{"伪造前缀时取最右的可信段", "172.18.0.3:40000", "1.2.3.4, 5.6.7.8, 10.0.0.7", "10.0.0.7"},
		{"忽略分号与空白", "172.18.0.3:40000", " 1.2.3.4 ,  10.0.0.7 ", "10.0.0.7"},
		{"XFF 全空则回落 RemoteAddr", "172.18.0.3:40000", " , ", "172.18.0.3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/v1/login", nil)
			r.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				r.Header.Set("X-Forwarded-For", tt.xff)
			}
			if got := clientIP(r); got != tt.want {
				t.Fatalf("clientIP = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetHealthz(t *testing.T) {
	h := NewHandler(nil, "/api/v1", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body Health
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Status != Ok {
		t.Fatalf("status field = %q, want %q", body.Status, Ok)
	}
}
