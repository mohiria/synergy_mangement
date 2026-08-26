package domain

import (
	"testing"
	"time"
)

var blockerNow = time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)

func blockerDay(offset int) *time.Time {
	d := blockerNow.AddDate(0, 0, offset)
	return &d
}

// baseBlockerFacts 一个不产生任何卡点的干净项目：任务在期内、输入已就绪、审批刚提交、无硬前置环。
func baseBlockerFacts() BlockerFacts {
	kr7 := int64(7)
	return BlockerFacts{
		Now:                 blockerNow,
		ApprovalTimeoutDays: ApprovalTimeoutDays,
		Tasks: []BlockerTaskFact{{
			ID: 1, Name: "现场调研", Status: TaskInProgress,
			OwnerID: 5, OwnerName: "王五",
			KrID: 10, KrOwnerID: &kr7, KrOwnerName: "赵七",
			StartDate: blockerDay(-3), EndDate: blockerDay(5),
		}},
		Inputs: []BlockerInputFact{{
			EdgeID: 100, TargetTaskID: 1, InputName: "现场数据包",
			Necessity: NecessityRequired, Ready: true,
		}},
	}
}

func findBlocker(bs []Blocker, key string) *Blocker {
	for i := range bs {
		if bs[i].Key == key {
			return &bs[i]
		}
	}
	return nil
}

// AC-11：四类结构化事实的进入与退出（我的工作 PRD §8.7）。
func TestDeriveBlockersEntryAndExit(t *testing.T) {
	kr7 := int64(7)
	kr8 := int64(8)
	cases := []struct {
		name    string
		mut     func(*BlockerFacts)
		wantKey string // 期望存在的卡点键；空串表示期望一条都不派生
	}{
		{"干净项目无卡点", func(*BlockerFacts) {}, ""},

		// —— 上游未就绪：必要输入未就绪且已到开始时间 ——
		{"必要输入未就绪且已开始", func(f *BlockerFacts) {
			f.Inputs[0].Ready = false
			f.Inputs[0].SourceTaskID = ptr64(2)
			f.Inputs[0].SourceTaskName = "地质勘察"
			f.Inputs[0].SourceOwnerID = 6
			f.Inputs[0].SourceOwnerName = "孙六"
		}, "upstream_unready:edge:100"},
		{"未到开始时间不算卡点", func(f *BlockerFacts) {
			f.Inputs[0].Ready = false
			f.Inputs[0].SourceTaskID = ptr64(2)
			f.Tasks[0].Status = TaskNotStarted
			f.Tasks[0].StartDate = blockerDay(2)
		}, ""},
		{"参考输入未就绪只提示不成卡点", func(f *BlockerFacts) {
			f.Inputs[0].Ready = false
			f.Inputs[0].Necessity = NecessityReference
			f.Inputs[0].SourceTaskID = ptr64(2)
		}, ""},
		{"输入就绪后自动解除", func(f *BlockerFacts) {
			f.Inputs[0].Ready = true
			f.Inputs[0].SourceTaskID = ptr64(2)
		}, ""},
		{"任务已完成不再看输入", func(f *BlockerFacts) {
			f.Inputs[0].Ready = false
			f.Inputs[0].SourceTaskID = ptr64(2)
			f.Tasks[0].Status = TaskCompleted
		}, ""},

		// —— 任务超期：截止已过且未完成 ——
		{"截止已过且未完成", func(f *BlockerFacts) { f.Tasks[0].EndDate = blockerDay(-1) }, "task_overdue:1"},
		{"截止已过但已完成", func(f *BlockerFacts) {
			f.Tasks[0].EndDate = blockerDay(-1)
			f.Tasks[0].Status = TaskCompleted
		}, ""},
		{"截止已过但已取消", func(f *BlockerFacts) {
			f.Tasks[0].EndDate = blockerDay(-1)
			f.Tasks[0].Status = TaskCancelled
		}, ""},
		{"草稿超期不入卡点", func(f *BlockerFacts) {
			f.Tasks[0].EndDate = blockerDay(-1)
			f.Tasks[0].Status = TaskDraft
		}, ""},
		{"改期生效后自动解除", func(f *BlockerFacts) { f.Tasks[0].EndDate = blockerDay(1) }, ""},

		// —— 审批超时：当前环节等待达到 N×24 小时 ——
		{"入池审批超时", func(f *BlockerFacts) {
			f.Tasks[0].Status = TaskPendingPoolReview
			f.Approvals = []BlockerApprovalFact{{
				Kind: "pool_review", RefID: 20, TaskID: 1,
				StageSince:  blockerNow.Add(-3 * 24 * time.Hour),
				ApproverIDs: []int64{7}, ApproverNames: []string{"赵七"},
			}}
		}, "approval_timeout:pool_review:20"},
		{"未达阈值不算超时", func(f *BlockerFacts) {
			f.Approvals = []BlockerApprovalFact{{
				Kind: "final_review", RefID: 30, TaskID: 1,
				StageSince:  blockerNow.Add(-47 * time.Hour),
				ApproverIDs: []int64{7}, ApproverNames: []string{"赵七"},
			}}
		}, ""},
		{"进入新环节重新计时", func(f *BlockerFacts) {
			f.Approvals = []BlockerApprovalFact{{
				Kind: "final_review", RefID: 30, TaskID: 1,
				StageSince:  blockerNow.Add(-1 * time.Hour),
				ApproverIDs: []int64{7}, ApproverNames: []string{"赵七"},
			}}
		}, ""},

		// —— 硬依赖互锁：硬前置交付边成环 ——
		{"硬前置成环", func(f *BlockerFacts) {
			f.Tasks = append(f.Tasks, BlockerTaskFact{
				ID: 2, Name: "地质勘察", Status: TaskInProgress, OwnerID: 6, OwnerName: "孙六",
				KrID: 11, KrOwnerID: &kr8, KrOwnerName: "周八", StartDate: blockerDay(-3), EndDate: blockerDay(5),
			})
			f.HardEdges = []HardEdge{{ID: 200, Source: 1, Target: 2}, {ID: 201, Source: 2, Target: 1}}
		}, "interlock:1"},
		{"无环不算互锁", func(f *BlockerFacts) {
			f.Tasks = append(f.Tasks, BlockerTaskFact{
				ID: 2, Name: "地质勘察", Status: TaskInProgress, OwnerID: 6,
				KrID: 11, KrOwnerID: &kr7, StartDate: blockerDay(-3), EndDate: blockerDay(5),
			})
			f.HardEdges = []HardEdge{{ID: 200, Source: 1, Target: 2}}
		}, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := baseBlockerFacts()
			tc.mut(&f)
			got := DeriveBlockers(f)
			if tc.wantKey == "" {
				if len(got) != 0 {
					t.Fatalf("DeriveBlockers() = %d 条卡点 %+v，期望 0 条", len(got), got)
				}
				return
			}
			if findBlocker(got, tc.wantKey) == nil {
				t.Fatalf("DeriveBlockers() 未派生 %s，实际 %+v", tc.wantKey, got)
			}
		})
	}
	_ = kr7
}

// 四类卡点的待行动人判定（我的工作 PRD §8.7 待行动人列）。
func TestDeriveBlockersActionOwners(t *testing.T) {
	kr8 := int64(8)
	cases := []struct {
		name  string
		mut   func(*BlockerFacts)
		key   string
		names []string
	}{
		{"上游未就绪指向上游任务负责人", func(f *BlockerFacts) {
			f.Inputs[0].Ready = false
			f.Inputs[0].SourceTaskID = ptr64(2)
			f.Inputs[0].SourceTaskName = "地质勘察"
			f.Inputs[0].SourceOwnerID = 6
			f.Inputs[0].SourceOwnerName = "孙六"
		}, "upstream_unready:edge:100", []string{"孙六"}},
		{"来源为指定成员时指向对接人", func(f *BlockerFacts) {
			f.Inputs[0].Ready = false
			f.Inputs[0].ProviderID = 9
			f.Inputs[0].ProviderName = "吴九"
		}, "upstream_unready:edge:100", []string{"吴九"}},
		{"任务超期指向任务负责人", func(f *BlockerFacts) {
			f.Tasks[0].EndDate = blockerDay(-1)
		}, "task_overdue:1", []string{"王五"}},
		{"或签中间审核指向全部未处理审核人", func(f *BlockerFacts) {
			f.Approvals = []BlockerApprovalFact{{
				Kind: "intermediate_review", RefID: 40, TaskID: 1,
				StageSince:  blockerNow.Add(-4 * 24 * time.Hour),
				ApproverIDs: []int64{6, 9}, ApproverNames: []string{"孙六", "吴九"},
			}}
		}, "approval_timeout:intermediate_review:40", []string{"孙六", "吴九"}},
		{"互锁指向环内各任务所属 KR 负责人", func(f *BlockerFacts) {
			f.Tasks = append(f.Tasks, BlockerTaskFact{
				ID: 2, Name: "地质勘察", Status: TaskInProgress, OwnerID: 6, OwnerName: "孙六",
				KrID: 11, KrOwnerID: &kr8, KrOwnerName: "周八", StartDate: blockerDay(-3), EndDate: blockerDay(5),
			})
			f.HardEdges = []HardEdge{{ID: 200, Source: 1, Target: 2}, {ID: 201, Source: 2, Target: 1}}
		}, "interlock:1", []string{"赵七", "周八"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := baseBlockerFacts()
			tc.mut(&f)
			b := findBlocker(DeriveBlockers(f), tc.key)
			if b == nil {
				t.Fatalf("未派生 %s", tc.key)
			}
			if len(b.ActionOwnerNames) != len(tc.names) {
				t.Fatalf("待行动人 = %v，期望 %v", b.ActionOwnerNames, tc.names)
			}
			for i, want := range tc.names {
				if b.ActionOwnerNames[i] != want {
					t.Fatalf("待行动人 = %v，期望 %v", b.ActionOwnerNames, tc.names)
				}
			}
			if len(b.ActionOwnerIDs) != len(b.ActionOwnerNames) {
				t.Fatalf("待行动人 ID 与姓名数量不一致：%v / %v", b.ActionOwnerIDs, b.ActionOwnerNames)
			}
		})
	}
}

// 等级按事实严重度派生（PRD 未定义，见 DeriveBlockers 注释的口径）。
func TestDeriveBlockersLevel(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*BlockerFacts)
		key  string
		want string
	}{
		{"任务超期为高风险", func(f *BlockerFacts) { f.Tasks[0].EndDate = blockerDay(-1) }, "task_overdue:1", "high_risk"},
		{"上游未就绪且任务未超期为预警", func(f *BlockerFacts) {
			f.Inputs[0].Ready = false
			f.Inputs[0].SourceTaskID = ptr64(2)
		}, "upstream_unready:edge:100", "warning"},
		{"上游未就绪且任务已超期为高风险", func(f *BlockerFacts) {
			f.Inputs[0].Ready = false
			f.Inputs[0].SourceTaskID = ptr64(2)
			f.Tasks[0].EndDate = blockerDay(-1)
		}, "upstream_unready:edge:100", "high_risk"},
		{"审批刚超阈值为预警", func(f *BlockerFacts) {
			f.Approvals = []BlockerApprovalFact{{
				Kind: "final_review", RefID: 30, TaskID: 1,
				StageSince:  blockerNow.Add(-3 * 24 * time.Hour),
				ApproverIDs: []int64{7}, ApproverNames: []string{"赵七"},
			}}
		}, "approval_timeout:final_review:30", "warning"},
		{"审批超两倍阈值为高风险", func(f *BlockerFacts) {
			f.Approvals = []BlockerApprovalFact{{
				Kind: "final_review", RefID: 30, TaskID: 1,
				StageSince:  blockerNow.Add(-6 * 24 * time.Hour),
				ApproverIDs: []int64{7}, ApproverNames: []string{"赵七"},
			}}
		}, "approval_timeout:final_review:30", "high_risk"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := baseBlockerFacts()
			tc.mut(&f)
			b := findBlocker(DeriveBlockers(f), tc.key)
			if b == nil {
				t.Fatalf("未派生 %s", tc.key)
			}
			if b.Level != tc.want {
				t.Fatalf("Level = %q，期望 %q", b.Level, tc.want)
			}
		})
	}
}

// 互锁卡点为环内每个任务各派生一条，并给出影响范围。
func TestDeriveBlockersInterlockAndImpact(t *testing.T) {
	kr8 := int64(8)
	f := baseBlockerFacts()
	f.Tasks = append(f.Tasks, BlockerTaskFact{
		ID: 2, Name: "地质勘察", Status: TaskInProgress, OwnerID: 6, OwnerName: "孙六",
		KrID: 11, KrOwnerID: &kr8, KrOwnerName: "周八", StartDate: blockerDay(-3), EndDate: blockerDay(5),
	})
	f.HardEdges = []HardEdge{{ID: 200, Source: 1, Target: 2}, {ID: 201, Source: 2, Target: 1}}
	got := DeriveBlockers(f)
	if findBlocker(got, "interlock:1") == nil || findBlocker(got, "interlock:2") == nil {
		t.Fatalf("互锁应为环内每个任务各派生一条，实际 %+v", got)
	}

	// 影响范围：超期任务沿硬前置下游传导。
	f2 := baseBlockerFacts()
	f2.Tasks[0].EndDate = blockerDay(-1)
	f2.Tasks = append(f2.Tasks, BlockerTaskFact{
		ID: 2, Name: "地质勘察", Status: TaskInProgress, OwnerID: 6,
		KrID: 11, KrOwnerID: &kr8, StartDate: blockerDay(-3), EndDate: blockerDay(5),
	})
	f2.HardEdges = []HardEdge{{ID: 200, Source: 1, Target: 2}}
	b := findBlocker(DeriveBlockers(f2), "task_overdue:1")
	if b == nil || b.ImpactNote == "" {
		t.Fatalf("超期任务应给出下游影响说明，实际 %+v", b)
	}
}

// 一键提醒：待行动人本人不提醒自己；任务负责人、KR 负责人、可编辑项目者可提醒；访客不可。
func TestCanRemindBlocker(t *testing.T) {
	kr7 := int64(7)
	b := Blocker{
		Key: "task_overdue:1", TaskID: 1, TaskOwnerID: 5, KrOwnerID: &kr7,
		ActionOwnerIDs: []int64{5},
	}
	if CanRemindBlocker(Actor{Role: RoleMember}, 5, b) {
		t.Fatal("待行动人本人不应提醒自己")
	}
	if !CanRemindBlocker(Actor{Role: RoleMember}, 7, b) {
		t.Fatal("KR 负责人应可提醒")
	}
	if !CanRemindBlocker(Actor{Role: RoleAdmin}, 9, b) {
		t.Fatal("项目管理员应可提醒")
	}
	if CanRemindBlocker(Actor{Role: RoleMember}, 9, b) {
		t.Fatal("无关成员不应可提醒")
	}
	if CanRemindBlocker(Actor{Role: RoleViewer}, 7, b) {
		t.Fatal("访客不应可提醒")
	}
}

func ptr64(v int64) *int64 { return &v }
