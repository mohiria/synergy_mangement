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
	ErrPackageFileNotFound  = errors.New("勾选的过程文件或外部材料不存在")
)

// ValidatePackage 校验成果包（AC-18、§7.7）：名称必填、目录非空；
// 交付物项须有已生效当前内容（候选不进包），过程文件与重要外部材料按需选、须真实存在于本项目。
func ValidatePackage(name string, deliverableIDs, taskFileIDs []int64, hasCurrent, taskFileExists func(int64) bool) error {
	n := strings.TrimSpace(name)
	if n == "" {
		return ErrPackageNameEmpty
	}
	if utf8.RuneCountInString(n) > 100 {
		return ErrPackageNameTooLong
	}
	if len(deliverableIDs)+len(taskFileIDs) == 0 {
		return ErrPackageEmpty
	}
	for _, id := range deliverableIDs {
		if !hasCurrent(id) {
			return ErrPackageItemNoCurrent
		}
	}
	for _, id := range taskFileIDs {
		if !taskFileExists(id) {
			return ErrPackageFileNotFound
		}
	}
	return nil
}

// CanCreatePackage 创建成果包：项目管理员／项目负责人（§3.4；其余成员查看/下载）。
func CanCreatePackage(a Actor) bool {
	return CanEditProject(a)
}
