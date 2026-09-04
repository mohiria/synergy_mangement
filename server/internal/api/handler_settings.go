package api

import (
	"encoding/json"
	"net/http"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// GetBranding 品牌信息（免登录，#210）：登录页在拿到会话前就要显示，因此只返回品牌字段。
func (s *Server) GetBranding(w http.ResponseWriter, r *http.Request) {
	st, err := s.q.GetSystemSettings(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, Branding{
		SystemName: st.SystemName, Subtitle: st.Subtitle, LoginHint: st.LoginHint,
		// #214 起按邮件通道是否已配置判定。
		CanRecoverPassword: false,
	})
}

// GetSystemSettings 系统设置 → 基本信息（仅系统管理员，#210）。
func (s *Server) GetSystemSettings(w http.ResponseWriter, r *http.Request) {
	if !requireSystemAdmin(w, r) {
		return
	}
	st, err := s.q.GetSystemSettings(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toSystemSettings(st))
}

// UpdateSystemSettings 修改基本信息（仅系统管理员，#210）：规则在 domain，写操作由装饰器进系统级审计。
func (s *Server) UpdateSystemSettings(w http.ResponseWriter, r *http.Request) {
	if !requireSystemAdmin(w, r) {
		return
	}
	var req SystemSettingsInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	in, err := domain.ValidateSystemSettings(domain.SystemSettingsInput{
		SystemName: req.SystemName, Subtitle: req.Subtitle, LoginHint: req.LoginHint, BaseURL: req.BaseUrl,
	})
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_system_settings", Message: err.Error()})
		return
	}
	st, err := s.q.UpdateSystemSettings(r.Context(), store.UpdateSystemSettingsParams{
		SystemName: in.SystemName, Subtitle: in.Subtitle, LoginHint: in.LoginHint, BaseUrl: in.BaseURL,
	})
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toSystemSettings(st))
}

func toSystemSettings(st store.SystemSetting) SystemSettings {
	return SystemSettings{
		SystemName: st.SystemName, Subtitle: st.Subtitle, LoginHint: st.LoginHint, BaseUrl: st.BaseUrl,
		UpdatedAt: st.UpdatedAt.Time,
	}
}
