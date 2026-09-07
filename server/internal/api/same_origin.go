package api

import (
	"net/http"
	"net/url"
	"strings"
)

// sameOriginMiddleware 写请求同源校验（#192）：cookie SameSite=Lax 之外的第二道 CSRF 防线，
// 挂在 /api 最外层、认证之前，登录接口同样覆盖。
//
//   - 带 Origin 时其 host 必须与请求 Host 一致（IP 部署无固定域名，只和 Host 比对，不写允许列表）；
//   - 缺 Origin 放行：浏览器跨站写请求必带 Origin，缺失只来自非浏览器客户端，拿不到受害者 cookie；
//     不回退查 Referer；
//   - Sec-Fetch-Site 为 cross-site 时直接拒绝，优先级高于 Origin；
//   - GET／HEAD／OPTIONS 不校验。
//
// 部署前提：Caddy reverse_proxy 与 Vite 代理（changeOrigin: false）都透传浏览器发来的 Host，
// 否则同源请求也会因 Host 被改写而被挡。
func sameOriginMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !crossOrigin(r) {
			next.ServeHTTP(w, r)
			return
		}
		writeJSON(w, http.StatusForbidden, Error{Code: "cross_origin", Message: "请求来源与本站不一致，已拒绝"})
	})
}

// crossOrigin 判定一个请求是否应按跨源写请求拒绝。
func crossOrigin(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")), "cross-site") {
		return true
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" {
		// 含 "null"（沙箱或跨站重定向产生的不透明来源）与无法解析的值，一律视为跨源。
		return true
	}
	return !strings.EqualFold(u.Host, r.Host)
}
