package domain

import "testing"

// #150 风险队列「未就绪摘要」口径（模块 PRD §5.2、§6.1、CR-22）：
// 计 KR 下未关闭任务作为接收方的全部未就绪交付物边——参考型输入一并计入
// （摘要提示口径，与硬前置卡任务口径无关）；已完成、已取消的接收方不再等待输入，
// 其输入边不计，因此计数不随「显示已完成」开关摆动（§7.2）。
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
				{TargetStatus: TaskInProgress, Ready: true},
				{TargetStatus: TaskNotStarted, Ready: true},
			},
			want: 0,
		},
		{
			name: "混合：只计未就绪的",
			inputs: []KrInputFact{
				{TargetStatus: TaskInProgress, Ready: false},
				{TargetStatus: TaskInProgress, Ready: true},
				{TargetStatus: TaskWaitingInput, Ready: false},
			},
			want: 2,
		},
		{
			name: "接收方已完成不计（开关两态下口径不变）",
			inputs: []KrInputFact{
				{TargetStatus: TaskCompleted, Ready: false},
				{TargetStatus: TaskInProgress, Ready: false},
			},
			want: 1,
		},
		{
			name: "接收方已取消不计",
			inputs: []KrInputFact{
				{TargetStatus: TaskCancelled, Ready: false},
			},
			want: 0,
		},
		{
			name: "草稿与待入池的接收方照常计",
			inputs: []KrInputFact{
				{TargetStatus: TaskDraft, Ready: false},
				{TargetStatus: TaskPendingPoolReview, Ready: false},
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
