package domain

import "testing"

// 导入记录的结果口径（§7.9、§11.1、AC-68；#80）：成功、部分失败、失败三值，
// 由本次真实写入量与失败摘要派生——失败绝不写成功（与 #53 同口径）。
func TestDeriveImportOutcome(t *testing.T) {
	cases := []struct {
		name    string
		counts  ImportCounts
		failure string
		want    string
		label   string
	}{
		{"整批写入成功", ImportCounts{Objectives: 2, KeyResults: 3, Tasks: 8}, "", ImportSuccess, "成功"},
		{"什么都没写但也没报错仍算成功", ImportCounts{}, "", ImportSuccess, "成功"},
		{"写了一部分又失败是部分失败", ImportCounts{Objectives: 1}, "第 3 行负责人不是项目成员", ImportPartial, "部分失败"},
		{"一条没写就失败是失败", ImportCounts{}, "所属 O 不存在", ImportFailed, "失败"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DeriveImportOutcome(c.counts, c.failure)
			if got != c.want {
				t.Fatalf("DeriveImportOutcome = %q, want %q", got, c.want)
			}
			if label := ImportOutcomeLabel(got); label != c.label {
				t.Fatalf("ImportOutcomeLabel(%q) = %q, want %q", got, label, c.label)
			}
		})
	}
	if label := ImportOutcomeLabel("bogus"); label != "失败" {
		t.Fatalf("未知结果不应回显枚举原文，也不应说成成功: %q", label)
	}
}
