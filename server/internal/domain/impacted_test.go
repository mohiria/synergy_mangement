package domain

import "testing"

// 协作关系 PRD §8.1：受影响 O／KR 只沿下游硬前置交付物边推导，反馈、参考输入与
// 普通双向协作不自动计入；多级传递、环形关系不死循环、同一 KR 去重。
func TestImpactedObjectives(t *testing.T) {
	// 任务 1 →(硬前置) 2 →(硬前置) 3；1 →(反馈) 4；1 →(信息) 5；2 →(硬前置，参考必要性) 6。
	tasks := map[int64]ImpactTaskFact{
		1: {TaskID: 1, KeyResultID: 10, KrDescription: "上线自动验收", ObjectiveID: 100, ObjectiveTitle: "提升交付质量"},
		2: {TaskID: 2, KeyResultID: 11, KrDescription: "现场回归通过", ObjectiveID: 100, ObjectiveTitle: "提升交付质量"},
		3: {TaskID: 3, KeyResultID: 12, KrDescription: "指标达标", ObjectiveID: 200, ObjectiveTitle: "提升客户满意"},
		4: {TaskID: 4, KeyResultID: 13, KrDescription: "反馈闭环", ObjectiveID: 200, ObjectiveTitle: "提升客户满意"},
		5: {TaskID: 5, KeyResultID: 14, KrDescription: "信息同步", ObjectiveID: 200, ObjectiveTitle: "提升客户满意"},
		6: {TaskID: 6, KeyResultID: 11, KrDescription: "现场回归通过", ObjectiveID: 100, ObjectiveTitle: "提升交付质量"},
	}
	edges := []ImpactEdgeFact{
		{SourceTaskID: i64(1), TargetTaskID: 2, EdgeType: EdgeHardPrerequisite},
		{SourceTaskID: i64(2), TargetTaskID: 3, EdgeType: EdgeHardPrerequisite},
		{SourceTaskID: i64(1), TargetTaskID: 4, EdgeType: EdgeFeedback},
		{SourceTaskID: i64(1), TargetTaskID: 5, EdgeType: EdgeInformation},
		{SourceTaskID: i64(2), TargetTaskID: 6, EdgeType: EdgeHardPrerequisite},
		// 成员来源的边没有对方任务，不参与推导。
		{SourceTaskID: nil, TargetTaskID: 1, EdgeType: EdgeHardPrerequisite},
	}

	got := ImpactedObjectives(1, tasks, edges)
	// 途经任务 2、3、6，所属 KR 为 11、12、11 —— 同一 KR 去重后应是两条。
	if len(got) != 2 {
		t.Fatalf("受影响 KR 数量 = %d，期望 2（KR11 与 KR12）: %+v", len(got), got)
	}
	// 顺序：按广度优先第一次到达的顺序稳定输出。
	want := []struct {
		kr  int64
		obj int64
	}{{11, 100}, {12, 200}}
	if len(got) < 2 {
		t.Fatalf("受影响项过少: %+v", got)
	}
	seen := map[int64]int64{}
	for _, it := range got {
		seen[it.KeyResultID] = it.ObjectiveID
	}
	for _, w := range want {
		if seen[w.kr] != w.obj {
			t.Fatalf("KR%d 应归属 O%d: %+v", w.kr, w.obj, got)
		}
	}
	for _, forbidden := range []int64{13, 14} {
		if _, bad := seen[forbidden]; bad {
			t.Fatalf("反馈／信息边不应计入受影响目标: %+v", got)
		}
	}
	// 起点任务自己所属的 KR 不算「受影响」——那是所属，不是被影响。
	if _, bad := seen[10]; bad {
		t.Fatalf("起点任务所属 KR 不应出现在受影响目标: %+v", got)
	}
}

// 环形硬前置关系不能让推导死循环，且不把起点 KR 算回来。
func TestImpactedObjectivesCycle(t *testing.T) {
	tasks := map[int64]ImpactTaskFact{
		1: {TaskID: 1, KeyResultID: 10, ObjectiveID: 100},
		2: {TaskID: 2, KeyResultID: 11, ObjectiveID: 100},
	}
	edges := []ImpactEdgeFact{
		{SourceTaskID: i64(1), TargetTaskID: 2, EdgeType: EdgeHardPrerequisite},
		{SourceTaskID: i64(2), TargetTaskID: 1, EdgeType: EdgeHardPrerequisite},
	}
	got := ImpactedObjectives(1, tasks, edges)
	if len(got) != 1 || got[0].KeyResultID != 11 {
		t.Fatalf("环形关系推导异常: %+v", got)
	}
}

// 没有下游硬前置边时返回空切片（而不是 nil），前端可直接判空态。
func TestImpactedObjectivesEmpty(t *testing.T) {
	got := ImpactedObjectives(1, map[int64]ImpactTaskFact{1: {TaskID: 1, KeyResultID: 10, ObjectiveID: 100}}, nil)
	if got == nil || len(got) != 0 {
		t.Fatalf("无下游硬前置边应返回空切片: %+v", got)
	}
}
