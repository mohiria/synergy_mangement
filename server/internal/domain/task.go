package domain

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// 任务生命周期状态（词汇表「任务生命周期状态」；PRD §5.1）。
const (
	TaskDraft                     = "draft"
	TaskPendingPoolReview         = "pending_pool_review"
	TaskNotStarted                = "not_started"
	TaskWaitingInput              = "waiting_input"
	TaskInProgress                = "in_progress"
	TaskPendingIntermediateReview = "pending_intermediate_review"
	TaskPendingFinalReview        = "pending_final_review"
	TaskCompleted                 = "completed"
	TaskCancelled                 = "cancelled"
)

// 入池审批单状态（词汇表「入池审批单」；PRD §5.2.A）。
const (
	PoolReviewPending  = "pending"
	PoolReviewApproved = "approved"
	PoolReviewRejected = "rejected"
)

// PoolExemptOpinion 免审时系统自动记录的免审原因（AC-26）。
const PoolExemptOpinion = "KR 负责人本人创建，免审入池"

var (
	ErrTaskNameEmpty        = errors.New("任务名称不能为空")
	ErrTaskNameTooLong      = errors.New("任务名称不能超过 200 字")
	ErrTaskOwnerNotEligible = errors.New("任务负责人必须是非只读的项目成员")
	ErrTaskPeriodInverted   = errors.New("任务截止时间不能早于开始时间")
	ErrTaskNotDraft         = errors.New("只有草稿任务可以提交入池")
	ErrKrOwnerMissing       = errors.New("所属 KR 尚未指定负责人，无人可审批")
	ErrNotKrOwner           = errors.New("入池审批只能由所属 KR 负责人处理")
	ErrPoolReviewNotPending = errors.New("任务不在待入池审批状态")
)

// NewTask 待创建的任务草稿最小骨架（PRD §9.1）。
type NewTask struct {
	Name    string
	OwnerID int64
	Start   time.Time
	End     time.Time
}

// TaskFacts 判定入池流转所需的任务事实。
type TaskFacts struct {
	Status    string
	CreatorID int64
	OwnerID   int64
	KrOwnerID *int64
	// ResultUpdate 成果更新的进程（词汇表「成果更新」）：空＝无，open＝已发起未提交，reviewing＝已提交在审。
	ResultUpdate string
}

// CanEditTaskConfig 任务编辑权限的统一口径（裁决 D2，#137）：
// 草稿期＝负责人／创建人／可编辑项目者；入池后＝负责人／所属 KR 负责人／可编辑项目者
// （创建人只在草稿期保留编辑权）。各配置判定在此之上叠加各自的状态守卫。
func CanEditTaskConfig(a Actor, userID int64, t TaskFacts) bool {
	if !CanWriteProject(a) {
		return false
	}
	if userID == t.OwnerID || CanEditProject(a) {
		return true
	}
	if t.Status == TaskDraft {
		return userID == t.CreatorID
	}
	return t.KrOwnerID != nil && userID == *t.KrOwnerID
}

// ValidateNewTask 校验任务草稿最小骨架（PRD §9.1：名称、负责人、开始／截止时间）。
func ValidateNewTask(n NewTask, roleOf func(int64) string) error {
	name := strings.TrimSpace(n.Name)
	if name == "" {
		return ErrTaskNameEmpty
	}
	if utf8.RuneCountInString(name) > 200 {
		return ErrTaskNameTooLong
	}
	if !eligibleOwner(roleOf(n.OwnerID)) {
		return ErrTaskOwnerNotEligible
	}
	if n.End.Before(n.Start) {
		return ErrTaskPeriodInverted
	}
	return nil
}

// CanCreateTask 判定能否创建任务：管理员、项目负责人与项目成员可建，访客不可（PRD §3.4）。
func CanCreateTask(a Actor) bool {
	return CanWriteProject(a)
}

// TaskCreationOutcome 派生新任务的初始状态：创建人是所属 KR 负责人时免审直接进入未开始（AC-26），
// 否则为草稿等待提交入池（AC-04）。
func TaskCreationOutcome(creatorID int64, krOwnerID *int64) (string, bool) {
	if krOwnerID != nil && *krOwnerID == creatorID {
		return TaskNotStarted, true
	}
	return TaskDraft, false
}

// SubmitPoolReview 校验提交入池：仅草稿可提交，所属 KR 必须已指定负责人（否则无人可审），
// 且任务上不能有待审批的关闭单（AC-57 双向互斥）。
func SubmitPoolReview(t TaskFacts, hasPendingChange bool) error {
	if t.Status != TaskDraft {
		return ErrTaskNotDraft
	}
	if hasPendingChange {
		return ErrCancelBlocked
	}
	if t.KrOwnerID == nil {
		return ErrKrOwnerMissing
	}
	return nil
}

// CanSubmitPoolReview 判定当前用户能否提交该任务入池：创建人、任务负责人或可编辑项目者（§3.4）。
func CanSubmitPoolReview(a Actor, userID int64, t TaskFacts, hasPendingChange bool) bool {
	if !CanWriteProject(a) || SubmitPoolReview(t, hasPendingChange) != nil {
		return false
	}
	return userID == t.CreatorID || userID == t.OwnerID || CanEditProject(a)
}

// DecidePoolReview 处理入池审批：仅所属 KR 负责人可处理（§3.3 管理员不可替代），
// 通过后任务进入未开始，退回后回到草稿（PRD §5.2.A）。
func DecidePoolReview(a Actor, t TaskFacts, actorID int64, approve bool, opinion string) (string, error) {
	if t.Status != TaskPendingPoolReview {
		return "", ErrPoolReviewNotPending
	}
	if !CanWriteProject(a) || t.KrOwnerID == nil || *t.KrOwnerID != actorID {
		return "", ErrNotKrOwner
	}
	if approve {
		return TaskNotStarted, nil
	}
	// MW-18：退回必须写清理由，与完成审核同口径。
	if strings.TrimSpace(opinion) == "" {
		return "", ErrRejectOpinionRequired
	}
	return TaskDraft, nil
}

// CanDecidePoolReview 判定当前用户能否处理该任务的入池审批（派生动作标志）。
func CanDecidePoolReview(a Actor, userID int64, t TaskFacts) bool {
	_, err := DecidePoolReview(a, t, userID, true, "")
	return err == nil
}
