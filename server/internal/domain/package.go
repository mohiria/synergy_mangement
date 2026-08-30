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

// PackageItemFacts 成果包目录项的库内事实（§7.7、AC-18）。
// 两种引用各占一侧：交付物项走 Deliverable*／Current*，任务文件项走 TaskFile*；
// Source* 是入包时快照下来的来源事实，来源文件被删除后 TaskFile* 全空、只剩快照。
type PackageItemFacts struct {
	DeliverableID       *int64
	DeliverableName     string
	DeliverableTaskName string
	CurrentFileID       *int64
	CurrentFileName     string
	CurrentObjectKey    string
	EffectiveAt         *time.Time

	TaskFileID        *int64
	TaskFileName      string
	TaskFileKind      string
	TaskFileObjectKey string
	TaskFileTaskName  string

	SourceTaskName string
	SourceFileName string
	SourceFileKind string
}

// PackageItem 归一后的成果包目录项：列表、来源清单与打包三处用同一份口径。
type PackageItem struct {
	DeliverableID *int64
	TaskFileID    *int64
	FileKind      string // 任务文件才有
	Name          string // 交付物项名称，或任务文件的文件名
	TaskName      string
	FileID        *int64 // 无可下载内容时为空
	FileName      string
	ObjectKey     string
	EffectiveAt   *time.Time
	SourceDeleted bool // 任务文件来源已被删除（F-10）
}

// ResolvePackageItem 归一目录项（§7.7、AC-18）。
// 交付物项解析到当前内容——被覆盖后自动指向新内容，没有已生效内容时不带文件；
// 任务文件项内容就是自己；来源文件被删除后条目不消失：按快照保留名称与所属任务、标为来源已删除，
// 不带内容（因此也不进包内），这是「保留逻辑清单和来源事实」的落点。
func ResolvePackageItem(f PackageItemFacts) PackageItem {
	if f.DeliverableID != nil {
		return PackageItem{
			DeliverableID: f.DeliverableID,
			Name:          f.DeliverableName,
			TaskName:      f.DeliverableTaskName,
			FileID:        f.CurrentFileID,
			FileName:      f.CurrentFileName,
			ObjectKey:     f.CurrentObjectKey,
			EffectiveAt:   f.EffectiveAt,
		}
	}
	if f.TaskFileID == nil {
		return PackageItem{
			FileKind:      f.SourceFileKind,
			Name:          f.SourceFileName,
			TaskName:      f.SourceTaskName,
			SourceDeleted: true,
		}
	}
	return PackageItem{
		TaskFileID: f.TaskFileID,
		FileKind:   f.TaskFileKind,
		Name:       f.TaskFileName,
		TaskName:   f.TaskFileTaskName,
		FileID:     f.TaskFileID,
		FileName:   f.TaskFileName,
		ObjectKey:  f.TaskFileObjectKey,
	}
}

// PackageManifestLine 来源清单一行（AC-18）：来源事实在前，解析结果在后。
// 三种去向各有措辞——有内容给文件名，交付物没有已生效内容给「暂无」，来源被删给「已删除」。
func PackageManifestLine(it PackageItem) string {
	line := it.TaskName + " / " + it.Name
	if it.FileKind != "" {
		line += "（" + TaskFileKindLabel(it.FileKind) + "）"
	}
	switch {
	case it.SourceDeleted:
		return line + " →（来源文件已删除）"
	case it.FileID == nil:
		return line + " →（暂无已生效当前内容）"
	default:
		return line + " → " + it.FileName
	}
}
