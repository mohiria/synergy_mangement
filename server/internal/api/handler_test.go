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

// #192：写请求同源校验——SameSite=Lax 之外的第二道 CSRF 防线，挂在 /api 最外层、认证之前。
// 只和请求 Host 比对（IP 部署无固定域名，不写允许列表）；缺 Origin 放行（非浏览器客户端拿不到受害者 cookie）；
// Sec-Fetch-Site: cross-site 直接拒绝；GET/HEAD/OPTIONS 不校验。
func TestSameOriginMiddleware(t *testing.T) {
	tests := []struct {
		name         string
		method       string
		origin       string
		secFetchSite string
		want         int
	}{
		{"同源 Origin 的 POST 通过", http.MethodPost, "http://app.local:8080", "", http.StatusOK},
		{"跨源 Origin 的 POST 拒绝", http.MethodPost, "http://evil.example", "", http.StatusForbidden},
		{"无 Origin 的 POST 通过", http.MethodPost, "", "", http.StatusOK},
		{"Sec-Fetch-Site cross-site 直接拒绝", http.MethodPost, "", "cross-site", http.StatusForbidden},
		{"Sec-Fetch-Site cross-site 即使 Origin 同源也拒绝", http.MethodPost, "http://app.local:8080", "cross-site", http.StatusForbidden},
		{"Sec-Fetch-Site same-origin 通过", http.MethodPost, "http://app.local:8080", "same-origin", http.StatusOK},
		{"Sec-Fetch-Site none（地址栏直达）通过", http.MethodPost, "", "none", http.StatusOK},
		{"GET 跨源不校验", http.MethodGet, "http://evil.example", "", http.StatusOK},
		{"HEAD 跨源不校验", http.MethodHead, "http://evil.example", "", http.StatusOK},
		{"OPTIONS 跨源不校验", http.MethodOptions, "http://evil.example", "", http.StatusOK},
		{"PUT 跨源拒绝", http.MethodPut, "http://evil.example", "", http.StatusForbidden},
		{"PATCH 跨源拒绝", http.MethodPatch, "http://evil.example", "", http.StatusForbidden},
		{"DELETE 跨源拒绝", http.MethodDelete, "http://evil.example", "", http.StatusForbidden},
		{"Origin: null 拒绝", http.MethodPost, "null", "", http.StatusForbidden},
		{"Origin host 大小写不敏感", http.MethodPost, "http://APP.LOCAL:8080", "", http.StatusOK},
		{"Origin 端口不同视为跨源", http.MethodPost, "http://app.local:9000", "", http.StatusForbidden},
		{"Origin 无端口而 Host 有端口视为跨源", http.MethodPost, "http://app.local", "", http.StatusForbidden},
		{"Origin 不是合法 URL 拒绝", http.MethodPost, "app.local:8080", "", http.StatusForbidden},
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := sameOriginMiddleware(next)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(tt.method, "/api/v1/auth/login", nil)
			r.Host = "app.local:8080"
			if tt.origin != "" {
				r.Header.Set("Origin", tt.origin)
			}
			if tt.secFetchSite != "" {
				r.Header.Set("Sec-Fetch-Site", tt.secFetchSite)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tt.want {
				t.Fatalf("status = %d, want %d (body %s)", w.Code, tt.want, w.Body.String())
			}
			if tt.want == http.StatusForbidden {
				var e Error
				if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil || e.Code != "cross_origin" {
					t.Fatalf("403 应带统一 Error 结构且 code=cross_origin，得到 %s", w.Body.String())
				}
			}
		})
	}
}
