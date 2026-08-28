package domain

import (
	"errors"
	"testing"
)

// AC-65：O 只有项目管理员可编辑；KR 由项目管理员或本 KR 负责人编辑。
func TestOkrEditPermissions(t *testing.T) {
	admin := Actor{Role: RoleAdmin}
	owner := Actor{IsOwner: true}
	member := Actor{Role: RoleMember}
	viewer := Actor{Role: RoleViewer}
	krOwner := int64(7)

	if !CanEditObjective(admin) || !CanEditObjective(owner) {
		t.Fatal("项目管理员与项目负责人应可编辑 O")
	}
	if CanEditObjective(member) || CanEditObjective(viewer) {
		t.Fatal("普通成员与只读成员不应可编辑 O")
	}
	if !CanEditKeyResult(admin, 9, &krOwner) {
		t.Fatal("项目管理员应可编辑任意 KR")
	}
	if !CanEditKeyResult(member, krOwner, &krOwner) {
		t.Fatal("KR 负责人应可编辑本人负责的 KR")
	}
	if CanEditKeyResult(member, 9, &krOwner) {
		t.Fatal("非本人负责的 KR 普通成员不应可编辑")
	}
	if CanEditKeyResult(viewer, krOwner, &krOwner) {
		t.Fatal("只读成员即便挂着 KR 负责人也不应可编辑")
	}
}

// AC-65：删除只对项目管理员开放，且存在下级对象时不允许删除。
func TestOkrDeleteRules(t *testing.T) {
	admin := Actor{Role: RoleAdmin}
	member := Actor{Role: RoleMember}

	if !CanDeleteObjective(admin, 0) {
		t.Fatal("管理员应可删除没有 KR 的 O")
	}
	if CanDeleteObjective(admin, 1) {
		t.Fatal("O 下有 KR 时不应可删")
	}
	if CanDeleteObjective(member, 0) {
		t.Fatal("普通成员不应可删 O")
	}
	if !CanDeleteKeyResult(admin, 0) {
		t.Fatal("管理员应可删除没有任务的 KR")
	}
	if CanDeleteKeyResult(admin, 1) {
		t.Fatal("KR 下有任务（含已完成、已取消）时不应可删")
	}
	if CanDeleteKeyResult(member, 0) {
		t.Fatal("普通成员不应可删 KR")
	}
	if err := DeleteObjectiveRule(admin, 2); !errors.Is(err, ErrObjectiveHasKeyResults) {
		t.Fatalf("O 下有 KR 应给出可读原因: %v", err)
	}
	if err := DeleteKeyResultRule(admin, 3); !errors.Is(err, ErrKeyResultHasTasks) {
		t.Fatalf("KR 下有任务应给出可读原因: %v", err)
	}
	if err := DeleteKeyResultRule(member, 0); !errors.Is(err, ErrOkrDeleteForbidden) {
		t.Fatalf("非管理员删除应被拒: %v", err)
	}
}

// AC-61：KR 负责人不可置空；更换负责人时继任者必须是非只读项目成员。
func TestValidateKeyResultUpdate(t *testing.T) {
	roleOf := func(id int64) string {
		switch id {
		case 5, 7:
			return RoleMember
		case 9:
			return RoleViewer
		}
		return ""
	}
	desc := "上线自动验收"
	if err := ValidateKeyResultUpdate(KeyResultUpdate{Description: &desc}, roleOf); err != nil {
		t.Fatalf("只改描述应通过: %v", err)
	}
	empty := "   "
	if err := ValidateKeyResultUpdate(KeyResultUpdate{Description: &empty}, roleOf); !errors.Is(err, ErrKrDescriptionEmpty) {
		t.Fatalf("描述不能改空: %v", err)
	}
	newOwner := int64(7)
	if err := ValidateKeyResultUpdate(KeyResultUpdate{OwnerID: &newOwner}, roleOf); err != nil {
		t.Fatalf("换成普通成员应通过: %v", err)
	}
	viewer := int64(9)
	if err := ValidateKeyResultUpdate(KeyResultUpdate{OwnerID: &viewer}, roleOf); !errors.Is(err, ErrKrOwnerNotEligible) {
		t.Fatalf("不能把只读成员任命为 KR 负责人: %v", err)
	}
	if err := ValidateKeyResultUpdate(KeyResultUpdate{ClearOwner: true}, roleOf); !errors.Is(err, ErrKrOwnerRequired) {
		t.Fatalf("KR 负责人不可置空: %v", err)
	}
	start, end := day("2026-09-10"), day("2026-09-01")
	if err := ValidateKeyResultUpdate(KeyResultUpdate{Start: start, End: end}, roleOf); !errors.Is(err, ErrKrPeriodInverted) {
		t.Fatalf("周期倒挂应被拒: %v", err)
	}
}

// AC-21／AC-61：成员仍在承担职责时不能被移出项目，返回的清单说明要先交接什么。
func TestMemberRemovalBlockedByDuties(t *testing.T) {
	if err := RemoveMemberRule(MemberDuties{}); err != nil {
		t.Fatalf("没有职责占位应可移出: %v", err)
	}
	duties := MemberDuties{KeyResults: []string{"上线自动验收"}, Tasks: []string{"联调验证"}}
	err := RemoveMemberRule(duties)
	if !errors.Is(err, ErrMemberHasDuties) {
		t.Fatalf("仍在担任 KR 负责人不应被移出: %v", err)
	}
	summary := MemberDutiesSummary(duties)
	if summary == "" {
		t.Fatal("应给出待交接清单")
	}
	for _, want := range []string{"上线自动验收", "联调验证"} {
		if !contains(summary, want) {
			t.Fatalf("待交接清单缺少 %q: %s", want, summary)
		}
	}
	// 只挂中间审核人／接收方／输入对接人同样算职责占位。
	for _, d := range []MemberDuties{
		{Reviewers: []string{"联调验证"}},
		{Receivers: []string{"联调验证"}},
		{InputProviders: []string{"接口口径"}},
	} {
		if err := RemoveMemberRule(d); !errors.Is(err, ErrMemberHasDuties) {
			t.Fatalf("%+v 应算职责占位: %v", d, err)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
