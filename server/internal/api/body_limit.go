package api

import (
	"errors"
	"net/http"
)

// maxRequestBodyBytes /api 请求体全局上限（#191）：入站 API 全部是 JSON（导入是 JSON 数组，
// 4 MB 足够上万行），文件本体走对象存储预签名 URL、不经 /api，因此不给任何接口开例外。
const maxRequestBodyBytes = 4 << 20

// bodyLimitMiddleware 请求体上限，挂在 /api 最外层：声明的 Content-Length 超限直接 413，
// 一个字节都不读；未声明长度（分块）时用 MaxBytesReader 包住 Body，读到上限即断开，
// 服务端永远不会把超限内容整包读入内存。
func bodyLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > maxRequestBodyBytes {
			writePayloadTooLarge(w)
			return
		}
		if r.Body != nil && r.Body != http.NoBody {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}

func writePayloadTooLarge(w http.ResponseWriter) {
	writeJSON(w, http.StatusRequestEntityTooLarge, Error{Code: "payload_too_large", Message: "请求内容超过 4 MB 上限"})
}

// isMaxBytesError 判断一个（可能被契约校验器包装过的）错误是否源自请求体超限。
func isMaxBytesError(err error) bool {
	var mbe *http.MaxBytesError
	return errors.As(err, &mbe)
}
