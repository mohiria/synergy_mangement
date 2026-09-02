package domain

import (
	"strings"
)

// EdgeDisplayName 输入源的可读标识（裁决 F1、#112；#178 后来源恒为任务）：
// 由已有事实派生，不让用户另填「输入名称」，读作「编号 · 任务名」。
// 事实缺失时给一句稳定兜底，避免界面上出现空白标识。
func EdgeDisplayName(sourceTaskCode, sourceTaskName string) string {
	code := strings.TrimSpace(sourceTaskCode)
	task := strings.TrimSpace(sourceTaskName)
	if task == "" {
		return "未命名输入源"
	}
	if code != "" {
		return code + " · " + task
	}
	return task
}
