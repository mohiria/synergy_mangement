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
	"synergy/server/internal/filestore"
	"synergy/server/internal/store"
)

func newTestHandler(t *testing.T, pool *pgxpool.Pool) http.Handler {
	t.Helper()
	// presign 为纯签名计算，测试无需真实 MinIO 服务。
	files, err := filestore.NewMinio("localhost:9000", "", "synergy", "synergy-dev-secret", "synergy-test", false)
	if err != nil {
		t.Fatalf("filestore: %v", err)
	}
	return api.NewHandler(pool, "/api/v1", files)
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

	// 更新项目：改负责人、状态与计划周期
	ps := openapiDate(t, "2026-09-01")
	pe := openapiDate(t, "2026-12-31")
	resp = doJSON(t, alice, http.MethodPut, fmt.Sprintf("%s/projects/%d", base, created.Id), api.UpdateProjectRequest{
		Name: "协同管理试点", OwnerId: aliceUser.ID, Status: api.ProjectStatusInProgress,
		PlannedStartDate: &ps, PlannedEndDate: &pe,
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
		Name: "任意", OwnerId: aliceUser.ID, Status: api.ProjectStatusNotStarted,
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
		Name: "成员试点", OwnerId: bobUser.ID, Status: api.ProjectStatusNotStarted,
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
		api.UpdateProjectMemberRoleRequest{Role: api.Member})
	wantStatus(t, resp, http.StatusOK)
	if m := decodeBody[api.ProjectMember](t, resp); m.Role != api.Member {
		t.Fatalf("角色调整返回异常: %+v", m)
	}

	// 已取消的 editor 角色（V4.4.3）→ 422
	resp = doJSON(t, alice, http.MethodPut, fmt.Sprintf("%s/projects/%d/members/%d", base, created.Id, carolUser.ID),
		map[string]any{"role": "editor"})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	if e := decodeBody[api.Error](t, resp); e.Code != "invalid_member_role" {
		t.Fatalf("code = %q, want invalid_member_role", e.Code)
	}

	// 普通成员仍不能管理成员 → 403
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

// O／KR 表格式创建（#3，AC-01）：一批建多个 O 与 KR、指定 KR 负责人；
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

	// alice 创建项目并任负责人，bob 加为普通成员
	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "OKR 试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
		api.AddProjectMemberRequest{UserId: bobUser.ID, Role: api.Member})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()

	// 新增端点：单项目详情，派生字段随身份变化
	resp = doJSON(t, bob, http.MethodGet, fmt.Sprintf("%s/projects/%d", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	if p := decodeBody[api.Project](t, resp); p.Id != created.Id || p.CanEdit {
		t.Fatalf("普通成员的项目详情派生字段异常: %+v", p)
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
	list := decodeBody[[]api.Objective](t, resp)
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
	if kr.StartDate == nil || kr.EndDate == nil {
		t.Fatalf("KR 周期未保存: %+v", kr)
	}
	if second := list[0].KeyResults[1]; second.OwnerId != nil || second.SortOrder != 2 {
		t.Fatalf("未指定负责人的 KR 异常: %+v", second)
	}

	// 向已有 O 追加 KR
	resp = doJSON(t, alice, http.MethodPost, okrURL, api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
		{ObjectiveId: &list[1].Id, KeyResults: &[]api.CreateKeyResultInput{{Description: "签下 10 家标杆客户"}}},
	}})
	wantStatus(t, resp, http.StatusCreated)
	list = decodeBody[[]api.Objective](t, resp)
	if len(list) != 2 || len(list[1].KeyResults) != 1 || list[1].KeyResults[0].Description != "签下 10 家标杆客户" {
		t.Fatalf("追加 KR 异常: %+v", list)
	}

	// 普通成员读取层级列表 200
	resp = doJSON(t, bob, http.MethodGet, okrURL, nil)
	wantStatus(t, resp, http.StatusOK)
	if got := decodeBody[[]api.Objective](t, resp); len(got) != 2 {
		t.Fatalf("成员读取 O／KR 列表异常: %+v", got)
	}

	// 普通成员批量创建 403（编辑项目结构需管理员／负责人）
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
// 普通成员提交任务→待入池审批→KR 负责人通过→未开始；退回→草稿可重新提交；
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

	// alice（管理员/负责人）建项目，bob、carol 为普通成员；bob 任 KR 负责人
	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "任务试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	for _, uid := range []int64{bobUser.ID, carolUser.ID} {
		resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
			api.AddProjectMemberRequest{UserId: uid, Role: api.Member})
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
	okr := decodeBody[[]api.Objective](t, resp)
	krWithOwner := okr[0].KeyResults[0].Id
	krNoOwner := okr[0].KeyResults[1].Id
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-12"), openapiDate(t, "2026-09-21")

	// AC-04：普通成员 carol 创建并提交，任务进入待入池审批
	resp = doJSON(t, carol, http.MethodPost, tasksURL, api.CreateTaskBatchRequest{
		SubmitForReview: true,
		Items: []api.CreateTaskItem{
			{KeyResultId: krWithOwner, Name: "验证现场联动异常回退", OwnerId: carolUser.ID, StartDate: start, EndDate: end},
		},
	})
	wantStatus(t, resp, http.StatusCreated)
	list := decodeBody[[]api.Task](t, resp)
	if len(list) != 1 || list[0].Status != api.TaskStatusPendingPoolReview {
		t.Fatalf("普通成员提交后应为待入池审批: %+v", list)
	}
	taskID := list[0].Id
	if list[0].PoolReview == nil || list[0].PoolReview.Status != api.PoolReviewStatusPending || list[0].PoolReview.Exempt {
		t.Fatalf("审批单异常: %+v", list[0].PoolReview)
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

	// 只读成员不能创建任务 → 403
	daveUser := seedUser(t, q, "dave", "赵六", "dave-pass")
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
		api.AddProjectMemberRequest{UserId: daveUser.ID, Role: api.Viewer})
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

	// 只读成员可查看任务列表
	resp = doJSON(t, dave, http.MethodGet, tasksURL, nil)
	wantStatus(t, resp, http.StatusOK)
	if got := decodeBody[[]api.Task](t, resp); len(got) != 3 {
		t.Fatalf("任务列表数量异常: %+v", got)
	}
}

// 任务创建邀请（#5，AC-03）：KR 负责人邀请成员→受邀人通过邀请创建并提交任务入池→邀请完成；
// 撤回后不可再响应；无关任务不使邀请结束；普通成员不可发邀请。
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

	// alice 建项目；bob、carol 普通成员；bob 任 KR1 负责人，另建无负责人的 KR2
	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "邀请试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	for _, uid := range []int64{bobUser.ID, carolUser.ID} {
		resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
			api.AddProjectMemberRequest{UserId: uid, Role: api.Member})
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
	okr := decodeBody[[]api.Objective](t, resp)
	kr1, kr2 := okr[0].KeyResults[0].Id, okr[0].KeyResults[1].Id
	invitesURL := fmt.Sprintf("%s/projects/%d/task-invites", base, created.Id)
	tasksURL := fmt.Sprintf("%s/projects/%d/tasks", base, created.Id)
	start, end := openapiDate(t, "2026-09-12"), openapiDate(t, "2026-09-21")

	// 普通成员 carol（非 KR 负责人）发邀请 403
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

	// 邀请自己 422；邀请只读成员 422
	daveUser := seedUser(t, q, "dave", "赵六", "dave-pass")
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
		api.AddProjectMemberRequest{UserId: daveUser.ID, Role: api.Viewer})
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

// 任务状态与进度（#6，AC-12）：开始执行、可空进度、取消保留原因、KR 覆盖度派生。
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
			api.AddProjectMemberRequest{UserId: uid, Role: api.Member})
		wantStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[[]api.Objective](t, resp)
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

	// KR 覆盖度：2 个入池任务、1 个已填，平均 45
	resp = doJSON(t, alice, http.MethodGet, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	okr = decodeBody[[]api.Objective](t, resp)
	summary := okr[0].KeyResults[0].ProgressSummary
	if summary == nil || summary.TotalTasks != 2 || summary.FilledTasks != 1 || summary.AverageProgress == nil || *summary.AverageProgress != 45 {
		t.Fatalf("KR 覆盖度异常: %+v", summary)
	}

	// 清除进度后覆盖度归零
	resp = doJSON(t, bob, http.MethodPut, progressURL(bobTask.Id), api.UpdateTaskProgressRequest{})
	wantStatus(t, resp, http.StatusOK)
	if got := decodeBody[api.Task](t, resp); got.Progress != nil {
		t.Fatalf("进度未清除: %+v", got)
	}

	// 取消：原因必填 422；负责人取消保留原因；已取消不计入覆盖度
	resp = doJSON(t, carol, http.MethodPost, statusURL(carolTask.Id), api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusCancelled})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()
	reason := "需求合并，不再单独执行"
	resp = doJSON(t, carol, http.MethodPost, statusURL(carolTask.Id), api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusCancelled, Reason: &reason})
	wantStatus(t, resp, http.StatusOK)
	cancelled := decodeBody[api.Task](t, resp)
	if cancelled.Status != api.TaskStatusCancelled || cancelled.CancelReason == nil || *cancelled.CancelReason != reason {
		t.Fatalf("取消后状态异常: %+v", cancelled)
	}
	resp = doJSON(t, alice, http.MethodGet, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	okr = decodeBody[[]api.Objective](t, resp)
	if s2 := okr[0].KeyResults[0].ProgressSummary; s2 == nil || s2.TotalTasks != 1 || s2.FilledTasks != 0 || s2.AverageProgress != nil {
		t.Fatalf("取消后覆盖度异常: %+v", okr[0].KeyResults[0].ProgressSummary)
	}

	// 已取消任务不可再取消 409
	resp = doJSON(t, carol, http.MethodPost, statusURL(carolTask.Id), api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusCancelled, Reason: &reason})
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
		api.AddProjectMemberRequest{UserId: bobUser.ID, Role: api.Member})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
		api.AddProjectMemberRequest{UserId: daveUser.ID, Role: api.Viewer})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[[]api.Objective](t, resp)
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
	if tasks[0].CurrentStage != "创建入池审批" || tasks[0].PendingActorName == nil || *tasks[0].PendingActorName != "李四" {
		t.Fatalf("当前环节/待行动人派生异常: %+v", tasks[0])
	}

	// 只读成员 dave 可查看详情（AC-34），但无任何动作标志
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
		t.Fatalf("只读成员不应有任何动作标志: %+v", detail.Task)
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
			api.AddProjectMemberRequest{UserId: uid, Role: api.Member})
		wantStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[[]api.Objective](t, resp)
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
		api.AddProjectMemberRequest{UserId: bobUser.ID, Role: api.Member})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[[]api.Objective](t, resp)
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

	// 再补一个交付物项（一个任务多项交付物）
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/deliverables", tasksURL, taskID),
		api.CreateDeliverableRequest{Name: "现场验收记录"})
	wantStatus(t, resp, http.StatusCreated)
	second := decodeBody[api.Deliverable](t, resp)

	// 空名称 422
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/deliverables", tasksURL, taskID),
		api.CreateDeliverableRequest{Name: "  "})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()

	// 任务开始执行后，负责人登记候选内容并取得预签名上传地址
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/update-status", tasksURL, taskID),
		api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/deliverables/%d/candidate", tasksURL, taskID, second.Id),
		api.UploadCandidateRequest{FileName: "现场验收记录.xlsx", FileType: sp("xlsx")})
	wantStatus(t, resp, http.StatusCreated)
	up := decodeBody[api.UploadCandidateResponse](t, resp)
	if up.UploadUrl == "" || up.File.State != api.Candidate {
		t.Fatalf("候选登记异常: %+v", up)
	}

	// 非负责人 alice 也可（管理员纠错）；无关成员会被拒——此处验证重复登记覆盖旧候选
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/deliverables/%d/candidate", tasksURL, taskID, second.Id),
		api.UploadCandidateRequest{FileName: "现场验收记录-rev2.xlsx"})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()

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

// 任务讨论与定向通知（#9，AC-35/AC-36）：只读成员可提交意见并 @ 成员；
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

	// alice 建项目；bob 成员并任 KR 负责人与任务负责人；carol 只读成员；dave 非成员
	resp := doJSON(t, alice, http.MethodPost, base+"/projects", api.CreateProjectRequest{Name: "讨论试点", OwnerId: aliceUser.ID})
	wantStatus(t, resp, http.StatusCreated)
	created := decodeBody[api.Project](t, resp)
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
		api.AddProjectMemberRequest{UserId: bobUser.ID, Role: api.Member})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/members", base, created.Id),
		api.AddProjectMemberRequest{UserId: carolUser.ID, Role: api.Viewer})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[[]api.Objective](t, resp)
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

	// AC-35：只读成员 carol 提交意见并 @ alice
	resp = doJSON(t, carol, http.MethodPost, discussURL,
		api.CreateDiscussionRequest{Content: "建议补充断链回退场景。", MentionUserIds: &[]int64{aliceUser.ID}})
	wantStatus(t, resp, http.StatusCreated)
	d := decodeBody[api.Discussion](t, resp)
	if d.AuthorName != "王五" || d.MentionNames == nil || (*d.MentionNames)[0] != "张三" {
		t.Fatalf("讨论意见异常: %+v", d)
	}

	// 非项目成员 dave 403
	resp = doJSON(t, dave, http.MethodPost, discussURL, api.CreateDiscussionRequest{Content: "外部插话"})
	wantStatus(t, resp, http.StatusForbidden)
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
	bobNotes := decodeBody[[]api.Notification](t, resp)
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
	resp = doJSON(t, dave, http.MethodGet, fmt.Sprintf("%s/%d", tasksURL, taskID), nil)
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

// 完成申请与 KR 终审（#10，AC-13/15/38/39/40）：提交→直接待 KR 终审→退回删候选回进行中
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
			api.AddProjectMemberRequest{UserId: uid, Role: api.Member})
		wantStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[[]api.Objective](t, resp)
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
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/deliverables", tasksURL, taskID),
		api.CreateDeliverableRequest{Name: "验收记录"})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()
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
		resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/deliverables/%d/candidate", tasksURL, taskID, did),
			api.UploadCandidateRequest{FileName: fmt.Sprintf("成果-%d.docx", i+1)})
		wantStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
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
	if pending.Status != api.TaskStatusPendingFinalReview || pending.CurrentStage != "KR 终审" {
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
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/deliverables/%d/candidate", tasksURL, taskID, d1),
		api.UploadCandidateRequest{FileName: "成果-终版.docx"})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()
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

// 必要输入与交付物边（#13，AC-07/28/48）：选择已有任务及交付物建边、复杂关系（双向/循环/跨 KR）、
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
			api.AddProjectMemberRequest{UserId: uid, Role: api.Member})
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
	okr := decodeBody[[]api.Objective](t, resp)
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

	// AC-28：B 的负责人 carol 选择 A 及其交付物建立必要输入边
	inputsURL := func(id int64) string { return fmt.Sprintf("%s/%d/inputs", tasksURL, id) }
	resp = doJSON(t, carol, http.MethodPost, inputsURL(taskB.Id), api.CreateTaskInputRequest{
		Name: "现场数据包", Necessity: api.Required, EdgeType: api.HardPrerequisite,
		SourceTaskId: taskA.Id, DeliverableId: &dA,
	})
	wantStatus(t, resp, http.StatusCreated)
	edge := decodeBody[api.DeliverableEdge](t, resp)
	if edge.Ready || edge.SourceTaskName == nil || *edge.SourceTaskName != "采集现场数据" {
		t.Fatalf("新建边应未就绪且含来源信息: %+v", edge)
	}

	// AC-07：反向再建一条反馈边（双向/循环关系保留真实连线）
	resp = doJSON(t, bob, http.MethodPost, inputsURL(taskA.Id), api.CreateTaskInputRequest{
		Name: "回归问题清单", Necessity: api.Reference, EdgeType: api.Feedback, SourceTaskId: taskB.Id,
	})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()
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
		Name: "自环", Necessity: api.Required, EdgeType: api.Information, SourceTaskId: taskB.Id,
	})
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	resp.Body.Close()

	// AC-48：A 走完成终审后当前内容生效 → 边自动就绪、B 不再等待输入
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/update-status", tasksURL, taskA.Id),
		api.UpdateTaskStatusRequest{Status: api.UpdateTaskStatusRequestStatusInProgress})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doJSON(t, bob, http.MethodPost, fmt.Sprintf("%s/%d/deliverables/%d/candidate", tasksURL, taskA.Id, dA),
		api.UploadCandidateRequest{FileName: "现场数据包.zip"})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()

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

	// 解除边：A 已完成（终态）目标边不可再配置 → 409/403；改由 B 的负责人解除指向 B 的输入边
	resp = doJSON(t, bob, http.MethodDelete, fmt.Sprintf("%s/projects/%d/edges/%d", base, created.Id, detailB.Outputs[0].Id), nil)
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodDelete, fmt.Sprintf("%s/projects/%d/edges/%d", base, created.Id, detailB.Inputs[0].Id), nil)
	wantStatus(t, resp, http.StatusNoContent)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodGet, fmt.Sprintf("%s/projects/%d/edges", base, created.Id), nil)
	wantStatus(t, resp, http.StatusOK)
	if edges := decodeBody[[]api.DeliverableEdge](t, resp); len(edges) != 1 {
		t.Fatalf("解除后边数量异常: %+v", edges)
	}
}

// 中间审核或签与退回（#11，AC-14/24/37）：配置或签组→提交进入中间审核→任一人通过进待终审
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
			api.AddProjectMemberRequest{UserId: uid, Role: api.Member})
		wantStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[[]api.Objective](t, resp)
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

	// carol 配置或签组（dave、erin）；只读成员会被拒
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
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/deliverables/%d/candidate", tasksURL, taskID, dA),
		api.UploadCandidateRequest{FileName: "验收方案V1.docx"})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/completion-reviews", tasksURL, taskID),
		api.SubmitCompletionRequest{Note: "请或签审核"})
	wantStatus(t, resp, http.StatusOK)
	submitted := decodeBody[api.Task](t, resp)
	if submitted.Status != api.TaskStatusPendingIntermediateReview || submitted.CurrentStage != "中间或签审核" {
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
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/deliverables/%d/candidate", tasksURL, taskID, dA),
		api.UploadCandidateRequest{FileName: "验收方案V2.docx"})
	wantStatus(t, resp, http.StatusCreated)
	resp.Body.Close()
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

// 指定成员输入请求（#14，AC-29/30）：草稿阶段不通知→入池通过后带上下文通知→同意接收（不就绪）
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
			api.AddProjectMemberRequest{UserId: uid, Role: api.Member})
		wantStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}
	resp = doJSON(t, alice, http.MethodPost, fmt.Sprintf("%s/projects/%d/objectives", base, created.Id),
		api.CreateOkrBatchRequest{Items: []api.CreateOkrBatchItem{
			{Title: sp("提升交付质量"), KeyResults: &[]api.CreateKeyResultInput{{Description: "上线自动验收", OwnerId: &bobUser.ID}}},
		}})
	wantStatus(t, resp, http.StatusCreated)
	okr := decodeBody[[]api.Objective](t, resp)
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
			Name: "接口字段口径", Necessity: api.Required, ProviderId: daveUser.ID,
			ContentNote: "请提供最新接口字段口径说明", ExpectedDate: expected,
		})
	wantStatus(t, resp, http.StatusCreated)
	edge := decodeBody[api.DeliverableEdge](t, resp)
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
	bad := api.CreateMemberInputRequest{Name: "缺期望时间", Necessity: api.Required, ProviderId: daveUser.ID, ContentNote: "x"}
	resp = doJSON(t, carol, http.MethodPost, fmt.Sprintf("%s/%d/member-inputs", tasksURL, taskID), map[string]any{
		"name": bad.Name, "necessity": bad.Necessity, "providerId": bad.ProviderId, "contentNote": bad.ContentNote,
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
	if provided.Request.State != api.InputRequestStateProvided || provided.UploadUrl == nil {
		t.Fatalf("提交结果异常: %+v", provided)
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
