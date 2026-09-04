package domain

// ProjectMemberFact 成员表里的一行事实：用户、显示名、成员角色。
type ProjectMemberFact struct {
	UserID int64
	Name   string
	Role   string
}

// FinalReviewers 终审人集合（裁决 11，#181）：项目负责人 + 成员表里的管理员，
// 按处理时点动态解析角色、不快照；项目负责人排首位（显示文案「待{首位姓名}等N人审批」）。
// 输入只有负责人与成员表：系统管理员的隐式身份不在其中，因此不进审批链（#200，ADR 0003）；
// 被显式加为管理员成员时与普通管理员无异。
func FinalReviewers(ownerID int64, ownerName string, members []ProjectMemberFact) ([]int64, []string) {
	ids := []int64{ownerID}
	names := []string{ownerName}
	seen := map[int64]bool{ownerID: true}
	for _, m := range members {
		if m.Role != RoleAdmin || seen[m.UserID] {
			continue
		}
		ids = append(ids, m.UserID)
		names = append(names, m.Name)
		seen[m.UserID] = true
	}
	return ids, names
}
