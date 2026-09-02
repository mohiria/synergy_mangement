package domain

import (
	"errors"
	"fmt"
	"strings"
)

// 任务创建邀请状态（词汇表「任务创建邀请」）。
const (
	TaskInvitePending   = "pending"
	TaskInviteCompleted = "completed"
	TaskInviteRevoked   = "revoked"
)

var (
	ErrInviteesEmpty      = errors.New("请至少选择一名受邀成员")
	ErrInviteSelf         = errors.New("不能邀请自己")
	ErrInviteeNotEligible = errors.New("受邀成员必须是非只读的项目成员")
	ErrInviteNotPending   = errors.New("邀请不在待处理状态")
	ErrInviteNotInvitee   = errors.New("只有受邀成员本人可以通过该邀请提交任务")
	ErrInviteKrMismatch   = errors.New("本批任务中至少要有一项属于邀请指定的 KR")
)

// NotifyTaskInvite 站内通知类型：任务创建邀请已发出（AC-03、§7.3）。
const NotifyTaskInvite = "task_invite"

// TaskInviteNotification 组装邀请通知正文（AC-03、§7.3）：带 KR 编号与名称，以及邀请说明。
// 受邀人不主动打开「我的工作」就不会知道自己被邀请拆任务，这条通知是 AC-03 闭环的一环。
func TaskInviteNotification(inviterName, krCode, krDescription, note string) string {
	subject := strings.TrimSpace(inviterName) + "邀请你"
	if strings.TrimSpace(inviterName) == "" {
		subject = "你被邀请"
	}
	content := fmt.Sprintf("%s在 %s「%s」下创建任务", subject, krCode, krDescription)
	if n := strings.TrimSpace(note); n != "" {
		content += "：" + n
	}
	return content
}

// CanInviteForKr 判定能否为某 KR 发出任务创建邀请（裁决 12，#183：KR 无负责人，
// 收归项目管理员与项目负责人——与任务创建权同口径）。
func CanInviteForKr(a Actor) bool {
	return CanEditProject(a)
}

// ValidateInvitees 校验受邀成员：非只读项目成员、不能邀请自己（原型 inviteMemberCandidates）。
func ValidateInvitees(inviterID int64, inviteeIDs []int64, roleOf func(int64) string) error {
	if len(inviteeIDs) == 0 {
		return ErrInviteesEmpty
	}
	for _, id := range inviteeIDs {
		if id == inviterID {
			return ErrInviteSelf
		}
		if role := roleOf(id); role != RoleAdmin && role != RoleMember {
			return ErrInviteeNotEligible
		}
	}
	return nil
}

// CanRevokeInvite 判定能否撤回邀请：待处理且动作人为邀请人／项目管理员／项目负责人。
func CanRevokeInvite(a Actor, userID, inviterID int64, state string) bool {
	if state != TaskInvitePending {
		return false
	}
	return userID == inviterID || CanEditProject(a)
}

// FulfillInvite 校验通过邀请提交任务：仅受邀人本人、邀请待处理、本批至少一项属于指定 KR（AC-03）。
func FulfillInvite(state string, inviteeID, actorID, inviteKrID int64, itemKrIDs []int64) error {
	if state != TaskInvitePending {
		return ErrInviteNotPending
	}
	if actorID != inviteeID {
		return ErrInviteNotInvitee
	}
	for _, krID := range itemKrIDs {
		if krID == inviteKrID {
			return nil
		}
	}
	return ErrInviteKrMismatch
}

// CanHandleInvite 判定当前用户能否响应邀请（派生动作标志）。
func CanHandleInvite(userID, inviteeID int64, state string) bool {
	return state == TaskInvitePending && userID == inviteeID
}
