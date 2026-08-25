package domain

import (
	"strings"
	"testing"
	"time"
)

func TestValidateProjectName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{"正常名称", "协同管理工具试点", nil},
		{"单字符", "A", nil},
		{"100 字符上限", strings.Repeat("名", 100), nil},
		{"空字符串", "", ErrProjectNameEmpty},
		{"仅空白", "   ", ErrProjectNameEmpty},
		{"超过 100 字符", strings.Repeat("名", 101), ErrProjectNameTooLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidateProjectName(tt.input); got != tt.wantErr {
				t.Fatalf("ValidateProjectName(%q) = %v, want %v", tt.input, got, tt.wantErr)
			}
		})
	}
}

func TestValidateProjectStatus(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{"未开始", "not_started", nil},
		{"进行中", "in_progress", nil},
		{"已完成", "completed", nil},
		{"已归档", "archived", nil},
		{"空字符串", "", ErrProjectStatusInvalid},
		{"未知取值", "done", ErrProjectStatusInvalid},
		{"中文取值", "未开始", ErrProjectStatusInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidateProjectStatus(tt.input); got != tt.wantErr {
				t.Fatalf("ValidateProjectStatus(%q) = %v, want %v", tt.input, got, tt.wantErr)
			}
		})
	}
}

func TestDefaultProjectStatus(t *testing.T) {
	if DefaultProjectStatus != "not_started" {
		t.Fatalf("DefaultProjectStatus = %q, want %q", DefaultProjectStatus, "not_started")
	}
	if err := ValidateProjectStatus(DefaultProjectStatus); err != nil {
		t.Fatalf("DefaultProjectStatus 应当是合法状态，got %v", err)
	}
}

func TestValidateProjectStage(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{"空（选填）", "", nil},
		{"正常阶段", "联合联调阶段", nil},
		{"50 字符上限", strings.Repeat("段", 50), nil},
		{"超过 50 字符", strings.Repeat("段", 51), ErrProjectStageTooLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidateProjectStage(tt.input); got != tt.wantErr {
				t.Fatalf("ValidateProjectStage(%q) = %v, want %v", tt.input, got, tt.wantErr)
			}
		})
	}
}

func TestValidateProjectPlan(t *testing.T) {
	d := func(s string) *time.Time {
		v, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatalf("bad date %q: %v", s, err)
		}
		return &v
	}
	tests := []struct {
		name       string
		start, end *time.Time
		wantErr    error
	}{
		{"都未填", nil, nil, nil},
		{"只填开始", d("2026-09-01"), nil, nil},
		{"只填完成", nil, d("2026-09-30"), nil},
		{"开始早于完成", d("2026-09-01"), d("2026-09-30"), nil},
		{"同一天", d("2026-09-01"), d("2026-09-01"), nil},
		{"完成早于开始", d("2026-09-30"), d("2026-09-01"), ErrProjectPlanInverted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidateProjectPlan(tt.start, tt.end); got != tt.wantErr {
				t.Fatalf("ValidateProjectPlan(%v, %v) = %v, want %v", tt.start, tt.end, got, tt.wantErr)
			}
		})
	}
}
