package domain

import (
	"errors"
	"strings"
	"time"
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

// InArchiveWindow 判定一条归档记录是否落在「时间」筛选区间内（§7.7 六个筛选维度之一，AC-17）。
// from／to 是日期，闭区间——终点当天整天都算在内（否则用户选到今天却看不到今天的东西）。
// 任一端为空表示该侧不限；两端都为空时一律通过。
// 没有时间的项（既无当前内容也无候选、或任务文件时间缺失）在给了区间后不返回：
// 它无法证明自己落在区间里，混进结果只会让「按时间筛」失去意义。
func InArchiveWindow(at, from, to *time.Time) bool {
	if from == nil && to == nil {
		return true
	}
	if at == nil {
		return false
	}
	if from != nil && at.Before(*from) {
		return false
	}
	if to != nil && !at.Before(to.AddDate(0, 0, 1)) {
		return false
	}
	return true
}

// CanCreatePackage 创建成果包：项目管理员／项目负责人（§3.4；其余成员查看/下载）。
func CanCreatePackage(a Actor) bool {
	return CanEditProject(a)
}
