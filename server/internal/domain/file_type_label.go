package domain

import "strings"

// fileTypeLabels 扩展名 → 类型显示文案；口径与前端既有 fileTypeLabel 一致。
var fileTypeLabels = map[string]string{
	"pdf": "PDF",
	"doc": "Word", "docx": "Word",
	"xls": "Excel", "xlsx": "Excel", "csv": "Excel",
	"ppt": "PowerPoint", "pptx": "PowerPoint",
	"png": "图片", "jpg": "图片", "jpeg": "图片", "gif": "图片", "webp": "图片",
	"zip": "ZIP",
}

// FileTypeLabel 文件类型显示文案（派生字段，裁决 J1、#142）：按扩展名归类，
// 未知类型与无扩展名统一「文件」；前端不按扩展名自己算。
func FileTypeLabel(fileName string) string {
	i := strings.LastIndex(fileName, ".")
	if i < 0 || i == len(fileName)-1 {
		return "文件"
	}
	if label, ok := fileTypeLabels[strings.ToLower(fileName[i+1:])]; ok {
		return label
	}
	return "文件"
}
