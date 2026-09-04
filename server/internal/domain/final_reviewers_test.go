package domain

import (
	"reflect"
	"testing"
)

// 终审人集合（裁决 11，#181）只从项目负责人与成员表里的管理员派生；
// #200：系统管理员的隐式身份不在成员表，因此不进审批链；被显式加为管理员成员才参与。
func TestFinalReviewers(t *testing.T) {
	cases := []struct {
		name      string
		ownerID   int64
		ownerName string
		members   []ProjectMemberFact
		wantIDs   []int64
		wantNames []string
	}{
		{"仅负责人", 9, "负责人", nil, []int64{9}, []string{"负责人"}},
		{"负责人排首位，管理员成员随后，普通成员与访客不进",
			9, "负责人",
			[]ProjectMemberFact{{2, "访客", RoleViewer}, {3, "成员", RoleMember}, {4, "管理员甲", RoleAdmin}, {5, "管理员乙", RoleAdmin}},
			[]int64{9, 4, 5}, []string{"负责人", "管理员甲", "管理员乙"}},
		{"负责人同时是管理员成员只算一次",
			9, "负责人",
			[]ProjectMemberFact{{9, "负责人", RoleAdmin}, {4, "管理员甲", RoleAdmin}},
			[]int64{9, 4}, []string{"负责人", "管理员甲"}},
		{"系统管理员只有隐式身份（不在成员表）时不进终审集合",
			9, "负责人",
			[]ProjectMemberFact{{4, "管理员甲", RoleAdmin}},
			[]int64{9, 4}, []string{"负责人", "管理员甲"}},
		{"系统管理员被显式加为管理员成员则参与",
			9, "负责人",
			[]ProjectMemberFact{{1, "系统管理员", RoleAdmin}},
			[]int64{9, 1}, []string{"负责人", "系统管理员"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ids, names := FinalReviewers(c.ownerID, c.ownerName, c.members)
			if !reflect.DeepEqual(ids, c.wantIDs) || !reflect.DeepEqual(names, c.wantNames) {
				t.Fatalf("FinalReviewers() = %v %v, want %v %v", ids, names, c.wantIDs, c.wantNames)
			}
		})
	}
}
