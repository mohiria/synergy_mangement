package domain

import (
	"errors"
	"testing"
)

func sptr(s string) *string { return &s }

// §9.1（#172 修订）：直接修改任务字段——至少一项修改值；不再要求修改原因。
func TestValidateKeyFieldChanges(t *testing.T) {
	roles := map[int64]string{5: RoleMember, 8: RoleViewer}
	roleOf := func(id int64) string { return roles[id] }
	start := date("2026-09-01")

	cases := []struct {
		name string
		c    KeyFieldChanges
		want error
	}{
		{"合法改名", KeyFieldChanges{Name: sptr("新任务名")}, nil},
		{"空修改", KeyFieldChanges{}, ErrChangeEmpty},
		{"新名称为空", KeyFieldChanges{Name: sptr("   ")}, ErrTaskNameEmpty},
		{"新负责人非成员", KeyFieldChanges{OwnerID: i64(99)}, ErrTaskOwnerNotEligible},
		{"新负责人是访客", KeyFieldChanges{OwnerID: i64(8)}, ErrTaskOwnerNotEligible},
		{"新截止早于开始", KeyFieldChanges{EndDate: day("2026-08-20")}, ErrTaskPeriodInverted},
		{"新截止合法", KeyFieldChanges{EndDate: day("2026-09-15")}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidateKeyFieldChanges(tc.c, roleOf, start); !errors.Is(got, tc.want) {
				t.Fatalf("ValidateKeyFieldChanges() = %v, want %v", got, tc.want)
			}
		})
	}
}

