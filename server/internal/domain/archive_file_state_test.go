package domain

import "testing"

// 裁决 G1（#140）：文件状态两档——所属任务已完成→已发布，其余全部→未发布。
func TestArchiveFileState(t *testing.T) {
	cases := []struct {
		name      string
		status    string
		wantState string
		wantLabel string
	}{
		{"已完成任务已发布", TaskCompleted, ArchivePublished, "已发布"},
		{"进行中未发布", TaskInProgress, ArchiveUnpublished, "未发布"},
		{"草稿未发布", TaskDraft, ArchiveUnpublished, "未发布"},
		{"待 KR 终审未发布", TaskPendingFinalReview, ArchiveUnpublished, "未发布"},
		{"已关闭未发布", TaskCancelled, ArchiveUnpublished, "未发布"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			state, label := ArchiveFileState(c.status)
			if state != c.wantState || label != c.wantLabel {
				t.Fatalf("ArchiveFileState(%q) = (%q,%q), want (%q,%q)", c.status, state, label, c.wantState, c.wantLabel)
			}
		})
	}
}
