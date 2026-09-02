package domain

import (
	"errors"
	"strings"
	"testing"
)

// AC-65（裁决 12，#183 修订）：O 与 KR 均只有项目管理员（含项目负责人）可编辑。
func TestOkrEditPermissions(t *testing.T) {
	admin := Actor{Role: RoleAdmin}
	owner := Actor{IsOwner: true}
	member := Actor{Role: RoleMember}
	viewer := Actor{Role: RoleViewer}

	if !CanEditObjective(admin) || !CanEditObjective(owner) {
		t.Fatal("项目管理员与项目负责人应可编辑 O")
	}
	if CanEditObjective(member) || CanEditObjective(viewer) {
		t.Fatal("项目成员与访客不应可编辑 O")
	}
	if !CanEditKeyResult(admin) || !CanEditKeyResult(owner) {
		t.Fatal("项目管理员与项目负责人应可编辑 KR")
	}
	if CanEditKeyResult(member) {
		t.Fatal("普通项目成员不应可编辑 KR（裁决 12）")
	}
	if CanEditKeyResult(viewer) {
		t.Fatal("访客不应可编辑 KR")
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
		t.Fatal("项目成员不应可删 O")
	}
	if !CanDeleteKeyResult(admin, 0) {
		t.Fatal("管理员应可删除没有任务的 KR")
	}
	if CanDeleteKeyResult(admin, 1) {
		t.Fatal("KR 下有任务（含已完成、已关闭）时不应可删")
	}
	if CanDeleteKeyResult(member, 0) {
		t.Fatal("项目成员不应可删 KR")
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

// AC-65（裁决 12，#183 修订）：KR 编辑只剩描述与量化指标两个结构字段。
func TestValidateKeyResultUpdate(t *testing.T) {
	desc := "上线自动验收"
	if err := ValidateKeyResultUpdate(KeyResultUpdate{Description: &desc}); err != nil {
		t.Fatalf("只改描述应通过: %v", err)
	}
	empty := "   "
	if err := ValidateKeyResultUpdate(KeyResultUpdate{Description: &empty}); !errors.Is(err, ErrKrDescriptionEmpty) {
		t.Fatalf("描述不能改空: %v", err)
	}
	long := strings.Repeat("述", 201)
	if err := ValidateKeyResultUpdate(KeyResultUpdate{Description: &long}); !errors.Is(err, ErrKrDescriptionTooLong) {
		t.Fatalf("描述超长应被拒: %v", err)
	}
	metric := strings.Repeat("标", 101)
	if err := ValidateKeyResultUpdate(KeyResultUpdate{Metric: &metric}); !errors.Is(err, ErrKrMetricTooLong) {
		t.Fatalf("量化指标超长应被拒: %v", err)
	}
}

// AC-21／AC-61：成员仍在承担职责时不能被移出项目，返回的清单说明要先交接什么。
func TestMemberRemovalBlockedByDuties(t *testing.T) {
	if err := RemoveMemberRule(MemberDuties{}); err != nil {
		t.Fatalf("没有职责占位应可移出: %v", err)
	}
	duties := MemberDuties{Tasks: []string{"联调验证"}}
	err := RemoveMemberRule(duties)
	if !errors.Is(err, ErrMemberHasDuties) {
		t.Fatalf("仍在担任任务负责人不应被移出: %v", err)
	}
	summary := MemberDutiesSummary(duties)
	if summary == "" {
		t.Fatal("应给出待交接清单")
	}
	if !contains(summary, "联调验证") {
		t.Fatalf("待交接清单缺少 %q: %s", "联调验证", summary)
	}
	// 只挂成果审核人／接收方同样算职责占位（#178：输入对接人职责随输入请求退场）。
	for _, d := range []MemberDuties{
		{Reviewers: []string{"联调验证"}},
		{Receivers: []string{"联调验证"}},
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
