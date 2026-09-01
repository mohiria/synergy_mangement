package domain

import "errors"

var (
	ErrTaskImportEmpty     = errors.New("导入内容不能为空")
	ErrTaskImportKrMissing = errors.New("每条任务都要指定所属 KR")
)

// ImportedTask 一条待导入的任务（裁决 #164：导入不再含预期交付物列）。
type ImportedTask struct {
	Task NewTask
}

// TaskImportGroup 按所属 KR 分组的一批待导入任务（AC-02b）。
type TaskImportGroup struct {
	KeyResultID int64
	Tasks       []ImportedTask
}

// ValidateTaskImport 整批校验任务导入：任何一条不合规都不写。
// 单条任务沿用创建任务时的同一份规则，导入不另设宽松口径。
func ValidateTaskImport(groups []TaskImportGroup, roleOf func(int64) string) error {
	if len(groups) == 0 {
		return ErrTaskImportEmpty
	}
	for _, g := range groups {
		if len(g.Tasks) == 0 {
			return ErrTaskImportEmpty
		}
		if g.KeyResultID <= 0 {
			return ErrTaskImportKrMissing
		}
		for _, it := range g.Tasks {
			if err := ValidateNewTask(it.Task, roleOf); err != nil {
				return err
			}
		}
	}
	return nil
}

// CanImportTasks 判定能否批量导入任务：与编辑项目结构同一道边界（§3.4）——
// 只有项目负责人与项目管理员可用，项目成员与访客不可。
func CanImportTasks(a Actor) bool {
	return CanEditProject(a)
}
