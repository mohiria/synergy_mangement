package domain

import "testing"

// RemindQuotaLeft（#129）：配额用完 → 不显示提醒按钮；多个待行动人任一人还能提醒就显示。
func TestRemindQuotaLeft(t *testing.T) {
	target := func(owners ...int64) RemindTarget {
		return RemindTarget{TaskID: 7, ActionOwnerIDs: owners}
	}
	counts := func(m map[int64]int) func(recipientID, taskID int64) int {
		return func(recipientID, taskID int64) int {
			if taskID != 7 {
				t.Fatalf("应按目标任务计数, got taskID=%d", taskID)
			}
			return m[recipientID]
		}
	}
	cases := []struct {
		name      string
		target    RemindTarget
		limit     int
		sentToday func(recipientID, taskID int64) int
		want      bool
	}{
		{"未接入计数按不限处理", target(2), 1, nil, true},
		{"配额用完不可提醒", target(2), 1, counts(map[int64]int{2: 1}), false},
		{"未用完可提醒", target(2), 2, counts(map[int64]int{2: 1}), true},
		{"多人任一人还能提醒就显示", target(2, 3), 1, counts(map[int64]int{2: 1, 3: 0}), true},
		{"多人全部用完则不显示", target(2, 3), 1, counts(map[int64]int{2: 1, 3: 1}), false},
		{"limit 非正回落默认值 1", target(2), 0, counts(map[int64]int{2: 1}), false},
		{"没有可寻址待行动人不显示", target(), 1, counts(map[int64]int{}), false},
		{"零值待行动人不参与判定", RemindTarget{TaskID: 7, ActionOwnerIDs: []int64{0}}, 1,
			counts(map[int64]int{0: 0}), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := RemindQuotaLeft(c.target, c.limit, c.sentToday); got != c.want {
				t.Fatalf("RemindQuotaLeft = %v, want %v", got, c.want)
			}
		})
	}
}
