package domain

import (
	"testing"
	"time"
)

func cst(y int, m time.Month, d, h int) time.Time {
	return time.Date(y, m, d, h, 0, 0, 0, ProjectLocation)
}

// 日期型字段（开始日、截止日）是「哪一天」而不是「哪一刻」：
// 截止日当天用户还有一整天工期，次日零点（项目时区）起才算超期（MW-15 · §5.4）。
// 回归背景：此前直接写 now.After(*t.EndDate)，而 DATE 被扫成 UTC 零点，
// 截止日当天 00:00（东八区 08:00）就判超期，「今天到期」这一排序层级因此恒空。
func TestOverdue(t *testing.T) {
	due := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC) // pgx 扫 DATE 的形态
	cases := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"截止日前一天", cst(2026, 9, 29, 12), false},
		{"截止日当天零点", cst(2026, 9, 30, 0), false},
		{"截止日当天傍晚", cst(2026, 9, 30, 18), false},
		{"截止日当天 23:59", time.Date(2026, 9, 30, 23, 59, 0, 0, ProjectLocation), false},
		{"次日零点", cst(2026, 10, 1, 0), true},
		{"次日白天", cst(2026, 10, 1, 9), true},
		{"UTC 表示的次日零点（等值时刻）", time.Date(2026, 9, 30, 16, 0, 0, 0, time.UTC), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Overdue(&due, tc.now); got != tc.want {
				t.Fatalf("Overdue(2026-09-30, %v) = %v, want %v", tc.now, got, tc.want)
			}
		})
	}
	if Overdue(nil, cst(2030, 1, 1, 0)) {
		t.Fatalf("无截止日不应判超期")
	}
}

func TestDueToday(t *testing.T) {
	due := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	if !DueToday(&due, cst(2026, 9, 30, 8)) {
		t.Fatalf("截止日当天应判「今天到期」")
	}
	if DueToday(&due, cst(2026, 9, 29, 23)) {
		t.Fatalf("前一天不应判「今天到期」")
	}
	if DueToday(&due, cst(2026, 10, 1, 0)) {
		t.Fatalf("次日不应判「今天到期」")
	}
}

// 开始日同理：当天零点（项目时区）起算已开始。
func TestStarted(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if Started(&start, cst(2026, 8, 31, 23)) {
		t.Fatalf("开始日前不应判已开始")
	}
	if !Started(&start, cst(2026, 9, 1, 0)) {
		t.Fatalf("开始日当天零点应判已开始")
	}
	if Started(nil, cst(2030, 1, 1, 0)) {
		t.Fatalf("无开始日不应判已开始")
	}
}
