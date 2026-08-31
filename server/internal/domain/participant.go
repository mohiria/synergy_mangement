package domain

import "errors"

// 参与人（词汇表「参与人」；主 PRD §4.1、§9.2）：任务上除负责人以外的协作者名单。
//
// 只作展示与检索——不产生待办、不进任何审批链、不影响权限、不参与我的工作归组与排序。
// 这条口径不是靠判定里额外写 if 来保证的，而是靠参与人根本不进 TaskFacts、MyWorkFacts、
// BlockerFacts 这些派生输入；participant_test.go 里对这几个结构的字段集下了断言看住它。
// 参与人也不属关键字段（§5.2.B），改名单直接生效，只由写路径装饰器留一条项目审计。

var (
	ErrParticipantNotMember = errors.New("参与人必须是项目成员")
	ErrParticipantIsOwner   = errors.New("任务负责人已单列，不必再选为参与人")
)

// ValidateParticipants 校验参与人名单：须为项目成员，且不含任务负责人本人。
// 空名单合法，表示不配置或清空。访客可以是参与人——参与人不带任何写权限，
// 与「访客可确认接收」同理（Q4／Q8 裁决）。
func ValidateParticipants(ownerID int64, userIDs []int64, isMember func(int64) bool) error {
	for _, id := range userIDs {
		if id == ownerID {
			return ErrParticipantIsOwner
		}
		if !isMember(id) {
			return ErrParticipantNotMember
		}
	}
	return nil
}

// NormalizeParticipants 名单归一：去重并保持选择顺序。
// 同一人重复勾选是界面操作噪声，不是业务错误，按一人记。
func NormalizeParticipants(userIDs []int64) []int64 {
	out := make([]int64, 0, len(userIDs))
	seen := make(map[int64]bool, len(userIDs))
	for _, id := range userIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// CanManageParticipants 判定能否配置参与人：负责人／创建人／可编辑项目者。
// 与交付物项配置同口径，但审核期间不锁——参与人不进审批链，改名单不会动到审批中的事实；
// 终态任务不再变更事实，因此已完成与已取消不可配置。
func CanManageParticipants(a Actor, userID int64, t TaskFacts) bool {
	if !CanWriteProject(a) || t.Status == TaskCompleted || t.Status == TaskCancelled {
		return false
	}
	return userID == t.OwnerID || userID == t.CreatorID || CanEditProject(a)
}
