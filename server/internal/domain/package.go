package domain

import "time"

// 裁决 G1（#140）：阶段成果包整体移除；本文件仅保留归档时间窗判定 InArchiveWindow。

// InArchiveWindow 判定一条归档记录是否落在「时间」筛选区间内（§7.7 六个筛选维度之一，AC-17）。
// from／to 是日期（date-only），闭区间——终点当天整天都算在内（否则用户选到今天却看不到今天的东西）。
// at 是时刻，先换算到 loc 时区取日历日再比（loc 为 nil 按服务器本地时区）：
// 日期与瞬时直接比会在时区差窗口里把「本地今天凌晨」的内容排除在「今天」之外。
// 任一端为空表示该侧不限；两端都为空时一律通过。
// 没有时间的项（既无当前内容也无候选、或任务文件时间缺失）在给了区间后不返回：
// 它无法证明自己落在区间里，混进结果只会让「按时间筛」失去意义。
func InArchiveWindow(at, from, to *time.Time, loc *time.Location) bool {
	if from == nil && to == nil {
		return true
	}
	if at == nil {
		return false
	}
	if loc == nil {
		loc = time.Local
	}
	// 统一压成「日历日」（loc 时区下的 0 点）再比：from／to 本身是 date-only，
	// 取其年月日即日历日；at 先换算进 loc 再取日历日。
	calDay := func(v time.Time) time.Time {
		return time.Date(v.Year(), v.Month(), v.Day(), 0, 0, 0, 0, time.UTC)
	}
	atDay := calDay(at.In(loc))
	if from != nil && atDay.Before(calDay(*from)) {
		return false
	}
	if to != nil && atDay.After(calDay(*to)) {
		return false
	}
	return true
}

