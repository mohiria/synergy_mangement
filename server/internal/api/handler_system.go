package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

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
		resp = append(resp, toSystemUser(store.User{
			ID: u.ID, Username: u.Username, DisplayName: u.DisplayName, Email: u.Email,
			IsSystemAdmin: u.IsSystemAdmin, CreatedAt: u.CreatedAt,
			MustChangePassword: u.MustChangePassword, DisabledAt: u.DisabledAt,
		}))
	}
	writeJSON(w, http.StatusOK, resp)
}

func toSystemUser(u store.User) SystemUser {
	return SystemUser{
		Id: u.ID, Username: u.Username, DisplayName: u.DisplayName, Email: u.Email,
		IsSystemAdmin: u.IsSystemAdmin, CreatedAt: u.CreatedAt.Time,
		MustChangePassword: optBool(u.MustChangePassword), Disabled: optBool(u.DisabledAt.Valid),
	}
}

// DisableSystemUser 停用用户（#204）：不能停用自己；停用后其全部会话立即吊销。
func (s *Server) DisableSystemUser(w http.ResponseWriter, r *http.Request, userId int64) {
	if !requireSystemAdmin(w, r) {
		return
	}
	if err := domain.CanDisableUser(currentUser(r).ID, userId); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "cannot_disable_self", Message: err.Error()})
		return
	}
	s.setUserDisabled(w, r, userId, pgtype.Timestamptz{Time: s.now(), Valid: true})
}

// EnableSystemUser 启用用户（#204）。
func (s *Server) EnableSystemUser(w http.ResponseWriter, r *http.Request, userId int64) {
	if !requireSystemAdmin(w, r) {
		return
	}
	s.setUserDisabled(w, r, userId, pgtype.Timestamptz{})
}

func (s *Server) setUserDisabled(w http.ResponseWriter, r *http.Request, userId int64, at pgtype.Timestamptz) {
	if _, err := s.q.GetUserByID(r.Context(), userId); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "not_found", Message: "用户不存在"})
			return
		}
		writeInternalError(w, r, err)
		return
	}
	u, err := s.q.SetUserDisabledAt(r.Context(), store.SetUserDisabledAtParams{ID: userId, DisabledAt: at})
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	if at.Valid {
		if _, err := s.q.DeleteUserSessions(r.Context(), userId); err != nil {
			writeInternalError(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, toSystemUser(u))
}

var _ = time.Now

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
	writeJSON(w, http.StatusCreated, toSystemUser(u))
}
