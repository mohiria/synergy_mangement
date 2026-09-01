package domain

import (
	"errors"
	"testing"
)

func TestDeliverableName(t *testing.T) {
	cases := []struct {
		name        string
		currentName string
		fileName    string
		want        string
	}{
		{"建项取文件名，去掉扩展名", "", "验收方案.docx", "验收方案"},
		{"多段扩展只去最后一段", "", "验收方案.tar.gz", "验收方案.tar"},
		{"没有扩展名就用整个文件名", "", "验收方案", "验收方案"},
		{"隐藏文件不当作纯扩展名", "", ".gitignore", ".gitignore"},
		{"文件名首尾空白不进项名", "", "  验收方案.pdf  ", "验收方案"},
		{"超长文件名截到 100 字", "", repeat("长", 120) + ".docx", repeat("长", 100)},
		{"成果更新不改项名", "验收方案", "验收方案V2终稿.pdf", "验收方案"},
		{"已有项名与新文件毫不相干也不动", "现场数据包", "别的东西.xlsx", "现场数据包"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DeliverableName(c.currentName, c.fileName); got != c.want {
				t.Fatalf("DeliverableName(%q, %q) = %q, want %q", c.currentName, c.fileName, got, c.want)
			}
		})
	}
}

func TestValidateNewDeliverableName(t *testing.T) {
	cases := []struct {
		name     string
		item     string
		existing []string
		wantErr  error
	}{
		{"新名字通过", "验收方案", []string{"现场数据包"}, nil},
		{"空名字挡下", "   ", nil, ErrDeliverableNameEmpty},
		{"超长挡下", repeat("长", 101), nil, ErrDeliverableNameTooLong},
		{"同任务重名挡下，不静默新建第二项", "验收方案", []string{"现场数据包", "验收方案"}, ErrDeliverableNameDuplicate},
		{"重名比较忽略首尾空白", "验收方案", []string{" 验收方案 "}, ErrDeliverableNameDuplicate},
		{"重名比较忽略英文大小写", "Report", []string{"report"}, ErrDeliverableNameDuplicate},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateNewDeliverableName(c.item, c.existing)
			if c.wantErr == nil {
				if err != nil {
					t.Fatalf("ValidateNewDeliverableName(%q, %v) = %v, want nil", c.item, c.existing, err)
				}
				return
			}
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("ValidateNewDeliverableName(%q, %v) = %v, want %v", c.item, c.existing, err, c.wantErr)
			}
		})
	}
}
