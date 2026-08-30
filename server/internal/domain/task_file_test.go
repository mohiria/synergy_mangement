package domain

import (
	"errors"
	"testing"
)

// §7.7 文件对象边界表（AC-17／AC-18；#79）：四类文件在三列上的取值一次说清，
// 免得「过程文件能不能算下游输入」这类问题散落在各处 handler 里各答一次。
func TestFileBoundary(t *testing.T) {
	cases := []struct {
		name              string
		kind              string
		entersCompletion  bool
		formalInput       bool
		packageSelectable bool
	}{
		{"当前交付物", FileKindCurrent, true, true, true},
		{"候选交付物", FileKindCandidate, true, false, false},
		{"过程文件", FileKindProcess, false, false, true},
		{"重要外部材料", FileKindExternal, false, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := FileBoundary(c.kind)
			if b.EntersCompletionReview != c.entersCompletion {
				t.Fatalf("EntersCompletionReview = %v, want %v", b.EntersCompletionReview, c.entersCompletion)
			}
			if b.FormalInput != c.formalInput {
				t.Fatalf("FormalInput = %v, want %v", b.FormalInput, c.formalInput)
			}
			if b.PackageSelectable != c.packageSelectable {
				t.Fatalf("PackageSelectable = %v, want %v", b.PackageSelectable, c.packageSelectable)
			}
		})
	}
	// 未知类型一律按最保守取值，不给意外的文件类型开任何口子。
	if b := FileBoundary("bogus"); b.EntersCompletionReview || b.FormalInput || b.PackageSelectable {
		t.Fatalf("未知文件类型不应有任何特权: %+v", b)
	}
}

// 过程文件与重要外部材料的类型校验与显示文案（词汇表两条新词）。
func TestValidateTaskFileKind(t *testing.T) {
	for _, k := range []string{TaskFileProcess, TaskFileExternal} {
		if err := ValidateTaskFileKind(k); err != nil {
			t.Fatalf("%s 应是合法类型: %v", k, err)
		}
	}
	if err := ValidateTaskFileKind("deliverable"); !errors.Is(err, ErrTaskFileKindInvalid) {
		t.Fatalf("交付物不是任务文件类型: %v", err)
	}
	if got := TaskFileKindLabel(TaskFileProcess); got != "过程文件" {
		t.Fatalf("TaskFileKindLabel(process) = %q", got)
	}
	if got := TaskFileKindLabel(TaskFileExternal); got != "重要外部材料" {
		t.Fatalf("TaskFileKindLabel(external) = %q", got)
	}
	if got := TaskFileKindLabel("bogus"); got != "任务文件" {
		t.Fatalf("未知类型应退化为通用词: %q", got)
	}
}

// 谁能上传／删除过程文件与外部材料：与配置输出同一批人（负责人／创建人／可编辑项目者），
// 已取消任务不再接受任何写入；已完成任务仍可补录——这两类文件不进审批、不影响任何判定。
func TestCanManageTaskFiles(t *testing.T) {
	facts := TaskFacts{Status: TaskInProgress, CreatorID: 3, OwnerID: 5}
	cases := []struct {
		name  string
		actor Actor
		user  int64
		t     TaskFacts
		want  bool
	}{
		{"负责人可管理", Actor{Role: RoleMember}, 5, facts, true},
		{"创建人可管理", Actor{Role: RoleMember}, 3, facts, true},
		{"管理员可管理", Actor{Role: RoleAdmin}, 9, facts, true},
		{"无关成员不可管理", Actor{Role: RoleMember}, 9, facts, false},
		{"只读成员不可管理", Actor{Role: RoleViewer}, 5, facts, false},
		{"草稿可管理", Actor{Role: RoleMember}, 5, TaskFacts{Status: TaskDraft, OwnerID: 5}, true},
		{"已完成仍可补录", Actor{Role: RoleMember}, 5, TaskFacts{Status: TaskCompleted, OwnerID: 5}, true},
		{"已取消不可管理", Actor{Role: RoleMember}, 5, TaskFacts{Status: TaskCancelled, OwnerID: 5}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanManageTaskFiles(tc.actor, tc.user, tc.t); got != tc.want {
				t.Fatalf("CanManageTaskFiles = %v, want %v", got, tc.want)
			}
		})
	}
}

// 背景说明是选填、有长度上限；文件名沿用候选内容那套校验。
func TestValidateTaskFileNote(t *testing.T) {
	if err := ValidateTaskFileNote(""); err != nil {
		t.Fatalf("背景说明选填: %v", err)
	}
	if err := ValidateTaskFileNote(repeat("说", 501)); !errors.Is(err, ErrTaskFileNoteTooLong) {
		t.Fatalf("超长说明应被拒: %v", err)
	}
}
