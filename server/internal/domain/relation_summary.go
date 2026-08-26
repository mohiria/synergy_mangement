package domain

// RelationEdgeFact 派生协作关系摘要所需的单条交付物边事实（词汇表「协作关系摘要」）。
type RelationEdgeFact struct {
	EdgeType     string
	SourceTaskID *int64 // 来源为指定项目成员时为空
	TargetTaskID int64
	Ready        bool
}

// TaskRelationRef 摘要中的一条直接协作关系：对方任务、关系类型与就绪事实。
type TaskRelationRef struct {
	TaskID   int64
	EdgeType string
	Ready    bool
}

// RelationSummary 派生任务概况的协作关系摘要（PRD §7.5、词汇表「协作关系摘要」）：
// 按直接上游／直接下游分组，只收对方为系统内任务的关系（指定项目成员输入没有对方任务）。
// 摘要不插入交付物中间节点，因此同一对方任务的同一关系类型合并为一条，
// 全部边就绪才算就绪；分组内按首次出现的边顺序稳定排列，无关系时为空切片。
func RelationSummary(taskID int64, edges []RelationEdgeFact) (upstream, downstream []TaskRelationRef) {
	upstream, downstream = []TaskRelationRef{}, []TaskRelationRef{}
	upIdx := make(map[TaskRelationRef]int)
	downIdx := make(map[TaskRelationRef]int)
	merge := func(group *[]TaskRelationRef, idx map[TaskRelationRef]int, other int64, edgeType string, ready bool) {
		key := TaskRelationRef{TaskID: other, EdgeType: edgeType}
		if i, ok := idx[key]; ok {
			(*group)[i].Ready = (*group)[i].Ready && ready
			return
		}
		idx[key] = len(*group)
		*group = append(*group, TaskRelationRef{TaskID: other, EdgeType: edgeType, Ready: ready})
	}
	for _, e := range edges {
		if e.SourceTaskID == nil {
			continue
		}
		switch {
		case e.TargetTaskID == taskID:
			merge(&upstream, upIdx, *e.SourceTaskID, e.EdgeType, e.Ready)
		case *e.SourceTaskID == taskID:
			merge(&downstream, downIdx, e.TargetTaskID, e.EdgeType, e.Ready)
		}
	}
	return upstream, downstream
}
