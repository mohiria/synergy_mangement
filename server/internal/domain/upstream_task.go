package domain

import "fmt"

// #178 裁决（裁决 8）：上游任务不存在时，替指定成员创建上游任务，由其完成——
// 关系回归任务与任务之间，输入请求机制整体退场。
// 新任务走 #162 口径直接入池（入池动态 + 通知所属 KR 负责人），
// 另通知新任务负责人（无认领确认环节）；系统自动建立「新上游任务 → 当前任务」的必要输入边。

// NotifyUpstreamTaskAssigned 站内通知类型：被指定为新建上游任务的负责人。
const NotifyUpstreamTaskAssigned = "upstream_task_assigned"

// UpstreamTaskNotifyTarget 计算要通知的新任务负责人；本人替自己创建时不另发。
func UpstreamTaskNotifyTarget(creatorID, ownerID int64) *int64 {
	if ownerID == creatorID {
		return nil
	}
	id := ownerID
	return &id
}

// UpstreamTaskNotification 组装通知正文；在 domain 统一定义，handler 只填充。
func UpstreamTaskNotification(creatorName, upstreamName, downstreamName string) string {
	return fmt.Sprintf("%s 为任务「%s」创建了上游任务「%s」并指定你为负责人",
		creatorName, downstreamName, upstreamName)
}
