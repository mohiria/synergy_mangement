package domain

import "testing"

// 裁决 J1（#142）：关系列表「当前交付物」列显示「文件类型 · 大小」，
// 类型文案由服务端派生，前端不按扩展名自己算。口径与前端既有 fileTypeLabel 一致。
func TestFileTypeLabel(t *testing.T) {
	cases := []struct {
		fileName string
		want     string
	}{
		{"报告.pdf", "PDF"},
		{"报告.PDF", "PDF"},
		{"方案.doc", "Word"},
		{"方案.docx", "Word"},
		{"清单.xls", "Excel"},
		{"清单.xlsx", "Excel"},
		{"清单.csv", "Excel"},
		{"汇报.ppt", "PowerPoint"},
		{"汇报.pptx", "PowerPoint"},
		{"截图.png", "图片"},
		{"截图.jpg", "图片"},
		{"截图.jpeg", "图片"},
		{"截图.gif", "图片"},
		{"截图.webp", "图片"},
		{"打包.zip", "ZIP"},
		{"未知.bin", "文件"},
		{"无扩展名", "文件"},
	}
	for _, c := range cases {
		if got := FileTypeLabel(c.fileName); got != c.want {
			t.Fatalf("FileTypeLabel(%q) = %q, want %q", c.fileName, got, c.want)
		}
	}
}
