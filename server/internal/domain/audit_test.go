package domain

import "testing"

// §10.4／R8：写路径装饰器要给每一类项目内变化定出可读的动作名。
// 路由没登记时退化为通用词，不回显 HTTP 方法与路径——但留痕本身不能少。
func TestAuditActionLabel(t *testing.T) {
	cases := []struct {
		method string
		route  string
		want   string
	}{
		{"POST", "/projects/{projectId}/tasks", "创建任务"},
		{"POST", "/projects/{projectId}/tasks/{taskId}/update-status", "流转任务状态"},
		{"PUT", "/projects/{projectId}/tasks/{taskId}/progress", "更新任务进度"},
		{"DELETE", "/projects/{projectId}/edges/{edgeId}", "解除交付物边"},
		{"PUT", "/projects/{projectId}/tasks/{taskId}/receivers", "配置接收方"},
		{"PUT", "/projects/{projectId}/tasks/{taskId}/participants", "配置参与人"},
		{"POST", "/projects/{projectId}/members", "新增项目成员"},
		{"PUT", "/projects/{projectId}/members/{userId}", "调整成员角色"},
		{"DELETE", "/projects/{projectId}/members/{userId}", "移出项目成员"},
		{"PATCH", "/projects/{projectId}", "修改项目信息"},
		{"POST", "/projects/{projectId}/some-future-write", "项目内写操作"},
	}
	for _, tc := range cases {
		if got := AuditActionLabel(tc.method, tc.route); got != tc.want {
			t.Fatalf("AuditActionLabel(%s %s) = %q, want %q", tc.method, tc.route, got, tc.want)
		}
	}
}

// R8：只读请求不留痕；项目域之外的请求也不留痕。
func TestAuditable(t *testing.T) {
	if Auditable("GET", "/projects/{projectId}/tasks") {
		t.Fatal("读请求不应留痕")
	}
	if Auditable("POST", "/auth/login") {
		t.Fatal("非项目域请求不应进项目审计")
	}
	if !Auditable("POST", "/projects/{projectId}/tasks") {
		t.Fatal("项目内写请求应留痕")
	}
	if !Auditable("DELETE", "/projects/{projectId}/edges/{edgeId}") {
		t.Fatal("DELETE 也应留痕")
	}
}

// R9：每个「卡点出现」最终都要有对应「卡点解除」。
// ticker 没有「变更前快照」，只能拿「仍记着出现未解除的键」与「当前仍在的卡点」比对补记。
func TestStaleBlockerResolutions(t *testing.T) {
	open := []OpenBlockerFact{
		{TaskID: 1, Key: "task_overdue:1", Summary: "卡点出现：任务超期"},
		{TaskID: 2, Key: "upstream_unready:edge:9", Summary: "卡点出现：上游未就绪 · 缺 现场数据包"},
	}
	current := []Blocker{{TaskID: 2, Key: "upstream_unready:edge:9", Kind: BlockerUpstreamUnready, Missing: "现场数据包"}}
	now := date("2026-09-10")

	got := StaleBlockerResolutions(open, current, now)
	if len(got) != 1 {
		t.Fatalf("应补记 1 条解除，got %+v", got)
	}
	if got[0].TaskID != 1 || got[0].Kind != ActivityBlockerResolved || got[0].BlockerKey != "task_overdue:1" {
		t.Fatalf("补记的解除事件不对: %+v", got[0])
	}
	if !got[0].OccurredAt.Equal(now) {
		t.Fatalf("解除没有可计算的发生时刻，应取本次比对时刻: %v", got[0].OccurredAt)
	}
	// 卡点仍在时不补记解除
	if len(StaleBlockerResolutions(open[1:], current, now)) != 0 {
		t.Fatal("仍然成立的卡点不应被补记解除")
	}
	// 没有悬空出现事件时返回空切片
	if got := StaleBlockerResolutions(nil, current, now); len(got) != 0 {
		t.Fatalf("无悬空出现事件时不应补记: %+v", got)
	}
}

// #206：系统级写操作（/system 下的用户管理与系统设置）自动落系统级审计；
// 认证类与个人中心的本人改密、改资料不落。动作名按路由映射，未登记的 /system 写路由退化为通用词。
func TestSystemAuditable(t *testing.T) {
	cases := []struct {
		method, route string
		want          bool
	}{
		{"POST", "/system/users", true},
		{"POST", "/system/users/{userId}/disable", true},
		{"POST", "/system/users/{userId}/enable", true},
		{"POST", "/system/users/{userId}/reset-password", true},
		{"PUT", "/system/users/{userId}/profile", true},
		{"PUT", "/system/users/{userId}/system-admin", true},
		{"PUT", "/system/settings", true},
		{"GET", "/system/users", false},
		{"GET", "/system/audit-logs", false},
		{"POST", "/auth/change-password", false},
		{"POST", "/auth/login", false},
		{"PUT", "/me/profile", false},
		{"POST", "/projects", false},
	}
	for _, c := range cases {
		t.Run(c.method+" "+c.route, func(t *testing.T) {
			if got := SystemAuditable(c.method, c.route); got != c.want {
				t.Fatalf("SystemAuditable(%s, %s) = %v, want %v", c.method, c.route, got, c.want)
			}
		})
	}
}

func TestSystemAuditActionLabel(t *testing.T) {
	cases := []struct {
		method, route, want string
	}{
		{"POST", "/system/users", "新建用户"},
		{"POST", "/system/users/{userId}/disable", "停用用户"},
		{"POST", "/system/users/{userId}/enable", "启用用户"},
		{"POST", "/system/users/{userId}/reset-password", "重置用户密码"},
		{"PUT", "/system/users/{userId}/profile", "修改用户资料"},
		{"PUT", "/system/users/{userId}/system-admin", "设／撤系统管理员"},
		{"PUT", "/system/whatever", "系统设置写操作"},
	}
	for _, c := range cases {
		t.Run(c.route, func(t *testing.T) {
			if got := AuditActionLabel(c.method, c.route); got != c.want {
				t.Fatalf("AuditActionLabel(%s, %s) = %q, want %q", c.method, c.route, got, c.want)
			}
		})
	}
}

