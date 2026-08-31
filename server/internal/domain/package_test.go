package domain

import (
	"testing"
	"time"
)

// §7.7 统一归档的「时间」筛选维（AC-17、#86）：按日期闭区间裁剪，端点当天算在内；
// 没有时间的项在给了区间后不返回——它无法证明自己落在区间里。
func TestInArchiveWindow(t *testing.T) {
	day := func(s string) time.Time {
		v, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatalf("bad date %q: %v", s, err)
		}
		return v
	}
	at := func(s string) *time.Time {
		v := day(s)
		return &v
	}
	from, to := day("2026-09-10"), day("2026-09-20")
	// 跨时区窗口用例用固定 +08 时区，不依赖跑测试机器的本地时区。
	cst := time.FixedZone("UTC+8", 8*3600)
	utcAt := func(s string) *time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatalf("bad time %q: %v", s, err)
		}
		return &v
	}
	sep1 := day("2026-09-01")
	cases := []struct {
		name string
		at   *time.Time
		from *time.Time
		to   *time.Time
		loc  *time.Location
		want bool
	}{
		{"不给区间时一律通过", at("2026-01-01"), nil, nil, nil, true},
		{"无时间且不给区间也通过", nil, nil, nil, nil, true},
		{"区间内", at("2026-09-15"), &from, &to, nil, true},
		{"起点当天算在内", at("2026-09-10"), &from, &to, nil, true},
		{"终点当天算在内", at("2026-09-20"), &from, &to, nil, true},
		{"终点当天的晚些时刻也算在内", func() *time.Time { v := day("2026-09-20").Add(23 * time.Hour); return &v }(), &from, &to, nil, true},
		{"早于起点", at("2026-09-09"), &from, &to, nil, false},
		{"晚于终点", at("2026-09-21"), &from, &to, nil, false},
		{"只给起点", at("2026-12-01"), &from, nil, nil, true},
		{"只给终点", at("2026-12-01"), nil, &to, nil, false},
		{"无时间的项在给了区间后不返回", nil, &from, nil, nil, false},
		// 时区窗口（本地已过午夜、UTC 还在昨天）：按 loc 的日历日判定，不按瞬时。
		{"本地今天凌晨的内容算在本地今天", utcAt("2026-08-31T16:20:00Z"), &sep1, &sep1, cst, true},
		{"本地已是明天凌晨的内容不算今天", utcAt("2026-09-01T17:00:00Z"), &sep1, &sep1, cst, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			loc := c.loc
			if loc == nil {
				loc = time.UTC
			}
			if got := InArchiveWindow(c.at, c.from, c.to, loc); got != c.want {
				t.Fatalf("InArchiveWindow = %v, want %v", got, c.want)
			}
		})
	}
}
