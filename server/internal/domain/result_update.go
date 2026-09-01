package domain

import "errors"

// 成果更新（词汇表「成果更新」；PRD §5.1、§5.3、AC-66）：已完成任务的负责人对交付物再次
// 发起的更新，走同一道完成审批。它是已完成任务唯一接受的审批单，审批期间任务状态保持已完成。
const (
	// ResultUpdateNone 无成果更新在途。
	ResultUpdateNone = ""
	// ResultUpdateOpen 已发起、候选内容尚未随完成申请提交。
	ResultUpdateOpen = "open"
	// ResultUpdateReviewing 已提交完成申请，中间或签或 KR 终审在审。
	ResultUpdateReviewing = "reviewing"
)

var (
	ErrResultUpdateNotCompleted  = errors.New("只有已完成任务可以发起成果更新")
	ErrResultUpdateExists        = errors.New("任务上已有未决成果更新")
	ErrResultUpdateForbidden     = errors.New("只有任务负责人与项目管理员可以发起成果更新")
	ErrResultUpdatePendingExists = errors.New("任务上存在未决审批单，暂不能发起成果更新")
)

// StartResultUpdateRule 校验发起成果更新（AC-66）：仅已完成任务；同一任务至多一件在途；
// 与其他未决审批单互斥（与 AC-57 关闭申请同口径）；KR 必须已指定负责人，否则无人终审；
// 发起人限任务负责人与可编辑项目者（创建人不在其列——成果由负责人交付）。
func StartResultUpdateRule(a Actor, userID int64, t TaskFacts, hasPendingChange bool) error {
	if t.Status != TaskCompleted {
		return ErrResultUpdateNotCompleted
	}
	if !CanWriteProject(a) {
		return ErrResultUpdateForbidden
	}
	if t.ResultUpdate != ResultUpdateNone {
		return ErrResultUpdateExists
	}
	if hasPendingChange {
		return ErrResultUpdatePendingExists
	}
	if t.KrOwnerID == nil {
		return ErrKrOwnerMissing
	}
	if userID != t.OwnerID && !CanEditProject(a) {
		return ErrResultUpdateForbidden
	}
	return nil
}

// CanStartResultUpdate 判定当前用户能否对本任务发起成果更新（派生字段）。
func CanStartResultUpdate(a Actor, userID int64, t TaskFacts, hasPendingChange bool) bool {
	return StartResultUpdateRule(a, userID, t, hasPendingChange) == nil
}

// InResultUpdate 报告任务是否有成果更新在途（已发起或在审）。
func InResultUpdate(t TaskFacts) bool {
	return t.Status == TaskCompleted && t.ResultUpdate != ResultUpdateNone
}

// ResultUpdateReviewInFlight 报告成果更新是否已提交、正在审批。
func ResultUpdateReviewInFlight(t TaskFacts) bool {
	return t.Status == TaskCompleted && t.ResultUpdate == ResultUpdateReviewing
}

// ResultUpdateStateAfterDecision 成果更新审结后的进程状态（裁决 #165）：
// 通过则进程结束（新内容已生效）；退回则回到「已发起」——候选保留在任务上，
// 负责人可删除、重传后带剩余候选重新提交。
func ResultUpdateStateAfterDecision(approve bool) string {
	if approve {
		return ResultUpdateNone
	}
	return ResultUpdateOpen
}

// CanDeleteCandidate 判定能否删除候选文件（裁决 #165）：与登记候选同口径——
// 负责人（管理员／项目负责人纠错），执行类状态或成果更新已发起；
// 完成申请在审期间不可删（候选已随申请快照）。页面只提供删除按钮，新增内容走既有上传入口。
func CanDeleteCandidate(a Actor, userID int64, t TaskFacts) bool {
	return CanUploadCandidate(a, userID, t)
}
