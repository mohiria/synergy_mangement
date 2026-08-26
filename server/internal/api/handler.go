package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
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
	uid := currentUser(r).ID
	rows, err := s.q.ListProjects(r.Context(), uid)
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
		}, p.OwnerName, projectActor(uid, p.OwnerID, p.MyRole)))
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
		writeInternalError(w)
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
	if !domain.CanEditProject(projectActor(uid, existing.OwnerID, existing.MyRole)) {
		writeForbidden(w)
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
	// 派生字段按更新后的负责人重新判定（负责人可能已易主）。
	writeJSON(w, http.StatusOK, toProject(p, owner.DisplayName, projectActor(uid, p.OwnerID, existing.MyRole)))
}

func (s *Server) ListProjectMembers(w http.ResponseWriter, r *http.Request, projectId int64) {
	if _, ok := s.fetchProject(w, r, projectId); !ok {
		return
	}
	rows, err := s.q.ListProjectMembers(r.Context(), projectId)
	if err != nil {
		writeInternalError(w)
		return
	}
	resp := make([]ProjectMember, 0, len(rows))
	for _, m := range rows {
		resp = append(resp, ProjectMember{
			UserId:      m.UserID,
			Username:    m.Username,
			DisplayName: m.DisplayName,
			Role:        MemberRole(m.Role),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) AddProjectMember(w http.ResponseWriter, r *http.Request, projectId int64) {
	var req AddProjectMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	if !domain.CanManageMembers(projectActor(currentUser(r).ID, proj.OwnerID, proj.MyRole)) {
		writeForbidden(w)
		return
	}
	if err := domain.ValidateMemberRole(string(req.Role)); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_member_role", Message: err.Error()})
		return
	}
	user, err := s.q.GetUserByID(r.Context(), req.UserId)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_user", Message: "用户不存在"})
		return
	}
	m, err := s.q.AddProjectMember(r.Context(), store.AddProjectMemberParams{
		ProjectID: projectId,
		UserID:    user.ID,
		Role:      string(req.Role),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeJSON(w, http.StatusConflict, Error{Code: "already_member", Message: "该用户已是项目成员"})
			return
		}
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, ProjectMember{
		UserId:      m.UserID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Role:        MemberRole(m.Role),
	})
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
	if !domain.CanManageMembers(projectActor(currentUser(r).ID, proj.OwnerID, proj.MyRole)) {
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
		writeInternalError(w)
		return
	}
	user, err := s.q.GetUserByID(r.Context(), m.UserID)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, ProjectMember{
		UserId:      m.UserID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Role:        MemberRole(m.Role),
	})
}

func (s *Server) RemoveProjectMember(w http.ResponseWriter, r *http.Request, projectId int64, userId int64) {
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	if !domain.CanManageMembers(projectActor(currentUser(r).ID, proj.OwnerID, proj.MyRole)) {
		writeForbidden(w)
		return
	}
	n, err := s.q.DeleteProjectMember(r.Context(), store.DeleteProjectMemberParams{
		ProjectID: projectId,
		UserID:    userId,
	})
	if err != nil {
		writeInternalError(w)
		return
	}
	if n == 0 {
		writeJSON(w, http.StatusNotFound, Error{Code: "member_not_found", Message: "该用户不是项目成员"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	}, row.OwnerName, projectActor(uid, row.OwnerID, row.MyRole)))
}

func (s *Server) ListObjectives(w http.ResponseWriter, r *http.Request, projectId int64) {
	if _, ok := s.fetchProject(w, r, projectId); !ok {
		return
	}
	resp, err := s.okrList(r.Context(), projectId)
	if err != nil {
		writeInternalError(w)
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
	if !domain.CanEditProject(projectActor(currentUser(r).ID, proj.OwnerID, proj.MyRole)) {
		writeForbidden(w)
		return
	}
	members, err := s.q.ListProjectMembers(r.Context(), projectId)
	if err != nil {
		writeInternalError(w)
		return
	}
	memberSet := make(map[int64]bool, len(members))
	for _, m := range members {
		memberSet[m.UserID] = true
	}
	items := toOkrBatchItems(req.Items)
	if err := domain.ValidateOkrBatch(items, func(id int64) bool { return memberSet[id] }); err != nil {
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
			writeInternalError(w)
			return
		}
	}
	// 整批一个事务：全部成功或全部失败（契约 createOkrBatch）。
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w)
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
				writeInternalError(w)
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
				RiskLevel:   domain.DefaultKrRiskLevel,
			})
			if err != nil {
				writeInternalError(w)
				return
			}
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w)
		return
	}
	resp, err := s.okrList(r.Context(), projectId)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

// okrList 组装 O 含下属 KR 的层级列表（按排序返回）。
func (s *Server) okrList(ctx context.Context, projectID int64) ([]Objective, error) {
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
	// KR 行的一行风险原因（AC-05）：来自 KR 下任务的派生卡点事实。
	taskRowsForNotes, err := s.q.ListProjectTasks(ctx, projectID)
	if err != nil {
		return nil, err
	}
	krByTask := make(map[int64]int64, len(taskRowsForNotes))
	for _, t := range taskRowsForNotes {
		krByTask[t.ID] = t.KeyResultID
	}
	blockers, err := s.projectBlockers(ctx, projectID)
	if err != nil {
		return nil, err
	}
	notesByKr := make(map[int64][]string)
	for _, b := range blockers {
		krID := krByTask[b.TaskID]
		notesByKr[krID] = append(notesByKr[krID], "缺 "+b.Missing+"："+b.Reason)
	}
	byObjective := make(map[int64][]KeyResult, len(objectives))
	for _, k := range krs {
		summary := domain.ProgressCoverage(factsByKr[k.ID])
		byObjective[k.ObjectiveID] = append(byObjective[k.ObjectiveID], KeyResult{
			Id:          k.ID,
			ObjectiveId: k.ObjectiveID,
			Description: k.Description,
			Metric:      optString(k.Metric),
			OwnerId:     fromPgInt8(k.OwnerID),
			OwnerName:   fromPgText(k.OwnerName),
			StartDate:   fromPgDate(k.StartDate),
			EndDate:     fromPgDate(k.EndDate),
			RiskLevel:   RiskLevel(k.RiskLevel),
			SortOrder:   int(k.SortOrder),
			ProgressSummary: &ProgressSummary{
				TotalTasks:      summary.TotalTasks,
				FilledTasks:     summary.FilledTasks,
				AverageProgress: summary.AverageProgress,
			},
			RiskNote: optString(domain.KrRiskNote(k.RiskLevel, notesByKr[k.ID])),
			OpenBlockerCount: func() *int {
				n := len(notesByKr[k.ID])
				if n == 0 {
					return nil
				}
				return &n
			}(),
		})
	}
	resp := make([]Objective, 0, len(objectives))
	for _, o := range objectives {
		kr := byObjective[o.ID]
		if kr == nil {
			kr = []KeyResult{}
		}
		resp = append(resp, Objective{
			Id:          o.ID,
			ProjectId:   o.ProjectID,
			Title:       o.Title,
			Description: optString(o.Description),
			SortOrder:   int(o.SortOrder),
			KeyResults:  kr,
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

// fetchProject 读取项目与当前用户在其中的成员角色；项目不存在时已写出 404 并返回 false。
func (s *Server) fetchProject(w http.ResponseWriter, r *http.Request, projectID int64) (store.GetProjectRow, bool) {
	row, err := s.q.GetProject(r.Context(), store.GetProjectParams{ID: projectID, UserID: currentUser(r).ID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "not_found", Message: "项目不存在"})
		} else {
			writeInternalError(w)
		}
		return store.GetProjectRow{}, false
	}
	return row, true
}

// projectActor 组装当前用户在某项目内的身份事实（domain.Actor）。
func projectActor(userID, ownerID int64, myRole pgtype.Text) domain.Actor {
	return domain.Actor{IsOwner: userID == ownerID, Role: myRole.String}
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

func toProject(p store.Project, ownerName string, actor domain.Actor) Project {
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
		CanEdit:          domain.CanEditProject(actor),
		CanManageMembers: domain.CanManageMembers(actor),
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

func writeInternalError(w http.ResponseWriter) {
	writeJSON(w, http.StatusInternalServerError, Error{Code: "internal_error", Message: "服务器内部错误"})
}
