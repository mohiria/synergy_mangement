package domain

import "errors"

var ErrMemberRoleInvalid = errors.New("成员角色不合法")

// 成员角色三值枚举（词汇表「成员角色」；PRD §3.2 系统权限，V4.4.3 取消可编辑成员）。
const (
	RoleAdmin  = "admin"
	RoleMember = "member"
	RoleViewer = "viewer"
)

// Actor 当前用户在某个项目内的身份事实：是否项目负责人、成员角色（非成员为空串）。
type Actor struct {
	IsOwner bool
	Role    string
}

var memberRoles = map[string]struct{}{
	RoleAdmin:  {},
	RoleMember: {},
	RoleViewer: {},
}

// ValidateMemberRole 校验成员角色是否属于三值枚举。
func ValidateMemberRole(role string) error {
	if _, ok := memberRoles[role]; !ok {
		return ErrMemberRoleInvalid
	}
	return nil
}

// CanManageMembers 判定能否管理项目成员和权限：仅项目管理员（PRD §3.4）；
// 项目负责人为独立角色、享有同等权限（PRD §0.4 V4.4.2）。
func CanManageMembers(a Actor) bool {
	return a.IsOwner || a.Role == RoleAdmin
}

// CanEditProject 判定能否编辑项目基础信息与配置：与管理成员同一规则（PRD §3.4）。
func CanEditProject(a Actor) bool {
	return a.IsOwner || a.Role == RoleAdmin
}

// CanWriteProject 判定当前身份是否具备写入资格：项目负责人、管理员或项目成员。
// 访客与已被移出项目的人一律不可写（§3.4 权限矩阵「只读列全部为 —」）。
// 这是全部 Can／Decide 判定的前置：任务负责人、KR 负责人、中间审核人这些职责
// 只在人还是非只读项目成员时才生效——成员被移除或降为只读后，职责随之失效（S2）。
// 唯一例外是「确认接收」，访客被指定为接收方时可以确认（AC-62）。
func CanWriteProject(a Actor) bool {
	return a.IsOwner || a.Role == RoleAdmin || a.Role == RoleMember
}

// CanReadProject 判定能否读取项目内容：项目内成员或项目负责人（PRD §3.3）。
// 非成员一律不可读，读越权与写越权同一道边界。
func CanReadProject(a Actor) bool {
	if a.IsOwner {
		return true
	}
	_, ok := memberRoles[a.Role]
	return ok
}

var ErrNoMembersSelected = errors.New("请先选择要加入的成员")

// 批量加入时按人跳过的原因（#93）。
const (
	SkipAlreadyMember = "already_member"
	SkipUserNotFound  = "user_not_found"
)

// SkippedMember 一次批量加入里被跳过的人及其原因。
type SkippedMember struct {
	UserID int64
	Reason string
}

// PlanAddMembers 规划一次批量加入：给定申请名单、系统内已知用户与项目现有成员，
// 分出真正要建立成员关系的名单与按人跳过的名单。
// 名单为空或角色不合法时整批拒绝；已在项目内与用户不存在按人跳过，不牵连其余人。
// 保持申请名单的先后次序，重复选中的同一人只算一次。
func PlanAddMembers(role string, requested, known, existing []int64) ([]int64, []SkippedMember, error) {
	if err := ValidateMemberRole(role); err != nil {
		return nil, nil, err
	}
	if len(requested) == 0 {
		return nil, nil, ErrNoMembersSelected
	}
	knownSet := make(map[int64]struct{}, len(known))
	for _, id := range known {
		knownSet[id] = struct{}{}
	}
	existingSet := make(map[int64]struct{}, len(existing))
	for _, id := range existing {
		existingSet[id] = struct{}{}
	}
	var add []int64
	var skipped []SkippedMember
	seen := make(map[int64]struct{}, len(requested))
	for _, id := range requested {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		_, isKnown := knownSet[id]
		_, isMember := existingSet[id]
		switch {
		case !isKnown:
			skipped = append(skipped, SkippedMember{UserID: id, Reason: SkipUserNotFound})
		case isMember:
			skipped = append(skipped, SkippedMember{UserID: id, Reason: SkipAlreadyMember})
		default:
			add = append(add, id)
		}
	}
	return add, skipped, nil
}

var skipReasonLabels = map[string]string{
	SkipAlreadyMember: "已在项目内",
	SkipUserNotFound:  "用户不存在",
}

// SkipReasonLabel 跳过原因的显示文案。
func SkipReasonLabel(reason string) string {
	if label, ok := skipReasonLabels[reason]; ok {
		return label
	}
	return reason
}
