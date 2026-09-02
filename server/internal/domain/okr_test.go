package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func i64(v int64) *int64 { return &v }

func day(s string) *time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return &t
}

// AC-01（裁决 12，#183 修订）：KR 只剩结构字段（描述、量化指标），无负责人与周期校验。
func TestValidateOkrBatch(t *testing.T) {
	kr := func(mut func(*NewKeyResult)) NewKeyResult {
		k := NewKeyResult{Description: "上线转化率提升到 5%"}
		if mut != nil {
			mut(&k)
		}
		return k
	}

	cases := []struct {
		name  string
		items []OkrBatchItem
		want  error
	}{
		{"空批量", nil, ErrOkrBatchEmpty},
		{"新建 O 带两条 KR", []OkrBatchItem{{
			Title:      "提升产品体验",
			KeyResults: []NewKeyResult{kr(nil), kr(nil)},
		}}, nil},
		{"新建 O 不带 KR 允许", []OkrBatchItem{{Title: "提升产品体验"}}, nil},
		{"title 与 objectiveId 同时给", []OkrBatchItem{{
			ObjectiveID: i64(9),
			Title:       "提升产品体验",
			KeyResults:  []NewKeyResult{kr(nil)},
		}}, ErrOkrItemAmbiguous},
		{"title 与 objectiveId 都不给", []OkrBatchItem{{
			KeyResults: []NewKeyResult{kr(nil)},
		}}, ErrOkrItemAmbiguous},
		{"已有 O 追加 KR", []OkrBatchItem{{
			ObjectiveID: i64(9),
			KeyResults:  []NewKeyResult{kr(nil)},
		}}, nil},
		{"已有 O 无 KR", []OkrBatchItem{{ObjectiveID: i64(9)}}, ErrOkrExistingNoKRs},
		{"O 标题全空白", []OkrBatchItem{{Title: "  "}}, ErrObjectiveTitleEmpty},
		{"O 标题超 100 字", []OkrBatchItem{{Title: strings.Repeat("目", 101)}}, ErrObjectiveTitleTooLong},
		{"O 说明超 500 字", []OkrBatchItem{{
			Title:       "提升产品体验",
			Description: strings.Repeat("说", 501),
		}}, ErrObjectiveDescTooLong},
		{"KR 描述空", []OkrBatchItem{{
			Title:      "提升产品体验",
			KeyResults: []NewKeyResult{kr(func(k *NewKeyResult) { k.Description = " " })},
		}}, ErrKrDescriptionEmpty},
		{"KR 描述超 200 字", []OkrBatchItem{{
			Title:      "提升产品体验",
			KeyResults: []NewKeyResult{kr(func(k *NewKeyResult) { k.Description = strings.Repeat("述", 201) })},
		}}, ErrKrDescriptionTooLong},
		{"量化指标超 100 字", []OkrBatchItem{{
			Title:      "提升产品体验",
			KeyResults: []NewKeyResult{kr(func(k *NewKeyResult) { k.Metric = strings.Repeat("标", 101) })},
		}}, ErrKrMetricTooLong},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ValidateOkrBatch(tc.items)
			if !errors.Is(got, tc.want) {
				t.Fatalf("ValidateOkrBatch() = %v, want %v", got, tc.want)
			}
		})
	}
}
