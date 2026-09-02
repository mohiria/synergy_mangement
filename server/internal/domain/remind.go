package domain

import (
	"errors"
	"fmt"
	"time"
)

// 提醒目标（模块 PRD §5.3、MW-13）。
//
// 提醒目标是「可寻址的当前待行动人」，不等于派生卡点：等待他人分组里尚未成卡点的等待
// （刚提交还没到审批超时阈值的审批件、未交付的上游任务）同样是提醒目标。
// 卡点目标按卡点合成键寻址，等待事项按 wait:<事项类型>:<事项 ID> 寻址。

// ErrRemindCooldown 冷却（MW-13）：按（发起人、被提醒人、任务）三元组计，
// 每天次数上限取项目规则设置，默认 1 次。
var ErrRemindCooldown = errors.New("今天对该成员的提醒次数已用完，明天再试")

// RemindTarget 一个提醒目标。
type RemindTarget struct {
	Key              string
	TaskID           int64 // 提醒所针对的任务（上游未就绪类为被卡住的下游任务，与卡点同口径）
	TaskName         string
	Missing          string // 缺什么：未就绪输入名、待处理的审批环节或按期完成
	Reason           string
	ActionOwnerIDs   []int64
	ActionOwnerNames []string
	Due              *time.Time // 任务截止时间
	ImpactNote       string     // 下游影响
	// 以下为权限判定用的任务事实。
	TaskOwnerID int64
	KrOwnerID   *int64
}

// RemindWaitFact 一条「等待他人」事项作为提醒目标的事实。
type RemindWaitFact struct {
	Kind             string // cancel_request／intermediate_review／final_review／upstream
	RefID            int64
	TaskID           int64
	TaskName         string
	Missing          string
	Reason           string
	ActionOwnerIDs   []int64
	ActionOwnerNames []string
	Due              *time.Time
	TaskOwnerID      int64
	KrOwnerID        *int64
}

// RemindTaskFact 提醒目标所在任务的事实（补齐权限判定、截止时间与下游影响）。
type RemindTaskFact struct {
	Name       string
	OwnerID    int64
	KrOwnerID  *int64
	End        *time.Time
	ImpactNote string
}

// RemindFacts 项目内全部可寻址提醒目标的输入事实。
type RemindFacts struct {
	Blockers []Blocker
	Waits    []RemindWaitFact
	Tasks    map[int64]RemindTaskFact
}

// RemindWaitKey 等待他人事项的提醒目标合成键。
func RemindWaitKey(kind string, refID int64) string {
	return fmt.Sprintf("wait:%s:%d", kind, refID)
}

// ApprovalWaitFact 审批环节（关闭申请、中间或签、KR 终审）的提醒目标事实。
func ApprovalWaitFact(kind string, refID, taskID int64, approverIDs []int64, approverNames []string, days *int) RemindWaitFact {
	missing := approvalStageLabels[kind]
	if missing == "" {
		missing = "审批"
	}
	missing += "处理"
	return RemindWaitFact{
		Kind: kind, RefID: refID, TaskID: taskID,
		Missing: missing, Reason: approvalWaitReason(missing, days),
		ActionOwnerIDs: approverIDs, ActionOwnerNames: approverNames,
	}
}

// approvalWaitReason 审批类提醒的原因文案，与「审批超时」卡点同一措辞。
func approvalWaitReason(missing string, days *int) string {
	if days == nil {
		return missing + "尚未完成"
	}
	return fmt.Sprintf("%s已等待 %d 天", missing, *days)
}

// UpstreamWaitFact 必要输入来源为上游任务、上游尚未交付时的提醒目标事实。
// 与「上游未就绪」卡点同口径：提醒针对被卡住的下游任务，待行动人是上游任务负责人。
func UpstreamWaitFact(edgeID, targetTaskID int64, inputName, sourceName string, sourceOwnerID int64, sourceOwnerName string) RemindWaitFact {
	return RemindWaitFact{
		Kind: "upstream", RefID: edgeID, TaskID: targetTaskID,
		Missing: inputName, Reason: "上游任务「" + sourceName + "」尚未交付当前内容",
		ActionOwnerIDs: []int64{sourceOwnerID}, ActionOwnerNames: []string{sourceOwnerName},
	}
}

// BlockerRemindTarget 把一条派生卡点转成提醒目标。
func BlockerRemindTarget(b Blocker, due *time.Time) RemindTarget {
	return RemindTarget{
		Key: b.Key, TaskID: b.TaskID, TaskName: b.TaskName,
		Missing: b.Missing, Reason: b.Reason,
		ActionOwnerIDs: b.ActionOwnerIDs, ActionOwnerNames: b.ActionOwnerNames,
		Due: due, ImpactNote: b.ImpactNote,
		TaskOwnerID: b.TaskOwnerID, KrOwnerID: b.KrOwnerID,
	}
}

// WaitRemindTarget 把一条等待他人事项转成提醒目标。
func WaitRemindTarget(w RemindWaitFact) RemindTarget {
	return RemindTarget{
		Key: RemindWaitKey(w.Kind, w.RefID), TaskID: w.TaskID, TaskName: w.TaskName,
		Missing: w.Missing, Reason: w.Reason,
		ActionOwnerIDs: dropZeroIDs(w.ActionOwnerIDs), ActionOwnerNames: w.ActionOwnerNames,
		Due: w.Due, TaskOwnerID: w.TaskOwnerID, KrOwnerID: w.KrOwnerID,
	}
}

// RemindTargets 汇总项目内全部提醒目标：派生卡点与尚未成卡点的等待事项各自可寻址，键不冲突。
// 同一件事既成卡点又在等待他人时两条并存——冷却按人＋任务计，重复提醒自然被拒。
func RemindTargets(f RemindFacts) []RemindTarget {
	out := make([]RemindTarget, 0, len(f.Blockers)+len(f.Waits))
	for _, b := range f.Blockers {
		t := BlockerRemindTarget(b, nil)
		if task, ok := f.Tasks[b.TaskID]; ok {
			t.Due = task.End
			if t.ImpactNote == "" {
				t.ImpactNote = task.ImpactNote
			}
		}
		out = append(out, t)
	}
	for _, w := range f.Waits {
		out = append(out, WaitRemindTarget(fillWaitTask(w, f.Tasks)))
	}
	return out
}

// fillWaitTask 用所在任务的事实补齐等待事项：任务名、负责人、KR 负责人与截止时间。
func fillWaitTask(w RemindWaitFact, tasks map[int64]RemindTaskFact) RemindWaitFact {
	t, ok := tasks[w.TaskID]
	if !ok {
		return w
	}
	w.TaskName = t.Name
	w.TaskOwnerID = t.OwnerID
	w.KrOwnerID = t.KrOwnerID
	w.Due = t.End
	return w
}

func dropZeroIDs(ids []int64) []int64 {
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id != 0 {
			out = append(out, id)
		}
	}
	return out
}

// CanRemind 一键提醒权限（模块 PRD §5.3、§10）：访客不可；待行动人不提醒自己；
// 任务负责人、所属 KR 负责人与可编辑项目者可提醒；没有可寻址的待行动人时不提醒。
func CanRemind(a Actor, userID int64, t RemindTarget) bool {
	if !CanEditProject(a) && a.Role != RoleMember {
		return false
	}
	if len(dropZeroIDs(t.ActionOwnerIDs)) == 0 {
		return false
	}
	for _, id := range t.ActionOwnerIDs {
		if id == userID {
			return false
		}
	}
	if t.TaskOwnerID == userID {
		return true
	}
	if t.KrOwnerID != nil && *t.KrOwnerID == userID {
		return true
	}
	return CanEditProject(a)
}

// RemindContent 提醒正文（模块 PRD §5.3）：自动带入任务、缺失输入、截止时间和下游影响。
// 缺字段的部分整段不拼，不留空壳。
func RemindContent(t RemindTarget) string {
	content := fmt.Sprintf("任务「%s」提醒：缺「%s」", t.TaskName, t.Missing)
	if t.Reason != "" {
		content += "（" + t.Reason + "）"
	}
	if t.Due != nil {
		content += "；截止 " + t.Due.Format("2006-01-02")
	}
	if t.ImpactNote != "" {
		content += "；" + t.ImpactNote
	}
	return content
}

// RemindAllowed 冷却判定（MW-13、AC-60）：按（发起人、被提醒人、任务）三元组计，
// sentToday 为该三元组当天已发出的提醒次数，limit 取项目规则设置的每日次数上限。
func RemindAllowed(sentToday, limit int) bool {
	if limit <= 0 {
		limit = DefaultRemindDailyLimit
	}
	return sentToday < limit
}

// RemindQuotaLeft 当日配额判定（#129）：该目标今天是否还有能提醒的待行动人——
// 任一待行动人（当前发起人、该人、目标任务）三元组次数未用完即真，全部用完则假；
// sentToday 为 nil 表示调用方未接入计数（按不限处理）。canRemind 的显隐 = 权限 && 本判定。
func RemindQuotaLeft(t RemindTarget, limit int, sentToday func(recipientID, taskID int64) int) bool {
	if sentToday == nil {
		return true
	}
	for _, id := range dropZeroIDs(t.ActionOwnerIDs) {
		if RemindAllowed(sentToday(id, t.TaskID), limit) {
			return true
		}
	}
	return false
}
