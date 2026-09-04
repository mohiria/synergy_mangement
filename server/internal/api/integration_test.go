package api_test

// 集成测试：httptest + 真实 Postgres（docker compose up -d postgres）。
// 每次运行建独立数据库并用 goose 跑迁移，结束后删除。
// 无 Postgres 环境用 go test -short ./... 跳过。

import (
	"bytes"
	"errors"
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
	"github.com/jackc/pgx/v5/pgconn"
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
	if up.UploadUrl == "" || up.File.State != api.Uploading {
		t.Fatalf("候选登记异常: %+v", up)
	}
	putObject(t, up.UploadUrl, content)
	resp = doJSON(t, c, http.MethodPost, url+"/commit", api.CommitUploadRequest{FileId: up.File.Id})
	wantStatus(t, resp, http.StatusOK)
	f := decodeBody[api.DeliverableFile](t, resp)
	if f.State != api.Candidate {
		t.Fatalf("确认后应为候选: %+v", f)
	}
	return f
}

// createDeliverable 走正式入口为任务新增一个交付物项（裁决 #164：创建任务不再带预期交付物）。
// 项名由文件名派生（去掉最后一段扩展名）。
func createDeliverable(t *testing.T, c *http.Client, tasksURL string, taskID int64, fileName string) int64 {
	t.Helper()
	resp := doJSON(t, c, http.MethodPost, fmt.Sprintf("%s/%d/deliverables", tasksURL, taskID),
		api.CreateDeliverableRequest{FileName: fileName})
	wantStatus(t, resp, http.StatusOK)
	detail := decodeBody[api.Task](t, resp)
	resp2 := doJSON(t, c, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
	wantStatus(t, resp2, http.StatusOK)
	ds := decodeBody[api.TaskDetail](t, resp2).Deliverables
	if len(ds) == 0 {
		t.Fatalf("交付物项未建立: %+v", detail)
	}
	return ds[len(ds)-1].Id
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
		Email: domain.NormalizeEmail(username + "@example.com"),
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

// 裁决 12（#183）：KR 无负责人，okr_assigned 指派通知随之退场，原 dropOkrAssigned 过滤助手删除。

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

// 结构变更（输入、输入源、接收方）#172 裁决后直接生效，
// 写入接口统一返回 200 Task；下面的 helper 承担「受理成功」与「按名字找边」两件事。

// wantStructureAccepted 断言结构变更已生效并回传任务最新状态。
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

	// 错误密码 401
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

// O／KR 表格式创建（#3，AC-01；裁决 12 #183：KR 只剩描述与量化指标，创建人落 created_by）：
// 仅项目管理员／项目负责人可创建；整批一个事务；已有 O 可继续追加 KR。
func TestOkrTableBatchCreate(t *testing.T) {
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

	// 表格式批量创建：两个 O，第一个带两条 KR（描述与量化指标；裁决 12：无负责人与周期字段）
	resp = doJSON(t, alice, http.MethodPost, okrURL, api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
		{Title: sp("提升产品体验"), KeyResults: &[]api.CreateKeyResultInput{
			{Description: "上线新版工作台", Metric: sp("转化率 5%")},
			{Description: "NPS 提升到 40"},
		}},
		{Title: sp("扩大市场份额")},
	}})
	wantStatus(t, resp, http.StatusCreated)
	batchResp := decodeBody[api.CreateOkrBatchResponse](t, resp)
	list := batchResp.Objectives
	if len(list) != 2 || list[0].Title != "提升产品体验" || list[1].Title != "扩大市场份额" {
		t.Fatalf("批量创建返回异常: %+v", list)
	}
	if len(list[0].KeyResults) != 2 || len(list[1].KeyResults) != 0 {
		t.Fatalf("KR 归属异常: %+v", list)
	}
	kr := list[0].KeyResults[0]
	if kr.RiskLevel != api.Normal || kr.SortOrder != 1 {
		t.Fatalf("KR 派生字段异常: %+v", kr)
	}
	// 裁决 12：O／KR 补创建人与创建时间（详情处展示；存量数据可缺省）。
	if kr.CreatedByName == nil || *kr.CreatedByName != "张三" || kr.CreatedAt == nil {
		t.Fatalf("KR 应带创建人与创建时间: %+v", kr)
	}
	if list[0].CreatedByName == nil || *list[0].CreatedByName != "张三" || list[0].CreatedAt == nil {
		t.Fatalf("O 应带创建人与创建时间: %+v", list[0])
	}
	// AC-59：O 的风险取下级最大值——下面没有 KR 或 KR 全正常时都是正常（#82）。
	if list[0].RiskLevel != api.Normal || list[0].RiskLevelLabel != "正常" || list[0].RiskNote != nil {
		t.Fatalf("KR 全正常时 O 应为正常: %+v / %q / %+v", list[0].RiskLevel, list[0].RiskLevelLabel, list[0].RiskNote)
	}
	if list[1].RiskLevel != api.Normal {
		t.Fatalf("没有 KR 的 O 应为正常: %+v", list[1].RiskLevel)
	}
	if second := list[0].KeyResults[1]; second.SortOrder != 2 {
		t.Fatalf("第二条 KR 异常: %+v", second)
	}

	// 裁决 12：KR 无负责人，#125 指派通知退场——创建后无人收到任何通知。
	resp = doJSON(t, bob, http.MethodGet, base+"/notifications", nil)
	wantStatus(t, resp, http.StatusOK)
	if bobNotes := decodeBody[[]api.Notification](t, resp); len(bobNotes) != 0 {
		t.Fatalf("裁决 12 后创建 O/KR 不应发通知: %+v", bobNotes)
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

	// KR 描述为空 422（裁决 12 后仅剩结构校验）
	resp = doJSON(t, alice, http.MethodPost, okrURL, api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
		{Title: sp("空描述"), KeyResults: &[]api.CreateKeyResultInput{{Description: "  "}}},
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

// 任务创建直接入池（裁决 #162）：创建即为正式任务、初始状态未开始；
// 入池写任务动态并站内通知所属 KR 负责人（本人创建不另发；KR 无负责人不发）。
func TestTaskCreateDirectPoolEntry(t *testing.T) {
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

	// alice（管理员/负责人）建项目，bob、carol 为项目成员
	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "任务试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	// 裁决 10：创建任务收归项目管理员——bob 任管理员，carol 普通成员。
	for _, m := range []struct {
		id   int64
		role api.MemberRole
	}{{bobUser.ID, api.Admin}, {carolUser.ID, api.Member}} {
		resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
			api.AddProjectMembersRequest{UserIds: []int64{m.id}, Role: m.role})
		wantStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{
				{Description: "上线自动验收"},
				{Description: "另一条 KR"},
			}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	kr2 := okr[0].KeyResults[1].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-12"), openapiDate(t, "2026-09-21")

	// 裁决 #162＋裁决 10：项目负责人 alice 创建 → 直接入池、未开始
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "验证现场联动异常回退", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	list := decodeBody[[]api.Task](t, resp)
	if len(list) != 1 || list[0].Status != api.TaskStatusNotStarted {
		t.Fatalf("创建后应直接进入未开始: %+v", list)
	}
	taskID := list[0].Id
	if list[0].StatusLabel != "未开始" {
		t.Fatalf("新任务显示文案 = %q, want 未开始", list[0].StatusLabel)
	}

	// 裁决 12（#183）：KR 无负责人，原入池站内通知退场——任务动态留痕即全部事实
	resp = doJSON(t, bob, http.MethodGet, base+"/notifications", nil)
	wantStatus(t, resp, http.StatusOK)
	if bobNotes := decodeBody[[]api.Notification](t, resp); len(bobNotes) != 0 {
		t.Fatalf("裁决 12 后创建任务不应发入池通知: %+v", bobNotes)
	}
	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	detail := decodeBody[api.TaskDetail](t, resp)
	foundEntry := false
	for _, a := range detail.Activities {
		if a.Kind == api.PoolEntered {
			foundEntry = true
		}
	}
	if !foundEntry {
		t.Fatalf("任务动态应有入池留痕: %+v", detail.Activities)
	}

	// 管理员 bob 创建 → 同样直接入池
	resp = doJSON(t, bob, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "输出验收清单模板", OwnerId: bobUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	list = decodeBody[[]api.Task](t, resp)
	var ownTask *api.Task
	for i := range list {
		if list[i].Name == "输出验收清单模板" {
			ownTask = &list[i]
		}
	}
	if ownTask == nil || ownTask.Status != api.TaskStatusNotStarted {
		t.Fatalf("管理员本人创建应直接未开始: %+v", list)
	}

	// 另一条 KR 下创建照常入池
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{
			{KeyResultId: kr2, Name: "另一条 KR 下的任务", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	list = decodeBody[[]api.Task](t, resp)
	var orphan *api.Task
	for i := range list {
		if list[i].Name == "另一条 KR 下的任务" {
			orphan = &list[i]
		}
	}
	if orphan == nil || orphan.Status != api.TaskStatusNotStarted {
		t.Fatalf("另一条 KR 下创建应直接未开始: %+v", list)
	}

	// 裁决 #164：五个选填字段随创建落库，任务概况可见。
	scope := api.ReceiverScopeMembers
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{{
			KeyResultId: kr1, Name: "带选填字段的任务", OwnerId: carolUser.ID,
			StartDate: start, EndDate: end,
			CompletionCriteria: sp("回归通过率 ≥ 99%"), Description: sp("覆盖联动断链后的自动回退"),
			ParticipantIds: &[]int64{bobUser.ID}, ReviewerIds: &[]int64{bobUser.ID},
			ReceiverScope: &scope, ReceiverIds: &[]int64{bobUser.ID},
		}},
	})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodGet, tasksURL, nil)
	wantStatus(t, resp, http.StatusOK)
	var created2 *api.Task
	for _, tk := range decodeBody[[]api.Task](t, resp) {
		if tk.Name == "带选填字段的任务" {
			cp := tk
			created2 = &cp
		}
	}
	if created2 == nil {
		t.Fatal("带选填字段的任务未落库")
	}
	if created2.CompletionCriteria == nil || *created2.CompletionCriteria != "回归通过率 ≥ 99%" ||
		created2.Description == nil || *created2.Description != "覆盖联动断链后的自动回退" {
		t.Fatalf("量化指标/任务说明未落库: %+v", created2)
	}
	if created2.Participants == nil || len(*created2.Participants) != 1 || (*created2.Participants)[0].UserId != bobUser.ID {
		t.Fatalf("参与人未落库: %+v", created2.Participants)
	}
	if created2.ReceiverScope != api.ReceiverScopeMembers || created2.Receivers == nil || len(*created2.Receivers) != 1 {
		t.Fatalf("接收方未落库: %+v", created2.Receivers)
	}
	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, created2.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	if d := decodeBody[api.TaskDetail](t, resp); len(d.Reviewers) != 1 || d.Reviewers[0].UserId != bobUser.ID {
		t.Fatalf("成果审核人未落库: %+v", d.Reviewers)
	}
	// 参与人不能选负责人本人 422
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{{
			KeyResultId: kr1, Name: "非法参与人", OwnerId: carolUser.ID,
			StartDate: start, EndDate: end, ParticipantIds: &[]int64{carolUser.ID},
		}},
	})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()

	// 校验失败整批不落库：截止早于开始 422
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "倒置周期", OwnerId: carolUser.ID, StartDate: end, EndDate: start},
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
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "越权任务", OwnerId: daveUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()

	// 裁决 10：普通项目成员也不能直接创建任务 → 403
	resp = doJSON(t, carol, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "成员越权任务", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()

	// 访客可查看任务列表
	resp = doJSON(t, dave, http.MethodGet, tasksURL, nil)
	wantStatus(t, resp, http.StatusOK)
	if got := decodeBody[[]api.Task](t, resp); len(got) != 4 {
		t.Fatalf("任务列表数量异常: %+v", got)
	}
}

// 任务创建邀请（#5，AC-03；MW-19；裁决 12 #183：邀请权收归项目管理员）：
// 管理员邀请成员→受邀人通过邀请创建任务（直接入池）→邀请完成；
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

	// alice 建项目；bob 任项目管理员（裁决 12：邀请权收归管理员）、carol 项目成员
	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "邀请试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	for _, m := range []struct {
		id   int64
		role api.MemberRole
	}{{bobUser.ID, api.Admin}, {carolUser.ID, api.Member}} {
		resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
			api.AddProjectMembersRequest{UserIds: []int64{m.id}, Role: m.role})
		wantStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{
				{Description: "上线自动验收"},
				{Description: "回归通过率达标"},
			}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1, kr2 := okr[0].KeyResults[0].Id, okr[0].KeyResults[1].Id
	invitesURL := fmt.Sprintf("%s/projects/%d/task-invites", base, created.Id)
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-12"), openapiDate(t, "2026-09-21")

	// 项目成员 carol 发邀请 403（裁决 12：仅项目管理员）
	resp = doJSON(t, carol, http.MethodPost, invitesURL, api.CreateTaskInvitesRequest{KeyResultId: kr1, InviteeIds: []int64{bobUser.ID}})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()

	// 管理员 bob 邀请 carol（KR 尚无任务也可邀请）
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

	// 无关 KR 下另建任务（alice 直建），邀请不结束（词汇表）
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
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
		TaskInviteId: &inviteID,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "冒名响应", OwnerId: bobUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()

	// 批内无指定 KR 任务 422
	resp = doJSON(t, carol, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		TaskInviteId: &inviteID,
		Items: []api.CreateTaskItem{
			{KeyResultId: kr2, Name: "跑偏的响应", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	if e := decodeBody[api.Error](t, resp); e.Code != "invalid_invite_response" {
		t.Fatalf("code = %q, want invalid_invite_response", e.Code)
	}

	// AC-03：carol 通过邀请在 KR1 创建 → 任务直接入池（裁决 #162）、邀请完成
	resp = doJSON(t, carol, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		TaskInviteId: &inviteID,
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
	if invited == nil || invited.Status != api.TaskStatusNotStarted {
		t.Fatalf("邀请响应任务状态异常: %+v", tasks)
	}
	resp = doJSON(t, carol, http.MethodGet, invitesURL, nil)
	wantStatus(t, resp, http.StatusOK)
	if got = decodeBody[[]api.TaskInvite](t, resp); got[0].State != api.TaskInviteStateCompleted {
		t.Fatalf("提交后邀请应完成: %+v", got[0])
	}

	// 已完成邀请再响应 409
	resp = doJSON(t, carol, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		TaskInviteId: &inviteID,
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
		TaskInviteId: &second,
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
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收"}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	// 裁决 10：创建任务收归项目管理员——alice（项目负责人）建两个任务：bob 负责、carol 负责
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
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

	// 裁决 10（#180）：关闭收归项目管理员直接操作——原因必填 422
	cancelURL := func(id int64) string { return fmt.Sprintf("%s/%d/cancellation", tasksURL, id) }
	resp = doJSON(t, alice, http.MethodPost, cancelURL(carolTask.Id), api.CloseTaskRequest{Reason: "   "})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()

	// 任务负责人与 KR 负责人都不再能关闭 403
	reason := "需求合并，不再单独执行"
	resp = doJSON(t, carol, http.MethodPost, cancelURL(carolTask.Id), api.CloseTaskRequest{Reason: reason})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodPost, cancelURL(carolTask.Id), api.CloseTaskRequest{Reason: reason})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()

	// 项目负责人直接关闭：即时生效、保留原因、无审批环节
	resp = doJSON(t, alice, http.MethodPost, cancelURL(carolTask.Id), api.CloseTaskRequest{Reason: reason})
	wantStatus(t, resp, http.StatusOK)
	cancelled := decodeBody[api.Task](t, resp)
	if cancelled.Status != api.TaskStatusCancelled || cancelled.CancelReason == nil || *cancelled.CancelReason != reason {
		t.Fatalf("关闭后状态异常: %+v", cancelled)
	}
	resp = doJSON(t, alice, http.MethodGet, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	okr = decodeBody[[]api.Objective](t, resp)
	// AC-63：已关闭整体剔除，剩下的一个任务未填进度按 0 计入
	if s2 := okr[0].KeyResults[0].ProgressSummary; s2 == nil || s2.TotalTasks != 1 || s2.FilledTasks != 0 ||
		s2.AverageProgress == nil || *s2.AverageProgress != 0 {
		t.Fatalf("关闭后覆盖度异常: %+v", okr[0].KeyResults[0].ProgressSummary)
	}

	// 已关闭任务不可再关闭 409
	resp = doJSON(t, alice, http.MethodPost, cancelURL(carolTask.Id), api.CloseTaskRequest{Reason: reason})
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()
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
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收"}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	// alice 创建任务（负责人 bob）→ 直接入池、未开始（裁决 #162）
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "验证现场联动异常回退", OwnerId: bobUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	tasks := decodeBody[[]api.Task](t, resp)
	taskID := tasks[0].Id
	if tasks[0].CurrentStage != "待开始执行" || tasks[0].PendingActorName == nil || *tasks[0].PendingActorName != "李四" {
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
	if detail.Task.CanStart || detail.Task.CanCancel {
		t.Fatalf("访客不应有任何动作标志: %+v", detail.Task)
	}

	// 任务负责人 bob 视角出现开始执行动作（AC-34 操作按钮按权限出现）
	resp = doJSON(t, bob, http.MethodGet, detailURL, nil)
	wantStatus(t, resp, http.StatusOK)
	if d := decodeBody[api.TaskDetail](t, resp); !d.Task.CanStart {
		t.Fatalf("任务负责人应可开始执行: %+v", d.Task)
	}

	// 不存在的任务 404
	resp = doJSON(t, alice, http.MethodGet, fmt.Sprintf("%s/99999", tasksURL), nil)
	wantStatus(t, resp, http.StatusNotFound)
	resp.Body.Close()
}

// 关键字段直接修改（AC-23、#172 裁决）：有编辑权限者修改立即生效、无修改原因，
// 动作写入任务动态（裁决 12，#183：KR 无负责人，原站内通知退场）。
func TestTaskFieldEditDirect(t *testing.T) {
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

	// alice 建项目；bob、carol 成员
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
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收"}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	// alice（项目负责人）创建任务 → 直接入池、未开始（裁决 #162；裁决 10：创建收归管理员）
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "验证现场联动异常回退", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	tasks := decodeBody[[]api.Task](t, resp)
	taskID := tasks[0].Id
	editURL := func(id int64) string { return fmt.Sprintf("%s/%d/edits", tasksURL, id) }

	// 空修改 422
	resp = doJSON(t, alice, http.MethodPost, editURL(taskID), api.EditTaskFieldsRequest{})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()

	// 裁决 10：任务负责人 carol 不再可改 403
	newEnd := openapiDate(t, "2026-10-15")
	resp = doJSON(t, carol, http.MethodPost, editURL(taskID), api.EditTaskFieldsRequest{EndDate: &newEnd})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()

	// 项目负责人 alice 改截止时间 → 立即生效、无修改原因（#172 裁决）
	resp = doJSON(t, alice, http.MethodPost, editURL(taskID), api.EditTaskFieldsRequest{EndDate: &newEnd})
	wantStatus(t, resp, http.StatusOK)
	edited := decodeBody[api.Task](t, resp)
	if edited.EndDate.Time.Format("2006-01-02") != "2026-10-15" {
		t.Fatalf("直接修改应立即生效: %+v", edited.EndDate)
	}
	if !edited.CanEditFields {
		t.Fatalf("项目负责人应保有编辑权限: %+v", edited)
	}

	// 动作写入任务动态；裁决 12（#183）：KR 无负责人，不再发字段修改站内通知
	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	detail := decodeBody[api.TaskDetail](t, resp)
	foundActivity := false
	for _, a := range detail.Activities {
		if a.Kind == api.FieldEdited && strings.Contains(a.Summary, "截止时间") {
			foundActivity = true
		}
	}
	if !foundActivity {
		t.Fatalf("字段修改应写入任务动态: %+v", detail.Activities)
	}
	resp = doJSON(t, bob, http.MethodGet, base+"/notifications", nil)
	wantStatus(t, resp, http.StatusOK)
	for _, n := range decodeBody[[]api.Notification](t, resp) {
		if n.Kind == "task_field_edited" {
			t.Fatalf("裁决 12 后不应再发字段修改通知: %+v", n)
		}
	}

	// 管理员（bob 升为管理员后）修改：立即生效
	resp = doJSON(t, alice, http.MethodPut, fmt.Sprintf("%s/projects/%d/members/%d", base, created.Id, bobUser.ID),
		api.UpdateProjectMemberRoleRequest{Role: api.Admin})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodPost, editURL(taskID), api.EditTaskFieldsRequest{OwnerId: &bobUser.ID})
	wantStatus(t, resp, http.StatusOK)
	if got := decodeBody[api.Task](t, resp); got.OwnerId != bobUser.ID {
		t.Fatalf("管理员修改应即时生效: %+v", got)
	}

	// 校验仍然生效：负责人须为项目成员（非成员被拒）；
	// carol（任务负责人）直接修改被拒 403（裁决 10 编辑收归管理员）
	stranger := int64(999999)
	resp = doJSON(t, bob, http.MethodPost, editURL(taskID), api.EditTaskFieldsRequest{OwnerId: &stranger})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodPost, editURL(taskID), api.EditTaskFieldsRequest{EndDate: &newEnd})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	_ = sp
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
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收"}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	// 裁决 #164：创建任务不再带预期交付物，交付物项建立后才出现在列表列
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "输出验收方案", OwnerId: bobUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	tasks := decodeBody[[]api.Task](t, resp)
	taskID := tasks[0].Id
	if tasks[0].DeliverableNames != nil {
		t.Fatalf("新任务不应有交付物项: %+v", tasks[0].DeliverableNames)
	}
	createDeliverable(t, bob, tasksURL, taskID, "验收方案 V1.docx")
	resp = doJSON(t, bob, http.MethodGet, tasksURL, nil)
	wantStatus(t, resp, http.StatusOK)
	for _, tk := range decodeBody[[]api.Task](t, resp) {
		if tk.Id == taskID && (tk.DeliverableNames == nil || (*tk.DeliverableNames)[0] != "验收方案 V1") {
			t.Fatalf("交付物列异常: %+v", tk.DeliverableNames)
		}
	}

	// 再补一个交付物项（一个任务多项交付物）；裁决 H1：提交完成申请前即时生效
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
	if up.UploadUrl == "" || up.File.State != api.Uploading {
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
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收"}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
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

	// AC-36：讨论通知只发任务负责人 bob 与被 @ 的 alice，携带 taskId 可直达讨论 Tab
	resp = doJSON(t, bob, http.MethodGet, base+"/notifications", nil)
	wantStatus(t, resp, http.StatusOK)
	bobNotes := []api.Notification{}
	for _, n := range decodeBody[[]api.Notification](t, resp) {
		if strings.HasPrefix(n.Kind, "discussion") {
			bobNotes = append(bobNotes, n)
		}
	}
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
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收"}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	// carol 建任务（带两个交付物项，直接入池）→ carol 开始执行
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "输出验收方案", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	tasks := decodeBody[[]api.Task](t, resp)
	taskID := tasks[0].Id
	// 裁决 H1（#141）：已入池任务加交付物项即时生效，不走审批（裁决 10：负责人／管理员）
	createDeliverable(t, carol, tasksURL, taskID, "验收方案.docx")
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/%d/deliverables", tasksURL, taskID),
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
	// 裁决 11：终审人为项目管理员集合——本项目只有项目负责人张三。
	if pending.Status != api.TaskStatusInReview || pending.CurrentStage != "待张三审批" {
		t.Fatalf("提交后应待终审: %+v", pending)
	}

	// 审核期间不可另传候选 409
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/deliverables/%d/candidate", tasksURL, taskID, d1),
		api.UploadCandidateRequest{FileName: "偷偷替换.docx"})
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()

	// AC-38（裁决 11 修订）：非项目管理员不能终审（KR 负责人 bob 亦不能）；退回意见必填
	resp = doJSON(t, carol, http.MethodGet, detailURL, nil)
	wantStatus(t, resp, http.StatusOK)
	detail = decodeBody[api.TaskDetail](t, resp)
	reviewID := detail.CompletionReviews[0].Id
	if len(detail.CompletionReviews[0].Items) != 2 {
		t.Fatalf("申请应含全部候选: %+v", detail.CompletionReviews[0].Items)
	}
	decisionURL := fmt.Sprintf("%s/%d/decision", completionURL, reviewID)
	resp = doJSON(t, bob, http.MethodPost, decisionURL, api.CompletionDecisionRequest{Decision: api.CompletionDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPost, decisionURL, api.CompletionDecisionRequest{Decision: api.CompletionDecisionRequestDecisionRejected})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()

	// AC-40（裁决 #165 修订）：退回后候选保留、任务回进行中、审核事实保留
	op := "样例覆盖不足"
	resp = doJSON(t, alice, http.MethodPost, decisionURL, api.CompletionDecisionRequest{Decision: api.CompletionDecisionRequestDecisionRejected, Opinion: &op})
	wantStatus(t, resp, http.StatusOK)
	rejected := decodeBody[api.Task](t, resp)
	if rejected.Status != api.TaskStatusInProgress {
		t.Fatalf("退回后应回进行中: %+v", rejected)
	}
	resp = doJSON(t, carol, http.MethodGet, detailURL, nil)
	wantStatus(t, resp, http.StatusOK)
	detail = decodeBody[api.TaskDetail](t, resp)
	for _, d := range detail.Deliverables {
		if d.Candidate == nil {
			t.Fatalf("退回后候选应保留: %+v", d)
		}
		if d.CanDeleteCandidate == nil || !*d.CanDeleteCandidate {
			t.Fatalf("负责人应可删除候选: %+v", d)
		}
	}
	if detail.CompletionReviews[0].State != api.CompletionReviewStateRejected || detail.CompletionReviews[0].Opinion == nil || *detail.CompletionReviews[0].Opinion != op {
		t.Fatalf("退回事实未保留: %+v", detail.CompletionReviews[0])
	}
	if detail.CompletionReviews[0].Items[0].FileId == nil {
		t.Fatalf("保留的候选应仍可下载: %+v", detail.CompletionReviews[0].Items[0])
	}

	// 裁决 #165：删除第二项候选（管理员按纠错口径同样可删）
	resp = doJSON(t, alice, http.MethodDelete, fmt.Sprintf("%s/%d/deliverables/%d/candidate", tasksURL, taskID, d2), nil)
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodGet, detailURL, nil)
	wantStatus(t, resp, http.StatusOK)
	detail = decodeBody[api.TaskDetail](t, resp)
	for _, d := range detail.Deliverables {
		if d.Id == d2 && d.Candidate != nil {
			t.Fatalf("删除后候选应消失: %+v", d)
		}
	}

	// 重传候选（仅第一项，覆盖保留的候选）并重提 → 第二次申请只带剩余候选（裁决 #165）
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
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/%d/decision", completionURL, newReviewID),
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
				{Description: "上线自动验收"},
				{Description: "现场回归通过"},
			}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1, kr2 := okr[0].KeyResults[0].Id, okr[0].KeyResults[1].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	// bob 免审建上游任务 A（带交付物）与跨 KR 下游任务 B（carol 负责）
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "采集现场数据", OwnerId: bobUser.ID, StartDate: start, EndDate: end},
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

	// 为 A 建交付物项（裁决 #164：创建任务不再带预期交付物）
	dA := createDeliverable(t, bob, tasksURL, taskA.Id, "现场数据包.zip")

	// AC-28：项目管理员为 B 选择 A 建立必要输入边（裁决 #163 不选对应交付物；
	// 裁决 10：配置输入源收归项目管理员，直接生效、边立即建立）
	inputsURL := func(id int64) string { return fmt.Sprintf("%s/%d/inputs", tasksURL, id) }
	resp = doJSON(t, alice, http.MethodPost, inputsURL(taskB.Id), api.CreateTaskInputRequest{
		Necessity: api.Required,
		SourceTaskIds: []int64{taskA.Id},
	})
	wantStructureAccepted(t, resp)
	edge := edgeOf(t, carol, base, created.Id, taskB.Id, "采集现场数据")
	if edge.Ready || edge.SourceTaskName == nil || *edge.SourceTaskName != "采集现场数据" {
		t.Fatalf("新建边应未就绪且含来源信息: %+v", edge)
	}

	// AC-07：反向再建一条反馈边（双向/循环关系保留真实连线）
	resp = doJSON(t, alice, http.MethodPost, inputsURL(taskA.Id), api.CreateTaskInputRequest{
		Necessity: api.Reference, SourceTaskIds: []int64{taskB.Id},
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

	// 自环 422；成员建边 403（裁决 10：配置输入源仅项目管理员）
	resp = doJSON(t, alice, http.MethodPost, inputsURL(taskB.Id), api.CreateTaskInputRequest{
		Necessity: api.Required, SourceTaskIds: []int64{taskB.Id},
	})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodPost, inputsURL(taskB.Id), api.CreateTaskInputRequest{
		Necessity: api.Required, SourceTaskIds: []int64{taskA.Id},
	})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()

	// AC-48：A 走完成终审后当前内容生效 → 边自动就绪、B 不再等待输入
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/update-status", tasksURL, taskA.Id),
		api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	uploadCandidate(t, bob, tasksURL, taskA.Id, dA, api.UploadCandidateRequest{FileName: "现场数据包.zip"}, "candidate-bytes")

	// 来源任务未完成时一律未就绪（裁决 #163）：已上传候选也不改变判定
	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/projects/%d/edges", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	for _, e := range decodeBody[[]api.DeliverableEdge](t, resp) {
		if e.TargetTaskId == taskB.Id && e.Ready {
			t.Fatalf("来源任务未完成不应就绪: %+v", e)
		}
	}

	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews", tasksURL, taskA.Id),
		api.SubmitCompletionRequest{Note: "数据包齐"})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskA.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	detailA := decodeBody[api.TaskDetail](t, resp)
	reviewID := detailA.CompletionReviews[0].Id
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews/%d/decision", tasksURL, taskA.Id, reviewID),
		api.CompletionDecisionRequest{Decision: api.CompletionDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskB.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	detailB := decodeBody[api.TaskDetail](t, resp)
	if len(detailB.Inputs) != 1 || !detailB.Inputs[0].Ready {
		t.Fatalf("来源任务完成后输入应自动就绪: %+v", detailB.Inputs)
	}
	// 裁决 #163＋J1：边展示来源任务全部当前文件，类型与大小由服务端派生。
	if detailB.Inputs[0].SourceCurrentFiles == nil || len(*detailB.Inputs[0].SourceCurrentFiles) != 1 {
		t.Fatalf("边上应带来源任务全部当前文件: %+v", detailB.Inputs[0])
	}
	scf := (*detailB.Inputs[0].SourceCurrentFiles)[0]
	if scf.FileTypeLabel == "" || scf.FileSize <= 0 || scf.FileName == "" {
		t.Fatalf("来源当前文件应带类型文案与大小: %+v", scf)
	}
	if detailB.Task.Status != api.TaskStatusNotStarted {
		t.Fatalf("输入就绪后应回未开始显示: %+v", detailB.Task.Status)
	}
	if len(detailB.Outputs) != 1 || detailB.Outputs[0].Necessity != api.Reference {
		t.Fatalf("B 的下游参考边异常: %+v", detailB.Outputs)
	}
	// AC-50：协作关系摘要按直接上游／下游分组派生，条目自带对方任务的展示事实（#173：按必要性）
	if len(detailB.Upstream) != 1 {
		t.Fatalf("B 的直接上游分组异常: %+v", detailB.Upstream)
	}
	up := detailB.Upstream[0]
	if up.TaskId != taskA.Id || up.TaskName != "采集现场数据" || up.Necessity != api.Required ||
		!up.Ready || up.OwnerName != bobUser.DisplayName || up.KrDescription == "" || up.TaskStatusLabel == "" {
		t.Fatalf("直接上游摘要事实异常: %+v", up)
	}
	if len(detailB.Downstream) != 1 || detailB.Downstream[0].TaskId != taskA.Id ||
		detailB.Downstream[0].Necessity != api.Reference {
		t.Fatalf("B 的直接下游分组异常: %+v", detailB.Downstream)
	}
	// CR PRD §8.1（#173 修订）：受影响 O／KR 只沿下游必要边推导——
	// A 的必要边指向 B，所以 A 的详情里有 B 所属 KR；B 只有一条指向 A 的参考边，不产生受影响目标。
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

	// 解除边：目标任务 A 已完成（终态）不允许修改 → 409
	resp = doJSON(t, alice, http.MethodDelete, fmt.Sprintf("%s/projects/%d/edges/%d", base, created.Id, detailB.Outputs[0].Id), nil)
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()
	// 解除输入源同样是关键字段变更（#172 直接生效；裁决 10：仅项目管理员）
	resp = doJSON(t, alice, http.MethodDelete, fmt.Sprintf("%s/projects/%d/edges/%d", base, created.Id, detailB.Inputs[0].Id), nil)
	wantStructureAccepted(t, resp)
	if edges := projectEdges(t, carol, base, created.Id); len(edges) != 1 {
		t.Fatalf("解除后边数量异常: %+v", edges)
	}
}

// 成果审核或签与退回（#11，AC-14/24/37；MW-07／MW-18）：配置或签组→提交进入成果审核→任一人通过进待终审
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
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收"}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "输出验收方案", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	tasks := decodeBody[[]api.Task](t, resp)
	taskID := tasks[0].Id

	// carol 配置或签组（dave、erin）；访客会被拒
	reviewersURL := fmt.Sprintf("%s/%d/reviewers", tasksURL, taskID)
	resp = doJSON(t, carol, http.MethodPut, reviewersURL, api.SetReviewersRequest{UserIds: []int64{daveUser.ID, erinUser.ID}})
	wantStatus(t, resp, http.StatusOK)
	if got := decodeBody[[]api.ReviewerInfo](t, resp); len(got) != 2 {
		t.Fatalf("或签组配置异常: %+v", got)
	}

	// 开始执行、建项、上传候选、提交 → 进入成果审核
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/update-status", tasksURL, taskID),
		api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	dA := createDeliverable(t, carol, tasksURL, taskID, "验收方案.docx")
	uploadCandidate(t, carol, tasksURL, taskID, dA, api.UploadCandidateRequest{FileName: "验收方案V1.docx"}, "candidate-bytes")
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews", tasksURL, taskID),
		api.SubmitCompletionRequest{Note: "请或签审核"})
	wantStatus(t, resp, http.StatusOK)
	submitted := decodeBody[api.Task](t, resp)
	// 或签组为 dave（赵六）、erin（钱七）：多人取首位加人数。
	if submitted.Status != api.TaskStatusInReview || submitted.CurrentStage != "待赵六等2人审批" {
		t.Fatalf("提交后应进入成果审核: %+v", submitted)
	}

	// 审核中不可再调整或签组 409
	resp = doJSON(t, carol, http.MethodPut, reviewersURL, api.SetReviewersRequest{UserIds: []int64{daveUser.ID}})
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()

	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	detail := decodeBody[api.TaskDetail](t, resp)
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

	// AC-24（裁决 #165 修订）：erin 退回（意见必填）→ 候选保留、任务回进行中、意见保留
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
	if detail.Deliverables[0].Candidate == nil {
		t.Fatalf("退回后候选应保留（裁决 #165）: %+v", detail.Deliverables[0])
	}

	// 重新提交完整流程：重传候选（覆盖保留的候选）→提交→dave 通过（或签任一人）→ 待 KR 终审、erin 待办关闭
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
	if afterOr.Status != api.TaskStatusInReview {
		t.Fatalf("或签通过后应待 KR 终审: %+v", afterOr)
	}
	// AC-14：其余待办自动关闭——erin 再处理返回状态冲突
	resp = doJSON(t, erin, http.MethodPost, newDecisionURL, api.CompletionDecisionRequest{Decision: api.CompletionDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()

	// 或签通过留痕；终审（项目管理员或签，裁决 11）闭环——KR 负责人 bob 不能终审
	resp = doJSON(t, erin, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	detail = decodeBody[api.TaskDetail](t, resp)
	cr := detail.CompletionReviews[0]
	if cr.IntermediateByName == nil || *cr.IntermediateByName != "赵六" || cr.IntermediateOpinion == nil || *cr.IntermediateOpinion != okOp {
		t.Fatalf("或签处理事实未留痕: %+v", cr)
	}
	resp = doJSON(t, bob, http.MethodPost, newDecisionURL, api.CompletionDecisionRequest{Decision: api.CompletionDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPost, newDecisionURL, api.CompletionDecisionRequest{Decision: api.CompletionDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusOK)
	if got := decodeBody[api.Task](t, resp); got.Status != api.TaskStatusCompleted {
		t.Fatalf("终审后应完成: %+v", got)
	}
}

// 替指定成员创建上游任务（#178 裁决：输入请求机制退场）：新任务直接入池并通知
// 新任务负责人，自动建立「新上游任务 → 当前任务」的必要输入边，就绪按来源任务已完成判定。
func TestCreateUpstreamTask(t *testing.T) {
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
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收"}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	// alice 建任务（直接入池）→ 替 dave 创建上游任务（裁决 10：两步都收归管理员）
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "回归验证分析", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	taskID := decodeBody[[]api.Task](t, resp)[0].Id
	upstreamURL := fmt.Sprintf("%s/%d/upstream-tasks", tasksURL, taskID)

	// 校验仍然生效：所属 KR 必须存在、负责人须为非只读成员
	resp = doJSON(t, alice, http.MethodPost, upstreamURL, api.CreateUpstreamTaskRequest{
		KeyResultId: 999999, Name: "无效 KR", OwnerId: daveUser.ID, StartDate: start, EndDate: end,
	})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPost, upstreamURL, api.CreateUpstreamTaskRequest{
		KeyResultId: kr1, Name: "无效负责人", OwnerId: 999999, StartDate: start, EndDate: end,
	})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()

	// 正常创建：新任务直接入池、边立即建立
	upstreamEnd := openapiDate(t, "2026-09-20")
	resp = doJSON(t, alice, http.MethodPost, upstreamURL, api.CreateUpstreamTaskRequest{
		KeyResultId: kr1, Name: "补充现场口径说明", OwnerId: daveUser.ID,
		StartDate: start, EndDate: upstreamEnd,
	})
	wantStatus(t, resp, http.StatusOK)
	currentTask := decodeBody[api.Task](t, resp)
	// 必要输入未到：当前任务显示等待输入
	if currentTask.Status != api.TaskStatusWaitingInput {
		t.Fatalf("必要输入未到应显示等待输入: %+v", currentTask.Status)
	}

	// 边事实：来源为新任务、必要、未就绪；期望时间＝上游任务截止（#174）
	edge := edgeOf(t, carol, base, created.Id, taskID, "补充现场口径说明")
	if edge.Ready || edge.Necessity != api.Required {
		t.Fatalf("新建上游边应为未就绪的必要输入: %+v", edge)
	}
	if edge.SourceTaskName == nil || *edge.SourceTaskName != "补充现场口径说明" {
		t.Fatalf("边应指向新上游任务: %+v", edge)
	}
	if edge.ExpectedDate == nil || edge.ExpectedDate.Time.Format("2006-01-02") != "2026-09-20" {
		t.Fatalf("期望时间应取上游任务截止日期: %+v", edge.ExpectedDate)
	}

	// 新任务事实：直接入池、负责人 dave、未开始
	resp = doJSON(t, carol, http.MethodGet, tasksURL, nil)
	wantStatus(t, resp, http.StatusOK)
	var upstream *api.Task
	for _, tk := range decodeBody[[]api.Task](t, resp) {
		if tk.Name == "补充现场口径说明" {
			v := tk
			upstream = &v
		}
	}
	if upstream == nil || upstream.OwnerId != daveUser.ID || upstream.Status != api.TaskStatusNotStarted {
		t.Fatalf("新上游任务应直接入池且负责人为指定成员: %+v", upstream)
	}

	// dave 收到「被指定为负责人」通知（无认领确认环节）
	resp = doJSON(t, dave, http.MethodGet, base+"/notifications", nil)
	wantStatus(t, resp, http.StatusOK)
	foundAssigned := false
	for _, n := range decodeBody[[]api.Notification](t, resp) {
		if n.Kind == "upstream_task_assigned" && n.TaskId != nil && *n.TaskId == upstream.Id &&
			strings.Contains(n.Content, "张三") && strings.Contains(n.Content, "补充现场口径说明") {
			foundAssigned = true
		}
	}
	if !foundAssigned {
		t.Fatalf("新任务负责人应收到通知")
	}

	// 裁决 12（#183）：KR 无负责人，替他人创建上游任务不再发入池通知（负责人通知保留）。
	resp = doJSON(t, bob, http.MethodGet, base+"/notifications", nil)
	wantStatus(t, resp, http.StatusOK)
	for _, n := range decodeBody[[]api.Notification](t, resp) {
		if n.Kind == "task_pool_entered" {
			t.Fatalf("裁决 12 后不应再发入池通知: %+v", n)
		}
	}
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
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收"}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	kr1 := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	edgesURL := fmt.Sprintf("%s/projects/%d/edges", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	// 管理员 alice 建两条上游任务与两条下游任务（C 用于多来源任务，D 用于多对接人）
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "采集现场数据", OwnerId: bobUser.ID, StartDate: start, EndDate: end},
			{KeyResultId: kr1, Name: "整理历史台账", OwnerId: bobUser.ID, StartDate: start, EndDate: end},
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
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/%d/inputs", tasksURL, taskC),
		api.CreateTaskInputRequest{
			Necessity: api.Required,
			SourceTaskIds: []int64{taskA, taskB2},
		})
	wantStructureAccepted(t, resp)
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

	// 同一次选择不可重复、不能选自身、不能空选
	for _, bad := range []api.CreateTaskInputRequest{
		{Necessity: api.Required, SourceTaskIds: []int64{taskA, taskA}},
		{Necessity: api.Required, SourceTaskIds: []int64{taskA, taskC}},
		{Necessity: api.Required, SourceTaskIds: []int64{}},
	} {
		resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/%d/inputs", tasksURL, taskC), bad)
		wantStatus(t, resp, http.StatusUnprocessableEntity)
		resp.Body.Close()
	}

	// 任一必要输入未就绪 ⇒ C 等待输入
	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskC), nil)
	wantStatus(t, resp, http.StatusOK)
	if detail := decodeBody[api.TaskDetail](t, resp); detail.Task.Status != api.TaskStatusWaitingInput || len(detail.Inputs) != 2 {
		t.Fatalf("多来源未就绪应等待输入: %+v", detail.Task.Status)
	}

	// #178 裁决：成员来源随输入请求机制退场——为 D 替 dave、erin 各创建一个上游任务，
	// 各自建立必要输入边并独立参与就绪判定。
	upstreamURL := fmt.Sprintf("%s/%d/upstream-tasks", tasksURL, taskD)
	for i, ownerID := range []int64{daveUser.ID, erinUser.ID} {
		resp = doJSON(t, alice, http.MethodPost, upstreamURL, api.CreateUpstreamTaskRequest{
			KeyResultId: kr1, Name: fmt.Sprintf("本方口径说明整理 %d", i+1), OwnerId: ownerID,
			StartDate: start, EndDate: end,
		})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	memberEdges := []api.DeliverableEdge{}
	for _, e := range projectEdges(t, carol, base, created.Id) {
		if e.TargetTaskId == taskD {
			memberEdges = append(memberEdges, e)
		}
	}
	if len(memberEdges) != 2 {
		t.Fatalf("两个上游任务应分别建边: %+v", memberEdges)
	}
	for _, e := range memberEdges {
		if e.Ready || e.Necessity != api.Required {
			t.Fatalf("新建上游边应为未就绪的必要输入: %+v", e)
		}
	}
	// 各上游负责人收到「被指定为负责人」通知
	for _, c := range []*http.Client{dave, erin} {
		resp = doJSON(t, c, http.MethodGet, base+"/notifications", nil)
		wantStatus(t, resp, http.StatusOK)
		found := false
		for _, n := range decodeBody[[]api.Notification](t, resp) {
			if n.Kind == "upstream_task_assigned" {
				found = true
			}
		}
		if !found {
			t.Fatalf("上游任务负责人应收到通知")
		}
	}
	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskD), nil)
	wantStatus(t, resp, http.StatusOK)
	if detail := decodeBody[api.TaskDetail](t, resp); detail.Task.Status != api.TaskStatusWaitingInput || len(detail.Inputs) != 2 {
		t.Fatalf("上游未完成时应等待输入: %+v", detail.Task.Status)
	}

	// 裁决 15（#185）：参考边退出风险识别——KR 未就绪摘要只计必要边，提醒计数含参考边。
	// 现状 4 条必要未就绪边；给 B 加一条参考输入后，notReadyCount 不变、reminderCount +1。
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/%d/inputs", tasksURL, taskB2),
		api.CreateTaskInputRequest{Necessity: api.Reference, SourceTaskIds: []int64{taskA}})
	wantStructureAccepted(t, resp)
	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	krView := decodeBody[[]api.Objective](t, resp)[0].KeyResults[0]
	if krView.NotReadyCount == nil || *krView.NotReadyCount != 4 {
		t.Fatalf("未就绪摘要应只计必要边（4 条）: %+v", krView.NotReadyCount)
	}
	if krView.ReminderCount == nil || *krView.ReminderCount != 5 {
		t.Fatalf("提醒计数应含参考边（5 条）: %+v", krView.ReminderCount)
	}
	_ = sp
	_ = edgesURL
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
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收"}}},
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
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "回归验证分析", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
			{KeyResultId: kr1, Name: "现场数据采集", OwnerId: bobUser.ID, StartDate: start, EndDate: end},
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

	// 下游挂一条来自上游任务的必要输入：上游未完成 ⇒ 上游未就绪卡点，待行动人为上游负责人 bob。
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/%d/inputs", tasksURL, downstream.Id),
		api.CreateTaskInputRequest{
			Necessity: api.Required,
			SourceTaskIds: []int64{upstream.Id},
		})
	wantStructureAccepted(t, resp)
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
	// #167：卡点条目按「编号＋标题＋负责人」展示上游任务，字段由 API 派生。
	if unready.SourceTaskCode == nil || *unready.SourceTaskCode == "" ||
		unready.SourceTaskName == nil || *unready.SourceTaskName != "现场数据采集" ||
		unready.SourceOwnerName == nil || *unready.SourceOwnerName != bobUser.DisplayName {
		t.Fatalf("卡点应带上游编号/标题/负责人: %+v", unready)
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
		// 裁决 14（#184）：任务级 riskLevel/riskNote 派生字段——超期任务为高风险，原因行同源。
		if tk.RiskLevel != api.HighRisk || tk.RiskLevelLabel != "高风险" {
			t.Fatalf("超期任务应派生任务级高风险: %+v / %q", tk.RiskLevel, tk.RiskLevelLabel)
		}
		if tk.RiskNote == nil || !strings.Contains(*tk.RiskNote, "超期") {
			t.Fatalf("任务风险原因行应来自抬高等级的事实: %+v", tk.RiskNote)
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
	// 只看提醒类通知。
	notes := []api.Notification{}
	for _, n := range decodeBody[[]api.Notification](t, resp) {
		if n.Kind == "blocker_remind" {
			notes = append(notes, n)
		}
	}
	if len(notes) != 1 || *notes[0].TaskId != downstream.Id {
		t.Fatalf("提醒通知异常: %+v", notes)
	}

	// 不存在的键（触发条件已消失）按 404 处理，没有手动解除接口。
	resp = doJSON(t, carol, http.MethodPost, remindURL, api.RemindRequest{TargetKey: "task_overdue:999999"})
	wantStatus(t, resp, http.StatusNotFound)
	resp.Body.Close()

	// 自动解除：上游任务走完终审（已完成）后，上游未就绪卡点消失，下游超期卡点仍在。
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/update-status", tasksURL, upstream.Id),
		api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	upstreamDeliverable := createDeliverable(t, bob, tasksURL, upstream.Id, "现场数据包.zip")
	uploadCandidate(t, bob, tasksURL, upstream.Id, upstreamDeliverable, api.UploadCandidateRequest{FileName: "现场数据包.zip"}, "candidate-bytes")
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews", tasksURL, upstream.Id),
		api.SubmitCompletionRequest{Note: "数据包齐"})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, upstream.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	reviewID := decodeBody[api.TaskDetail](t, resp).CompletionReviews[0].Id
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews/%d/decision", tasksURL, upstream.Id, reviewID),
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
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收"}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	myWorkURL := fmt.Sprintf("%s/projects/%d/my-work", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	// alice 创建任务（直接入池）→ carol 待我处理出现任务卡；bob 暂无审批件
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "输出验收方案", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	tasks := decodeBody[[]api.Task](t, resp)
	taskID := tasks[0].Id

	resp = doJSON(t, bob, http.MethodGet, myWorkURL, nil)
	wantStatus(t, resp, http.StatusOK)
	bobWork := decodeBody[api.MyWork](t, resp)
	if len(bobWork.Approvals) != 0 {
		t.Fatalf("此时不应有待我审批: %+v", bobWork.Approvals)
	}
	// 身份卡（#69；裁决 12：无 KR 负责人职责）：bob 当前不承担任何行动职责。
	if id := bobWork.Identity; id.UserId != bobUser.ID || id.DisplayName != "李四" ||
		id.RoleLabel != "项目成员" || id.ResponsibilitiesLabel != "当前未承担行动职责" {
		t.Fatalf("身份卡异常: %+v", bobWork.Identity)
	}
	resp = doJSON(t, carol, http.MethodGet, myWorkURL, nil)
	wantStatus(t, resp, http.StatusOK)
	carolWork := decodeBody[api.MyWork](t, resp)
	if len(carolWork.Pending) != 1 || carolWork.Pending[0].Kind != "task" {
		t.Fatalf("入池后任务应在待我处理: %+v", carolWork.Pending)
	}

	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/update-status", tasksURL, taskID),
		api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	dA := createDeliverable(t, carol, tasksURL, taskID, "验收方案.docx")
	uploadCandidate(t, carol, tasksURL, taskID, dA, api.UploadCandidateRequest{FileName: "验收方案.docx"}, "candidate-bytes")
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews", tasksURL, taskID),
		api.SubmitCompletionRequest{Note: "请终审"})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	// 裁决 11：终审归入项目管理员（alice）的待我审批；普通成员 bob 不持有终审件。
	resp = doJSON(t, alice, http.MethodGet, myWorkURL, nil)
	wantStatus(t, resp, http.StatusOK)
	aliceWork := decodeBody[api.MyWork](t, resp)
	var hasFinal bool
	for _, it := range aliceWork.Approvals {
		if it.Kind == "final_review" {
			hasFinal = true
		}
	}
	if !hasFinal {
		t.Fatalf("终审应归入项目管理员的待我审批: %+v", aliceWork.Approvals)
	}
	resp = doJSON(t, bob, http.MethodGet, myWorkURL, nil)
	wantStatus(t, resp, http.StatusOK)
	bobWork = decodeBody[api.MyWork](t, resp)
	for _, it := range bobWork.Approvals {
		if it.Kind == "final_review" {
			t.Fatalf("普通成员不应持有终审件（裁决 11）: %+v", it)
		}
	}
	resp = doJSON(t, carol, http.MethodGet, myWorkURL, nil)
	wantStatus(t, resp, http.StatusOK)
	carolWork = decodeBody[api.MyWork](t, resp)
	if len(carolWork.Pending) != 0 {
		t.Fatalf("完成审批中的任务不应在待我处理: %+v", carolWork.Pending)
	}
	var hasWaitingCompletion bool
	for _, it := range carolWork.Waiting {
		// AC-04：等待环节显示当前审批人姓名（裁决 11：终审人为项目管理员 alice／张三）。
		if it.Kind == "waiting_completion" && it.Stage != nil && *it.Stage == "待张三审批" {
			hasWaitingCompletion = true
		}
	}
	if !hasWaitingCompletion {
		t.Fatalf("完成申请应在等待他人并显示当前环节: %+v", carolWork.Waiting)
	}

	// 卡点：alice 名下一个已超期任务派生任务超期卡点 → alice 与我相关的卡点（AC-11）
	pastStart, pastEnd := openapiDate(t, "2020-01-01"), openapiDate(t, "2020-02-01")
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
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
	aliceWork = decodeBody[api.MyWork](t, resp)
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
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收"}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-10")

	// 三个任务：A→B→C 硬前置链；再加 C→B 硬前置构成循环；B→A 反馈边不影响。
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
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
	// #173 裁决：互锁与关键路径沿「必要」边分析，参考边不参与。
	mkEdge := func(srcName string, dst int64, necessity api.Necessity) api.DeliverableEdge {
		t.Helper()
		resp := doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/%d/inputs", tasksURL, dst), api.CreateTaskInputRequest{
			Necessity: necessity, SourceTaskIds: []int64{byName[srcName]},
		})
		wantStructureAccepted(t, resp)
		// 输入源标识由来源任务派生（#112）：按来源任务名定位这条边。
		return edgeOf(t, bob, base, created.Id, dst, srcName)
	}
	eAB := mkEdge("任务A", byName["任务B"], api.Required)
	eBC := mkEdge("任务B", byName["任务C"], api.Required)
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

	// 加 C→B 必要边构成循环，再加 B→A 参考边
	mkEdge("任务C", byName["任务B"], api.Required)
	mkEdge("任务B", byName["任务A"], api.Reference)

	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/projects/%d/edges", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	edges = decodeBody[[]api.DeliverableEdge](t, resp)
	var interlocked, referenceOnPath, abOnPath int
	for _, e := range edges {
		if e.InterlockRisk != nil && *e.InterlockRisk {
			interlocked++
			if e.OnCriticalPath != nil && *e.OnCriticalPath {
				t.Fatalf("互锁边不应进入关键路径: %+v", e)
			}
		}
		if e.Necessity == api.Reference && (e.OnCriticalPath != nil || e.InterlockRisk != nil) {
			referenceOnPath++
		}
		if e.Id == eAB.Id && e.OnCriticalPath != nil && *e.OnCriticalPath {
			abOnPath++
		}
	}
	if interlocked != 2 {
		t.Fatalf("B↔C 两条必要边应标互锁: %d", interlocked)
	}
	if referenceOnPath != 0 {
		t.Fatalf("参考边不应派生互锁与关键路径字段")
	}
	if abOnPath != 1 {
		t.Fatalf("循环外的 A→B 应保留在关键路径: %d", abOnPath)
	}
}

// 成果与归档、轻量成果包（#24，AC-17/18）：归档视角展示当前成果与审批记录数；
// 勾选当前成果生成目录与来源清单；整包下载解析当前内容（需要 MinIO，不可达时跳过下载断言）。
func TestArtifacts(t *testing.T) {
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
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收"}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	// 走完整链路生成一份当前内容：建任务→开始→候选→终审通过
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "输出验收方案", OwnerId: bobUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	tasks := decodeBody[[]api.Task](t, resp)
	taskID := tasks[0].Id
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/update-status", tasksURL, taskID),
		api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	dA := createDeliverable(t, bob, tasksURL, taskID, "验收方案.docx")
	uploadCandidate(t, bob, tasksURL, taskID, dA, api.UploadCandidateRequest{FileName: "验收方案V1.docx"}, "acceptance-doc-bytes")

	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews", tasksURL, taskID),
		api.SubmitCompletionRequest{Note: "请终审"})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	detail := decodeBody[api.TaskDetail](t, resp)
	reviewID := detail.CompletionReviews[0].Id
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews/%d/decision", tasksURL, taskID, reviewID),
		api.CompletionDecisionRequest{Decision: api.CompletionDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	// 下游任务以这份成果为必要输入，边上绑定交付物项：归档列表的「来源关系边」列由此而来。
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
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
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/%d/inputs", tasksURL, downstreamID),
		api.CreateTaskInputRequest{
			Necessity: api.Required,
			SourceTaskIds: []int64{taskID},
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
	// 裁决 12（#183）：KR 无负责人，组头只剩交付物数量。
	if akr.DeliverableCount != 1 {
		t.Fatalf("KR 分组头异常: 交付物数=%d", akr.DeliverableCount)
	}
	// #171：未配置接收方不显示口径文字（空串），不再回「未配置」。
	if at.OwnerName != "李四" || at.ReceiverLabel != "" {
		t.Fatalf("归档任务负责人／接收方异常: %q / %q", at.OwnerName, at.ReceiverLabel)
	}
	// 裁决 G1（#140）：文件状态两档——所属任务已完成→已发布。
	if at.FileState == nil || string(*at.FileState) != "published" ||
		at.FileStateLabel == nil || *at.FileStateLabel != "已发布" {
		t.Fatalf("归档文件状态派生异常: %+v %+v", at.FileState, at.FileStateLabel)
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
		e.NecessityLabel != "必要" || e.TargetTaskName != "按方案执行验收" {
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

	// 删除任务文件：不进审批，直接生效（外部材料同理）。
	resp = doJSON(t, bob, http.MethodDelete, fmt.Sprintf("%s/%d/files/%d", tasksURL, downstreamID, externalFile.Id), nil)
	wantStatus(t, resp, http.StatusNoContent)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, downstreamID), nil)
	wantStatus(t, resp, http.StatusOK)
	if left := *decodeBody[api.TaskDetail](t, resp).Files; len(left) != 1 || left[0].Id != processFile.Id {
		t.Fatalf("删除后应只剩过程文件: %+v", left)
	}

	// 裁决 G1（#140）：被删除的文件不出现在成果归档页。
	resp = doJSON(t, alice, http.MethodGet, fmt.Sprintf("%s/projects/%d/artifacts", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	for _, o := range decodeBody[[]api.ArtifactObjective](t, resp) {
		for _, k := range o.Krs {
			for _, task := range k.Tasks {
				if task.Files == nil {
					continue
				}
				for _, f := range *task.Files {
					if f.Id == externalFile.Id {
						t.Fatalf("已删除文件不应出现在归档页: %+v", f)
					}
				}
			}
		}
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
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收"}}},
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
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "输出验收方案", OwnerId: bobUser.ID, StartDate: start, EndDate: soon},
			{KeyResultId: kr1, Name: "临近截止任务", OwnerId: bobUser.ID, StartDate: start, EndDate: soon},
			{KeyResultId: kr1, Name: "上游未完成任务", OwnerId: bobUser.ID, StartDate: start, EndDate: far},
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
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/%d/inputs", tasksURL, taskB.Id),
		api.CreateTaskInputRequest{Necessity: api.Required, SourceTaskIds: []int64{taskC.Id}})
	wantStructureAccepted(t, resp)
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/update-status", tasksURL, taskA.Id),
		api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	dA := createDeliverable(t, bob, tasksURL, taskA.Id, "验收方案.docx")
	uploadCandidate(t, bob, tasksURL, taskA.Id, dA, api.UploadCandidateRequest{FileName: "验收方案V1.docx"}, "candidate-bytes")
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews", tasksURL, taskA.Id),
		api.SubmitCompletionRequest{Note: "请终审"})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskA.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	detail := decodeBody[api.TaskDetail](t, resp)
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews/%d/decision", tasksURL, taskA.Id, detail.CompletionReviews[0].Id),
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

// 表格导入（#27，AC-02；裁决 #162）：结构化导入生成 O/KR 与任务并直接入池（整批事务）；
// 按 KR 批量提交；KR 负责人批量通过或退回。
func TestImportAndPool(t *testing.T) {
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

	// AC-02：管理员导入 1 O × 2 KR × 3 任务（裁决 #162：直接入池）
	imp := api.ImportRequest{SourceFileName: sp("2026Q3 目标拆解.xlsx"), Items: []api.ImportItem{{
		Title: sp("提升交付质量"),
		KeyResults: &[]api.ImportKrItem{
			{Description: "上线自动验收", Tasks: &[]api.ImportTaskItem{
				{Name: "导入任务一", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
				{Name: "导入任务二", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
			}},
			{Description: "现场回归通过", Tasks: &[]api.ImportTaskItem{
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
		if task.Status != api.TaskStatusNotStarted {
			t.Fatalf("导入任务应直接入池为未开始: %+v", task)
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
	// 管理员导入两条任务（直接入池），带预期交付物
	resp = doJSON(t, alice, http.MethodPost, importTasksURL, api.ImportTasksRequest{
		SourceFileName: sp("任务批量导入.xlsx"),
		Items: []api.ImportTaskGroup{{KeyResultId: kr1ID, Tasks: []api.ImportTaskItem{
			{Name: "导入任务四", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
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
	if !ok || four.Status != api.TaskStatusNotStarted || four.KeyResultId != kr1ID {
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

	// 裁决 12（#183）：KR 无负责人，导入不再发入池通知——入池事实只留任务动态。
	resp = doJSON(t, bob, http.MethodGet, base+"/notifications", nil)
	wantStatus(t, resp, http.StatusOK)
	for _, n := range decodeBody[[]api.Notification](t, resp) {
		if n.Kind == "task_pool_entered" {
			t.Fatalf("裁决 12 后导入不应发入池通知: %+v", n)
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
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收"}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	// carol 创建任务（直接入池，裁决 #162）
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
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
	if ft := flagsOf(carol); !ft.CanStart {
		t.Fatalf("任务负责人应可开始执行: %+v", ft)
	}
	if ft := flagsOf(bob); ft.CanStart {
		t.Fatalf("非负责人（KR 负责人）不应可开始执行: %+v", ft)
	}
	if ft := flagsOf(dave); ft.CanStart || ft.CanEditFields {
		t.Fatalf("访客不应有业务动作标志: %+v", ft)
	}

	// 访客：不能建任务/建 OKR，但可讨论、可查看下载（§3.4）
	resp = doJSON(t, dave, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
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
	if w := getWork(carol); len(w.Pending) != 1 {
		t.Fatalf("任务负责人的待我处理异常: %+v", w.Pending)
	}
	if w := getWork(bob); len(w.Pending)+len(w.Approvals)+len(w.Waiting) != 0 {
		t.Fatalf("KR 负责人此时不应有行动事项: %+v", w)
	}
	if w := getWork(dave); len(w.Pending)+len(w.Approvals)+len(w.Waiting)+len(w.Blockers) != 0 {
		t.Fatalf("访客不应有行动事项: %+v", w)
	}

	// AC-22（#178 修订）：外部传递不产生外部账号——替他人创建上游任务时，
	// 新任务负责人必须是项目内非访客成员。
	start2, end2 := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/%d/upstream-tasks", tasksURL, taskID),
		api.CreateUpstreamTaskRequest{KeyResultId: kr1, Name: "外部材料代录",
			OwnerId: daveUser.ID, StartDate: start2, EndDate: end2})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/%d/upstream-tasks", tasksURL, taskID),
		api.CreateUpstreamTaskRequest{KeyResultId: kr1, Name: "外部材料由协调人李四收集后代录",
			OwnerId: bobUser.ID, StartDate: start2, EndDate: end2})
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
	if ra := resp.Header.Get("Retry-After"); ra == "" {
		t.Fatal("429 应带 Retry-After 头")
	}
	e := decodeBody[api.RateLimitedError](t, resp)
	if e.Code != "rate_limited" {
		t.Fatalf("code = %q, want rate_limited", e.Code)
	}
	// #209：剩余等待秒数在 (0, 锁定窗口] 内，文案含秒数。
	if e.RetryAfterSeconds <= 0 || e.RetryAfterSeconds > int(domain.LoginLockWindow/time.Second) {
		t.Fatalf("retryAfterSeconds = %d 超出范围", e.RetryAfterSeconds)
	}
	if !strings.Contains(e.Message, fmt.Sprintf("%d 秒", e.RetryAfterSeconds)) {
		t.Fatalf("文案应含剩余秒数: %q", e.Message)
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
			{Title: sp("联调收敛"), KeyResults: &[]api.CreateKeyResultInput{{Description: "打通端到端"}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	kr1 := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives[0].KeyResults[0].Id

	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	// 周期取已经过去的窗口：创建即入池进入未开始（裁决 #162），立即成为超期卡点，
	// 「卡点出现」由创建这次写操作 diff 出来。
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

	// alice 建任务（裁决 10）→ 入池留痕（裁决 #162）
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "端到端联调", OwnerId: bobUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	taskID := decodeBody[[]api.Task](t, resp)[0].Id

	acts := activitiesOf(taskID)
	entered := has(acts, api.PoolEntered)
	if entered == nil || entered.ActorName == nil || *entered.ActorName != "张三" {
		t.Fatalf("任务入池应留痕并带行动人: %+v", acts)
	}
	if entered.KindLabel != "任务入池" {
		t.Fatalf("动态类型中文名异常: %+v", entered)
	}

	// 创建即进入未开始，截止时间已过 ⇒ 派生出任务超期卡点，diff 补记「卡点出现」
	opened := has(acts, api.BlockerOpened)
	if opened == nil {
		t.Fatalf("入池后应补记卡点出现: %+v", kinds(acts))
	}
	if opened.ActorName != nil {
		t.Fatalf("系统派生事件不应带行动人: %+v", opened)
	}
	if opened.Summary != "卡点出现：任务超期 · 缺 按期完成任务" {
		t.Fatalf("卡点动态文案异常: %+v", opened.Summary)
	}

	// 直接修改把截止时间挪到未来（#172；裁决 10：管理员）→ 超期条件消失，diff 补记「卡点解除」
	future := openapiDate(t, "2026-12-31")
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/%d/edits", tasksURL, taskID),
		api.EditTaskFieldsRequest{EndDate: &future})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	acts = activitiesOf(taskID)
	if has(acts, api.FieldEdited) == nil {
		t.Fatalf("字段修改生效应留痕: %+v", kinds(acts))
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
			{Title: sp("交付到位"), KeyResults: &[]api.CreateKeyResultInput{{Description: "成果按时移交"}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	// carol 建两个任务并入池通过、开始执行
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "指定接收方的任务", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
			{KeyResultId: kr1, Name: "全员接收的任务", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	tasks := decodeBody[[]api.Task](t, resp)
	taskA, taskB := tasks[0].Id, tasks[1].Id
	for _, id := range []int64{taskA, taskB} {
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

	// 任务 A 指定 dave 为接收方；任务 B 取「项目全体成员」。
	// 接收方是关键字段（#172 直接生效；裁决 10：仅项目管理员）。
	resp = doJSON(t, alice, http.MethodPut, receiversA, api.SetReceiversRequest{Scope: api.ReceiverScopeMembers, UserIds: &[]int64{daveUser.ID}})
	configured := wantStructureAccepted(t, resp)
	if configured.ReceiverScope != api.ReceiverScopeMembers || configured.Receivers == nil || len(*configured.Receivers) != 1 {
		t.Fatalf("接收方配置未即时生效: %+v", configured)
	}
	resp = doJSON(t, alice, http.MethodPut, fmt.Sprintf("%s/%d/receivers", tasksURL, taskB),
		api.SetReceiversRequest{Scope: api.ReceiverScopeAll})
	wantStructureAccepted(t, resp)

	// 终审通过前没有待接收项
	myWorkURL := fmt.Sprintf("%s/projects/%d/my-work", base, created.Id)
	resp = doJSON(t, dave, http.MethodGet, myWorkURL, nil)
	wantStatus(t, resp, http.StatusOK)
	if work := decodeBody[api.MyWork](t, resp); len(work.Receipts) != 0 {
		t.Fatalf("终审通过前不应有待接收项: %+v", work.Receipts)
	}

	// 走完完成审核：建项 → 上传候选 → 提交 → bob 终审通过
	approve := func(taskID int64, fileName string) {
		t.Helper()
		detailURL := fmt.Sprintf("%s/%d", tasksURL, taskID)
		did := createDeliverable(t, carol, tasksURL, taskID, fileName)
		uploadCandidate(t, carol, tasksURL, taskID, did, api.UploadCandidateRequest{FileName: fileName}, "candidate-bytes")
		r := doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews", tasksURL, taskID),
			api.SubmitCompletionRequest{Note: "请终审"})
		wantStatus(t, r, http.StatusOK)
		r.Body.Close()
		r = doJSON(t, carol, http.MethodGet, detailURL, nil)
		wantStatus(t, r, http.StatusOK)
		d := decodeBody[api.TaskDetail](t, r)
		r = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews/%d/decision", tasksURL, taskID, d.CompletionReviews[0].Id),
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
			{Title: sp("按时推进"), KeyResults: &[]api.CreateKeyResultInput{{Description: "变更及时审批"}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	// carol 的任务提交完成申请进入 KR 终审（裁决 10 后关闭申请退场，等待事项用完成申请）：
	// 刚提交，未达审批超时阈值，因此没有任何派生卡点
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "等审批的任务", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	task := decodeBody[[]api.Task](t, resp)[0]
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/update-status", tasksURL, task.Id),
		api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	d1 := createDeliverable(t, carol, tasksURL, task.Id, "验收结论.docx")
	uploadCandidate(t, carol, tasksURL, task.Id, d1, api.UploadCandidateRequest{FileName: "验收结论.docx"}, "candidate-bytes")
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews", tasksURL, task.Id),
		api.SubmitCompletionRequest{Note: "已完成"})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

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
		if work.Waiting[i].Kind == "waiting_completion" {
			waiting = &work.Waiting[i]
		}
	}
	if waiting == nil {
		t.Fatalf("等待他人应有完成申请卡片: %+v", work.Waiting)
	}
	if !waiting.CanRemind || waiting.RefKey == nil || !strings.HasPrefix(*waiting.RefKey, "wait:final_review:") {
		t.Fatalf("尚未成卡点的等待事项也应可提醒并按自身键寻址: %+v", waiting)
	}

	remindURL := fmt.Sprintf("%s/projects/%d/reminders", base, created.Id)
	// 不提醒本人：待行动人（终审人＝项目负责人 alice，裁决 11）自己不能提醒
	resp = doJSON(t, alice, http.MethodPost, remindURL, api.RemindRequest{TargetKey: *waiting.RefKey})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()

	// 提交人可提醒；通知带入任务、缺失环节与截止时间
	resp = doJSON(t, carol, http.MethodPost, remindURL, api.RemindRequest{TargetKey: *waiting.RefKey})
	wantStatus(t, resp, http.StatusNoContent)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodGet, base+"/notifications", nil)
	wantStatus(t, resp, http.StatusOK)
	notes := []api.Notification{}
	for _, n := range decodeBody[[]api.Notification](t, resp) {
		if n.Kind == "blocker_remind" {
			notes = append(notes, n)
		}
	}
	if len(notes) != 1 || notes[0].TaskId == nil || *notes[0].TaskId != task.Id {
		t.Fatalf("提醒通知异常: %+v", notes)
	}
	for _, want := range []string{"等审批的任务", "终审处理", "2026-09-30"} {
		if !strings.Contains(notes[0].Content, want) {
			t.Fatalf("提醒正文缺「%s」: %s", want, notes[0].Content)
		}
	}

	// 冷却：同一人对同一任务当天第二次提醒被拒
	resp = doJSON(t, carol, http.MethodPost, remindURL, api.RemindRequest{TargetKey: *waiting.RefKey})
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()

	// 目标已处理后按不存在处理：alice 通过终审，等待事项消失
	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, task.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	reviewID := decodeBody[api.TaskDetail](t, resp).CompletionReviews[0].Id
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews/%d/decision", tasksURL, task.Id, reviewID),
		api.CompletionDecisionRequest{Decision: api.CompletionDecisionRequestDecisionApproved})
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
			{Title: sp("按期交付"), KeyResults: &[]api.CreateKeyResultInput{{Description: "无超期任务"}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	kr1 := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)

	// 建一个不会超期的任务并开始执行：此时没有任何卡点动态
	future := time.Now().AddDate(0, 0, 30).Format("2006-01-02")
	start, end := openapiDate(t, time.Now().Format("2006-01-02")), openapiDate(t, future)
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
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

// O／KR 编辑删除与成员移出守卫（AC-21、AC-61、AC-65，回归 S2／U6；
// 裁决 12 #183：KR 无负责人——编辑收归管理员、职责检查无 KR 项、无交接）。
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
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收"}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	objectiveID, kr1 := okr[0].Id, okr[0].KeyResults[0].Id

	// AC-65（裁决 12 修订）：O 与 KR 都只有项目管理员（含项目负责人）可编辑
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
	resp = doJSON(t, bob, http.MethodPatch, krURL, api.UpdateKeyResultRequest{Description: sp("成员也无权改")})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPatch, krURL, api.UpdateKeyResultRequest{Description: sp("上线自动验收 V2")})
	wantStatus(t, resp, http.StatusOK)
	if got := decodeBody[api.KeyResult](t, resp); got.Description != "上线自动验收 V2" {
		t.Fatalf("KR 描述未更新: %+v", got)
	}

	// 造一件未决审批（裁决 10 后只剩完成审批）：
	// alice 建任务（负责人 carol），carol 提交完成申请进入待终审（裁决 11：管理员集合或签）
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{{KeyResultId: kr1, Name: "联调验证", OwnerId: carolUser.ID, StartDate: start, EndDate: end}},
	})
	wantStatus(t, resp, http.StatusCreated)
	taskID := decodeBody[[]api.Task](t, resp)[0].Id
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/update-status", tasksURL, taskID),
		api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	dHandover := createDeliverable(t, carol, tasksURL, taskID, "联调记录.docx")
	uploadCandidate(t, carol, tasksURL, taskID, dHandover, api.UploadCandidateRequest{FileName: "联调记录.docx"}, "candidate-bytes")
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews", tasksURL, taskID),
		api.SubmitCompletionRequest{Note: "联调完成"})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	pendingReviewID := decodeBody[api.TaskDetail](t, resp).CompletionReviews[0].Id

	// AC-21／AC-61（裁决 12 后无 KR 负责人职责）：carol 仍是任务负责人，不能被移出，
	// 409 里点名待交接的任务
	resp = doJSON(t, alice, http.MethodDelete, fmt.Sprintf("%s/%d", membersURL, carolUser.ID), nil)
	wantStatus(t, resp, http.StatusConflict)
	if e := decodeBody[api.Error](t, resp); !strings.Contains(e.Message, "联调验证") {
		t.Fatalf("409 未列出待交接的任务: %+v", e)
	}

	// S2：bob 被降为只读后同样不能编辑 KR、不能终审（角色失效即职责失效）
	resp = doJSON(t, alice, http.MethodPut, fmt.Sprintf("%s/%d", membersURL, bobUser.ID),
		api.UpdateProjectMemberRoleRequest{Role: api.Viewer})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews/%d/decision", tasksURL, taskID, pendingReviewID),
		api.CompletionDecisionRequest{Decision: api.CompletionDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodPatch, krURL, api.UpdateKeyResultRequest{Description: sp("只读也想改")})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPut, fmt.Sprintf("%s/%d", membersURL, bobUser.ID),
		api.UpdateProjectMemberRoleRequest{Role: api.Member})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	// 裁决 11／12：终审件在项目管理员（alice）的待我审批；普通成员不持有终审件。
	resp = doJSON(t, alice, http.MethodGet, fmt.Sprintf("%s/projects/%d/my-work", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	work := decodeBody[api.MyWork](t, resp)
	found := false
	for _, it := range work.Approvals {
		if it.Kind == "final_review" && it.TaskId != nil && *it.TaskId == taskID {
			found = true
		}
	}
	if !found {
		t.Fatalf("终审件应在项目管理员的待我审批: %+v", work.Approvals)
	}
	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/projects/%d/my-work", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	for _, it := range decodeBody[api.MyWork](t, resp).Approvals {
		if it.Kind == "final_review" {
			t.Fatalf("普通成员不应持有终审件（裁决 11）: %+v", it)
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

// 结构变更直接生效（#172 裁决、§5.2.B）：接收方属关键字段，有编辑权限者修改立即生效，
// 无变更单；动作写入任务动态（裁决 12，#183：KR 无负责人，原站内通知退场）。
func TestStructureEditDirect(t *testing.T) {
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
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收"}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	kr1 := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items:           []api.CreateTaskItem{{KeyResultId: kr1, Name: "联调验证", OwnerId: carolUser.ID, StartDate: start, EndDate: end}},
	})
	wantStatus(t, resp, http.StatusCreated)
	taskID := decodeBody[[]api.Task](t, resp)[0].Id

	// 裁决 10：接收方配置收归项目管理员——负责人 carol 403，项目负责人 alice 直接生效
	receiversURL := fmt.Sprintf("%s/%d/receivers", tasksURL, taskID)
	resp = doJSON(t, carol, http.MethodPut, receiversURL, api.SetReceiversRequest{Scope: api.ReceiverScopeAll})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPut, receiversURL, api.SetReceiversRequest{Scope: api.ReceiverScopeAll})
	applied := wantStructureAccepted(t, resp)
	if applied.ReceiverScope != api.ReceiverScopeAll {
		t.Fatalf("接收方配置应立即生效: %+v", applied.ReceiverScope)
	}
	// 动作写入任务动态（任务字段修改）；裁决 12：不再发字段修改站内通知
	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	detail := decodeBody[api.TaskDetail](t, resp)
	foundActivity := false
	for _, a := range detail.Activities {
		if a.Kind == api.FieldEdited && strings.Contains(a.Summary, "接收方") {
			foundActivity = true
		}
	}
	if !foundActivity {
		t.Fatalf("字段修改应写入任务动态: %+v", detail.Activities)
	}
	resp = doJSON(t, bob, http.MethodGet, base+"/notifications", nil)
	wantStatus(t, resp, http.StatusOK)
	for _, n := range decodeBody[[]api.Notification](t, resp) {
		if n.Kind == "task_field_edited" {
			t.Fatalf("裁决 12 后不应再发字段修改通知: %+v", n)
		}
	}

	// 管理员（bob 升为管理员后）修改同样直接生效
	resp = doJSON(t, alice, http.MethodPut, fmt.Sprintf("%s/projects/%d/members/%d", base, created.Id, bobUser.ID),
		api.UpdateProjectMemberRoleRequest{Role: api.Admin})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodPut, receiversURL, api.SetReceiversRequest{Scope: api.ReceiverScopeMembers, UserIds: &[]int64{carolUser.ID}})
	wantStructureAccepted(t, resp)
}

// 裁决 H1（#141）：提交完成申请之前，交付物项的新增／删除完全自由、即时生效，不走审批；
// 完成申请在审期间冻结；已发布（有当前内容）的项不可删，回报指向成果更新。
func TestDeliverableStructureFree(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	carolUser := seedUser(t, q, "carol", "王五", "carol-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	alice, bob, carol := newClient(t), newClient(t), newClient(t)
	for _, l := range []struct {
		c              *http.Client
		user, password string
	}{{alice, "alice", "alice-pass"}, {bob, "bob", "bob-pass"}, {carol, "carol", "carol-pass"}} {
		resp := doJSON(t, l.c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: l.user, Password: l.password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}

	sp := func(v string) *string { return &v }
	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "交付物自由增删", OwnerId: aliceUser.ID})
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
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收"}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	kr1 := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items:           []api.CreateTaskItem{{KeyResultId: kr1, Name: "联调验证", OwnerId: carolUser.ID, StartDate: start, EndDate: end}},
	})
	wantStatus(t, resp, http.StatusCreated)
	taskID := decodeBody[[]api.Task](t, resp)[0].Id
	deliverablesURL := fmt.Sprintf("%s/%d/deliverables", tasksURL, taskID)
	detailURL := fmt.Sprintf("%s/%d", tasksURL, taskID)
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/update-status", tasksURL, taskID),
		api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	// 已入池任务新增交付物项即时生效，无变更单
	resp = doJSON(t, carol, http.MethodPost, deliverablesURL, api.CreateDeliverableRequest{FileName: "联调报告.docx"})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	first := deliverableOf(t, carol, base, created.Id, taskID, "联调报告")
	if !first.CanDelete {
		t.Fatalf("未发布的项应可删除: %+v", first)
	}
	// 第二项同样即时生效（不再与待审批单互斥）
	resp = doJSON(t, carol, http.MethodPost, deliverablesURL, api.CreateDeliverableRequest{FileName: "另一项.docx"})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	// 空项可自由删
	second := deliverableOf(t, carol, base, created.Id, taskID, "另一项")
	resp = doJSON(t, carol, http.MethodDelete, fmt.Sprintf("%s/%d", deliverablesURL, second.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	// 仅候选的项也可删（候选对象文件同步清理，不留孤行）
	uploadCandidate(t, carol, tasksURL, taskID, first.Id, api.UploadCandidateRequest{FileName: "联调报告v1.docx"}, "candidate-bytes")
	resp = doJSON(t, carol, http.MethodDelete, fmt.Sprintf("%s/%d", deliverablesURL, first.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodGet, detailURL, nil)
	wantStatus(t, resp, http.StatusOK)
	if ds := decodeBody[api.TaskDetail](t, resp).Deliverables; len(ds) != 0 {
		t.Fatalf("删除后不应残留交付物项: %+v", ds)
	}

	// 提交完成申请 → 在审期间增删冻结
	resp = doJSON(t, carol, http.MethodPost, deliverablesURL, api.CreateDeliverableRequest{FileName: "联调报告.docx"})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	kept := deliverableOf(t, carol, base, created.Id, taskID, "联调报告")
	uploadCandidate(t, carol, tasksURL, taskID, kept.Id, api.UploadCandidateRequest{FileName: "联调报告终稿.docx"}, "candidate-bytes")
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews", tasksURL, taskID),
		api.SubmitCompletionRequest{Note: "请终审"})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodPost, deliverablesURL, api.CreateDeliverableRequest{FileName: "迟到的项.docx"})
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodDelete, fmt.Sprintf("%s/%d", deliverablesURL, kept.Id), nil)
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()

	// bob 终审通过 → 已发布的项不可删，回报指向成果更新
	resp = doJSON(t, carol, http.MethodGet, detailURL, nil)
	wantStatus(t, resp, http.StatusOK)
	d := decodeBody[api.TaskDetail](t, resp)
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews/%d/decision", tasksURL, taskID, d.CompletionReviews[0].Id),
		api.CompletionDecisionRequest{Decision: api.CompletionDecisionRequestDecisionApproved})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	published := deliverableOf(t, carol, base, created.Id, taskID, "联调报告")
	if published.Current == nil || published.CanDelete {
		t.Fatalf("已发布的项 canDelete 应为 false: %+v", published)
	}
	resp = doJSON(t, carol, http.MethodDelete, fmt.Sprintf("%s/%d", deliverablesURL, published.Id), nil)
	wantStatus(t, resp, http.StatusConflict)
	if e := decodeBody[api.Error](t, resp); e.Code != "deliverable_has_current" {
		t.Fatalf("已发布项删除的回报应指向成果更新: %+v", e)
	}
}

// 关闭与终态收口（AC-57、裁决 10，#180，回归 R3）：项目管理员直接关闭即时生效；
// 关闭后任务进入终态，不能修改字段、不能再次关闭。
func TestCloseTaskOnTerminalTask(t *testing.T) {
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
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收"}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	kr1 := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{{KeyResultId: kr1, Name: "联调验证", OwnerId: carolUser.ID, StartDate: start, EndDate: end}},
	})
	wantStatus(t, resp, http.StatusCreated)
	taskID := decodeBody[[]api.Task](t, resp)[0].Id

	// 裁决 10：任务负责人与 KR 负责人都不能关闭；管理员修改字段照常可用
	cancelURL := fmt.Sprintf("%s/%d/cancellation", tasksURL, taskID)
	newEnd := openapiDate(t, "2026-10-15")
	editURL := fmt.Sprintf("%s/%d/edits", tasksURL, taskID)
	for _, c := range []*http.Client{carol, bob} {
		resp = doJSON(t, c, http.MethodPost, cancelURL, api.CloseTaskRequest{Reason: "需求取消"})
		wantStatus(t, resp, http.StatusForbidden)
		resp.Body.Close()
	}
	resp = doJSON(t, alice, http.MethodPost, editURL, api.EditTaskFieldsRequest{EndDate: &newEnd})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	// 项目负责人直接关闭：即时生效
	resp = doJSON(t, alice, http.MethodPost, cancelURL, api.CloseTaskRequest{Reason: "需求取消"})
	wantStatus(t, resp, http.StatusOK)
	cancelled := decodeBody[api.Task](t, resp)
	if cancelled.Status != api.TaskStatusCancelled {
		t.Fatalf("任务应为已关闭: %+v", cancelled.Status)
	}

	// 终态任务：不能修改字段、不能再次关闭
	resp = doJSON(t, alice, http.MethodPost, editURL, api.EditTaskFieldsRequest{EndDate: &newEnd})
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPost, cancelURL, api.CloseTaskRequest{Reason: "再次关闭"})
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()

	// 关闭动作写入任务动态（裁决 10：无审批环节，动态即留痕）
	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
	wantStatus(t, resp, http.StatusOK)
	after := decodeBody[api.TaskDetail](t, resp)
	foundClosed := false
	for _, a := range after.Activities {
		if a.Kind == "task_closed" && strings.Contains(a.Summary, "需求取消") {
			foundClosed = true
		}
	}
	if !foundClosed {
		t.Fatalf("关闭应写入任务动态: %+v", after.Activities)
	}
	_ = sp
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
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收"}}},
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
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收"}}},
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
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收"}}},
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
		api.CreateTaskInputRequest{Necessity: api.Required, SourceTaskIds: []int64{up}})
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
			{Description: "KR 一一"},
			{Description: "KR 一二"},
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

// S3：改密码后本人其余会话立即失效，当前会话保留；新密码生效、旧密码失效。
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

	// 当前密码不对、新密码过短、新密码与旧密码相同都要被拒
	resp := doJSON(t, first, http.MethodPost, base+"/auth/change-password",
		api.ChangePasswordRequest{CurrentPassword: strPtr("wrong-pass"), NewPassword: "brand-new-pass"})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
	resp = doJSON(t, first, http.MethodPost, base+"/auth/change-password",
		api.ChangePasswordRequest{CurrentPassword: strPtr("alice-pass"), NewPassword: "short7x"})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
	resp = doJSON(t, first, http.MethodPost, base+"/auth/change-password",
		api.ChangePasswordRequest{CurrentPassword: strPtr("alice-pass"), NewPassword: "alice-pass"})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()

	// 改密码成功
	resp = doJSON(t, first, http.MethodPost, base+"/auth/change-password",
		api.ChangePasswordRequest{CurrentPassword: strPtr("alice-pass"), NewPassword: "brand-new-pass"})
	wantStatus(t, resp, http.StatusNoContent)
	resp.Body.Close()

	// 当前会话仍然可用，另一个会话立即失效
	resp = doJSON(t, first, http.MethodGet, base+"/auth/me", nil)
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, second, http.MethodGet, base+"/auth/me", nil)
	wantStatus(t, resp, http.StatusUnauthorized)
	resp.Body.Close()

	// 旧密码不再能登录，新密码可以
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

	// 建一个停在关闭申请审批的任务（#172 裁决：变更类审批只剩关闭申请），提交时间往前拨 2 天
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("按时推进"), KeyResults: &[]api.CreateKeyResultInput{{Description: "变更及时审批"}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	kr1 := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives[0].KeyResults[0].Id
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/tasks", base, created.Id),
		api.CreateTaskBatchRequest{
			Items: []api.CreateTaskItem{{
				KeyResultId: kr1, Name: "等审批的任务", OwnerId: carolUser.ID,
				StartDate: openapiDate(t, "2026-09-01"), EndDate: openapiDate(t, "2026-09-30"),
			}},
		})
	wantStatus(t, resp, http.StatusCreated)
	task := decodeBody[[]api.Task](t, resp)[0]
	tasksURLSettings := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/update-status", tasksURLSettings, task.Id),
		api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	dTimeout := createDeliverable(t, carol, tasksURLSettings, task.Id, "等审批材料.docx")
	uploadCandidate(t, carol, tasksURLSettings, task.Id, dTimeout, api.UploadCandidateRequest{FileName: "等审批材料.docx"}, "candidate-bytes")
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews", tasksURLSettings, task.Id),
		api.SubmitCompletionRequest{Note: "请终审"})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	if _, err := pool.Exec(context.Background(),
		"UPDATE completion_reviews SET submitted_at = now() - interval '2 days' WHERE task_id = $1", task.Id); err != nil {
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
		// 裁决 11：终审件在项目管理员（alice）的待我审批。
		r := doJSON(t, alice, http.MethodGet, myWorkURL, nil)
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
			{Title: sp("协作到位"), KeyResults: &[]api.CreateKeyResultInput{{Description: "关键材料按时产出"}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "编写评审材料", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	taskID := decodeBody[[]api.Task](t, resp)[0].Id

	participantsURL := fmt.Sprintf("%s/%d/participants", tasksURL, taskID)
	// 无关成员不可配置
	resp = doJSON(t, dave, http.MethodPut, participantsURL, api.SetParticipantsRequest{UserIds: []int64{aliceUser.ID}})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	// 裁决 10：参与人配置收归项目管理员——任务负责人 carol 403
	resp = doJSON(t, carol, http.MethodPut, participantsURL,
		api.SetParticipantsRequest{UserIds: []int64{daveUser.ID}})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	// 非项目成员进不了名单
	resp = doJSON(t, alice, http.MethodPut, participantsURL, api.SetParticipantsRequest{UserIds: []int64{9999}})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
	// 负责人已单列，不重复出现在参与人里
	resp = doJSON(t, alice, http.MethodPut, participantsURL, api.SetParticipantsRequest{UserIds: []int64{carolUser.ID}})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()

	// 管理员配置生效：不属关键字段，直接落库，任务上不留任何待审批变更单
	resp = doJSON(t, alice, http.MethodPut, participantsURL,
		api.SetParticipantsRequest{UserIds: []int64{daveUser.ID, aliceUser.ID, daveUser.ID}})
	wantStatus(t, resp, http.StatusOK)
	saved := decodeBody[api.Task](t, resp)
	if saved.Participants == nil || len(*saved.Participants) != 2 {
		t.Fatalf("参与人应直接生效并去重: %+v", saved.Participants)
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
	if seen.CanUpdateProgress || seen.CanEditFields || seen.CanStart {
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

// 成果更新（AC-66、AC-33、AC-39；#78，裁决 #165 修订）：已完成任务重新发起交付物更新，
// 走同一道完成审批，审批期间任务保持已完成、当前内容继续有效；终审通过后候选覆盖当前内容
// 且旧文件不可恢复，退回则候选保留、进程回到已发起、当前内容不变。
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
			{Title: sp("成果可持续更新"), KeyResults: &[]api.CreateKeyResultInput{{Description: "交付物随迭代更新"}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives
	kr1 := okr[0].KeyResults[0].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")

	// alice 建任务（裁决 10：创建收归管理员）→ carol 执行 → 首次定稿走完完成审批
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "输出接口说明", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	taskID := decodeBody[[]api.Task](t, resp)[0].Id
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/update-status", tasksURL, taskID),
		api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	detailURL := fmt.Sprintf("%s/%d", tasksURL, taskID)
	deliverableID := createDeliverable(t, carol, tasksURL, taskID, "接口说明.docx")
	uploadCandidate(t, carol, tasksURL, taskID, deliverableID, api.UploadCandidateRequest{FileName: "接口说明-v1.docx"}, "v1-bytes")
	completionURL := fmt.Sprintf("%s/%d/completion-reviews", tasksURL, taskID)
	resp = doJSON(t, carol, http.MethodPost, completionURL, api.SubmitCompletionRequest{Note: "首次定稿"})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodGet, detailURL, nil)
	wantStatus(t, resp, http.StatusOK)
	detail := decodeBody[api.TaskDetail](t, resp)
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/%d/decision", completionURL, detail.CompletionReviews[0].Id),
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
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/%d/decision", completionURL, updateReviewID),
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
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/%d/decision", completionURL, detail.CompletionReviews[0].Id),
		api.CompletionDecisionRequest{Decision: api.CompletionDecisionRequestDecisionRejected, Opinion: &op})
	wantStatus(t, resp, http.StatusOK)
	rejected := decodeBody[api.Task](t, resp)
	if rejected.Status != api.TaskStatusCompleted {
		t.Fatalf("成果更新退回后任务仍为已完成: %+v", rejected.Status)
	}
	// 裁决 #165：退回后成果更新回到「已发起」——候选保留，可删改后重新提交。
	if rejected.ResultUpdate == nil || *rejected.ResultUpdate != api.ResultUpdateStateOpen {
		t.Fatalf("退回后成果更新应回到已发起: %+v", rejected.ResultUpdate)
	}
	resp = doJSON(t, alice, http.MethodGet, detailURL, nil)
	wantStatus(t, resp, http.StatusOK)
	detail = decodeBody[api.TaskDetail](t, resp)
	d = detail.Deliverables[0]
	if d.Candidate == nil {
		t.Fatalf("退回后候选应保留（裁决 #165）: %+v", d)
	}
	if d.Current == nil || d.Current.FileName != "接口说明-v2.docx" {
		t.Fatalf("退回不改变当前内容: %+v", d.Current)
	}
	// 负责人删除保留的候选（成果更新已发起时可删），随后进程仍为已发起。
	resp = doJSON(t, carol, http.MethodDelete, fmt.Sprintf("%s/%d/deliverables/%d/candidate", tasksURL, taskID, deliverableID), nil)
	wantStatus(t, resp, http.StatusOK)
	if got := decodeBody[api.Task](t, resp); got.ResultUpdate == nil || *got.ResultUpdate != api.ResultUpdateStateOpen {
		t.Fatalf("删除候选不应改变成果更新进程: %+v", got.ResultUpdate)
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
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{{KeyResultId: kr1, Name: "待取消任务", OwnerId: bobUser.ID, StartDate: start, EndDate: end}},
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
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/%d/cancellation", tasksURL, cancelledID),
		api.CloseTaskRequest{Reason: "需求取消"})
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
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收"}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	kr1 := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives[0].KeyResults[0].Id
	tasksURL := projectURL + "/tasks"
	start, end := openapiDate(t, "2026-09-01"), openapiDate(t, "2026-09-30")
	resp = doJSON(t, alice, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		Items: []api.CreateTaskItem{
			{KeyResultId: kr1, Name: "输出验收方案", OwnerId: bobUser.ID, StartDate: start, EndDate: end},
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
		"", "/tasks", "/objectives", "/edges", "/blockers", "/artifacts", "/report", "/my-work", "/members", "/settings",
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
	deliverableID := createDeliverable(t, bob, tasksURL, taskID, "验收方案.docx")
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
			Necessity: api.Required, SourceTaskIds: []int64{taskID}}},
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

// #192：写请求同源校验在真实装配（含登录接口）上的四条验收——同源通过、跨源 403、
// 无 Origin 通过、Sec-Fetch-Site: cross-site 403；GET 不受影响。
func TestSameOriginGuardOnRealHandler(t *testing.T) {
	q, pool := setupDB(t)
	seedUser(t, q, "alice", "张三", "alice-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	loginWith := func(headers map[string]string) *http.Response {
		t.Helper()
		body, _ := json.Marshal(api.LoginRequest{Username: "alice", Password: "alice-pass"})
		req, err := http.NewRequest(http.MethodPost, base+"/auth/login", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := newClient(t).Do(req)
		if err != nil {
			t.Fatalf("login: %v", err)
		}
		return resp
	}

	t.Run("同源 Origin 的登录通过", func(t *testing.T) {
		resp := loginWith(map[string]string{"Origin": ts.URL, "Sec-Fetch-Site": "same-origin"})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	})
	t.Run("跨源 Origin 的登录得 403", func(t *testing.T) {
		resp := loginWith(map[string]string{"Origin": "http://evil.example"})
		wantStatus(t, resp, http.StatusForbidden)
		if e := decodeBody[api.Error](t, resp); e.Code != "cross_origin" {
			t.Fatalf("code = %q, want cross_origin", e.Code)
		}
	})
	t.Run("无 Origin 的登录通过", func(t *testing.T) {
		resp := loginWith(nil)
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	})
	t.Run("Sec-Fetch-Site cross-site 得 403", func(t *testing.T) {
		resp := loginWith(map[string]string{"Sec-Fetch-Site": "cross-site"})
		wantStatus(t, resp, http.StatusForbidden)
		resp.Body.Close()
	})
	t.Run("GET 带跨源 Origin 不受影响", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, base+"/healthz", nil)
		req.Header.Set("Origin", "http://evil.example")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("healthz: %v", err)
		}
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	})
	t.Run("已登录后的跨源写请求同样 403", func(t *testing.T) {
		c := newClient(t)
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: "alice", Password: "alice-pass"})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
		req, _ := http.NewRequest(http.MethodPost, base+"/projects", strings.NewReader(`{"name":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://evil.example")
		resp, err := c.Do(req)
		if err != nil {
			t.Fatalf("projects: %v", err)
		}
		wantStatus(t, resp, http.StatusForbidden)
		resp.Body.Close()
	})
}

// #191：真实装配上的请求体上限——超限 413 与统一 Error；上限以内（含只差几十字节）行为不变。
func TestRequestBodyLimit(t *testing.T) {
	q, pool := setupDB(t)
	seedUser(t, q, "alice", "张三", "alice-pass")

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	// 合法 JSON 内部填空白，把 body 撑到目标大小：JSON 语法允许对象内任意空白。
	paddedLogin := func(size int) string {
		head := `{"username":"alice","password":"alice-pass"`
		tail := `}`
		return head + strings.Repeat(" ", size-len(head)-len(tail)) + tail
	}
	limit := 4 << 20

	t.Run("超限 1 字节得 413", func(t *testing.T) {
		resp := doRaw(t, newClient(t), http.MethodPost, base+"/auth/login", paddedLogin(limit+1))
		wantStatus(t, resp, http.StatusRequestEntityTooLarge)
		if e := decodeBody[api.Error](t, resp); e.Code != "payload_too_large" {
			t.Fatalf("code = %q, want payload_too_large", e.Code)
		}
	})
	t.Run("恰好上限通过", func(t *testing.T) {
		resp := doRaw(t, newClient(t), http.MethodPost, base+"/auth/login", paddedLogin(limit))
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	})
	t.Run("未声明长度的超限 body 同样 413", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, base+"/auth/login", io.NopCloser(strings.NewReader(paddedLogin(limit+1))))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.ContentLength = -1 // 强制分块传输，服务端拿不到 Content-Length
		req.Header.Set("Content-Type", "application/json")
		resp, err := newClient(t).Do(req)
		if err != nil {
			t.Fatalf("login: %v", err)
		}
		wantStatus(t, resp, http.StatusRequestEntityTooLarge)
		resp.Body.Close()
	})
}

// #200：系统管理员隐式视同任意项目的管理员——能读写其非成员的私有项目、项目列表含全部项目；
// 成员列表不含仅隐式身份；当前用户接口带 isSystemAdmin；普通非成员对同一项目仍是 404。
func TestSystemAdminImplicitProjectAdmin(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	seedUser(t, q, "bob", "李四", "bob-pass")
	rootUser := seedUser(t, q, "root", "系统管理员", "root-pass1")
	if _, err := q.SetUserSystemAdmin(context.Background(), store.SetUserSystemAdminParams{ID: rootUser.ID, IsSystemAdmin: true}); err != nil {
		t.Fatalf("set system admin: %v", err)
	}

	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	login := func(username, password string) *http.Client {
		t.Helper()
		c := newClient(t)
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
		return c
	}
	alice := login("alice", "alice-pass")
	bob := login("bob", "bob-pass")
	root := login("root", "root-pass1")

	// 当前用户接口带系统管理员标记。
	me := decodeBody[api.CurrentUser](t, doJSONAgain(t, root, http.MethodGet, base+"/auth/me"))
	if !me.IsSystemAdmin {
		t.Fatalf("root 应为系统管理员: %+v", me)
	}
	if decodeBody[api.CurrentUser](t, doJSONAgain(t, alice, http.MethodGet, base+"/auth/me")).IsSystemAdmin {
		t.Fatal("alice 不应为系统管理员")
	}

	// alice 建一个私有项目，root 与 bob 都不是成员。
	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "私有项目", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)

	// 普通非成员：404（读越权与写越权同一道边界）。
	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/projects/%d", base, created.Id), nil)
	wantStatus(t, resp, http.StatusNotFound)
	resp.Body.Close()

	// 系统管理员：项目列表含全部项目，读得到、派生权限视同管理员。
	list := decodeBody[[]api.Project](t, doJSONAgain(t, root, http.MethodGet, base+"/projects"))
	found := false
	for _, p := range list {
		if p.Id == created.Id {
			found = true
			if !p.CanEdit || p.ImplicitViewer {
				t.Fatalf("系统管理员在项目列表里应可编辑且不是隐式访客: %+v", p)
			}
		}
	}
	if !found {
		t.Fatalf("系统管理员的项目列表应含非成员项目: %+v", list)
	}
	got := decodeBody[api.Project](t, doJSONAgain(t, root, http.MethodGet, fmt.Sprintf("%s/projects/%d", base, created.Id)))
	if !got.CanEdit || got.ImplicitViewer {
		t.Fatalf("系统管理员读项目应视同管理员: %+v", got)
	}

	// 写：改项目基础信息、改规则设置、加成员，均 200。
	resp = doJSON(t, root, http.MethodPut, fmt.Sprintf("%s/projects/%d", base, created.Id), api.UpdateProjectRequest{
		Name: "私有项目（管理员改名）", OwnerId: aliceUser.ID, Status: api.ProjectStatusInProgress, Visibility: api.Private,
	})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, root, http.MethodGet, fmt.Sprintf("%s/projects/%d/settings", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	// 成员列表不含仅隐式身份的系统管理员。
	members := decodeBody[[]api.ProjectMember](t, doJSONAgain(t, root, http.MethodGet, fmt.Sprintf("%s/projects/%d/members", base, created.Id)))
	for _, m := range members {
		if m.UserId == rootUser.ID {
			t.Fatalf("仅隐式身份的系统管理员不应出现在成员列表: %+v", members)
		}
	}
	// 项目审计里的操作者是本人（隐式身份的写操作照常留痕）。
	audits := decodeBody[[]api.AuditLog](t, doJSONAgain(t, root, http.MethodGet, fmt.Sprintf("%s/projects/%d/audit-logs", base, created.Id)))
	seenRoot := false
	for _, a := range audits {
		if a.ActorName != nil && *a.ActorName == rootUser.DisplayName {
			seenRoot = true
		}
	}
	if !seenRoot {
		t.Fatalf("系统管理员的项目写操作应进项目审计且操作者为本人: %+v", audits)
	}
}

// #201：系统设置用户列表只对系统管理员开放——200 含全部用户与标记，普通用户 403。
func TestSystemUsersListRequiresSystemAdmin(t *testing.T) {
	q, pool := setupDB(t)
	seedUser(t, q, "alice", "张三", "alice-pass")
	rootUser := seedUser(t, q, "root", "系统管理员", "root-pass1")
	if _, err := q.SetUserSystemAdmin(context.Background(), store.SetUserSystemAdminParams{ID: rootUser.ID, IsSystemAdmin: true}); err != nil {
		t.Fatalf("set system admin: %v", err)
	}
	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"
	login := func(username, password string) *http.Client {
		t.Helper()
		c := newClient(t)
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
		return c
	}
	alice := login("alice", "alice-pass")
	root := login("root", "root-pass1")

	resp := doJSON(t, alice, http.MethodGet, base+"/system/users", nil)
	wantStatus(t, resp, http.StatusForbidden)
	if e := decodeBody[api.Error](t, resp); e.Code != "system_admin_required" {
		t.Fatalf("code = %q, want system_admin_required", e.Code)
	}

	users := decodeBody[[]api.SystemUser](t, doJSONAgain(t, root, http.MethodGet, base+"/system/users"))
	if len(users) != 2 {
		t.Fatalf("应列出全部 2 个用户: %+v", users)
	}
	if users[0].Username != "alice" || users[0].IsSystemAdmin || users[1].Username != "root" || !users[1].IsSystemAdmin {
		t.Fatalf("列表按 id 升序且带系统管理员标记: %+v", users)
	}
}

// #202：邮箱必填且全局唯一（大小写不敏感）；迁移在含既有用户的库上可 up/down 且回填占位；
// 当前用户、用户摘要与系统用户列表都带邮箱。
func TestUserEmailRequiredAndUnique(t *testing.T) {
	q, pool := setupDB(t)

	// 迁移回退到 00050、插入无邮箱的存量用户、再升级：回填 <用户名>@local.invalid。
	migDB, err := sql.Open("pgx", pool.Config().ConnString())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer migDB.Close()
	if err := goose.DownTo(migDB, "../../migrations", 50); err != nil {
		t.Fatalf("goose down: %v", err)
	}
	if _, err := migDB.Exec(`INSERT INTO users (username, display_name, password_hash) VALUES ('legacy', '存量用户', 'x')`); err != nil {
		t.Fatalf("insert legacy: %v", err)
	}
	if err := goose.Up(migDB, "../../migrations"); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	legacy, err := q.GetUserByUsername(context.Background(), "legacy")
	if err != nil {
		t.Fatalf("legacy: %v", err)
	}
	if legacy.Email != "legacy@local.invalid" {
		t.Fatalf("存量用户应回填占位邮箱，得到 %q", legacy.Email)
	}

	// 重复邮箱（含大小写差异）被唯一索引拒绝。
	seedUser(t, q, "alice", "张三", "alice-pass")
	hash, _ := domain.HashPassword("dup-pass-1")
	_, err = q.CreateUser(context.Background(), store.CreateUserParams{
		Username: "alice2", DisplayName: "张三二号", PasswordHash: hash, Email: "ALICE@Example.com",
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("大小写不同的重复邮箱应触发唯一冲突，得到 %v", err)
	}
	if _, err := q.GetUserByEmail(context.Background(), "Alice@EXAMPLE.com"); err != nil {
		t.Fatalf("按邮箱查找应大小写不敏感: %v", err)
	}

	// 接口带邮箱。
	rootUser := seedUser(t, q, "root", "系统管理员", "root-pass1")
	if _, err := q.SetUserSystemAdmin(context.Background(), store.SetUserSystemAdminParams{ID: rootUser.ID, IsSystemAdmin: true}); err != nil {
		t.Fatalf("set system admin: %v", err)
	}
	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"
	root := newClient(t)
	resp := doJSON(t, root, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: "root", Password: "root-pass1"})
	wantStatus(t, resp, http.StatusOK)
	if me := decodeBody[api.CurrentUser](t, resp); me.Email != "root@example.com" {
		t.Fatalf("当前用户应带邮箱: %+v", me)
	}
	users := decodeBody[[]api.UserSummary](t, doJSONAgain(t, root, http.MethodGet, base+"/users"))
	for _, u := range users {
		if u.Email == "" {
			t.Fatalf("用户摘要应带邮箱: %+v", u)
		}
	}
	sys := decodeBody[[]api.SystemUser](t, doJSONAgain(t, root, http.MethodGet, base+"/system/users"))
	for _, u := range sys {
		if u.Email == "" {
			t.Fatalf("系统用户列表应带邮箱: %+v", u)
		}
	}
}

// #203：管理员建号 → 新用户带「须改密码」→ 改密前业务接口 403 → 改密（免旧密码）后进入系统；
// 字段规则与重复用户名／邮箱；建号仅系统管理员可调。
func TestCreateSystemUserAndForcedPasswordChange(t *testing.T) {
	q, pool := setupDB(t)
	seedUser(t, q, "alice", "张三", "alice-pass")
	rootUser := seedUser(t, q, "root", "系统管理员", "root-pass1")
	if _, err := q.SetUserSystemAdmin(context.Background(), store.SetUserSystemAdminParams{ID: rootUser.ID, IsSystemAdmin: true}); err != nil {
		t.Fatalf("set system admin: %v", err)
	}
	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"
	login := func(username, password string) (*http.Client, api.CurrentUser) {
		t.Helper()
		c := newClient(t)
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		return c, decodeBody[api.CurrentUser](t, resp)
	}
	alice, _ := login("alice", "alice-pass")
	root, _ := login("root", "root-pass1")

	newUser := api.CreateSystemUserRequest{Username: "wangwu", DisplayName: "王五", Email: "WangWu@Example.com", Password: "init-pass-1"}
	// 非系统管理员 403。
	resp := doJSON(t, alice, http.MethodPost, base+"/system/users", newUser)
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	// 字段规则：密码过短／过长、用户名非法、邮箱非法 → 422。
	for name, bad := range map[string]api.CreateSystemUserRequest{
		"密码 7 位":  {Username: "u1", DisplayName: "x", Email: "u1@example.com", Password: "abcdefg"},
		"密码 33 位": {Username: "u2", DisplayName: "x", Email: "u2@example.com", Password: strings.Repeat("a", 33)},
		"用户名大写":  {Username: "Bad", DisplayName: "x", Email: "u3@example.com", Password: "abcdefgh"},
		"邮箱非法":   {Username: "user4", DisplayName: "x", Email: "nope", Password: "abcdefgh"},
	} {
		resp := doJSON(t, root, http.MethodPost, base+"/system/users", bad)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("%s: status = %d, want 422", name, resp.StatusCode)
		}
		resp.Body.Close()
	}
	// 建号成功：邮箱归一小写、带须改密码。
	resp = doJSON(t, root, http.MethodPost, base+"/system/users", newUser)
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.SystemUser](t, resp)
	if created.Email != "wangwu@example.com" || created.MustChangePassword == nil || !*created.MustChangePassword {
		t.Fatalf("建号结果异常: %+v", created)
	}
	// 重复用户名 409 username_taken；重复邮箱（大小写不同）409 email_taken。
	resp = doJSON(t, root, http.MethodPost, base+"/system/users", api.CreateSystemUserRequest{Username: "wangwu", DisplayName: "x", Email: "other@example.com", Password: "abcdefgh"})
	wantStatus(t, resp, http.StatusConflict)
	if e := decodeBody[api.Error](t, resp); e.Code != "username_taken" {
		t.Fatalf("code = %q, want username_taken", e.Code)
	}
	resp = doJSON(t, root, http.MethodPost, base+"/system/users", api.CreateSystemUserRequest{Username: "wangwu2", DisplayName: "x", Email: "WANGWU@example.com", Password: "abcdefgh"})
	wantStatus(t, resp, http.StatusConflict)
	if e := decodeBody[api.Error](t, resp); e.Code != "email_taken" {
		t.Fatalf("code = %q, want email_taken", e.Code)
	}

	// 新用户首登：标记为真；业务接口 403 password_change_required；读当前用户与登出放行。
	wang, me := login("wangwu", "init-pass-1")
	if !me.MustChangePassword {
		t.Fatalf("新用户登录应带须改密码: %+v", me)
	}
	resp = doJSON(t, wang, http.MethodGet, base+"/projects", nil)
	wantStatus(t, resp, http.StatusForbidden)
	if e := decodeBody[api.Error](t, resp); e.Code != "password_change_required" {
		t.Fatalf("code = %q, want password_change_required", e.Code)
	}
	resp = doJSON(t, wang, http.MethodGet, base+"/auth/me", nil)
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	// 改密：新密码与初始密码相同被拒；不足 8 位被拒；合法则免旧密码通过并清除标记。
	resp = doJSON(t, wang, http.MethodPost, base+"/auth/change-password", api.ChangePasswordRequest{NewPassword: "init-pass-1"})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
	resp = doJSON(t, wang, http.MethodPost, base+"/auth/change-password", api.ChangePasswordRequest{NewPassword: "short7x"})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
	resp = doJSON(t, wang, http.MethodPost, base+"/auth/change-password", api.ChangePasswordRequest{NewPassword: "my-new-pass-9"})
	wantStatus(t, resp, http.StatusNoContent)
	resp.Body.Close()
	if me := decodeBody[api.CurrentUser](t, doJSONAgain(t, wang, http.MethodGet, base+"/auth/me")); me.MustChangePassword {
		t.Fatalf("改密后标记应清除: %+v", me)
	}
	resp = doJSON(t, wang, http.MethodGet, base+"/projects", nil)
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	// 非首次改密的用户：省略当前密码或当前密码错误 → 422。
	resp = doJSON(t, alice, http.MethodPost, base+"/auth/change-password", api.ChangePasswordRequest{NewPassword: "another-pass-1"})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
}

func strPtr(v string) *string { return &v }

// #204：停用／启用——停用后原会话 401；正确密码登录得「已停用」（403），错误密码仍是统一文案（401）；
// /users 与成员列表默认不含停用用户，成员列表 includeDisabled 带回并标记；任务负责人带 ownerDisabled；
// 不能停用自己；启用后可登录。
func TestDisableAndEnableUser(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	rootUser := seedUser(t, q, "root", "系统管理员", "root-pass1")
	if _, err := q.SetUserSystemAdmin(context.Background(), store.SetUserSystemAdminParams{ID: rootUser.ID, IsSystemAdmin: true}); err != nil {
		t.Fatalf("set system admin: %v", err)
	}
	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"
	loginRaw := func(username, password string) (*http.Client, *http.Response) {
		t.Helper()
		c := newClient(t)
		return c, doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
	}
	alice, resp := loginRaw("alice", "alice-pass")
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	bob, resp := loginRaw("bob", "bob-pass")
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	root, resp := loginRaw("root", "root-pass1")
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	// alice 建项目，bob 加为成员并负责一个任务。
	resp = doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "停用演示", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	project := decodeBody[api.Project](t, resp)
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, project.Id), api.AddProjectMembersRequest{UserIds: []int64{bobUser.ID}, Role: api.Member})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, project.Id), api.CreateOkrBatchRequest{
		Items: []api.CreateOkrBatchItem{{Title: strPtr("O1"), KeyResults: &[]api.CreateKeyResultInput{{Description: "KR1"}}}},
	})
	wantStatus(t, resp, http.StatusCreated)
	krID := decodeBody[api.CreateOkrBatchResponse](t, resp).Objectives[0].KeyResults[0].Id
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/tasks", base, project.Id), api.CreateTaskBatchRequest{Items: []api.CreateTaskItem{{
		KeyResultId: krID, Name: "李四的任务", OwnerId: bobUser.ID, StartDate: openapiDate(t, "2026-09-01"), EndDate: openapiDate(t, "2026-09-30"),
	}}})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()

	// 不能停用自己。
	resp = doJSON(t, root, http.MethodPost, fmt.Sprintf("%s/system/users/%d/disable", base, rootUser.ID), nil)
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
	// 非系统管理员 403。
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/system/users/%d/disable", base, bobUser.ID), nil)
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()

	// 停用 bob。
	resp = doJSON(t, root, http.MethodPost, fmt.Sprintf("%s/system/users/%d/disable", base, bobUser.ID), nil)
	wantStatus(t, resp, http.StatusOK)
	if u := decodeBody[api.SystemUser](t, resp); u.Disabled == nil || !*u.Disabled {
		t.Fatalf("停用后应标记 disabled: %+v", u)
	}
	// 原会话 401。
	resp = doJSON(t, bob, http.MethodGet, base+"/auth/me", nil)
	wantStatus(t, resp, http.StatusUnauthorized)
	resp.Body.Close()
	// 正确密码：403 已停用；错误密码：401 统一文案。
	_, resp = loginRaw("bob", "bob-pass")
	wantStatus(t, resp, http.StatusForbidden)
	if e := decodeBody[api.Error](t, resp); e.Code != "account_disabled" {
		t.Fatalf("code = %q, want account_disabled", e.Code)
	}
	_, resp = loginRaw("bob", "wrong-pass")
	wantStatus(t, resp, http.StatusUnauthorized)
	if e := decodeBody[api.Error](t, resp); e.Code != "invalid_credentials" {
		t.Fatalf("code = %q, want invalid_credentials", e.Code)
	}
	// /users 不含 bob；成员列表默认不含 bob，includeDisabled 带回并标记。
	for _, u := range decodeBody[[]api.UserSummary](t, doJSONAgain(t, alice, http.MethodGet, base+"/users")) {
		if u.Id == bobUser.ID {
			t.Fatal("停用用户不应出现在 /users")
		}
	}
	for _, m := range decodeBody[[]api.ProjectMember](t, doJSONAgain(t, alice, http.MethodGet, fmt.Sprintf("%s/projects/%d/members", base, project.Id))) {
		if m.UserId == bobUser.ID {
			t.Fatal("停用成员默认不应出现在成员列表")
		}
	}
	found := false
	for _, m := range decodeBody[[]api.ProjectMember](t, doJSONAgain(t, alice, http.MethodGet, fmt.Sprintf("%s/projects/%d/members?includeDisabled=true", base, project.Id))) {
		if m.UserId == bobUser.ID {
			found = true
			if m.Disabled == nil || !*m.Disabled {
				t.Fatalf("带回的停用成员应标记 disabled: %+v", m)
			}
		}
	}
	if !found {
		t.Fatal("includeDisabled=true 应带回停用成员")
	}
	// 任务仍显示 bob 的名字并带 ownerDisabled。
	tasks := decodeBody[[]api.Task](t, doJSONAgain(t, alice, http.MethodGet, fmt.Sprintf("%s/projects/%d/tasks", base, project.Id)))
	if len(tasks) != 1 || tasks[0].OwnerName != "李四" || tasks[0].OwnerDisabled == nil || !*tasks[0].OwnerDisabled {
		t.Fatalf("任务应保留负责人姓名并标记停用: %+v", tasks)
	}

	// 启用后可登录。
	resp = doJSON(t, root, http.MethodPost, fmt.Sprintf("%s/system/users/%d/enable", base, bobUser.ID), nil)
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	_, resp = loginRaw("bob", "bob-pass")
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
}

// #205：重置密码 → 旧会话 401、新密码登录被引导改密；改显示名与邮箱（重复邮箱 409）；
// 设／撤系统管理员且不能撤销自己；三项接口仅系统管理员可调。
func TestSystemUserResetProfileAdmin(t *testing.T) {
	q, pool := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	rootUser := seedUser(t, q, "root", "系统管理员", "root-pass1")
	if _, err := q.SetUserSystemAdmin(context.Background(), store.SetUserSystemAdminParams{ID: rootUser.ID, IsSystemAdmin: true}); err != nil {
		t.Fatalf("set system admin: %v", err)
	}
	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"
	loginRaw := func(username, password string) (*http.Client, *http.Response) {
		t.Helper()
		c := newClient(t)
		return c, doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
	}
	alice, resp := loginRaw("alice", "alice-pass")
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	bob, resp := loginRaw("bob", "bob-pass")
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	root, resp := loginRaw("root", "root-pass1")
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	bobURL := fmt.Sprintf("%s/system/users/%d", base, bobUser.ID)

	// 非系统管理员 403。
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, bobURL + "/reset-password", api.ResetPasswordRequest{Password: "reset-pass-1"}},
		{http.MethodPut, bobURL + "/profile", api.UpdateUserProfileRequest{DisplayName: "x", Email: "x@example.com"}},
		{http.MethodPut, bobURL + "/system-admin", api.SetSystemAdminRequest{IsSystemAdmin: true}},
	} {
		resp := doJSON(t, alice, tc.method, tc.path, tc.body)
		wantStatus(t, resp, http.StatusForbidden)
		resp.Body.Close()
	}

	// 重置密码：过短 422；成功后 bob 旧会话 401、新密码登录带须改密码。
	resp = doJSON(t, root, http.MethodPost, bobURL+"/reset-password", api.ResetPasswordRequest{Password: "short7x"})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
	resp = doJSON(t, root, http.MethodPost, bobURL+"/reset-password", api.ResetPasswordRequest{Password: "reset-pass-1"})
	wantStatus(t, resp, http.StatusOK)
	if u := decodeBody[api.SystemUser](t, resp); u.MustChangePassword == nil || !*u.MustChangePassword {
		t.Fatalf("重置后应置须改密码: %+v", u)
	}
	resp = doJSON(t, bob, http.MethodGet, base+"/auth/me", nil)
	wantStatus(t, resp, http.StatusUnauthorized)
	resp.Body.Close()
	_, resp = loginRaw("bob", "reset-pass-1")
	wantStatus(t, resp, http.StatusOK)
	if me := decodeBody[api.CurrentUser](t, resp); !me.MustChangePassword {
		t.Fatalf("新密码登录应被引导改密: %+v", me)
	}

	// 改资料：显示名与邮箱；改为他人已用邮箱（大小写不同）409 email_taken。
	resp = doJSON(t, root, http.MethodPut, bobURL+"/profile", api.UpdateUserProfileRequest{DisplayName: "李四改", Email: "Bob.New@Example.com"})
	wantStatus(t, resp, http.StatusOK)
	if u := decodeBody[api.SystemUser](t, resp); u.DisplayName != "李四改" || u.Email != "bob.new@example.com" {
		t.Fatalf("改资料结果异常: %+v", u)
	}
	resp = doJSON(t, root, http.MethodPut, bobURL+"/profile", api.UpdateUserProfileRequest{DisplayName: "李四", Email: "ALICE@example.com"})
	wantStatus(t, resp, http.StatusConflict)
	if e := decodeBody[api.Error](t, resp); e.Code != "email_taken" {
		t.Fatalf("code = %q, want email_taken", e.Code)
	}
	resp = doJSON(t, root, http.MethodPut, bobURL+"/profile", api.UpdateUserProfileRequest{DisplayName: " ", Email: "bob@example.com"})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
	_ = aliceUser

	// 设／撤系统管理员：设 bob 成功；撤销自己被拒；撤 bob 成功。
	resp = doJSON(t, root, http.MethodPut, bobURL+"/system-admin", api.SetSystemAdminRequest{IsSystemAdmin: true})
	wantStatus(t, resp, http.StatusOK)
	if u := decodeBody[api.SystemUser](t, resp); !u.IsSystemAdmin {
		t.Fatalf("应已设为系统管理员: %+v", u)
	}
	resp = doJSON(t, root, http.MethodPut, fmt.Sprintf("%s/system/users/%d/system-admin", base, rootUser.ID), api.SetSystemAdminRequest{IsSystemAdmin: false})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	if e := decodeBody[api.Error](t, resp); e.Code != "cannot_revoke_own_admin" {
		t.Fatalf("code = %q, want cannot_revoke_own_admin", e.Code)
	}
	resp = doJSON(t, root, http.MethodPut, bobURL+"/system-admin", api.SetSystemAdminRequest{IsSystemAdmin: false})
	wantStatus(t, resp, http.StatusOK)
	if u := decodeBody[api.SystemUser](t, resp); u.IsSystemAdmin {
		t.Fatalf("应已撤销系统管理员: %+v", u)
	}
	// 不存在的用户 404。
	resp = doJSON(t, root, http.MethodPut, fmt.Sprintf("%s/system/users/%d/system-admin", base, 99999), api.SetSystemAdminRequest{IsSystemAdmin: true})
	wantStatus(t, resp, http.StatusNotFound)
	resp.Body.Close()
}

// #206：建号、停用、重置密码、设撤管理员各产生一条系统级审计（操作者为当前管理员、无项目作用域、
// 密码不进摘要）；操作审计只对系统管理员可见；项目域审计行为不变（既有用例覆盖）。
func TestSystemAuditLogs(t *testing.T) {
	q, pool := setupDB(t)
	seedUser(t, q, "alice", "张三", "alice-pass")
	rootUser := seedUser(t, q, "root", "系统管理员", "root-pass1")
	if _, err := q.SetUserSystemAdmin(context.Background(), store.SetUserSystemAdminParams{ID: rootUser.ID, IsSystemAdmin: true}); err != nil {
		t.Fatalf("set system admin: %v", err)
	}
	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"
	login := func(username, password string) *http.Client {
		t.Helper()
		c := newClient(t)
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
		return c
	}
	alice := login("alice", "alice-pass")
	root := login("root", "root-pass1")

	resp := doJSON(t, alice, http.MethodGet, base+"/system/audit-logs", nil)
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()

	// 四种系统级写操作。
	resp = doJSON(t, root, http.MethodPost, base+"/system/users", api.CreateSystemUserRequest{Username: "carol", DisplayName: "王五", Email: "carol@example.com", Password: "init-pass-1"})
	wantStatus(t, resp, http.StatusCreated)
	carol := decodeBody[api.SystemUser](t, resp)
	carolURL := fmt.Sprintf("%s/system/users/%d", base, carol.Id)
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, carolURL + "/disable", nil},
		{http.MethodPost, carolURL + "/reset-password", api.ResetPasswordRequest{Password: "reset-pass-1"}},
		{http.MethodPut, carolURL + "/system-admin", api.SetSystemAdminRequest{IsSystemAdmin: true}},
	} {
		resp := doJSON(t, root, tc.method, tc.path, tc.body)
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	// 失败的写操作不留痕：撤销自己 422。
	resp = doJSON(t, root, http.MethodPut, fmt.Sprintf("%s/system/users/%d/system-admin", base, rootUser.ID), api.SetSystemAdminRequest{IsSystemAdmin: false})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()

	logs := decodeBody[[]api.AuditLog](t, doJSONAgain(t, root, http.MethodGet, base+"/system/audit-logs"))
	if len(logs) != 4 {
		t.Fatalf("应有 4 条系统级审计，得到 %d: %+v", len(logs), logs)
	}
	wantActions := []string{"设／撤系统管理员", "重置用户密码", "停用用户", "新建用户"}
	for i, a := range logs {
		if a.Action != wantActions[i] {
			t.Fatalf("第 %d 条动作 = %q, want %q", i, a.Action, wantActions[i])
		}
		if a.ActorName == nil || *a.ActorName != "系统管理员" {
			t.Fatalf("操作者应为当前管理员: %+v", a)
		}
		if strings.Contains(a.Action, "pass") || strings.Contains(a.Route, "init-pass") {
			t.Fatalf("密码不应进入审计: %+v", a)
		}
	}
	if logs[3].ObjectType == nil || *logs[3].ObjectType != "users" || logs[0].ObjectId == nil || *logs[0].ObjectId != carol.Id {
		t.Fatalf("对象应指向用户: %+v %+v", logs[3], logs[0])
	}
	// 系统级记录不混入任何项目的审计（项目域查询按 project_id 过滤，直接查库确认作用域为空）。
	rows, err := q.ListSystemAuditLogs(context.Background(), 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, r := range rows {
		if r.ProjectID.Valid {
			t.Fatalf("系统级审计的 project_id 应为空: %+v", r)
		}
	}
}

// #207：本人改显示名与邮箱——即时生效于当前用户；邮箱重复 409；不产生系统级审计；
// 「须改密码」状态下不放行（仍 403）。
func TestUpdateMyProfile(t *testing.T) {
	q, pool := setupDB(t)
	seedUser(t, q, "alice", "张三", "alice-pass")
	seedUser(t, q, "bob", "李四", "bob-pass")
	rootUser := seedUser(t, q, "root", "系统管理员", "root-pass1")
	if _, err := q.SetUserSystemAdmin(context.Background(), store.SetUserSystemAdminParams{ID: rootUser.ID, IsSystemAdmin: true}); err != nil {
		t.Fatalf("set system admin: %v", err)
	}
	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"
	login := func(username, password string) *http.Client {
		t.Helper()
		c := newClient(t)
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
		return c
	}
	alice := login("alice", "alice-pass")
	root := login("root", "root-pass1")

	resp := doJSON(t, alice, http.MethodPut, base+"/me/profile", api.UpdateUserProfileRequest{DisplayName: "张三丰", Email: "Alice.New@Example.com"})
	wantStatus(t, resp, http.StatusOK)
	if me := decodeBody[api.CurrentUser](t, resp); me.DisplayName != "张三丰" || me.Email != "alice.new@example.com" || me.Username != "alice" {
		t.Fatalf("改资料结果异常: %+v", me)
	}
	if me := decodeBody[api.CurrentUser](t, doJSONAgain(t, alice, http.MethodGet, base+"/auth/me")); me.DisplayName != "张三丰" {
		t.Fatalf("当前用户应即时更新: %+v", me)
	}
	resp = doJSON(t, alice, http.MethodPut, base+"/me/profile", api.UpdateUserProfileRequest{DisplayName: "张三丰", Email: "BOB@example.com"})
	wantStatus(t, resp, http.StatusConflict)
	if e := decodeBody[api.Error](t, resp); e.Code != "email_taken" {
		t.Fatalf("code = %q, want email_taken", e.Code)
	}
	resp = doJSON(t, alice, http.MethodPut, base+"/me/profile", api.UpdateUserProfileRequest{DisplayName: "张三丰", Email: ""})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
	// 本人改资料不进系统级审计。
	if logs := decodeBody[[]api.AuditLog](t, doJSONAgain(t, root, http.MethodGet, base+"/system/audit-logs")); len(logs) != 0 {
		t.Fatalf("本人改资料不应产生系统级审计: %+v", logs)
	}
}

// #208：两处登录后会话列表两条且当前会话有标识；「退出其他设备」后另一处 401、本处不受影响；
// 最近登录时间在当前用户与用户管理列表可见。
func TestLoginSecuritySessions(t *testing.T) {
	q, pool := setupDB(t)
	seedUser(t, q, "alice", "张三", "alice-pass")
	rootUser := seedUser(t, q, "root", "系统管理员", "root-pass1")
	if _, err := q.SetUserSystemAdmin(context.Background(), store.SetUserSystemAdminParams{ID: rootUser.ID, IsSystemAdmin: true}); err != nil {
		t.Fatalf("set system admin: %v", err)
	}
	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"
	login := func(username, password string) (*http.Client, api.CurrentUser) {
		t.Helper()
		c := newClient(t)
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		return c, decodeBody[api.CurrentUser](t, resp)
	}
	phone, _ := login("alice", "alice-pass")
	laptop, me := login("alice", "alice-pass")
	if me.LastLoginAt == nil {
		t.Fatalf("登录应记录最近登录时间: %+v", me)
	}

	sessions := decodeBody[[]api.SessionInfo](t, doJSONAgain(t, laptop, http.MethodGet, base+"/me/sessions"))
	if len(sessions) != 2 {
		t.Fatalf("应有 2 条活跃会话: %+v", sessions)
	}
	current := 0
	for _, x := range sessions {
		if x.Current {
			current++
		}
		if x.CreatedAt.IsZero() || x.LastActiveAt.IsZero() || x.ExpiresAt.IsZero() {
			t.Fatalf("会话时间字段应齐全: %+v", x)
		}
	}
	if current != 1 {
		t.Fatalf("应恰有 1 条当前会话: %+v", sessions)
	}

	resp := doJSON(t, laptop, http.MethodPost, base+"/me/sessions/logout-others", nil)
	wantStatus(t, resp, http.StatusNoContent)
	resp.Body.Close()
	resp = doJSON(t, phone, http.MethodGet, base+"/auth/me", nil)
	wantStatus(t, resp, http.StatusUnauthorized)
	resp.Body.Close()
	if left := decodeBody[[]api.SessionInfo](t, doJSONAgain(t, laptop, http.MethodGet, base+"/me/sessions")); len(left) != 1 || !left[0].Current {
		t.Fatalf("退出其他设备后只剩当前会话: %+v", left)
	}

	root, _ := login("root", "root-pass1")
	for _, u := range decodeBody[[]api.SystemUser](t, doJSONAgain(t, root, http.MethodGet, base+"/system/users")) {
		if u.Username == "alice" && u.LastLoginAt == nil {
			t.Fatalf("用户管理列表应显示最近登录: %+v", u)
		}
	}
}

// #210：品牌读取免登录；修改基本信息仅系统管理员，超长被拒，改后品牌读取同步；写操作进系统级审计。
func TestSystemSettingsBranding(t *testing.T) {
	q, pool := setupDB(t)
	seedUser(t, q, "alice", "张三", "alice-pass")
	rootUser := seedUser(t, q, "root", "系统管理员", "root-pass1")
	if _, err := q.SetUserSystemAdmin(context.Background(), store.SetUserSystemAdminParams{ID: rootUser.ID, IsSystemAdmin: true}); err != nil {
		t.Fatalf("set system admin: %v", err)
	}
	ts := httptest.NewServer(newTestHandler(t, pool))
	defer ts.Close()
	base := ts.URL + "/api/v1"

	// 未登录可读品牌，默认值齐全。
	resp, err := http.Get(base + "/branding")
	if err != nil {
		t.Fatalf("branding: %v", err)
	}
	wantStatus(t, resp, http.StatusOK)
	b := decodeBody[api.Branding](t, resp)
	if b.SystemName != "协同管理工具" || b.Subtitle != "O／KR／任务协同推进" || b.LoginHint != "账号由管理员分配" || b.CanRecoverPassword {
		t.Fatalf("默认品牌异常: %+v", b)
	}

	login := func(username, password string) *http.Client {
		t.Helper()
		c := newClient(t)
		resp := doJSON(t, c, http.MethodPost, base+"/auth/login", api.LoginRequest{Username: username, Password: password})
		wantStatus(t, resp, http.StatusOK)
		resp.Body.Close()
		return c
	}
	alice := login("alice", "alice-pass")
	root := login("root", "root-pass1")
	body := api.SystemSettingsInput{SystemName: "新名称", Subtitle: "新副标题", LoginHint: "请用工号登录", BaseUrl: "http://203.0.113.10/"}
	resp = doJSON(t, alice, http.MethodPut, base+"/system/settings", body)
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodGet, base+"/system/settings", nil)
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	// 超长 422（名称 11 字）。
	resp = doJSON(t, root, http.MethodPut, base+"/system/settings", api.SystemSettingsInput{SystemName: strings.Repeat("名", 11), Subtitle: "", LoginHint: "", BaseUrl: ""})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
	resp = doJSON(t, root, http.MethodPut, base+"/system/settings", body)
	wantStatus(t, resp, http.StatusOK)
	if st := decodeBody[api.SystemSettings](t, resp); st.SystemName != "新名称" || st.BaseUrl != "http://203.0.113.10" {
		t.Fatalf("保存结果异常: %+v", st)
	}
	resp, _ = http.Get(base + "/branding")
	if b := decodeBody[api.Branding](t, resp); b.SystemName != "新名称" || b.Subtitle != "新副标题" || b.LoginHint != "请用工号登录" {
		t.Fatalf("品牌读取应同步: %+v", b)
	}
	logs := decodeBody[[]api.AuditLog](t, doJSONAgain(t, root, http.MethodGet, base+"/system/audit-logs"))
	if len(logs) != 1 || logs[0].Action != "修改系统基本信息" {
		t.Fatalf("应有一条系统级审计: %+v", logs)
	}
}

