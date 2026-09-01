package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"synergy/server/internal/domain"
	"synergy/server/internal/filestore"
	"synergy/server/internal/store"
)

// Server 实现生成的 ServerInterface。handler 保持薄层：
// 解析请求、调 domain/store、写响应；业务规则一律在 internal/domain。
type Server struct {
	db       *pgxpool.Pool
	q        *store.Queries
	files    filestore.Store
	throttle *domain.LoginThrottle
	now      func() time.Time
}

var _ ServerInterface = (*Server)(nil)

// NewServer 持有连接池以支持多语句事务（如 O／KR 批量创建），常规查询走 sqlc Queries。
func NewServer(db *pgxpool.Pool, files filestore.Store) *Server {
	return &Server{db: db, q: store.New(db), files: files, throttle: domain.NewLoginThrottle(), now: time.Now}
}

// NewHandler 组装路由与会话中间件，main 与集成测试共用同一套装配。
func NewHandler(db *pgxpool.Pool, baseURL string, files filestore.Store) http.Handler {
	return NewHandlerFromServer(NewServer(db, files), baseURL)
}

// NewHandlerFromServer 由既有 Server 组装路由；main 需要同一个 Server 同时跑卡点 ticker。
func NewHandlerFromServer(s *Server, baseURL string) http.Handler {
	return HandlerWithOptions(s, StdHTTPServerOptions{
		BaseURL:    baseURL,
		BaseRouter: http.NewServeMux(),
		// 切片里靠前的先包住 handler，也就是越靠前越内层：写路径装饰器要放最前，
		// 才能在会话中间件之后运行、拿得到当前用户。
		Middlewares: []MiddlewareFunc{s.writePathMiddleware, requestIDMiddleware, s.sessionMiddleware, requestValidator()},
	})
}

// requestValidator 用契约本身兜底校验请求：enum、required、长度与格式不再依赖各 handler 手工判定。
// 鉴权仍由 sessionMiddleware 负责，这里只放行安全方案（避免与会话校验重复）。
func requestValidator() MiddlewareFunc {
	swagger, err := GetSwagger()
	if err != nil {
		panic("加载嵌入契约失败: " + err.Error())
	}
	return nethttpmiddleware.OapiRequestValidatorWithOptions(swagger, &nethttpmiddleware.Options{
		SilenceServersWarning: true,
		Options: openapi3filter.Options{
			AuthenticationFunc: func(context.Context, *openapi3filter.AuthenticationInput) error { return nil },
		},
		ErrorHandlerWithOpts: func(_ context.Context, err error, w http.ResponseWriter, _ *http.Request, opts nethttpmiddleware.ErrorHandlerOpts) {
			switch opts.StatusCode {
			case http.StatusNotFound:
				writeJSON(w, http.StatusNotFound, Error{Code: "not_found", Message: "资源不存在"})
			case http.StatusMethodNotAllowed:
				w.WriteHeader(http.StatusMethodNotAllowed)
			default:
				// 契约校验不通过统一按 422 回，与各 handler 手工校验同口径。
				writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: err.Error()})
			}
		},
	})
}

// requestIDMiddleware 给每个请求分配 id，回写响应头并放进 context：500 日志与客户端看到的
// 响应可以互相对上，是本地部署下唯一的排障线索。
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get("X-Request-Id"))
		if id == "" {
			var buf [12]byte
			if _, err := rand.Read(buf[:]); err != nil {
				id = strconv.FormatInt(time.Now().UnixNano(), 36)
			} else {
				id = hex.EncodeToString(buf[:])
			}
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxRequestID, id)))
	})
}

// requestIDFrom 取当前请求 id；无中间件时返回 "-"。
func requestIDFrom(ctx context.Context) string {
	if id, ok := ctx.Value(ctxRequestID).(string); ok && id != "" {
		return id
	}
	return "-"
}

const sessionCookieName = "session"

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxToken
	ctxRequestID
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
	ip := clientIP(r)
	if !s.throttle.Allow(req.Username, ip, now) {
		writeJSON(w, http.StatusTooManyRequests, Error{Code: "rate_limited", Message: "登录失败次数过多，请稍后再试"})
		return
	}
	user, err := s.q.GetUserByUsername(r.Context(), req.Username)
	if err != nil || !domain.VerifyPassword(user.PasswordHash, req.Password) {
		s.throttle.RecordFailure(req.Username, ip, now)
		writeJSON(w, http.StatusUnauthorized, Error{Code: "invalid_credentials", Message: "用户名或口令错误"})
		return
	}
	s.throttle.RecordSuccess(req.Username, ip)
	token, err := domain.NewSessionToken()
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	err = s.q.CreateSession(r.Context(), store.CreateSessionParams{
		Token:     token,
		UserID:    user.ID,
		ExpiresAt: pgtype.Timestamptz{Time: now.Add(domain.SessionTTL), Valid: true},
	})
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	setSessionCookie(w, token)
	writeJSON(w, http.StatusOK, toCurrentUser(user))
}

// ChangePassword 修改本人口令（S3）：改完把本人其余会话一并吊销，
// 只保留当前会话——否则旧口令泄露后已经建立的会话仍然有效，改口令等于没改。
func (s *Server) ChangePassword(w http.ResponseWriter, r *http.Request) {
	var req ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	user := currentUser(r)
	token, _ := r.Context().Value(ctxToken).(string)
	if !domain.VerifyPassword(user.PasswordHash, req.CurrentPassword) {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_password", Message: domain.ErrPasswordWrong.Error()})
		return
	}
	if err := domain.ValidatePasswordChange(req.CurrentPassword, req.NewPassword); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_password", Message: err.Error()})
		return
	}
	hash, err := domain.HashPassword(req.NewPassword)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	if err := qtx.UpdateUserPassword(r.Context(), store.UpdateUserPasswordParams{ID: user.ID, PasswordHash: hash}); err != nil {
		writeInternalError(w, r, err)
		return
	}
	if _, err := qtx.DeleteOtherUserSessions(r.Context(), store.DeleteOtherUserSessionsParams{UserID: user.ID, Token: token}); err != nil {
		writeInternalError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
	token, _ := r.Context().Value(ctxToken).(string)
	if err := s.q.DeleteSession(r.Context(), token); err != nil {
		writeInternalError(w, r, err)
		return
	}
	clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) GetCurrentUser(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, toCurrentUser(currentUser(r)))
}

func (s *Server) ListProjects(w http.ResponseWriter, r *http.Request) {
	uid := currentUser(r).ID
	rows, err := s.q.ListProjects(r.Context(), uid)
	if err != nil {
		writeInternalError(w, r, err)
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
			Visibility:       p.Visibility,
		}, p.OwnerName, projectActor(uid, p.OwnerID, p.MyRole, p.Visibility)))
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
	uid := currentUser(r).ID
	p, err := s.q.CreateProject(r.Context(), store.CreateProjectParams{
		Name:             name,
		CreatedBy:        uid,
		OwnerID:          owner.ID,
		Status:           domain.DefaultProjectStatus,
		Stage:            toPgText(stage),
		PlannedStartDate: toPgDate(req.PlannedStartDate),
		PlannedEndDate:   toPgDate(req.PlannedEndDate),
		Role:             domain.RoleAdmin, // 创建人自动成为项目管理员成员（bootstrap）
	})
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	actor := domain.Actor{IsOwner: owner.ID == uid, Role: domain.RoleAdmin}
	writeJSON(w, http.StatusCreated, toProject(store.Project{
		ID:               p.ID,
		Name:             p.Name,
		CreatedBy:        p.CreatedBy,
		CreatedAt:        p.CreatedAt,
		OwnerID:          p.OwnerID,
		Status:           p.Status,
		Stage:            p.Stage,
		PlannedStartDate: p.PlannedStartDate,
		PlannedEndDate:   p.PlannedEndDate,
		Visibility:       p.Visibility,
	}, owner.DisplayName, actor))
}

func (s *Server) UpdateProject(w http.ResponseWriter, r *http.Request, projectId int64) {
	var req UpdateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	uid := currentUser(r).ID
	existing, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	if !domain.CanEditProject(projectActor(uid, existing.OwnerID, existing.MyRole, existing.Visibility)) {
		writeForbidden(w)
		return
	}
	name := strings.TrimSpace(req.Name)
	stage := trimStage(req.Stage)
	if !s.validateProjectFields(w, name, stage, string(req.Status), req.PlannedStartDate, req.PlannedEndDate) {
		return
	}
	if err := domain.ValidateProjectVisibility(string(req.Visibility)); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_project_visibility", Message: err.Error()})
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
		Visibility:       string(req.Visibility),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "not_found", Message: "项目不存在"})
			return
		}
		writeInternalError(w, r, err)
		return
	}
	// 派生字段按更新后的负责人重新判定（负责人可能已易主）。
	writeJSON(w, http.StatusOK, toProject(p, owner.DisplayName, projectActor(uid, p.OwnerID, existing.MyRole, p.Visibility)))
}

func (s *Server) ListProjectMembers(w http.ResponseWriter, r *http.Request, projectId int64) {
	if _, ok := s.fetchProject(w, r, projectId); !ok {
		return
	}
	rows, err := s.q.ListProjectMembers(r.Context(), projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	resp := make([]ProjectMember, 0, len(rows))
	for _, m := range rows {
		resp = append(resp, ProjectMember{
			UserId:      m.UserID,
			Username:    m.Username,
			DisplayName: m.DisplayName,
			Role:        MemberRole(m.Role),
			RoleLabel:   optString(domain.MemberRoleLabel(m.Role)),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) AddProjectMembers(w http.ResponseWriter, r *http.Request, projectId int64) {
	var req AddProjectMembersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	if !domain.CanManageMembers(projectActor(currentUser(r).ID, proj.OwnerID, proj.MyRole, proj.Visibility)) {
		writeForbidden(w)
		return
	}
	users, err := s.q.ListUsers(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	known := make([]int64, 0, len(users))
	nameOf := make(map[int64]store.ListUsersRow, len(users))
	for _, u := range users {
		known = append(known, u.ID)
		nameOf[u.ID] = u
	}
	current, err := s.q.ListProjectMembers(r.Context(), projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	existing := make([]int64, 0, len(current))
	for _, m := range current {
		existing = append(existing, m.UserID)
	}
	add, skipped, err := domain.PlanAddMembers(string(req.Role), req.UserIds, known, existing)
	if err != nil {
		code := "invalid_member_role"
		if errors.Is(err, domain.ErrNoMembersSelected) {
			code = "no_members_selected"
		}
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: code, Message: err.Error()})
		return
	}
	// 一批要么全建、要么全不建：部分写入会让「逐人结果」与库里的事实对不上。
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	added := make([]ProjectMember, 0, len(add))
	for _, id := range add {
		m, err := qtx.AddProjectMember(r.Context(), store.AddProjectMemberParams{
			ProjectID: projectId,
			UserID:    id,
			Role:      string(req.Role),
		})
		if err != nil {
			writeInternalError(w, r, err)
			return
		}
		u := nameOf[id]
		added = append(added, ProjectMember{
			UserId:      m.UserID,
			Username:    u.Username,
			DisplayName: u.DisplayName,
			Role:        MemberRole(m.Role),
			RoleLabel:   optString(domain.MemberRoleLabel(m.Role)),
		})
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w, r, err)
		return
	}
	out := make([]SkippedMember, 0, len(skipped))
	for _, sk := range skipped {
		item := SkippedMember{
			UserId:      sk.UserID,
			Reason:      SkippedMemberReason(sk.Reason),
			ReasonLabel: domain.SkipReasonLabel(sk.Reason),
		}
		if u, ok := nameOf[sk.UserID]; ok {
			item.DisplayName = optString(u.DisplayName)
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusCreated, AddProjectMembersResult{Added: added, Skipped: out})
}

func (s *Server) UpdateProjectMemberRole(w http.ResponseWriter, r *http.Request, projectId int64, userId int64) {
	var req UpdateProjectMemberRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	if !domain.CanManageMembers(projectActor(currentUser(r).ID, proj.OwnerID, proj.MyRole, proj.Visibility)) {
		writeForbidden(w)
		return
	}
	if err := domain.ValidateMemberRole(string(req.Role)); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_member_role", Message: err.Error()})
		return
	}
	m, err := s.q.UpdateProjectMemberRole(r.Context(), store.UpdateProjectMemberRoleParams{
		ProjectID: projectId,
		UserID:    userId,
		Role:      string(req.Role),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "member_not_found", Message: "该用户不是项目成员"})
			return
		}
		writeInternalError(w, r, err)
		return
	}
	user, err := s.q.GetUserByID(r.Context(), m.UserID)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ProjectMember{
		UserId:      m.UserID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Role:        MemberRole(m.Role),
		RoleLabel:   optString(domain.MemberRoleLabel(m.Role)),
	})
}

func (s *Server) RemoveProjectMember(w http.ResponseWriter, r *http.Request, projectId int64, userId int64) {
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	if !domain.CanManageMembers(projectActor(currentUser(r).ID, proj.OwnerID, proj.MyRole, proj.Visibility)) {
		writeForbidden(w)
		return
	}
	// AC-21／AC-61：仍在承担职责的人不能被移出——KR 负责人一走，待终审的完成申请
	// 就再也无人可决策。先列清待交接项，让管理员知道要先做什么。
	duties, err := s.memberDuties(r.Context(), projectId, userId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	if err := domain.RemoveMemberRule(duties); err != nil {
		writeJSON(w, http.StatusConflict, Error{
			Code:    "member_has_duties",
			Message: err.Error() + "（" + domain.MemberDutiesSummary(duties) + "）",
		})
		return
	}
	n, err := s.q.DeleteProjectMember(r.Context(), store.DeleteProjectMemberParams{
		ProjectID: projectId,
		UserID:    userId,
	})
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	if n == 0 {
		writeJSON(w, http.StatusNotFound, Error{Code: "member_not_found", Message: "该用户不是项目成员"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// memberDuties 汇总某成员在项目里仍占着的职责（AC-21、AC-61）。
func (s *Server) memberDuties(ctx context.Context, projectID, userID int64) (domain.MemberDuties, error) {
	out := domain.MemberDuties{}
	krs, err := s.q.ListKeyResultsOwnedBy(ctx, store.ListKeyResultsOwnedByParams{ProjectID: projectID, OwnerID: pgtype.Int8{Int64: userID, Valid: true}})
	if err != nil {
		return out, err
	}
	for _, k := range krs {
		out.KeyResults = append(out.KeyResults, k.Description)
	}
	tasks, err := s.q.ListTasksOwnedBy(ctx, store.ListTasksOwnedByParams{ProjectID: projectID, OwnerID: userID})
	if err != nil {
		return out, err
	}
	for _, t := range tasks {
		out.Tasks = append(out.Tasks, t.Name)
	}
	if out.Reviewers, err = s.q.ListReviewerDutiesOf(ctx, store.ListReviewerDutiesOfParams{ProjectID: projectID, UserID: userID}); err != nil {
		return out, err
	}
	if out.Receivers, err = s.q.ListReceiverDutiesOf(ctx, store.ListReceiverDutiesOfParams{ProjectID: projectID, UserID: userID}); err != nil {
		return out, err
	}
	if out.InputProviders, err = s.q.ListInputProviderDutiesOf(ctx, store.ListInputProviderDutiesOfParams{ProjectID: projectID, ProviderID: userID}); err != nil {
		return out, err
	}
	return out, nil
}

func (s *Server) GetProject(w http.ResponseWriter, r *http.Request, projectId int64) {
	row, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	writeJSON(w, http.StatusOK, toProject(store.Project{
		ID:               row.ID,
		Name:             row.Name,
		CreatedBy:        row.CreatedBy,
		CreatedAt:        row.CreatedAt,
		OwnerID:          row.OwnerID,
		Status:           row.Status,
		Stage:            row.Stage,
		PlannedStartDate: row.PlannedStartDate,
		PlannedEndDate:   row.PlannedEndDate,
		Visibility:       row.Visibility,
	}, row.OwnerName, projectActor(uid, row.OwnerID, row.MyRole, row.Visibility)))
}

func (s *Server) ListObjectives(w http.ResponseWriter, r *http.Request, projectId int64) {
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	resp, err := s.okrList(r.Context(), projectId, projectActor(uid, proj.OwnerID, proj.MyRole, proj.Visibility), uid)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) CreateOkrBatch(w http.ResponseWriter, r *http.Request, projectId int64) {
	var req CreateOkrBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	if !domain.CanEditProject(projectActor(currentUser(r).ID, proj.OwnerID, proj.MyRole, proj.Visibility)) {
		writeForbidden(w)
		return
	}
	members, err := s.q.ListProjectMembers(r.Context(), projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	roleByID := make(map[int64]string, len(members))
	for _, m := range members {
		roleByID[m.UserID] = m.Role
	}
	items := toOkrBatchItems(req.Items)
	if err := domain.ValidateOkrBatch(items, func(id int64) string { return roleByID[id] }); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_okr", Message: err.Error()})
		return
	}
	for _, item := range items {
		if item.ObjectiveID == nil {
			continue
		}
		if _, err := s.q.GetObjective(r.Context(), store.GetObjectiveParams{ID: *item.ObjectiveID, ProjectID: projectId}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_objective", Message: "所属 O 不存在"})
				return
			}
			writeInternalError(w, r, err)
			return
		}
	}
	// 整批一个事务：全部成功或全部失败（契约 createOkrBatch）。
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	for _, item := range items {
		objectiveID := int64(0)
		if item.ObjectiveID != nil {
			objectiveID = *item.ObjectiveID
		} else {
			o, err := qtx.CreateObjective(r.Context(), store.CreateObjectiveParams{
				ProjectID:   projectId,
				Title:       item.Title,
				Description: item.Description,
			})
			if err != nil {
				writeInternalError(w, r, err)
				return
			}
			objectiveID = o.ID
		}
		for _, k := range item.KeyResults {
			_, err := qtx.CreateKeyResult(r.Context(), store.CreateKeyResultParams{
				ObjectiveID: objectiveID,
				Description: k.Description,
				Metric:      k.Metric,
				OwnerID:     toPgInt8(k.OwnerID),
				StartDate:   toPgDateFromTime(k.Start),
				EndDate:     toPgDateFromTime(k.End),
			})
			if err != nil {
				writeInternalError(w, r, err)
				return
			}
			// #125「保存并通知负责人」：本次指派的 KR 负责人逐条收站内通知，本人不收；
			// 与创建同一事务，创建失败不发通知。
			if k.OwnerID != nil && *k.OwnerID != currentUser(r).ID {
				if _, err := qtx.CreateNotification(r.Context(), store.CreateNotificationParams{
					UserID:    *k.OwnerID,
					Kind:      domain.NotifyOkrAssigned,
					Content:   domain.OkrAssignedContent(k.Description),
					ProjectID: pgtype.Int8{Int64: projectId, Valid: true},
				}); err != nil {
					writeInternalError(w, r, err)
					return
				}
			}
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w, r, err)
		return
	}
	objectivesResp, err := s.okrList(r.Context(), projectId, projectActor(currentUser(r).ID, proj.OwnerID, proj.MyRole, proj.Visibility), currentUser(r).ID)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	// 已通知人数按人去重（规则在 domain），提示「已通知 N 名负责人」用。
	writeJSON(w, http.StatusCreated, CreateOkrBatchResponse{
		Objectives:    objectivesResp,
		NotifiedCount: len(domain.OkrNotifyTargets(currentUser(r).ID, items)),
	})
}

// okrList 组装 O 含下属 KR 的层级列表（按排序返回）。
// okrList 组装项目 O／KR 列表与派生字段（覆盖度、风险、任务数与编辑／删除动作标志）。
func (s *Server) okrList(ctx context.Context, projectID int64, actor domain.Actor, userID int64) ([]Objective, error) {
	objectives, err := s.q.ListObjectives(ctx, projectID)
	if err != nil {
		return nil, err
	}
	krs, err := s.q.ListKeyResultsByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	// KR 层进度数据覆盖度（AC-12）：原始事实来自任务表，聚合规则在 domain。
	progressRows, err := s.q.ListTaskProgressByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	factsByKr := make(map[int64][]domain.TaskProgressFact)
	for _, row := range progressRows {
		factsByKr[row.KeyResultID] = append(factsByKr[row.KeyResultID], domain.TaskProgressFact{
			Status:   row.Status,
			Progress: fromPgInt4(row.Progress),
		})
	}
	// KR 风险等级与一行原因（AC-05、PRD §5.7）：读时派生，事实来自 KR 下任务的
	// 卡点与日期，规则在 domain；临期阈值取项目规则设置（AC-60）。
	taskRows, err := s.q.ListProjectTasks(ctx, projectID)
	if err != nil {
		return nil, err
	}
	krByTask := make(map[int64]int64, len(taskRows))
	codeByTask := make(map[int64]string, len(taskRows))
	riskTasksByKr := make(map[int64][]domain.RiskTaskFact)
	for _, t := range taskRows {
		krByTask[t.ID] = t.KeyResultID
		codeByTask[t.ID] = domain.TaskCode(int(t.ObjectiveCodeSeq), int(t.KrCodeSeq), int(t.CodeSeq))
		riskTasksByKr[t.KeyResultID] = append(riskTasksByKr[t.KeyResultID], domain.RiskTaskFact{
			ID:      t.ID,
			Name:    t.Name,
			Status:  t.Status,
			EndDate: pgDateAsTime(t.EndDate),
		})
	}
	blockers, err := s.projectBlockers(ctx, projectID)
	if err != nil {
		return nil, err
	}
	blockersByKr := make(map[int64][]domain.Blocker)
	for _, b := range blockers {
		krID := krByTask[b.TaskID]
		blockersByKr[krID] = append(blockersByKr[krID], b)
	}
	settings, err := s.projectSettings(ctx, projectID)
	if err != nil {
		return nil, err
	}
	// 未就绪摘要（#150，模块 PRD §5.2）：输入边就绪事实按目标任务的 KR 归组，计数规则在 domain。
	readinessRows, err := s.q.ListInputReadinessByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	inputFactsByKr := make(map[int64][]domain.KrInputFact)
	for _, row := range readinessRows {
		// 裁决 #163：任务来源按来源任务已完成判定，成员来源按输入请求已提供判定。
		ready := domain.EdgeReady(row.SourceTaskStatus.String)
		if row.InputRequestState.Valid {
			ready = domain.MemberEdgeReady(row.InputRequestState.String)
		}
		inputFactsByKr[row.KeyResultID] = append(inputFactsByKr[row.KeyResultID], domain.KrInputFact{
			TargetStatus: row.TargetStatus,
			Ready:        ready,
		})
	}
	// KR 下任务数（含已完成与已关闭）：OKR 表「任务」列与删除守卫同源（AC-65）。
	taskCounts, err := s.taskCountByKeyResult(ctx, projectID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	krCountByObjective := make(map[int64]int, len(objectives))
	for _, k := range krs {
		krCountByObjective[k.ObjectiveID]++
	}
	byObjective := make(map[int64][]KeyResult, len(objectives))
	for _, k := range krs {
		summary := domain.ProgressCoverage(factsByKr[k.ID])
		risk := domain.DeriveKrRisk(now, settings.DueSoonDays, riskTasksByKr[k.ID], blockersByKr[k.ID])
		byObjective[k.ObjectiveID] = append(byObjective[k.ObjectiveID], KeyResult{
			Id:             k.ID,
			ObjectiveId:    k.ObjectiveID,
			Code:           domain.KeyResultCode(int(k.ObjectiveCodeSeq), int(k.CodeSeq)),
			RiskLevelLabel: domain.RiskLevelLabel(risk.Level),
			Description:    k.Description,
			Metric:         optString(k.Metric),
			OwnerId:        fromPgInt8(k.OwnerID),
			OwnerName:      fromPgText(k.OwnerName),
			StartDate:      fromPgDate(k.StartDate),
			EndDate:        fromPgDate(k.EndDate),
			RiskLevel:      RiskLevel(risk.Level),
			SortOrder:      int(k.SortOrder),
			ProgressSummary: &ProgressSummary{
				TotalTasks:      summary.TotalTasks,
				FilledTasks:     summary.FilledTasks,
				AverageProgress: summary.AverageProgress,
			},
			RiskNote: optString(risk.Note),
			OpenBlockerCount: func() *int {
				n := len(blockersByKr[k.ID])
				if n == 0 {
					return nil
				}
				return &n
			}(),
			// 未就绪摘要（#150）：0 不返回（CR-22「不显示 0 未就绪」）。
			NotReadyCount: func() *int {
				n := domain.CountNotReadyInputs(inputFactsByKr[k.ID])
				if n == 0 {
					return nil
				}
				return &n
			}(),
			TaskCount: intPtr(taskCounts[k.ID]),
			CanEdit:   boolPtr(domain.CanEditKeyResult(actor, userID, fromPgInt8(k.OwnerID))),
			CanDelete: boolPtr(domain.CanDeleteKeyResult(actor, taskCounts[k.ID])),
			// 风险队列副行（#122）：KR 下风险最高的一条卡点，挑选规则在 domain。
			TopBlocker: func() *TopBlocker {
				b := domain.SelectTopBlocker(blockersByKr[k.ID])
				if b == nil {
					return nil
				}
				return &TopBlocker{
					TaskId:    b.TaskID,
					TaskCode:  codeByTask[b.TaskID],
					Kind:      BlockerKind(b.Kind),
					KindLabel: domain.BlockerKindLabel(b.Kind),
					Summary:   b.Reason,
					Level:     RiskLevel(b.Level),
				}
			}(),
		})
	}
	resp := make([]Objective, 0, len(objectives))
	for _, o := range objectives {
		kr := byObjective[o.ID]
		if kr == nil {
			kr = []KeyResult{}
		}
		// O 层风险（AC-59）：只取下级 KR 的最大值，规则在 domain，前端不复算。
		krRisks := make([]domain.KrRisk, 0, len(kr))
		for _, k := range kr {
			krRisks = append(krRisks, domain.KrRisk{Level: string(k.RiskLevel), Note: derefString(k.RiskNote)})
		}
		objRisk := domain.DeriveObjectiveRisk(krRisks)
		resp = append(resp, Objective{
			RiskLevel:      RiskLevel(objRisk.Level),
			RiskLevelLabel: domain.RiskLevelLabel(objRisk.Level),
			RiskNote:       optString(objRisk.Note),
			Id:             o.ID,
			ProjectId:      o.ProjectID,
			Code:           domain.ObjectiveCode(int(o.CodeSeq)),
			Title:          o.Title,
			Description:    optString(o.Description),
			SortOrder:      int(o.SortOrder),
			KeyResults:     kr,
			CanEdit:        boolPtr(domain.CanEditObjective(actor)),
			CanDelete:      boolPtr(domain.CanDeleteObjective(actor, krCountByObjective[o.ID])),
		})
	}
	return resp, nil
}

// toOkrBatchItems 把契约请求映射为 domain 输入（去除首尾空白，规则校验在 domain）。
func toOkrBatchItems(reqItems []CreateOkrBatchItem) []domain.OkrBatchItem {
	items := make([]domain.OkrBatchItem, 0, len(reqItems))
	for _, it := range reqItems {
		item := domain.OkrBatchItem{ObjectiveID: it.ObjectiveId}
		if it.Title != nil {
			item.Title = strings.TrimSpace(*it.Title)
		}
		if it.Description != nil {
			item.Description = strings.TrimSpace(*it.Description)
		}
		if it.KeyResults != nil {
			for _, k := range *it.KeyResults {
				kr := domain.NewKeyResult{
					Description: strings.TrimSpace(k.Description),
					OwnerID:     k.OwnerId,
					Start:       toTimePtr(k.StartDate),
					End:         toTimePtr(k.EndDate),
				}
				if k.Metric != nil {
					kr.Metric = strings.TrimSpace(*k.Metric)
				}
				item.KeyResults = append(item.KeyResults, kr)
			}
		}
		items = append(items, item)
	}
	return items
}

// fetchProject 读取项目与当前用户在其中的成员角色；项目不存在或当前用户不可读时已写出 404 并返回 false。
// 这里是全部项目内端点的唯一读入口：非成员统一按「不存在」处理，不泄露项目是否存在（PRD §3.3）。
func (s *Server) fetchProject(w http.ResponseWriter, r *http.Request, projectID int64) (store.GetProjectRow, bool) {
	uid := currentUser(r).ID
	row, err := s.q.GetProject(r.Context(), store.GetProjectParams{ID: projectID, UserID: uid})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "not_found", Message: "项目不存在"})
		} else {
			writeInternalError(w, r, err)
		}
		return store.GetProjectRow{}, false
	}
	if !domain.CanReadProject(projectActor(uid, row.OwnerID, row.MyRole, row.Visibility)) {
		writeJSON(w, http.StatusNotFound, Error{Code: "not_found", Message: "项目不存在"})
		return store.GetProjectRow{}, false
	}
	return row, true
}

// projectActor 组装当前用户在某项目内的身份事实（domain.Actor）。
// 判定本身在 domain.ProjectIdentity——显式成员身份优先，公开项目才落到隐式访客（#111），
// 这里只负责把行上的列喂进去，handler 不再各写一遍身份。
func projectActor(userID, ownerID int64, myRole pgtype.Text, visibility string) domain.Actor {
	return domain.ProjectIdentity(userID, ownerID, myRole.String, visibility)
}

func (s *Server) ListUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListUsers(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
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

func toProject(p store.Project, ownerName string, actor domain.Actor) Project {
	return Project{
		Id:               p.ID,
		Name:             p.Name,
		OwnerId:          p.OwnerID,
		OwnerName:        ownerName,
		Status:           ProjectStatus(p.Status),
		StatusLabel:      optString(domain.ProjectStatusLabel(p.Status)),
		Stage:            fromPgText(p.Stage),
		PlannedStartDate: fromPgDate(p.PlannedStartDate),
		PlannedEndDate:   fromPgDate(p.PlannedEndDate),
		CreatedAt:        p.CreatedAt.Time,
		CanEdit:          domain.CanEditProject(actor),
		CanManageMembers: domain.CanManageMembers(actor),
		Visibility:       ProjectVisibility(p.Visibility),
		VisibilityLabel:  domain.VisibilityLabel(p.Visibility),
		// 隐式访客是「靠项目公开才看得见」的身份（#111）：项目列表按它分区，前端不自己算。
		ImplicitViewer: actor.Implicit,
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

// boolPtr／intPtr 把派生标志装进契约的可选字段。
func boolPtr(v bool) *bool { return &v }

func intPtr(v int) *int { return &v }

// derefString 取可选字符串的值，未设置时为空串。
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func optString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func toPgInt8(v *int64) pgtype.Int8 {
	if v == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *v, Valid: true}
}

func fromPgInt8(v pgtype.Int8) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

func toPgDateFromTime(t *time.Time) pgtype.Date {
	if t == nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: *t, Valid: true}
}

func toPgDate(d *openapi_types.Date) pgtype.Date {
	if d == nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: d.Time, Valid: true}
}

// pgDateAsTime 把库里的 DATE 转成 domain 判定用的时刻指针（日期部分即可，时区判定在 domain）。
func pgDateAsTime(d pgtype.Date) *time.Time {
	if !d.Valid {
		return nil
	}
	t := d.Time
	return &t
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

func writeForbidden(w http.ResponseWriter) {
	writeJSON(w, http.StatusForbidden, Error{Code: "forbidden", Message: "无权执行该动作"})
}

// writeInternalError 统一记录 500 的真实原因（带 requestID、方法与路径）后，只向客户端回通用文案。
// 内网单机部署没有 APM 兜底，不记日志等于生产故障不可诊断。
func writeInternalError(w http.ResponseWriter, r *http.Request, errs ...error) {
	var cause error
	if len(errs) > 0 {
		cause = errs[0]
	}
	log.Printf("[500] request_id=%s %s %s: %v", requestIDFrom(r.Context()), r.Method, r.URL.Path, cause)
	writeJSON(w, http.StatusInternalServerError, Error{Code: "internal_error", Message: "服务器内部错误"})
}

// clientIP 取请求的真实来源 IP，用于登录限速按 (用户名, IP) 计数。
// 部署形态是 Caddy 反代 app（web/Caddyfile），RemoteAddr 恒为 Caddy 的容器地址，
// 必须读 X-Forwarded-For。客户端可以自带伪造的 XFF，Caddy 只把真实对端追加在尾部，
// 因此取最右一段；开发直连没有 XFF，回落到 RemoteAddr。
func clientIP(r *http.Request) string {
	parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(parts) - 1; i >= 0; i-- {
		if ip := strings.TrimSpace(parts[i]); ip != "" {
			return ip
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
