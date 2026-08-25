package api

import (
	"encoding/json"
	"net/http"
)

// Server 实现生成的 ServerInterface。handler 保持薄层：
// 解析请求、调 domain/store、写响应；业务规则一律在 internal/domain。
type Server struct{}

var _ ServerInterface = (*Server)(nil)

func NewServer() *Server {
	return &Server{}
}

func (s *Server) GetHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, Health{Status: Ok})
}

func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	notImplemented(w)
}

func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
	notImplemented(w)
}

func (s *Server) GetCurrentUser(w http.ResponseWriter, r *http.Request) {
	notImplemented(w)
}

func (s *Server) ListProjects(w http.ResponseWriter, r *http.Request) {
	notImplemented(w)
}

func (s *Server) CreateProject(w http.ResponseWriter, r *http.Request) {
	notImplemented(w)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func notImplemented(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotImplemented, Error{
		Code:    "not_implemented",
		Message: "接口尚未实现",
	})
}
