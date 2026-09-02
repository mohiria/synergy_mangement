package domain

import "testing"

// #150 风险队列「未就绪摘要」口径（模块 PRD §5.2、§6.1、CR-22；裁决 15 #185 修订）：
// 只计 KR 下未关闭任务作为接收方的未就绪**必要**交付物边——参考边退出风险识别，
// 不再计入；已完成、已取消的接收方不再等待输入，其输入边不计，
// 因此计数不随「显示已完成」开关摆动（§7.2）。
func TestCountNotReadyInputs(t *testing.T) {
	cases := []struct {
		name   string
		inputs []KrInputFact
		want   int
	}{
		{name: "空 KR 无输入边", inputs: nil, want: 0},
		{
			name: "全部就绪计 0",
			inputs: []KrInputFact{
				{TargetStatus: TaskInProgress, Ready: true, Necessity: NecessityRequired},
				{TargetStatus: TaskNotStarted, Ready: true, Necessity: NecessityRequired},
			},
			want: 0,
		},
		{
			name: "混合：只计未就绪的必要边",
			inputs: []KrInputFact{
				{TargetStatus: TaskInProgress, Ready: false, Necessity: NecessityRequired},
				{TargetStatus: TaskInProgress, Ready: true, Necessity: NecessityRequired},
				{TargetStatus: TaskWaitingInput, Ready: false, Necessity: NecessityRequired},
			},
			want: 2,
		},
		{
			name: "参考未就绪不计入未就绪摘要（裁决 15）",
			inputs: []KrInputFact{
				{TargetStatus: TaskInProgress, Ready: false, Necessity: NecessityReference},
				{TargetStatus: TaskInProgress, Ready: false, Necessity: NecessityRequired},
			},
			want: 1,
		},
		{
			name: "接收方已完成不计（开关两态下口径不变）",
			inputs: []KrInputFact{
				{TargetStatus: TaskCompleted, Ready: false, Necessity: NecessityRequired},
				{TargetStatus: TaskInProgress, Ready: false, Necessity: NecessityRequired},
			},
			want: 1,
		},
		{
			name: "接收方已取消不计",
			inputs: []KrInputFact{
				{TargetStatus: TaskCancelled, Ready: false, Necessity: NecessityRequired},
			},
			want: 0,
		},
		{
			name: "未开始与等待输入的接收方照常计",
			inputs: []KrInputFact{
				{TargetStatus: TaskNotStarted, Ready: false, Necessity: NecessityRequired},
				{TargetStatus: TaskWaitingInput, Ready: false, Necessity: NecessityRequired},
			},
			want: 2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CountNotReadyInputs(tc.inputs); got != tc.want {
				t.Fatalf("CountNotReadyInputs() = %d, want %d", got, tc.want)
			}
		})
	}
}

// 裁决 15（#185）词条「提醒」：上游未就绪但不构成卡点的中性提示。
// KR 的提醒计数含参考边——风险队列第二档「KR 编号 · 提醒」由它派生；
// 终态接收方同样不计。
func TestCountUnreadyReminders(t *testing.T) {
	cases := []struct {
		name   string
		inputs []KrInputFact
		want   int
	}{
		{name: "无输入边", inputs: nil, want: 0},
		{
			name: "必要与参考的未就绪都计",
			inputs: []KrInputFact{
				{TargetStatus: TaskInProgress, Ready: false, Necessity: NecessityRequired},
				{TargetStatus: TaskInProgress, Ready: false, Necessity: NecessityReference},
				{TargetStatus: TaskInProgress, Ready: true, Necessity: NecessityReference},
			},
			want: 2,
		},
		{
			name: "终态接收方不计",
			inputs: []KrInputFact{
				{TargetStatus: TaskCompleted, Ready: false, Necessity: NecessityReference},
				{TargetStatus: TaskCancelled, Ready: false, Necessity: NecessityRequired},
			},
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CountUnreadyReminders(tc.inputs); got != tc.want {
				t.Fatalf("CountUnreadyReminders() = %d, want %d", got, tc.want)
			}
		})
	}
}
