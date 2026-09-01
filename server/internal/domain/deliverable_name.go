package domain

import (
	"strings"
	"unicode/utf8"
)

// DeliverableName 交付物项名（裁决 G1、#113）：新增交付物不再手填名称，项名取上传文件的文件名。
// 去掉最后一段扩展名——扩展名说的是文件格式，不是这件成果叫什么；换个格式重传时项名不该跟着变。
// 已有项名一律原样保留：成果更新只换内容，不改项名，否则下游引用与成果包目录里的名字会跟着漂。
func DeliverableName(currentName, fileName string) string {
	if cur := strings.TrimSpace(currentName); cur != "" {
		return cur
	}
	file := strings.TrimSpace(fileName)
	name := file
	// 首字符就是点的是隐藏文件（.gitignore），整串都是名字，不是纯扩展名。
	if i := strings.LastIndex(file, "."); i > 0 {
		name = file[:i]
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = file
	}
	if utf8.RuneCountInString(name) > deliverableNameRunes {
		name = string([]rune(name)[:deliverableNameRunes])
	}
	return name
}

// ValidateNewDeliverableName 新增交付物项的项名校验（裁决 G1、#113）：
// 同一任务下重名一律挡下，不静默新建第二项——同一件成果的新版本走已有项的「重传候选内容」。
func ValidateNewDeliverableName(name string, existing []string) error {
	if err := ValidateDeliverableName(name); err != nil {
		return err
	}
	n := strings.TrimSpace(name)
	for _, e := range existing {
		if strings.EqualFold(strings.TrimSpace(e), n) {
			return ErrDeliverableNameDuplicate
		}
	}
	return nil
}
