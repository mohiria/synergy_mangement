package domain

import (
	"errors"
	"testing"
)

// AC-29／§9.1：指定项目成员输入——对接人须为非访客，所需内容与期望时间必填。
func TestValidateMemberInput(t *testing.T) {
	roles := map[int64]string{3: RoleMember, 4: RoleAdmin, 5: RoleViewer}
	roleOf := func(id int64) string { return roles[id] }
	base := MemberInput{Name: "接口规范说明", Necessity: NecessityRequired, ProviderID: 3, ContentNote: "请提供最新接口字段口径", HasExpectedDate: true}
	cases := []struct {
		name string
		mut  func(*MemberInput)
		want error
	}{
		{"合法请求", func(*MemberInput) {}, nil},
		{"名称为空", func(m *MemberInput) { m.Name = " " }, ErrEdgeNameEmpty},
		{"对接人访客", func(m *MemberInput) { m.ProviderID = 5 }, ErrProviderNotEligible},
		{"对接人非成员", func(m *MemberInput) { m.ProviderID = 99 }, ErrProviderNotEligible},
		{"所需内容必填", func(m *MemberInput) { m.ContentNote = "  " }, ErrContentNoteRequired},
		{"期望时间必填", func(m *MemberInput) { m.HasExpectedDate = false }, ErrExpectedDateRequired},
		{"必要性非法", func(m *MemberInput) { m.Necessity = "optional" }, ErrNecessityInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := base
			tc.mut(&m)
			if got := ValidateMemberInput(m, roleOf); !errors.Is(got, tc.want) {
				t.Fatalf("ValidateMemberInput() = %v, want %v", got, tc.want)
			}
		})
	}
}

// AC-30／§5.5：同意接收——仅对接人本人、仅待接收状态；接收不等于就绪。
func TestAcceptInputRule(t *testing.T) {
	if err := AcceptInputRule(InputRequestPending, 3, 3); err != nil {
		t.Fatalf("对接人应可接收: %v", err)
	}
	if err := AcceptInputRule(InputRequestPending, 3, 9); !errors.Is(err, ErrNotProvider) {
		t.Fatalf("非对接人应被拒: %v", err)
	}
	if err := AcceptInputRule(InputRequestAccepted, 3, 3); !errors.Is(err, ErrInputStateConflict) {
		t.Fatalf("重复接收应冲突: %v", err)
	}
	if err := AcceptInputRule(InputRequestProvided, 3, 3); !errors.Is(err, ErrInputStateConflict) {
		t.Fatalf("已提供不可再接收: %v", err)
	}
}

// AC-30：提交内容——仅对接人、须先同意接收、内容或文件至少其一；提交后输入才就绪。
func TestProvideInputRule(t *testing.T) {
	if err := ProvideInputRule(InputRequestAccepted, 3, 3, true); err != nil {
		t.Fatalf("已接收后应可提交: %v", err)
	}
	if err := ProvideInputRule(InputRequestPending, 3, 3, true); !errors.Is(err, ErrInputNotAccepted) {
		t.Fatalf("未接收不可提交: %v", err)
	}
	if err := ProvideInputRule(InputRequestAccepted, 3, 9, true); !errors.Is(err, ErrNotProvider) {
		t.Fatalf("非对接人不可提交: %v", err)
	}
	if err := ProvideInputRule(InputRequestAccepted, 3, 3, false); !errors.Is(err, ErrInputContentRequired) {
		t.Fatalf("空内容不可提交: %v", err)
	}
	if err := ProvideInputRule(InputRequestProvided, 3, 3, true); !errors.Is(err, ErrInputStateConflict) {
		t.Fatalf("重复提交应冲突: %v", err)
	}
}

// 词汇表「输入就绪」：来自指定成员的边在已提供后就绪。
func TestMemberEdgeReady(t *testing.T) {
	if MemberEdgeReady(InputRequestPending) || MemberEdgeReady(InputRequestAccepted) {
		t.Fatal("待接收/已接收不应就绪")
	}
	if !MemberEdgeReady(InputRequestProvided) {
		t.Fatal("已提供应就绪")
	}
}

// AC-53：一次配置可多选对接人——至少一名、不可重复、逐名沿用单人校验。
func TestValidateMemberInputs(t *testing.T) {
	roles := map[int64]string{3: RoleMember, 4: RoleAdmin, 5: RoleViewer}
	roleOf := func(id int64) string { return roles[id] }
	base := MemberInputs{Name: "接口规范说明", Necessity: NecessityRequired, ProviderIDs: []int64{3, 4}, ContentNote: "请提供最新接口字段口径", HasExpectedDate: true}
	cases := []struct {
		name string
		mut  func(*MemberInputs)
		want error
	}{
		{"多对接人合法", func(*MemberInputs) {}, nil},
		{"单对接人合法", func(m *MemberInputs) { m.ProviderIDs = []int64{3} }, nil},
		{"未选对接人", func(m *MemberInputs) { m.ProviderIDs = nil }, ErrProvidersEmpty},
		{"对接人重复", func(m *MemberInputs) { m.ProviderIDs = []int64{3, 4, 3} }, ErrProviderDuplicated},
		{"含访客", func(m *MemberInputs) { m.ProviderIDs = []int64{3, 5} }, ErrProviderNotEligible},
		{"含非成员", func(m *MemberInputs) { m.ProviderIDs = []int64{3, 99} }, ErrProviderNotEligible},
		{"所需内容必填", func(m *MemberInputs) { m.ContentNote = " " }, ErrContentNoteRequired},
		{"期望时间必填", func(m *MemberInputs) { m.HasExpectedDate = false }, ErrExpectedDateRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := base
			tc.mut(&m)
			if got := ValidateMemberInputs(m, roleOf); !errors.Is(got, tc.want) {
				t.Fatalf("ValidateMemberInputs() = %v, want %v", got, tc.want)
			}
		})
	}
}
