package domain

import "testing"

// 输入源的可读标识由已有事实派生（裁决 F1、#112；#178 后来源恒为任务）：
// 不再让用户填「输入名称」，读作「编号 · 任务名」。
func TestEdgeDisplayName(t *testing.T) {
	cases := []struct {
		name       string
		sourceCode string
		sourceTask string
		want       string
	}{
		{"来源任务：编号加任务名", "T1.1.2", "输出存储过程改造清单与工作量评估", "T1.1.2 · 输出存储过程改造清单与工作量评估"},
		{"来源任务缺编号：只读任务名", "", "输出存储过程改造清单", "输出存储过程改造清单"},
		{"事实缺失：给稳定的兜底文案", "", "", "未命名输入源"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EdgeDisplayName(tc.sourceCode, tc.sourceTask)
			if got != tc.want {
				t.Fatalf("EdgeDisplayName(%q,%q) = %q, want %q", tc.sourceCode, tc.sourceTask, got, tc.want)
			}
		})
	}
}
