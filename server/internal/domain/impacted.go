package domain

// 受影响 O／KR（协作关系 PRD §8.1）。

// ImpactTaskFact 推导受影响目标所需的任务归属事实。
type ImpactTaskFact struct {
	TaskID         int64
	KeyResultID    int64
	KrDescription  string
	ObjectiveID    int64
	ObjectiveTitle string
}

// ImpactEdgeFact 推导用的交付物边事实（#173 裁决：关系类型删除，只看必要性）。
type ImpactEdgeFact struct {
	SourceTaskID *int64
	TargetTaskID int64
	Necessity    string
}

// ImpactedTarget 一条受影响的 O／KR（系统推导）。
type ImpactedTarget struct {
	KeyResultID    int64
	KrDescription  string
	ObjectiveID    int64
	ObjectiveTitle string
}

// ImpactedObjectives 派生受影响 O／KR（协作关系 PRD §8.1；#173 裁决修订）：
// 从当前任务出发沿下游「必要」交付物边做传递闭包，收集途经任务所属的 KR 及其 O。
// 只认必要——参考输入表达的是协作参考而不是「我不交付你就做不了」，
// 自动把它算成受影响会让影响面失真；成员来源的边没有对方任务，同样不参与。
// 起点任务自己所属的 KR 是「所属」而非「受影响」，即便被环形关系绕回来也不收。
// 同一 KR 去重，按广度优先首次到达的顺序稳定输出；无结果时返回空切片。
func ImpactedObjectives(taskID int64, tasks map[int64]ImpactTaskFact, edges []ImpactEdgeFact) []ImpactedTarget {
	downstream := make(map[int64][]int64, len(edges))
	for _, e := range edges {
		if e.SourceTaskID == nil || e.Necessity != NecessityRequired {
			continue
		}
		downstream[*e.SourceTaskID] = append(downstream[*e.SourceTaskID], e.TargetTaskID)
	}
	out := []ImpactedTarget{}
	visited := map[int64]bool{taskID: true}
	seenKr := map[int64]bool{}
	if origin, ok := tasks[taskID]; ok {
		seenKr[origin.KeyResultID] = true
	}
	queue := append([]int64{}, downstream[taskID]...)
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if visited[cur] {
			continue
		}
		visited[cur] = true
		if t, ok := tasks[cur]; ok && !seenKr[t.KeyResultID] {
			seenKr[t.KeyResultID] = true
			out = append(out, ImpactedTarget{
				KeyResultID:    t.KeyResultID,
				KrDescription:  t.KrDescription,
				ObjectiveID:    t.ObjectiveID,
				ObjectiveTitle: t.ObjectiveTitle,
			})
		}
		queue = append(queue, downstream[cur]...)
	}
	return out
}
