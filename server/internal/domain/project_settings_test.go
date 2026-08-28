package domain

import (
	"errors"
	"testing"
)

// AC-60：规则设置三项均有默认值（审批超时 3 天、临期 3 天、提醒冷却每天 1 次）。
func TestDefaultProjectSettings(t *testing.T) {
	got := DefaultProjectSettings()
	want := ProjectSettings{ApprovalTimeoutDays: 3, DueSoonDays: 3, RemindDailyLimit: 1}
	if got != want {
		t.Fatalf("默认规则设置 = %+v，期望 %+v", got, want)
	}
}

// 读路径兜底：库里缺值或历史数据为 0 时回落默认值，不让 0 阈值把全部审批件判成超时。
func TestNormalizeProjectSettings(t *testing.T) {
	cases := []struct {
		name string
		in   ProjectSettings
		want ProjectSettings
	}{
		{"全缺回落默认", ProjectSettings{}, ProjectSettings{3, 3, 1}},
		{"负数回落默认", ProjectSettings{-1, -2, -3}, ProjectSettings{3, 3, 1}},
		{"已配置值原样保留", ProjectSettings{1, 7, 5}, ProjectSettings{1, 7, 5}},
		{"部分缺失只补缺失项", ProjectSettings{ApprovalTimeoutDays: 10}, ProjectSettings{10, 3, 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeProjectSettings(tc.in); got != tc.want {
				t.Fatalf("NormalizeProjectSettings(%+v) = %+v，期望 %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestValidateProjectSettings(t *testing.T) {
	ok := ProjectSettings{ApprovalTimeoutDays: 3, DueSoonDays: 3, RemindDailyLimit: 1}
	cases := []struct {
		name string
		in   ProjectSettings
		want error
	}{
		{"默认值合法", ok, nil},
		{"边界下限合法", ProjectSettings{1, 1, 1}, nil},
		{"边界上限合法", ProjectSettings{30, 30, 20}, nil},
		{"审批超时为零", ProjectSettings{0, 3, 1}, ErrApprovalTimeoutDaysRange},
		{"审批超时超上限", ProjectSettings{31, 3, 1}, ErrApprovalTimeoutDaysRange},
		{"临期阈值为零", ProjectSettings{3, 0, 1}, ErrDueSoonDaysRange},
		{"临期阈值超上限", ProjectSettings{3, 31, 1}, ErrDueSoonDaysRange},
		{"提醒次数为零", ProjectSettings{3, 3, 0}, ErrRemindDailyLimitRange},
		{"提醒次数超上限", ProjectSettings{3, 3, 21}, ErrRemindDailyLimitRange},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateProjectSettings(tc.in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("ValidateProjectSettings(%+v) = %v，期望 %v", tc.in, err, tc.want)
			}
		})
	}
}

// 主 PRD §7.9：规则设置仅项目管理员可改；项目负责人享有同等权限。
func TestCanEditProjectSettings(t *testing.T) {
	cases := []struct {
		name  string
		actor Actor
		want  bool
	}{
		{"项目管理员可改", Actor{Role: RoleAdmin}, true},
		{"项目负责人可改", Actor{IsOwner: true, Role: RoleMember}, true},
		{"普通成员不可改", Actor{Role: RoleMember}, false},
		{"只读成员不可改", Actor{Role: RoleViewer}, false},
		{"非成员不可改", Actor{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanEditProjectSettings(tc.actor); got != tc.want {
				t.Fatalf("CanEditProjectSettings(%+v) = %v，期望 %v", tc.actor, got, tc.want)
			}
		})
	}
}
