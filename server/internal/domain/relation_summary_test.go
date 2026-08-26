package domain

import "testing"

// PRD §7.5、词汇表「协作关系摘要」：任务概况按直接上游／直接下游分组展示直接协作关系；
// 只展示对方为系统内任务的关系（指定项目成员输入没有对方任务），不插入交付物中间节点，
// 因此同一对方任务的同一关系类型合并为一条，全部边就绪才算就绪。
func TestRelationSummary(t *testing.T) {
	cases := []struct {
		name           string
		taskID         int64
		edges          []RelationEdgeFact
		wantUpstream   []TaskRelationRef
		wantDownstream []TaskRelationRef
	}{
		{
			name:           "没有关系时两组均为空",
			taskID:         1,
			edges:          nil,
			wantUpstream:   []TaskRelationRef{},
			wantDownstream: []TaskRelationRef{},
		},
		{
			name:   "指定项目成员输入没有对方任务，不进入上游摘要",
			taskID: 1,
			edges: []RelationEdgeFact{
				{EdgeType: EdgeHandover, SourceTaskID: nil, TargetTaskID: 1, Ready: false},
			},
			wantUpstream:   []TaskRelationRef{},
			wantDownstream: []TaskRelationRef{},
		},
		{
			name:   "来源任务进上游、本任务为来源的进下游",
			taskID: 1,
			edges: []RelationEdgeFact{
				{EdgeType: EdgeHardPrerequisite, SourceTaskID: i64(2), TargetTaskID: 1, Ready: true},
				{EdgeType: EdgeHandover, SourceTaskID: i64(1), TargetTaskID: 3, Ready: false},
			},
			wantUpstream:   []TaskRelationRef{{TaskID: 2, EdgeType: EdgeHardPrerequisite, Ready: true}},
			wantDownstream: []TaskRelationRef{{TaskID: 3, EdgeType: EdgeHandover, Ready: false}},
		},
		{
			name:   "同一对方任务同一关系类型的多条边合并，任一未就绪即未就绪",
			taskID: 1,
			edges: []RelationEdgeFact{
				{EdgeType: EdgeHardPrerequisite, SourceTaskID: i64(2), TargetTaskID: 1, Ready: true},
				{EdgeType: EdgeHardPrerequisite, SourceTaskID: i64(2), TargetTaskID: 1, Ready: false},
			},
			wantUpstream:   []TaskRelationRef{{TaskID: 2, EdgeType: EdgeHardPrerequisite, Ready: false}},
			wantDownstream: []TaskRelationRef{},
		},
		{
			name:   "同一对方任务的不同关系类型各成一条，按首次出现排序",
			taskID: 1,
			edges: []RelationEdgeFact{
				{EdgeType: EdgeInformation, SourceTaskID: i64(2), TargetTaskID: 1, Ready: true},
				{EdgeType: EdgeHardPrerequisite, SourceTaskID: i64(2), TargetTaskID: 1, Ready: true},
			},
			wantUpstream: []TaskRelationRef{
				{TaskID: 2, EdgeType: EdgeInformation, Ready: true},
				{TaskID: 2, EdgeType: EdgeHardPrerequisite, Ready: true},
			},
			wantDownstream: []TaskRelationRef{},
		},
		{
			name:   "双向关系同时出现在上游与下游",
			taskID: 1,
			edges: []RelationEdgeFact{
				{EdgeType: EdgeHandover, SourceTaskID: i64(2), TargetTaskID: 1, Ready: true},
				{EdgeType: EdgeFeedback, SourceTaskID: i64(1), TargetTaskID: 2, Ready: false},
			},
			wantUpstream:   []TaskRelationRef{{TaskID: 2, EdgeType: EdgeHandover, Ready: true}},
			wantDownstream: []TaskRelationRef{{TaskID: 2, EdgeType: EdgeFeedback, Ready: false}},
		},
		{
			name:   "与本任务无关的边不进入任一分组",
			taskID: 1,
			edges: []RelationEdgeFact{
				{EdgeType: EdgeHandover, SourceTaskID: i64(2), TargetTaskID: 3, Ready: true},
			},
			wantUpstream:   []TaskRelationRef{},
			wantDownstream: []TaskRelationRef{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			up, down := RelationSummary(tc.taskID, tc.edges)
			assertRefs(t, "上游", up, tc.wantUpstream)
			assertRefs(t, "下游", down, tc.wantDownstream)
		})
	}
}

func assertRefs(t *testing.T, group string, got, want []TaskRelationRef) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s 关系条数 = %d, want %d（got %+v）", group, len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s 第 %d 条 = %+v, want %+v", group, i+1, got[i], want[i])
		}
	}
}
