package domain

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// 任务生命周期状态（词汇表「任务生命周期状态」；PRD §5.1；裁决 #162 删除草稿与待入池审批）。
const (
	TaskNotStarted                = "not_started"
	TaskWaitingInput              = "waiting_input"
	TaskInProgress                = "in_progress"
	TaskPendingIntermediateReview = "pending_intermediate_review"
	TaskPendingFinalReview        = "pending_final_review"
	TaskCompleted                 = "completed"
	TaskCancelled                 = "cancelled"
)

var (
	ErrTaskNameEmpty        = errors.New("任务名称不能为空")
	ErrTaskNameTooLong      = errors.New("任务名称不能超过 200 字")
	ErrTaskOwnerNotEligible = errors.New("任务负责人必须是非只读的项目成员")
	ErrTaskPeriodInverted   = errors.New("任务截止时间不能早于开始时间")
	ErrKrOwnerMissing       = errors.New("所属 KR 尚未指定负责人，无人可审批")
	ErrNotKrOwner           = errors.New("只能由所属 KR 负责人处理")
)

// NewTask 待创建的任务草稿最小骨架（PRD §9.1）。
type NewTask struct {
	Name    string
	OwnerID int64
	Start   time.Time
	End     time.Time
}

// TaskFacts 判定任务流转所需的任务事实。
type TaskFacts struct {
	Status    string
	CreatorID int64
	OwnerID   int64
	KrOwnerID *int64
	// ResultUpdate 成果更新的进程（词汇表「成果更新」）：空＝无，open＝已发起未提交，reviewing＝已提交在审。
	ResultUpdate string
}

// CanEditTaskConfig 任务编辑权限的统一口径（裁决 D2，#137；裁决 #162 后无草稿期）：
// 负责人／所属 KR 负责人／可编辑项目者。各配置判定在此之上叠加各自的状态守卫。
func CanEditTaskConfig(a Actor, userID int64, t TaskFacts) bool {
	if !CanWriteProject(a) {
		return false
	}
	if userID == t.OwnerID || CanEditProject(a) {
		return true
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

