package domain

import (
	"errors"
	"testing"
)

// AC-11／§8.4：上报卡点——类型、缺失输入／条件、阻塞原因、希望行动人必填；等级仅预警或高风险。
func TestValidateBlocker(t *testing.T) {
	members := map[int64]bool{3: true, 7: true}
	isMember := func(id int64) bool { return members[id] }
	base := NewBlocker{Kind: BlockerInputMissing, Missing: "现场数据包", Reason: "上游未交付", ActionOwnerID: 7, Level: "warning"}
	cases := []struct {
		name string
		mut  func(*NewBlocker)
		want error
	}{
		{"合法卡点", func(*NewBlocker) {}, nil},
		{"高风险合法", func(b *NewBlocker) { b.Level = "high_risk" }, nil},
		{"类型非法", func(b *NewBlocker) { b.Kind = "vibe" }, ErrBlockerKindInvalid},
		{"缺失内容必填", func(b *NewBlocker) { b.Missing = " " }, ErrBlockerMissingRequired},
		{"阻塞原因必填", func(b *NewBlocker) { b.Reason = " " }, ErrBlockerReasonRequired},
		{"希望行动人须为项目成员", func(b *NewBlocker) { b.ActionOwnerID = 99 }, ErrBlockerActionOwnerInvalid},
		{"等级不可为正常", func(b *NewBlocker) { b.Level = "normal" }, ErrBlockerLevelInvalid},
		{"等级非法", func(b *NewBlocker) { b.Level = "red" }, ErrBlockerLevelInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := base
			tc.mut(&b)
			if got := ValidateBlocker(b, isMember); !errors.Is(got, tc.want) {
				t.Fatalf("ValidateBlocker() = %v, want %v", got, tc.want)
			}
		})
	}
}

// 执行者上报：负责人／创建人／可编辑项目者，已入池执行类状态。
func TestCanReportBlocker(t *testing.T) {
	facts := TaskFacts{Status: TaskInProgress, CreatorID: 3, OwnerID: 5}
	if !CanReportBlocker(Actor{Role: RoleMember}, 5, facts) {
		t.Fatal("负责人应可上报")
	}
	if !CanReportBlocker(Actor{Role: RoleMember}, 5, TaskFacts{Status: TaskNotStarted, OwnerID: 5}) {
		t.Fatal("未开始也可上报（输入缺失常发生在开始前）")
	}
	if CanReportBlocker(Actor{Role: RoleMember}, 9, facts) {
		t.Fatal("无关成员不应可上报")
	}
	if CanReportBlocker(Actor{Role: RoleMember}, 5, TaskFacts{Status: TaskDraft, OwnerID: 5}) {
		t.Fatal("草稿不应可上报")
	}
	if CanReportBlocker(Actor{Role: RoleAdmin}, 9, TaskFacts{Status: TaskCompleted, OwnerID: 5}) {
		t.Fatal("终态不应可上报")
	}
}

// 提醒与解除：开放中才可；提醒＝上报人/任务负责人/可编辑者；解除另含希望行动人。
func TestBlockerActions(t *testing.T) {
	f := BlockerFacts{State: BlockerOpen, CreatedBy: 3, ActionOwnerID: 7, TaskOwnerID: 5}
	if !CanRemindBlocker(Actor{Role: RoleMember}, 3, f) || !CanRemindBlocker(Actor{Role: RoleMember}, 5, f) {
		t.Fatal("上报人与任务负责人应可提醒")
	}
	if CanRemindBlocker(Actor{Role: RoleMember}, 7, f) {
		t.Fatal("希望行动人自己无需提醒自己")
	}
	if !CanResolveBlocker(Actor{Role: RoleMember}, 7, f) {
		t.Fatal("希望行动人应可解除")
	}
	if CanResolveBlocker(Actor{Role: RoleMember}, 9, f) {
		t.Fatal("无关成员不应可解除")
	}
	resolved := BlockerFacts{State: BlockerResolved, CreatedBy: 3, ActionOwnerID: 7, TaskOwnerID: 5}
	if CanRemindBlocker(Actor{Role: RoleAdmin}, 3, resolved) || CanResolveBlocker(Actor{Role: RoleAdmin}, 3, resolved) {
		t.Fatal("已解除的卡点不应再有动作")
	}
}
