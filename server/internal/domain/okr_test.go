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

func TestValidateOkrBatch(t *testing.T) {
	roles := map[int64]string{1: RoleMember, 2: RoleAdmin, 8: RoleViewer}
	roleOf := func(id int64) string { return roles[id] }

	kr := func(mut func(*NewKeyResult)) NewKeyResult {
		k := NewKeyResult{Description: "上线转化率提升到 5%", OwnerID: i64(1)}
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
		{"KR 负责人非项目成员", []OkrBatchItem{{
			Title:      "提升产品体验",
			KeyResults: []NewKeyResult{kr(func(k *NewKeyResult) { k.OwnerID = i64(99) })},
		}}, ErrKrOwnerNotEligible},
		// #95：访客担任 KR 负责人会让入池、关键字段变更与完成终审无人可推进。
		{"KR 负责人是访客", []OkrBatchItem{{
			Title:      "提升产品体验",
			KeyResults: []NewKeyResult{kr(func(k *NewKeyResult) { k.OwnerID = i64(8) })},
		}}, ErrKrOwnerNotEligible},
		{"KR 负责人是项目管理员", []OkrBatchItem{{
			Title:      "提升产品体验",
			KeyResults: []NewKeyResult{kr(func(k *NewKeyResult) { k.OwnerID = i64(2) })},
		}}, nil},
		{"KR 负责人可不指定", []OkrBatchItem{{
			Title:      "提升产品体验",
			KeyResults: []NewKeyResult{kr(func(k *NewKeyResult) { k.OwnerID = nil })},
		}}, nil},
		{"KR 周期截止早于开始", []OkrBatchItem{{
			Title: "提升产品体验",
			KeyResults: []NewKeyResult{kr(func(k *NewKeyResult) {
				k.Start = day("2026-09-01")
				k.End = day("2026-08-01")
			})},
		}}, ErrKrPeriodInverted},
		{"KR 周期只填开始", []OkrBatchItem{{
			Title: "提升产品体验",
			KeyResults: []NewKeyResult{kr(func(k *NewKeyResult) {
				k.Start = day("2026-09-01")
			})},
		}}, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ValidateOkrBatch(tc.items, roleOf)
			if !errors.Is(got, tc.want) {
				t.Fatalf("ValidateOkrBatch() = %v, want %v", got, tc.want)
			}
		})
	}
}
