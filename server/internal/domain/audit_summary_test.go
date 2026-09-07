package domain

import (
	"strings"
	"testing"
)

// #215：系统级审计摘要——记脱敏后的前后值，密码类字段只记「已更新」、值永不出现，
// 未登记字段丢弃，有写前快照且无差异时写「无字段变化」。
func TestSystemAuditSummary(t *testing.T) {
	cases := []struct {
		name   string
		before map[string]string
		after  map[string]string
		want   string
	}{
		{"基本信息只记变化项、空值显示（空）",
			map[string]string{"systemName": "协同管理工具", "subtitle": "副", "loginHint": "x", "baseUrl": ""},
			map[string]string{"systemName": "新名称", "subtitle": "副", "loginHint": "x", "baseUrl": "http://203.0.113.10"},
			"系统名称：协同管理工具 → 新名称；访问地址：（空） → http://203.0.113.10"},
		{"新建用户无写前快照：按固定顺序列全部字段，密码只记已更新",
			nil,
			map[string]string{"password": "init-pass-1", "email": "carol@example.com", "username": "carol", "displayName": "王五"},
			"账号：carol；显示名：王五；邮箱：carol@example.com；密码：已更新"},
		{"重置密码只有密码字段", nil, map[string]string{"password": "s3cret"}, "密码：已更新"},
		{"未登记字段丢弃", nil, map[string]string{"foo": "bar"}, ""},
		{"有快照但无差异", map[string]string{"displayName": "王五", "email": "c@x.co"}, map[string]string{"displayName": "王五", "email": "c@x.co"}, "无字段变化"},
		{"布尔与邮件事件开关",
			map[string]string{"isSystemAdmin": "否"},
			map[string]string{"isSystemAdmin": "是"},
			"系统管理员：否 → 是"},
		{"邮件通知事件键用通知事件文案",
			map[string]string{"enabled": "是", "event:" + NotifyTaskInvite: "是"},
			map[string]string{"enabled": "否", "event:" + NotifyTaskInvite: "否"},
			"邮件通知总开关：是 → 否；" + MailNotifyKindLabel(NotifyTaskInvite) + "：是 → 否"},
		{"邮件通道换密码且改主机",
			map[string]string{"host": "old.example.com", "port": "25", "encryption": "none", "username": "", "fromName": "", "fromAddress": "a@b.co"},
			map[string]string{"host": "smtp.example.com", "port": "587", "encryption": "starttls", "username": "bot", "fromName": "", "fromAddress": "a@b.co", "password": "pw"},
			"SMTP 主机：old.example.com → smtp.example.com；端口：25 → 587；加密方式：none → starttls；账号：（空） → bot；密码：已更新"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SystemAuditSummary(tc.before, tc.after)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			if v, ok := tc.after["password"]; ok && strings.Contains(got, v) {
				t.Fatalf("密码值不应出现在摘要里: %q", got)
			}
		})
	}
}

// 请求体展平：布尔记「是／否」，数字原样，邮件事件数组展开成 event:<kind>，其它类型丢弃。
func TestSystemAuditFieldsFromJSON(t *testing.T) {
	got := SystemAuditFieldsFromJSON(map[string]any{
		"host": "smtp.example.com", "port": float64(587), "isSystemAdmin": true, "enabled": false,
		"events": []any{map[string]any{"kind": NotifyTaskInvite, "enabled": true}, map[string]any{"kind": "x", "enabled": false}},
		"nested": map[string]any{"a": 1}, "nothing": nil,
	})
	want := map[string]string{
		"host": "smtp.example.com", "port": "587", "isSystemAdmin": "是", "enabled": "否",
		SystemAuditEventPrefix + NotifyTaskInvite: "是", SystemAuditEventPrefix + "x": "否",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("key %s: got %q, want %q (all: %v)", k, got[k], v, got)
		}
	}
}
