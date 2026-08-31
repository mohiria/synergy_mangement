package domain

import "testing"

// #138（裁决 E1）：就地编辑保存路由的对外表达——由 FieldChangeRoute 的结果映射，
// 前端只消费该派生值，不复算规则。
func TestFieldEditMode(t *testing.T) {
	cases := []struct {
		name    string
		outcome FieldChangeOutcome
		want    string
	}{
		{"草稿直接生效", FieldChangeDirect, "direct"},
		{"KR 负责人免审即时生效", FieldChangeExempt, "exempt"},
		{"其余进入审批", FieldChangePending, "approval"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := FieldEditMode(c.outcome); got != c.want {
				t.Fatalf("FieldEditMode(%v) = %q, want %q", c.outcome, got, c.want)
			}
		})
	}
}
