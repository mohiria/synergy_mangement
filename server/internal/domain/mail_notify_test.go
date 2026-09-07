package domain

import "testing"

// #213：系统总开关 × 系统事件开关 × 个人总开关 × 个人事件开关 × 停用状态，五项全部成立才发；
// 不在五类里的 kind 不发；缺省（无个人偏好）视为全开。
func TestShouldMailNotification(t *testing.T) {
	on := func() MailSwitches { return AllOn() }
	off := func(kind string) MailSwitches {
		m := AllOn()
		m.Events[kind] = false
		return m
	}
	masterOff := func() MailSwitches {
		m := AllOn()
		m.Enabled = false
		return m
	}
	cases := []struct {
		name     string
		kind     string
		system   MailSwitches
		user     MailSwitches
		disabled bool
		want     bool
	}{
		{"全开发送", NotifyDiscussionMention, on(), on(), false, true},
		{"系统总开关关", NotifyDiscussionMention, masterOff(), on(), false, false},
		{"系统事件关", NotifyTaskInvite, off(NotifyTaskInvite), on(), false, false},
		{"个人总开关关", NotifyBlockerRemind, on(), masterOff(), false, false},
		{"个人事件关", NotifyUpstreamTaskAssigned, on(), off(NotifyUpstreamTaskAssigned), false, false},
		{"收件人已停用", NotifyDiscussionOwner, on(), on(), true, false},
		{"系统关别的事件不影响本事件", NotifyDiscussionOwner, off(NotifyTaskInvite), on(), false, true},
		{"未知 kind 不发", "something_else", on(), on(), false, false},
		{"个人偏好缺省全开", NotifyTaskInvite, on(), MailSwitches{Enabled: true}, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ShouldMailNotification(c.kind, c.system, c.user, c.disabled); got != c.want {
				t.Fatalf("ShouldMailNotification(%s) = %v, want %v", c.kind, got, c.want)
			}
		})
	}
}
