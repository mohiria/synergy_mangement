package domain

// 导入记录（词汇表「导入记录」；PRD §7.9、§10.4、§11.1；AC-68）：
// 每次表格导入留存的业务事实——操作人、时间、源文件名、本次新建的 O／KR／任务数量与结果。
// 它与通用操作审计不是一回事：通用审计只记 actor／action／route／对象类型与 ID，
// 不含文件名与影响计数，也不记失败的那一次。
const (
	ImportSuccess = "success"
	ImportPartial = "partial"
	ImportFailed  = "failed"
)

// ImportCounts 一次导入真实写入的对象数量（不是请求里报的条数）。
type ImportCounts struct {
	Objectives int
	KeyResults int
	Tasks      int
}

// Total 本次真实写入的对象总数。
func (c ImportCounts) Total() int {
	return c.Objectives + c.KeyResults + c.Tasks
}

// DeriveImportOutcome 由真实写入量与失败摘要派生导入结果：
// 无失败即成功；有失败但已写入过对象是部分失败；一条没写就失败是失败。
// 失败绝不写成功（与 #53「失败不再伪装成功」同口径）。
func DeriveImportOutcome(counts ImportCounts, failure string) string {
	if failure == "" {
		return ImportSuccess
	}
	if counts.Total() > 0 {
		return ImportPartial
	}
	return ImportFailed
}

var importOutcomeLabels = map[string]string{
	ImportSuccess: "成功",
	ImportPartial: "部分失败",
	ImportFailed:  "失败",
}

// ImportOutcomeLabel 导入结果显示文案（派生字段）；未知取值退化为「失败」——
// 拿不准的一次导入宁可显示失败，也不能说成成功。
func ImportOutcomeLabel(outcome string) string {
	if label, ok := importOutcomeLabels[outcome]; ok {
		return label
	}
	return "失败"
}
