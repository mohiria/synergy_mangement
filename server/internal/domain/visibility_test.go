package domain

import (
	"errors"
	"testing"
)

func TestValidateProjectVisibility(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr error
	}{
		{"私有合法", VisibilityPrivate, nil},
		{"公开合法", VisibilityPublic, nil},
		{"空值不合法", "", ErrProjectVisibilityInvalid},
		{"未知取值不合法", "internal", ErrProjectVisibilityInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateProjectVisibility(c.value)
			if c.wantErr == nil {
				if err != nil {
					t.Fatalf("ValidateProjectVisibility(%q) = %v, want nil", c.value, err)
				}
				return
			}
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("ValidateProjectVisibility(%q) = %v, want %v", c.value, err, c.wantErr)
			}
		})
	}
}

// 有效身份判定是全系统唯一的入口（#111）：成员角色 → 隐式访客 → 无身份。
func TestProjectIdentity(t *testing.T) {
	const me, owner = int64(5), int64(9)
	cases := []struct {
		name       string
		userID     int64
		ownerID    int64
		memberRole string
		visibility string
		want       Actor
	}{
		{"私有项目的项目负责人", owner, owner, "", VisibilityPrivate, Actor{IsOwner: true}},
		{"私有项目的管理员", me, owner, RoleAdmin, VisibilityPrivate, Actor{Role: RoleAdmin}},
		{"私有项目的项目成员", me, owner, RoleMember, VisibilityPrivate, Actor{Role: RoleMember}},
		{"私有项目的访客", me, owner, RoleViewer, VisibilityPrivate, Actor{Role: RoleViewer}},
		{"私有项目的非成员无身份", me, owner, "", VisibilityPrivate, Actor{}},
		{"公开项目的非成员得隐式访客", me, owner, "", VisibilityPublic, Actor{Role: RoleViewer, Implicit: true}},
		{"公开项目的显式成员身份优先", me, owner, RoleMember, VisibilityPublic, Actor{Role: RoleMember}},
		{"公开项目的显式访客不算隐式", me, owner, RoleViewer, VisibilityPublic, Actor{Role: RoleViewer}},
		{"公开项目的项目负责人仍是负责人", owner, owner, "", VisibilityPublic, Actor{IsOwner: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ProjectIdentity(c.userID, c.ownerID, c.memberRole, c.visibility, false)
			if got != c.want {
				t.Fatalf("ProjectIdentity(%d, %d, %q, %q) = %+v, want %+v",
					c.userID, c.ownerID, c.memberRole, c.visibility, got, c.want)
			}
		})
	}
}

// 隐式访客读得全、写不了（裁决 D 附）：读放开与写拒绝同一次落地。
func TestImplicitViewerReadsAllButWritesNothing(t *testing.T) {
	me := int64(5)
	implicit := Actor{Role: RoleViewer, Implicit: true}
	facts := TaskFacts{Status: TaskInProgress, CreatorID: me, OwnerID: me}
	notStarted := TaskFacts{Status: TaskNotStarted, CreatorID: me, OwnerID: me}
	finalPending := TaskFacts{Status: TaskInReview, CreatorID: me, OwnerID: me}
	intermediate := TaskFacts{Status: TaskInReview, CreatorID: me, OwnerID: me}

	// 读：与显式访客同等，页面与下载都放开。
	if !CanReadProject(implicit) {
		t.Fatal("隐式访客应可读项目")
	}

	writes := []struct {
		name    string
		allowed bool
	}{
		{"创建任务", CanCreateTask(implicit)},
		{"开始任务", CanStartTask(implicit, me, notStarted)},
		{"更新进度", CanUpdateProgress(implicit, me, facts)},
		{"关闭任务", CanCloseTask(implicit, facts)},
		{"配置输入", CanConfigureInputs(implicit, me, facts)},
		{"配置接收方", CanConfigureReceivers(implicit, me, facts)},
		{"配置交付物项", CanManageDeliverables(implicit, me, facts)},
		{"登记候选内容", CanUploadCandidate(implicit, me, facts)},
		{"提交完成申请", CanSubmitCompletion(implicit, me, facts, 1)},
		{"调整成果审核人", CanManageReviewers(implicit, me, facts)},
		{"编辑项目", CanEditProject(implicit)},
		{"管理成员", CanManageMembers(implicit)},
		{"发一键提醒", CanRemind(implicit, me, RemindTarget{TaskID: 1, ActionOwnerIDs: []int64{9}, TaskOwnerID: 9})},
		// 发表讨论是隐式访客与显式访客唯一的差别：显式访客可以，隐式不行。
		{"发表讨论", CanDiscuss(implicit)},
	}
	for _, w := range writes {
		if w.allowed {
			t.Fatalf("隐式访客不应可%s", w.name)
		}
	}

	if err := TaskEditRule(implicit, me, facts); !errors.Is(err, ErrChangeForbidden) {
		t.Fatalf("隐式访客不应可修改关键字段: %v", err)
	}
	if err := CloseTaskRule(implicit, facts); !errors.Is(err, ErrCancelForbidden) {
		t.Fatalf("隐式访客不应可关闭任务: %v", err)
	}
	if _, err := DecideCompletionRule(implicit, finalPending, true, ""); !errors.Is(err, ErrNotFinalReviewer) {
		t.Fatalf("隐式访客不应可终审: %v", err)
	}
	if _, _, err := DecideIntermediateRule(implicit, intermediate, me,
		func(int64) bool { return true }, true, ""); !errors.Is(err, ErrNotReviewer) {
		t.Fatalf("隐式访客不应可成果审核: %v", err)
	}
	// 确认接收是显式访客唯一的写操作（AC-62），隐式访客连这一条也没有——它不落成员表，不能被指定为接收方。
	receipt := ReceiptFact{ID: 1, TaskID: 1, UserID: me}
	if err := CanConfirmReceipt(implicit, me, receipt); !errors.Is(err, ErrReceiptNotMine) {
		t.Fatalf("隐式访客不应可确认接收: %v", err)
	}
	if err := CanConfirmReceipt(Actor{Role: RoleViewer}, me, receipt); err != nil {
		t.Fatalf("显式访客被指定为接收方时应可确认接收: %v", err)
	}
}

// #200：系统管理员对任意项目隐式视同项目管理员，但显式负责人／管理员身份仍按显式表达；
// 隐式身份只影响权限判定（SystemAdmin 位标记来源），不是隐式访客。
func TestProjectIdentitySystemAdmin(t *testing.T) {
	const me, owner = int64(5), int64(9)
	cases := []struct {
		name       string
		userID     int64
		ownerID    int64
		memberRole string
		visibility string
		want       Actor
	}{
		{"私有项目的非成员系统管理员视同管理员", me, owner, "", VisibilityPrivate, Actor{Role: RoleAdmin, SystemAdmin: true}},
		{"公开项目的非成员系统管理员视同管理员而非隐式访客", me, owner, "", VisibilityPublic, Actor{Role: RoleAdmin, SystemAdmin: true}},
		{"显式访客的系统管理员权限仍视同管理员", me, owner, RoleViewer, VisibilityPrivate, Actor{Role: RoleAdmin, SystemAdmin: true}},
		{"显式项目成员的系统管理员权限仍视同管理员", me, owner, RoleMember, VisibilityPrivate, Actor{Role: RoleAdmin, SystemAdmin: true}},
		{"显式管理员的系统管理员按显式身份", me, owner, RoleAdmin, VisibilityPrivate, Actor{Role: RoleAdmin}},
		{"项目负责人的系统管理员按负责人身份", owner, owner, "", VisibilityPrivate, Actor{IsOwner: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ProjectIdentity(c.userID, c.ownerID, c.memberRole, c.visibility, true)
			if got != c.want {
				t.Fatalf("ProjectIdentity(..., systemAdmin=true) = %+v, want %+v", got, c.want)
			}
			if !CanEditProject(got) || !CanManageMembers(got) || !CanReadProject(got) || !CanWriteProject(got) {
				t.Fatalf("系统管理员应具备管理员全部权限：%+v", got)
			}
			if got.Implicit || !CanDiscuss(got) {
				t.Fatalf("系统管理员不是隐式访客，应可发表讨论：%+v", got)
			}
		})
	}
}
