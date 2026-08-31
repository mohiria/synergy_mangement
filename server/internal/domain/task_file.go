package domain

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// 任务文件（词汇表「过程文件」「重要外部材料」；主 PRD §7.7 文件对象边界表、§8.5、AC-17／AC-18）。
//
// 这两类文件与交付物落在同一个任务下、走同一套两阶段提交，但边界完全不同：
// 它们不进入完成审批，也不作为下游任务的正式输入（外部材料可作输入证据，但不把输入置为就绪），
// 只是可以按需选进成果包。边界表写在这里，各处 handler 一律来查，不各自记一份。
const (
	// TaskFileProcess 过程文件：任务执行过程中产生、不作为正式成果提交审核的资料。
	TaskFileProcess = "process"
	// TaskFileExternal 重要外部材料：项目外部提供、经内部协调人代为录入的资料。
	TaskFileExternal = "external"
)

// 任务文件的两阶段提交状态：与候选内容同口径，未确认写入对象存储的记录不参与任何展示。
const (
	TaskFileUploading = "uploading"
	TaskFileReady     = "ready"
)

// §7.7 文件对象边界表的四类文件。前两类是交付物内容的两种状态，后两类是任务文件。
const (
	FileKindCurrent   = DeliverableCurrent
	FileKindCandidate = DeliverableCandidate
	FileKindProcess   = TaskFileProcess
	FileKindExternal  = TaskFileExternal
)

var (
	ErrTaskFileKindInvalid = errors.New("任务文件类型不合法")
	ErrTaskFileNoteTooLong = errors.New("背景说明不能超过 500 字")
)

// FileBoundaryFacts §7.7 边界表的一行：一类文件在三列上的取值。
type FileBoundaryFacts struct {
	// EntersCompletionReview 是否进入完成审批（当前交付物按任务配置，候选是本次审批的标的）。
	EntersCompletionReview bool
	// FormalInput 是否可作为下游任务的正式输入（参与就绪判定）。
	FormalInput bool
	// PackageSelectable 是否可以按需选进成果包。
	PackageSelectable bool
}

// fileBoundaries §7.7 文件对象边界表。未登记的类型一律无特权（见 FileBoundary）。
var fileBoundaries = map[string]FileBoundaryFacts{
	FileKindCurrent:   {EntersCompletionReview: true, FormalInput: true, PackageSelectable: true},
	FileKindCandidate: {EntersCompletionReview: true, FormalInput: false, PackageSelectable: false},
	FileKindProcess:   {EntersCompletionReview: false, FormalInput: false, PackageSelectable: true},
	FileKindExternal:  {EntersCompletionReview: false, FormalInput: false, PackageSelectable: true},
}

// FileBoundary 取一类文件的边界事实；未知类型按最保守取值（三列全 false），
// 不给意外的文件类型开任何口子。
func FileBoundary(kind string) FileBoundaryFacts {
	return fileBoundaries[kind]
}

// ValidateTaskFileKind 校验任务文件类型：只有过程文件与重要外部材料两值，
// 交付物内容不走这条路径。
func ValidateTaskFileKind(kind string) error {
	if kind != TaskFileProcess && kind != TaskFileExternal {
		return ErrTaskFileKindInvalid
	}
	return nil
}

var taskFileKindLabels = map[string]string{
	TaskFileProcess:  "过程文件",
	TaskFileExternal: "重要外部材料",
}

// TaskFileKindLabel 任务文件类型显示文案（派生字段）；未知类型退化为通用词，不回显枚举原文。
func TaskFileKindLabel(kind string) string {
	if label, ok := taskFileKindLabels[kind]; ok {
		return label
	}
	return "任务文件"
}

// CanManageTaskFiles 判定能否上传／删除任务文件：与配置输出同一批人
// （负责人／创建人／可编辑项目者）。已关闭任务不再接受任何写入；
// 已完成任务仍可补录——这两类文件不进审批、不影响就绪与任何派生判定，
// 补一份过程文件或外部材料不改变任何既成事实。
func CanManageTaskFiles(a Actor, userID int64, t TaskFacts) bool {
	if !CanWriteProject(a) || t.Status == TaskCancelled {
		return false
	}
	return userID == t.OwnerID || userID == t.CreatorID || CanEditProject(a)
}

// ValidateTaskFileNote 校验背景说明：选填，上限 500 字。
func ValidateTaskFileNote(note string) error {
	if utf8.RuneCountInString(strings.TrimSpace(note)) > 500 {
		return ErrTaskFileNoteTooLong
	}
	return nil
}
