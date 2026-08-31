package api

import (
	"context"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// 写路径装饰器（R8、R9）：项目内的每一次成功写请求，统一落一条项目审计，
// 并比对写前写后的派生卡点集合补记「卡点出现／解除」。
//
// 这两件事此前挂在各个 handler 的尾部：新写一个端点就得记得手工挂一行，
// 漏挂没有任何信号——§10.4 要求的人员、关系、成果、成果包变化因此全无痕，
// 卡点解除也只在六个挂了快照的写路径上才记得住。放到中间件里之后，
// 新增写路径默认就被覆盖，最差也是落一条动作名笼统的审计。

// projectIDPattern 从 /projects/{id}/... 里取项目 ID；装饰器只作用于项目域。
var projectIDPattern = regexp.MustCompile(`^/projects/(\d+)(/|$)`)

// statusRecorder 记住响应状态码：只有 2xx 才算写成功，失败的请求不留痕、不比对卡点。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusRecorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// writePathMiddleware 见文件头说明。
func (s *Server) writePathMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		projectID, rest, ok := projectScope(r.URL.Path)
		if !ok || !domain.Auditable(r.Method, routeTemplate(rest)) {
			next.ServeHTTP(w, r)
			return
		}
		// 请求内记忆化：写前快照与写后派生看到的是不同事实，中间必须清一次缓存；
		// 清完之后 handler 自己算的那一份就能被写后比对复用，省掉整套重复派生（P1）。
		ctx, cache := withBlockerCache(r.Context())
		r = r.WithContext(ctx)
		// 写前快照：取不到就按「这次不比对」处理，不影响业务动作。
		before := s.blockerSnapshot(ctx, projectID)
		cache.reset()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status < 200 || rec.status >= 300 {
			return
		}
		// 请求已经完成，用后台上下文收尾：留痕失败不回滚业务动作，也不该被客户端断连打断。
		after := context.WithoutCancel(r.Context())
		s.recordAudit(after, projectID, r)
		s.recordBlockerChanges(after, projectID, before)
	})
}

// projectScope 解析项目 ID 与其后的路径片段；不是项目域请求时返回 ok=false。
func projectScope(path string) (int64, string, bool) {
	i := strings.Index(path, "/projects/")
	if i < 0 {
		return 0, "", false
	}
	rest := path[i:]
	m := projectIDPattern.FindStringSubmatch(rest)
	if m == nil {
		return 0, "", false
	}
	id, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, "", false
	}
	return id, rest, true
}

// routeTemplate 把具体路径还原成契约里的路由模板（数字段落换回占位符），
// 让动作名的映射按路由而不是按具体 ID 匹配。
func routeTemplate(path string) string {
	segs := strings.Split(path, "/")
	names := map[string]string{
		"projects":           "{projectId}",
		"tasks":              "{taskId}",
		"members":            "{userId}",
		"objectives":         "{objectiveId}",
		"key-results":        "{keyResultId}",
		"edges":              "{edgeId}",
		"deliverables":       "{deliverableId}",
		"field-changes":      "{changeId}",
		"completion-reviews": "{reviewId}",
		"input-requests":     "{requestId}",
		"task-invites":       "{inviteId}",
		"packages":           "{packageId}",
	}
	for i, seg := range segs {
		if seg == "" || !isNumeric(seg) {
			continue
		}
		placeholder := "{id}"
		if i > 0 {
			if v, ok := names[segs[i-1]]; ok {
				placeholder = v
			}
		}
		segs[i] = placeholder
	}
	return strings.Join(segs, "/")
}

func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

// recordAudit 落一条项目审计。审计是留痕不是业务前置条件：写失败只记日志。
func (s *Server) recordAudit(ctx context.Context, projectID int64, r *http.Request) {
	route := routeTemplate(mustRest(r.URL.Path))
	actor := currentUser(r)
	objectType, objectID := auditObject(route, mustRest(r.URL.Path))
	if err := s.q.CreateAuditLog(ctx, store.CreateAuditLogParams{
		ProjectID:  projectID,
		ActorID:    toPgInt8(&actor.ID),
		Action:     domain.AuditActionLabel(r.Method, route),
		Method:     r.Method,
		Route:      route,
		ObjectType: objectType,
		ObjectID:   toPgInt8(objectID),
	}); err != nil {
		log.Printf("record audit failed: project=%d route=%s err=%v", projectID, route, err)
	}
}

func mustRest(path string) string {
	_, rest, ok := projectScope(path)
	if !ok {
		return path
	}
	return rest
}

// auditObject 取本次写操作最直接的对象类型与 ID（路径里项目之后的第一个带 ID 的资源）。
func auditObject(route, path string) (string, *int64) {
	rSegs := strings.Split(route, "/")
	pSegs := strings.Split(path, "/")
	if len(rSegs) != len(pSegs) {
		return "", nil
	}
	// 跳过 /projects/{projectId}，从第 3 段起找第一个占位符。
	for i := 3; i < len(rSegs); i++ {
		if !strings.HasPrefix(rSegs[i], "{") {
			continue
		}
		id, err := strconv.ParseInt(pSegs[i], 10, 64)
		if err != nil {
			return "", nil
		}
		return rSegs[i-1], &id
	}
	if len(rSegs) > 2 {
		return rSegs[len(rSegs)-1], nil
	}
	return "project", nil
}

// ListAuditLogs 项目操作审计（§10.4）：只对项目管理员开放——
// 审计要回答「谁把这条边删了」「谁改了接收方」，属于管理视角而不是日常协作视角。
func (s *Server) ListAuditLogs(w http.ResponseWriter, r *http.Request, projectId int64, params ListAuditLogsParams) {
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	if !domain.CanEditProject(projectActor(uid, proj.OwnerID, proj.MyRole, proj.Visibility)) {
		writeForbidden(w)
		return
	}
	limit := int32(100)
	if params.Limit != nil {
		limit = int32(*params.Limit)
	}
	rows, err := s.q.ListAuditLogsByProject(r.Context(), store.ListAuditLogsByProjectParams{ProjectID: projectId, Limit: limit})
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	out := make([]AuditLog, 0, len(rows))
	for _, a := range rows {
		item := AuditLog{
			Id:         a.ID,
			Action:     a.Action,
			Method:     a.Method,
			Route:      a.Route,
			ObjectType: optString(a.ObjectType),
			ActorName:  fromPgText(a.ActorName),
			OccurredAt: a.CreatedAt.Time,
		}
		if a.ObjectID.Valid {
			item.ObjectId = &a.ObjectID.Int64
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, out)
}

// removeObject 删除对象存储里的一个对象；失败排进补偿队列由 ticker 重试（E3）。
// §5.3 说的「旧文件永久删除、不保留副本」要在存储层成立，就不能把删除失败静默吞掉：
// 库里的行没了、对象还在桶里，从合规角度是真问题。
func (s *Server) removeObject(ctx context.Context, key string) {
	if key == "" {
		return
	}
	if err := s.files.Remove(ctx, key); err == nil {
		return
	} else {
		log.Printf("remove object failed, queued for retry: key=%s err=%v", key, err)
		if qerr := s.q.EnqueueObjectDeletion(ctx, store.EnqueueObjectDeletionParams{
			ObjectKey: key, LastError: err.Error(),
		}); qerr != nil {
			log.Printf("enqueue object deletion failed: key=%s err=%v", key, qerr)
		}
	}
}
