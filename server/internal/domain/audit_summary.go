package domain

import (
	"strconv"
	"strings"
)

// 系统级审计摘要（#215）：记脱敏后的前后值，供系统设置页「操作记录」展示变更内容。

// systemAuditFields 摘要登记字段的固定顺序与显示文案；不在表里的键一律丢弃。
var systemAuditFields = []struct{ key, label string }{
	{"systemName", "系统名称"}, {"subtitle", "副标题"}, {"loginHint", "登录页提示语"}, {"baseUrl", "访问地址"},
	{"host", "SMTP 主机"}, {"port", "端口"}, {"encryption", "加密方式"}, {"username", "账号"},
	{"fromName", "发件人显示名"}, {"fromAddress", "发件人地址"},
	{"displayName", "显示名"}, {"email", "邮箱"}, {"isSystemAdmin", "系统管理员"},
	{"enabled", "邮件通知总开关"},
}

// SystemAuditSecretField 密码类字段：摘要只记「已更新」，值永不出现。
const SystemAuditSecretField = "password"

// SystemAuditEventPrefix 邮件通知事件开关在 before/after 里的键前缀，后接通知事件 kind。
const SystemAuditEventPrefix = "event:"

// SystemAuditSummary 由写前、写后两组字段拼出摘要：有写前快照只记变化项「标签：旧 → 新」，
// 无写前快照（新建）记全部登记字段「标签：新」；空值显示「（空）」；密码只记「已更新」；
// 有写前快照且无差异返回「无字段变化」。
func SystemAuditSummary(before, after map[string]string) string {
	var parts []string
	add := func(key, label string) {
		newVal, ok := after[key]
		if !ok {
			return
		}
		if before == nil {
			parts = append(parts, label+"："+auditValue(newVal))
			return
		}
		oldVal := before[key]
		if oldVal == newVal {
			return
		}
		parts = append(parts, label+"："+auditValue(oldVal)+" → "+auditValue(newVal))
	}
	for _, f := range systemAuditFields {
		add(f.key, f.label)
	}
	for _, kind := range MailNotifyKinds {
		add(SystemAuditEventPrefix+kind, MailNotifyKindLabel(kind))
	}
	if v, ok := after[SystemAuditSecretField]; ok && v != "" {
		parts = append(parts, "密码：已更新")
	}
	if len(parts) == 0 {
		if before != nil {
			return "无字段变化"
		}
		return ""
	}
	return strings.Join(parts, "；")
}

func auditValue(v string) string {
	if v == "" {
		return "（空）"
	}
	return v
}

// SystemAuditFieldsFromJSON 把已解析的 JSON 请求体展平成摘要字段。
func SystemAuditFieldsFromJSON(body map[string]any) map[string]string {
	out := make(map[string]string, len(body))
	for k, v := range body {
		if k == "events" {
			items, _ := v.([]any)
			for _, it := range items {
				ev, _ := it.(map[string]any)
				kind, _ := ev["kind"].(string)
				if kind == "" {
					continue
				}
				if s, ok := auditScalar(ev["enabled"]); ok {
					out[SystemAuditEventPrefix+kind] = s
				}
			}
			continue
		}
		if s, ok := auditScalar(v); ok {
			out[k] = s
		}
	}
	return out
}

func auditScalar(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case bool:
		if x {
			return "是", true
		}
		return "否", true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	}
	return "", false
}
