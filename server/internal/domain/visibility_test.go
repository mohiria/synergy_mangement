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
			got := ProjectIdentity(c.userID, c.ownerID, c.memberRole, c.visibility)
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
	facts := TaskFacts{Status: TaskInProgress, CreatorID: me, OwnerID: me, KrOwnerID: i64(5)}
	notStarted := TaskFacts{Status: TaskNotStarted, CreatorID: me, OwnerID: me, KrOwnerID: i64(5)}
	draft := TaskFacts{Status: TaskDraft, CreatorID: me, OwnerID: me, KrOwnerID: i64(5)}
	poolPending := TaskFacts{Status: TaskPendingPoolReview, CreatorID: me, OwnerID: me, KrOwnerID: i64(5)}
	finalPending := TaskFacts{Status: TaskPendingFinalReview, CreatorID: me, OwnerID: me, KrOwnerID: i64(5)}
	intermediate := TaskFacts{Status: TaskPendingIntermediateReview, CreatorID: me, OwnerID: me, KrOwnerID: i64(5)}

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
		{"发起关闭", CanCancelTask(implicit, me, facts, false)},
		{"提交入池", CanSubmitPoolReview(implicit, me, draft, false)},
		{"审批入池", CanDecidePoolReview(implicit, me, poolPending)},
		{"配置输入", CanConfigureInputs(implicit, me, facts)},
		{"配置接收方", CanConfigureReceivers(implicit, me, facts)},
		{"配置交付物项", CanManageDeliverables(implicit, me, facts)},
		{"登记候选内容", CanUploadCandidate(implicit, me, facts)},
		{"提交完成申请", CanSubmitCompletion(implicit, me, facts, 1)},
		{"调整成果审核人", CanManageReviewers(implicit, me, facts)},
		{"编辑项目", CanEditProject(implicit)},
		{"管理成员", CanManageMembers(implicit)},
		{"创建成果包", CanCreatePackage(implicit)},
		{"发一键提醒", CanRemind(implicit, me, RemindTarget{TaskID: 1, ActionOwnerIDs: []int64{9}, TaskOwnerID: 9})},
		// 发表讨论是隐式访客与显式访客唯一的差别：显式访客可以，隐式不行。
		{"发表讨论", CanDiscuss(implicit)},
	}
	for _, w := range writes {
		if w.allowed {
			t.Fatalf("隐式访客不应可%s", w.name)
		}
	}

	if _, err := FieldChangeRoute(implicit, me, facts, false); !errors.Is(err, ErrChangeForbidden) {
		t.Fatalf("隐式访客不应可提交关键字段修改: %v", err)
	}
	if _, err := CancelRoute(implicit, me, facts, false); !errors.Is(err, ErrCancelForbidden) {
		t.Fatalf("隐式访客不应可发起关闭申请: %v", err)
	}
	if _, err := DecidePoolReview(implicit, poolPending, me, true, ""); !errors.Is(err, ErrNotKrOwner) {
		t.Fatalf("隐式访客不应可处理入池审批: %v", err)
	}
	if err := DecideFieldChangeRule(implicit, FieldChangePendingState, facts, me, true, ""); !errors.Is(err, ErrNotKrOwner) {
		t.Fatalf("隐式访客不应可处理变更单: %v", err)
	}
	if _, err := DecideCompletionRule(implicit, finalPending, me, true, ""); !errors.Is(err, ErrNotKrOwner) {
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
