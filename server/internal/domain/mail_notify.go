package domain

// 站内通知同步邮件（模块 PRD §10.3；#213）：五个事件与现有站内通知 kind 一一对应。

// MailNotifyKinds 可同步邮件的站内通知 kind，顺序即界面顺序。
var MailNotifyKinds = []string{
	NotifyDiscussionMention, NotifyDiscussionOwner, NotifyTaskInvite, NotifyUpstreamTaskAssigned, NotifyBlockerRemind,
}

var mailNotifyKindLabels = map[string]string{
	NotifyDiscussionMention:    "讨论区被 @",
	NotifyDiscussionOwner:      "讨论区任务负责人收到留言",
	NotifyTaskInvite:           "任务创建邀请",
	NotifyUpstreamTaskAssigned: "被指定为上游任务负责人",
	NotifyBlockerRemind:        "卡点一键提醒",
}

// MailNotifyKindLabel 事件显示文案。
func MailNotifyKindLabel(kind string) string {
	if l, ok := mailNotifyKindLabels[kind]; ok {
		return l
	}
	return kind
}

// MailSwitches 一组开关：总开关 + 按 kind 的事件开关（缺省视为开）。
type MailSwitches struct {
	Enabled bool
	Events  map[string]bool
}

// AllOn 全开（个人无偏好行时的缺省）。
func AllOn() MailSwitches {
	m := make(map[string]bool, len(MailNotifyKinds))
	for _, k := range MailNotifyKinds {
		m[k] = true
	}
	return MailSwitches{Enabled: true, Events: m}
}

// ShouldMailNotification 站内通知是否同步成邮件（#213）：只对五类事件；系统总开关、系统事件开关、
// 个人总开关、个人事件开关全部为开且收件人未停用才发。事件开关缺省（map 里没有）视为开。
func ShouldMailNotification(kind string, system, user MailSwitches, recipientDisabled bool) bool {
	if _, known := mailNotifyKindLabels[kind]; !known || recipientDisabled {
		return false
	}
	return system.Enabled && switchOn(system, kind) && user.Enabled && switchOn(user, kind)
}

func switchOn(sw MailSwitches, kind string) bool {
	on, ok := sw.Events[kind]
	return !ok || on
}
