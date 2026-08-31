package domain

import (
	"testing"
	"time"
)

// SelectTopBlocker（#122）：等级高者优先，同级按等待更久（Since 更早）优先，再同按 Key 稳定。
func TestSelectTopBlocker(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	mk := func(key, level string, since time.Time) Blocker {
		return Blocker{Key: key, Kind: BlockerTaskOverdue, Level: level, Since: since}
	}
	cases := []struct {
		name     string
		blockers []Blocker
		wantKey  string
		wantNil  bool
	}{
		{name: "无卡点返回 nil", blockers: nil, wantNil: true},
		{
			name: "等级高者优先，不看天数",
			blockers: []Blocker{
				mk("w-old", RiskWarning, base.AddDate(0, 0, -30)),
				mk("h-new", RiskHighRisk, base.AddDate(0, 0, -1)),
			},
			wantKey: "h-new",
		},
		{
			name: "同级按等待更久（Since 更早）优先",
			blockers: []Blocker{
				mk("h-3d", RiskHighRisk, base.AddDate(0, 0, -3)),
				mk("h-15d", RiskHighRisk, base.AddDate(0, 0, -15)),
			},
			wantKey: "h-15d",
		},
		{
			name: "等级与天数都同则按 Key 字典序稳定",
			blockers: []Blocker{
				mk("b-key", RiskWarning, base.AddDate(0, 0, -5)),
				mk("a-key", RiskWarning, base.AddDate(0, 0, -5)),
			},
			wantKey: "a-key",
		},
		{
			name: "单条直接返回",
			blockers: []Blocker{
				mk("only", RiskWarning, base),
			},
			wantKey: "only",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SelectTopBlocker(tc.blockers)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("want nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("want %s, got nil", tc.wantKey)
			}
			if got.Key != tc.wantKey {
				t.Fatalf("want %s, got %s", tc.wantKey, got.Key)
			}
		})
	}
}
