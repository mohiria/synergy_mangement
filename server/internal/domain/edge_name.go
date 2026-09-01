package domain

import (
	"strings"
	"unicode/utf8"
)

// 「所需内容」摘要的长度上限：够读出这条输入要什么，又不至于把一行事实撑成一段话。
const edgeNoteSummaryRunes = 40

// EdgeDisplayName 输入源的可读标识（裁决 F1、#112）：由已有事实派生，不让用户另填「输入名称」。
// 来源是已有任务时读作「编号 · 任务名」；来源是指定项目成员时取「所需内容」的摘要。
// 两种事实都没有时给一句稳定兜底，避免界面上出现空白标识。
func EdgeDisplayName(sourceTaskCode, sourceTaskName, contentNote string) string {
	code := strings.TrimSpace(sourceTaskCode)
	task := strings.TrimSpace(sourceTaskName)
	if task != "" {
		if code != "" {
			return code + " · " + task
		}
		return task
	}
	note := strings.TrimSpace(contentNote)
	if note == "" {
		return "未命名输入源"
	}
	if utf8.RuneCountInString(note) > edgeNoteSummaryRunes {
		return string([]rune(note)[:edgeNoteSummaryRunes]) + "…"
	}
	return note
}
