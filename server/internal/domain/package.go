package domain

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var (
	ErrPackageNameEmpty     = errors.New("成果包名称不能为空")
	ErrPackageNameTooLong   = errors.New("成果包名称不能超过 100 字")
	ErrPackageEmpty         = errors.New("请至少勾选一项当前成果")
	ErrPackageItemNoCurrent = errors.New("勾选的交付物必须有已生效的当前内容")
)

// ValidatePackage 校验成果包（AC-18）：名称必填、目录非空、勾选项须有已生效当前内容。
func ValidatePackage(name string, deliverableIDs []int64, hasCurrent func(int64) bool) error {
	n := strings.TrimSpace(name)
	if n == "" {
		return ErrPackageNameEmpty
	}
	if utf8.RuneCountInString(n) > 100 {
		return ErrPackageNameTooLong
	}
	if len(deliverableIDs) == 0 {
		return ErrPackageEmpty
	}
	for _, id := range deliverableIDs {
		if !hasCurrent(id) {
			return ErrPackageItemNoCurrent
		}
	}
	return nil
}

// CanCreatePackage 创建成果包：项目管理员／项目负责人（§3.4；其余成员查看/下载）。
func CanCreatePackage(a Actor) bool {
	return CanEditProject(a)
}
