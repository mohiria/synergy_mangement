package domain

import (
	"testing"
	"time"
)

// AC-58：必要输入未就绪不阻断任何动作——开始、上传候选、提交完成申请都放行。
func TestUnreadyInputDoesNotBlockActions(t *testing.T) {
	owner := int64(5)
	notStarted := TaskFacts{Status: TaskNotStarted, CreatorID: owner, OwnerID: owner}
	// 未开始且必要输入未就绪，页面汇总显示「等待输入」，但开始动作照常可用。
	if got := DeriveDisplayStatus(notStarted.Status, true); got != TaskWaitingInput {
		t.Fatalf("未开始且输入未就绪应显示等待输入，got %q", got)
	}
	if !CanStartTask(Actor{Role: RoleMember}, owner, notStarted) {
		t.Fatal("必要输入未就绪不应挡住开始")
	}
	waiting := notStarted
	waiting.Status = TaskWaitingInput
	if !CanUploadCandidate(Actor{Role: RoleMember}, owner, waiting) {
		t.Fatal("必要输入未就绪不应挡住上传候选内容")
	}
	inProgress := notStarted
	inProgress.Status = TaskInProgress
	if !CanUploadCandidate(Actor{Role: RoleMember}, owner, inProgress) {
		t.Fatal("进行中应可上传候选内容")
	}
	if err := SubmitCompletionRule(inProgress, 1, "已完成"); err != nil {
		t.Fatalf("必要输入未就绪不应挡住提交完成申请: %v", err)
	}
	// 开始之后既不再叠加「等待输入」，也不再派生「上游未就绪」卡点。
	if got := DeriveDisplayStatus(TaskInProgress, true); got != TaskInProgress {
		t.Fatalf("进行中不应叠加等待输入，got %q", got)
	}
}

// AC-58：下游负责人的「等待他人-上游任务」条目与提醒入口保留，与本任务是否已开始无关，
// 否则下游就没有催上游的抓手。
func TestUpstreamWaitingSurvivesTaskStart(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	me := int64(5)
	upstream := WorkUpstreamFact{
		EdgeID: 100, TargetTaskID: 1, TargetName: "下游任务", TargetOwnerID: me,
		SourceTaskID: i64(2), SourceName: "上游任务", SourceOwnerID: 9, SourceOwnerName: "吴九",
		InputName: "现场数据包", Ready: false, Necessity: NecessityRequired,
	}
	for _, status := range []string{TaskWaitingInput, TaskInProgress} {
		g := MyWork(MyWorkFacts{
			UserID: me,
			Actor:  Actor{Role: RoleMember},
			Now:    now,
			Tasks: []WorkTaskFact{
				{ID: 1, Name: "下游任务", DisplayStatus: status, OwnerID: me, CreatorID: me},
				{ID: 2, Name: "上游任务", DisplayStatus: TaskInProgress, OwnerID: 9, CreatorID: 9},
			},
			Upstreams: []WorkUpstreamFact{upstream},
		})
		var found *WorkItem
		for i := range g.Waiting {
			if g.Waiting[i].Kind == "upstream" {
				found = &g.Waiting[i]
			}
		}
		if found == nil {
			t.Fatalf("状态 %s 时上游等待条目丢失: %+v", status, g.Waiting)
		}
		if !found.CanRemind {
			t.Fatalf("状态 %s 时上游提醒入口丢失: %+v", status, found)
		}
	}
}
