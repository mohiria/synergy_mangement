package domain

import (
	"errors"
	"testing"
)

// AC-03：只有该 KR 负责人、项目管理员或项目负责人可以发出任务创建邀请。
func TestCanInviteForKr(t *testing.T) {
	cases := []struct {
		name    string
		actor   Actor
		user    int64
		krOwner *int64
		want    bool
	}{
		{"KR 负责人可邀请", Actor{Role: RoleMember}, 7, i64(7), true},
		{"项目管理员可邀请", Actor{Role: RoleAdmin}, 9, i64(7), true},
		{"项目负责人可邀请", Actor{IsOwner: true}, 9, i64(7), true},
		{"项目成员不可邀请", Actor{Role: RoleMember}, 9, i64(7), false},
		{"访客不可邀请", Actor{Role: RoleViewer}, 9, i64(7), false},
		{"KR 无负责人时管理员仍可邀请", Actor{Role: RoleAdmin}, 9, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanInviteForKr(tc.actor, tc.user, tc.krOwner); got != tc.want {
				t.Fatalf("CanInviteForKr() = %v, want %v", got, tc.want)
			}
		})
	}
}

// 受邀成员必须是非只读项目成员，且不能邀请自己。
func TestValidateInvitees(t *testing.T) {
	roles := map[int64]string{3: RoleMember, 4: RoleAdmin, 5: RoleViewer}
	roleOf := func(id int64) string { return roles[id] }
	cases := []struct {
		name     string
		inviter  int64
		invitees []int64
		want     error
	}{
		{"合法多人邀请", 7, []int64{3, 4}, nil},
		{"邀请自己", 3, []int64{3}, ErrInviteSelf},
		{"邀请访客", 7, []int64{5}, ErrInviteeNotEligible},
		{"邀请非成员", 7, []int64{99}, ErrInviteeNotEligible},
		{"空列表", 7, nil, ErrInviteesEmpty},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidateInvitees(tc.inviter, tc.invitees, roleOf); !errors.Is(got, tc.want) {
				t.Fatalf("ValidateInvitees() = %v, want %v", got, tc.want)
			}
		})
	}
}

// 撤回：仅待处理邀请可撤回，动作人为邀请人／项目管理员／项目负责人。
func TestCanRevokeInvite(t *testing.T) {
	cases := []struct {
		name    string
		actor   Actor
		user    int64
		inviter int64
		state   string
		want    bool
	}{
		{"邀请人可撤回", Actor{Role: RoleMember}, 7, 7, TaskInvitePending, true},
		{"管理员可撤回", Actor{Role: RoleAdmin}, 9, 7, TaskInvitePending, true},
		{"无关成员不可撤回", Actor{Role: RoleMember}, 9, 7, TaskInvitePending, false},
		{"已完成不可撤回", Actor{Role: RoleAdmin}, 7, 7, TaskInviteCompleted, false},
		{"已撤回不可再撤回", Actor{Role: RoleAdmin}, 7, 7, TaskInviteRevoked, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanRevokeInvite(tc.actor, tc.user, tc.inviter, tc.state); got != tc.want {
				t.Fatalf("CanRevokeInvite() = %v, want %v", got, tc.want)
			}
		})
	}
}

// AC-03：受邀人通过邀请提交关联任务；本批至少一项属于邀请指定 KR；
// 词汇表：同一 KR 下无关任务不会使邀请结束（他人创建不触发完成）。
func TestFulfillInvite(t *testing.T) {
	cases := []struct {
		name    string
		state   string
		invitee int64
		actor   int64
		krID    int64
		itemKrs []int64
		want    error
	}{
		{"受邀人在指定 KR 提交", TaskInvitePending, 3, 3, 11, []int64{11, 12}, nil},
		{"非受邀人不能通过邀请提交", TaskInvitePending, 3, 9, 11, []int64{11}, ErrInviteNotInvitee},
		{"批内无指定 KR 任务", TaskInvitePending, 3, 3, 11, []int64{12}, ErrInviteKrMismatch},
		{"已撤回邀请不可响应", TaskInviteRevoked, 3, 3, 11, []int64{11}, ErrInviteNotPending},
		{"已完成邀请不可再响应", TaskInviteCompleted, 3, 3, 11, []int64{11}, ErrInviteNotPending},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FulfillInvite(tc.state, tc.invitee, tc.actor, tc.krID, tc.itemKrs); !errors.Is(got, tc.want) {
				t.Fatalf("FulfillInvite() = %v, want %v", got, tc.want)
			}
		})
	}
}

// 派生动作标志：受邀人本人且待处理时可响应。
func TestCanHandleInvite(t *testing.T) {
	if !CanHandleInvite(3, 3, TaskInvitePending) {
		t.Fatal("受邀人待处理邀请应可响应")
	}
	if CanHandleInvite(9, 3, TaskInvitePending) {
		t.Fatal("非受邀人不应可响应")
	}
	if CanHandleInvite(3, 3, TaskInviteRevoked) {
		t.Fatal("已撤回邀请不应可响应")
	}
}

// AC-03（#83）：邀请发出后受邀人要收到带 KR 和邀请说明的站内通知；
// 撤回不补发（与 #5 的撤回口径一致），因此这里只定型「发出」这一条的文案。
func TestTaskInviteNotificationContent(t *testing.T) {
	cases := []struct {
		name    string
		inviter string
		code    string
		desc    string
		note    string
		want    string
	}{
		{
			"带邀请说明", "王浩然", "KR1.2", "现场回归通过", "按上周口径拆到人",
			"王浩然邀请你在 KR1.2「现场回归通过」下创建任务：按上周口径拆到人",
		},
		{
			"未填说明时不留空冒号", "王浩然", "KR1.2", "现场回归通过", "",
			"王浩然邀请你在 KR1.2「现场回归通过」下创建任务",
		},
		{
			"邀请人姓名缺失时退化", "", "KR2.1", "上线自动验收", "",
			"你被邀请在 KR2.1「上线自动验收」下创建任务",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := TaskInviteNotification(c.inviter, c.code, c.desc, c.note); got != c.want {
				t.Fatalf("TaskInviteNotification = %q, want %q", got, c.want)
			}
		})
	}
}
