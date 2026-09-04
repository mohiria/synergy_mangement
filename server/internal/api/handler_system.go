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
	if _, ok := s.loadSystemUser(w, r, userId); !ok {
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

// loadSystemUser 取目标用户，不存在则 404。
func (s *Server) loadSystemUser(w http.ResponseWriter, r *http.Request, userId int64) (store.User, bool) {
	u, err := s.q.GetUserByID(r.Context(), userId)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "not_found", Message: "用户不存在"})
		} else {
			writeInternalError(w, r, err)
		}
		return store.User{}, false
	}
	return u, true
}

// ResetSystemUserPassword 管理员重置密码（#205）：新密码按共用规则；该用户全部会话失效并置「须改密码」。
func (s *Server) ResetSystemUserPassword(w http.ResponseWriter, r *http.Request, userId int64) {
	if !requireSystemAdmin(w, r) {
		return
	}
	var req ResetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	if _, ok := s.loadSystemUser(w, r, userId); !ok {
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
	u, err := s.q.ResetUserPassword(r.Context(), store.ResetUserPasswordParams{ID: userId, PasswordHash: hash})
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	if _, err := s.q.DeleteUserSessions(r.Context(), userId); err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toSystemUser(u))
}

// UpdateSystemUserProfile 管理员改显示名与邮箱（#205）。
func (s *Server) UpdateSystemUserProfile(w http.ResponseWriter, r *http.Request, userId int64) {
	if !requireSystemAdmin(w, r) {
		return
	}
	var req UpdateUserProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	if _, ok := s.loadSystemUser(w, r, userId); !ok {
		return
	}
	u, ok := s.updateUserProfile(w, r, userId, req.DisplayName, req.Email)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toSystemUser(u))
}

// updateUserProfile 显示名与邮箱的共用写路径（#205 管理员改、#207 本人改）：规则在 domain，
// 重复邮箱由唯一索引兜底映射 409 email_taken。
func (s *Server) updateUserProfile(w http.ResponseWriter, r *http.Request, userId int64, displayName, email string) (store.User, bool) {
	display := strings.TrimSpace(displayName)
	if err := domain.ValidateDisplayName(display); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_display_name", Message: err.Error()})
		return store.User{}, false
	}
	if err := domain.ValidateEmail(email); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_email", Message: err.Error()})
		return store.User{}, false
	}
	u, err := s.q.UpdateUserProfile(r.Context(), store.UpdateUserProfileParams{ID: userId, DisplayName: display, Email: domain.NormalizeEmail(email)})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeJSON(w, http.StatusConflict, Error{Code: "email_taken", Message: domain.ErrEmailTaken.Error()})
			return store.User{}, false
		}
		writeInternalError(w, r, err)
		return store.User{}, false
	}
	return u, true
}

// SetSystemUserAdmin 设／撤系统管理员（#205）：不能撤销自己。
func (s *Server) SetSystemUserAdmin(w http.ResponseWriter, r *http.Request, userId int64) {
	if !requireSystemAdmin(w, r) {
		return
	}
	var req SetSystemAdminRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	if err := domain.CanRevokeSystemAdmin(currentUser(r).ID, userId, req.IsSystemAdmin); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "cannot_revoke_own_admin", Message: err.Error()})
		return
	}
	if _, ok := s.loadSystemUser(w, r, userId); !ok {
		return
	}
	u, err := s.q.SetUserSystemAdmin(r.Context(), store.SetUserSystemAdminParams{ID: userId, IsSystemAdmin: req.IsSystemAdmin})
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toSystemUser(u))
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
	writeJSON(w, http.StatusCreated, toSystemUser(u))
}

// ListSystemAuditLogs 系统级操作审计（#206）：只列 project 作用域为空的记录，列与项目审计一致。
func (s *Server) ListSystemAuditLogs(w http.ResponseWriter, r *http.Request, params ListSystemAuditLogsParams) {
	if !requireSystemAdmin(w, r) {
		return
	}
	limit := int32(100)
	if params.Limit != nil {
		limit = int32(*params.Limit)
	}
	rows, err := s.q.ListSystemAuditLogs(r.Context(), limit)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	out := make([]AuditLog, 0, len(rows))
	for _, a := range rows {
		item := AuditLog{
			Id: a.ID, Action: a.Action, Method: a.Method, Route: a.Route,
			ObjectType: optString(a.ObjectType), ActorName: fromPgText(a.ActorName), OccurredAt: a.CreatedAt.Time,
		}
		if a.ObjectID.Valid {
			item.ObjectId = &a.ObjectID.Int64
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, out)
}

// UpdateMyProfile 个人中心改显示名与邮箱（#207）：与管理员改资料共用写路径；本人改资料不进审计
// （/me 不在 /system 下，写路径装饰器自然不记）。
func (s *Server) UpdateMyProfile(w http.ResponseWriter, r *http.Request) {
	var req UpdateUserProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	u, ok := s.updateUserProfile(w, r, currentUser(r).ID, req.DisplayName, req.Email)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toCurrentUser(u))
}
