package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"synergy/server/internal/domain"
)

// 系统级审计摘要的取材（#215）：写前快照按路由查库，写后字段来自请求体；拼接规则在 domain。

// systemAuditSnapshot 按路由取库里的当前字段（写前调用是「前」、写后调用是「后」——后值因此是归一落库的值，
// 不是请求原文）。新建、重置密码、停用启用、Logo、测试邮件没有可比对的字段，返回 nil；
// 查库失败也按 nil 处理——摘要退化成「标签：新值」或空，不影响业务动作。
func (s *Server) systemAuditSnapshot(ctx context.Context, rest string) map[string]string {
	route := routeTemplate(rest)
	switch route {
	case "/system/settings":
		ss, err := s.q.GetSystemSettings(ctx)
		if err != nil {
			return nil
		}
		return map[string]string{"systemName": ss.SystemName, "subtitle": ss.Subtitle, "loginHint": ss.LoginHint, "baseUrl": ss.BaseUrl}
	case "/system/mail-settings":
		ms, err := s.q.GetMailSettings(ctx)
		if err != nil {
			return nil
		}
		return map[string]string{
			"host": ms.Host, "port": strconv.Itoa(int(ms.Port)), "encryption": ms.Encryption, "username": ms.Username,
			"fromName": ms.FromName, "fromAddress": ms.FromAddress,
		}
	case "/system/mail-notify":
		ms, err := s.q.GetMailSettings(ctx)
		if err != nil {
			return nil
		}
		sw := systemSwitches(ms)
		out := map[string]string{"enabled": auditBool(sw.Enabled)}
		for _, kind := range domain.MailNotifyKinds {
			out[domain.SystemAuditEventPrefix+kind] = auditBool(sw.Events[kind])
		}
		return out
	case "/system/users/{userId}/profile", "/system/users/{userId}/system-admin":
		_, id := systemAuditObject(route, rest)
		if id == nil {
			return nil
		}
		u, err := s.q.GetUserByID(ctx, *id)
		if err != nil {
			return nil
		}
		return map[string]string{"displayName": u.DisplayName, "email": u.Email, "isSystemAdmin": auditBool(u.IsSystemAdmin)}
	}
	return nil
}

// systemAuditAfter 写后字段：有库快照的路由用库快照，请求体只补密码是否更新；其余（新建用户、重置密码）用请求体。
func (s *Server) systemAuditAfter(ctx context.Context, rest string, body map[string]string) map[string]string {
	db := s.systemAuditSnapshot(ctx, rest)
	if db == nil {
		return body
	}
	if v, ok := body[domain.SystemAuditSecretField]; ok {
		db[domain.SystemAuditSecretField] = v
	}
	return db
}

// systemAuditBody 从 JSON 请求体取字段；读完把 body 放回去给 handler。非 JSON（Logo 上传）不读。
func systemAuditBody(r *http.Request) map[string]string {
	if r.Body == nil || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return nil
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	var m map[string]any
	if json.Unmarshal(body, &m) != nil {
		return nil
	}
	return domain.SystemAuditFieldsFromJSON(m)
}

func auditBool(b bool) string {
	if b {
		return "是"
	}
	return "否"
}
