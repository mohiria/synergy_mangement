package domain

import (
	"fmt"
	"sort"
)

// NotifyOkrAssigned 站内通知类型：新增 O/KR 时被指派为 KR 负责人（#125）。
const NotifyOkrAssigned = "okr_assigned"

// OkrNotifyTargets 计算「保存并通知负责人」的通知对象（#125）：
// 本批全部 KR 负责人去重，剔除操作者本人与未指定负责人的行，按 ID 升序稳定输出。
func OkrNotifyTargets(actorID int64, items []OkrBatchItem) []int64 {
	seen := make(map[int64]bool)
	out := []int64{}
	for _, item := range items {
		for _, k := range item.KeyResults {
			if k.OwnerID == nil || *k.OwnerID == actorID || seen[*k.OwnerID] {
				continue
			}
			seen[*k.OwnerID] = true
			out = append(out, *k.OwnerID)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// OkrAssignedContent 通知文案（#125）；在 domain 统一定义，handler 只填充。
func OkrAssignedContent(krDescription string) string {
	return fmt.Sprintf("你被指派为 KR「%s」的负责人，可在项目总览查看并继续拆解任务", krDescription)
}
