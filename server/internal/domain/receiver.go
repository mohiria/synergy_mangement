package domain

import (
	"errors"
	"sort"
	"time"
)

// 接收方与接收记录（词汇表「接收方」「接收记录」；主 PRD §4.1、模块 PRD §8.6）。
// 接收方是任务的按需字段（主 PRD §9.2）：一至多名具体成员，或「所有项目成员」。
// 任务终审通过时按当时事实为每位接收方生成一条待接收项；确认后成为接收记录。

// 接收方范围。
const (
	ReceiverScopeNone    = "none"
	ReceiverScopeMembers = "members"
	ReceiverScopeAll     = "all"
)

// receiverScopeLabels 接收方范围的中文显示文案（变更单差异行与页面共用）。
var receiverScopeLabels = map[string]string{
	ReceiverScopeNone:    "不配置",
	ReceiverScopeMembers: "指定成员",
	ReceiverScopeAll:     "所有项目成员",
}

// ReceiverScopeLabel 接收方范围显示文案（派生字段）；未知取值不回显枚举原文。
func ReceiverScopeLabel(scope string) string {
	if label, ok := receiverScopeLabels[scope]; ok {
		return label
	}
	return "不配置"
}

var (
	ErrReceiverScopeInvalid = errors.New("接收方范围取值非法")
	ErrReceiverEmpty        = errors.New("指定成员为接收方时至少选择一人")
	ErrReceiverNotMember    = errors.New("接收方必须是项目成员")
	ErrReceiptNotMine       = errors.New("只有接收方本人可以确认接收")
	ErrReceiptConfirmed     = errors.New("该接收项已确认")
)

// ReceiptFact 一条待接收项／接收记录（模块 PRD §8.3 词汇表）：未确认为待接收项，已确认为接收记录。
type ReceiptFact struct {
	ID          int64
	TaskID      int64
	TaskName    string
	UserID      int64
	UserName    string
	GeneratedAt time.Time
	ConfirmedAt *time.Time
}

// ValidateReceivers 校验接收方配置：范围合法；指定成员时至少一人且均为项目成员。
// 接收方只查看、下载与确认接收，不拥有审核权（主 PRD §3.1），因此访客也可以是接收方。
func ValidateReceivers(scope string, ids []int64, isMember func(int64) bool) error {
	switch scope {
	case ReceiverScopeNone, ReceiverScopeAll:
		return nil
	case ReceiverScopeMembers:
		if len(ids) == 0 {
			return ErrReceiverEmpty
		}
		for _, id := range ids {
			if !isMember(id) {
				return ErrReceiverNotMember
			}
		}
		return nil
	default:
		return ErrReceiverScopeInvalid
	}
}

// CanConfigureReceivers 判定能否配置接收方；与输入／输出配置同口径（主 PRD §7.5、§3.4）。
func CanConfigureReceivers(a Actor, userID int64, t TaskFacts) bool {
	return CanConfigureInputs(a, userID, t)
}

// ReceiptTargets 终审通过时展开的接收方名单（模块 PRD §8.6）：
// 「所有项目成员」按当时项目成员逐人生成，指定成员按配置生成，未配置则不生成；去重并按 ID 升序稳定输出。
func ReceiptTargets(scope string, receiverIDs, memberIDs []int64) []int64 {
	var src []int64
	switch scope {
	case ReceiverScopeAll:
		src = memberIDs
	case ReceiverScopeMembers:
		src = receiverIDs
	}
	seen := make(map[int64]bool, len(src))
	out := make([]int64, 0, len(src))
	for _, id := range src {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// CanConfirmReceipt 判定能否确认接收：仅接收方本人、且尚未确认（接收方无审核权，不提供退回）。
func CanConfirmReceipt(a Actor, userID int64, r ReceiptFact) error {
	// 隐式访客不落成员表，不可能被指定为接收方（#111）：即便手上有 ID 也当作不是本人的。
	if a.Implicit || r.UserID != userID {
		return ErrReceiptNotMine
	}
	if r.ConfirmedAt != nil {
		return ErrReceiptConfirmed
	}
	return nil
}
