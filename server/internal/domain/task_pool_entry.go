package domain

import "fmt"

// 裁决 #162：取消入池审批——任务创建与导入直接入池。
// 补偿机制：入池写任务动态，并站内通知所属 KR 负责人（本人创建不另发）。

// NotifyTaskPoolEntered 站内通知类型：新任务入池。
const NotifyTaskPoolEntered = "task_pool_entered"

// PoolEntryNotifyTarget 计算任务入池要通知的 KR 负责人：
// KR 未指定负责人或创建人就是 KR 负责人本人时不发，返回 nil。
func PoolEntryNotifyTarget(creatorID int64, krOwnerID *int64) *int64 {
	if krOwnerID == nil || *krOwnerID == creatorID {
		return nil
	}
	id := *krOwnerID
	return &id
}

// PoolEntryNotification 组装入池通知正文；在 domain 统一定义，handler 只填充。
func PoolEntryNotification(creatorName, taskName, krCode, krDescription string) string {
	return fmt.Sprintf("%s创建的任务「%s」已进入 %s「%s」任务池，初始状态未开始",
		creatorName, taskName, krCode, krDescription)
}
