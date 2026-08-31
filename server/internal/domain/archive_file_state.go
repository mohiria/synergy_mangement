package domain

// 裁决 G1（#140）：成果归档页的「文件状态」两档——已发布（所属任务已完成）／未发布（其他状态）。
// 只用于归档列表层的呈现；交付物内容状态五档（contentState）在抽屉与审核处不受影响。
const (
	ArchivePublished   = "published"
	ArchiveUnpublished = "unpublished"
)

// ArchiveFileState 按所属任务状态派生归档文件状态与显示文案。
func ArchiveFileState(taskStatus string) (state, label string) {
	if taskStatus == TaskCompleted {
		return ArchivePublished, "已发布"
	}
	return ArchiveUnpublished, "未发布"
}
