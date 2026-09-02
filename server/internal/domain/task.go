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

// CanEditTaskConfig 任务编辑权限的统一口径（裁决 10，#180：任务配置收归项目管理员，
// 裁决 D2 #137 四角色口径作废）：仅可编辑项目者（项目负责人与项目管理员）。
// 各配置判定在此之上叠加各自的状态守卫。
func CanEditTaskConfig(a Actor, userID int64, t TaskFacts) bool {
	_ = userID
	return CanEditProject(a)
}

// OwnerOrProjectAdmin 任务负责人保留动作的口径（裁决 10，#180）：负责人本人或项目管理员。
// 覆盖上传链路——交付物项、过程文件／外部材料、成果审核人配置。
func OwnerOrProjectAdmin(a Actor, userID int64, t TaskFacts) bool {
	if !CanWriteProject(a) {
		return false
	}
	return userID == t.OwnerID || CanEditProject(a)
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

// CanCreateTask 判定能否创建任务（裁决 10，#180）：收归项目管理员与项目负责人；
// 项目成员经任务创建邀请（AC-03）间接创建的路径不在此判定内。
func CanCreateTask(a Actor) bool {
	return CanEditProject(a)
}

