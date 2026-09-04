package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// requireSystemAdmin 系统设置准入（#201）：非系统管理员一律 403，规则在 domain。
func requireSystemAdmin(w http.ResponseWriter, r *http.Request) bool {
	if err := domain.CanAccessSystemSettings(currentUser(r).IsSystemAdmin); err != nil {
		writeJSON(w, http.StatusForbidden, Error{Code: "system_admin_required", Message: err.Error()})
		return false
	}
	return true
}

// ListSystemUsers 系统设置 → 用户管理的只读列表（#201）。
func (s *Server) ListSystemUsers(w http.ResponseWriter, r *http.Request) {
	if !requireSystemAdmin(w, r) {
		return
	}
	rows, err := s.q.ListSystemUsers(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	resp := make([]SystemUser, 0, len(rows))
	for _, u := range rows {
		resp = append(resp, SystemUser{
			Id: u.ID, Username: u.Username, DisplayName: u.DisplayName, Email: u.Email,
			IsSystemAdmin: u.IsSystemAdmin, CreatedAt: u.CreatedAt.Time,
			MustChangePassword: &u.MustChangePassword,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// CreateSystemUser 管理员建号（#203）：字段规则在 domain；初始密码由管理员设定，
// 新用户带「须改密码」标记，首次登录强制改密。用户名／邮箱重复由唯一索引兜底映射为 409。
func (s *Server) CreateSystemUser(w http.ResponseWriter, r *http.Request) {
	if !requireSystemAdmin(w, r) {
		return
	}
	var req CreateSystemUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	username := strings.TrimSpace(req.Username)
	display := strings.TrimSpace(req.DisplayName)
	if err := domain.ValidateUsername(username); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_username", Message: err.Error()})
		return
	}
	if err := domain.ValidateDisplayName(display); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_display_name", Message: err.Error()})
		return
	}
	if err := domain.ValidateEmail(req.Email); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_email", Message: err.Error()})
		return
	}
	if err := domain.ValidatePasswordLength(req.Password); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_password", Message: err.Error()})
		return
	}
	hash, err := domain.HashPassword(req.Password)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	u, err := s.q.CreateUser(r.Context(), store.CreateUserParams{
		Username: username, DisplayName: display, PasswordHash: hash,
		Email: domain.NormalizeEmail(req.Email), MustChangePassword: true,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			if strings.Contains(pgErr.ConstraintName, "email") {
				writeJSON(w, http.StatusConflict, Error{Code: "email_taken", Message: domain.ErrEmailTaken.Error()})
			} else {
				writeJSON(w, http.StatusConflict, Error{Code: "username_taken", Message: domain.ErrUsernameTaken.Error()})
			}
			return
		}
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, SystemUser{
		Id: u.ID, Username: u.Username, DisplayName: u.DisplayName, Email: u.Email,
		IsSystemAdmin: u.IsSystemAdmin, CreatedAt: u.CreatedAt.Time, MustChangePassword: &u.MustChangePassword,
	})
}
