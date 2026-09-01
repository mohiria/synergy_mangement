package domain

import (
	"strings"
	"time"
)

// 项目域审计与卡点解除补偿（§10.4；R8、R9）。
//
// 留痕之所以此前大面积漏记，是因为它挂在每个 handler 的尾部——写一个新端点就得记得
// 手工挂一行，漏了没人会发现。这里把「哪些请求要留痕」「留痕叫什么」变成可判定的规则，
// 由写路径装饰器统一执行：新增写路径默认就被覆盖，最差也是落一条通用动作名。

// auditActions 路由到动作名的映射；键是「方法 路由模板」。
var auditActions = map[string]string{
	"POST /projects":                                                 "创建项目",
	"PATCH /projects/{projectId}":                                    "修改项目信息",
	"POST /projects/{projectId}/members":                             "新增项目成员",
	"PUT /projects/{projectId}/members/{userId}":                     "调整成员角色",
	"DELETE /projects/{projectId}/members/{userId}":                  "移出项目成员",
	"PUT /projects/{projectId}/settings":                             "修改项目规则设置",
	"POST /projects/{projectId}/objectives":                          "创建 O／KR",
	"PATCH /projects/{projectId}/objectives/{objectiveId}":           "编辑 O",
	"DELETE /projects/{projectId}/objectives/{objectiveId}":          "删除 O",
	"PATCH /projects/{projectId}/key-results/{keyResultId}":          "编辑 KR",
	"DELETE /projects/{projectId}/key-results/{keyResultId}":         "删除 KR",
	"POST /projects/{projectId}/tasks":                               "创建任务",
	"POST /projects/{projectId}/tasks/{taskId}/update-status":        "流转任务状态",
	"PUT /projects/{projectId}/tasks/{taskId}/progress":              "更新任务进度",
	"POST /projects/{projectId}/tasks/{taskId}/cancellation":         "发起任务关闭申请",
	"POST /projects/{projectId}/tasks/{taskId}/field-changes":        "提交关键字段修改",
	"POST /projects/{projectId}/tasks/{taskId}/inputs":               "配置任务输入",
	"POST /projects/{projectId}/tasks/{taskId}/member-inputs":        "指定输入对接人",
	"POST /projects/{projectId}/tasks/{taskId}/deliverables":         "配置交付物项",
	"PUT /projects/{projectId}/tasks/{taskId}/reviewers":             "配置成果审核人",
	"PUT /projects/{projectId}/tasks/{taskId}/receivers":             "配置接收方",
	"PUT /projects/{projectId}/tasks/{taskId}/participants":          "配置参与人",
	"POST /projects/{projectId}/tasks/{taskId}/confirm-receipt":      "确认接收",
	"POST /projects/{projectId}/tasks/{taskId}/completion-reviews":   "提交完成申请",
	"POST /projects/{projectId}/tasks/{taskId}/result-update":        "发起成果更新",
	"POST /projects/{projectId}/tasks/{taskId}/files":                "上传过程文件／外部材料",
	"DELETE /projects/{projectId}/tasks/{taskId}/files/{fileId}":     "删除过程文件／外部材料",
	"DELETE /projects/{projectId}/edges/{edgeId}":                    "解除交付物边",
	"POST /projects/{projectId}/task-invites":                        "发出任务创建邀请",
	"POST /projects/{projectId}/import":                              "导入 O／KR／任务",
	"POST /projects/{projectId}/reminders":                           "一键提醒",
}

// AuditActionLabel 一次写请求对应的动作名（派生字段）。
// 未登记的路由退化为通用词——宁可动作名笼统，也不让新增写路径完全没有留痕。
func AuditActionLabel(method, route string) string {
	if label, ok := auditActions[strings.ToUpper(method)+" "+route]; ok {
		return label
	}
	return "项目内写操作"
}

// Auditable 判定一次请求是否进项目审计：项目域内的写请求才记。
// 读请求不改变事实，认证类请求不属于任何项目。
func Auditable(method, route string) bool {
	switch strings.ToUpper(method) {
	case "POST", "PUT", "PATCH", "DELETE":
	default:
		return false
	}
	return strings.HasPrefix(route, "/projects/{projectId}")
}

// OpenBlockerFact 一条「已记出现、尚未记解除」的卡点动态（留痕侧事实）。
type OpenBlockerFact struct {
	TaskID  int64
	Key     string
	Summary string
}

// StaleBlockerResolutions 补记悬空的「卡点解除」（R9）。
// 写触发的 diff 只在挂了快照的写路径上生效，时间型卡点的消失更是没有任何写操作可依附，
// 于是动态流里会留下「出现」却永无「解除」的悬空条目。ticker 没有变更前快照，
// 只能拿留痕里仍处于「出现未解除」的键与当前仍成立的卡点比对，差集即为要补记的解除。
// 解除没有可计算的发生时刻，取本次比对时刻。
func StaleBlockerResolutions(open []OpenBlockerFact, current []Blocker, now time.Time) []TaskActivity {
	live := make(map[string]struct{}, len(current))
	for _, b := range current {
		live[b.Key] = struct{}{}
	}
	out := []TaskActivity{}
	for _, o := range open {
		if _, still := live[o.Key]; still {
			continue
		}
		out = append(out, TaskActivity{
			TaskID:     o.TaskID,
			Kind:       ActivityBlockerResolved,
			Summary:    ActivityKindLabel(ActivityBlockerResolved) + "：" + strings.TrimPrefix(o.Summary, ActivityKindLabel(ActivityBlockerOpened)+"："),
			OccurredAt: now,
			BlockerKey: o.Key,
		})
	}
	return out
}
