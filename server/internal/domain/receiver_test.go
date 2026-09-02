package domain

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// 归档等列表的接收方展示文案（#171，#17/#18 反馈）：指定成员只列名单、无「指定成员：」
// 前缀；全员统一「项目全体成员」；未配置不显示口径文字（空串）。
func TestReceiverDisplay(t *testing.T) {
	cases := []struct {
		name  string
		scope string
		names []string
		want  string
	}{
		{"指定成员只列名单", ReceiverScopeMembers, []string{"张三", "李四"}, "张三、李四"},
		{"全员统一文案", ReceiverScopeAll, nil, "项目全体成员"},
		{"未配置不显示口径文字", ReceiverScopeNone, nil, ""},
		{"未知取值按未配置", "weird", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReceiverDisplay(tc.scope, tc.names); got != tc.want {
				t.Fatalf("ReceiverDisplay(%q, %v) = %q, want %q", tc.scope, tc.names, got, tc.want)
			}
		})
	}
	// 抽屉等共用 ReceiverScopeLabel 处同步（#171）：全员文案统一「项目全体成员」。
	if got := ReceiverScopeLabel(ReceiverScopeAll); got != "项目全体成员" {
		t.Fatalf("ReceiverScopeLabel(all) = %q, want 项目全体成员", got)
	}
}

// 接收方配置校验（模块 PRD §8.6；主 PRD §9.2 按需字段）。
func TestValidateReceivers(t *testing.T) {
	members := map[int64]bool{1: true, 2: true, 3: true}
	isMember := func(id int64) bool { return members[id] }
	cases := []struct {
		name  string
		scope string
		ids   []int64
		want  error
	}{
		{"不配置接收方", ReceiverScopeNone, nil, nil},
		{"所有项目成员不需要名单", ReceiverScopeAll, nil, nil},
		{"指定成员", ReceiverScopeMembers, []int64{1, 3}, nil},
		{"指定成员为空", ReceiverScopeMembers, nil, ErrReceiverEmpty},
		{"接收方不是项目成员", ReceiverScopeMembers, []int64{1, 9}, ErrReceiverNotMember},
		{"范围非法", "everyone", []int64{1}, ErrReceiverScopeInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := ValidateReceivers(c.scope, c.ids, isMember); !errors.Is(err, c.want) {
				t.Fatalf("ValidateReceivers = %v, want %v", err, c.want)
			}
		})
	}
}

// 配置权限与输入配置同口径（裁决 10，#180）：仅项目管理员，终态不可。
func TestCanConfigureReceivers(t *testing.T) {
	facts := TaskFacts{Status: TaskInProgress, CreatorID: 3, OwnerID: 5}
	if CanConfigureReceivers(Actor{Role: RoleMember}, 5, facts) {
		t.Fatal("负责人不再可配置接收方（裁决 10）")
	}
	if !CanConfigureReceivers(Actor{Role: RoleAdmin}, 9, facts) {
		t.Fatal("管理员应可配置接收方")
	}
	if CanConfigureReceivers(Actor{Role: RoleMember}, 9, facts) {
		t.Fatal("无关成员不应可配置接收方")
	}
	if CanConfigureReceivers(Actor{Role: RoleAdmin}, 9, TaskFacts{Status: TaskCompleted, OwnerID: 5}) {
		t.Fatal("已完成任务不应可配置接收方")
	}
}

// MW-09／模块 PRD §8.6：终审通过时展开接收方名单；「所有项目成员」按当时成员逐人生成。
func TestReceiptTargets(t *testing.T) {
	members := []int64{4, 2, 7}
	cases := []struct {
		name      string
		scope     string
		receivers []int64
		want      []int64
	}{
		{"未配置不生成", ReceiverScopeNone, []int64{2}, []int64{}},
		{"所有项目成员逐人生成", ReceiverScopeAll, nil, []int64{2, 4, 7}},
		{"指定成员去重升序", ReceiverScopeMembers, []int64{7, 2, 7}, []int64{2, 7}},
		{"指定成员为空不生成", ReceiverScopeMembers, nil, []int64{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ReceiptTargets(c.scope, c.receivers, members)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("ReceiptTargets = %v, want %v", got, c.want)
			}
		})
	}
}

// 接收方没有审核权，本组不提供退回；只有本人可确认，且只能确认一次（模块 PRD §3.2.C）。
func TestCanConfirmReceipt(t *testing.T) {
	at := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	pending := ReceiptFact{ID: 1, TaskID: 8, UserID: 5, GeneratedAt: at}
	if err := CanConfirmReceipt(Actor{Role: RoleMember}, 5, pending); err != nil {
		t.Fatalf("接收方本人应可确认: %v", err)
	}
	if err := CanConfirmReceipt(Actor{Role: RoleMember}, 9, pending); !errors.Is(err, ErrReceiptNotMine) {
		t.Fatalf("他人确认应被拒: %v", err)
	}
	confirmed := pending
	confirmed.ConfirmedAt = &at
	if err := CanConfirmReceipt(Actor{Role: RoleMember}, 5, confirmed); !errors.Is(err, ErrReceiptConfirmed) {
		t.Fatalf("重复确认应被拒: %v", err)
	}
}

// MW-09：终审通过后接收方的待我接收出现待接收项；确认后退出本组。
func TestMyWorkReceipts(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	me := int64(5)
	confirmedAt := now.AddDate(0, 0, -1)
	g := MyWork(MyWorkFacts{
		UserID: me,
		Now:    now,
		Tasks: []WorkTaskFact{
			{ID: 8, Name: "交付任务", DisplayStatus: TaskCompleted, OwnerID: 9, CreatorID: 9},
		},
		Receipts: []ReceiptFact{
			{ID: 1, TaskID: 8, TaskName: "交付任务", UserID: me, GeneratedAt: now.AddDate(0, 0, -3)},
			{ID: 2, TaskID: 8, TaskName: "交付任务", UserID: 9, GeneratedAt: now.AddDate(0, 0, -3)},
			{ID: 3, TaskID: 9, TaskName: "已确认任务", UserID: me, GeneratedAt: now.AddDate(0, 0, -3), ConfirmedAt: &confirmedAt},
		},
	})
	if len(g.Receipts) != 1 {
		t.Fatalf("待我接收应只剩本人未确认的一条: %+v", g.Receipts)
	}
	it := g.Receipts[0]
	if it.Kind != "receipt" || it.RefID == nil || *it.RefID != 1 || it.TaskID == nil || *it.TaskID != 8 {
		t.Fatalf("待接收项事实不对: %+v", it)
	}
	if it.ActionLabel != WorkActionHandle {
		t.Fatalf("待我接收是本人要办的事，动作应为「%s」: %q", WorkActionHandle, it.ActionLabel)
	}
	if it.DrawerTab != "overview" {
		t.Fatalf("待我接收进任务概况定位确认接收区: %q", it.DrawerTab)
	}
	if it.WaitingDays == nil || *it.WaitingDays != 3 {
		t.Fatalf("已等待天数应按生成时间算: %+v", it.WaitingDays)
	}
}
