package domain

import "testing"

// 输入源的可读标识由已有事实派生（裁决 F1、#112）：不再让用户填「输入名称」。
// 来源为已有任务时读作「编号 · 任务名」；来源为指定项目成员时取「所需内容」摘要。
func TestEdgeDisplayName(t *testing.T) {
	long := "需要把三套核心库在多时区多语种环境下的兼容性评估结论、改造清单与回退预案一并整理成可直接执行的材料"
	cases := []struct {
		name       string
		sourceCode string
		sourceTask string
		note       string
		want       string
	}{
		{"来源任务：编号加任务名", "T1.1.2", "输出存储过程改造清单与工作量评估", "", "T1.1.2 · 输出存储过程改造清单与工作量评估"},
		{"来源任务缺编号：只读任务名", "", "输出存储过程改造清单", "", "输出存储过程改造清单"},
		{"成员来源：取所需内容", "", "", "提供三套核心库的连接串与账号口径", "提供三套核心库的连接串与账号口径"},
		{"成员来源：所需内容超长时截断到 40 字并加省略号", "", "", long, string([]rune(long)[:40]) + "…"},
		{"所需内容两端空白不计入", "", "", "  连接串口径  ", "连接串口径"},
		{"任务来源优先于所需内容", "T2.1.1", "编制割接实施方案", "顺手写的备注", "T2.1.1 · 编制割接实施方案"},
		{"两种事实都没有：给稳定的兜底文案", "", "", "", "未命名输入源"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EdgeDisplayName(tc.sourceCode, tc.sourceTask, tc.note)
			if got != tc.want {
				t.Fatalf("EdgeDisplayName(%q,%q,%q) = %q, want %q", tc.sourceCode, tc.sourceTask, tc.note, got, tc.want)
			}
		})
	}
}
