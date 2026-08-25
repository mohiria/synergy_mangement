package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// Server 实现生成的 ServerInterface。handler 保持薄层：
// 解析请求、调 domain/store、写响应；业务规则一律在 internal/domain。
type Server struct {
	q        *store.Queries
	throttle *domain.LoginThrottle
	now      func() time.Time
}

var _ ServerInterface = (*Server)(nil)

func NewServer(q *store.Queries) *Server {
	return &Server{q: q, throttle: domain.NewLoginThrottle(), now: time.Now}
}

// NewHandler 组装路由与会话中间件，main 与集成测试共用同一套装配。
func NewHandler(q *store.Queries, baseURL string) http.Handler {
	s := NewServer(q)
	return HandlerWithOptions(s, StdHTTPServerOptions{
		BaseURL:     baseURL,
		BaseRouter:  http.NewServeMux(),
		Middlewares: []MiddlewareFunc{s.sessionMiddleware},
	})
}

const sessionCookieName = "session"

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxToken
)

// 无需会话即可访问的路径（按路由后缀匹配，其余一律要求有效会话）。
var publicSuffixes = []string{"/healthz", "/auth/login"}

func (s *Server) sessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, p := range publicSuffixes {
			if strings.HasSuffix(r.URL.Path, p) {
				next.ServeHTTP(w, r)
				return
			}
		}
		c, err := r.Cookie(sessionCookieName)
		if err != nil || c.Value == "" {
			writeUnauthorized(w)
			return
		}
		ctx := r.Context()
		sess, err := s.q.GetSession(ctx, c.Value)
		if err != nil {
			clearSessionCookie(w)
			writeUnauthorized(w)
			return
		}
		user, err := s.q.GetUserByID(ctx, sess.UserID)
		if err != nil {
			clearSessionCookie(w)
			writeUnauthorized(w)
			return
		}
		if newExpiry, renew := domain.SessionRenewal(s.now(), sess.ExpiresAt.Time); renew {
			err := s.q.UpdateSessionExpiry(ctx, store.UpdateSessionExpiryParams{
				Token:     sess.Token,
				ExpiresAt: pgtype.Timestamptz{Time: newExpiry, Valid: true},
			})
			if err == nil {
				setSessionCookie(w, sess.Token)
			}
		}
		ctx = context.WithValue(ctx, ctxUser, user)
		ctx = context.WithValue(ctx, ctxToken, sess.Token)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func currentUser(r *http.Request) store.User {
	u, _ := r.Context().Value(ctxUser).(store.User)
	return u
}

func (s *Server) GetHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, Health{Status: Ok})
}

func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" || req.Password == "" {
		writeJSON(w, http.StatusUnauthorized, Error{Code: "invalid_credentials", Message: "用户名或口令错误"})
		return
	}
	now := s.now()
	if !s.throttle.Allow(req.Username, now) {
		writeJSON(w, http.StatusTooManyRequests, Error{Code: "rate_limited", Message: "登录失败次数过多，请稍后再试"})
		return
	}
	user, err := s.q.GetUserByUsername(r.Context(), req.Username)
	if err != nil || !domain.VerifyPassword(user.PasswordHash, req.Password) {
		s.throttle.RecordFailure(req.Username, now)
		writeJSON(w, http.StatusUnauthorized, Error{Code: "invalid_credentials", Message: "用户名或口令错误"})
		return
	}
	s.throttle.RecordSuccess(req.Username)
	token, err := domain.NewSessionToken()
	if err != nil {
		writeInternalError(w)
		return
	}
	err = s.q.CreateSession(r.Context(), store.CreateSessionParams{
		Token:     token,
		UserID:    user.ID,
		ExpiresAt: pgtype.Timestamptz{Time: now.Add(domain.SessionTTL), Valid: true},
	})
	if err != nil {
		writeInternalError(w)
		return
	}
	setSessionCookie(w, token)
	writeJSON(w, http.StatusOK, toCurrentUser(user))
}

func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
	token, _ := r.Context().Value(ctxToken).(string)
	if err := s.q.DeleteSession(r.Context(), token); err != nil {
		writeInternalError(w)
		return
	}
	clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) GetCurrentUser(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, toCurrentUser(currentUser(r)))
}

func (s *Server) ListProjects(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListProjects(r.Context())
	if err != nil {
		writeInternalError(w)
		return
	}
	resp := make([]Project, 0, len(rows))
	for _, p := range rows {
		resp = append(resp, toProject(store.Project{
			ID:               p.ID,
			Name:             p.Name,
			CreatedBy:        p.CreatedBy,
			CreatedAt:        p.CreatedAt,
			OwnerID:          p.OwnerID,
			Status:           p.Status,
			Stage:            p.Stage,
			PlannedStartDate: p.PlannedStartDate,
			PlannedEndDate:   p.PlannedEndDate,
		}, p.OwnerName))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) CreateProject(w http.ResponseWriter, r *http.Request) {
	var req CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	name := strings.TrimSpace(req.Name)
	stage := trimStage(req.Stage)
	if !s.validateProjectFields(w, name, stage, domain.DefaultProjectStatus, req.PlannedStartDate, req.PlannedEndDate) {
		return
	}
	owner, err := s.q.GetUserByID(r.Context(), req.OwnerId)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_owner", Message: "项目负责人不存在"})
		return
	}
	p, err := s.q.CreateProject(r.Context(), store.CreateProjectParams{
		Name:             name,
		CreatedBy:        currentUser(r).ID,
		OwnerID:          owner.ID,
		Status:           domain.DefaultProjectStatus,
		Stage:            toPgText(stage),
		PlannedStartDate: toPgDate(req.PlannedStartDate),
		PlannedEndDate:   toPgDate(req.PlannedEndDate),
	})
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, toProject(p, owner.DisplayName))
}

func (s *Server) UpdateProject(w http.ResponseWriter, r *http.Request, projectId int64) {
	var req UpdateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	name := strings.TrimSpace(req.Name)
	stage := trimStage(req.Stage)
	if !s.validateProjectFields(w, name, stage, string(req.Status), req.PlannedStartDate, req.PlannedEndDate) {
		return
	}
	owner, err := s.q.GetUserByID(r.Context(), req.OwnerId)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_owner", Message: "项目负责人不存在"})
		return
	}
	p, err := s.q.UpdateProject(r.Context(), store.UpdateProjectParams{
		ID:               projectId,
		Name:             name,
		OwnerID:          owner.ID,
		Status:           string(req.Status),
		Stage:            toPgText(stage),
		PlannedStartDate: toPgDate(req.PlannedStartDate),
		PlannedEndDate:   toPgDate(req.PlannedEndDate),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "not_found", Message: "项目不存在"})
			return
		}
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, toProject(p, owner.DisplayName))
}

func (s *Server) ListUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListUsers(r.Context())
	if err != nil {
		writeInternalError(w)
		return
	}
	resp := make([]UserSummary, 0, len(rows))
	for _, u := range rows {
		resp = append(resp, UserSummary{Id: u.ID, Username: u.Username, DisplayName: u.DisplayName})
	}
	writeJSON(w, http.StatusOK, resp)
}

// validateProjectFields 统一走 domain 校验；未通过时已写出 422 响应并返回 false。
func (s *Server) validateProjectFields(w http.ResponseWriter, name, stage, status string, start, end *openapi_types.Date) bool {
	if err := domain.ValidateProjectName(name); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_project_name", Message: err.Error()})
		return false
	}
	if err := domain.ValidateProjectStatus(status); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_project_status", Message: err.Error()})
		return false
	}
	if err := domain.ValidateProjectStage(stage); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_project_stage", Message: err.Error()})
		return false
	}
	if err := domain.ValidateProjectPlan(toTimePtr(start), toTimePtr(end)); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_project_plan", Message: err.Error()})
		return false
	}
	return true
}

func toCurrentUser(u store.User) CurrentUser {
	return CurrentUser{Id: u.ID, Username: u.Username, DisplayName: u.DisplayName}
}

func toProject(p store.Project, ownerName string) Project {
	return Project{
		Id:               p.ID,
		Name:             p.Name,
		OwnerId:          p.OwnerID,
		OwnerName:        ownerName,
		Status:           ProjectStatus(p.Status),
		Stage:            fromPgText(p.Stage),
		PlannedStartDate: fromPgDate(p.PlannedStartDate),
		PlannedEndDate:   fromPgDate(p.PlannedEndDate),
		CreatedAt:        p.CreatedAt.Time,
	}
}

func trimStage(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

func toPgText(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: s != ""}
}

func fromPgText(t pgtype.Text) *string {
	if !t.Valid || t.String == "" {
		return nil
	}
	return &t.String
}

func toPgDate(d *openapi_types.Date) pgtype.Date {
	if d == nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: d.Time, Valid: true}
}

func fromPgDate(d pgtype.Date) *openapi_types.Date {
	if !d.Valid {
		return nil
	}
	return &openapi_types.Date{Time: d.Time}
}

func toTimePtr(d *openapi_types.Date) *time.Time {
	if d == nil {
		return nil
	}
	return &d.Time
}

func setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(domain.SessionTTL / time.Second),
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeUnauthorized(w http.ResponseWriter) {
	writeJSON(w, http.StatusUnauthorized, Error{Code: "unauthorized", Message: "未登录或会话已过期"})
}

func writeInternalError(w http.ResponseWriter) {
	writeJSON(w, http.StatusInternalServerError, Error{Code: "internal_error", Message: "服务器内部错误"})
}
