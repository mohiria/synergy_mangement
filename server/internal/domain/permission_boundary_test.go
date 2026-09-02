package domain

import (
	"errors"
	"testing"
)

// S2／§3.4：职责只在「当前仍是非只读项目成员」时才生效。
// 被移出项目（Role 为空）或被降为只读的人，即便还挂着任务负责人、KR 负责人、
// 成果审核人的身份，也不得再编辑或审批；确认接收是访客唯一的写操作（AC-62）。
func TestWriteActionsRequireNonViewerMembership(t *testing.T) {
	me := int64(5)
	facts := TaskFacts{Status: TaskInProgress, CreatorID: me, OwnerID: me, KrOwnerID: i64(5)}
	notStarted := TaskFacts{Status: TaskNotStarted, CreatorID: me, OwnerID: me, KrOwnerID: i64(5)}
	finalPending := TaskFacts{Status: TaskPendingFinalReview, CreatorID: me, OwnerID: me, KrOwnerID: i64(5)}
	intermediate := TaskFacts{Status: TaskPendingIntermediateReview, CreatorID: me, OwnerID: me, KrOwnerID: i64(5)}

	for _, actor := range []Actor{{Role: RoleViewer}, {Role: ""}} {
		name := "访客"
		if actor.Role == "" {
			name = "已被移出项目"
		}
		t.Run(name, func(t *testing.T) {
			if CanCreateTask(actor) {
				t.Fatal("不应可创建任务")
			}
			if CanStartTask(actor, me, notStarted) {
				t.Fatal("不应可开始任务")
			}
			if CanUpdateProgress(actor, me, facts) {
				t.Fatal("不应可更新进度")
			}
			if CanCancelTask(actor, me, facts, false) {
				t.Fatal("不应可发起关闭")
			}
			if CanConfigureInputs(actor, me, facts) {
				t.Fatal("不应可配置输入")
			}
			if CanConfigureReceivers(actor, me, facts) {
				t.Fatal("不应可配置接收方")
			}
			if CanManageDeliverables(actor, me, facts) {
				t.Fatal("不应可配置交付物项")
			}
			if CanUploadCandidate(actor, me, facts) {
				t.Fatal("不应可登记候选内容")
			}
			if CanSubmitCompletion(actor, me, facts, 1) {
				t.Fatal("不应可提交完成申请")
			}
			if CanManageReviewers(actor, me, facts) {
				t.Fatal("不应可调整成果审核人")
			}
			if CanAbandonCancelRequest(actor, me, me, CancelRequestRejectedState, false) {
				t.Fatal("不应可放弃关闭申请")
			}
			if err := TaskEditRule(actor, me, facts, false); !errors.Is(err, ErrChangeForbidden) {
				t.Fatalf("不应可修改关键字段: %v", err)
			}
			if _, err := CancelRoute(actor, me, facts, false); !errors.Is(err, ErrCancelForbidden) {
				t.Fatalf("不应可发起关闭申请: %v", err)
			}
			if err := DecideCancelRequestRule(actor, CancelRequestPendingState, facts, me, true, ""); !errors.Is(err, ErrNotKrOwner) {
				t.Fatalf("不应可处理关闭申请: %v", err)
			}
			if _, err := DecideCompletionRule(actor, finalPending, me, true, ""); !errors.Is(err, ErrNotKrOwner) {
				t.Fatalf("不应可终审: %v", err)
			}
			if _, _, err := DecideIntermediateRule(actor, intermediate, me,
				func(int64) bool { return true }, true, ""); !errors.Is(err, ErrNotReviewer) {
				t.Fatalf("不应可成果审核: %v", err)
			}
			// AC-62：确认接收是访客唯一保留的写操作，不受本前置约束。
			if err := CanConfirmReceipt(actor, me, ReceiptFact{ID: 1, TaskID: 1, UserID: me}); err != nil {
				t.Fatalf("被指定为接收方时应可确认接收: %v", err)
			}
		})
	}

	// 反向对照：同样的职责在项目成员身上照常生效。
	member := Actor{Role: RoleMember}
	if !CanStartTask(member, me, notStarted) || !CanUpdateProgress(member, me, facts) {
		t.Fatal("项目成员的任务负责人职责应照常生效")
	}
	if err := DecideCancelRequestRule(member, CancelRequestPendingState, facts, me, true, ""); err != nil {
		t.Fatalf("项目成员任 KR 负责人应可处理关闭申请: %v", err)
	}
}

// S2／§3.4：访客不能被任命为任务负责人或 KR 负责人。
func TestViewerCannotBeAppointedOwner(t *testing.T) {
	roleOf := func(id int64) string {
		switch id {
		case 5:
			return RoleMember
		case 9:
			return RoleViewer
		}
		return ""
	}
	base := NewTask{Name: "联调验证", OwnerID: 5, Start: date("2026-09-01"), End: date("2026-09-30")}
	if err := ValidateNewTask(base, roleOf); err != nil {
		t.Fatalf("项目成员应可任任务负责人: %v", err)
	}
	viewerOwned := base
	viewerOwned.OwnerID = 9
	if err := ValidateNewTask(viewerOwned, roleOf); !errors.Is(err, ErrTaskOwnerNotEligible) {
		t.Fatalf("访客不应可任任务负责人: %v", err)
	}
	strangerOwned := base
	strangerOwned.OwnerID = 77
	if err := ValidateNewTask(strangerOwned, roleOf); !errors.Is(err, ErrTaskOwnerNotEligible) {
		t.Fatalf("非成员不应可任任务负责人: %v", err)
	}

	items := []OkrBatchItem{{Title: "提升交付质量", KeyResults: []NewKeyResult{{Description: "上线自动验收", OwnerID: i64(9)}}}}
	if err := ValidateOkrBatch(items, roleOf); !errors.Is(err, ErrKrOwnerNotEligible) {
		t.Fatalf("访客不应可任 KR 负责人: %v", err)
	}
	items[0].KeyResults[0].OwnerID = i64(5)
	if err := ValidateOkrBatch(items, roleOf); err != nil {
		t.Fatalf("项目成员应可任 KR 负责人: %v", err)
	}
}
