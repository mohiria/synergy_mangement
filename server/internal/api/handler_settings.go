package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

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
	b := Branding{
		SystemName: st.SystemName, Subtitle: st.Subtitle, LoginHint: st.LoginHint,
		// #214 起按邮件通道是否已配置判定。
		CanRecoverPassword: false,
	}
	if st.LogoKey != "" {
		v := int(st.LogoVersion)
		b.LogoVersion = &v
	}
	writeJSON(w, http.StatusOK, b)
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
	out := SystemSettings{
		SystemName: st.SystemName, Subtitle: st.Subtitle, LoginHint: st.LoginHint, BaseUrl: st.BaseUrl,
		UpdatedAt: st.UpdatedAt.Time,
	}
	if st.LogoKey != "" {
		v := int(st.LogoVersion)
		out.LogoVersion = &v
	}
	return out
}

// UploadSystemLogo 上传 logo（#211）：类型按内容探测（不信文件名），规则在 domain；
// 存对象存储 system/logo/v{N}，版本号自增；旧对象删除。
func (s *Server) UploadSystemLogo(w http.ResponseWriter, r *http.Request) {
	if !requireSystemAdmin(w, r) {
		return
	}
	var req UploadLogoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	data, err := base64.StdEncoding.DecodeString(req.DataBase64)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_logo", Message: "文件内容不是合法的 base64"})
		return
	}
	detected := http.DetectContentType(data)
	if err := domain.ValidateLogo(detected, int64(len(data))); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_logo", Message: err.Error()})
		return
	}
	prev, err := s.q.GetSystemSettings(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	key := fmt.Sprintf("system/logo/v%d", prev.LogoVersion+1)
	if err := s.files.Put(r.Context(), key, bytes.NewReader(data), int64(len(data))); err != nil {
		writeInternalError(w, r, err)
		return
	}
	st, err := s.q.SetSystemLogo(r.Context(), store.SetSystemLogoParams{LogoKey: key, LogoContentType: detected})
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	if prev.LogoKey != "" && prev.LogoKey != key {
		s.removeObject(r.Context(), prev.LogoKey)
	}
	writeJSON(w, http.StatusOK, toSystemSettings(st))
}

// DeleteSystemLogo 删除 logo 恢复默认首字（#211）。
func (s *Server) DeleteSystemLogo(w http.ResponseWriter, r *http.Request) {
	if !requireSystemAdmin(w, r) {
		return
	}
	prev, err := s.q.GetSystemSettings(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	st, err := s.q.SetSystemLogo(r.Context(), store.SetSystemLogoParams{LogoKey: "", LogoContentType: ""})
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	if prev.LogoKey != "" {
		s.removeObject(r.Context(), prev.LogoKey)
	}
	writeJSON(w, http.StatusOK, toSystemSettings(st))
}

// GetBrandingLogo 出图（免登录，#211）：经后端流式读取，不给对象存储直链（兼容 MinIO 不对外暴露）；
// URL 带版本参数，配合 ETag 与长缓存，换图后浏览器能拿到新图。
func (s *Server) GetBrandingLogo(w http.ResponseWriter, r *http.Request, _ GetBrandingLogoParams) {
	st, err := s.q.GetSystemSettings(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	if st.LogoKey == "" {
		writeJSON(w, http.StatusNotFound, Error{Code: "not_found", Message: "未设置 logo"})
		return
	}
	etag := `"logo-v` + strconv.FormatInt(int64(st.LogoVersion), 10) + `"`
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	rc, err := s.files.Get(r.Context(), st.LogoKey)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", st.LogoContentType)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}
