package domain

import (
	"testing"
	"time"
)

// 卡点留痕（ADR 0001／0002；模块 PRD §8.7、MW-12）：卡点是读时派生、没有持久身份的，
// 出现与解除靠业务写操作前后的卡点集合按合成键 diff 得到。
func TestBlockerActivityDiff(t *testing.T) {
	now := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
	mk := func(key, kind, missing, level string) Blocker {
		return Blocker{
			Key: key, Kind: kind, TaskID: 7, TaskName: "联调验证",
			Missing: missing, Reason: "上游未交付", Level: level,
			ActionOwnerNames: []string{"周宁"}, Since: now.AddDate(0, 0, -2), OccurredAt: now,
		}
	}
	a := mk("upstream_unready:edge:1", BlockerUpstreamUnready, "接口清单", "warning")
	b := mk("task_overdue:7", BlockerTaskOverdue, "按期完成任务", "high_risk")

	cases := []struct {
		name   string
		before []Blocker
		after  []Blocker
		want   []TaskActivity
	}{
		{
			name:   "前后都没有卡点：不产生动态",
			before: nil,
			after:  nil,
			want:   []TaskActivity{},
		},
		{
			name:   "新出现的卡点记一条出现",
			before: nil,
			after:  []Blocker{a},
			want: []TaskActivity{
				{TaskID: 7, Kind: ActivityBlockerOpened, Summary: "卡点出现：上游未就绪 · 缺 接口清单", OccurredAt: now},
			},
		},
		{
			name:   "消失的卡点记一条解除",
			before: []Blocker{a},
			after:  nil,
			want: []TaskActivity{
				{TaskID: 7, Kind: ActivityBlockerResolved, Summary: "卡点解除：上游未就绪 · 缺 接口清单", OccurredAt: now},
			},
		},
		{
			name:   "始终存在的卡点不重复记",
			before: []Blocker{a},
			after:  []Blocker{a},
			want:   []TaskActivity{},
		},
		{
			name:   "同一卡点等级变化不算新事实",
			before: []Blocker{mk("upstream_unready:edge:1", BlockerUpstreamUnready, "接口清单", "warning")},
			after:  []Blocker{mk("upstream_unready:edge:1", BlockerUpstreamUnready, "接口清单", "high_risk")},
			want:   []TaskActivity{},
		},
		{
			name:   "一次写操作里可以同时出现与解除",
			before: []Blocker{a},
			after:  []Blocker{b},
			want: []TaskActivity{
				{TaskID: 7, Kind: ActivityBlockerOpened, Summary: "卡点出现：任务超期 · 缺 按期完成任务", OccurredAt: now},
				{TaskID: 7, Kind: ActivityBlockerResolved, Summary: "卡点解除：上游未就绪 · 缺 接口清单", OccurredAt: now},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BlockerActivityDiff(tc.before, tc.after, now)
			if len(got) != len(tc.want) {
				t.Fatalf("动态条数 = %d, want %d（got %+v）", len(got), len(tc.want), got)
			}
			for i := range tc.want {
				if got[i].TaskID != tc.want[i].TaskID || got[i].Kind != tc.want[i].Kind ||
					got[i].Summary != tc.want[i].Summary || !got[i].OccurredAt.Equal(tc.want[i].OccurredAt) {
					t.Errorf("第 %d 条 = %+v, want %+v", i+1, got[i], tc.want[i])
				}
				if got[i].ActorID != nil {
					t.Errorf("第 %d 条是系统派生事件，不应带行动人: %+v", i+1, got[i])
				}
			}
		})
	}
}

// 动态类型的中文名（ADR 0002 表格）；行级显示消费派生字段，前端不按枚举拼文案。
func TestActivityKindLabel(t *testing.T) {
	cases := map[string]string{
		ActivityPoolSubmitted:          "提交入池审批",
		ActivityPoolApproved:           "入池审批通过",
		ActivityPoolRejected:           "入池审批退回",
		ActivityFieldChangeSubmitted:   "提交关键字段修改",
		ActivityFieldChangeApproved:    "关键字段修改生效",
		ActivityFieldChangeRejected:    "关键字段修改退回",
		ActivityFieldChangeAbandoned:   "放弃关键字段修改",
		ActivityCompletionSubmitted:    "提交完成申请",
		ActivityCompletionApproved:     "完成审核通过",
		ActivityCompletionRejected:     "完成审核退回",
		ActivityReceiptConfirmed:       "确认接收",
		ActivityBlockerOpened:          "卡点出现",
		ActivityBlockerResolved:        "卡点解除",
		"unknown_kind_not_in_contract": "任务动态",
	}
	for kind, want := range cases {
		if got := ActivityKindLabel(kind); got != want {
			t.Errorf("ActivityKindLabel(%q) = %q, want %q", kind, got, want)
		}
	}
}
