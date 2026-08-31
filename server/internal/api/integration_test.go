package api_test

// 集成测试：httptest + 真实 Postgres（docker compose up -d postgres）。
// 每次运行建独立数据库并用 goose 跑迁移，结束后删除。
// 无 Postgres 环境用 go test -short ./... 跳过。

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/pressly/goose/v3"

	"synergy/server/internal/api"
	"synergy/server/internal/domain"
	"synergy/server/internal/filestore"
	"synergy/server/internal/store"
)

func newTestHandler(t *testing.T, pool *pgxpool.Pool) http.Handler {
	t.Helper()
	// 上传两阶段提交后，候选内容必须真的落进对象存储，MinIO 与 Postgres 一样是集成测试的前置依赖。
	// 凭据取自环境变量（与 compose 同名），本地默认值对应 .env.example。
	files, err := filestore.NewMinio(
		envOr("TEST_MINIO_ENDPOINT", "localhost:9000"), "",
		envOr("MINIO_ROOT_USER", "synergy"),
		envOr("MINIO_ROOT_PASSWORD", "synergy-dev-secret"),
		"synergy-test", false)
	if err != nil {
		t.Fatalf("filestore: %v", err)
	}
	// MinIO 容器可达时确保测试桶存在（真实上传/打包路径）；不可达时相关断言自动降级。
	_ = files.EnsureBucket(context.Background())
	return api.NewHandler(pool, "/api/v1", files)
}

// putObject 直传对象；MinIO 与 Postgres 一样是集成测试的前置依赖（docker compose up -d minio）。
func putObject(t *testing.T, url, content string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(content))
	if err != nil {
		t.Fatalf("构造上传请求: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("MinIO 不可达（docker compose up -d minio）: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("上传对象失败: %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
}

// uploadTaskFile 走完任务文件的两阶段提交：登记 uploading → 直传对象 → commit 转 ready（§7.7）。
func uploadTaskFile(t *testing.T, c *http.Client, tasksURL string, taskID int64, req api.UploadTaskFileRequest, content string) api.TaskFile {
	t.Helper()
	resp := doJSON(t, c, http.MethodPost, fmt.Sprintf("%s/%d/files", tasksURL, taskID), req)
	wantStatus(t, resp, http.StatusCreated)
	up := decodeBody[api.UploadTaskFileResponse](t, resp)
	if up.UploadUrl == "" {
		t.Fatalf("任务文件登记异常: %+v", up)
	}
	putObject(t, up.UploadUrl, content)
	resp = doJSON(t, c, http.MethodPost, fmt.Sprintf("%s/%d/files/%d/commit", tasksURL, taskID, up.File.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	return decodeBody[api.TaskFile](t, resp)
}

// uploadCandidate 走完候选内容的两阶段提交：登记 uploading → 直传对象 → commit 转 candidate。
func uploadCandidate(t *testing.T, c *http.Client, tasksURL string, taskID, deliverableID int64, req api.UploadCandidateRequest, content string) api.DeliverableFile {
	t.Helper()
	url := fmt.Sprintf("%s/%d/deliverables/%d/candidate", tasksURL, taskID, deliverableID)
	resp := doJSON(t, c, http.MethodPost, url, req)
	wantStatus(t, resp, http.StatusCreated)
	up := decodeBody[api.UploadCandidateResponse](t, resp)
	if up.UploadUrl == "" || up.File.State != api.DeliverableFileStateUploading {
		t.Fatalf("候选登记异常: %+v", up)
	}
	putObject(t, up.UploadUrl, content)
	resp = doJSON(t, c, http.MethodPost, url+"/commit", api.CommitUploadRequest{FileId: up.File.Id})
	wantStatus(t, resp, http.StatusOK)
	f := decodeBody[api.DeliverableFile](t, resp)
	if f.State != api.DeliverableFileStateCandidate {
		t.Fatalf("确认后应为候选: %+v", f)
	}
	return f
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func setupDB(t *testing.T) (*store.Queries, *pgxpool.Pool) {
	t.Helper()
	if testing.Short() {
		t.Skip("需要真实 Postgres，-short 模式跳过")
	}
	adminDSN := os.Getenv("TEST_DATABASE_URL")
	if adminDSN == "" {
		adminDSN = "postgres://synergy:synergy@localhost:5432/synergy?sslmode=disable"
	}

	adminDB, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatalf("open admin db: %v", err)
	}
	if err := adminDB.Ping(); err != nil {
		t.Fatalf("Postgres 不可达（docker compose up -d postgres）: %v", err)
	}

	dbName := fmt.Sprintf("synergy_test_%d", time.Now().UnixNano())
	if _, err := adminDB.Exec("CREATE DATABASE " + dbName); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminDB.Exec("DROP DATABASE " + dbName + " WITH (FORCE)")
		_ = adminDB.Close()
	})

	u, err := url.Parse(adminDSN)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	u.Path = "/" + dbName
	testDSN := u.String()

	migDB, err := sql.Open("pgx", testDSN)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	if err := goose.Up(migDB, "../../migrations"); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	_ = migDB.Close()

	pool, err := pgxpool.New(context.Background(), testDSN)
	if err != nil {
		t.Fatalf("pgxpool: %v", err)
	}
	t.Cleanup(pool.Close)
	return store.New(pool), pool
}

// queryCounter 数一次请求里实际发出的 SQL 条数（P1 的验收要「附前后计数」）。
type queryCounter struct {
	mu sync.Mutex
	n  int
}

func (c *queryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	return ctx
}

func (c *queryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (c *queryCounter) reset() {
	c.mu.Lock()
	c.n = 0
	c.mu.Unlock()
}

func (c *queryCounter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// setupCountedDB 与 setupDB 同构，但连接池带查询计数器。
func setupCountedDB(t *testing.T) (*store.Queries, *pgxpool.Pool, *queryCounter) {
	t.Helper()
	q, pool := setupDB(t)
	cfg := pool.Config().Copy()
	counter := &queryCounter{}
	cfg.ConnConfig.Tracer = counter
	counted, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("counted pool: %v", err)
	}
	t.Cleanup(counted.Close)
	return q, counted, counter
}

func seedUser(t *testing.T, q *store.Queries, username, display, password string) store.User {
	t.Helper()
	hash, err := domain.HashPassword(password)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	u, err := q.CreateUser(context.Background(), store.CreateUserParams{
		Username: username, DisplayName: display, PasswordHash: hash,
	})
	if err != nil {
		t.Fatalf("seed user %s: %v", username, err)
	}
	return u
}

func newClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	return &http.Client{Jar: jar}
}

func doJSON(t *testing.T, c *http.Client, method, urlStr string, body any) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	req, err := http.NewRequest(method, urlStr, &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, urlStr, err)
	}
	return resp
}

// doJSONAgain 直接返回 200 的响应，供「读一次就地断言」的场景内联使用。
func doJSONAgain(t *testing.T, c *http.Client, method, urlStr string) *http.Response {
	t.Helper()
	resp := doJSON(t, c, method, urlStr, nil)
	wantStatus(t, resp, http.StatusOK)
	return resp
}

// doRaw 发送原样 JSON 文本：用于断言契约外的字段（如只读派生字段）被忽略。
func doRaw(t *testing.T, c *http.Client, method, urlStr, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, urlStr, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, urlStr, err)
	}
	return resp
}

// dropOkrAssigned 过滤新增 O/KR 的指派通知（#125）：多数用例只断言自己触发的那一条通知，
// 而创建 OKR 的准备步骤会给 KR 负责人发 okr_assigned，先滤掉再断言。
func dropOkrAssigned(notes []api.Notification) []api.Notification {
	out := []api.Notification{}
	for _, n := range notes {
		if n.Kind != "okr_assigned" {
			out = append(out, n)
		}
	}
	return out
}

func decodeBody[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return v
}

func openapiDate(t *testing.T, s string) openapi_types.Date {
	t.Helper()
	v, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("bad date %q: %v", s, err)
	}
	return openapi_types.Date{Time: v}
}

func wantStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		// 带上响应体：只报状态码时，排查一次意外的 409／422 得再跑一遍加日志。
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d, body=%s", resp.StatusCode, want, strings.TrimSpace(string(body)))
	}
}

// 结构变更（输入、输入源、输出、接收方）改走关键字段修改审批后（AC-23），
// 写入接口统一返回 200 Task；下面三个 helper 承担「受理成功」与「按名字找边」两件事。

// wantStructureAccepted 断言结构变更已受理并回传任务最新状态。
func wantStructureAccepted(t *testing.T, resp *http.Response) api.Task {
	t.Helper()
	wantStatus(t, resp, http.StatusOK)
	return decodeBody[api.Task](t, resp)
}

// projectEdges 取项目全部交付物边（写入接口不再直接回传新建的边）。
func projectEdges(t *testing.T, c *http.Client, base string, projectID int64) []api.DeliverableEdge {
	t.Helper()
	resp := doJSON(t, c, http.MethodGet, fmt.Sprintf("%s/projects/%d/edges", base, projectID), nil)
	wantStatus(t, resp, http.StatusOK)
	return decodeBody[[]api.DeliverableEdge](t, resp)
}

// edgeOf 在项目边里按目标任务与边名定位一条边。
func edgeOf(t *testing.T, c *http.Client, base string, projectID, targetTaskID int64, name string) api.DeliverableEdge {
	t.Helper()
	for _, e := range projectEdges(t, c, base, projectID) {
		// name 是派生标识（#112）：任务来源为「编号 · 任务名」，成员来源为「所需内容」摘要，
		// 用例给出的定位串按包含匹配即可。
		if e.TargetTaskId == targetTaskID && strings.Contains(e.Name, name) {
			return e
		}
	}
	t.Fatalf("未找到任务 %d 上名为 %q 的交付物边", targetTaskID, name)
	return api.DeliverableEdge{}
}

// approveStructureChange 所属 KR 负责人通过任务上那张待审批的变更单。
func approveStructureChange(t *testing.T, approver *http.Client, base string, projectID, taskID int64, task api.Task) {
	t.Helper()
	if task.FieldChange == nil || task.FieldChange.State != api.FieldChangeStatePending {
		t.Fatalf("任务 %d 上没有待审批变更单: %+v", taskID, task.FieldChange)
	}
	resp := doJSON(t, approver, http.MethodPost,
		fmt.Sprintf("%s/projects/%d/tasks/%d/field-changes/%d/decision", base, projectID, taskID, task.FieldChange.Id),
		api.FieldChangeDecisionRequest{Decision: api.FieldChangeDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
}

// deliverableOf 在任务详情里按名字定位交付物项。
func deliverableOf(t *testing.T, c *http.Client, base string, projectID, taskID int64, name string) api.Deliverable {
	t.Helper()
	resp := doJSON(t, c, http.MethodGet, fmt.Sprintf("%s/projects/%d/tasks/%d", base, projectID, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	for _, d := range decodeBody[api.TaskDetail](t, resp).Deliverables {
		if d.Name == name {
			return d
		}
	}
	t.Fatalf("未找到任务 %d 上名为 %q 的交付物项", taskID, name)
	return api.Deliverable{}
}

func TestAuthAndProjectsEndToEnd(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	alice := newClient(t)
	bob := newClient(t)

	// 未登录访问受保护接口被拒
	resp := doJSON(t, alice, http.MethodGet, base+"/projects", nil)
	wantStatus(t, resp, http.StatusUnauthorized)
	resp.Body.Close()

	// 错误口令 401
	resp = doJSON(t, alice, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: "alice", Password: "wrong"})
	wantStatus(t, resp, http.StatusUnauthorized)
	resp.Body.Close()

	// 正确登录建立会话
	resp = doJSON(t, alice, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: "alice", Password: "alice-pass"})
	wantStatus(t, resp, http.StatusOK)
	me := decodeBody[api.CurrentUser](t, resp)
	if me.Username != "alice" || me.DisplayName != "张三" {
		t.Fatalf("login 返回身份不符: %+v", me)
	}

	// 不同用户登录后各自身份生效（AC-21 前提）
	resp = doJSON(t, bob, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: "bob", Password: "bob-pass"})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodGet, base+"/auth/me", nil)
	wantStatus(t, resp, http.StatusOK)
	if got := decodeBody[api.CurrentUser](t, resp); got.Username != "alice" {
		t.Fatalf("alice 会话返回了 %q", got.Username)
	}
	resp = doJSON(t, bob, http.MethodGet, base+"/auth/me", nil)
	wantStatus(t, resp, http.StatusOK)
	if got := decodeBody[api.CurrentUser](t, resp); got.Username != "bob" {
		t.Fatalf("bob 会话返回了 %q", got.Username)
	}

	// alice 创建项目（负责人指定 bob），bob 可在列表中看到派生字段
	stage := "联合联调阶段"
	resp = doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{
		Name: "协同管理试点", OwnerId: bobUser.ID, Stage: &stage,
	})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	if created.Name != "协同管理试点" || created.Id == 0 {
		t.Fatalf("创建项目返回异常: %+v", created)
	}
	if created.OwnerId != bobUser.ID || created.OwnerName != "李四" {
		t.Fatalf("负责人派生字段异常: %+v", created)
	}
	if created.Status != api.ProjectStatusNotStarted {
		t.Fatalf("新建项目状态 = %q, want not_started", created.Status)
	}
	if created.Stage == nil || *created.Stage != "联合联调阶段" {
		t.Fatalf("阶段字段异常: %+v", created.Stage)
	}
	resp = doJSON(t, bob, http.MethodGet, base+"/projects", nil)
	wantStatus(t, resp, http.StatusOK)
	list := decodeBody[[]api.Project](t, resp)
	if len(list) != 1 || list[0].Id != created.Id || list[0].OwnerName != "李四" {
		t.Fatalf("项目列表异常: %+v", list)
	}

	// 项目名校验 422
	resp = doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "   ", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()

	// 负责人不存在 422
	resp = doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "无主项目", OwnerId: 99999})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	if e := decodeBody[api.Error](t, resp); e.Code != "invalid_owner" {
		t.Fatalf("code = %q, want invalid_owner", e.Code)
	}

	// 计划完成日期早于开始日期 422
	start := openapiDate(t, "2026-09-30")
	end := openapiDate(t, "2026-09-01")
	resp = doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{
		Name: "倒置周期", OwnerId: aliceUser.ID, PlannedStartDate: &start, PlannedEndDate: &end,
	})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	if e := decodeBody[api.Error](t, resp); e.Code != "invalid_project_plan" {
		t.Fatalf("code = %q, want invalid_project_plan", e.Code)
	}

	// 新建项目默认私有（AC-69）：可见性不由创建时指定，改开关走项目设置。
	if created.Visibility != api.Private || created.VisibilityLabel != "私有项目" || created.ImplicitViewer {
		t.Fatalf("新建项目应默认私有: %+v", created)
	}

	// 更新项目：改负责人、状态与计划周期
	ps := openapiDate(t, "2026-09-01")
	pe := openapiDate(t, "2026-12-31")
	resp = doJSON(t, alice, http.MethodPut, fmt.Sprintf("%s/projects/%d", base, created.Id), api.UpdateProjectRequest{
		Name: "协同管理试点", OwnerId: aliceUser.ID, Status: api.ProjectStatusInProgress,
		PlannedStartDate: &ps, PlannedEndDate: &pe, Visibility: api.Private,
	})
	wantStatus(t, resp, http.StatusOK)
	updated := decodeBody[api.Project](t, resp)
	if updated.Status != api.ProjectStatusInProgress || updated.OwnerName != "张三" {
		t.Fatalf("更新项目返回异常: %+v", updated)
	}
	if updated.PlannedStartDate == nil || updated.PlannedEndDate == nil {
		t.Fatalf("计划周期未保存: %+v", updated)
	}
	if updated.Stage != nil {
		t.Fatalf("全量更新未清空阶段: %+v", updated.Stage)
	}

	// 更新不存在的项目 404
	resp = doJSON(t, alice, http.MethodPut, base+"/projects/99999", api.UpdateProjectRequest{
		Name: "任意", OwnerId: aliceUser.ID, Status: api.ProjectStatusNotStarted, Visibility: api.Private,
	})
	wantStatus(t, resp, http.StatusNotFound)
	resp.Body.Close()

	// 用户目录（人员选择）
	resp = doJSON(t, bob, http.MethodGet, base+"/users", nil)
	wantStatus(t, resp, http.StatusOK)
	users := decodeBody[[]api.UserSummary](t, resp)
	if len(users) != 2 || users[0].DisplayName != "张三" {
		t.Fatalf("用户列表异常: %+v", users)
	}

	// 登出后会话失效
	resp = doJSON(t, alice, http.MethodPost, base+"/auth/logout", nil)
	wantStatus(t, resp, http.StatusNoContent)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodGet, base+"/auth/me", nil)
	wantStatus(t, resp, http.StatusUnauthorized)
	resp.Body.Close()

	// bob 的会话不受 alice 登出影响
	resp = doJSON(t, bob, http.MethodGet, base+"/auth/me", nil)
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
}

// 成员与角色权限骨架（#2，AC-21/AC-22）：管理员加成员赋角色，动作权限按角色在 domain 判定；
// 项目负责人与管理员独立同权（V4.4.2），负责人不自动成为成员。
func TestProjectMembersAndPermissions(t *testing.T) {
	q, pool := setupDB(t)
	seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	carolUser := seedUser(t, q, "carol", "王五", "carol-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	login := func(c *http.Client, username, password string) {
		t.Helper()
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	alice, bob, carol := newClient(t), newClient(t), newClient(t)
	login(alice, "alice", "alice-pass")
	login(bob, "bob", "bob-pass")
	login(carol, "carol", "carol-pass")

	// alice 创建项目，负责人指定 bob；创建人自动成为管理员成员，负责人不自动入成员表
	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "成员试点", OwnerId: bobUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	if !created.CanEdit || !created.CanManageMembers {
		t.Fatalf("创建人应可编辑并管理成员: %+v", created)
	}
	membersURL := fmt.Sprintf("%s/projects/%d/members", base, created.Id)

	resp = doJSON(t, alice, http.MethodGet, membersURL, nil)
	wantStatus(t, resp, http.StatusOK)
	members := decodeBody[[]api.ProjectMember](t, resp)
	if len(members) != 1 || members[0].Username != "alice" || members[0].Role != api.Admin {
		t.Fatalf("创建人应为唯一管理员成员: %+v", members)
	}

	// 管理员把 carol 加为访客
	resp = doJSON(t, alice, http.MethodPost, membersURL, api.AddProjectMembersRequest{UserIds: []int64{carolUser.ID}, Role: api.Viewer})
	wantStatus(t, resp, http.StatusCreated)
	result := decodeBody[api.AddProjectMembersResult](t, resp)
	if len(result.Added) != 1 || result.Added[0].DisplayName != "王五" || result.Added[0].Role != api.Viewer ||
		len(result.Skipped) != 0 {
		t.Fatalf("加入成员返回异常: %+v", result)
	}

	// 访客既不能管理成员也不能编辑项目 → 403
	resp = doJSON(t, carol, http.MethodPost, membersURL, api.AddProjectMembersRequest{UserIds: []int64{bobUser.ID}, Role: api.Member})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodPut, fmt.Sprintf("%s/projects/%d", base, created.Id), api.UpdateProjectRequest{
		Name: "成员试点", OwnerId: bobUser.ID, Status: api.ProjectStatusNotStarted, Visibility: api.Private,
	})
	wantStatus(t, resp, http.StatusForbidden)
	if e := decodeBody[api.Error](t, resp); e.Code != "forbidden" {
		t.Fatalf("code = %q, want forbidden", e.Code)
	}

	// carol 视角的派生字段为不可操作
	resp = doJSON(t, carol, http.MethodGet, base+"/projects", nil)
	wantStatus(t, resp, http.StatusOK)
	if list := decodeBody[[]api.Project](t, resp); list[0].CanEdit || list[0].CanManageMembers {
		t.Fatalf("访客派生字段异常: %+v", list[0])
	}

	// 项目负责人 bob 非成员，但享有与管理员同等权限（V4.4.2）：可编辑项目、可调整成员角色
	resp = doJSON(t, bob, http.MethodGet, base+"/projects", nil)
	wantStatus(t, resp, http.StatusOK)
	if list := decodeBody[[]api.Project](t, resp); !list[0].CanEdit || !list[0].CanManageMembers {
		t.Fatalf("负责人派生字段异常: %+v", list[0])
	}
	resp = doJSON(t, bob, http.MethodPut, fmt.Sprintf("%s/projects/%d/members/%d", base, created.Id, carolUser.ID),
		api.UpdateProjectMemberRoleRequest{Role: api.Member})
	wantStatus(t, resp, http.StatusOK)
	if m := decodeBody[api.ProjectMember](t, resp); m.Role != api.Member {
		t.Fatalf("角色调整返回异常: %+v", m)
	}

	// 已关闭的 editor 角色（V4.4.3）→ 422
	// 契约校验中间件先于 handler 拦下枚举越界，返回通用 invalid_request；
	// domain 的 invalid_member_role 作为兜底保留（契约未声明枚举的入口仍走它）。
	resp = doJSON(t, alice, http.MethodPut, fmt.Sprintf("%s/projects/%d/members/%d", base, created.Id, carolUser.ID),
		map[string]any{"role": "editor"})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	if e := decodeBody[api.Error](t, resp); e.Code != "invalid_request" && e.Code != "invalid_member_role" {
		t.Fatalf("code = %q, want invalid_request 或 invalid_member_role", e.Code)
	}

	// 项目成员仍不能管理成员 → 403
	resp = doJSON(t, carol, http.MethodPost, membersURL, api.AddProjectMembersRequest{UserIds: []int64{bobUser.ID}, Role: api.Member})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()

	// #93 批量加入：重复的人与不存在的用户按人跳过，名单里其余人照加
	resp = doJSON(t, alice, http.MethodPost, membersURL,
		api.AddProjectMembersRequest{UserIds: []int64{carolUser.ID, 99999, bobUser.ID}, Role: api.Member})
	wantStatus(t, resp, http.StatusCreated)
	result = decodeBody[api.AddProjectMembersResult](t, resp)
	if len(result.Added) != 1 || result.Added[0].UserId != bobUser.ID {
		t.Fatalf("批量加入的 added 异常: %+v", result)
	}
	if len(result.Skipped) != 2 ||
		result.Skipped[0].UserId != carolUser.ID || result.Skipped[0].Reason != api.AlreadyMember ||
		result.Skipped[0].ReasonLabel != "已在项目内" || result.Skipped[0].DisplayName == nil ||
		result.Skipped[1].UserId != 99999 || result.Skipped[1].Reason != api.UserNotFound {
		t.Fatalf("批量加入的 skipped 异常: %+v", result.Skipped)
	}
	// bob 本轮已被加成员，后续用例仍按「项目负责人非成员」的口径断言，这里加完即撤
	resp = doJSON(t, alice, http.MethodDelete, fmt.Sprintf("%s/%d", membersURL, bobUser.ID), nil)
	wantStatus(t, resp, http.StatusNoContent)
	resp.Body.Close()

	// 非法角色 422（契约校验先拦，见上方说明）
	resp = doJSON(t, alice, http.MethodPost, membersURL, map[string]any{"userIds": []int64{bobUser.ID}, "role": "boss"})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	if e := decodeBody[api.Error](t, resp); e.Code != "invalid_request" && e.Code != "invalid_member_role" {
		t.Fatalf("code = %q, want invalid_request 或 invalid_member_role", e.Code)
	}

	// 空名单 422
	resp = doJSON(t, alice, http.MethodPost, membersURL, map[string]any{"userIds": []int64{}, "role": "member"})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	if e := decodeBody[api.Error](t, resp); e.Code != "invalid_request" && e.Code != "no_members_selected" {
		t.Fatalf("code = %q, want invalid_request 或 no_members_selected", e.Code)
	}

	// 调整非成员（bob）角色 404
	resp = doJSON(t, alice, http.MethodPut, fmt.Sprintf("%s/projects/%d/members/%d", base, created.Id, bobUser.ID),
		api.UpdateProjectMemberRoleRequest{Role: api.Member})
	wantStatus(t, resp, http.StatusNotFound)
	resp.Body.Close()

	// 移出成员：204，再移一次 404
	resp = doJSON(t, alice, http.MethodDelete, fmt.Sprintf("%s/projects/%d/members/%d", base, created.Id, carolUser.ID), nil)
	wantStatus(t, resp, http.StatusNoContent)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodDelete, fmt.Sprintf("%s/projects/%d/members/%d", base, created.Id, carolUser.ID), nil)
	wantStatus(t, resp, http.StatusNotFound)
	resp.Body.Close()

	// 项目不存在 404
	resp = doJSON(t, alice, http.MethodGet, base+"/projects/99999/members", nil)
	wantStatus(t, resp, http.StatusNotFound)
	resp.Body.Close()
}

// O／KR 表格式创建（#3，AC-01；AC-06 展开层所需的量化指标与 KR 负责人由此供给）：一批建多个 O 与 KR、指定 KR 负责人；
// 仅项目管理员／项目负责人可创建；整批一个事务；已有 O 可继续追加 KR。
func TestOkrTableBatchCreate(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	carolUser := seedUser(t, q, "carol", "王五", "carol-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	login := func(c *http.Client, username, password string) {
		t.Helper()
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	alice, bob := newClient(t), newClient(t)
	login(alice, "alice", "alice-pass")
	login(bob, "bob", "bob-pass")

	sp := func(s string) *string { return &s }

	// alice 创建项目并任负责人，bob 加为项目成员
	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "OKR 试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
		api.AddProjectMembersRequest{UserIds: []int64{bobUser.ID}, Role: api.Member})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()

	// 新增端点：单项目详情，派生字段随身份变化
	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/projects/%d", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	if p := decodeBody[api.Project](t, resp); p.Id != created.Id || p.CanEdit {
		t.Fatalf("项目成员的项目详情派生字段异常: %+v", p)
	}

	okrURL := fmt.Sprintf("%s/projects/%d/objectives", base, created.Id)
	start := openapiDate(t, "2026-09-01")
	end := openapiDate(t, "2026-12-31")

	// 表格式批量创建：两个 O，第一个带两条 KR（一条指定负责人、量化指标和周期）
	resp = doJSON(t, alice, http.MethodPost, okrURL, api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
		{Title: sp("提升产品体验"), KeyResults: &[]api.CreateKeyResultInput{
			{Description: "上线新版工作台", Metric: sp("转化率 5%"), OwnerId: &bobUser.ID, StartDate: &start, EndDate: &end},
			{Description: "NPS 提升到 40"},
		}},
		{Title: sp("扩大市场份额")},
	}})
	wantStatus(t, resp, http.StatusCreated)
	batchResp := decodeBody[api.CreateOkrBatchResponse](t, resp)
	list := batchResp.Objectives
	// #125「保存并通知负责人」：本次指派 bob 一名负责人，人数按人去重；操作者本人不计。
	if batchResp.NotifiedCount != 1 {
		t.Fatalf("已通知负责人数异常: %d", batchResp.NotifiedCount)
	}
	if len(list) != 2 || list[0].Title != "提升产品体验" || list[1].Title != "扩大市场份额" {
		t.Fatalf("批量创建返回异常: %+v", list)
	}
	if len(list[0].KeyResults) != 2 || len(list[1].KeyResults) != 0 {
		t.Fatalf("KR 归属异常: %+v", list)
	}
	kr := list[0].KeyResults[0]
	if kr.OwnerName == nil || *kr.OwnerName != "李四" || kr.RiskLevel != api.Normal || kr.SortOrder != 1 {
		t.Fatalf("KR 派生字段异常: %+v", kr)
	}
	// AC-59：O 的风险取下级最大值——下面没有 KR 或 KR 全正常时都是正常（#82）。
	if list[0].RiskLevel != api.Normal || list[0].RiskLevelLabel != "正常" || list[0].RiskNote != nil {
		t.Fatalf("KR 全正常时 O 应为正常: %+v / %q / %+v", list[0].RiskLevel, list[0].RiskLevelLabel, list[0].RiskNote)
	}
	if list[1].RiskLevel != api.Normal {
		t.Fatalf("没有 KR 的 O 应为正常: %+v", list[1].RiskLevel)
	}
	if kr.StartDate == nil || kr.EndDate == nil {
		t.Fatalf("KR 周期未保存: %+v", kr)
	}
	if second := list[0].KeyResults[1]; second.OwnerId != nil || second.SortOrder != 2 {
		t.Fatalf("未指定负责人的 KR 异常: %+v", second)
	}

	// #125：被指派的 KR 负责人收到 okr_assigned 站内通知，文案含 KR 描述；操作者本人不收。
	resp = doJSON(t, bob, http.MethodGet, base+"/notifications", nil)
	wantStatus(t, resp, http.StatusOK)
	bobNotes := decodeBody[[]api.Notification](t, resp)
	if len(bobNotes) != 1 || bobNotes[0].Kind != "okr_assigned" || !strings.Contains(bobNotes[0].Content, "上线新版工作台") {
		t.Fatalf("KR 负责人应收到指派通知: %+v", bobNotes)
	}
	resp = doJSON(t, alice, http.MethodGet, base+"/notifications", nil)
	wantStatus(t, resp, http.StatusOK)
	if aliceNotes := decodeBody[[]api.Notification](t, resp); len(aliceNotes) != 0 {
		t.Fatalf("操作者本人不应收指派通知: %+v", aliceNotes)
	}

	// 向已有 O 追加 KR
	resp = doJSON(t, alice, http.MethodPost, okrURL, api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
		{ObjectiveId: &list[1].Id, KeyResults: &[]api.CreateKeyResultInput{{Description: "签下 10 家标杆客户"}}},
	}})
	wantStatus(t, resp, http.StatusCreated)
	list = decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	if len(list) != 2 || len(list[1].KeyResults) != 1 || list[1].KeyResults[0].Description != "签下 10 家标杆客户" {
		t.Fatalf("追加 KR 异常: %+v", list)
	}

	// 项目成员读取层级列表 200
	resp = doJSON(t, bob, http.MethodGet, okrURL, nil)
	wantStatus(t, resp, http.StatusOK)
	if got := decodeBody[[]api.Objective](t, resp); len(got) != 2 {
		t.Fatalf("成员读取 O／KR 列表异常: %+v", got)
	}

	// 项目成员批量创建 403（编辑项目结构需管理员／负责人）
	resp = doJSON(t, bob, http.MethodPost, okrURL, api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{{Title: sp("越权 O")}}})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()

	// KR 负责人不是项目成员 422
	resp = doJSON(t, alice, http.MethodPost, okrURL, api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
		{Title: sp("负责人越界"), KeyResults: &[]api.CreateKeyResultInput{{Description: "x", OwnerId: &carolUser.ID}}},
	}})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	if e := decodeBody[api.Error](t, resp); e.Code != "invalid_okr" {
		t.Fatalf("code = %q, want invalid_okr", e.Code)
	}

	// title 与 objectiveId 同时给 422
	resp = doJSON(t, alice, http.MethodPost, okrURL, api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
		{ObjectiveId: &list[0].Id, Title: sp("二义项")},
	}})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()

	// 指向不存在的 O 422，且整批不落库（事务前置校验）
	resp = doJSON(t, alice, http.MethodPost, okrURL, api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
		{Title: sp("好 O")},
		{ObjectiveId: sp2int64(99999), KeyResults: &[]api.CreateKeyResultInput{{Description: "x"}}},
	}})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	if e := decodeBody[api.Error](t, resp); e.Code != "invalid_objective" {
		t.Fatalf("code = %q, want invalid_objective", e.Code)
	}
	resp = doJSON(t, alice, http.MethodGet, okrURL, nil)
	wantStatus(t, resp, http.StatusOK)
	if got := decodeBody[[]api.Objective](t, resp); len(got) != 2 {
		t.Fatalf("失败批次不应写入任何 O: %+v", got)
	}
}

func sp2int64(v int64) *int64 { return &v }

// 任务创建、入池审批与免审（#4，AC-04／AC-26）：
// 项目成员提交任务→待入池审批→KR 负责人通过→未开始；退回→草稿可重新提交；
// KR 负责人本人创建免审直接未开始并记录免审原因；管理员不能替代 KR 负责人审批。
func TestTaskCreateAndPoolReview(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	carolUser := seedUser(t, q, "carol", "王五", "carol-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	login := func(c *http.Client, username, password string) {
		t.Helper()
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	alice, bob, carol := newClient(t), newClient(t), newClient(t)
	login(alice, "alice", "alice-pass")
	login(bob, "bob", "bob-pass")
	login(carol, "carol", "carol-pass")

	sp := func(s string) *string { return &s }

	// alice（管理员/负责人）建项目，bob、carol 为项目成员；bob 任 KR 负责人
	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "任务试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	for _, uid := range []int64{bobUser.ID, carolUser.ID} {
		resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
			api.AddProjectMembersRequest{UserIds: []int64{uid}, Role: api.Member})
		wantStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{
				{Description: "上线自动验收", OwnerId: &bobUser.ID},
				{Description: "未指负责人的 KR"},
			}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	krWithOwner := okr[0].KeyResults[0].Id
	krNoOwner := okr[0].KeyResults[1].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-12"), openapiDate(t, "2026-09-21")

	// AC-04：项目成员 carol 创建并提交，任务进入待入池审批
	resp = doJSON(t, carol, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items: []api.CreateTaskItem{
			{KeyResultId: krWithOwner, Name: "验证现场联动异常回退", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	list := decodeBody[[]api.Task](t, resp)
	if len(list) != 1 || list[0].Status != api.TaskStatusPendingPoolReview {
		t.Fatalf("项目成员提交后应为待入池审批: %+v", list)
	}
	taskID := list[0].Id
	if list[0].PoolReview == nil || list[0].PoolReview.Status != api.PoolReviewStatusPending || list[0].PoolReview.Exempt {
		t.Fatalf("审批单异常: %+v", list[0].PoolReview)
	}
	// AC-04：面向用户显示当前审批人姓名（入池审批人是 KR 负责人 bob／李四）。
	if list[0].StatusLabel != "待李四审批" {
		t.Fatalf("待入池审批任务显示文案 = %q, want 待李四审批", list[0].StatusLabel)
	}
	if list[0].PoolReview.StatusLabel != "待李四审批" {
		t.Fatalf("入池审批单显示文案 = %q, want 待李四审批", list[0].PoolReview.StatusLabel)
	}
	// carol 视角不可自审；bob（KR 负责人）视角可审
	if list[0].CanDecidePoolReview {
		t.Fatalf("提交人不应可处理审批: %+v", list[0])
	}
	resp = doJSON(t, bob, http.MethodGet, tasksURL, nil)
	wantStatus(t, resp, http.StatusOK)
	if got := decodeBody[[]api.Task](t, resp); !got[0].CanDecidePoolReview {
		t.Fatalf("KR 负责人应可处理审批: %+v", got[0])
	}

	// §3.3：管理员 alice 不能替代 KR 负责人 → 403
	decisionURL := fmt.Sprintf("%s/%d/pool-review-decision", tasksURL, taskID)
	resp = doJSON(t, alice, http.MethodPost, decisionURL, api.PoolReviewDecisionRequest{Decision: api.PoolReviewDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()

	// bob 退回 → 任务回到草稿，意见保留
	opinion := "验收口径不清，请补充完成标准"
	resp = doJSON(t, bob, http.MethodPost, decisionURL, api.PoolReviewDecisionRequest{Decision: api.PoolReviewDecisionRequestDecisionRejected, Opinion: &opinion})
	wantStatus(t, resp, http.StatusOK)
	rejected := decodeBody[api.Task](t, resp)
	if rejected.Status != api.TaskStatusDraft || rejected.PoolReview == nil || rejected.PoolReview.Status != api.PoolReviewStatusRejected {
		t.Fatalf("退回后状态异常: %+v", rejected)
	}
	if rejected.PoolReview.Opinion == nil || *rejected.PoolReview.Opinion != opinion {
		t.Fatalf("退回意见未保留: %+v", rejected.PoolReview)
	}
	if rejected.StatusLabel != "草稿" || rejected.PoolReview.StatusLabel != "已退回" {
		t.Fatalf("退回后显示文案异常: %q / %q", rejected.StatusLabel, rejected.PoolReview.StatusLabel)
	}

	// 草稿可重新提交（生成新审批单），KR 负责人通过 → 未开始
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/submit-pool-review", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	if got := decodeBody[api.Task](t, resp); got.Status != api.TaskStatusPendingPoolReview {
		t.Fatalf("重新提交后应为待入池审批: %+v", got)
	}
	resp = doJSON(t, bob, http.MethodPost, decisionURL, api.PoolReviewDecisionRequest{Decision: api.PoolReviewDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusOK)
	approved := decodeBody[api.Task](t, resp)
	if approved.Status != api.TaskStatusNotStarted || approved.PoolReview.Status != api.PoolReviewStatusApproved {
		t.Fatalf("通过后应进入未开始: %+v", approved)
	}

	// 已处理的审批不可重复处理 → 409
	resp = doJSON(t, bob, http.MethodPost, decisionURL, api.PoolReviewDecisionRequest{Decision: api.PoolReviewDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()

	// AC-26：KR 负责人 bob 在本人 KR 下创建 → 免审直接未开始并记录免审原因
	resp = doJSON(t, bob, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items: []api.CreateTaskItem{
			{KeyResultId: krWithOwner, Name: "输出验收清单模板", OwnerId: bobUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	list = decodeBody[[]api.Task](t, resp)
	var exemptTask *api.Task
	for i := range list {
		if list[i].Name == "输出验收清单模板" {
			exemptTask = &list[i]
		}
	}
	if exemptTask == nil || exemptTask.Status != api.TaskStatusNotStarted {
		t.Fatalf("免审任务应直接未开始: %+v", list)
	}
	if exemptTask.PoolReview == nil || !exemptTask.PoolReview.Exempt || exemptTask.PoolReview.Status != api.PoolReviewStatusApproved || exemptTask.PoolReview.Opinion == nil {
		t.Fatalf("免审应生成已通过并记录原因的审批单: %+v", exemptTask.PoolReview)
	}
	if exemptTask.PoolReview.StatusLabel != "免审通过" {
		t.Fatalf("免审审批单显示文案 = %q, want 免审通过", exemptTask.PoolReview.StatusLabel)
	}

	// KR 未指定负责人时提交入池 422
	resp = doJSON(t, carol, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items: []api.CreateTaskItem{
			{KeyResultId: krNoOwner, Name: "无人可审的任务", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	if e := decodeBody[api.Error](t, resp); e.Code != "kr_owner_missing" {
		t.Fatalf("code = %q, want kr_owner_missing", e.Code)
	}

	// 不提交入池时保存为草稿
	resp = doJSON(t, carol, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: false,
		Items: []api.CreateTaskItem{
			{KeyResultId: krNoOwner, Name: "先存草稿的任务", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	list = decodeBody[[]api.Task](t, resp)
	var draft *api.Task
	for i := range list {
		if list[i].Name == "先存草稿的任务" {
			draft = &list[i]
		}
	}
	if draft == nil || draft.Status != api.TaskStatusDraft || draft.PoolReview != nil {
		t.Fatalf("草稿保存异常: %+v", draft)
	}

	// 校验失败整批不落库：截止早于开始 422
	resp = doJSON(t, carol, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: false,
		Items: []api.CreateTaskItem{
			{KeyResultId: krWithOwner, Name: "倒置周期", OwnerId: carolUser.ID, StartDate: end, EndDate: start},
		},
	})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	if e := decodeBody[api.Error](t, resp); e.Code != "invalid_task" {
		t.Fatalf("code = %q, want invalid_task", e.Code)
	}

	// 访客不能创建任务 → 403
	daveUser := seedUser(t, q, "dave", "赵六", "dave-pass")
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
		api.AddProjectMembersRequest{UserIds: []int64{daveUser.ID}, Role: api.Viewer})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()
	dave := newClient(t)
	login(dave, "dave", "dave-pass")
	resp = doJSON(t, dave, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: false,
		Items: []api.CreateTaskItem{
			{KeyResultId: krWithOwner, Name: "越权任务", OwnerId: daveUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()

	// 访客可查看任务列表
	resp = doJSON(t, dave, http.MethodGet, tasksURL, nil)
	wantStatus(t, resp, http.StatusOK)
	if got := decodeBody[[]api.Task](t, resp); len(got) != 3 {
		t.Fatalf("任务列表数量异常: %+v", got)
	}
}

// 任务创建邀请（#5，AC-03；MW-19）：KR 负责人邀请成员→受邀人通过邀请创建并提交任务入池→邀请完成；
// 撤回后不可再响应；无关任务不使邀请结束；项目成员不可发邀请。
func TestTaskInviteLifecycle(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	carolUser := seedUser(t, q, "carol", "王五", "carol-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	login := func(c *http.Client, username, password string) {
		t.Helper()
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	alice, bob, carol := newClient(t), newClient(t), newClient(t)
	login(alice, "alice", "alice-pass")
	login(bob, "bob", "bob-pass")
	login(carol, "carol", "carol-pass")

	sp := func(s string) *string { return &s }

	// alice 建项目；bob、carol 项目成员；bob 任 KR1 负责人，另建无负责人的 KR2
	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "邀请试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	for _, uid := range []int64{bobUser.ID, carolUser.ID} {
		resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
			api.AddProjectMembersRequest{UserIds: []int64{uid}, Role: api.Member})
		wantStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{
				{Description: "上线自动验收", OwnerId: &bobUser.ID},
				{Description: "回归通过率达标", OwnerId: &bobUser.ID},
			}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1, kr2 := okr[0].KeyResults[0].Id, okr[0].KeyResults[1].Id
	invitesURL := fmt.Sprintf("%s/projects/%d/task-invites", base, created.Id)
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-12"), openapiDate(t, "2026-09-21")

	// 项目成员 carol（非 KR 负责人）发邀请 403
	resp = doJSON(t, carol, http.MethodPost, invitesURL, api.CreateTaskInvitesRequest{KeyResultId: kr1, InviteeIds: []int64{bobUser.ID}})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()

	// KR 负责人 bob 邀请 carol（KR 尚无任务也可邀请）
	note := "请结合你负责的工作，在该 KR 下补充需要推进的任务。"
	resp = doJSON(t, bob, http.MethodPost, invitesURL, api.CreateTaskInvitesRequest{KeyResultId: kr1, InviteeIds: []int64{carolUser.ID}, Note: &note})
	wantStatus(t, resp, http.StatusCreated)
	invites := decodeBody[[]api.TaskInvite](t, resp)
	if len(invites) != 1 || invites[0].State != api.TaskInviteStatePending || invites[0].InviteeName != "王五" {
		t.Fatalf("邀请创建异常: %+v", invites)
	}
	// AC-03（#83）：受邀人收到带 KR 编号、名称与邀请说明的站内通知；非受邀人没有。
	resp = doJSON(t, carol, http.MethodGet, base+"/notifications", nil)
	wantStatus(t, resp, http.StatusOK)
	carolNotes := decodeBody[[]api.Notification](t, resp)
	if len(carolNotes) != 1 || carolNotes[0].Kind != "task_invite" {
		t.Fatalf("受邀人应收到任务创建邀请通知: %+v", carolNotes)
	}
	if !strings.Contains(carolNotes[0].Content, "KR1.1「上线自动验收」") || !strings.Contains(carolNotes[0].Content, note) {
		t.Fatalf("邀请通知应带 KR 与邀请说明: %q", carolNotes[0].Content)
	}
	resp = doJSON(t, alice, http.MethodGet, base+"/notifications", nil)
	wantStatus(t, resp, http.StatusOK)
	if others := decodeBody[[]api.Notification](t, resp); len(others) != 0 {
		t.Fatalf("非受邀人不应收到邀请通知: %+v", others)
	}
	inviteID := invites[0].Id
	if invites[0].CanHandle {
		t.Fatalf("邀请人视角不应可响应: %+v", invites[0])
	}

	// 受邀人 carol 视角可响应，邀请说明保留
	resp = doJSON(t, carol, http.MethodGet, invitesURL, nil)
	wantStatus(t, resp, http.StatusOK)
	got := decodeBody[[]api.TaskInvite](t, resp)
	if !got[0].CanHandle || got[0].Note == nil || *got[0].Note != note {
		t.Fatalf("受邀人视角异常: %+v", got[0])
	}

	// carol 在无关 KR 提交任务，邀请不结束（词汇表）
	resp = doJSON(t, carol, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr2, Name: "与邀请无关的任务", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodGet, invitesURL, nil)
	wantStatus(t, resp, http.StatusOK)
	if got = decodeBody[[]api.TaskInvite](t, resp); got[0].State != api.TaskInviteStatePending {
		t.Fatalf("无关任务不应使邀请结束: %+v", got[0])
	}

	// 他人（bob）带 carol 的邀请 ID 提交 403
	resp = doJSON(t, bob, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true, TaskInviteId: &inviteID,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "冒名响应", OwnerId: bobUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()

	// 批内无指定 KR 任务 422
	resp = doJSON(t, carol, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true, TaskInviteId: &inviteID,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr2, Name: "跑偏的响应", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	if e := decodeBody[api.Error](t, resp); e.Code != "invalid_invite_response" {
		t.Fatalf("code = %q, want invalid_invite_response", e.Code)
	}

	// AC-03：carol 通过邀请在 KR1 创建并提交 → 任务待入池审批、邀请完成
	resp = doJSON(t, carol, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true, TaskInviteId: &inviteID,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "验证现场联动异常回退", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	tasks := decodeBody[[]api.Task](t, resp)
	var invited *api.Task
	for i := range tasks {
		if tasks[i].Name == "验证现场联动异常回退" {
			invited = &tasks[i]
		}
	}
	if invited == nil || invited.Status != api.TaskStatusPendingPoolReview {
		t.Fatalf("邀请响应任务状态异常: %+v", tasks)
	}
	resp = doJSON(t, carol, http.MethodGet, invitesURL, nil)
	wantStatus(t, resp, http.StatusOK)
	if got = decodeBody[[]api.TaskInvite](t, resp); got[0].State != api.TaskInviteStateCompleted {
		t.Fatalf("提交后邀请应完成: %+v", got[0])
	}

	// 已完成邀请再响应 409
	resp = doJSON(t, carol, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true, TaskInviteId: &inviteID,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "重复响应", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()

	// 撤回流程：bob 再邀请 carol，然后撤回；carol 响应 409；重复撤回 409
	resp = doJSON(t, bob, http.MethodPost, invitesURL, api.CreateTaskInvitesRequest{KeyResultId: kr1, InviteeIds: []int64{carolUser.ID}})
	wantStatus(t, resp, http.StatusCreated)
	invites = decodeBody[[]api.TaskInvite](t, resp)
	second := invites[0].Id
	if invites[0].State != api.TaskInviteStatePending || !invites[0].CanRevoke {
		t.Fatalf("第二次邀请异常: %+v", invites[0])
	}
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/revoke", invitesURL, second), nil)
	wantStatus(t, resp, http.StatusOK)
	if iv := decodeBody[api.TaskInvite](t, resp); iv.State != api.TaskInviteStateRevoked {
		t.Fatalf("撤回后状态异常: %+v", iv)
	}
	// 撤回不补发通知（#83；与 #5 的撤回口径一致）：只有两次「发出」各留一条。
	resp = doJSON(t, carol, http.MethodGet, base+"/notifications", nil)
	wantStatus(t, resp, http.StatusOK)
	inviteNotes := 0
	for _, n := range decodeBody[[]api.Notification](t, resp) {
		if n.Kind == "task_invite" {
			inviteNotes++
		}
	}
	if inviteNotes != 2 {
		t.Fatalf("撤回不应补发通知，两次发出各一条: %d", inviteNotes)
	}
	resp = doJSON(t, carol, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true, TaskInviteId: &second,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "响应已撤回邀请", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/revoke", invitesURL, second), nil)
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()

	// 邀请自己 422；邀请访客 422
	daveUser := seedUser(t, q, "dave", "赵六", "dave-pass")
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
		api.AddProjectMembersRequest{UserIds: []int64{daveUser.ID}, Role: api.Viewer})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodPost, invitesURL, api.CreateTaskInvitesRequest{KeyResultId: kr1, InviteeIds: []int64{bobUser.ID}})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodPost, invitesURL, api.CreateTaskInvitesRequest{KeyResultId: kr1, InviteeIds: []int64{daveUser.ID}})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	if e := decodeBody[api.Error](t, resp); e.Code != "invalid_invitees" {
		t.Fatalf("code = %q, want invalid_invitees", e.Code)
	}
}

// 任务状态与进度（#6，AC-12；AC-06 展开层所需的任务、负责人、状态、进度事实由此供给）：
// 开始执行、可空进度、取消保留原因、KR 覆盖度派生。
func TestTaskStatusAndProgress(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	carolUser := seedUser(t, q, "carol", "王五", "carol-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	login := func(c *http.Client, username, password string) {
		t.Helper()
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	alice, bob, carol := newClient(t), newClient(t), newClient(t)
	login(alice, "alice", "alice-pass")
	login(bob, "bob", "bob-pass")
	login(carol, "carol", "carol-pass")

	sp := func(s string) *string { return &s }
	ipt := func(v int) *int { return &v }

	// alice 建项目；bob、carol 成员；bob 任 KR 负责人
	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "进度试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	for _, uid := range []int64{bobUser.ID, carolUser.ID} {
		resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
			api.AddProjectMembersRequest{UserIds: []int64{uid}, Role: api.Member})
		wantStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	// bob（KR 负责人）免审建两个任务：一个自己负责、一个 carol 负责
	resp = doJSON(t, bob, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "验收脚本编写", OwnerId: bobUser.ID, StartDate: start, EndDate: end},
			{KeyResultId: kr1, Name: "联调环境准备", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	tasks := decodeBody[[]api.Task](t, resp)
	if len(tasks) != 2 {
		t.Fatalf("任务数量异常: %+v", tasks)
	}
	var bobTask, carolTask api.Task
	for _, task := range tasks {
		if task.OwnerId == bobUser.ID {
			bobTask = task
		} else {
			carolTask = task
		}
	}
	if !bobTask.CanStart || bobTask.CanUpdateProgress {
		t.Fatalf("未开始任务派生标志异常: %+v", bobTask)
	}

	// 非负责人 carol 开始 bob 的任务 403
	statusURL := func(id int64) string { return fmt.Sprintf("%s/%d/update-status", tasksURL, id) }
	progressURL := func(id int64) string { return fmt.Sprintf("%s/%d/progress", tasksURL, id) }
	resp = doJSON(t, carol, http.MethodPost, statusURL(bobTask.Id), api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()

	// 负责人开始执行：未开始 → 进行中
	resp = doJSON(t, bob, http.MethodPost, statusURL(bobTask.Id), api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress})
	wantStatus(t, resp, http.StatusOK)
	started := decodeBody[api.Task](t, resp)
	if started.Status != api.TaskStatusInProgress || !started.CanUpdateProgress {
		t.Fatalf("开始执行后状态异常: %+v", started)
	}

	// 重复开始 409
	resp = doJSON(t, bob, http.MethodPost, statusURL(bobTask.Id), api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress})
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()

	// 未开始的任务不能填进度 409（系统不虚构进度）
	resp = doJSON(t, carol, http.MethodPut, progressURL(carolTask.Id), api.UpdateTaskProgressRequest{Progress: ipt(50)})
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()

	// 负责人填进度；非法值 422；清除进度
	resp = doJSON(t, bob, http.MethodPut, progressURL(bobTask.Id), api.UpdateTaskProgressRequest{Progress: ipt(45)})
	wantStatus(t, resp, http.StatusOK)
	if got := decodeBody[api.Task](t, resp); got.Progress == nil || *got.Progress != 45 {
		t.Fatalf("进度未保存: %+v", got)
	}
	resp = doJSON(t, bob, http.MethodPut, progressURL(bobTask.Id), api.UpdateTaskProgressRequest{Progress: ipt(101)})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()

	// KR 汇总（AC-63）：2 个入池任务、1 个已填 45、1 个未填按 0 计入 → 平均 23
	resp = doJSON(t, alice, http.MethodGet, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	okr = decodeBody[[]api.Objective](t, resp)
	summary := okr[0].KeyResults[0].ProgressSummary
	if summary == nil || summary.TotalTasks != 2 || summary.FilledTasks != 1 || summary.AverageProgress == nil || *summary.AverageProgress != 23 {
		t.Fatalf("KR 覆盖度异常: %+v", summary)
	}

	// 清除进度后覆盖度归零
	resp = doJSON(t, bob, http.MethodPut, progressURL(bobTask.Id), api.UpdateTaskProgressRequest{})
	wantStatus(t, resp, http.StatusOK)
	if got := decodeBody[api.Task](t, resp); got.Progress != nil {
		t.Fatalf("进度未清除: %+v", got)
	}

	// 取消改走所属 KR 负责人审批（AC-57）：原因必填 422
	cancelURL := func(id int64) string { return fmt.Sprintf("%s/%d/cancellation", tasksURL, id) }
	resp = doJSON(t, carol, http.MethodPost, cancelURL(carolTask.Id), api.TaskCancellationRequest{Reason: "   "})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()

	// 任务负责人发起：任务状态不变，生成待 KR 负责人审批的关闭单
	reason := "需求合并，不再单独执行"
	resp = doJSON(t, carol, http.MethodPost, cancelURL(carolTask.Id), api.TaskCancellationRequest{Reason: reason})
	wantStatus(t, resp, http.StatusOK)
	requested := decodeBody[api.Task](t, resp)
	if requested.Status == api.TaskStatusCancelled {
		t.Fatalf("关闭申请不应即时生效: %+v", requested)
	}
	if requested.FieldChange == nil || requested.FieldChange.ChangeType != api.Cancel ||
		requested.FieldChange.State != api.FieldChangeStatePending {
		t.Fatalf("未生成待审批关闭单: %+v", requested.FieldChange)
	}
	if len(requested.FieldChange.Changes) != 1 || requested.FieldChange.Changes[0].Field != "status" ||
		requested.FieldChange.Changes[0].NewValue != "已关闭" {
		t.Fatalf("关闭单差异行异常: %+v", requested.FieldChange.Changes)
	}
	// 互斥：待审批关闭单在时，编辑与再次发起关闭的入口都关闭
	if requested.CanProposeFieldChange || requested.CanCancel {
		t.Fatalf("未决关闭单期间不应保留其他审批入口: %+v", requested)
	}
	resp = doJSON(t, carol, http.MethodPost, cancelURL(carolTask.Id), api.TaskCancellationRequest{Reason: reason})
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()

	// KR 负责人 bob 通过关闭单：任务进入已关闭并保留原因
	resp = doJSON(t, bob, http.MethodPost,
		fmt.Sprintf("%s/%d/field-changes/%d/decision", tasksURL, carolTask.Id, requested.FieldChange.Id),
		api.FieldChangeDecisionRequest{Decision: api.FieldChangeDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusOK)
	cancelled := decodeBody[api.Task](t, resp)
	if cancelled.Status != api.TaskStatusCancelled || cancelled.CancelReason == nil || *cancelled.CancelReason != reason {
		t.Fatalf("取消后状态异常: %+v", cancelled)
	}
	resp = doJSON(t, alice, http.MethodGet, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	okr = decodeBody[[]api.Objective](t, resp)
	// AC-63：已关闭整体剔除，剩下的一个任务未填进度按 0 计入
	if s2 := okr[0].KeyResults[0].ProgressSummary; s2 == nil || s2.TotalTasks != 1 || s2.FilledTasks != 0 ||
		s2.AverageProgress == nil || *s2.AverageProgress != 0 {
		t.Fatalf("取消后覆盖度异常: %+v", okr[0].KeyResults[0].ProgressSummary)
	}

	// 已关闭任务不可再发起关闭 409
	resp = doJSON(t, carol, http.MethodPost, cancelURL(carolTask.Id), api.TaskCancellationRequest{Reason: reason})
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()

	// KR 负责人在本人负责 KR 下关闭免审即时生效，且仍留一张已通过的单
	resp = doJSON(t, bob, http.MethodPost, cancelURL(bobTask.Id), api.TaskCancellationRequest{Reason: "并入其他任务"})
	wantStatus(t, resp, http.StatusOK)
	exempt := decodeBody[api.Task](t, resp)
	if exempt.Status != api.TaskStatusCancelled {
		t.Fatalf("KR 负责人取消应免审即时生效: %+v", exempt)
	}
}

// 任务详情（#7，AC-31/AC-34）：全体成员（含只读）可查看基础信息与审核记录，
// 当前环节/待行动人派生，动作标志按业务权限区分。
func TestTaskDetail(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	daveUser := seedUser(t, q, "dave", "赵六", "dave-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	login := func(c *http.Client, username, password string) {
		t.Helper()
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	alice, bob, dave := newClient(t), newClient(t), newClient(t)
	login(alice, "alice", "alice-pass")
	login(bob, "bob", "bob-pass")
	login(dave, "dave", "dave-pass")

	sp := func(s string) *string { return &s }

	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "详情试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
		api.AddProjectMembersRequest{UserIds: []int64{bobUser.ID}, Role: api.Member})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
		api.AddProjectMembersRequest{UserIds: []int64{daveUser.ID}, Role: api.Viewer})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	// alice 创建任务（负责人 bob）→ 待入池审批
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "验证现场联动异常回退", OwnerId: bobUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	tasks := decodeBody[[]api.Task](t, resp)
	taskID := tasks[0].Id
	// AC-04：当前环节文案同样显示当前审批人姓名（入池审批人是 KR 负责人 bob／李四）。
	if tasks[0].CurrentStage != "待李四审批" || tasks[0].PendingActorName == nil || *tasks[0].PendingActorName != "李四" {
		t.Fatalf("当前环节/待行动人派生异常: %+v", tasks[0])
	}

	// 访客 dave 可查看详情（AC-34），但无任何动作标志
	detailURL := fmt.Sprintf("%s/%d", tasksURL, taskID)
	resp = doJSON(t, dave, http.MethodGet, detailURL, nil)
	wantStatus(t, resp, http.StatusOK)
	detail := decodeBody[api.TaskDetail](t, resp)
	if detail.ObjectiveTitle != "提升交付质量" || detail.KrDescription != "上线自动验收" {
		t.Fatalf("O/KR 归属派生异常: %+v", detail)
	}
	if len(detail.PoolReviews) != 1 || detail.PoolReviews[0].Status != api.PoolReviewStatusPending {
		t.Fatalf("审核记录异常: %+v", detail.PoolReviews)
	}
	if detail.Task.CanDecidePoolReview || detail.Task.CanSubmitPoolReview || detail.Task.CanStart || detail.Task.CanCancel {
		t.Fatalf("访客不应有任何动作标志: %+v", detail.Task)
	}

	// KR 负责人 bob 视角出现审批动作（AC-34 操作按钮按权限出现）
	resp = doJSON(t, bob, http.MethodGet, detailURL, nil)
	wantStatus(t, resp, http.StatusOK)
	if d := decodeBody[api.TaskDetail](t, resp); !d.Task.CanDecidePoolReview {
		t.Fatalf("KR 负责人应可处理审批: %+v", d.Task)
	}

	// 审核历史累积：退回→重提后详情含两条记录
	decisionURL := fmt.Sprintf("%s/%d/pool-review-decision", tasksURL, taskID)
	op := "先补充完成标准"
	resp = doJSON(t, bob, http.MethodPost, decisionURL, api.PoolReviewDecisionRequest{Decision: api.PoolReviewDecisionRequestDecisionRejected, Opinion: &op})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/%d/submit-pool-review", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, dave, http.MethodGet, detailURL, nil)
	wantStatus(t, resp, http.StatusOK)
	detail = decodeBody[api.TaskDetail](t, resp)
	if len(detail.PoolReviews) != 2 || detail.PoolReviews[0].Status != api.PoolReviewStatusPending || detail.PoolReviews[1].Status != api.PoolReviewStatusRejected {
		t.Fatalf("审核记录顺序异常: %+v", detail.PoolReviews)
	}
	if detail.PoolReviews[1].Opinion == nil || *detail.PoolReviews[1].Opinion != op {
		t.Fatalf("退回意见未保留: %+v", detail.PoolReviews[1])
	}

	// 不存在的任务 404
	resp = doJSON(t, alice, http.MethodGet, fmt.Sprintf("%s/99999", tasksURL), nil)
	wantStatus(t, resp, http.StatusNotFound)
	resp.Body.Close()
}

// 关键字段修改审批（#12，AC-23）：审批期间旧值生效，通过后新值生效，退回后拟议值作废并
// 派生退回待处理事项；KR 负责人本人免审即时生效；草稿直接完善。
func TestFieldChangeApproval(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	carolUser := seedUser(t, q, "carol", "王五", "carol-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	login := func(c *http.Client, username, password string) {
		t.Helper()
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	alice, bob, carol := newClient(t), newClient(t), newClient(t)
	login(alice, "alice", "alice-pass")
	login(bob, "bob", "bob-pass")
	login(carol, "carol", "carol-pass")

	sp := func(s string) *string { return &s }

	// alice 建项目；bob、carol 成员；bob 任 KR 负责人
	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "变更试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	for _, uid := range []int64{bobUser.ID, carolUser.ID} {
		resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
			api.AddProjectMembersRequest{UserIds: []int64{uid}, Role: api.Member})
		wantStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	// carol 创建草稿（不提交）→ 草稿直接完善不生成变更单
	resp = doJSON(t, carol, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: false,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "初稿任务", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	tasks := decodeBody[[]api.Task](t, resp)
	taskID := tasks[0].Id
	changeURL := func(id int64) string { return fmt.Sprintf("%s/%d/field-changes", tasksURL, id) }
	draftEdit := api.SubmitFieldChangeRequest{}
	draftEdit.Changes.Name = sp("验证现场联动异常回退")
	draftEdit.Changes.Description = sp("覆盖联动断链后的自动回退")
	resp = doJSON(t, carol, http.MethodPost, changeURL(taskID), draftEdit)
	wantStatus(t, resp, http.StatusOK)
	edited := decodeBody[api.Task](t, resp)
	if edited.Name != "验证现场联动异常回退" || edited.FieldChange != nil {
		t.Fatalf("草稿完善异常: %+v", edited)
	}

	// 提交入池并通过，任务进入未开始
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/submit-pool-review", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/pool-review-decision", tasksURL, taskID),
		api.PoolReviewDecisionRequest{Decision: api.PoolReviewDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	// carol 提交关键字段修改（改截止时间）：原因必填 422
	newEnd := openapiDate(t, "2026-10-15")
	noReason := api.SubmitFieldChangeRequest{}
	noReason.Changes.EndDate = &newEnd
	resp = doJSON(t, carol, http.MethodPost, changeURL(taskID), noReason)
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()

	// 带原因提交 → 待审批，旧值继续生效（AC-23）
	withReason := api.SubmitFieldChangeRequest{Reason: sp("联调窗口顺延")}
	withReason.Changes.EndDate = &newEnd
	resp = doJSON(t, carol, http.MethodPost, changeURL(taskID), withReason)
	wantStatus(t, resp, http.StatusOK)
	pendingTask := decodeBody[api.Task](t, resp)
	if pendingTask.EndDate.Time.Format("2006-01-02") != "2026-09-30" {
		t.Fatalf("审批期间旧值应继续生效: %+v", pendingTask.EndDate)
	}
	if pendingTask.FieldChange == nil || pendingTask.FieldChange.State != api.FieldChangeStatePending {
		t.Fatalf("拟议值标示缺失: %+v", pendingTask.FieldChange)
	}
	if len(pendingTask.FieldChange.Changes) != 1 || pendingTask.FieldChange.Changes[0].NewValue != "2026-10-15" {
		t.Fatalf("差异快照异常: %+v", pendingTask.FieldChange.Changes)
	}
	changeID := pendingTask.FieldChange.Id

	// 重复提交 409
	resp = doJSON(t, carol, http.MethodPost, changeURL(taskID), withReason)
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()

	// 管理员 alice 不能替代 KR 负责人审批 403
	decisionURL := fmt.Sprintf("%s/%d/decision", changeURL(taskID), changeID)
	resp = doJSON(t, alice, http.MethodPost, decisionURL, api.FieldChangeDecisionRequest{Decision: api.FieldChangeDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()

	// KR 负责人退回 → 拟议值作废、旧值不变、退回待处理事项出现（AC-23）
	op := "顺延理由不充分"
	resp = doJSON(t, bob, http.MethodPost, decisionURL, api.FieldChangeDecisionRequest{Decision: api.FieldChangeDecisionRequestDecisionRejected, Opinion: &op})
	wantStatus(t, resp, http.StatusOK)
	rejected := decodeBody[api.Task](t, resp)
	if rejected.EndDate.Time.Format("2006-01-02") != "2026-09-30" {
		t.Fatalf("退回后旧值应保持不变: %+v", rejected.EndDate)
	}
	if rejected.FieldChange == nil || rejected.FieldChange.State != api.FieldChangeStateRejected || rejected.FieldChange.Resolved {
		t.Fatalf("退回待处理事项缺失: %+v", rejected.FieldChange)
	}
	if rejected.FieldChange.CanAbandon == nil || *rejected.FieldChange.CanAbandon {
		t.Fatalf("非提交人视角不应可放弃: %+v", rejected.FieldChange)
	}

	// carol 重新提交 → 旧退回单 resolved，新单待审批；KR 负责人通过 → 新值生效
	resp = doJSON(t, carol, http.MethodPost, changeURL(taskID), withReason)
	wantStatus(t, resp, http.StatusOK)
	pendingTask = decodeBody[api.Task](t, resp)
	if pendingTask.FieldChange == nil || pendingTask.FieldChange.State != api.FieldChangeStatePending {
		t.Fatalf("重新提交后应有新待审批单: %+v", pendingTask.FieldChange)
	}
	newChangeID := pendingTask.FieldChange.Id
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/decision", changeURL(taskID), newChangeID),
		api.FieldChangeDecisionRequest{Decision: api.FieldChangeDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusOK)
	approved := decodeBody[api.Task](t, resp)
	if approved.EndDate.Time.Format("2006-01-02") != "2026-10-15" {
		t.Fatalf("通过后新值应生效: %+v", approved.EndDate)
	}
	if approved.FieldChange != nil {
		t.Fatalf("已通过后不应再有需要关注的变更单: %+v", approved.FieldChange)
	}

	// 详情含全部变更历史（含旧退回单已 resolved）
	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	detail := decodeBody[api.TaskDetail](t, resp)
	if len(detail.FieldChanges) != 2 {
		t.Fatalf("变更历史数量异常: %+v", detail.FieldChanges)
	}
	if detail.FieldChanges[1].State != api.FieldChangeStateRejected || !detail.FieldChanges[1].Resolved {
		t.Fatalf("旧退回单应已随重新提交处理: %+v", detail.FieldChanges[1])
	}

	// KR 负责人 bob 免审即时生效并留一张已通过免审变更单
	exemptEdit := api.SubmitFieldChangeRequest{Reason: sp("负责人调整")}
	exemptEdit.Changes.OwnerId = &bobUser.ID
	resp = doJSON(t, bob, http.MethodPost, changeURL(taskID), exemptEdit)
	wantStatus(t, resp, http.StatusOK)
	exempted := decodeBody[api.Task](t, resp)
	if exempted.OwnerId != bobUser.ID {
		t.Fatalf("免审修改应即时生效: %+v", exempted)
	}
	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	detail = decodeBody[api.TaskDetail](t, resp)
	if detail.FieldChanges[0].State != api.FieldChangeStateApproved || !detail.FieldChanges[0].Exempt {
		t.Fatalf("免审变更单异常: %+v", detail.FieldChanges[0])
	}
	if detail.FieldChanges[0].Changes[0].OldValue != "王五" || detail.FieldChanges[0].Changes[0].NewValue != "李四" {
		t.Fatalf("负责人差异应显示姓名: %+v", detail.FieldChanges[0].Changes)
	}
}

// 交付物模型与文件存取（#8，AC-32/AC-33）：交付物项、候选登记与预签名地址、
// 多项当前交付物展示、候选仅提示审核中。当前内容的生成流转在 #10 终审实现，
// 此处经 store 层直接种入已生效内容验证展示端语义。
func TestDeliverablesAndFiles(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	login := func(c *http.Client, username, password string) {
		t.Helper()
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	alice, bob := newClient(t), newClient(t)
	login(alice, "alice", "alice-pass")
	login(bob, "bob", "bob-pass")

	sp := func(s string) *string { return &s }

	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "交付物试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
		api.AddProjectMembersRequest{UserIds: []int64{bobUser.ID}, Role: api.Member})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	// 创建任务时带预期交付物 → 自动建交付物项并出现在列表列
	resp = doJSON(t, bob, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "输出验收方案", OwnerId: bobUser.ID, StartDate: start, EndDate: end, ExpectedDeliverable: sp("验收方案 V1")},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	tasks := decodeBody[[]api.Task](t, resp)
	taskID := tasks[0].Id
	if tasks[0].DeliverableNames == nil || (*tasks[0].DeliverableNames)[0] != "验收方案 V1" {
		t.Fatalf("预期交付物列异常: %+v", tasks[0].DeliverableNames)
	}

	// 再补一个交付物项（一个任务多项交付物）；bob 是所属 KR 负责人，免审即时生效
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/deliverables", tasksURL, taskID),
		api.CreateDeliverableRequest{FileName: "现场验收记录.docx"})
	wantStructureAccepted(t, resp)
	second := deliverableOf(t, bob, base, created.Id, taskID, "现场验收记录")

	// 空文件名 422
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/deliverables", tasksURL, taskID),
		api.CreateDeliverableRequest{FileName: "  "})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()

	// 同名文件不静默新建第二项（裁决 G1）：项名派生后与已有项重名 422
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/deliverables", tasksURL, taskID),
		api.CreateDeliverableRequest{FileName: "现场验收记录.pdf"})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()

	// 任务开始执行后，负责人登记候选内容并取得预签名上传地址
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/update-status", tasksURL, taskID),
		api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	candidateURL := fmt.Sprintf("%s/%d/deliverables/%d/candidate", tasksURL, taskID, second.Id)
	resp = doJSON(t, bob, http.MethodPost, candidateURL,
		api.UploadCandidateRequest{FileName: "现场验收记录.xlsx", FileType: sp("xlsx")})
	wantStatus(t, resp, http.StatusCreated)
	up := decodeBody[api.UploadCandidateResponse](t, resp)
	if up.UploadUrl == "" || up.File.State != api.DeliverableFileStateUploading {
		t.Fatalf("候选登记异常: %+v", up)
	}
	// R4：文件没真的上传就点确认，必须被拒；此时也还不是候选内容
	resp = doJSON(t, bob, http.MethodPost, candidateURL+"/commit", api.CommitUploadRequest{FileId: up.File.Id})
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	if d := decodeBody[api.TaskDetail](t, resp); d.Deliverables[1].Candidate != nil {
		t.Fatalf("未确认上传不应成为候选内容: %+v", d.Deliverables[1].Candidate)
	}
	putObject(t, up.UploadUrl, "candidate-bytes")
	resp = doJSON(t, bob, http.MethodPost, candidateURL+"/commit", api.CommitUploadRequest{FileId: up.File.Id})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	// 非负责人 alice 也可（管理员纠错）；无关成员会被拒——此处验证重复登记覆盖旧候选
	uploadCandidate(t, bob, tasksURL, taskID, second.Id, api.UploadCandidateRequest{FileName: "现场验收记录-rev2.xlsx"}, "candidate-bytes")

	// 种入一份已生效当前内容（#10 终审前的展示语义验证）
	ctx := context.Background()
	deliverables, err := q.ListDeliverablesByTask(ctx, taskID)
	if err != nil || len(deliverables) != 2 {
		t.Fatalf("交付物项异常: %v %+v", err, deliverables)
	}
	firstID := deliverables[0].ID
	cur, err := q.CreateDeliverableFile(ctx, store.CreateDeliverableFileParams{
		DeliverableID: firstID, State: "current", FileName: "验收方案V1.docx", FileType: "docx",
		FileSize: 2048, ObjectKey: "deliverables/test/current.docx", UploadedBy: bobUser.ID,
	})
	if err != nil {
		t.Fatalf("seed current: %v", err)
	}

	// AC-32/33：详情列出全部交付物项；第一项有当前内容，第二项只有候选（概况仅提示由前端负责）
	resp = doJSON(t, alice, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	detail := decodeBody[api.TaskDetail](t, resp)
	if len(detail.Deliverables) != 2 {
		t.Fatalf("交付物项数量异常: %+v", detail.Deliverables)
	}
	if detail.Deliverables[0].Current == nil || detail.Deliverables[0].Current.FileName != "验收方案V1.docx" {
		t.Fatalf("当前内容缺失: %+v", detail.Deliverables[0])
	}
	if detail.Deliverables[1].Candidate == nil || detail.Deliverables[1].Candidate.FileName != "现场验收记录-rev2.xlsx" {
		t.Fatalf("候选覆盖异常: %+v", detail.Deliverables[1])
	}
	// 裁决 G：项名取首次建项时的文件名，成果更新只换内容不改名——
	// 建项文件是「现场验收记录.docx」，此后传了 .xlsx 与 -rev2.xlsx，项名仍是「现场验收记录」。
	if detail.Deliverables[1].Name != "现场验收记录" {
		t.Fatalf("成果更新不应改项名: %+v", detail.Deliverables[1].Name)
	}
	if detail.Deliverables[1].Current != nil {
		t.Fatalf("候选不应提前成为当前内容: %+v", detail.Deliverables[1])
	}

	// 任一项目成员可取预签名下载地址（§3.3）
	resp = doJSON(t, alice, http.MethodGet, fmt.Sprintf("%s/projects/%d/files/%d/download-url", base, created.Id, cur.ID), nil)
	wantStatus(t, resp, http.StatusOK)
	if u := decodeBody[api.DownloadUrlResponse](t, resp); u.Url == "" {
		t.Fatalf("下载地址为空")
	}

	// 不存在的文件 404
	resp = doJSON(t, alice, http.MethodGet, fmt.Sprintf("%s/projects/%d/files/99999/download-url", base, created.Id), nil)
	wantStatus(t, resp, http.StatusNotFound)
	resp.Body.Close()
}

// 任务讨论与定向通知（#9，AC-35/AC-36）：访客可提交意见并 @ 成员；
// 意见不可改删（无端点）；通知只发任务负责人与被 @ 成员并可直达讨论 Tab。
func TestDiscussionsAndNotifications(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	carolUser := seedUser(t, q, "carol", "王五", "carol-pass")
	daveUser := seedUser(t, q, "dave", "赵六", "dave-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	login := func(c *http.Client, username, password string) {
		t.Helper()
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	alice, bob, carol, dave := newClient(t), newClient(t), newClient(t), newClient(t)
	login(alice, "alice", "alice-pass")
	login(bob, "bob", "bob-pass")
	login(carol, "carol", "carol-pass")
	login(dave, "dave", "dave-pass")

	sp := func(s string) *string { return &s }

	// alice 建项目；bob 成员并任 KR 负责人与任务负责人；carol 访客；dave 非成员
	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "讨论试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
		api.AddProjectMembersRequest{UserIds: []int64{bobUser.ID}, Role: api.Member})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
		api.AddProjectMembersRequest{UserIds: []int64{carolUser.ID}, Role: api.Viewer})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")
	resp = doJSON(t, bob, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "输出验收方案", OwnerId: bobUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	tasks := decodeBody[[]api.Task](t, resp)
	taskID := tasks[0].Id
	discussURL := fmt.Sprintf("%s/%d/discussions", tasksURL, taskID)

	// AC-35：访客 carol 提交意见并 @ alice
	resp = doJSON(t, carol, http.MethodPost, discussURL,
		api.CreateDiscussionRequest{Content: "建议补充断链回退场景。", MentionUserIds: &[]int64{aliceUser.ID}})
	wantStatus(t, resp, http.StatusCreated)
	d := decodeBody[api.Discussion](t, resp)
	if d.AuthorName != "王五" || d.MentionNames == nil || (*d.MentionNames)[0] != "张三" {
		t.Fatalf("讨论意见异常: %+v", d)
	}

	// 非项目成员 dave 404：读边界收口后，非成员一律按「项目不存在」处理，不泄露项目是否存在（PRD §3.3）
	resp = doJSON(t, dave, http.MethodPost, discussURL, api.CreateDiscussionRequest{Content: "外部插话"})
	wantStatus(t, resp, http.StatusNotFound)
	resp.Body.Close()

	// 空内容 422；@ 非成员 422
	resp = doJSON(t, carol, http.MethodPost, discussURL, api.CreateDiscussionRequest{Content: "  "})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodPost, discussURL,
		api.CreateDiscussionRequest{Content: "x", MentionUserIds: &[]int64{daveUser.ID}})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()

	// AC-36：通知只发任务负责人 bob 与被 @ 的 alice，携带 taskId 可直达讨论 Tab
	resp = doJSON(t, bob, http.MethodGet, base+"/notifications", nil)
	wantStatus(t, resp, http.StatusOK)
	bobNotes := dropOkrAssigned(decodeBody[[]api.Notification](t, resp))
	if len(bobNotes) != 1 || bobNotes[0].Kind != "discussion_owner" || bobNotes[0].TaskId == nil || *bobNotes[0].TaskId != taskID {
		t.Fatalf("负责人通知异常: %+v", bobNotes)
	}
	resp = doJSON(t, alice, http.MethodGet, base+"/notifications", nil)
	wantStatus(t, resp, http.StatusOK)
	aliceNotes := decodeBody[[]api.Notification](t, resp)
	if len(aliceNotes) != 1 || aliceNotes[0].Kind != "discussion_mention" {
		t.Fatalf("被 @ 通知异常: %+v", aliceNotes)
	}
	resp = doJSON(t, carol, http.MethodGet, base+"/notifications", nil)
	wantStatus(t, resp, http.StatusOK)
	if got := decodeBody[[]api.Notification](t, resp); len(got) != 0 {
		t.Fatalf("作者不应收到通知: %+v", got)
	}

	// 详情讨论 Tab 数据；已提交意见无编辑/删除路径（契约不存在对应端点）
	// 用访客 carol 读取：非成员 dave 已无读权限（读边界收口，PRD §3.3）
	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	detail := decodeBody[api.TaskDetail](t, resp)
	if len(detail.Discussions) != 1 || detail.Discussions[0].Content != "建议补充断链回退场景。" {
		t.Fatalf("详情讨论异常: %+v", detail.Discussions)
	}

	// 已读标记
	resp = doJSON(t, bob, http.MethodPost, base+"/notifications/read-all", nil)
	wantStatus(t, resp, http.StatusNoContent)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodGet, base+"/notifications", nil)
	wantStatus(t, resp, http.StatusOK)
	bobNotes = decodeBody[[]api.Notification](t, resp)
	if bobNotes[0].ReadAt == nil {
		t.Fatalf("通知未标记已读: %+v", bobNotes[0])
	}
}

// 完成申请与 KR 终审（#10，AC-13/15/38/39/40；MW-02／MW-03／MW-18）：提交→直接待 KR 终审→退回删候选回进行中
// →重传重提→通过覆盖当前内容并删除旧文件、任务完成；未包含的当前交付物不变。
func TestCompletionReviewFlow(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	carolUser := seedUser(t, q, "carol", "王五", "carol-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	login := func(c *http.Client, username, password string) {
		t.Helper()
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	alice, bob, carol := newClient(t), newClient(t), newClient(t)
	login(alice, "alice", "alice-pass")
	login(bob, "bob", "bob-pass")
	login(carol, "carol", "carol-pass")

	sp := func(s string) *string { return &s }

	// alice 建项目；bob（KR 负责人）、carol（任务负责人）成员
	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "终审试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	for _, uid := range []int64{bobUser.ID, carolUser.ID} {
		resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
			api.AddProjectMembersRequest{UserIds: []int64{uid}, Role: api.Member})
		wantStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	// carol 建任务（带两个交付物项）→ bob 入池通过 → carol 开始执行
	resp = doJSON(t, carol, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "输出验收方案", OwnerId: carolUser.ID, StartDate: start, EndDate: end, ExpectedDeliverable: sp("验收方案")},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	tasks := decodeBody[[]api.Task](t, resp)
	taskID := tasks[0].Id
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/pool-review-decision", tasksURL, taskID),
		api.PoolReviewDecisionRequest{Decision: api.PoolReviewDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	// 输出属关键字段：已入池任务由所属 KR 负责人 bob 加交付物项，免审即时生效（AC-23）
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/deliverables", tasksURL, taskID),
		api.CreateDeliverableRequest{FileName: "验收记录.docx"})
	wantStructureAccepted(t, resp)
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/update-status", tasksURL, taskID),
		api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	// 无候选提交 422
	completionURL := fmt.Sprintf("%s/%d/completion-reviews", tasksURL, taskID)
	resp = doJSON(t, carol, http.MethodPost, completionURL, api.SubmitCompletionRequest{Note: "第一批成果"})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()

	// 上传两项候选
	detailURL := fmt.Sprintf("%s/%d", tasksURL, taskID)
	resp = doJSON(t, carol, http.MethodGet, detailURL, nil)
	wantStatus(t, resp, http.StatusOK)
	detail := decodeBody[api.TaskDetail](t, resp)
	d1, d2 := detail.Deliverables[0].Id, detail.Deliverables[1].Id
	for i, did := range []int64{d1, d2} {
		uploadCandidate(t, carol, tasksURL, taskID, did, api.UploadCandidateRequest{FileName: fmt.Sprintf("成果-%d.docx", i+1)}, "candidate-bytes")
	}

	// AC-13：提交完成申请直接进入待 KR 终审；canSubmitCompletion 派生
	resp = doJSON(t, carol, http.MethodGet, detailURL, nil)
	wantStatus(t, resp, http.StatusOK)
	detail = decodeBody[api.TaskDetail](t, resp)
	if detail.Task.CanSubmitCompletion == nil || !*detail.Task.CanSubmitCompletion {
		t.Fatalf("负责人应可提交完成申请: %+v", detail.Task)
	}
	resp = doJSON(t, carol, http.MethodPost, completionURL, api.SubmitCompletionRequest{Note: "第一批成果，请终审"})
	wantStatus(t, resp, http.StatusOK)
	pending := decodeBody[api.Task](t, resp)
	if pending.Status != api.TaskStatusPendingFinalReview || pending.CurrentStage != "待李四审批" {
		t.Fatalf("提交后应待 KR 终审: %+v", pending)
	}

	// 审核期间不可另传候选 409
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/deliverables/%d/candidate", tasksURL, taskID, d1),
		api.UploadCandidateRequest{FileName: "偷偷替换.docx"})
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()

	// AC-38：非 KR 负责人不能终审；退回意见必填
	resp = doJSON(t, carol, http.MethodGet, detailURL, nil)
	wantStatus(t, resp, http.StatusOK)
	detail = decodeBody[api.TaskDetail](t, resp)
	reviewID := detail.CompletionReviews[0].Id
	if len(detail.CompletionReviews[0].Items) != 2 {
		t.Fatalf("申请应含全部候选: %+v", detail.CompletionReviews[0].Items)
	}
	decisionURL := fmt.Sprintf("%s/%d/decision", completionURL, reviewID)
	resp = doJSON(t, alice, http.MethodPost, decisionURL, api.CompletionDecisionRequest{Decision: api.CompletionDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodPost, decisionURL, api.CompletionDecisionRequest{Decision: api.CompletionDecisionRequestDecisionRejected})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()

	// AC-40：退回删除候选文件、任务回进行中、审核事实保留
	op := "样例覆盖不足"
	resp = doJSON(t, bob, http.MethodPost, decisionURL, api.CompletionDecisionRequest{Decision: api.CompletionDecisionRequestDecisionRejected, Opinion: &op})
	wantStatus(t, resp, http.StatusOK)
	rejected := decodeBody[api.Task](t, resp)
	if rejected.Status != api.TaskStatusInProgress {
		t.Fatalf("退回后应回进行中: %+v", rejected)
	}
	resp = doJSON(t, carol, http.MethodGet, detailURL, nil)
	wantStatus(t, resp, http.StatusOK)
	detail = decodeBody[api.TaskDetail](t, resp)
	for _, d := range detail.Deliverables {
		if d.Candidate != nil {
			t.Fatalf("退回后候选应删除: %+v", d)
		}
	}
	if detail.CompletionReviews[0].State != api.CompletionReviewStateRejected || detail.CompletionReviews[0].Opinion == nil || *detail.CompletionReviews[0].Opinion != op {
		t.Fatalf("退回事实未保留: %+v", detail.CompletionReviews[0])
	}
	if detail.CompletionReviews[0].Items[0].FileId != nil {
		t.Fatalf("已删除候选不应再提供下载: %+v", detail.CompletionReviews[0].Items[0])
	}

	// 重传候选（仅第一项）并重提 → AC-39/15：通过后候选成为当前内容、任务完成
	uploadCandidate(t, carol, tasksURL, taskID, d1, api.UploadCandidateRequest{FileName: "成果-终版.docx"}, "candidate-bytes")
	// 先给第二项种一份当前内容，验证「未包含的当前交付物不变」
	seeded, err := q.CreateDeliverableFile(context.Background(), store.CreateDeliverableFileParams{
		DeliverableID: d2, State: "current", FileName: "旧成果-2.docx", ObjectKey: "deliverables/test/old-2.docx", UploadedBy: carolUser.ID,
	})
	if err != nil {
		t.Fatalf("seed current: %v", err)
	}
	resp = doJSON(t, carol, http.MethodPost, completionURL, api.SubmitCompletionRequest{Note: "补充样例后重提"})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodGet, detailURL, nil)
	wantStatus(t, resp, http.StatusOK)
	detail = decodeBody[api.TaskDetail](t, resp)
	newReviewID := detail.CompletionReviews[0].Id
	if len(detail.CompletionReviews[0].Items) != 1 {
		t.Fatalf("第二次申请只应含新候选: %+v", detail.CompletionReviews[0].Items)
	}
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/decision", completionURL, newReviewID),
		api.CompletionDecisionRequest{Decision: api.CompletionDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusOK)
	done := decodeBody[api.Task](t, resp)
	if done.Status != api.TaskStatusCompleted || done.CurrentStage != "已闭环" {
		t.Fatalf("终审通过后任务应完成: %+v", done)
	}
	// AC-63：完成即 100% 并锁定编辑
	if done.Progress == nil || *done.Progress != 100 {
		t.Fatalf("终审通过后进度应置 100: %+v", done.Progress)
	}
	if done.CanUpdateProgress {
		t.Fatal("已完成任务不应再显示进度编辑入口")
	}
	resp = doJSON(t, carol, http.MethodPut, fmt.Sprintf("%s/%d/progress", tasksURL, taskID),
		api.UpdateTaskProgressRequest{Progress: func(v int) *int { return &v }(60)})
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodGet, detailURL, nil)
	wantStatus(t, resp, http.StatusOK)
	detail = decodeBody[api.TaskDetail](t, resp)
	var first, secondD *api.Deliverable
	for i := range detail.Deliverables {
		switch detail.Deliverables[i].Id {
		case d1:
			first = &detail.Deliverables[i]
		case d2:
			secondD = &detail.Deliverables[i]
		}
	}
	if first == nil || first.Current == nil || first.Current.FileName != "成果-终版.docx" || first.Current.EffectiveAt == nil {
		t.Fatalf("候选未覆盖为当前内容: %+v", first)
	}
	if first.Candidate != nil {
		t.Fatalf("通过后不应残留候选: %+v", first)
	}
	if secondD == nil || secondD.Current == nil || secondD.Current.Id != seeded.ID {
		t.Fatalf("未包含的当前交付物应保持不变: %+v", secondD)
	}
}

// 必要输入与交付物边（#13，AC-07/28/48；CR-08 双向与环形、CR-12 候选不提前就绪）：
// 选择已有任务及交付物建边、复杂关系（双向/循环/跨 KR）、
// 就绪状态派生（候选不提前满足、当前生效后自动就绪）、必要输入未到显示等待输入。
func TestDeliverableEdgesAndReadiness(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	carolUser := seedUser(t, q, "carol", "王五", "carol-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	login := func(c *http.Client, username, password string) {
		t.Helper()
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	alice, bob, carol := newClient(t), newClient(t), newClient(t)
	login(alice, "alice", "alice-pass")
	login(bob, "bob", "bob-pass")
	login(carol, "carol", "carol-pass")

	sp := func(s string) *string { return &s }

	// 项目：bob 任 KR1 负责人、carol 成员；两个 KR 支持跨 KR 关系
	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "关系试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	for _, uid := range []int64{bobUser.ID, carolUser.ID} {
		resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
			api.AddProjectMembersRequest{UserIds: []int64{uid}, Role: api.Member})
		wantStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{
				{Description: "上线自动验收", OwnerId: &bobUser.ID},
				{Description: "现场回归通过", OwnerId: &bobUser.ID},
			}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1, kr2 := okr[0].KeyResults[0].Id, okr[0].KeyResults[1].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	// bob 免审建上游任务 A（带交付物）与跨 KR 下游任务 B（carol 负责）
	resp = doJSON(t, bob, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "采集现场数据", OwnerId: bobUser.ID, StartDate: start, EndDate: end, ExpectedDeliverable: sp("现场数据包")},
			{KeyResultId: kr2, Name: "回归验证分析", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	tasks := decodeBody[[]api.Task](t, resp)
	var taskA, taskB api.Task
	for _, task := range tasks {
		if task.Name == "采集现场数据" {
			taskA = task
		} else {
			taskB = task
		}
	}

	// 取 A 的交付物项
	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskA.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	detailA := decodeBody[api.TaskDetail](t, resp)
	dA := detailA.Deliverables[0].Id

	// AC-28：B 的负责人 carol 选择 A 及其交付物建立必要输入边；
	// 输入与输入源是关键字段（AC-23），已入池任务先进所属 KR 负责人审批，通过后边才建立
	inputsURL := func(id int64) string { return fmt.Sprintf("%s/%d/inputs", tasksURL, id) }
	resp = doJSON(t, carol, http.MethodPost, inputsURL(taskB.Id), api.CreateTaskInputRequest{
		Necessity: api.Required, EdgeType: api.HardPrerequisite,
		SourceTaskIds: []int64{taskA.Id}, DeliverableId: &dA,
	})
	pendingEdge := wantStructureAccepted(t, resp)
	if len(projectEdges(t, carol, base, created.Id)) != 0 {
		t.Fatal("待审批期间不应先建边")
	}
	approveStructureChange(t, bob, base, created.Id, taskB.Id, pendingEdge)
	edge := edgeOf(t, carol, base, created.Id, taskB.Id, "采集现场数据")
	if edge.Ready || edge.SourceTaskName == nil || *edge.SourceTaskName != "采集现场数据" {
		t.Fatalf("新建边应未就绪且含来源信息: %+v", edge)
	}

	// AC-07：反向再建一条反馈边（双向/循环关系保留真实连线）；bob 是 KR 负责人，免审即时生效
	resp = doJSON(t, bob, http.MethodPost, inputsURL(taskA.Id), api.CreateTaskInputRequest{
		Necessity: api.Reference, EdgeType: api.Feedback, SourceTaskIds: []int64{taskB.Id},
	})
	wantStructureAccepted(t, resp)
	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/projects/%d/edges", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	if edges := decodeBody[[]api.DeliverableEdge](t, resp); len(edges) != 2 {
		t.Fatalf("交付物边数量异常: %+v", edges)
	}

	// 必要输入未到：B 显示等待输入（存储态未变）
	resp = doJSON(t, carol, http.MethodGet, tasksURL, nil)
	wantStatus(t, resp, http.StatusOK)
	list := decodeBody[[]api.Task](t, resp)
	for _, task := range list {
		if task.Id == taskB.Id && task.Status != api.TaskStatusWaitingInput {
			t.Fatalf("必要输入未到应显示等待输入: %+v", task)
		}
		if task.Id == taskA.Id && task.Status != api.TaskStatusNotStarted {
			t.Fatalf("参考输入不应影响 A 的状态: %+v", task)
		}
	}

	// 自环 422；无关成员建边 403
	resp = doJSON(t, carol, http.MethodPost, inputsURL(taskB.Id), api.CreateTaskInputRequest{
		Necessity: api.Required, EdgeType: api.Information, SourceTaskIds: []int64{taskB.Id},
	})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()

	// AC-48：A 走完成终审后当前内容生效 → 边自动就绪、B 不再等待输入
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/update-status", tasksURL, taskA.Id),
		api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	uploadCandidate(t, bob, tasksURL, taskA.Id, dA, api.UploadCandidateRequest{FileName: "现场数据包.zip"}, "candidate-bytes")

	// 仅候选时仍未就绪（AC-48）
	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/projects/%d/edges", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	for _, e := range decodeBody[[]api.DeliverableEdge](t, resp) {
		if e.TargetTaskId == taskB.Id {
			if e.Ready || !e.HasCandidate {
				t.Fatalf("仅候选不应就绪: %+v", e)
			}
		}
	}

	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews", tasksURL, taskA.Id),
		api.SubmitCompletionRequest{Note: "数据包齐"})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskA.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	detailA = decodeBody[api.TaskDetail](t, resp)
	reviewID := detailA.CompletionReviews[0].Id
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews/%d/decision", tasksURL, taskA.Id, reviewID),
		api.CompletionDecisionRequest{Decision: api.CompletionDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskB.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	detailB := decodeBody[api.TaskDetail](t, resp)
	if len(detailB.Inputs) != 1 || !detailB.Inputs[0].Ready {
		t.Fatalf("当前内容生效后输入应自动就绪: %+v", detailB.Inputs)
	}
	if detailB.Inputs[0].CurrentFileId == nil {
		t.Fatalf("边上应关联当前交付物: %+v", detailB.Inputs[0])
	}
	if detailB.Task.Status != api.TaskStatusNotStarted {
		t.Fatalf("输入就绪后应回未开始显示: %+v", detailB.Task.Status)
	}
	if len(detailB.Outputs) != 1 || detailB.Outputs[0].EdgeType != api.Feedback {
		t.Fatalf("B 的下游反馈边异常: %+v", detailB.Outputs)
	}
	// AC-50：协作关系摘要按直接上游／下游分组派生，条目自带对方任务的展示事实
	if len(detailB.Upstream) != 1 {
		t.Fatalf("B 的直接上游分组异常: %+v", detailB.Upstream)
	}
	up := detailB.Upstream[0]
	if up.TaskId != taskA.Id || up.TaskName != "采集现场数据" || up.EdgeType != api.HardPrerequisite ||
		!up.Ready || up.OwnerName != bobUser.DisplayName || up.KrDescription == "" || up.TaskStatusLabel == "" {
		t.Fatalf("直接上游摘要事实异常: %+v", up)
	}
	if len(detailB.Downstream) != 1 || detailB.Downstream[0].TaskId != taskA.Id ||
		detailB.Downstream[0].EdgeType != api.Feedback {
		t.Fatalf("B 的直接下游分组异常: %+v", detailB.Downstream)
	}
	// CR PRD §8.1：受影响 O／KR 只沿下游硬前置边推导——
	// A 硬前置指向 B，所以 A 的详情里有 B 所属 KR；B 只有一条指向 A 的反馈边，不产生受影响目标。
	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskA.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	detailA2 := decodeBody[api.TaskDetail](t, resp)
	if len(detailA2.ImpactedTargets) != 1 || detailA2.ImpactedTargets[0].KrDescription != "现场回归通过" {
		t.Fatalf("A 的受影响 O／KR 异常: %+v", detailA2.ImpactedTargets)
	}
	if len(detailB.ImpactedTargets) != 0 {
		t.Fatalf("反馈边不应产生受影响目标: %+v", detailB.ImpactedTargets)
	}
	// AC-50：任务详情页头的更新时间随任务写入推进
	if !detailA.Task.UpdatedAt.After(taskA.UpdatedAt) {
		t.Fatalf("任务更新时间应随状态推进: %v → %v", taskA.UpdatedAt, detailA.Task.UpdatedAt)
	}

	// 解除边：目标任务 A 已完成（终态）不再接受任何变更单 → 409
	resp = doJSON(t, bob, http.MethodDelete, fmt.Sprintf("%s/projects/%d/edges/%d", base, created.Id, detailB.Outputs[0].Id), nil)
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()
	// 解除输入源同样是关键字段变更：carol 提交，待审批期间边仍在，bob 通过后才真的解除
	resp = doJSON(t, carol, http.MethodDelete, fmt.Sprintf("%s/projects/%d/edges/%d", base, created.Id, detailB.Inputs[0].Id), nil)
	pendingRemove := wantStructureAccepted(t, resp)
	if len(projectEdges(t, carol, base, created.Id)) != 2 {
		t.Fatal("待审批期间不应先解除边")
	}
	approveStructureChange(t, bob, base, created.Id, taskB.Id, pendingRemove)
	if edges := projectEdges(t, carol, base, created.Id); len(edges) != 1 {
		t.Fatalf("解除后边数量异常: %+v", edges)
	}
}

// 中间审核或签与退回（#11，AC-14/24/37；MW-07／MW-18）：配置或签组→提交进入中间审核→任一人通过进待终审
// 且其余待办关闭→终审闭环；退回路径删除候选、意见保留、任务回进行中可重新提交完整流程。
func TestIntermediateReviewOrSign(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	carolUser := seedUser(t, q, "carol", "王五", "carol-pass")
	daveUser := seedUser(t, q, "dave", "赵六", "dave-pass")
	erinUser := seedUser(t, q, "erin", "钱七", "erin-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	login := func(c *http.Client, username, password string) {
		t.Helper()
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	alice, bob, carol, dave, erin := newClient(t), newClient(t), newClient(t), newClient(t), newClient(t)
	login(alice, "alice", "alice-pass")
	login(bob, "bob", "bob-pass")
	login(carol, "carol", "carol-pass")
	login(dave, "dave", "dave-pass")
	login(erin, "erin", "erin-pass")

	sp := func(s string) *string { return &s }

	// 项目：bob KR 负责人；carol 任务负责人；dave、erin 或签组
	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "或签试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	for _, uid := range []int64{bobUser.ID, carolUser.ID, daveUser.ID, erinUser.ID} {
		resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
			api.AddProjectMembersRequest{UserIds: []int64{uid}, Role: api.Member})
		wantStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	resp = doJSON(t, carol, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "输出验收方案", OwnerId: carolUser.ID, StartDate: start, EndDate: end, ExpectedDeliverable: sp("验收方案")},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	tasks := decodeBody[[]api.Task](t, resp)
	taskID := tasks[0].Id
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/pool-review-decision", tasksURL, taskID),
		api.PoolReviewDecisionRequest{Decision: api.PoolReviewDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	// carol 配置或签组（dave、erin）；访客会被拒
	reviewersURL := fmt.Sprintf("%s/%d/reviewers", tasksURL, taskID)
	resp = doJSON(t, carol, http.MethodPut, reviewersURL, api.SetReviewersRequest{UserIds: []int64{daveUser.ID, erinUser.ID}})
	wantStatus(t, resp, http.StatusOK)
	if got := decodeBody[[]api.ReviewerInfo](t, resp); len(got) != 2 {
		t.Fatalf("或签组配置异常: %+v", got)
	}

	// 开始执行、上传候选、提交 → 进入中间审核
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/update-status", tasksURL, taskID),
		api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	detail := decodeBody[api.TaskDetail](t, resp)
	dA := detail.Deliverables[0].Id
	uploadCandidate(t, carol, tasksURL, taskID, dA, api.UploadCandidateRequest{FileName: "验收方案V1.docx"}, "candidate-bytes")
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews", tasksURL, taskID),
		api.SubmitCompletionRequest{Note: "请或签审核"})
	wantStatus(t, resp, http.StatusOK)
	submitted := decodeBody[api.Task](t, resp)
	// 或签组为 dave（赵六）、erin（钱七）：多人取首位加人数。
	if submitted.Status != api.TaskStatusPendingIntermediateReview || submitted.CurrentStage != "待赵六等2人审批" {
		t.Fatalf("提交后应进入中间审核: %+v", submitted)
	}

	// 审核中不可再调整或签组 409
	resp = doJSON(t, carol, http.MethodPut, reviewersURL, api.SetReviewersRequest{UserIds: []int64{daveUser.ID}})
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()

	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	detail = decodeBody[api.TaskDetail](t, resp)
	reviewID := detail.CompletionReviews[0].Id
	if detail.CompletionReviews[0].State != api.CompletionReviewStateIntermediateReview {
		t.Fatalf("申请状态异常: %+v", detail.CompletionReviews[0])
	}
	// AC-37：任务负责人 carol 与 KR 负责人 bob（非组员）均无处理标志
	if detail.CompletionReviews[0].CanDecide == nil || *detail.CompletionReviews[0].CanDecide {
		t.Fatalf("非审核人不应可处理: %+v", detail.CompletionReviews[0])
	}
	decisionURL := fmt.Sprintf("%s/%d/completion-reviews/%d/decision", tasksURL, taskID, reviewID)
	resp = doJSON(t, bob, http.MethodPost, decisionURL, api.CompletionDecisionRequest{Decision: api.CompletionDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()

	// AC-24：erin 退回（意见必填）→ 候选删除、任务回进行中、意见保留
	resp = doJSON(t, erin, http.MethodPost, decisionURL, api.CompletionDecisionRequest{Decision: api.CompletionDecisionRequestDecisionRejected})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
	op := "验收样例与现场口径不一致"
	resp = doJSON(t, erin, http.MethodPost, decisionURL, api.CompletionDecisionRequest{Decision: api.CompletionDecisionRequestDecisionRejected, Opinion: &op})
	wantStatus(t, resp, http.StatusOK)
	if got := decodeBody[api.Task](t, resp); got.Status != api.TaskStatusInProgress {
		t.Fatalf("退回后应回进行中: %+v", got)
	}
	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	detail = decodeBody[api.TaskDetail](t, resp)
	if detail.CompletionReviews[0].State != api.CompletionReviewStateRejected || *detail.CompletionReviews[0].Opinion != op {
		t.Fatalf("退回意见未保留: %+v", detail.CompletionReviews[0])
	}
	if detail.Deliverables[0].Candidate != nil {
		t.Fatalf("退回后候选应删除: %+v", detail.Deliverables[0])
	}

	// 重新提交完整流程：重传候选→提交→dave 通过（或签任一人）→ 待 KR 终审、erin 待办关闭
	uploadCandidate(t, carol, tasksURL, taskID, dA, api.UploadCandidateRequest{FileName: "验收方案V2.docx"}, "candidate-bytes")
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews", tasksURL, taskID),
		api.SubmitCompletionRequest{Note: "修正口径后重提"})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, dave, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	detail = decodeBody[api.TaskDetail](t, resp)
	newReviewID := detail.CompletionReviews[0].Id
	if detail.CompletionReviews[0].CanDecide == nil || !*detail.CompletionReviews[0].CanDecide {
		t.Fatalf("或签组成员应可处理: %+v", detail.CompletionReviews[0])
	}
	newDecisionURL := fmt.Sprintf("%s/%d/completion-reviews/%d/decision", tasksURL, taskID, newReviewID)
	okOp := "口径一致，通过"
	resp = doJSON(t, dave, http.MethodPost, newDecisionURL, api.CompletionDecisionRequest{Decision: api.CompletionDecisionRequestDecisionApproved, Opinion: &okOp})
	wantStatus(t, resp, http.StatusOK)
	afterOr := decodeBody[api.Task](t, resp)
	if afterOr.Status != api.TaskStatusPendingFinalReview {
		t.Fatalf("或签通过后应待 KR 终审: %+v", afterOr)
	}
	// AC-14：其余待办自动关闭——erin 再处理返回状态冲突
	resp = doJSON(t, erin, http.MethodPost, newDecisionURL, api.CompletionDecisionRequest{Decision: api.CompletionDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()

	// 或签通过留痕；KR 负责人终审闭环
	resp = doJSON(t, erin, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	detail = decodeBody[api.TaskDetail](t, resp)
	cr := detail.CompletionReviews[0]
	if cr.IntermediateByName == nil || *cr.IntermediateByName != "赵六" || cr.IntermediateOpinion == nil || *cr.IntermediateOpinion != okOp {
		t.Fatalf("或签处理事实未留痕: %+v", cr)
	}
	resp = doJSON(t, bob, http.MethodPost, newDecisionURL, api.CompletionDecisionRequest{Decision: api.CompletionDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusOK)
	if got := decodeBody[api.Task](t, resp); got.Status != api.TaskStatusCompleted {
		t.Fatalf("终审后应完成: %+v", got)
	}
}

// 指定成员输入请求（#14，AC-29/30；MW-10／MW-11）：草稿阶段不通知→入池通过后带上下文通知→同意接收（不就绪）
// →提交内容后输入就绪；无拒绝/转派端点。
func TestMemberInputRequests(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	carolUser := seedUser(t, q, "carol", "王五", "carol-pass")
	daveUser := seedUser(t, q, "dave", "赵六", "dave-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	login := func(c *http.Client, username, password string) {
		t.Helper()
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	alice, bob, carol, dave := newClient(t), newClient(t), newClient(t), newClient(t)
	login(alice, "alice", "alice-pass")
	login(bob, "bob", "bob-pass")
	login(carol, "carol", "carol-pass")
	login(dave, "dave", "dave-pass")

	sp := func(s string) *string { return &s }

	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "对接试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	for _, uid := range []int64{bobUser.ID, carolUser.ID, daveUser.ID} {
		resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
			api.AddProjectMembersRequest{UserIds: []int64{uid}, Role: api.Member})
		wantStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	// carol 建草稿（不提交）→ 配置成员输入（dave）→ 通知不发（AC-29 前半）
	resp = doJSON(t, carol, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: false,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "回归验证分析", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	tasks := decodeBody[[]api.Task](t, resp)
	taskID := tasks[0].Id
	expected := openapiDate(t, "2026-09-10")
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/member-inputs", tasksURL, taskID),
		api.CreateMemberInputRequest{
			Necessity: api.Required, ProviderIds: []int64{daveUser.ID},
			ContentNote: "请提供最新接口字段口径说明", ExpectedDate: expected,
		})
	// 草稿任务的结构变更直接生效，不生成变更单（AC-23）
	wantStructureAccepted(t, resp)
	edge := edgeOf(t, carol, base, created.Id, taskID, "接口字段口径")
	if edge.InputRequest == nil || edge.InputRequest.State != api.InputRequestStatePending || edge.Ready {
		t.Fatalf("成员输入边异常: %+v", edge)
	}
	requestID := edge.InputRequest.Id
	resp = doJSON(t, dave, http.MethodGet, base+"/notifications", nil)
	wantStatus(t, resp, http.StatusOK)
	if got := decodeBody[[]api.Notification](t, resp); len(got) != 0 {
		t.Fatalf("草稿阶段不应打扰对接人: %+v", got)
	}

	// 期望时间必填 422
	bad := api.CreateMemberInputRequest{Necessity: api.Required, ProviderIds: []int64{daveUser.ID}, ContentNote: "x"}
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/member-inputs", tasksURL, taskID), map[string]any{
		"necessity": bad.Necessity, "providerIds": bad.ProviderIds, "contentNote": bad.ContentNote,
	})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()

	// 提交入池 → KR 负责人通过 → dave 收到带上下文通知（AC-29 后半）
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/submit-pool-review", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, dave, http.MethodGet, base+"/notifications", nil)
	wantStatus(t, resp, http.StatusOK)
	if got := decodeBody[[]api.Notification](t, resp); len(got) != 0 {
		t.Fatalf("待审批阶段仍不应通知: %+v", got)
	}
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/pool-review-decision", tasksURL, taskID),
		api.PoolReviewDecisionRequest{Decision: api.PoolReviewDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusOK)
	afterPool := decodeBody[api.Task](t, resp)
	if afterPool.Status != api.TaskStatusWaitingInput {
		t.Fatalf("必要输入未到应显示等待输入: %+v", afterPool.Status)
	}
	resp = doJSON(t, dave, http.MethodGet, base+"/notifications", nil)
	wantStatus(t, resp, http.StatusOK)
	notes := decodeBody[[]api.Notification](t, resp)
	if len(notes) != 1 || notes[0].Kind != "input_request" || notes[0].TaskId == nil || *notes[0].TaskId != taskID {
		t.Fatalf("入池通过后应发带上下文通知: %+v", notes)
	}

	// AC-30：他人不能接收；dave 同意接收（接收≠就绪）；未接收不能提交
	acceptURL := fmt.Sprintf("%s/projects/%d/input-requests/%d/accept", base, created.Id, requestID)
	provideURL := fmt.Sprintf("%s/projects/%d/input-requests/%d/provide", base, created.Id, requestID)
	resp = doJSON(t, carol, http.MethodPost, acceptURL, nil)
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	resp = doJSON(t, dave, http.MethodPost, provideURL, api.ProvideInputRequest{Text: sp("先提交试试")})
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()
	resp = doJSON(t, dave, http.MethodPost, acceptURL, nil)
	wantStatus(t, resp, http.StatusOK)
	accepted := decodeBody[api.InputRequest](t, resp)
	if accepted.State != api.InputRequestStateAccepted {
		t.Fatalf("接收状态异常: %+v", accepted)
	}
	resp = doJSON(t, carol, http.MethodGet, tasksURL, nil)
	wantStatus(t, resp, http.StatusOK)
	for _, task := range decodeBody[[]api.Task](t, resp) {
		if task.Id == taskID && task.Status != api.TaskStatusWaitingInput {
			t.Fatalf("接收不等于就绪，应仍等待输入: %+v", task.Status)
		}
	}

	// 空内容提交 422；提交文字+文件 → 已提供、输入就绪、任务不再等待输入
	resp = doJSON(t, dave, http.MethodPost, provideURL, api.ProvideInputRequest{})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
	resp = doJSON(t, dave, http.MethodPost, provideURL, api.ProvideInputRequest{Text: sp("口径见附件"), FileName: sp("接口口径.xlsx")})
	wantStatus(t, resp, http.StatusOK)
	provided := decodeBody[api.ProvideInputResponse](t, resp)
	// R4：带附件时先停在 uploading，文件真的写进对象存储并确认后才转已提供
	if provided.Request.State != api.InputRequestStateUploading || provided.UploadUrl == nil {
		t.Fatalf("提交结果异常: %+v", provided)
	}
	commitURL := fmt.Sprintf("%s/projects/%d/input-requests/%d/commit", base, created.Id, requestID)
	resp = doJSON(t, dave, http.MethodPost, commitURL, nil)
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	if d := decodeBody[api.TaskDetail](t, resp); d.Inputs[0].Ready || d.Task.Status != api.TaskStatusWaitingInput {
		t.Fatalf("附件未上传前不应就绪: %+v %v", d.Inputs[0], d.Task.Status)
	}
	putObject(t, *provided.UploadUrl, "input-bytes")
	resp = doJSON(t, dave, http.MethodPost, commitURL, nil)
	wantStatus(t, resp, http.StatusOK)
	if got := decodeBody[api.InputRequest](t, resp); got.State != api.InputRequestStateProvided {
		t.Fatalf("确认后应为已提供: %+v", got)
	}

	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	detail := decodeBody[api.TaskDetail](t, resp)
	if !detail.Inputs[0].Ready || detail.Task.Status != api.TaskStatusNotStarted {
		t.Fatalf("提交后输入应就绪: %+v %v", detail.Inputs[0], detail.Task.Status)
	}
	if detail.Inputs[0].InputRequest == nil || detail.Inputs[0].InputRequest.ProvidedText == nil {
		t.Fatalf("提交内容缺失: %+v", detail.Inputs[0].InputRequest)
	}

	// 文件下载地址；重复提交冲突
	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/projects/%d/input-requests/%d/file-url", base, created.Id, requestID), nil)
	wantStatus(t, resp, http.StatusOK)
	if u := decodeBody[api.DownloadUrlResponse](t, resp); u.Url == "" {
		t.Fatalf("文件地址为空")
	}
	resp = doJSON(t, dave, http.MethodPost, provideURL, api.ProvideInputRequest{Text: sp("再提一次")})
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()
}

// AC-53 后端：一次配置可多选来源任务或多名对接人，分别建边；各边独立参与就绪判定，
// 任一必要输入未就绪仍使目标任务显示「等待输入」。
func TestMultiSourceInputs(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	carolUser := seedUser(t, q, "carol", "王五", "carol-pass")
	daveUser := seedUser(t, q, "dave", "赵六", "dave-pass")
	erinUser := seedUser(t, q, "erin", "钱七", "erin-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	login := func(c *http.Client, username, password string) {
		t.Helper()
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	alice, bob, carol, dave, erin := newClient(t), newClient(t), newClient(t), newClient(t), newClient(t)
	login(alice, "alice", "alice-pass")
	login(bob, "bob", "bob-pass")
	login(carol, "carol", "carol-pass")
	login(dave, "dave", "dave-pass")
	login(erin, "erin", "erin-pass")

	sp := func(s string) *string { return &s }

	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "多来源试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	for _, uid := range []int64{bobUser.ID, carolUser.ID, daveUser.ID, erinUser.ID} {
		resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
			api.AddProjectMembersRequest{UserIds: []int64{uid}, Role: api.Member})
		wantStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	kr1 := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	edgesURL := fmt.Sprintf("%s/projects/%d/edges", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	// KR 负责人 bob 免审建两条上游任务与两条下游任务（C 用于多来源任务，D 用于多对接人）
	resp = doJSON(t, bob, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "采集现场数据", OwnerId: bobUser.ID, StartDate: start, EndDate: end, ExpectedDeliverable: sp("现场数据包")},
			{KeyResultId: kr1, Name: "整理历史台账", OwnerId: bobUser.ID, StartDate: start, EndDate: end, ExpectedDeliverable: sp("历史台账")},
			{KeyResultId: kr1, Name: "回归验证分析", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
			{KeyResultId: kr1, Name: "外部口径汇总", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	byName := map[string]int64{}
	for _, task := range decodeBody[[]api.Task](t, resp) {
		byName[task.Name] = task.Id
	}
	taskA, taskB2, taskC, taskD := byName["采集现场数据"], byName["整理历史台账"], byName["回归验证分析"], byName["外部口径汇总"]

	// AC-53：一次选择两个来源任务 → 分别建立两条边，各自独立未就绪
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/inputs", tasksURL, taskC),
		api.CreateTaskInputRequest{
			Necessity: api.Required, EdgeType: api.HardPrerequisite,
			SourceTaskIds: []int64{taskA, taskB2},
		})
	pendingMulti := wantStructureAccepted(t, resp)
	approveStructureChange(t, bob, base, created.Id, taskC, pendingMulti)
	multi := []api.DeliverableEdge{}
	for _, e := range projectEdges(t, carol, base, created.Id) {
		if e.TargetTaskId == taskC {
			multi = append(multi, e)
		}
	}
	if len(multi) != 2 || multi[0].Id == multi[1].Id {
		t.Fatalf("多来源应分别建边: %+v", multi)
	}
	if multi[0].SourceTaskId == nil || *multi[0].SourceTaskId != taskA || multi[1].SourceTaskId == nil || *multi[1].SourceTaskId != taskB2 {
		t.Fatalf("应按选择顺序对应来源任务: %+v", multi)
	}
	for _, e := range multi {
		if e.Ready || e.TargetTaskId != taskC {
			t.Fatalf("新建多来源边应指向目标任务且未就绪: %+v", e)
		}
	}

	// 同一次选择不可重复、多选时不能指定交付物项、不能选自身、不能空选
	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskA), nil)
	wantStatus(t, resp, http.StatusOK)
	dA := decodeBody[api.TaskDetail](t, resp).Deliverables[0].Id
	for _, bad := range []api.CreateTaskInputRequest{
		{Necessity: api.Required, EdgeType: api.Information, SourceTaskIds: []int64{taskA, taskA}},
		{Necessity: api.Required, EdgeType: api.Information, SourceTaskIds: []int64{taskA, taskB2}, DeliverableId: &dA},
		{Necessity: api.Required, EdgeType: api.Information, SourceTaskIds: []int64{taskA, taskC}},
		{Necessity: api.Required, EdgeType: api.Information, SourceTaskIds: []int64{}},
	} {
		resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/inputs", tasksURL, taskC), bad)
		wantStatus(t, resp, http.StatusUnprocessableEntity)
		resp.Body.Close()
	}

	// 任一必要输入未就绪 ⇒ C 等待输入
	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskC), nil)
	wantStatus(t, resp, http.StatusOK)
	if detail := decodeBody[api.TaskDetail](t, resp); detail.Task.Status != api.TaskStatusWaitingInput || len(detail.Inputs) != 2 {
		t.Fatalf("多来源未就绪应等待输入: %+v", detail.Task.Status)
	}

	// AC-53：一次选择两名对接人 → 各自建边、各自生成输入请求与通知
	expected := openapiDate(t, "2026-09-10")
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/member-inputs", tasksURL, taskD),
		api.CreateMemberInputRequest{
			Necessity: api.Required, ProviderIds: []int64{daveUser.ID, erinUser.ID},
			ContentNote: "请各自提供本方口径说明", ExpectedDate: expected,
		})
	pendingMembers := wantStructureAccepted(t, resp)
	approveStructureChange(t, bob, base, created.Id, taskD, pendingMembers)
	memberEdges := []api.DeliverableEdge{}
	for _, e := range projectEdges(t, carol, base, created.Id) {
		// 成员来源没有来源任务，标识取「所需内容」摘要（#112）。
		if e.TargetTaskId == taskD && e.Name == "请各自提供本方口径说明" {
			memberEdges = append(memberEdges, e)
		}
	}
	if len(memberEdges) != 2 {
		t.Fatalf("多对接人应分别建边: %+v", memberEdges)
	}
	requestIDs := map[int64]int64{}
	for _, e := range memberEdges {
		if e.InputRequest == nil || e.InputRequest.State != api.InputRequestStatePending || e.Ready {
			t.Fatalf("每名对接人应各自生成待接收输入请求: %+v", e)
		}
		requestIDs[e.InputRequest.ProviderId] = e.InputRequest.Id
	}
	if len(requestIDs) != 2 {
		t.Fatalf("输入请求应按对接人各一条: %+v", requestIDs)
	}
	for _, c := range []*http.Client{dave, erin} {
		resp = doJSON(t, c, http.MethodGet, base+"/notifications", nil)
		wantStatus(t, resp, http.StatusOK)
		notes := decodeBody[[]api.Notification](t, resp)
		if len(notes) != 1 || notes[0].Kind != "input_request" || notes[0].TaskId == nil || *notes[0].TaskId != taskD {
			t.Fatalf("每名对接人应各收到一条带上下文通知: %+v", notes)
		}
	}
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/member-inputs", tasksURL, taskD),
		api.CreateMemberInputRequest{
			Necessity: api.Required, ProviderIds: []int64{daveUser.ID, daveUser.ID},
			ContentNote: "x", ExpectedDate: expected,
		})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/member-inputs", tasksURL, taskD),
		api.CreateMemberInputRequest{
			Necessity: api.Required, ProviderIds: []int64{},
			ContentNote: "x", ExpectedDate: expected,
		})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()

	// 各对接人独立走「同意接收 → 提交内容」；只完成一人时 D 仍等待输入
	act := func(c *http.Client, providerID int64) {
		t.Helper()
		id := requestIDs[providerID]
		r := doJSON(t, c, http.MethodPost, fmt.Sprintf("%s/projects/%d/input-requests/%d/accept", base, created.Id, id), nil)
		wantStatus(t, r, http.StatusOK)
		r.Body.Close()
		r = doJSON(t, c, http.MethodPost, fmt.Sprintf("%s/projects/%d/input-requests/%d/provide", base, created.Id, id),
			api.ProvideInputRequest{Text: sp("本方口径说明")})
		wantStatus(t, r, http.StatusOK)
		r.Body.Close()
	}
	act(dave, daveUser.ID)
	resp = doJSON(t, dave, http.MethodPost, fmt.Sprintf("%s/projects/%d/input-requests/%d/accept", base, created.Id, requestIDs[erinUser.ID]), nil)
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodGet, edgesURL, nil)
	wantStatus(t, resp, http.StatusOK)
	readyByProvider := map[int64]bool{}
	for _, e := range decodeBody[[]api.DeliverableEdge](t, resp) {
		if e.TargetTaskId == taskD && e.InputRequest != nil {
			readyByProvider[e.InputRequest.ProviderId] = e.Ready
		}
	}
	if !readyByProvider[daveUser.ID] || readyByProvider[erinUser.ID] {
		t.Fatalf("对接人各边应独立就绪: %+v", readyByProvider)
	}
	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskD), nil)
	wantStatus(t, resp, http.StatusOK)
	if detail := decodeBody[api.TaskDetail](t, resp); detail.Task.Status != api.TaskStatusWaitingInput {
		t.Fatalf("仍有对接人未提交时应继续等待输入: %+v", detail.Task.Status)
	}

	// 两名对接人都提交后不再等待输入
	act(erin, erinUser.ID)
	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskD), nil)
	wantStatus(t, resp, http.StatusOK)
	if detail := decodeBody[api.TaskDetail](t, resp); detail.Task.Status != api.TaskStatusNotStarted {
		t.Fatalf("全部对接人提交后应解除等待输入: %+v", detail.Task.Status)
	}
}

// 结构化卡点与一键提醒（#15，AC-11）：执行者填写类型/缺失/原因/希望行动人上报，
// 一键提醒发定向通知；解除后保留处理事实且不可再动作。
// AC-11、MW-12／MW-13、CR-16（卡点表达的数据侧）：卡点由四类结构化事实派生，
// 触发条件消失即自动解除；一键提醒当前待行动人。
// 审批超时一类需要跨 N×24 小时，只在 domain 单测覆盖。
func TestDerivedBlockersAndRemind(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	carolUser := seedUser(t, q, "carol", "王五", "carol-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	login := func(c *http.Client, username, password string) {
		t.Helper()
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	alice, bob, carol := newClient(t), newClient(t), newClient(t)
	login(alice, "alice", "alice-pass")
	login(bob, "bob", "bob-pass")
	login(carol, "carol", "carol-pass")

	sp := func(s string) *string { return &s }

	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "卡点试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	for _, uid := range []int64{bobUser.ID, carolUser.ID} {
		resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
			api.AddProjectMembersRequest{UserIds: []int64{uid}, Role: api.Member})
		wantStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	blockersURL := fmt.Sprintf("%s/projects/%d/blockers", base, created.Id)
	remindURL := fmt.Sprintf("%s/projects/%d/reminders", base, created.Id)
	// 已过期的周期：截止已过 ⇒ 任务超期；开始时间已到 ⇒ 未就绪的必要输入成卡点。
	start, end := openapiDate(t, "2020-01-01"), openapiDate(t, "2020-02-01")

	// bob 免审建两个任务：下游由 carol 负责，上游由 bob 负责。
	resp = doJSON(t, bob, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "回归验证分析", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
			{KeyResultId: kr1, Name: "现场数据采集", OwnerId: bobUser.ID, StartDate: start, EndDate: end, ExpectedDeliverable: sp("现场数据包")},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	tasks := decodeBody[[]api.Task](t, resp)
	var downstream, upstream api.Task
	for _, tk := range tasks {
		if tk.Name == "回归验证分析" {
			downstream = tk
		} else {
			upstream = tk
		}
	}

	// 下游挂一条来自上游任务的必要输入：上游未交付 ⇒ 上游未就绪卡点，待行动人为上游负责人 bob。
	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, upstream.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	upstreamDeliverable := decodeBody[api.TaskDetail](t, resp).Deliverables[0].Id
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/inputs", tasksURL, downstream.Id),
		api.CreateTaskInputRequest{
			Necessity: api.Required, EdgeType: api.HardPrerequisite,
			SourceTaskIds: []int64{upstream.Id}, DeliverableId: &upstreamDeliverable,
		})
	pendingInput := wantStructureAccepted(t, resp)
	approveStructureChange(t, bob, base, created.Id, downstream.Id, pendingInput)
	edge := edgeOf(t, carol, base, created.Id, downstream.Id, "现场数据采集")

	byKind := func(c *http.Client) map[string]api.Blocker {
		t.Helper()
		r := doJSON(t, c, http.MethodGet, blockersURL, nil)
		wantStatus(t, r, http.StatusOK)
		out := map[string]api.Blocker{}
		for _, b := range decodeBody[[]api.Blocker](t, r) {
			out[b.Key] = b
		}
		return out
	}

	upstreamKey := fmt.Sprintf("upstream_unready:edge:%d", edge.Id)
	overdueKey := fmt.Sprintf("task_overdue:%d", downstream.Id)
	got := byKind(carol)
	unready, ok := got[upstreamKey]
	if !ok {
		t.Fatalf("必要输入未就绪应派生卡点: %+v", got)
	}
	if len(unready.ActionOwnerIds) != 1 || unready.ActionOwnerIds[0] != bobUser.ID {
		t.Fatalf("上游未就绪的待行动人应为上游负责人: %+v", unready)
	}
	if _, ok := got[overdueKey]; !ok {
		t.Fatalf("截止已过的任务应派生超期卡点: %+v", got)
	}
	if got[overdueKey].Level != api.HighRisk {
		t.Fatalf("任务超期应为高风险: %+v", got[overdueKey])
	}

	// R5／U3：KR 风险等级是读时派生值，真实路径（任务超期）就能产生高风险，
	// 且总览（objectives）与报告读同一份派生结果，原因行与等级同源。
	resp = doJSON(t, alice, http.MethodGet, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	derivedKr := decodeBody[[]api.Objective](t, resp)[0].KeyResults[0]
	if derivedKr.RiskLevel != api.HighRisk {
		t.Fatalf("KR 下有超期任务时应派生高风险: %+v", derivedKr)
	}
	if derivedKr.RiskNote == nil || !strings.Contains(*derivedKr.RiskNote, "任务超期") {
		t.Fatalf("高风险 KR 的原因行应来自抬高等级的那条卡点: %+v", derivedKr.RiskNote)
	}
	// AC-59 的「与 O」这一半（#82）：O 只取下级 KR 的最大值，原因行同源；接口不接受写入。
	objectiveView := decodeBody[[]api.Objective](t, doJSONAgain(t, alice, http.MethodGet, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id)))[0]
	if objectiveView.RiskLevel != api.HighRisk || objectiveView.RiskLevelLabel != "高风险" {
		t.Fatalf("O 下有高风险 KR 时应派生高风险: %+v / %q", objectiveView.RiskLevel, objectiveView.RiskLevelLabel)
	}
	if objectiveView.RiskNote == nil || *objectiveView.RiskNote != *derivedKr.RiskNote {
		t.Fatalf("O 的风险原因行应与抬高等级的 KR 同源: %+v", objectiveView.RiskNote)
	}
	resp = doRaw(t, alice, http.MethodPatch, fmt.Sprintf("%s/projects/%d/objectives/%d", base, created.Id, objectiveView.Id),
		`{"riskLevel":"normal","riskNote":"人工抹平"}`)
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	afterWrite := decodeBody[[]api.Objective](t, doJSONAgain(t, alice, http.MethodGet, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id)))[0]
	if afterWrite.RiskLevel != api.HighRisk {
		t.Fatalf("风险等级是只读派生字段，不应被写入改变: %+v", afterWrite.RiskLevel)
	}

	resp = doJSON(t, alice, http.MethodGet, fmt.Sprintf("%s/projects/%d/report?range=all", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	reportKrs := decodeBody[api.Report](t, resp).KrProgress
	if len(reportKrs) != 1 || reportKrs[0].RiskLevel != derivedKr.RiskLevel {
		t.Fatalf("报告的 KR 风险等级应与总览同源: %+v", reportKrs)
	}

	// 任务列表按派生结果给出卡点计数（下游两条：上游未就绪 + 超期）。
	resp = doJSON(t, carol, http.MethodGet, tasksURL, nil)
	wantStatus(t, resp, http.StatusOK)
	for _, tk := range decodeBody[[]api.Task](t, resp) {
		if tk.Id != downstream.Id {
			continue
		}
		if tk.OpenBlockerCount == nil || *tk.OpenBlockerCount != 2 {
			t.Fatalf("派生卡点计数异常: %+v", tk)
		}
	}

	// AC-11：一键提醒当前待行动人；待行动人自己不能提醒自己。
	resp = doJSON(t, bob, http.MethodPost, remindURL, api.RemindRequest{TargetKey: upstreamKey})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodPost, remindURL, api.RemindRequest{TargetKey: upstreamKey})
	wantStatus(t, resp, http.StatusNoContent)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodGet, base+"/notifications", nil)
	wantStatus(t, resp, http.StatusOK)
	notes := dropOkrAssigned(decodeBody[[]api.Notification](t, resp))
	if len(notes) != 1 || notes[0].Kind != "blocker_remind" || *notes[0].TaskId != downstream.Id {
		t.Fatalf("提醒通知异常: %+v", notes)
	}

	// 不存在的键（触发条件已消失）按 404 处理，没有手动解除接口。
	resp = doJSON(t, carol, http.MethodPost, remindURL, api.RemindRequest{TargetKey: "task_overdue:999999"})
	wantStatus(t, resp, http.StatusNotFound)
	resp.Body.Close()

	// 自动解除：上游任务走完终审、当前内容生效后，上游未就绪卡点消失，下游超期卡点仍在。
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/update-status", tasksURL, upstream.Id),
		api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	uploadCandidate(t, bob, tasksURL, upstream.Id, upstreamDeliverable, api.UploadCandidateRequest{FileName: "现场数据包.zip"}, "candidate-bytes")
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews", tasksURL, upstream.Id),
		api.SubmitCompletionRequest{Note: "数据包齐"})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, upstream.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	reviewID := decodeBody[api.TaskDetail](t, resp).CompletionReviews[0].Id
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews/%d/decision", tasksURL, upstream.Id, reviewID),
		api.CompletionDecisionRequest{Decision: api.CompletionDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	got = byKind(carol)
	if _, ok := got[upstreamKey]; ok {
		t.Fatalf("输入就绪后卡点应自动解除: %+v", got)
	}
	if _, ok := got[overdueKey]; !ok {
		t.Fatalf("超期卡点不应被一并解除: %+v", got)
	}
}

// 我的工作五分组（#16，AC-16；MW-01／MW-04／MW-08／MW-12）：五组齐备；KR 终审归入待我审批；
// 提交人视角在等待他人。MW-09 的待接收组恒空——接收方尚未建模。
func TestMyWorkFiveGroups(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	carolUser := seedUser(t, q, "carol", "王五", "carol-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	login := func(c *http.Client, username, password string) {
		t.Helper()
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	alice, bob, carol := newClient(t), newClient(t), newClient(t)
	login(alice, "alice", "alice-pass")
	login(bob, "bob", "bob-pass")
	login(carol, "carol", "carol-pass")

	sp := func(s string) *string { return &s }

	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "工作台试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	for _, uid := range []int64{bobUser.ID, carolUser.ID} {
		resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
			api.AddProjectMembersRequest{UserIds: []int64{uid}, Role: api.Member})
		wantStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	myWorkURL := fmt.Sprintf("%s/projects/%d/my-work", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	// carol 提交任务入池（待审批）→ carol 等待他人、bob（KR 负责人）待我审批
	resp = doJSON(t, carol, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "输出验收方案", OwnerId: carolUser.ID, StartDate: start, EndDate: end, ExpectedDeliverable: sp("验收方案")},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	tasks := decodeBody[[]api.Task](t, resp)
	taskID := tasks[0].Id

	resp = doJSON(t, bob, http.MethodGet, myWorkURL, nil)
	wantStatus(t, resp, http.StatusOK)
	bobWork := decodeBody[api.MyWork](t, resp)
	if len(bobWork.Approvals) != 1 || bobWork.Approvals[0].Kind != "pool_review" {
		t.Fatalf("入池审批应在待我审批: %+v", bobWork.Approvals)
	}
	// 身份卡（#69）：身份文案与当前职责随事实派生；bob 是 KR 负责人。
	if id := bobWork.Identity; id.UserId != bobUser.ID || id.DisplayName != "李四" ||
		id.RoleLabel != "项目成员" || id.ResponsibilitiesLabel != "KR 负责人" {
		t.Fatalf("身份卡异常: %+v", bobWork.Identity)
	}
	resp = doJSON(t, carol, http.MethodGet, myWorkURL, nil)
	wantStatus(t, resp, http.StatusOK)
	carolWork := decodeBody[api.MyWork](t, resp)
	if len(carolWork.Waiting) != 1 || carolWork.Waiting[0].Kind != "waiting_pool" {
		t.Fatalf("入池申请应在等待他人: %+v", carolWork.Waiting)
	}
	if len(carolWork.Pending) != 0 {
		t.Fatalf("待审批任务不应在待我处理: %+v", carolWork.Pending)
	}

	// 通过入池 → carol 待我处理出现任务卡；走到 KR 终审 → bob 待我审批出现 final_review（AC-16）
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/pool-review-decision", tasksURL, taskID),
		api.PoolReviewDecisionRequest{Decision: api.PoolReviewDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodGet, myWorkURL, nil)
	wantStatus(t, resp, http.StatusOK)
	carolWork = decodeBody[api.MyWork](t, resp)
	if len(carolWork.Pending) != 1 || carolWork.Pending[0].Kind != "task" {
		t.Fatalf("入池后任务应在待我处理: %+v", carolWork.Pending)
	}

	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/update-status", tasksURL, taskID),
		api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	detail := decodeBody[api.TaskDetail](t, resp)
	dA := detail.Deliverables[0].Id
	uploadCandidate(t, carol, tasksURL, taskID, dA, api.UploadCandidateRequest{FileName: "验收方案.docx"}, "candidate-bytes")
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews", tasksURL, taskID),
		api.SubmitCompletionRequest{Note: "请终审"})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	resp = doJSON(t, bob, http.MethodGet, myWorkURL, nil)
	wantStatus(t, resp, http.StatusOK)
	bobWork = decodeBody[api.MyWork](t, resp)
	var hasFinal bool
	for _, it := range bobWork.Approvals {
		if it.Kind == "final_review" {
			hasFinal = true
		}
	}
	if !hasFinal {
		t.Fatalf("KR 终审应归入待我审批: %+v", bobWork.Approvals)
	}
	resp = doJSON(t, carol, http.MethodGet, myWorkURL, nil)
	wantStatus(t, resp, http.StatusOK)
	carolWork = decodeBody[api.MyWork](t, resp)
	if len(carolWork.Pending) != 0 {
		t.Fatalf("完成审批中的任务不应在待我处理: %+v", carolWork.Pending)
	}
	var hasWaitingCompletion bool
	for _, it := range carolWork.Waiting {
		// AC-04：等待环节显示当前审批人姓名（KR 终审的审批人是 KR 负责人 bob／李四）。
		if it.Kind == "waiting_completion" && it.Stage != nil && *it.Stage == "待李四审批" {
			hasWaitingCompletion = true
		}
	}
	if !hasWaitingCompletion {
		t.Fatalf("完成申请应在等待他人并显示当前环节: %+v", carolWork.Waiting)
	}

	// 卡点：alice 名下一个已超期任务派生任务超期卡点 → alice 与我相关的卡点（AC-11）
	pastStart, pastEnd := openapiDate(t, "2020-01-01"), openapiDate(t, "2020-02-01")
	resp = doJSON(t, bob, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "验收环境准备", OwnerId: aliceUser.ID, StartDate: pastStart, EndDate: pastEnd},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	var overdueTask api.Task
	for _, tk := range decodeBody[[]api.Task](t, resp) {
		if tk.Name == "验收环境准备" {
			overdueTask = tk
		}
	}
	resp = doJSON(t, alice, http.MethodGet, myWorkURL, nil)
	wantStatus(t, resp, http.StatusOK)
	aliceWork := decodeBody[api.MyWork](t, resp)
	wantKey := fmt.Sprintf("task_overdue:%d", overdueTask.Id)
	if len(aliceWork.Blockers) != 1 || aliceWork.Blockers[0].Kind != "blocker" ||
		aliceWork.Blockers[0].RefKey == nil || *aliceWork.Blockers[0].RefKey != wantKey {
		t.Fatalf("任务超期应进入与我相关的卡点（期望 %s）: %+v", wantKey, aliceWork.Blockers)
	}
	// 五组字段齐备（待我接收当前恒空）
	if aliceWork.Receipts == nil {
		t.Fatalf("五组应齐备: %+v", aliceWork)
	}
}

// 循环互锁与关键路径（#23，AC-10；CR-09／CR-10 的数据侧）：硬前置循环标互锁并暂停该部分关键路径；
// 反馈循环不入关键路径；链上硬前置边派生 onCriticalPath。
func TestInterlockAndCriticalPath(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	login := func(c *http.Client, username, password string) {
		t.Helper()
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	alice, bob := newClient(t), newClient(t)
	login(alice, "alice", "alice-pass")
	login(bob, "bob", "bob-pass")

	sp := func(s string) *string { return &s }

	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "互锁试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
		api.AddProjectMembersRequest{UserIds: []int64{bobUser.ID}, Role: api.Member})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-10")

	// 三个任务：A→B→C 硬前置链；再加 C→B 硬前置构成循环；B→A 反馈边不影响。
	resp = doJSON(t, bob, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "任务A", OwnerId: bobUser.ID, StartDate: start, EndDate: end},
			{KeyResultId: kr1, Name: "任务B", OwnerId: bobUser.ID, StartDate: start, EndDate: end},
			{KeyResultId: kr1, Name: "任务C", OwnerId: bobUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	tasks := decodeBody[[]api.Task](t, resp)
	byName := map[string]int64{}
	for _, task := range tasks {
		byName[task.Name] = task.Id
	}
	mkEdge := func(srcName string, dst int64, et api.EdgeType) api.DeliverableEdge {
		t.Helper()
		resp := doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/inputs", tasksURL, dst), api.CreateTaskInputRequest{
			Necessity: api.Required, EdgeType: et, SourceTaskIds: []int64{byName[srcName]},
		})
		wantStructureAccepted(t, resp)
		// 输入源标识由来源任务派生（#112）：按来源任务名定位这条边。
		return edgeOf(t, bob, base, created.Id, dst, srcName)
	}
	eAB := mkEdge("任务A", byName["任务B"], api.HardPrerequisite)
	eBC := mkEdge("任务B", byName["任务C"], api.HardPrerequisite)
	_ = eAB
	_ = eBC

	// 链上边应派生 onCriticalPath（日期齐备）
	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/projects/%d/edges", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	edges := decodeBody[[]api.DeliverableEdge](t, resp)
	for _, e := range edges {
		if e.InterlockRisk == nil || *e.InterlockRisk {
			t.Fatalf("无循环不应互锁: %+v", e)
		}
		if e.OnCriticalPath == nil || !*e.OnCriticalPath {
			t.Fatalf("链上硬前置边应在关键路径: %+v", e)
		}
	}

	// 加 C→B 硬前置构成循环，再加 B→A 反馈边
	mkEdge("任务C", byName["任务B"], api.HardPrerequisite)
	mkEdge("任务B", byName["任务A"], api.Feedback)

	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/projects/%d/edges", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	edges = decodeBody[[]api.DeliverableEdge](t, resp)
	var interlocked, feedbackOnPath, abOnPath int
	for _, e := range edges {
		if e.InterlockRisk != nil && *e.InterlockRisk {
			interlocked++
			if e.OnCriticalPath != nil && *e.OnCriticalPath {
				t.Fatalf("互锁边不应进入关键路径: %+v", e)
			}
		}
		if e.EdgeType == api.Feedback && e.OnCriticalPath != nil {
			feedbackOnPath++
		}
		if e.Id == eAB.Id && e.OnCriticalPath != nil && *e.OnCriticalPath {
			abOnPath++
		}
	}
	if interlocked != 2 {
		t.Fatalf("B↔C 两条硬前置边应标互锁: %d", interlocked)
	}
	if feedbackOnPath != 0 {
		t.Fatalf("反馈边不应派生关键路径字段")
	}
	if abOnPath != 1 {
		t.Fatalf("循环外的 A→B 应保留在关键路径: %d", abOnPath)
	}
}

// 成果与归档、轻量成果包（#24，AC-17/18）：归档视角展示当前成果与审批记录数；
// 勾选当前成果生成目录与来源清单；整包下载解析当前内容（需要 MinIO，不可达时跳过下载断言）。
func TestArtifactsAndPackages(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	login := func(c *http.Client, username, password string) {
		t.Helper()
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	alice, bob := newClient(t), newClient(t)
	login(alice, "alice", "alice-pass")
	login(bob, "bob", "bob-pass")

	sp := func(s string) *string { return &s }

	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "归档试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
		api.AddProjectMembersRequest{UserIds: []int64{bobUser.ID}, Role: api.Member})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	// 走完整链路生成一份当前内容：建任务→开始→候选→终审通过
	resp = doJSON(t, bob, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "输出验收方案", OwnerId: bobUser.ID, StartDate: start, EndDate: end, ExpectedDeliverable: sp("验收方案")},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	tasks := decodeBody[[]api.Task](t, resp)
	taskID := tasks[0].Id
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/update-status", tasksURL, taskID),
		api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	detail := decodeBody[api.TaskDetail](t, resp)
	dA := detail.Deliverables[0].Id
	uploadCandidate(t, bob, tasksURL, taskID, dA, api.UploadCandidateRequest{FileName: "验收方案V1.docx"}, "acceptance-doc-bytes")

	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews", tasksURL, taskID),
		api.SubmitCompletionRequest{Note: "请终审"})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	detail = decodeBody[api.TaskDetail](t, resp)
	reviewID := detail.CompletionReviews[0].Id
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews/%d/decision", tasksURL, taskID, reviewID),
		api.CompletionDecisionRequest{Decision: api.CompletionDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	// 下游任务以这份成果为必要输入，边上绑定交付物项：归档列表的「来源关系边」列由此而来。
	resp = doJSON(t, bob, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "按方案执行验收", OwnerId: bobUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	var downstreamID int64
	for _, task := range decodeBody[[]api.Task](t, resp) {
		if task.Name == "按方案执行验收" {
			downstreamID = task.Id
		}
	}
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/inputs", tasksURL, downstreamID),
		api.CreateTaskInputRequest{
			Necessity: api.Required, EdgeType: api.HardPrerequisite,
			SourceTaskIds: []int64{taskID}, DeliverableId: &dA,
		})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp.Body.Close()

	// AC-17：归档视角——O/KR/任务结构、当前内容、审批记录数；无历史入口（契约即无）
	resp = doJSON(t, alice, http.MethodGet, fmt.Sprintf("%s/projects/%d/artifacts", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	artifacts := decodeBody[[]api.ArtifactObjective](t, resp)
	if len(artifacts) != 1 || len(artifacts[0].Krs) != 1 || len(artifacts[0].Krs[0].Tasks) != 1 {
		t.Fatalf("归档结构异常: %+v", artifacts)
	}
	// 只有带交付物的任务进归档视角，下游任务没有输出因此不成行。
	at := artifacts[0].Krs[0].Tasks[0]
	if at.ReviewCount != 1 || at.Deliverables[0].Current == nil {
		t.Fatalf("归档任务节点异常: %+v", at)
	}
	if at.StatusLabel != "已完成" {
		t.Fatalf("归档任务显示文案 = %q, want 已完成", at.StatusLabel)
	}
	// AC-17 列表层九列所需的派生字段（#68）：编号、任务负责人、接收方与 KR 组头事实。
	ao, akr := artifacts[0], artifacts[0].Krs[0]
	if ao.Code != "O1" || akr.Code != "KR1.1" || at.Code != "T1.1.1" {
		t.Fatalf("归档编号异常: O=%q KR=%q 任务=%q", ao.Code, akr.Code, at.Code)
	}
	if akr.OwnerName != "李四" || akr.DeliverableCount != 1 {
		t.Fatalf("KR 分组头异常: 负责人=%q 交付物数=%d", akr.OwnerName, akr.DeliverableCount)
	}
	if at.OwnerName != "李四" || at.ReceiverLabel != "未配置" {
		t.Fatalf("归档任务负责人／接收方异常: %q / %q", at.OwnerName, at.ReceiverLabel)
	}
	// 内容状态与提交／生效时间读时派生：终审通过后是「已生效」，时间取生效时刻。
	adl := at.Deliverables[0]
	if adl.ContentState != api.DeliverableContentStateEffective || adl.ContentStateLabel != "已生效" {
		t.Fatalf("内容状态异常: %q / %q", adl.ContentState, adl.ContentStateLabel)
	}
	if adl.ContentStateAt == nil || !adl.ContentStateAt.Equal(*adl.Current.EffectiveAt) {
		t.Fatalf("提交／生效时间未取当前内容生效时刻: %+v", adl.ContentStateAt)
	}
	// 来源关系边可在列表层看到并跳到下游任务（#68）。
	if len(adl.Edges) != 1 {
		t.Fatalf("来源关系边异常: %+v", adl.Edges)
	}
	if e := adl.Edges[0]; e.TargetTaskId != downstreamID ||
		e.EdgeTypeLabel != "硬前置交付" || e.TargetTaskName != "按方案执行验收" {
		t.Fatalf("来源关系边字段异常: %+v", e)
	}

	// §7.7 过程文件与重要外部材料（#79）：落在任务下，与交付物同走两阶段提交。
	processFile := uploadTaskFile(t, bob, tasksURL, downstreamID,
		api.UploadTaskFileRequest{Kind: api.Process, FileName: "联调记录.md", Note: sp("每日联调纪要")}, "process-bytes")
	externalFile := uploadTaskFile(t, bob, tasksURL, downstreamID,
		api.UploadTaskFileRequest{Kind: api.External, FileName: "厂商回执.pdf"}, "external-bytes")
	if processFile.KindLabel != "过程文件" || externalFile.KindLabel != "重要外部材料" {
		t.Fatalf("任务文件类型文案异常: %q / %q", processFile.KindLabel, externalFile.KindLabel)
	}
	if processFile.Note == nil || *processFile.Note != "每日联调纪要" {
		t.Fatalf("背景说明未保存: %+v", processFile.Note)
	}
	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, downstreamID), nil)
	wantStatus(t, resp, http.StatusOK)
	downstreamDetail := decodeBody[api.TaskDetail](t, resp)
	if downstreamDetail.Files == nil || len(*downstreamDetail.Files) != 2 {
		t.Fatalf("任务详情应带两份任务文件: %+v", downstreamDetail.Files)
	}
	// §7.7 边界：这两类文件不作为下游正式输入——上游成果虽已生效，但外部材料不改变任何就绪判定；
	// 它们也不进入完成审批：只有它们、没有候选内容时提交完成申请仍是「没有候选交付物内容」。
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/update-status", tasksURL, downstreamID),
		api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews", tasksURL, downstreamID),
		api.SubmitCompletionRequest{Note: "只有过程文件"})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
	// 预签名下载走任务文件自己的入口（与交付物文件各自一套 id，互不通用）。
	resp = doJSON(t, alice, http.MethodGet, fmt.Sprintf("%s/projects/%d/task-files/%d/download-url", base, created.Id, processFile.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	if decodeBody[api.DownloadUrlResponse](t, resp).Url == "" {
		t.Fatal("任务文件应可取得预签名下载地址")
	}
	// 归档列表层可见（AC-17「过程文件」；F-08 的「文件类型」筛选维由它支撑）。
	resp = doJSON(t, alice, http.MethodGet, fmt.Sprintf("%s/projects/%d/artifacts", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	var archivedFiles []api.TaskFile
	for _, o := range decodeBody[[]api.ArtifactObjective](t, resp) {
		for _, k := range o.Krs {
			for _, task := range k.Tasks {
				if task.TaskId == downstreamID && task.Files != nil {
					archivedFiles = *task.Files
				}
			}
		}
	}
	if len(archivedFiles) != 2 {
		t.Fatalf("归档视角应带任务文件: %+v", archivedFiles)
	}

	// §7.7「时间」筛选维（#86）：服务端裁剪，端点当天算在内，区间外一条不返回。
	today := time.Now().Format("2006-01-02")
	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	countRows := func(list []api.ArtifactObjective) (deliverables, files int) {
		for _, o := range list {
			for _, k := range o.Krs {
				for _, task := range k.Tasks {
					deliverables += len(task.Deliverables)
					if task.Files != nil {
						files += len(*task.Files)
					}
				}
			}
		}
		return
	}
	resp = doJSON(t, alice, http.MethodGet,
		fmt.Sprintf("%s/projects/%d/artifacts?from=%s&to=%s", base, created.Id, today, today), nil)
	wantStatus(t, resp, http.StatusOK)
	if d, f := countRows(decodeBody[[]api.ArtifactObjective](t, resp)); d == 0 || f != 2 {
		t.Fatalf("今天生成的内容应落在今天的区间内: 交付物=%d 文件=%d", d, f)
	}
	resp = doJSON(t, alice, http.MethodGet,
		fmt.Sprintf("%s/projects/%d/artifacts?from=%s", base, created.Id, tomorrow), nil)
	wantStatus(t, resp, http.StatusOK)
	if list := decodeBody[[]api.ArtifactObjective](t, resp); len(list) != 0 {
		d, f := countRows(list)
		t.Fatalf("明天之后没有任何内容，应筛空: 交付物=%d 文件=%d", d, f)
	}

	// AC-18：项目成员不能建包；管理员勾选当前成果生成
	pkgURL := fmt.Sprintf("%s/projects/%d/packages", base, created.Id)
	resp = doJSON(t, bob, http.MethodPost, pkgURL, api.CreatePackageRequest{Name: "联调成果", DeliverableIds: []int64{dA}})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPost, pkgURL, api.CreatePackageRequest{
		Name: "联调成果", DeliverableIds: []int64{dA}, TaskFileIds: &[]int64{processFile.Id},
	})
	wantStatus(t, resp, http.StatusCreated)
	pkg := decodeBody[api.ArtifactPackage](t, resp)
	if len(pkg.Items) != 2 || pkg.Items[0].FileName == nil || *pkg.Items[0].FileName != "验收方案V1.docx" {
		t.Fatalf("成果包目录异常: %+v", pkg)
	}
	// AC-18：目录同时含当前成果与必要过程文件，两类目录项各自可辨。
	fileItem := pkg.Items[1]
	if fileItem.TaskFileId == nil || *fileItem.TaskFileId != processFile.Id || fileItem.DeliverableId != nil {
		t.Fatalf("过程文件目录项异常: %+v", fileItem)
	}
	if fileItem.FileKind == nil || *fileItem.FileKind != api.Process || fileItem.DeliverableName != "联调记录.md" {
		t.Fatalf("过程文件目录项字段异常: %+v", fileItem)
	}

	// 整包下载（zip）。对象不可读时整体失败：以前会把错误文本塞进包里再回 200（E1）。
	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/%d/download", pkgURL, pkg.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	if ct := resp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("下载内容类型异常: %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if len(body) == 0 {
		t.Fatalf("zip 内容为空")
	}
	if !bytes.Contains(body, []byte("acceptance-doc-bytes")) {
		// zip 默认 Deflate 会压缩内容，改校验 zip 目录含文件名即可。
		if !bytes.Contains(body, []byte(".docx")) {
			t.Fatalf("zip 未包含当前内容条目")
		}
	}
	if !bytes.Contains(body, []byte("联调记录.md")) {
		t.Fatalf("zip 未包含选进包的过程文件")
	}

	// 删除任务文件：不进审批，直接生效（外部材料同理）。
	resp = doJSON(t, bob, http.MethodDelete, fmt.Sprintf("%s/%d/files/%d", tasksURL, downstreamID, externalFile.Id), nil)
	wantStatus(t, resp, http.StatusNoContent)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, downstreamID), nil)
	wantStatus(t, resp, http.StatusOK)
	if left := *decodeBody[api.TaskDetail](t, resp).Files; len(left) != 1 || left[0].Id != processFile.Id {
		t.Fatalf("删除后应只剩过程文件: %+v", left)
	}

	// F-10（#88）：删掉被成果包引用的过程文件——目录与来源清单保留条目并标注「来源文件已删除」，
	// 包内不再放该文件。此前条目随来源级联消失，包从 2 项变 1 项、清单少一行（§7.7、AC-18）。
	resp = doJSON(t, bob, http.MethodDelete, fmt.Sprintf("%s/%d/files/%d", tasksURL, downstreamID, processFile.Id), nil)
	wantStatus(t, resp, http.StatusNoContent)
	resp.Body.Close()

	resp = doJSON(t, alice, http.MethodGet, pkgURL, nil)
	wantStatus(t, resp, http.StatusOK)
	pkgs := decodeBody[[]api.ArtifactPackage](t, resp)
	if len(pkgs) != 1 || len(pkgs[0].Items) != 2 {
		t.Fatalf("来源删除后目录项数应不变: %+v", pkgs)
	}
	gone := pkgs[0].Items[1]
	if !gone.SourceDeleted {
		t.Fatalf("来源已删除的条目应标注 sourceDeleted: %+v", gone)
	}
	if gone.DeliverableName != "联调记录.md" || gone.TaskFileId != nil || gone.FileId != nil {
		t.Fatalf("来源已删除的条目应按快照保留名称、不再带内容: %+v", gone)
	}
	if gone.FileKind == nil || *gone.FileKind != api.Process {
		t.Fatalf("来源已删除的条目应保留文件类型: %+v", gone)
	}
	if pkgs[0].Items[0].SourceDeleted {
		t.Fatalf("交付物目录项的 sourceDeleted 恒为假: %+v", pkgs[0].Items[0])
	}

	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/%d/download", pkgURL, pkg.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("解包失败: %v", err)
	}
	var manifest string
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, f.Name)
		if f.Name != "成果包目录.txt" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("读清单失败: %v", err)
		}
		raw, _ := io.ReadAll(rc)
		rc.Close()
		manifest = string(raw)
	}
	for _, n := range names {
		if strings.Contains(n, "联调记录.md") {
			t.Fatalf("来源已删除的文件不应进包: %v", names)
		}
	}
	if !strings.Contains(manifest, "联调记录.md（过程文件） →（来源文件已删除）") {
		t.Fatalf("来源清单未保留条目并标注已删除:\n%s", manifest)
	}
	if strings.Count(manifest, "\n") != 2 {
		t.Fatalf("来源清单应仍有两行:\n%s", manifest)
	}
}

// 项目报告（#25，AC-19）：四档范围切换生成真实报告（KR 进展、完成成果、卡点、待决策、下一步）。
func TestProjectReport(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	login := func(c *http.Client, username, password string) {
		t.Helper()
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	alice, bob := newClient(t), newClient(t)
	login(alice, "alice", "alice-pass")
	login(bob, "bob", "bob-pass")

	sp := func(s string) *string { return &s }

	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "报告试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
		api.AddProjectMembersRequest{UserIds: []int64{bobUser.ID}, Role: api.Member})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start := openapiDate(t, "2026-08-01")
	soon := openapiDate(t, time.Now().AddDate(0, 0, 2).Format("2006-01-02"))

	// 任务 A：走完整链路到完成（产生完成成果与 completedInRange）；任务 B：临近截止（下一步）；
	// 任务 C：B 的上游，始终不完成，用于让 B 的必要输入未就绪。
	far := openapiDate(t, "2026-12-31")
	resp = doJSON(t, bob, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "输出验收方案", OwnerId: bobUser.ID, StartDate: start, EndDate: soon, ExpectedDeliverable: sp("验收方案")},
			{KeyResultId: kr1, Name: "临近截止任务", OwnerId: bobUser.ID, StartDate: start, EndDate: soon},
			{KeyResultId: kr1, Name: "上游未完成任务", OwnerId: bobUser.ID, StartDate: start, EndDate: far, ExpectedDeliverable: sp("上游数据包")},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	tasks := decodeBody[[]api.Task](t, resp)
	var taskA, taskB, taskC api.Task
	for _, task := range tasks {
		switch task.Name {
		case "输出验收方案":
			taskA = task
		case "临近截止任务":
			taskB = task
		case "上游未完成任务":
			taskC = task
		}
	}

	// B 挂一条来自 C 的必要输入边：C 未交付 ⇒ B 的必要输入未就绪（§5.1 等待输入）。
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/inputs", tasksURL, taskB.Id),
		api.CreateTaskInputRequest{Necessity: api.Required, EdgeType: api.HardPrerequisite, SourceTaskIds: []int64{taskC.Id}})
	wantStructureAccepted(t, resp)
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/update-status", tasksURL, taskA.Id),
		api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskA.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	detail := decodeBody[api.TaskDetail](t, resp)
	dA := detail.Deliverables[0].Id
	uploadCandidate(t, bob, tasksURL, taskA.Id, dA, api.UploadCandidateRequest{FileName: "验收方案V1.docx"}, "candidate-bytes")
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews", tasksURL, taskA.Id),
		api.SubmitCompletionRequest{Note: "请终审"})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskA.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	detail = decodeBody[api.TaskDetail](t, resp)
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews/%d/decision", tasksURL, taskA.Id, detail.CompletionReviews[0].Id),
		api.CompletionDecisionRequest{Decision: api.CompletionDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	reportURL := fmt.Sprintf("%s/projects/%d/report", base, created.Id)
	// 今天范围：完成成果与 completedInRange 均可见（刚刚发生）
	resp = doJSON(t, alice, http.MethodGet, reportURL+"?range=today", nil)
	wantStatus(t, resp, http.StatusOK)
	rep := decodeBody[api.Report](t, resp)
	if rep.Range != api.ReportRangeToday || len(rep.KrProgress) != 1 {
		t.Fatalf("报告基本结构异常: %+v", rep.Range)
	}
	if rep.KrProgress[0].CompletedInRange != 1 {
		t.Fatalf("范围内完成任务数异常: %+v", rep.KrProgress[0])
	}
	if len(rep.CompletedDeliverables) != 1 || rep.CompletedDeliverables[0].FileName != "验收方案V1.docx" {
		t.Fatalf("完成成果异常: %+v", rep.CompletedDeliverables)
	}
	// B 的必要输入未就绪且已到开始时间 ⇒ 派生一条上游未就绪卡点（AC-11）。
	if len(rep.Blockers) != 1 || rep.Blockers[0].Kind != api.UpstreamUnready ||
		rep.Blockers[0].ActionOwnerName == nil || *rep.Blockers[0].ActionOwnerName != "李四" {
		t.Fatalf("卡点异常: %+v", rep.Blockers)
	}
	if len(rep.NextSteps) == 0 {
		t.Fatalf("下一步为空: %+v", rep.NextSteps)
	}
	// AC-04 + §5.1：下一步条目输出面向用户的状态文案，且与任务列表同口径——
	// B 的必要输入未就绪，存储态仍是未开始，报告须显示「等待输入」。
	var nextB *api.ReportNextStep
	for i := range rep.NextSteps {
		if rep.NextSteps[i].TaskName == "临近截止任务" {
			nextB = &rep.NextSteps[i]
		}
	}
	if nextB == nil || nextB.Status != api.TaskStatusWaitingInput || nextB.StatusLabel != "等待输入" {
		t.Fatalf("下一步显示状态异常: %+v", rep.NextSteps)
	}
	// 「等待输入」还要说清缺哪一项（与我的工作同一口径）。
	if nextB.UnreadyNote == nil || *nextB.UnreadyNote != "上游未就绪：缺 上游未完成任务" {
		t.Fatalf("下一步未就绪注记异常: %q", derefStr(nextB.UnreadyNote))
	}

	// 项目整体（默认 all）与非法范围
	resp = doJSON(t, alice, http.MethodGet, reportURL, nil)
	wantStatus(t, resp, http.StatusOK)
	if rep := decodeBody[api.Report](t, resp); rep.Range != api.ReportRangeAll {
		t.Fatalf("默认范围应为 all: %+v", rep.Range)
	}
	resp = doJSON(t, alice, http.MethodGet, reportURL+"?range=year", nil)
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
}

// 报告导出（#26，AC-20）：Gotenberg 渲染 PDF 与移动端长图；服务不可达时跳过。
func TestReportExport(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")

	// Gotenberg 可达性探测
	gt := os.Getenv("GOTENBERG_URL")
	if gt == "" {
		gt = "http://localhost:3000"
	}
	if resp, err := http.Get(gt + "/health"); err != nil {
		t.Skipf("Gotenberg 不可达（docker compose up -d gotenberg）: %v", err)
	} else {
		resp.Body.Close()
	}

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	alice := newClient(t)
	resp := doJSON(t, alice, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: "alice", Password: "alice-pass"})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "导出试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)

	// PDF
	resp = doJSON(t, alice, http.MethodGet, fmt.Sprintf("%s/projects/%d/report/export?range=all&format=pdf", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	if ct := resp.Header.Get("Content-Type"); ct != "application/pdf" {
		t.Fatalf("PDF 内容类型异常: %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.HasPrefix(body, []byte("%PDF")) {
		t.Fatalf("非 PDF 内容: %q", body[:min(8, len(body))])
	}

	// 长图 PNG
	resp = doJSON(t, alice, http.MethodGet, fmt.Sprintf("%s/projects/%d/report/export?range=week&format=image", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Fatalf("PNG 内容类型异常: %q", ct)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if len(body) < 8 || body[1] != 'P' || body[2] != 'N' || body[3] != 'G' {
		t.Fatalf("非 PNG 内容")
	}
}

// 表格导入与批量入池（#27，AC-02/25）：结构化导入生成 O/KR 与任务草稿（整批事务）；
// 按 KR 批量提交；KR 负责人批量通过或退回。
func TestImportAndBatchPool(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	carolUser := seedUser(t, q, "carol", "王五", "carol-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	login := func(c *http.Client, username, password string) {
		t.Helper()
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	alice, bob, carol := newClient(t), newClient(t), newClient(t)
	login(alice, "alice", "alice-pass")
	login(bob, "bob", "bob-pass")
	login(carol, "carol", "carol-pass")

	sp := func(s string) *string { return &s }

	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "导入试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	for _, uid := range []int64{bobUser.ID, carolUser.ID} {
		resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
			api.AddProjectMembersRequest{UserIds: []int64{uid}, Role: api.Member})
		wantStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}

	importURL := fmt.Sprintf("%s/projects/%d/import", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	// 项目成员导入 403
	badReq := api.ImportRequest{Items: []api.ImportItem{{Title: sp("越权 O")}}}
	resp = doJSON(t, carol, http.MethodPost, importURL, badReq)
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()

	// AC-02：管理员导入 1 O × 2 KR × 3 任务草稿
	imp := api.ImportRequest{SourceFileName: sp("2026Q3 目标拆解.xlsx"), Items: []api.ImportItem{{
		Title: sp("提升交付质量"),
		KeyResults: &[]api.ImportKrItem{
			{Description: "上线自动验收", OwnerId: &bobUser.ID, Tasks: &[]api.ImportTaskItem{
				{Name: "导入任务一", OwnerId: carolUser.ID, StartDate: start, EndDate: end, ExpectedDeliverable: sp("方案一")},
				{Name: "导入任务二", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
			}},
			{Description: "现场回归通过", OwnerId: &bobUser.ID, Tasks: &[]api.ImportTaskItem{
				{Name: "导入任务三", OwnerId: bobUser.ID, StartDate: start, EndDate: end},
			}},
		},
	}}}
	resp = doJSON(t, alice, http.MethodPost, importURL, imp)
	wantStatus(t, resp, http.StatusCreated)
	result := decodeBody[api.ImportResult](t, resp)
	if len(result.Objectives) != 1 || len(result.Objectives[0].KeyResults) != 2 || len(result.Tasks) != 3 {
		t.Fatalf("导入结构异常: O=%d tasks=%d", len(result.Objectives), len(result.Tasks))
	}
	for _, task := range result.Tasks {
		if task.Status != api.TaskStatusDraft {
			t.Fatalf("导入任务应为草稿: %+v", task)
		}
	}

	// 校验失败整批回滚：含非法任务的导入不落任何数据
	bad := api.ImportRequest{Items: []api.ImportItem{{
		Title: sp("坏 O"),
		KeyResults: &[]api.ImportKrItem{{Description: "坏 KR", Tasks: &[]api.ImportTaskItem{
			{Name: "  ", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		}}},
	}}}
	resp = doJSON(t, alice, http.MethodPost, importURL, bad)
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	resp = doJSON(t, alice, http.MethodGet, tasksURL, nil)
	wantStatus(t, resp, http.StatusOK)
	if got := decodeBody[[]api.Task](t, resp); len(got) != 3 {
		t.Fatalf("失败导入不应落库: %d", len(got))
	}

	// AC-68：导入记录——成功一条、失败一条，最新在前；计数取真实写入量，失败不写成功。
	recordsURL := fmt.Sprintf("%s/projects/%d/import-records", base, created.Id)
	resp = doJSON(t, carol, http.MethodGet, recordsURL, nil)
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodGet, recordsURL, nil)
	wantStatus(t, resp, http.StatusOK)
	records := decodeBody[[]api.ImportRecord](t, resp)
	if len(records) != 2 {
		t.Fatalf("成功与失败各应留一条导入记录: %+v", records)
	}
	failed := records[0]
	if failed.Result != api.Failed || failed.ResultLabel != "失败" {
		t.Fatalf("失败的一次不应写成功: %+v", failed)
	}
	if failed.ObjectiveCount != 0 || failed.KeyResultCount != 0 || failed.TaskCount != 0 {
		t.Fatalf("整批回滚的导入计数应为 0: %+v", failed)
	}
	if failed.FailureSummary == nil || *failed.FailureSummary == "" {
		t.Fatalf("失败记录应带摘要: %+v", failed.FailureSummary)
	}
	success := records[1]
	if success.Result != api.Success || success.ResultLabel != "成功" {
		t.Fatalf("成功的一次记录异常: %+v", success)
	}
	if success.ObjectiveCount != 1 || success.KeyResultCount != 2 || success.TaskCount != 3 {
		t.Fatalf("导入计数应取真实写入量: %+v", success)
	}
	if success.SourceFileName == nil || *success.SourceFileName != "2026Q3 目标拆解.xlsx" {
		t.Fatalf("源文件名未留存: %+v", success.SourceFileName)
	}
	if success.OperatorName != "张三" || success.ImportedAt.IsZero() {
		t.Fatalf("操作人与时间未留存: %+v", success)
	}

	// AC-02b（#107）：任务导入器只导任务，所属 KR 必须已存在；入口只对负责人／管理员开放。
	importTasksURL := fmt.Sprintf("%s/projects/%d/import-tasks", base, created.Id)
	kr1ID := result.Objectives[0].KeyResults[0].Id
	// 项目成员 403
	resp = doJSON(t, carol, http.MethodPost, importTasksURL, api.ImportTasksRequest{
		Items: []api.ImportTaskGroup{{KeyResultId: kr1ID, Tasks: []api.ImportTaskItem{
			{Name: "越权任务", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		}}},
	})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	// 所属 KR 不在本项目内 422
	resp = doJSON(t, alice, http.MethodPost, importTasksURL, api.ImportTasksRequest{
		Items: []api.ImportTaskGroup{{KeyResultId: 999999, Tasks: []api.ImportTaskItem{
			{Name: "野 KR 任务", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		}}},
	})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	if e := decodeBody[api.Error](t, resp); e.Code != "invalid_key_result" {
		t.Fatalf("code = %q, want invalid_key_result", e.Code)
	}
	// 管理员导入两条任务草稿，带预期交付物
	resp = doJSON(t, alice, http.MethodPost, importTasksURL, api.ImportTasksRequest{
		SourceFileName: sp("任务批量导入.xlsx"),
		Items: []api.ImportTaskGroup{{KeyResultId: kr1ID, Tasks: []api.ImportTaskItem{
			{Name: "导入任务四", OwnerId: carolUser.ID, StartDate: start, EndDate: end, ExpectedDeliverable: sp("方案四")},
			{Name: "导入任务五", OwnerId: bobUser.ID, StartDate: start, EndDate: end},
		}}},
	})
	wantStatus(t, resp, http.StatusCreated)
	taskResult := decodeBody[api.ImportTasksResult](t, resp)
	if len(taskResult.Tasks) != 5 {
		t.Fatalf("任务导入后项目内应有 5 项任务: %d", len(taskResult.Tasks))
	}
	imported := map[string]api.Task{}
	for _, task := range taskResult.Tasks {
		imported[task.Name] = task
	}
	four, ok := imported["导入任务四"]
	if !ok || four.Status != api.TaskStatusDraft || four.KeyResultId != kr1ID {
		t.Fatalf("导入任务四异常: %+v", four)
	}
	if _, ok := imported["导入任务五"]; !ok {
		t.Fatalf("导入任务五未落库")
	}
	// AC-68：任务导入同样留记录，计数只算任务
	resp = doJSON(t, alice, http.MethodGet, recordsURL, nil)
	wantStatus(t, resp, http.StatusOK)
	afterTaskImport := decodeBody[[]api.ImportRecord](t, resp)
	if len(afterTaskImport) != 4 {
		t.Fatalf("任务导入的成功与失败各应再留一条: %d", len(afterTaskImport))
	}
	latest := afterTaskImport[0]
	if latest.Result != api.Success || latest.TaskCount != 2 || latest.ObjectiveCount != 0 || latest.KeyResultCount != 0 {
		t.Fatalf("任务导入记录异常: %+v", latest)
	}
	if latest.SourceFileName == nil || *latest.SourceFileName != "任务批量导入.xlsx" {
		t.Fatalf("任务导入源文件名未留存: %+v", latest.SourceFileName)
	}

	// AC-25：按 KR1 批量提交（任务一、二）
	kr1Tasks := []int64{}
	for _, task := range result.Tasks {
		if task.Name == "导入任务一" || task.Name == "导入任务二" {
			kr1Tasks = append(kr1Tasks, task.Id)
		}
	}
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/projects/%d/tasks/batch-pool-submit", base, created.Id),
		api.BatchPoolSubmitRequest{TaskIds: kr1Tasks})
	wantStatus(t, resp, http.StatusOK)
	list := decodeBody[[]api.Task](t, resp)
	pendingCount := 0
	for _, task := range list {
		if task.Status == api.TaskStatusPendingPoolReview {
			pendingCount++
		}
	}
	if pendingCount != 2 {
		t.Fatalf("批量提交后待审批数量异常: %d", pendingCount)
	}

	decideURL := fmt.Sprintf("%s/projects/%d/tasks/batch-pool-decision", base, created.Id)

	// 非法 decision 必须 422，且不得改变任何任务状态。
	// 回归背景：此前 decision 未校验，approve 由 == "approved" 推出，任意非法值都会静默执行「批量退回」。
	resp = doJSON(t, bob, http.MethodPost, decideURL, map[string]any{"taskIds": kr1Tasks, "decision": "APPROVE"})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/projects/%d/tasks", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	stillPending := 0
	for _, task := range decodeBody[[]api.Task](t, resp) {
		if task.Status == api.TaskStatusPendingPoolReview {
			stillPending++
		}
	}
	if stillPending != 2 {
		t.Fatalf("非法 decision 后待审批数量 = %d, want 2（不应被静默退回）", stillPending)
	}

	// 非 KR 负责人批量处理 403；KR 负责人 bob 批量通过
	resp = doJSON(t, alice, http.MethodPost, decideURL, api.BatchPoolDecisionRequest{TaskIds: kr1Tasks, Decision: api.BatchPoolDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodPost, decideURL, api.BatchPoolDecisionRequest{TaskIds: kr1Tasks, Decision: api.BatchPoolDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusOK)
	list = decodeBody[[]api.Task](t, resp)
	notStarted := 0
	for _, task := range list {
		if task.Status == api.TaskStatusNotStarted {
			notStarted++
		}
	}
	if notStarted != 2 {
		t.Fatalf("批量通过后未开始数量异常: %d", notStarted)
	}

	// 批量退回路径：任务三提交后被退回
	var task3 int64
	for _, task := range result.Tasks {
		if task.Name == "导入任务三" {
			task3 = task.Id
		}
	}
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/projects/%d/tasks/batch-pool-submit", base, created.Id),
		api.BatchPoolSubmitRequest{TaskIds: []int64{task3}})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	op := "范围与 KR 不匹配"
	resp = doJSON(t, bob, http.MethodPost, decideURL, api.BatchPoolDecisionRequest{TaskIds: []int64{task3}, Decision: api.BatchPoolDecisionRequestDecisionRejected, Opinion: &op})
	wantStatus(t, resp, http.StatusOK)
	list = decodeBody[[]api.Task](t, resp)
	for _, task := range list {
		if task.Id == task3 && task.Status != api.TaskStatusDraft {
			t.Fatalf("批量退回后应回草稿: %+v", task)
		}
	}
}

// 统一权限验收与外部边界（#28，AC-21/22；MW-16）：切换身份后项目事实不变，只改变动作权限与
// 个人工作内容；全流程只依赖内部账号，外部传递由内部成员（协调人）以输入请求代录。
func TestUnifiedPermissionsAcrossIdentities(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	carolUser := seedUser(t, q, "carol", "王五", "carol-pass")
	daveUser := seedUser(t, q, "dave", "赵六", "dave-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	login := func(c *http.Client, username, password string) {
		t.Helper()
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	alice, bob, carol, dave := newClient(t), newClient(t), newClient(t), newClient(t)
	login(alice, "alice", "alice-pass")
	login(bob, "bob", "bob-pass")
	login(carol, "carol", "carol-pass")
	login(dave, "dave", "dave-pass")

	sp := func(s string) *string { return &s }

	// alice 管理员/项目负责人；bob KR 负责人（项目成员）；carol 项目成员；dave 访客
	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "权限验收", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	for uid, role := range map[int64]api.MemberRole{bobUser.ID: api.Member, carolUser.ID: api.Member, daveUser.ID: api.Viewer} {
		resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
			api.AddProjectMembersRequest{UserIds: []int64{uid}, Role: role})
		wantStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	// carol 提交任务（待入池审批）
	resp = doJSON(t, carol, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "输出验收方案", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()

	// AC-21：四个身份看到的项目事实一致（任务清单核心字段逐项相等）
	type factRow struct {
		Id      int64
		Name    string
		Status  api.TaskStatus
		OwnerId int64
	}
	factsOf := func(c *http.Client) []factRow {
		t.Helper()
		resp := doJSON(t, c, http.MethodGet, tasksURL, nil)
		wantStatus(t, resp, http.StatusOK)
		list := decodeBody[[]api.Task](t, resp)
		out := make([]factRow, 0, len(list))
		for _, task := range list {
			out = append(out, factRow{Id: task.Id, Name: task.Name, Status: task.Status, OwnerId: task.OwnerId})
		}
		return out
	}
	ref := factsOf(alice)
	for name, c := range map[string]*http.Client{"bob": bob, "carol": carol, "dave": dave} {
		got := factsOf(c)
		if len(got) != len(ref) {
			t.Fatalf("%s 看到的任务数量不同: %d vs %d", name, len(got), len(ref))
		}
		for i := range ref {
			if got[i] != ref[i] {
				t.Fatalf("%s 看到的项目事实不同: %+v vs %+v", name, got[i], ref[i])
			}
		}
	}

	// AC-21：动作权限随身份变化——同一任务的派生标志
	flagsOf := func(c *http.Client) api.Task {
		t.Helper()
		resp := doJSON(t, c, http.MethodGet, tasksURL, nil)
		wantStatus(t, resp, http.StatusOK)
		return decodeBody[[]api.Task](t, resp)[0]
	}
	if ft := flagsOf(bob); !ft.CanDecidePoolReview {
		t.Fatalf("KR 负责人应可审批: %+v", ft)
	}
	if ft := flagsOf(carol); ft.CanDecidePoolReview {
		t.Fatalf("提交人不应可审批: %+v", ft)
	}
	if ft := flagsOf(alice); ft.CanDecidePoolReview {
		t.Fatalf("管理员不能替代 KR 负责人审批: %+v", ft)
	}
	if ft := flagsOf(dave); ft.CanDecidePoolReview || ft.CanProposeFieldChange {
		t.Fatalf("访客不应有业务动作标志: %+v", ft)
	}

	// 访客：不能建任务/建 OKR，但可讨论、可查看下载（§3.4）
	resp = doJSON(t, dave, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: false,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "越权任务", OwnerId: daveUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	resp = doJSON(t, dave, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{{Title: sp("越权 O")}}})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	taskID := ref[0].Id
	resp = doJSON(t, dave, http.MethodPost, fmt.Sprintf("%s/%d/discussions", tasksURL, taskID),
		api.CreateDiscussionRequest{Content: "访客也可以提意见"})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()

	// AC-21：个人工作内容随身份变化
	myWorkURL := fmt.Sprintf("%s/projects/%d/my-work", base, created.Id)
	getWork := func(c *http.Client) api.MyWork {
		t.Helper()
		resp := doJSON(t, c, http.MethodGet, myWorkURL, nil)
		wantStatus(t, resp, http.StatusOK)
		return decodeBody[api.MyWork](t, resp)
	}
	if w := getWork(bob); len(w.Approvals) != 1 {
		t.Fatalf("KR 负责人的待我审批异常: %+v", w.Approvals)
	}
	if w := getWork(carol); len(w.Approvals) != 0 || len(w.Waiting) != 1 {
		t.Fatalf("提交人的分组异常: 审批 %d 等待 %d", len(w.Approvals), len(w.Waiting))
	}
	if w := getWork(dave); len(w.Pending)+len(w.Approvals)+len(w.Waiting)+len(w.Blockers) != 0 {
		t.Fatalf("访客不应有行动事项: %+v", w)
	}

	// AC-22：外部传递不产生外部账号——对接人必须是项目内非访客；
	// 外部材料由内部协调人（成员）代为接收与提交。
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/pool-review-decision", tasksURL, taskID),
		api.PoolReviewDecisionRequest{Decision: api.PoolReviewDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	expected := openapiDate(t, "2026-09-10")
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/member-inputs", tasksURL, taskID),
		api.CreateMemberInputRequest{Necessity: api.Required, ProviderIds: []int64{daveUser.ID},
			ContentNote: "外部材料需由内部协调人代录", ExpectedDate: expected})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/member-inputs", tasksURL, taskID),
		api.CreateMemberInputRequest{Necessity: api.Required, ProviderIds: []int64{bobUser.ID},
			ContentNote: "外部材料由协调人李四收集后代录", ExpectedDate: expected})
	wantStructureAccepted(t, resp)
}

func TestLoginRateLimit(t *testing.T) {
	_, pool := setupDB(t)

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	c := newClient(t)
	for i := 0; i < domain.MaxLoginFailures; i++ {
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: "nobody", Password: "x"})
		wantStatus(t, resp, http.StatusUnauthorized)
		resp.Body.Close()
	}
	resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: "nobody", Password: "x"})
	wantStatus(t, resp, http.StatusTooManyRequests)
	e := decodeBody[api.Error](t, resp)
	if e.Code != "rate_limited" {
		t.Fatalf("code = %q, want rate_limited", e.Code)
	}
}

func derefStr(v *string) string {
	if v == nil {
		return "<nil>"
	}
	return *v
}

// 任务动态（#43，ADR 0002；MW-18 退回理由进动态）：三道审批的提交与处理留痕，退回理由进动态；
// 卡点出现由写操作前后的派生卡点集合 diff 补记，系统派生事件没有行动人。
func TestTaskActivity(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	login := func(c *http.Client, username, password string) {
		t.Helper()
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	alice, bob := newClient(t), newClient(t)
	login(alice, "alice", "alice-pass")
	login(bob, "bob", "bob-pass")
	sp := func(s string) *string { return &s }

	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "动态试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
		api.AddProjectMembersRequest{UserIds: []int64{bobUser.ID}, Role: api.Member})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("联调收敛"), KeyResults: &[]api.CreateKeyResultInput{{Description: "打通端到端", OwnerId: &aliceUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	kr1 := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives[0].KeyResults[0].Id

	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	// 周期取已经过去的窗口：任务在草稿态不派生卡点，入池通过进入未开始后才成为超期卡点，
	// 这样「卡点出现」正好由入池通过这次写操作 diff 出来。
	start, end := openapiDate(t, "2026-01-01"), openapiDate(t, "2026-01-05")
	activitiesOf := func(taskID int64) []api.TaskActivity {
		t.Helper()
		r := doJSON(t, alice, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
		wantStatus(t, r, http.StatusOK)
		return decodeBody[api.TaskDetail](t, r).Activities
	}
	kinds := func(as []api.TaskActivity) []api.TaskActivityKind {
		out := make([]api.TaskActivityKind, 0, len(as))
		for _, a := range as {
			out = append(out, a.Kind)
		}
		return out
	}
	has := func(as []api.TaskActivity, kind api.TaskActivityKind) *api.TaskActivity {
		for i := range as {
			if as[i].Kind == kind {
				return &as[i]
			}
		}
		return nil
	}

	// bob 建任务并提交入池 → 提交留痕；alice（KR 负责人）退回 → 理由进动态（MW-18）
	resp = doJSON(t, bob, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "端到端联调", OwnerId: bobUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	taskID := decodeBody[[]api.Task](t, resp)[0].Id

	acts := activitiesOf(taskID)
	if len(acts) != 1 || acts[0].Kind != api.PoolSubmitted || acts[0].ActorName == nil || *acts[0].ActorName != "李四" {
		t.Fatalf("提交入池应留痕并带行动人: %+v", acts)
	}
	if acts[0].KindLabel != "提交入池审批" {
		t.Fatalf("动态类型中文名异常: %+v", acts[0])
	}

	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/%d/pool-review-decision", tasksURL, taskID),
		api.PoolReviewDecisionRequest{Decision: api.PoolReviewDecisionRequestDecisionRejected, Opinion: sp("范围写得太粗")})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	acts = activitiesOf(taskID)
	rejected := has(acts, api.PoolRejected)
	if rejected == nil || rejected.Summary != "入池审批退回：范围写得太粗" {
		t.Fatalf("退回理由应进动态: %+v", kinds(acts))
	}
	// 最新在前
	if acts[0].Kind != api.PoolRejected {
		t.Fatalf("动态应最新在前: %+v", kinds(acts))
	}

	// 重新提交并通过 → 任务进入未开始
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/submit-pool-review", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/%d/pool-review-decision", tasksURL, taskID),
		api.PoolReviewDecisionRequest{Decision: api.PoolReviewDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	acts = activitiesOf(taskID)
	if has(acts, api.PoolApproved) == nil {
		t.Fatalf("入池通过应留痕: %+v", kinds(acts))
	}
	// 入池通过让任务从草稿进入未开始，截止时间已过 ⇒ 派生出任务超期卡点，diff 补记「卡点出现」
	opened := has(acts, api.BlockerOpened)
	if opened == nil {
		t.Fatalf("入池通过后应补记卡点出现: %+v", kinds(acts))
	}
	if opened.ActorName != nil {
		t.Fatalf("系统派生事件不应带行动人: %+v", opened)
	}
	if opened.Summary != "卡点出现：任务超期 · 缺 按期完成任务" {
		t.Fatalf("卡点动态文案异常: %+v", opened.Summary)
	}

	// 关键字段修改把截止时间挪到未来 → 通过后超期条件消失，diff 补记「卡点解除」
	future := openapiDate(t, "2026-12-31")
	back := api.SubmitFieldChangeRequest{Reason: sp("窗口顺延")}
	back.Changes.EndDate = &future
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/field-changes", tasksURL, taskID), back)
	wantStatus(t, resp, http.StatusOK)
	backID := decodeBody[api.Task](t, resp).FieldChange.Id
	if has(activitiesOf(taskID), api.FieldChangeSubmitted) == nil {
		t.Fatalf("提交关键字段修改应留痕: %+v", kinds(activitiesOf(taskID)))
	}
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/%d/field-changes/%d/decision", tasksURL, taskID, backID),
		api.FieldChangeDecisionRequest{Decision: api.FieldChangeDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	acts = activitiesOf(taskID)
	if has(acts, api.FieldChangeApproved) == nil {
		t.Fatalf("关键字段修改生效应留痕: %+v", kinds(acts))
	}
	if has(acts, api.BlockerResolved) == nil {
		t.Fatalf("卡点条件消失后应补记卡点解除: %+v", kinds(acts))
	}
}

// MW-09：接收方与接收记录。终审通过后每位接收方的「待我接收」出现待接收项；
// 进任务概况确认接收后卡片消失、形成接收记录并留痕（模块 PRD §3.2.C、§8.6）。
func TestReceiversAndReceipts(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	carolUser := seedUser(t, q, "carol", "王五", "carol-pass")
	daveUser := seedUser(t, q, "dave", "赵六", "dave-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	login := func(c *http.Client, username, password string) {
		t.Helper()
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	alice, bob, carol, dave := newClient(t), newClient(t), newClient(t), newClient(t)
	login(alice, "alice", "alice-pass")
	login(bob, "bob", "bob-pass")
	login(carol, "carol", "carol-pass")
	login(dave, "dave", "dave-pass")
	sp := func(s string) *string { return &s }

	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "成果接收试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	for _, uid := range []int64{bobUser.ID, carolUser.ID, daveUser.ID} {
		resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
			api.AddProjectMembersRequest{UserIds: []int64{uid}, Role: api.Member})
		wantStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("交付到位"), KeyResults: &[]api.CreateKeyResultInput{{Description: "成果按时移交", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	// carol 建两个任务并入池通过、开始执行
	resp = doJSON(t, carol, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "指定接收方的任务", OwnerId: carolUser.ID, StartDate: start, EndDate: end, ExpectedDeliverable: sp("移交清单")},
			{KeyResultId: kr1, Name: "全员接收的任务", OwnerId: carolUser.ID, StartDate: start, EndDate: end, ExpectedDeliverable: sp("通报材料")},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	tasks := decodeBody[[]api.Task](t, resp)
	taskA, taskB := tasks[0].Id, tasks[1].Id
	for _, id := range []int64{taskA, taskB} {
		resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/pool-review-decision", tasksURL, id),
			api.PoolReviewDecisionRequest{Decision: api.PoolReviewDecisionRequestDecisionApproved})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
		resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/update-status", tasksURL, id),
			api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}

	// 接收方配置口径同输入配置：无关成员不可配置；指定成员时名单不能为空
	receiversA := fmt.Sprintf("%s/%d/receivers", tasksURL, taskA)
	resp = doJSON(t, dave, http.MethodPut, receiversA, api.SetReceiversRequest{Scope: api.ReceiverScopeMembers, UserIds: &[]int64{daveUser.ID}})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodPut, receiversA, api.SetReceiversRequest{Scope: api.ReceiverScopeMembers})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()

	// 任务 A 指定 dave 为接收方；任务 B 取「所有项目成员」。
	// 接收方是关键字段（AC-23）：任务已入池，carol 提交后待 KR 负责人 bob 审批，通过后才生效。
	resp = doJSON(t, carol, http.MethodPut, receiversA, api.SetReceiversRequest{Scope: api.ReceiverScopeMembers, UserIds: &[]int64{daveUser.ID}})
	pendingReceivers := wantStructureAccepted(t, resp)
	if pendingReceivers.ReceiverScope != api.ReceiverScopeNone {
		t.Fatalf("待审批期间接收方不应变更: %+v", pendingReceivers.ReceiverScope)
	}
	approveStructureChange(t, bob, base, created.Id, taskA, pendingReceivers)
	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskA), nil)
	wantStatus(t, resp, http.StatusOK)
	configured := decodeBody[api.TaskDetail](t, resp).Task
	if configured.ReceiverScope != api.ReceiverScopeMembers || configured.Receivers == nil || len(*configured.Receivers) != 1 {
		t.Fatalf("接收方配置未生效: %+v", configured)
	}
	resp = doJSON(t, carol, http.MethodPut, fmt.Sprintf("%s/%d/receivers", tasksURL, taskB),
		api.SetReceiversRequest{Scope: api.ReceiverScopeAll})
	pendingReceiversB := wantStructureAccepted(t, resp)
	approveStructureChange(t, bob, base, created.Id, taskB, pendingReceiversB)

	// 终审通过前没有待接收项
	myWorkURL := fmt.Sprintf("%s/projects/%d/my-work", base, created.Id)
	resp = doJSON(t, dave, http.MethodGet, myWorkURL, nil)
	wantStatus(t, resp, http.StatusOK)
	if work := decodeBody[api.MyWork](t, resp); len(work.Receipts) != 0 {
		t.Fatalf("终审通过前不应有待接收项: %+v", work.Receipts)
	}

	// 走完完成审核：上传候选 → 提交 → bob 终审通过
	approve := func(taskID int64, fileName string) {
		t.Helper()
		detailURL := fmt.Sprintf("%s/%d", tasksURL, taskID)
		r := doJSON(t, carol, http.MethodGet, detailURL, nil)
		wantStatus(t, r, http.StatusOK)
		d := decodeBody[api.TaskDetail](t, r)
		uploadCandidate(t, carol, tasksURL, taskID, d.Deliverables[0].Id, api.UploadCandidateRequest{FileName: fileName}, "candidate-bytes")
		r = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews", tasksURL, taskID),
			api.SubmitCompletionRequest{Note: "请终审"})
		wantStatus(t, r, http.StatusOK)
		r.Body.Close()
		r = doJSON(t, carol, http.MethodGet, detailURL, nil)
		wantStatus(t, r, http.StatusOK)
		d = decodeBody[api.TaskDetail](t, r)
		r = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews/%d/decision", tasksURL, taskID, d.CompletionReviews[0].Id),
			api.CompletionDecisionRequest{Decision: api.CompletionDecisionRequestDecisionApproved})
		wantStatus(t, r, http.StatusOK)
		r.Body.Close()
	}
	approve(taskA, "移交清单.docx")
	approve(taskB, "通报材料.docx")

	// MW-09：dave 的待我接收出现两条（A 指定接收方 + B 全员）；carol 只因 B 全员出现一条
	resp = doJSON(t, dave, http.MethodGet, myWorkURL, nil)
	wantStatus(t, resp, http.StatusOK)
	daveWork := decodeBody[api.MyWork](t, resp)
	if len(daveWork.Receipts) != 2 {
		t.Fatalf("接收方待我接收应有两条: %+v", daveWork.Receipts)
	}
	for _, it := range daveWork.Receipts {
		if it.Kind != "receipt" || it.ActionLabel != "去处理" || it.DrawerTab == nil || *it.DrawerTab != "overview" {
			t.Fatalf("待接收项卡片事实不对: %+v", it)
		}
	}
	resp = doJSON(t, carol, http.MethodGet, myWorkURL, nil)
	wantStatus(t, resp, http.StatusOK)
	carolWork := decodeBody[api.MyWork](t, resp)
	if len(carolWork.Receipts) != 1 || carolWork.Receipts[0].TaskId == nil || *carolWork.Receipts[0].TaskId != taskB {
		t.Fatalf("「所有项目成员」应逐人生成待接收项: %+v", carolWork.Receipts)
	}

	// 任务详情：接收方看到自己的待确认项；他人不能替其确认
	detailA := fmt.Sprintf("%s/%d", tasksURL, taskA)
	resp = doJSON(t, dave, http.MethodGet, detailA, nil)
	wantStatus(t, resp, http.StatusOK)
	dA := decodeBody[api.TaskDetail](t, resp)
	if len(dA.Receipts) != 1 || dA.Receipts[0].ConfirmedAt != nil {
		t.Fatalf("任务详情应有一条未确认的待接收项: %+v", dA.Receipts)
	}
	if dA.Task.CanConfirmReceipt == nil || !*dA.Task.CanConfirmReceipt {
		t.Fatalf("接收方本人应可确认接收: %+v", dA.Task)
	}
	confirmA := fmt.Sprintf("%s/%d/confirm-receipt", tasksURL, taskA)
	resp = doJSON(t, alice, http.MethodPost, confirmA, nil)
	wantStatus(t, resp, http.StatusNotFound)
	resp.Body.Close()

	// 确认接收：待接收项退出本组、形成接收记录、动作进任务动态
	resp = doJSON(t, dave, http.MethodPost, confirmA, nil)
	wantStatus(t, resp, http.StatusOK)
	confirmed := decodeBody[api.Task](t, resp)
	if confirmed.CanConfirmReceipt == nil || *confirmed.CanConfirmReceipt {
		t.Fatalf("确认后不应再有待接收项: %+v", confirmed)
	}
	resp = doJSON(t, dave, http.MethodPost, confirmA, nil)
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()

	resp = doJSON(t, dave, http.MethodGet, myWorkURL, nil)
	wantStatus(t, resp, http.StatusOK)
	if work := decodeBody[api.MyWork](t, resp); len(work.Receipts) != 1 {
		t.Fatalf("确认后待我接收应只剩一条: %+v", work.Receipts)
	}
	resp = doJSON(t, dave, http.MethodGet, detailA, nil)
	wantStatus(t, resp, http.StatusOK)
	dA = decodeBody[api.TaskDetail](t, resp)
	if len(dA.Receipts) != 1 || dA.Receipts[0].ConfirmedAt == nil {
		t.Fatalf("确认后应形成接收记录: %+v", dA.Receipts)
	}
	hasReceiptActivity := false
	for _, a := range dA.Activities {
		if a.Kind == api.ReceiptConfirmed {
			hasReceiptActivity = true
			if a.ActorName == nil || *a.ActorName != "赵六" {
				t.Fatalf("确认接收动态应记录行动人: %+v", a)
			}
		}
	}
	if !hasReceiptActivity {
		t.Fatalf("确认接收应进任务动态: %+v", dA.Activities)
	}
}

// MW-13 的另一半：等待他人分组里尚未成卡点的等待事项也能提醒当前待行动人；
// 同一人对同一任务当天第二次提醒被拒；不能提醒本人（模块 PRD §5.3）。
func TestRemindWaitingTargets(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	carolUser := seedUser(t, q, "carol", "王五", "carol-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	login := func(c *http.Client, username, password string) {
		t.Helper()
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	alice, bob, carol := newClient(t), newClient(t), newClient(t)
	login(alice, "alice", "alice-pass")
	login(bob, "bob", "bob-pass")
	login(carol, "carol", "carol-pass")
	sp := func(s string) *string { return &s }

	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "提醒试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	for _, uid := range []int64{bobUser.ID, carolUser.ID} {
		resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
			api.AddProjectMembersRequest{UserIds: []int64{uid}, Role: api.Member})
		wantStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("按时推进"), KeyResults: &[]api.CreateKeyResultInput{{Description: "入池及时审批", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	// carol 提交入池申请：刚提交，未达审批超时阈值，因此没有任何派生卡点
	resp = doJSON(t, carol, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "等审批的任务", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	task := decodeBody[[]api.Task](t, resp)[0]

	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/projects/%d/blockers", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	if bs := decodeBody[[]api.Blocker](t, resp); len(bs) != 0 {
		t.Fatalf("刚提交不应有派生卡点: %+v", bs)
	}

	// 等待他人卡片按事项自身的提醒目标寻址，且给出提醒入口
	myWorkURL := fmt.Sprintf("%s/projects/%d/my-work", base, created.Id)
	resp = doJSON(t, carol, http.MethodGet, myWorkURL, nil)
	wantStatus(t, resp, http.StatusOK)
	work := decodeBody[api.MyWork](t, resp)
	var waiting *api.WorkItem
	for i := range work.Waiting {
		if work.Waiting[i].Kind == "waiting_pool" {
			waiting = &work.Waiting[i]
		}
	}
	if waiting == nil {
		t.Fatalf("等待他人应有入池申请卡片: %+v", work.Waiting)
	}
	if !waiting.CanRemind || waiting.RefKey == nil || !strings.HasPrefix(*waiting.RefKey, "wait:pool_review:") {
		t.Fatalf("尚未成卡点的等待事项也应可提醒并按自身键寻址: %+v", waiting)
	}

	remindURL := fmt.Sprintf("%s/projects/%d/reminders", base, created.Id)
	// 不提醒本人：待行动人（KR 负责人 bob）自己不能提醒
	resp = doJSON(t, bob, http.MethodPost, remindURL, api.RemindRequest{TargetKey: *waiting.RefKey})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()

	// 提交人可提醒；通知带入任务、缺失环节与截止时间
	resp = doJSON(t, carol, http.MethodPost, remindURL, api.RemindRequest{TargetKey: *waiting.RefKey})
	wantStatus(t, resp, http.StatusNoContent)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodGet, base+"/notifications", nil)
	wantStatus(t, resp, http.StatusOK)
	notes := dropOkrAssigned(decodeBody[[]api.Notification](t, resp))
	if len(notes) != 1 || notes[0].Kind != "blocker_remind" || notes[0].TaskId == nil || *notes[0].TaskId != task.Id {
		t.Fatalf("提醒通知异常: %+v", notes)
	}
	for _, want := range []string{"等审批的任务", "入池审批处理", "2026-09-30"} {
		if !strings.Contains(notes[0].Content, want) {
			t.Fatalf("提醒正文缺「%s」: %s", want, notes[0].Content)
		}
	}

	// 冷却：同一人对同一任务当天第二次提醒被拒
	resp = doJSON(t, carol, http.MethodPost, remindURL, api.RemindRequest{TargetKey: *waiting.RefKey})
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()

	// 目标已处理后按不存在处理
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/pool-review-decision", tasksURL, task.Id),
		api.PoolReviewDecisionRequest{Decision: api.PoolReviewDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPost, remindURL, api.RemindRequest{TargetKey: *waiting.RefKey})
	wantStatus(t, resp, http.StatusNotFound)
	resp.Body.Close()
}

// 时间型卡点的动态留痕（ADR 0001）：审批超时与任务超期不由写操作触发，
// 由进程内每小时 ticker 扫描活跃项目补记；时间戳取真实发生时刻，重复扫描不重记。
func TestTimeBlockerActivitySweep(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	alice, bob := newClient(t), newClient(t)
	for _, c := range []struct {
		client   *http.Client
		username string
		password string
	}{{alice, "alice", "alice-pass"}, {bob, "bob", "bob-pass"}} {
		resp := doJSON(t, c.client, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: c.username, Password: c.password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	sp := func(s string) *string { return &s }

	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "留痕试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
		api.AddProjectMembersRequest{UserIds: []int64{bobUser.ID}, Role: api.Member})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("按期交付"), KeyResults: &[]api.CreateKeyResultInput{{Description: "无超期任务", OwnerId: &aliceUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	kr1 := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)

	// 建一个不会超期的任务并开始执行：此时没有任何卡点动态
	future := time.Now().AddDate(0, 0, 30).Format("2006-01-02")
	start, end := openapiDate(t, time.Now().Format("2006-01-02")), openapiDate(t, future)
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "按期交付的任务", OwnerId: bobUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	task := decodeBody[[]api.Task](t, resp)[0]
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/update-status", tasksURL, task.Id),
		api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	detailURL := fmt.Sprintf("%s/%d", tasksURL, task.Id)
	blockerActivities := func(client *http.Client) []api.TaskActivity {
		t.Helper()
		r := doJSON(t, client, http.MethodGet, detailURL, nil)
		wantStatus(t, r, http.StatusOK)
		out := []api.TaskActivity{}
		for _, a := range decodeBody[api.TaskDetail](t, r).Activities {
			if a.Kind == api.BlockerOpened {
				out = append(out, a)
			}
		}
		return out
	}
	if got := blockerActivities(alice); len(got) != 0 {
		t.Fatalf("尚未超期不应有卡点动态: %+v", got)
	}

	// 时间流逝：把周期改到过去（不经过任何业务写操作，因此写触发的 diff 抓不到）
	// 日期一律按项目时区解读（domain.ProjectLocation；R7 的同一口径）——
	// 用机器本地时区断言会让 UTC+8 以外的开发机误报红。
	overdueOn := time.Now().In(domain.ProjectLocation).AddDate(0, 0, -5).Format("2006-01-02")
	if _, err := pool.Exec(context.Background(),
		"UPDATE tasks SET start_date = $2, end_date = $2 WHERE id = $1", task.Id, overdueOn); err != nil {
		t.Fatalf("模拟超期失败: %v", err)
	}
	if got := blockerActivities(alice); len(got) != 0 {
		t.Fatalf("只读派生不应产生动态: %+v", got)
	}

	// ticker 扫描一次：补记「卡点出现」，时间戳取真实发生时刻（截止日），不是扫描时刻
	sweeper := api.NewServer(pool, nil)
	sweeper.SweepBlockerActivities(context.Background())
	got := blockerActivities(alice)
	if len(got) != 1 {
		t.Fatalf("ticker 应补记一条任务超期动态: %+v", got)
	}
	if got[0].Summary != "卡点出现：任务超期 · 缺 按期完成任务" {
		t.Fatalf("补记文案异常: %q", got[0].Summary)
	}
	if got[0].OccurredAt.In(domain.ProjectLocation).Format("2006-01-02") != overdueOn {
		t.Fatalf("时间戳应取真实发生时刻 %s: %v", overdueOn, got[0].OccurredAt)
	}
	if got[0].ActorName != nil {
		t.Fatalf("系统派生事件不应有行动人: %+v", got[0])
	}

	// 再扫一次（等价于下一小时或进程重启后重扫）：不重记
	sweeper.SweepBlockerActivities(context.Background())
	sweeper.SweepBlockerActivities(context.Background())
	if again := blockerActivities(alice); len(again) != 1 {
		t.Fatalf("重复扫描不应重复记账: %+v", again)
	}

	// 之后的业务写操作也不会再记一条（写触发 diff 与 ticker 指向同一条事实）
	progress := 40
	resp = doJSON(t, bob, http.MethodPut, fmt.Sprintf("%s/%d/progress", tasksURL, task.Id),
		api.UpdateTaskProgressRequest{Progress: &progress})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	if again := blockerActivities(alice); len(again) != 1 {
		t.Fatalf("写触发 diff 不应与 ticker 重复记账: %+v", again)
	}
}

// 读边界（PRD §3.3 / AC-21）：非项目成员看不到项目，也读不到项目内任何内容。
// 回归背景：此前 ListProjects／GetProject 均不按成员过滤，任意登录用户可读任意项目全量内容。
func TestNonMemberProjectReadBoundary(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	seedUser(t, q, "mallory", "外人", "mallory-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	login := func(c *http.Client, username, password string) {
		t.Helper()
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	alice, mallory := newClient(t), newClient(t)
	login(alice, "alice", "alice-pass")
	login(mallory, "mallory", "mallory-pass")

	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "读边界验收", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)

	// 非成员的项目列表里不出现该项目
	resp = doJSON(t, mallory, http.MethodGet, base+"/projects", nil)
	wantStatus(t, resp, http.StatusOK)
	for _, p := range decodeBody[[]api.Project](t, resp) {
		if p.Id == created.Id {
			t.Fatalf("非成员的项目列表出现了他人项目 #%d", created.Id)
		}
	}

	// 项目内各读端点统一 404：不泄露项目是否存在
	for _, path := range []string{"", "/tasks", "/edges", "/blockers", "/artifacts", "/my-work", "/report"} {
		url := fmt.Sprintf("%s/projects/%d%s", base, created.Id, path)
		resp = doJSON(t, mallory, http.MethodGet, url, nil)
		wantStatus(t, resp, http.StatusNotFound)
		resp.Body.Close()
	}

	// 成员一侧不受影响（避免过度收紧）
	resp = doJSON(t, alice, http.MethodGet, base+"/projects", nil)
	wantStatus(t, resp, http.StatusOK)
	if list := decodeBody[[]api.Project](t, resp); len(list) != 1 || list[0].Id != created.Id {
		t.Fatalf("创建人的项目列表异常: %+v", list)
	}
	for _, path := range []string{"", "/tasks", "/edges", "/blockers", "/artifacts", "/my-work", "/report"} {
		url := fmt.Sprintf("%s/projects/%d%s", base, created.Id, path)
		resp = doJSON(t, alice, http.MethodGet, url, nil)
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
}

// O／KR 编辑删除、KR 负责人交接与成员移出守卫（AC-21、AC-61、AC-65，回归 S2／U6）。
func TestOkrEditHandoverAndMemberRemoval(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	carolUser := seedUser(t, q, "carol", "王五", "carol-pass")
	daveUser := seedUser(t, q, "dave", "赵六", "dave-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"
	sp := func(v string) *string { return &v }

	alice, bob, carol, dave := newClient(t), newClient(t), newClient(t), newClient(t)
	for _, l := range []struct {
		c              *http.Client
		user, password string
	}{{alice, "alice", "alice-pass"}, {bob, "bob", "bob-pass"}, {carol, "carol", "carol-pass"}, {dave, "dave", "dave-pass"}} {
		resp := doJSON(t, l.c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: l.user, Password: l.password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}

	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "OKR 编辑", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	membersURL := fmt.Sprintf("%s/projects/%d/members", base, created.Id)
	for _, m := range []struct {
		id   int64
		role api.MemberRole
	}{{bobUser.ID, api.Member}, {carolUser.ID, api.Member}, {daveUser.ID, api.Member}} {
		resp = doJSON(t, alice, http.MethodPost, membersURL, api.AddProjectMembersRequest{UserIds: []int64{m.id}, Role: m.role})
		wantStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}

	// dave 降为只读，后面用来验证「访客不能被任命为负责人」（S2）
	resp = doJSON(t, alice, http.MethodPut, fmt.Sprintf("%s/%d", membersURL, daveUser.ID),
		api.UpdateProjectMemberRoleRequest{Role: api.Viewer})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	objectivesURL := fmt.Sprintf("%s/projects/%d/objectives", base, created.Id)
	resp = doJSON(t, alice, http.MethodPost, objectivesURL,
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	objectiveID, kr1 := okr[0].Id, okr[0].KeyResults[0].Id

	// 访客不能被任命为 KR 负责人（创建路径）
	resp = doJSON(t, alice, http.MethodPost, objectivesURL,
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("只读负责人"), KeyResults: &[]api.CreateKeyResultInput{{Description: "不该建成", OwnerId: &daveUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()

	// AC-65：O 只有项目管理员可编辑；KR 由管理员或本 KR 负责人编辑
	objURL := fmt.Sprintf("%s/projects/%d/objectives/%d", base, created.Id, objectiveID)
	krURL := fmt.Sprintf("%s/projects/%d/key-results/%d", base, created.Id, kr1)
	resp = doJSON(t, bob, http.MethodPatch, objURL, api.UpdateObjectiveRequest{Title: sp("越权改 O")})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPatch, objURL, api.UpdateObjectiveRequest{Title: sp("提升交付质量 V2")})
	wantStatus(t, resp, http.StatusOK)
	if got := decodeBody[api.Objective](t, resp); got.Title != "提升交付质量 V2" {
		t.Fatalf("O 标题未更新: %+v", got)
	}
	resp = doJSON(t, carol, http.MethodPatch, krURL, api.UpdateKeyResultRequest{Description: sp("越权改 KR")})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodPatch, krURL, api.UpdateKeyResultRequest{Description: sp("上线自动验收 V2")})
	wantStatus(t, resp, http.StatusOK)
	if got := decodeBody[api.KeyResult](t, resp); got.Description != "上线自动验收 V2" {
		t.Fatalf("KR 描述未更新: %+v", got)
	}

	// AC-61：负责人不可置空；访客不能接任
	resp = doJSON(t, alice, http.MethodPatch, krURL, map[string]any{"ownerId": nil})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPatch, krURL, api.UpdateKeyResultRequest{OwnerId: &daveUser.ID})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()

	// 造一件未决审批：carol 建任务提交入池，待 KR 负责人 bob 处理
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")
	resp = doJSON(t, carol, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items:           []api.CreateTaskItem{{KeyResultId: kr1, Name: "联调验证", OwnerId: carolUser.ID, StartDate: start, EndDate: end}},
	})
	wantStatus(t, resp, http.StatusCreated)
	taskID := decodeBody[[]api.Task](t, resp)[0].Id

	// AC-61：交接确认信息给出未决审批条数
	resp = doJSON(t, alice, http.MethodGet, krURL, nil)
	wantStatus(t, resp, http.StatusOK)
	if preview := decodeBody[api.KrHandoverPreview](t, resp); preview.PendingApprovals != 1 {
		t.Fatalf("未决审批条数异常: %+v", preview)
	}

	// AC-21／AC-61：bob 仍是 KR 负责人，不能被移出，409 里点名待交接的 KR
	resp = doJSON(t, alice, http.MethodDelete, fmt.Sprintf("%s/%d", membersURL, bobUser.ID), nil)
	wantStatus(t, resp, http.StatusConflict)
	if e := decodeBody[api.Error](t, resp); !strings.Contains(e.Message, "上线自动验收 V2") {
		t.Fatalf("409 未列出待交接的 KR: %+v", e)
	}

	// S2：bob 被降为只读后，仍挂着 KR 负责人也不能再审批
	resp = doJSON(t, alice, http.MethodPut, fmt.Sprintf("%s/%d", membersURL, bobUser.ID),
		api.UpdateProjectMemberRoleRequest{Role: api.Viewer})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/pool-review-decision", tasksURL, taskID),
		api.PoolReviewDecisionRequest{Decision: api.PoolReviewDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodPatch, krURL, api.UpdateKeyResultRequest{Description: sp("只读也想改")})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPut, fmt.Sprintf("%s/%d", membersURL, bobUser.ID),
		api.UpdateProjectMemberRoleRequest{Role: api.Member})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	// AC-61：交接给 carol，未决审批转交并发站内通知，审批件进入其「待我审批」
	resp = doJSON(t, alice, http.MethodPatch, krURL, api.UpdateKeyResultRequest{OwnerId: &carolUser.ID})
	wantStatus(t, resp, http.StatusOK)
	if got := decodeBody[api.KeyResult](t, resp); got.OwnerId == nil || *got.OwnerId != carolUser.ID {
		t.Fatalf("KR 负责人未交接: %+v", got)
	}
	resp = doJSON(t, carol, http.MethodGet, base+"/notifications", nil)
	wantStatus(t, resp, http.StatusOK)
	notes := decodeBody[[]api.Notification](t, resp)
	if len(notes) != 1 || notes[0].Kind != "kr_handover" {
		t.Fatalf("继任者未收到交接通知: %+v", notes)
	}
	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/projects/%d/my-work", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	work := decodeBody[api.MyWork](t, resp)
	found := false
	for _, it := range work.Approvals {
		if it.TaskId != nil && *it.TaskId == taskID {
			found = true
		}
	}
	if !found {
		t.Fatalf("转交后审批件应进入继任者待我审批: %+v", work.Approvals)
	}
	// 原负责人不再持有该审批件
	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/projects/%d/my-work", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	for _, it := range decodeBody[api.MyWork](t, resp).Approvals {
		if it.TaskId != nil && *it.TaskId == taskID {
			t.Fatalf("交接后原负责人不应再持有审批件: %+v", it)
		}
	}

	// AC-65：KR 下有任务、O 下有 KR 时删除被拒
	resp = doJSON(t, alice, http.MethodDelete, krURL, nil)
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodDelete, objURL, nil)
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()

	// 派生标志与「任务」列
	resp = doJSON(t, alice, http.MethodGet, objectivesURL, nil)
	wantStatus(t, resp, http.StatusOK)
	list := decodeBody[[]api.Objective](t, resp)
	var target api.KeyResult
	for _, o := range list {
		for _, k := range o.KeyResults {
			if k.Id == kr1 {
				target = k
			}
		}
	}
	if target.TaskCount == nil || *target.TaskCount != 1 {
		t.Fatalf("KR 任务数派生异常: %+v", target.TaskCount)
	}
	if target.CanDelete == nil || *target.CanDelete {
		t.Fatalf("有任务的 KR 不应可删: %+v", target.CanDelete)
	}
	if target.CanEdit == nil || !*target.CanEdit {
		t.Fatalf("管理员应可编辑 KR: %+v", target.CanEdit)
	}

	// 空 O 可删（管理员），项目成员不可删
	resp = doJSON(t, alice, http.MethodPost, objectivesURL,
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{{Title: sp("待删除的 O")}}})
	wantStatus(t, resp, http.StatusCreated)
	var emptyObjective int64
	for _, o := range decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives {
		if o.Title == "待删除的 O" {
			emptyObjective = o.Id
		}
	}
	emptyURL := fmt.Sprintf("%s/projects/%d/objectives/%d", base, created.Id, emptyObjective)
	resp = doJSON(t, carol, http.MethodDelete, emptyURL, nil)
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodDelete, emptyURL, nil)
	wantStatus(t, resp, http.StatusNoContent)
	resp.Body.Close()

	// 没有职责占位的成员可以正常移出
	resp = doJSON(t, alice, http.MethodDelete, fmt.Sprintf("%s/%d", membersURL, daveUser.ID), nil)
	wantStatus(t, resp, http.StatusNoContent)
	resp.Body.Close()
}

// 结构变更被退回后不生效（AC-23、§5.2.B，回归 R1）：输出属关键字段，
// 已入池任务由任务负责人提交后进所属 KR 负责人审批，退回则交付物项不产生，
// 且退回未处理期间不接受新的变更单。
func TestStructureChangeRejected(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	carolUser := seedUser(t, q, "carol", "王五", "carol-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"
	sp := func(v string) *string { return &v }

	alice, bob, carol := newClient(t), newClient(t), newClient(t)
	for _, l := range []struct {
		c              *http.Client
		user, password string
	}{{alice, "alice", "alice-pass"}, {bob, "bob", "bob-pass"}, {carol, "carol", "carol-pass"}} {
		resp := doJSON(t, l.c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: l.user, Password: l.password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}

	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "结构变更审批", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	for _, uid := range []int64{bobUser.ID, carolUser.ID} {
		resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
			api.AddProjectMembersRequest{UserIds: []int64{uid}, Role: api.Member})
		wantStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	kr1 := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")
	resp = doJSON(t, bob, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items:           []api.CreateTaskItem{{KeyResultId: kr1, Name: "联调验证", OwnerId: carolUser.ID, StartDate: start, EndDate: end}},
	})
	wantStatus(t, resp, http.StatusCreated)
	taskID := decodeBody[[]api.Task](t, resp)[0].Id

	// 任务负责人 carol 新增交付物项 → 待审批，交付物项尚未产生
	deliverablesURL := fmt.Sprintf("%s/%d/deliverables", tasksURL, taskID)
	resp = doJSON(t, carol, http.MethodPost, deliverablesURL, api.CreateDeliverableRequest{FileName: "联调报告.docx"})
	pending := wantStructureAccepted(t, resp)
	if pending.FieldChange == nil || pending.FieldChange.ChangeType != api.Structure {
		t.Fatalf("未生成结构变更单: %+v", pending.FieldChange)
	}
	if len(pending.FieldChange.Changes) != 1 || pending.FieldChange.Changes[0].Label != "预期交付物" {
		t.Fatalf("结构变更差异行异常: %+v", pending.FieldChange.Changes)
	}
	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	if ds := decodeBody[api.TaskDetail](t, resp).Deliverables; len(ds) != 0 {
		t.Fatalf("待审批期间不应先建交付物项: %+v", ds)
	}
	// 互斥：待审批期间不接受第二张单
	resp = doJSON(t, carol, http.MethodPost, deliverablesURL, api.CreateDeliverableRequest{FileName: "另一项.docx"})
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()

	// KR 负责人退回：交付物项仍不产生
	resp = doJSON(t, bob, http.MethodPost,
		fmt.Sprintf("%s/%d/field-changes/%d/decision", tasksURL, taskID, pending.FieldChange.Id),
		api.FieldChangeDecisionRequest{Decision: api.FieldChangeDecisionRequestDecisionRejected, Opinion: sp("交付物口径未定")})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	if ds := decodeBody[api.TaskDetail](t, resp).Deliverables; len(ds) != 0 {
		t.Fatalf("退回后不应产生交付物项: %+v", ds)
	}

	// 重新提交并通过：这次才真的建出来
	resp = doJSON(t, carol, http.MethodPost, deliverablesURL, api.CreateDeliverableRequest{FileName: "联调报告.docx"})
	again := wantStructureAccepted(t, resp)
	approveStructureChange(t, bob, base, created.Id, taskID, again)
	if d := deliverableOf(t, carol, base, created.Id, taskID, "联调报告"); d.Name != "联调报告" {
		t.Fatalf("通过后应建出交付物项: %+v", d)
	}
}

// 变更单与取消的双向互斥（AC-57，回归 R3）：未决变更单在时不能发起关闭，
// 取消生效后任务进入终态，既有变更单不得再被处理、也不接受新的审批单。
func TestFieldChangeOnTerminalTask(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	carolUser := seedUser(t, q, "carol", "王五", "carol-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	login := func(c *http.Client, username, password string) {
		t.Helper()
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	alice, bob, carol := newClient(t), newClient(t), newClient(t)
	login(alice, "alice", "alice-pass")
	login(bob, "bob", "bob-pass")
	login(carol, "carol", "carol-pass")
	sp := func(s string) *string { return &s }

	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "终态收口", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	for _, uid := range []int64{bobUser.ID, carolUser.ID} {
		resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
			api.AddProjectMembersRequest{UserIds: []int64{uid}, Role: api.Member})
		wantStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	kr1 := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	resp = doJSON(t, carol, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items:           []api.CreateTaskItem{{KeyResultId: kr1, Name: "联调验证", OwnerId: carolUser.ID, StartDate: start, EndDate: end}},
	})
	wantStatus(t, resp, http.StatusCreated)
	taskID := decodeBody[[]api.Task](t, resp)[0].Id
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/pool-review-decision", tasksURL, taskID),
		api.PoolReviewDecisionRequest{Decision: api.PoolReviewDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	// carol 提交关键字段变更 → 待 KR 负责人审批
	newEnd := openapiDate(t, "2026-10-15")
	change := api.SubmitFieldChangeRequest{Reason: sp("联调窗口顺延")}
	change.Changes.EndDate = &newEnd
	change.Changes.Name = sp("被改写的名称")
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/field-changes", tasksURL, taskID), change)
	wantStatus(t, resp, http.StatusOK)
	pending := decodeBody[api.Task](t, resp)
	changeID := pending.FieldChange.Id

	// 变更单未决期间不能发起关闭，KR 负责人的免审通道同样受互斥约束（AC-57）
	cancelURL := fmt.Sprintf("%s/%d/cancellation", tasksURL, taskID)
	for _, c := range []*http.Client{carol, bob} {
		resp = doJSON(t, c, http.MethodPost, cancelURL, api.TaskCancellationRequest{Reason: "需求取消"})
		wantStatus(t, resp, http.StatusConflict)
		resp.Body.Close()
	}

	// 变更单退回后取消恢复可用；KR 负责人本人负责 KR 下免审即时生效
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/field-changes/%d/decision", tasksURL, taskID, changeID),
		api.FieldChangeDecisionRequest{Decision: api.FieldChangeDecisionRequestDecisionRejected, Opinion: sp("窗口不顺延")})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodPost, cancelURL, api.TaskCancellationRequest{Reason: "需求取消"})
	wantStatus(t, resp, http.StatusOK)
	cancelled := decodeBody[api.Task](t, resp)
	if cancelled.Status != api.TaskStatusCancelled {
		t.Fatalf("任务应为已关闭: %+v", cancelled.Status)
	}

	// 终态任务不再接受任何审批单：既有变更单不可再处理，也不能提交新的关键字段修改
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/field-changes/%d/decision", tasksURL, taskID, changeID),
		api.FieldChangeDecisionRequest{Decision: api.FieldChangeDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/field-changes", tasksURL, taskID), change)
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()

	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	after := decodeBody[api.TaskDetail](t, resp)
	if after.Task.Name != "联调验证" || after.Task.EndDate.Time.Format("2006-01-02") != "2026-09-30" {
		t.Fatalf("终态任务字段被变更单改写: name=%q end=%v", after.Task.Name, after.Task.EndDate)
	}
	// 详情侧也不应再下发可决策标志
	if after.Task.FieldChange != nil && after.Task.FieldChange.CanDecide != nil && *after.Task.FieldChange.CanDecide {
		t.Fatalf("终态任务仍下发 canDecide=true: %+v", after.Task.FieldChange)
	}
}

// 交付物内容同态唯一（回归 D1）：库层偏唯一索引兜底，应用层删旧建新失败也不会留下两行同态记录。
func TestDeliverableFileStateUnique(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	alice := newClient(t)
	resp := doJSON(t, alice, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: "alice", Password: "alice-pass"})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	sp := func(s string) *string { return &s }

	resp = doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "唯一内容", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收", OwnerId: &aliceUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	kr1 := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{{KeyResultId: kr1, Name: "产出方案", OwnerId: aliceUser.ID, StartDate: start, EndDate: end}},
	})
	wantStatus(t, resp, http.StatusCreated)
	taskID := decodeBody[[]api.Task](t, resp)[0].Id
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/%d/deliverables", tasksURL, taskID), api.CreateDeliverableRequest{FileName: "验收方案.docx"})
	wantStructureAccepted(t, resp)
	deliverableID := deliverableOf(t, alice, base, created.Id, taskID, "验收方案").Id

	newFile := func(state, key string) error {
		_, err := q.CreateDeliverableFile(context.Background(), store.CreateDeliverableFileParams{
			DeliverableID: deliverableID, State: state, FileName: "a.pdf", ObjectKey: key, UploadedBy: aliceUser.ID,
		})
		return err
	}
	for _, state := range []string{"candidate", "current"} {
		if err := newFile(state, state+"-1"); err != nil {
			t.Fatalf("首份 %s 应可写入: %v", state, err)
		}
		if err := newFile(state, state+"-2"); err == nil {
			t.Fatalf("同一交付物项写入第二份 %s 应被库层拒绝", state)
		}
	}
}

// S3：登录限速按 (用户名, 来源 IP) 计数。攻击者从自己的 IP 打满某账号的失败额度后，
// 该账号的真实用户从别的 IP 仍能正常登录。
func TestLoginThrottleIsolatesSourceIP(t *testing.T) {
	q, pool := setupDB(t)
	seedUser(t, q, "alice", "张三", "alice-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	loginFrom := func(ip, password string) *http.Response {
		t.Helper()
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(api.LoginRequest{Username: "alice", Password: password}); err != nil {
			t.Fatalf("encode: %v", err)
		}
		req, err := http.NewRequest(http.MethodPost, base+"/auth/login", &buf)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		// Caddy 反代把真实对端追加在 XFF 尾部；前缀模拟客户端自带的伪造值。
		req.Header.Set("X-Forwarded-For", "1.2.3.4, "+ip)
		resp, err := newClient(t).Do(req)
		if err != nil {
			t.Fatalf("login: %v", err)
		}
		return resp
	}

	const attacker, victim = "10.0.0.66", "10.0.0.7"
	for i := 0; i < domain.MaxLoginFailures; i++ {
		resp := loginFrom(attacker, "wrong")
		wantStatus(t, resp, http.StatusUnauthorized)
		resp.Body.Close()
	}
	resp := loginFrom(attacker, "wrong")
	wantStatus(t, resp, http.StatusTooManyRequests)
	resp.Body.Close()

	resp = loginFrom(victim, "alice-pass")
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
}

// 过期会话有清理路径：登出与滑动续期都不删过期行，靠每小时 ticker 的 SweepAuthState 收口。
func TestDeleteExpiredSessions(t *testing.T) {
	q, _ := setupDB(t)
	ctx := context.Background()
	alice := seedUser(t, q, "alice", "张三", "alice-pass")

	mustSession := func(token string, expiresAt time.Time) {
		t.Helper()
		if err := q.CreateSession(ctx, store.CreateSessionParams{
			Token: token, UserID: alice.ID,
			ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
		}); err != nil {
			t.Fatalf("create session %s: %v", token, err)
		}
	}
	now := time.Now()
	mustSession("expired", now.Add(-time.Hour))
	mustSession("live", now.Add(domain.SessionTTL))

	n, err := q.DeleteExpiredSessions(ctx)
	if err != nil {
		t.Fatalf("DeleteExpiredSessions: %v", err)
	}
	if n != 1 {
		t.Fatalf("清理条数 = %d, want 1", n)
	}
	if _, err := q.GetSession(ctx, "live"); err != nil {
		t.Fatalf("未过期会话不应被清理: %v", err)
	}
}

// P1：一次任务写请求此前要跑 30+ 条项目级查询——写路径装饰器的写后快照与
// writeTask → taskList 把同一份卡点算了两遍。记忆化之后重复的那一整套派生消失。
// 这里数的是真实发出的 SQL 条数，作为回归护栏钉住量级。
func TestWritePathQueryBudget(t *testing.T) {
	q, pool, counter := setupCountedDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"
	sp := func(v string) *string { return &v }

	alice := newClient(t)
	resp := doJSON(t, alice, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: "alice", Password: "alice-pass"})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "查询预算", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收", OwnerId: &aliceUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	kr1 := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{{KeyResultId: kr1, Name: "联调验证", OwnerId: aliceUser.ID, StartDate: start, EndDate: end}},
	})
	wantStatus(t, resp, http.StatusCreated)
	taskID := decodeBody[[]api.Task](t, resp)[0].Id

	// 量一次典型的任务写：开始执行
	counter.reset()
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/%d/update-status", tasksURL, taskID),
		api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	got := counter.count()
	// 实测：记忆化前 37 条，记忆化后 30 条（省掉的正是写后重复的那一整套卡点派生）。
	// 预算钉在 32，留一点余量给后续新增的单条查询，但挡住「同一份卡点又算两遍」的回退。
	const budget = 32
	if got > budget {
		t.Fatalf("单次任务写的 SQL 条数 = %d，超出预算 %d（记忆化失效？）", got, budget)
	}
	t.Logf("单次任务写实际 SQL 条数 = %d（记忆化前 37）", got)

	// 服务端裁剪：krId 过滤与 includeCompleted
	resp = doJSON(t, alice, http.MethodGet, fmt.Sprintf("%s?krId=%d", tasksURL, kr1), nil)
	wantStatus(t, resp, http.StatusOK)
	if n := len(decodeBody[[]api.Task](t, resp)); n != 1 {
		t.Fatalf("按 KR 裁剪后应只剩本 KR 的任务，got %d", n)
	}
	resp = doJSON(t, alice, http.MethodGet, fmt.Sprintf("%s?krId=%d", tasksURL, kr1+9999), nil)
	wantStatus(t, resp, http.StatusOK)
	if n := len(decodeBody[[]api.Task](t, resp)); n != 0 {
		t.Fatalf("不存在的 KR 不应返回任务，got %d", n)
	}
	resp = doJSON(t, alice, http.MethodGet, fmt.Sprintf("%s/projects/%d/edges?krId=%d", base, created.Id, kr1), nil)
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
}

// §10.4／R8：写路径装饰器给项目内每一次成功写操作留痕——
// 成员增删、关系解除、成果包创建这些此前完全无痕的动作都要有据可查，
// 失败的请求不留痕，只读请求不留痕，只有项目管理员能看。
func TestWritePathAudit(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	carolUser := seedUser(t, q, "carol", "王五", "carol-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"
	sp := func(v string) *string { return &v }

	alice, bob := newClient(t), newClient(t)
	for _, l := range []struct {
		c              *http.Client
		user, password string
	}{{alice, "alice", "alice-pass"}, {bob, "bob", "bob-pass"}} {
		resp := doJSON(t, l.c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: l.user, Password: l.password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}

	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "留痕", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	membersURL := fmt.Sprintf("%s/projects/%d/members", base, created.Id)
	for _, id := range []int64{bobUser.ID, carolUser.ID} {
		resp = doJSON(t, alice, http.MethodPost, membersURL, api.AddProjectMembersRequest{UserIds: []int64{id}, Role: api.Member})
		wantStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}
	// 失败的写请求不留痕（#93 后重复加入不再是失败，改用空名单这条真失败的请求）
	resp = doJSON(t, alice, http.MethodPost, membersURL, map[string]any{"userIds": []int64{}, "role": "member"})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
	// 没有职责占位的成员可以移出
	resp = doJSON(t, alice, http.MethodDelete, fmt.Sprintf("%s/%d", membersURL, carolUser.ID), nil)
	wantStatus(t, resp, http.StatusNoContent)
	resp.Body.Close()

	auditURL := fmt.Sprintf("%s/projects/%d/audit-logs", base, created.Id)
	resp = doJSON(t, bob, http.MethodGet, auditURL, nil)
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodGet, auditURL, nil)
	wantStatus(t, resp, http.StatusOK)
	logs := decodeBody[[]api.AuditLog](t, resp)
	actions := map[string]int{}
	for _, l := range logs {
		actions[l.Action]++
		if l.ActorName == nil || *l.ActorName != aliceUser.DisplayName {
			t.Fatalf("审计应记下行动人: %+v", l)
		}
	}
	if actions["新增项目成员"] != 2 {
		t.Fatalf("成员新增留痕条数异常（失败的那次不应留痕）: %+v", actions)
	}
	if actions["移出项目成员"] != 1 {
		t.Fatalf("成员移出未留痕: %+v", actions)
	}
	// 读请求不留痕：再取一次审计，条数不变
	before := len(logs)
	resp = doJSON(t, alice, http.MethodGet, fmt.Sprintf("%s/projects/%d/tasks", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodGet, auditURL, nil)
	wantStatus(t, resp, http.StatusOK)
	if after := len(decodeBody[[]api.AuditLog](t, resp)); after != before {
		t.Fatalf("读请求不应留痕: %d → %d", before, after)
	}

	// 关系解除也留痕（此前完全无痕的一类）
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收", OwnerId: &aliceUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	kr1 := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "上游", OwnerId: aliceUser.ID, StartDate: start, EndDate: end},
			{KeyResultId: kr1, Name: "下游", OwnerId: aliceUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	var up, down int64
	for _, tk := range decodeBody[[]api.Task](t, resp) {
		if tk.Name == "上游" {
			up = tk.Id
		} else {
			down = tk.Id
		}
	}
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/%d/inputs", tasksURL, down),
		api.CreateTaskInputRequest{Necessity: api.Required, EdgeType: api.HardPrerequisite, SourceTaskIds: []int64{up}})
	wantStructureAccepted(t, resp)
	edge := edgeOf(t, alice, base, created.Id, down, "上游")
	resp = doJSON(t, alice, http.MethodDelete, fmt.Sprintf("%s/projects/%d/edges/%d", base, created.Id, edge.Id), nil)
	wantStructureAccepted(t, resp)

	resp = doJSON(t, alice, http.MethodGet, auditURL, nil)
	wantStatus(t, resp, http.StatusOK)
	actions = map[string]int{}
	for _, l := range decodeBody[[]api.AuditLog](t, resp) {
		actions[l.Action]++
	}
	for _, want := range []string{"创建 O／KR", "创建任务", "配置任务输入", "解除交付物边"} {
		if actions[want] == 0 {
			t.Fatalf("%q 未留痕: %+v", want, actions)
		}
	}
}

// AC-64 编号稳定：O 编号为自然数、KR 形如 KR1.1、任务形如 T1.1.1；
// 编号创建时分配并持久保存，删除 O2 后 O3 编号不变、新建的 O 也不复用 2。
func TestEntityCodesStable(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"
	sp := func(v string) *string { return &v }

	alice := newClient(t)
	resp := doJSON(t, alice, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: "alice", Password: "alice-pass"})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	resp = doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "编号稳定", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	objectivesURL := fmt.Sprintf("%s/projects/%d/objectives", base, created.Id)

	resp = doJSON(t, alice, http.MethodPost, objectivesURL, api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
		{Title: sp("O 一"), KeyResults: &[]api.CreateKeyResultInput{
			{Description: "KR 一一", OwnerId: &aliceUser.ID},
			{Description: "KR 一二", OwnerId: &aliceUser.ID},
		}},
		{Title: sp("O 二")},
		{Title: sp("O 三")},
	}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	codeOf := func(list []api.Objective, title string) string {
		for _, o := range list {
			if o.Title == title {
				return o.Code
			}
		}
		return ""
	}
	if codeOf(okr, "O 一") != "O1" || codeOf(okr, "O 二") != "O2" || codeOf(okr, "O 三") != "O3" {
		t.Fatalf("O 编号异常: %+v", okr)
	}
	if okr[0].KeyResults[0].Code != "KR1.1" || okr[0].KeyResults[1].Code != "KR1.2" {
		t.Fatalf("KR 编号异常: %+v", okr[0].KeyResults)
	}

	// 任务编号形如 T1.1.1，同 KR 内递增
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")
	kr11 := okr[0].KeyResults[0].Id
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{
			{KeyResultId: kr11, Name: "任务甲", OwnerId: aliceUser.ID, StartDate: start, EndDate: end},
			{KeyResultId: kr11, Name: "任务乙", OwnerId: aliceUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	byName := map[string]string{}
	var firstTaskID int64
	for _, tk := range decodeBody[[]api.Task](t, resp) {
		byName[tk.Name] = tk.Code
		if tk.Name == "任务甲" {
			firstTaskID = tk.Id
		}
	}
	if byName["任务甲"] != "T1.1.1" || byName["任务乙"] != "T1.1.2" {
		t.Fatalf("任务编号异常: %+v", byName)
	}
	_ = firstTaskID

	// 删除 O2：O3 编号不变，新建的 O 取 O4 而不是复用 2
	var o2 int64
	for _, o := range okr {
		if o.Title == "O 二" {
			o2 = o.Id
		}
	}
	resp = doJSON(t, alice, http.MethodDelete, fmt.Sprintf("%s/%d", objectivesURL, o2), nil)
	wantStatus(t, resp, http.StatusNoContent)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPost, objectivesURL,
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{{Title: sp("O 四")}}})
	wantStatus(t, resp, http.StatusCreated)
	after := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	if codeOf(after, "O 一") != "O1" || codeOf(after, "O 三") != "O3" {
		t.Fatalf("删除 O2 后其余 O 编号应保持不变: %+v", after)
	}
	if codeOf(after, "O 四") != "O4" {
		t.Fatalf("新建 O 不应复用被删的编号: %+v", after)
	}
}

// S3：改口令后本人其余会话立即失效，当前会话保留；新口令生效、旧口令失效。
func TestChangePasswordRevokesOtherSessions(t *testing.T) {
	q, pool := setupDB(t)
	seedUser(t, q, "alice", "张三", "alice-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	// 两个客户端各自登录，模拟同一账号的两个会话
	first, second := newClient(t), newClient(t)
	for _, c := range []*http.Client{first, second} {
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login",
			api.LoginRequest{Username: "alice", Password: "alice-pass"})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}

	// 当前口令不对、新口令过短、新口令与旧口令相同都要被拒
	resp := doJSON(t, first, http.MethodPost, base+"/auth/change-password",
		api.ChangePasswordRequest{CurrentPassword: "wrong-pass", NewPassword: "brand-new-pass"})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
	resp = doJSON(t, first, http.MethodPost, base+"/auth/change-password",
		api.ChangePasswordRequest{CurrentPassword: "alice-pass", NewPassword: "short7x"})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
	resp = doJSON(t, first, http.MethodPost, base+"/auth/change-password",
		api.ChangePasswordRequest{CurrentPassword: "alice-pass", NewPassword: "alice-pass"})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()

	// 改口令成功
	resp = doJSON(t, first, http.MethodPost, base+"/auth/change-password",
		api.ChangePasswordRequest{CurrentPassword: "alice-pass", NewPassword: "brand-new-pass"})
	wantStatus(t, resp, http.StatusNoContent)
	resp.Body.Close()

	// 当前会话仍然可用，另一个会话立即失效
	resp = doJSON(t, first, http.MethodGet, base+"/auth/me", nil)
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, second, http.MethodGet, base+"/auth/me", nil)
	wantStatus(t, resp, http.StatusUnauthorized)
	resp.Body.Close()

	// 旧口令不再能登录，新口令可以
	third := newClient(t)
	resp = doJSON(t, third, http.MethodPost, base+"/auth/login",
		api.LoginRequest{Username: "alice", Password: "alice-pass"})
	wantStatus(t, resp, http.StatusUnauthorized)
	resp.Body.Close()
	resp = doJSON(t, third, http.MethodPost, base+"/auth/login",
		api.LoginRequest{Username: "alice", Password: "brand-new-pass"})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
}

// AC-60 项目规则设置：三项阈值按项目生效、仅项目管理员可改；
// 审批超时阈值改小后，「审批超时」卡点与我的工作审批件的超期标红按新值同源判定（R12）。
func TestProjectSettingsThresholds(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	carolUser := seedUser(t, q, "carol", "王五", "carol-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	login := func(c *http.Client, username, password string) {
		t.Helper()
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	alice, bob, carol := newClient(t), newClient(t), newClient(t)
	login(alice, "alice", "alice-pass")
	login(bob, "bob", "bob-pass")
	login(carol, "carol", "carol-pass")
	sp := func(s string) *string { return &s }

	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "规则试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	for _, m := range []struct {
		id   int64
		role api.MemberRole
	}{{bobUser.ID, api.Member}, {carolUser.ID, api.Member}} {
		resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
			api.AddProjectMembersRequest{UserIds: []int64{m.id}, Role: m.role})
		wantStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}
	settingsURL := fmt.Sprintf("%s/projects/%d/settings", base, created.Id)

	// 默认值：审批超时 3 天、临期 3 天、提醒每天 1 次；canEdit 只对项目管理员为真
	resp = doJSON(t, alice, http.MethodGet, settingsURL, nil)
	wantStatus(t, resp, http.StatusOK)
	got := decodeBody[api.ProjectSettings](t, resp)
	if got.ApprovalTimeoutDays != 3 || got.DueSoonDays != 3 || got.RemindDailyLimit != 1 || !got.CanEdit {
		t.Fatalf("默认规则设置异常: %+v", got)
	}
	resp = doJSON(t, bob, http.MethodGet, settingsURL, nil)
	wantStatus(t, resp, http.StatusOK)
	if g := decodeBody[api.ProjectSettings](t, resp); g.CanEdit {
		t.Fatalf("项目成员不应可改规则设置: %+v", g)
	}

	// 项目成员改不动；取值越界 422
	resp = doJSON(t, bob, http.MethodPut, settingsURL, api.UpdateProjectSettingsRequest{
		ApprovalTimeoutDays: 1, DueSoonDays: 3, RemindDailyLimit: 1,
	})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	// 越界取值由契约校验挡在 handler 之前（domain.ValidateProjectSettings 为同口径兜底）
	resp = doJSON(t, alice, http.MethodPut, settingsURL, api.UpdateProjectSettingsRequest{
		ApprovalTimeoutDays: 0, DueSoonDays: 3, RemindDailyLimit: 1,
	})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	if e := decodeBody[api.Error](t, resp); e.Code != "invalid_request" {
		t.Fatalf("code = %q, want invalid_request", e.Code)
	}

	// 建一个停在入池审批的任务，并把提交时间往前拨 2 天
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("按时推进"), KeyResults: &[]api.CreateKeyResultInput{{Description: "入池及时审批", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	kr1 := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives[0].KeyResults[0].Id
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/projects/%d/tasks", base, created.Id),
		api.CreateTaskBatchRequest{
			SubmitForReview: true,
			Items: []api.CreateTaskItem{{
				KeyResultId: kr1, Name: "等审批的任务", OwnerId: carolUser.ID,
				StartDate: openapiDate(t, "2026-09-01"), EndDate: openapiDate(t, "2026-09-30"),
			}},
		})
	wantStatus(t, resp, http.StatusCreated)
	task := decodeBody[[]api.Task](t, resp)[0]
	if _, err := pool.Exec(context.Background(),
		"UPDATE pool_reviews SET submitted_at = now() - interval '2 days' WHERE task_id = $1", task.Id); err != nil {
		t.Fatalf("回拨提交时间失败: %v", err)
	}

	blockersURL := fmt.Sprintf("%s/projects/%d/blockers", base, created.Id)
	myWorkURL := fmt.Sprintf("%s/projects/%d/my-work", base, created.Id)
	approvalTimeouts := func() []api.Blocker {
		t.Helper()
		r := doJSON(t, bob, http.MethodGet, blockersURL, nil)
		wantStatus(t, r, http.StatusOK)
		out := []api.Blocker{}
		for _, b := range decodeBody[[]api.Blocker](t, r) {
			if b.Kind == api.ApprovalTimeout {
				out = append(out, b)
			}
		}
		return out
	}
	approvalOverdue := func() bool {
		t.Helper()
		r := doJSON(t, bob, http.MethodGet, myWorkURL, nil)
		wantStatus(t, r, http.StatusOK)
		items := decodeBody[api.MyWork](t, r).Approvals
		if len(items) != 1 {
			t.Fatalf("待我审批应有 1 条: %+v", items)
		}
		return items[0].Overdue != nil && *items[0].Overdue
	}

	// 默认 3 天：等待 2 天既不成卡点也不标红
	if got := approvalTimeouts(); len(got) != 0 {
		t.Fatalf("未达默认阈值不应有审批超时卡点: %+v", got)
	}
	if approvalOverdue() {
		t.Fatal("未达默认阈值的审批件不应标红")
	}

	// 阈值改为 1 天：同一份事实立刻成卡点并标红，两处同源
	resp = doJSON(t, alice, http.MethodPut, settingsURL, api.UpdateProjectSettingsRequest{
		ApprovalTimeoutDays: 1, DueSoonDays: 2, RemindDailyLimit: 2,
	})
	wantStatus(t, resp, http.StatusOK)
	if g := decodeBody[api.ProjectSettings](t, resp); g.ApprovalTimeoutDays != 1 || g.DueSoonDays != 2 || g.RemindDailyLimit != 2 {
		t.Fatalf("规则设置未落库: %+v", g)
	}
	timeouts := approvalTimeouts()
	if len(timeouts) != 1 || timeouts[0].TaskId != task.Id {
		t.Fatalf("阈值改为 1 天后应派生审批超时卡点: %+v", timeouts)
	}
	if !strings.Contains(timeouts[0].Reason, "超过阈值 1 天") {
		t.Fatalf("卡点原因应按新阈值出文案: %q", timeouts[0].Reason)
	}
	if !approvalOverdue() {
		t.Fatal("阈值改为 1 天后我的工作审批件应标红")
	}

	// 提醒冷却上限改为 2：同一发起人对同一被提醒人的同一任务当天可发两次，第三次被拒
	remindURL := fmt.Sprintf("%s/projects/%d/reminders", base, created.Id)
	key := timeouts[0].Key
	for i := 0; i < 2; i++ {
		resp = doJSON(t, carol, http.MethodPost, remindURL, api.RemindRequest{TargetKey: key})
		wantStatus(t, resp, http.StatusNoContent)
		resp.Body.Close()
	}
	resp = doJSON(t, carol, http.MethodPost, remindURL, api.RemindRequest{TargetKey: key})
	wantStatus(t, resp, http.StatusConflict)
	if e := decodeBody[api.Error](t, resp); e.Code != "remind_cooldown" {
		t.Fatalf("code = %q, want remind_cooldown", e.Code)
	}
}

// 参与人（#72-1 裁定；词汇表「参与人」、主 PRD §9.2）：按需字段，配置直接生效并留痕，
// 不进审批链、不影响权限、不产生待办——名单变化不得在「我的工作」里给参与人多出任何事项。
func TestTaskParticipants(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	carolUser := seedUser(t, q, "carol", "王五", "carol-pass")
	daveUser := seedUser(t, q, "dave", "赵六", "dave-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	login := func(c *http.Client, username, password string) {
		t.Helper()
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	alice, bob, carol, dave := newClient(t), newClient(t), newClient(t), newClient(t)
	login(alice, "alice", "alice-pass")
	login(bob, "bob", "bob-pass")
	login(carol, "carol", "carol-pass")
	login(dave, "dave", "dave-pass")
	sp := func(s string) *string { return &s }

	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "参与人试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	for _, m := range []struct {
		id   int64
		role api.MemberRole
	}{{bobUser.ID, api.Member}, {carolUser.ID, api.Member}, {daveUser.ID, api.Viewer}} {
		resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
			api.AddProjectMembersRequest{UserIds: []int64{m.id}, Role: m.role})
		wantStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("协作到位"), KeyResults: &[]api.CreateKeyResultInput{{Description: "关键材料按时产出", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	resp = doJSON(t, carol, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "编写评审材料", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	taskID := decodeBody[[]api.Task](t, resp)[0].Id
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/pool-review-decision", tasksURL, taskID),
		api.PoolReviewDecisionRequest{Decision: api.PoolReviewDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	participantsURL := fmt.Sprintf("%s/%d/participants", tasksURL, taskID)
	// 无关成员不可配置
	resp = doJSON(t, dave, http.MethodPut, participantsURL, api.SetParticipantsRequest{UserIds: []int64{aliceUser.ID}})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	// 非项目成员进不了名单
	resp = doJSON(t, carol, http.MethodPut, participantsURL, api.SetParticipantsRequest{UserIds: []int64{9999}})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
	// 负责人已单列，不重复出现在参与人里
	resp = doJSON(t, carol, http.MethodPut, participantsURL, api.SetParticipantsRequest{UserIds: []int64{carolUser.ID}})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()

	// 负责人配置生效：不属关键字段，直接落库，任务上不留任何待审批变更单
	resp = doJSON(t, carol, http.MethodPut, participantsURL,
		api.SetParticipantsRequest{UserIds: []int64{daveUser.ID, aliceUser.ID, daveUser.ID}})
	wantStatus(t, resp, http.StatusOK)
	saved := decodeBody[api.Task](t, resp)
	if saved.Participants == nil || len(*saved.Participants) != 2 {
		t.Fatalf("参与人应直接生效并去重: %+v", saved.Participants)
	}
	if saved.FieldChange != nil {
		t.Fatalf("参与人不属关键字段，不应产生变更单: %+v", saved.FieldChange)
	}
	if saved.PendingReviewCount != nil && *saved.PendingReviewCount != 0 {
		t.Fatalf("参与人不应进审批链: %+v", saved.PendingReviewCount)
	}
	names := map[string]bool{}
	for _, p := range *saved.Participants {
		names[p.DisplayName] = true
	}
	if !names["赵六"] || !names["张三"] {
		t.Fatalf("参与人名单不对: %+v", *saved.Participants)
	}

	// 参与人不获得任何权限，也不因此收到待办
	resp = doJSON(t, dave, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	seen := decodeBody[api.TaskDetail](t, resp).Task
	if seen.CanUpdateProgress || seen.CanProposeFieldChange || seen.CanStart {
		t.Fatalf("参与人不应获得任何写权限: %+v", seen)
	}
	if seen.CanManageParticipants != nil && *seen.CanManageParticipants {
		t.Fatalf("访客即使是参与人也不能改名单: %+v", seen)
	}
	resp = doJSON(t, dave, http.MethodGet, fmt.Sprintf("%s/projects/%d/my-work", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	work := decodeBody[api.MyWork](t, resp)
	if n := len(work.Pending) + len(work.Approvals) + len(work.Receipts) + len(work.Waiting) + len(work.Blockers); n != 0 {
		t.Fatalf("参与人不应收到任何事项，得到 %d 条", n)
	}

	// 清空名单同样直接生效
	resp = doJSON(t, alice, http.MethodPut, participantsURL, api.SetParticipantsRequest{UserIds: []int64{}})
	wantStatus(t, resp, http.StatusOK)
	if cleared := decodeBody[api.Task](t, resp); cleared.Participants == nil || len(*cleared.Participants) != 0 {
		t.Fatalf("清空参与人未生效: %+v", cleared.Participants)
	}

	// 留痕：§10.4 全量操作审计里能查到「配置参与人」
	resp = doJSON(t, alice, http.MethodGet, fmt.Sprintf("%s/projects/%d/audit-logs", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	found := false
	for _, l := range decodeBody[[]api.AuditLog](t, resp) {
		if l.Action == "配置参与人" {
			found = true
		}
	}
	if !found {
		t.Fatal("配置参与人应留痕")
	}
}

// 成果更新（AC-66、AC-33、AC-39；#78）：已完成任务重新发起交付物更新，走同一道完成审批，
// 审批期间任务保持已完成、当前内容继续有效；终审通过后候选覆盖当前内容且旧文件不可恢复，
// 退回则候选删除、当前内容不变。「已生效 · 有更新审核中」由本流程产生。
func TestResultUpdateFlow(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	carolUser := seedUser(t, q, "carol", "王五", "carol-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"
	login := func(c *http.Client, username, password string) {
		t.Helper()
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	alice, bob, carol := newClient(t), newClient(t), newClient(t)
	login(alice, "alice", "alice-pass")
	login(bob, "bob", "bob-pass")
	login(carol, "carol", "carol-pass")
	sp := func(s string) *string { return &s }

	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "成果更新试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	for _, uid := range []int64{bobUser.ID, carolUser.ID} {
		resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
			api.AddProjectMembersRequest{UserIds: []int64{uid}, Role: api.Member})
		wantStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("成果可持续更新"), KeyResults: &[]api.CreateKeyResultInput{{Description: "交付物随迭代更新", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	// carol 建任务 → bob 入池通过 → carol 执行 → 首次定稿走完完成审批
	resp = doJSON(t, carol, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "输出接口说明", OwnerId: carolUser.ID, StartDate: start, EndDate: end, ExpectedDeliverable: sp("接口说明")},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	taskID := decodeBody[[]api.Task](t, resp)[0].Id
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/pool-review-decision", tasksURL, taskID),
		api.PoolReviewDecisionRequest{Decision: api.PoolReviewDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/update-status", tasksURL, taskID),
		api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	detailURL := fmt.Sprintf("%s/%d", tasksURL, taskID)
	resp = doJSON(t, carol, http.MethodGet, detailURL, nil)
	wantStatus(t, resp, http.StatusOK)
	detail := decodeBody[api.TaskDetail](t, resp)
	deliverableID := detail.Deliverables[0].Id
	uploadCandidate(t, carol, tasksURL, taskID, deliverableID, api.UploadCandidateRequest{FileName: "接口说明-v1.docx"}, "v1-bytes")
	completionURL := fmt.Sprintf("%s/%d/completion-reviews", tasksURL, taskID)
	resp = doJSON(t, carol, http.MethodPost, completionURL, api.SubmitCompletionRequest{Note: "首次定稿"})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodGet, detailURL, nil)
	wantStatus(t, resp, http.StatusOK)
	detail = decodeBody[api.TaskDetail](t, resp)
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/decision", completionURL, detail.CompletionReviews[0].Id),
		api.CompletionDecisionRequest{Decision: api.CompletionDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusOK)
	done := decodeBody[api.Task](t, resp)
	if done.Status != api.TaskStatusCompleted {
		t.Fatalf("首次定稿后应已完成: %+v", done)
	}
	if done.ResultUpdate == nil || *done.ResultUpdate != api.ResultUpdateStateNone {
		t.Fatalf("尚未发起成果更新: %+v", done.ResultUpdate)
	}
	// 派生入口按身份区分：任务负责人可发起，KR 负责人（项目成员）不可代发起
	resp = doJSON(t, carol, http.MethodGet, detailURL, nil)
	wantStatus(t, resp, http.StatusOK)
	detail = decodeBody[api.TaskDetail](t, resp)
	if detail.Task.CanStartResultUpdate == nil || !*detail.Task.CanStartResultUpdate {
		t.Fatalf("已完成任务的负责人应可发起成果更新: %+v", detail.Task)
	}
	resp = doJSON(t, bob, http.MethodGet, detailURL, nil)
	wantStatus(t, resp, http.StatusOK)
	if bobView := decodeBody[api.TaskDetail](t, resp); bobView.Task.CanStartResultUpdate == nil || *bobView.Task.CanStartResultUpdate {
		t.Fatalf("KR 负责人不是任务负责人时不应有发起入口: %+v", bobView.Task.CanStartResultUpdate)
	}
	resp = doJSON(t, alice, http.MethodGet, detailURL, nil)
	wantStatus(t, resp, http.StatusOK)
	detail = decodeBody[api.TaskDetail](t, resp)
	firstFileID := detail.Deliverables[0].Current.Id

	// 未发起成果更新时已完成任务不能传候选（§5.3）
	resultUpdateURL := fmt.Sprintf("%s/%d/result-update", tasksURL, taskID)
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/deliverables/%d/candidate", tasksURL, taskID, deliverableID),
		api.UploadCandidateRequest{FileName: "偷偷替换.docx"})
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()

	// 无关成员不可发起；负责人可发起，且不改变任务生命周期状态
	resp = doJSON(t, bob, http.MethodPost, resultUpdateURL, nil)
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodPost, resultUpdateURL, nil)
	wantStatus(t, resp, http.StatusOK)
	opened := decodeBody[api.Task](t, resp)
	if opened.Status != api.TaskStatusCompleted {
		t.Fatalf("成果更新不改变任务状态: %+v", opened.Status)
	}
	if opened.ResultUpdate == nil || *opened.ResultUpdate != api.ResultUpdateStateOpen {
		t.Fatalf("成果更新进程应为 open: %+v", opened.ResultUpdate)
	}
	if opened.CanUploadCandidate == nil || !*opened.CanUploadCandidate {
		t.Fatalf("发起后应可登记候选内容: %+v", opened)
	}
	// 同一任务至多一件在途
	resp = doJSON(t, carol, http.MethodPost, resultUpdateURL, nil)
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()

	// 上传新候选并提交：任务仍为已完成，内容状态是「已生效 · 有更新审核中」，当前内容仍可下载
	uploadCandidate(t, carol, tasksURL, taskID, deliverableID, api.UploadCandidateRequest{FileName: "接口说明-v2.docx"}, "v2-bytes")
	// AC-67：候选已上传但尚未随完成申请提交，任务详情与归档都只能说「待提交审核」
	resp = doJSON(t, carol, http.MethodGet, detailURL, nil)
	wantStatus(t, resp, http.StatusOK)
	detail = decodeBody[api.TaskDetail](t, resp)
	if ds := detail.Deliverables[0]; ds.ContentState != api.DeliverableContentStatePendingSubmit || ds.ContentStateLabel != "待提交审核" {
		t.Fatalf("未提交的候选不得声称在审: %q / %q", ds.ContentState, ds.ContentStateLabel)
	}
	resp = doJSON(t, alice, http.MethodGet, fmt.Sprintf("%s/projects/%d/artifacts", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	for _, o := range decodeBody[[]api.ArtifactObjective](t, resp) {
		for _, kr := range o.Krs {
			for _, at := range kr.Tasks {
				for _, ad := range at.Deliverables {
					if at.TaskId == taskID && ad.ContentState != api.DeliverableContentStatePendingSubmit {
						t.Fatalf("归档也应显示待提交审核: %q", ad.ContentState)
					}
				}
			}
		}
	}
	resp = doJSON(t, carol, http.MethodPost, completionURL, api.SubmitCompletionRequest{Note: "补充错误码后更新"})
	wantStatus(t, resp, http.StatusOK)
	reviewing := decodeBody[api.Task](t, resp)
	if reviewing.Status != api.TaskStatusCompleted {
		t.Fatalf("成果更新审批期间任务应保持已完成: %+v", reviewing.Status)
	}
	if reviewing.ResultUpdate == nil || *reviewing.ResultUpdate != api.ResultUpdateStateReviewing {
		t.Fatalf("提交后成果更新进程应为 reviewing: %+v", reviewing.ResultUpdate)
	}
	resp = doJSON(t, alice, http.MethodGet, detailURL, nil)
	wantStatus(t, resp, http.StatusOK)
	detail = decodeBody[api.TaskDetail](t, resp)
	d := detail.Deliverables[0]
	if d.ContentState != api.DeliverableContentStateUpdating || d.ContentStateLabel != "已生效 · 有更新审核中" {
		t.Fatalf("审核中应为「已生效 · 有更新审核中」: %q / %q", d.ContentState, d.ContentStateLabel)
	}
	if d.Current == nil || d.Current.Id != firstFileID {
		t.Fatalf("审核期间当前内容不得被提前替换: %+v", d.Current)
	}
	if d.Candidate == nil || d.Candidate.FileName != "接口说明-v2.docx" {
		t.Fatalf("候选内容应在审: %+v", d.Candidate)
	}
	// 审核期间不能再传候选
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/deliverables/%d/candidate", tasksURL, taskID, deliverableID),
		api.UploadCandidateRequest{FileName: "再改一版.docx"})
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()

	// AC-39：终审通过后候选覆盖当前内容，旧文件永久删除
	updateReviewID := detail.CompletionReviews[0].Id
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/decision", completionURL, updateReviewID),
		api.CompletionDecisionRequest{Decision: api.CompletionDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusOK)
	updated := decodeBody[api.Task](t, resp)
	if updated.Status != api.TaskStatusCompleted {
		t.Fatalf("成果更新通过后任务仍为已完成: %+v", updated.Status)
	}
	if updated.ResultUpdate == nil || *updated.ResultUpdate != api.ResultUpdateStateNone {
		t.Fatalf("终审后成果更新进程应结束: %+v", updated.ResultUpdate)
	}
	resp = doJSON(t, alice, http.MethodGet, detailURL, nil)
	wantStatus(t, resp, http.StatusOK)
	detail = decodeBody[api.TaskDetail](t, resp)
	d = detail.Deliverables[0]
	if d.Current == nil || d.Current.FileName != "接口说明-v2.docx" || d.Current.Id == firstFileID {
		t.Fatalf("候选应覆盖为新的当前内容: %+v", d.Current)
	}
	if d.Candidate != nil || d.ContentState != api.DeliverableContentStateEffective {
		t.Fatalf("通过后不应残留候选: %+v / %q", d.Candidate, d.ContentState)
	}
	files, err := q.ListDeliverableFilesByTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("列任务文件: %v", err)
	}
	for _, f := range files {
		if f.ID == firstFileID {
			t.Fatal("被覆盖的旧文件应永久删除、不可恢复")
		}
	}

	// 退回路径：再发起一次，候选被删除、当前内容不变、任务仍为已完成
	resp = doJSON(t, carol, http.MethodPost, resultUpdateURL, nil)
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	uploadCandidate(t, carol, tasksURL, taskID, deliverableID, api.UploadCandidateRequest{FileName: "接口说明-v3.docx"}, "v3-bytes")
	resp = doJSON(t, carol, http.MethodPost, completionURL, api.SubmitCompletionRequest{Note: "再更新一版"})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodGet, detailURL, nil)
	wantStatus(t, resp, http.StatusOK)
	detail = decodeBody[api.TaskDetail](t, resp)
	op := "错误码仍有遗漏"
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/decision", completionURL, detail.CompletionReviews[0].Id),
		api.CompletionDecisionRequest{Decision: api.CompletionDecisionRequestDecisionRejected, Opinion: &op})
	wantStatus(t, resp, http.StatusOK)
	rejected := decodeBody[api.Task](t, resp)
	if rejected.Status != api.TaskStatusCompleted {
		t.Fatalf("成果更新退回后任务仍为已完成: %+v", rejected.Status)
	}
	if rejected.ResultUpdate == nil || *rejected.ResultUpdate != api.ResultUpdateStateNone {
		t.Fatalf("退回后成果更新进程应结束: %+v", rejected.ResultUpdate)
	}
	resp = doJSON(t, alice, http.MethodGet, detailURL, nil)
	wantStatus(t, resp, http.StatusOK)
	detail = decodeBody[api.TaskDetail](t, resp)
	d = detail.Deliverables[0]
	if d.Candidate != nil {
		t.Fatalf("退回后候选应删除: %+v", d.Candidate)
	}
	if d.Current == nil || d.Current.FileName != "接口说明-v2.docx" {
		t.Fatalf("退回不改变当前内容: %+v", d.Current)
	}
	// 发起、通过、退回均进任务动态（#64 写路径留痕）
	kinds := map[string]bool{}
	for _, a := range detail.Activities {
		kinds[string(a.Kind)] = true
	}
	for _, want := range []string{domain.ActivityResultUpdateStarted, domain.ActivityCompletionApproved, domain.ActivityCompletionRejected} {
		if !kinds[want] {
			t.Fatalf("任务动态缺少 %s: %+v", want, kinds)
		}
	}

	// 已关闭任务永不可发起成果更新
	resp = doJSON(t, bob, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: false,
		Items:           []api.CreateTaskItem{{KeyResultId: kr1, Name: "待取消任务", OwnerId: bobUser.ID, StartDate: start, EndDate: end}},
	})
	wantStatus(t, resp, http.StatusCreated)
	var cancelledID int64
	for _, tk := range decodeBody[[]api.Task](t, resp) {
		if tk.Name == "待取消任务" {
			cancelledID = tk.Id
		}
	}
	if cancelledID == 0 {
		t.Fatal("未找到待取消任务")
	}
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/cancellation", tasksURL, cancelledID),
		api.TaskCancellationRequest{Reason: "需求取消"})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/result-update", tasksURL, cancelledID), nil)
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()
}

// AC-69：项目可见性开关。public 项目让系统内任何登录用户成为隐式访客——
// 读全开、写全关，两件事在同一处判定（domain.ProjectIdentity），这里逐条验到端点上。
func TestPublicProjectImplicitViewer(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	daveUser := seedUser(t, q, "dave", "赵六", "dave-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	login := func(c *http.Client, username, password string) {
		t.Helper()
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	alice, bob, dave := newClient(t), newClient(t), newClient(t)
	login(alice, "alice", "alice-pass")
	login(bob, "bob", "bob-pass")
	login(dave, "dave", "dave-pass")

	sp := func(s string) *string { return &s }
	ipt := func(v int) *int { return &v }

	// alice 建项目并拉 bob 进来当项目成员；dave 始终不是本项目的任何成员。
	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "公开可见性试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	projectURL := fmt.Sprintf("%s/projects/%d", base, created.Id)
	resp = doJSON(t, alice, http.MethodPost, projectURL+"/members",
		api.AddProjectMembersRequest{UserIds: []int64{bobUser.ID}, Role: api.Member})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPost, projectURL+"/objectives",
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	kr1 := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives[0].KeyResults[0].Id
	tasksURL := projectURL + "/tasks"
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")
	resp = doJSON(t, bob, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "输出验收方案", OwnerId: bobUser.ID, StartDate: start, EndDate: end,
				ExpectedDeliverable: sp("验收方案")},
			{KeyResultId: kr1, Name: "承接验收方案", OwnerId: bobUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	tasks := decodeBody[[]api.Task](t, resp)
	taskID, downstreamID := tasks[0].Id, tasks[1].Id

	// 私有阶段：非成员一律 404，项目列表里也看不到（与现状一致）。
	resp = doJSON(t, dave, http.MethodGet, projectURL, nil)
	wantStatus(t, resp, http.StatusNotFound)
	resp.Body.Close()
	resp = doJSON(t, dave, http.MethodGet, tasksURL, nil)
	wantStatus(t, resp, http.StatusNotFound)
	resp.Body.Close()
	resp = doJSON(t, dave, http.MethodGet, base+"/projects", nil)
	wantStatus(t, resp, http.StatusOK)
	for _, p := range decodeBody[[]api.Project](t, resp) {
		if p.Id == created.Id {
			t.Fatalf("私有项目不应出现在非成员的项目列表: %+v", p)
		}
	}

	// 只有项目负责人与项目管理员能改这个开关：bob 是项目成员，403。
	resp = doJSON(t, bob, http.MethodPut, projectURL, api.UpdateProjectRequest{
		Name: created.Name, OwnerId: aliceUser.ID, Status: api.ProjectStatusNotStarted, Visibility: api.Public,
	})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	// 非法取值 422：二值枚举在契约边界就被请求校验中间件挡下（与项目状态同一处理），
	// domain.ValidateProjectVisibility 是同一条规则在域层的表述，兜住绕过中间件的调用。
	resp = doJSON(t, alice, http.MethodPut, projectURL, map[string]any{
		"name": created.Name, "ownerId": aliceUser.ID, "status": "not_started", "visibility": "internal",
	})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()

	// 切公开
	resp = doJSON(t, alice, http.MethodPut, projectURL, api.UpdateProjectRequest{
		Name: created.Name, OwnerId: aliceUser.ID, Status: api.ProjectStatusNotStarted, Visibility: api.Public,
	})
	wantStatus(t, resp, http.StatusOK)
	if p := decodeBody[api.Project](t, resp); p.Visibility != api.Public || p.VisibilityLabel != "公开项目" || p.ImplicitViewer {
		t.Fatalf("切公开后的项目字段异常: %+v", p)
	}

	// 读：dave 以隐式访客身份看得到全部读端点，派生字段标明他不是成员。
	resp = doJSON(t, dave, http.MethodGet, projectURL, nil)
	wantStatus(t, resp, http.StatusOK)
	seen := decodeBody[api.Project](t, resp)
	if !seen.ImplicitViewer || seen.CanEdit || seen.CanManageMembers {
		t.Fatalf("隐式访客派生字段异常: %+v", seen)
	}
	for _, path := range []string{
		"", "/tasks", "/objectives", "/edges", "/blockers", "/artifacts", "/packages", "/report", "/my-work", "/members", "/settings",
		fmt.Sprintf("/tasks/%d", taskID),
	} {
		resp = doJSON(t, dave, http.MethodGet, projectURL+path, nil)
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	// 事实与成员看到的一致（AC-21 同一份事实）
	factsOf := func(c *http.Client) []int64 {
		t.Helper()
		r := doJSON(t, c, http.MethodGet, tasksURL, nil)
		wantStatus(t, r, http.StatusOK)
		out := []int64{}
		for _, task := range decodeBody[[]api.Task](t, r) {
			out = append(out, task.Id)
		}
		return out
	}
	if len(factsOf(dave)) != len(factsOf(bob)) {
		t.Fatalf("隐式访客看到的任务数量应与成员一致: %v vs %v", factsOf(dave), factsOf(bob))
	}
	// 项目列表里能拿到公开项目，且带着「我不是成员」的标记
	resp = doJSON(t, dave, http.MethodGet, base+"/projects", nil)
	wantStatus(t, resp, http.StatusOK)
	var listed *api.Project
	for _, p := range decodeBody[[]api.Project](t, resp) {
		if p.Id == created.Id {
			cp := p
			listed = &cp
		}
	}
	if listed == nil || !listed.ImplicitViewer {
		t.Fatalf("公开项目应出现在非成员列表并标记 implicitViewer: %+v", listed)
	}

	// 下载：交付物文件的预签名地址照常发放（裁决 D 附「可看可下载全部」）。
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/update-status", tasksURL, taskID),
		api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	deliverableID := decodeBody[api.TaskDetail](t, resp).Deliverables[0].Id
	file := uploadCandidate(t, bob, tasksURL, taskID, deliverableID,
		api.UploadCandidateRequest{FileName: "验收方案.docx", FileType: sp("docx")}, "candidate-bytes")
	resp = doJSON(t, dave, http.MethodGet, fmt.Sprintf("%s/files/%d/download-url", projectURL, file.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	if u := decodeBody[api.DownloadUrlResponse](t, resp); u.Url == "" {
		t.Fatal("隐式访客应能取到下载地址")
	}

	// 写：每一个写端点都拒绝。403 是权限判定的结果；少数端点先撞上「目标不属于我」的 404／409，
	// 一并接受——要点是没有任何一个写动作成功。
	writes := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"编辑项目", http.MethodPut, "", api.UpdateProjectRequest{
			Name: created.Name, OwnerId: aliceUser.ID, Status: api.ProjectStatusNotStarted, Visibility: api.Private}},
		{"加成员", http.MethodPost, "/members", api.AddProjectMembersRequest{UserIds: []int64{daveUser.ID}, Role: api.Member}},
		{"改规则设置", http.MethodPut, "/settings", api.UpdateProjectSettingsRequest{ApprovalTimeoutDays: 5, DueSoonDays: 5, RemindDailyLimit: 5}},
		{"建 O／KR", http.MethodPost, "/objectives", api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{{Title: sp("插一脚")}}}},
		{"建任务", http.MethodPost, "/tasks", api.CreateTaskBatchRequest{Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "插进来的任务", OwnerId: daveUser.ID, StartDate: start, EndDate: end}}}},
		{"发讨论", http.MethodPost, fmt.Sprintf("/tasks/%d/discussions", taskID), api.CreateDiscussionRequest{Content: "路过说一句"}},
		{"配置输入", http.MethodPost, fmt.Sprintf("/tasks/%d/inputs", downstreamID), api.CreateTaskInputRequest{
			Necessity: api.Required, EdgeType: api.HardPrerequisite, SourceTaskIds: []int64{taskID}}},
		{"新增交付物项", http.MethodPost, fmt.Sprintf("/tasks/%d/deliverables", taskID), api.CreateDeliverableRequest{FileName: "插进来的成果.docx"}},
		{"登记候选内容", http.MethodPost, fmt.Sprintf("/tasks/%d/deliverables/%d/candidate", taskID, deliverableID),
			api.UploadCandidateRequest{FileName: "覆盖.docx"}},
		{"改进度", http.MethodPut, fmt.Sprintf("/tasks/%d/progress", taskID), api.UpdateTaskProgressRequest{Progress: ipt(80)}},
		{"改状态", http.MethodPost, fmt.Sprintf("/tasks/%d/update-status", taskID),
			api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress}},
		{"确认接收", http.MethodPost, fmt.Sprintf("/tasks/%d/confirm-receipt", taskID), nil},
		// 提醒目标要先解析得出来才轮到权限判定：隐式访客在本项目里没有任何可提醒的目标，
		// 这里落在 404；「隐式访客不可提醒」这条规则本身由 domain 的表驱动单测覆盖。
		{"发提醒", http.MethodPost, "/reminders", api.RemindRequest{TargetKey: "wait:task:1"}},
		{"建成果包", http.MethodPost, "/packages", api.CreatePackageRequest{Name: "插进来的包", DeliverableIds: []int64{deliverableID}}},
		{"导入 O／KR", http.MethodPost, "/import", api.ImportRequest{
			Items: []api.ImportItem{{Title: sp("插进来的 O"),
				KeyResults: &[]api.ImportKrItem{{Description: "插进来的 KR"}}}}}},
		{"看导入记录", http.MethodGet, "/import-records", nil},
		{"看操作审计", http.MethodGet, "/audit-logs", nil},
	}
	for _, wcase := range writes {
		resp = doJSON(t, dave, wcase.method, projectURL+wcase.path, wcase.body)
		if resp.StatusCode < 400 {
			t.Fatalf("隐式访客不应可%s，实际 %d", wcase.name, resp.StatusCode)
		}
		if resp.StatusCode == http.StatusForbidden {
			if e := decodeBody[api.Error](t, resp); e.Code == "" {
				t.Fatalf("%s 的拒绝响应缺少错误码", wcase.name)
			}
			continue
		}
		resp.Body.Close()
	}

	// 不出现在成员列表，也不进人员选择器（成员列表就是选择器的数据源）。
	resp = doJSON(t, dave, http.MethodGet, projectURL+"/members", nil)
	wantStatus(t, resp, http.StatusOK)
	for _, m := range decodeBody[[]api.ProjectMember](t, resp) {
		if m.UserId == daveUser.ID {
			t.Fatalf("隐式访客不应出现在成员列表: %+v", m)
		}
	}
	// 不进我的工作的五组归类：他在本项目里没有任何职责。
	resp = doJSON(t, dave, http.MethodGet, projectURL+"/my-work", nil)
	wantStatus(t, resp, http.StatusOK)
	mw := decodeBody[api.MyWork](t, resp)
	if n := len(mw.Pending) + len(mw.Approvals) + len(mw.Receipts) + len(mw.Waiting) + len(mw.Blockers); n != 0 {
		t.Fatalf("隐式访客的我的工作应为空: %+v", mw)
	}
	// 不收该项目的站内通知
	resp = doJSON(t, dave, http.MethodGet, base+"/notifications", nil)
	wantStatus(t, resp, http.StatusOK)
	for _, n := range decodeBody[[]api.Notification](t, resp) {
		if n.ProjectId != nil && *n.ProjectId == created.Id {
			t.Fatalf("隐式访客不应收到该项目通知: %+v", n)
		}
	}

	// 显式成员身份优先：把 dave 加成访客后，他不再是隐式身份，讨论也随之放开（AC-35）。
	resp = doJSON(t, alice, http.MethodPost, projectURL+"/members",
		api.AddProjectMembersRequest{UserIds: []int64{daveUser.ID}, Role: api.Viewer})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()
	resp = doJSON(t, dave, http.MethodGet, projectURL, nil)
	wantStatus(t, resp, http.StatusOK)
	if p := decodeBody[api.Project](t, resp); p.ImplicitViewer {
		t.Fatalf("显式成员身份应优先于隐式身份: %+v", p)
	}
	resp = doJSON(t, dave, http.MethodPost, fmt.Sprintf("%s/tasks/%d/discussions", projectURL, taskID),
		api.CreateDiscussionRequest{Content: "作为显式访客说一句"})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()

	// 切回私有：非成员重新 404（这次用另一个非成员验证，dave 已经是显式访客）。
	resp = doJSON(t, alice, http.MethodPut, projectURL, api.UpdateProjectRequest{
		Name: created.Name, OwnerId: aliceUser.ID, Status: api.ProjectStatusNotStarted, Visibility: api.Private,
	})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	erin := newClient(t)
	seedUser(t, q, "erin", "钱七", "erin-pass")
	login(erin, "erin", "erin-pass")
	resp = doJSON(t, erin, http.MethodGet, projectURL, nil)
	wantStatus(t, resp, http.StatusNotFound)
	resp.Body.Close()
}
