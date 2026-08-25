package api_test

// 集成测试：httptest + 真实 Postgres（docker compose up -d postgres）。
// 每次运行建独立数据库并用 goose 跑迁移，结束后删除。
// 无 Postgres 环境用 go test -short ./... 跳过。

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/pressly/goose/v3"

	"synergy/server/internal/api"
	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

func setupDB(t *testing.T) *store.Queries {
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
	return store.New(pool)
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
		t.Fatalf("status = %d, want %d", resp.StatusCode, want)
	}
}

func TestAuthAndProjectsEndToEnd(t *testing.T) {
	q := setupDB(t)
	aliceUser := seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")

	ts := httptest.NewServer(api.NewHandler(q, "/api/v1"))
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
	if created.Status != api.NotStarted {
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

	// 更新项目：改负责人、状态与计划周期
	ps := openapiDate(t, "2026-09-01")
	pe := openapiDate(t, "2026-12-31")
	resp = doJSON(t, alice, http.MethodPut, fmt.Sprintf("%s/projects/%d", base, created.Id), api.UpdateProjectRequest{
		Name: "协同管理试点", OwnerId: aliceUser.ID, Status: api.InProgress,
		PlannedStartDate: &ps, PlannedEndDate: &pe,
	})
	wantStatus(t, resp, http.StatusOK)
	updated := decodeBody[api.Project](t, resp)
	if updated.Status != api.InProgress || updated.OwnerName != "张三" {
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
		Name: "任意", OwnerId: aliceUser.ID, Status: api.NotStarted,
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
	q := setupDB(t)
	seedUser(t, q, "alice", "张三", "alice-pass")
	bobUser := seedUser(t, q, "bob", "李四", "bob-pass")
	carolUser := seedUser(t, q, "carol", "王五", "carol-pass")

	ts := httptest.NewServer(api.NewHandler(q, "/api/v1"))
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

	// 管理员把 carol 加为只读成员
	resp = doJSON(t, alice, http.MethodPost, membersURL, api.AddProjectMemberRequest{UserId: carolUser.ID, Role: api.Viewer})
	wantStatus(t, resp, http.StatusCreated)
	added := decodeBody[api.ProjectMember](t, resp)
	if added.DisplayName != "王五" || added.Role != api.Viewer {
		t.Fatalf("加入成员返回异常: %+v", added)
	}

	// 只读成员既不能管理成员也不能编辑项目 → 403
	resp = doJSON(t, carol, http.MethodPost, membersURL, api.AddProjectMemberRequest{UserId: bobUser.ID, Role: api.Member})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodPut, fmt.Sprintf("%s/projects/%d", base, created.Id), api.UpdateProjectRequest{
		Name: "成员试点", OwnerId: bobUser.ID, Status: api.NotStarted,
	})
	wantStatus(t, resp, http.StatusForbidden)
	if e := decodeBody[api.Error](t, resp); e.Code != "forbidden" {
		t.Fatalf("code = %q, want forbidden", e.Code)
	}

	// carol 视角的派生字段为不可操作
	resp = doJSON(t, carol, http.MethodGet, base+"/projects", nil)
	wantStatus(t, resp, http.StatusOK)
	if list := decodeBody[[]api.Project](t, resp); list[0].CanEdit || list[0].CanManageMembers {
		t.Fatalf("只读成员派生字段异常: %+v", list[0])
	}

	// 项目负责人 bob 非成员，但享有与管理员同等权限（V4.4.2）：可编辑项目、可调整成员角色
	resp = doJSON(t, bob, http.MethodGet, base+"/projects", nil)
	wantStatus(t, resp, http.StatusOK)
	if list := decodeBody[[]api.Project](t, resp); !list[0].CanEdit || !list[0].CanManageMembers {
		t.Fatalf("负责人派生字段异常: %+v", list[0])
	}
	resp = doJSON(t, bob, http.MethodPut, fmt.Sprintf("%s/projects/%d/members/%d", base, created.Id, carolUser.ID),
		api.UpdateProjectMemberRoleRequest{Role: api.Editor})
	wantStatus(t, resp, http.StatusOK)
	if m := decodeBody[api.ProjectMember](t, resp); m.Role != api.Editor {
		t.Fatalf("角色调整返回异常: %+v", m)
	}

	// 可编辑成员仍不能管理成员 → 403
	resp = doJSON(t, carol, http.MethodPost, membersURL, api.AddProjectMemberRequest{UserId: bobUser.ID, Role: api.Member})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()

	// 重复加入 409
	resp = doJSON(t, alice, http.MethodPost, membersURL, api.AddProjectMemberRequest{UserId: carolUser.ID, Role: api.Member})
	wantStatus(t, resp, http.StatusConflict)
	if e := decodeBody[api.Error](t, resp); e.Code != "already_member" {
		t.Fatalf("code = %q, want already_member", e.Code)
	}

	// 非法角色 422
	resp = doJSON(t, alice, http.MethodPost, membersURL, map[string]any{"userId": bobUser.ID, "role": "boss"})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	if e := decodeBody[api.Error](t, resp); e.Code != "invalid_member_role" {
		t.Fatalf("code = %q, want invalid_member_role", e.Code)
	}

	// 用户不存在 422
	resp = doJSON(t, alice, http.MethodPost, membersURL, api.AddProjectMemberRequest{UserId: 99999, Role: api.Member})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	if e := decodeBody[api.Error](t, resp); e.Code != "invalid_user" {
		t.Fatalf("code = %q, want invalid_user", e.Code)
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

func TestLoginRateLimit(t *testing.T) {
	q := setupDB(t)

	ts := httptest.NewServer(api.NewHandler(q, "/api/v1"))
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
