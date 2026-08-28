package domain

import "time"

// ProjectLocation 项目时区。日期型字段（开始日、截止日）表达的是「哪一天」，
// 判定必须落在这个时区上；数据库把 DATE 扫成 UTC 零点，直接与时刻比较会整体漂移。
// V1 全系统单一时区，项目级时区配置未定义前取常量（PRD §5.4）。
var ProjectLocation = time.FixedZone("CST", 8*60*60)

// dayStart 取该日期在项目时区的零点：只用日期部分，忽略扫库带来的时刻。
func dayStart(d time.Time) time.Time {
	y, m, day := d.Date()
	return time.Date(y, m, day, 0, 0, 0, 0, ProjectLocation)
}

// Overdue 判定截止日是否已过：截止日当天不算，次日零点（项目时区）起算超期。
func Overdue(due *time.Time, now time.Time) bool {
	if due == nil {
		return false
	}
	return !now.Before(dayStart(*due).AddDate(0, 0, 1))
}

// DueToday 判定是否今天到期（项目时区同一自然日）。
func DueToday(due *time.Time, now time.Time) bool {
	if due == nil {
		return false
	}
	return dayStart(*due).Equal(dayStart(now.In(ProjectLocation)))
}

// Started 判定开始日是否已到：当天零点（项目时区）起算已开始。
func Started(start *time.Time, now time.Time) bool {
	if start == nil {
		return false
	}
	return !now.Before(dayStart(*start))
}
