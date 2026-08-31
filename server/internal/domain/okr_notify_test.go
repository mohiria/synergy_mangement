package domain

import (
	"reflect"
	"testing"
)

// OkrNotifyTargets（#125）：本批 KR 负责人去重、剔除操作者本人与未指定负责人，按 ID 升序。
func TestOkrNotifyTargets(t *testing.T) {
	id := func(v int64) *int64 { return &v }
	kr := func(owner *int64) NewKeyResult { return NewKeyResult{Description: "kr", OwnerID: owner} }
	cases := []struct {
		name  string
		actor int64
		items []OkrBatchItem
		want  []int64
	}{
		{name: "空批次无通知", actor: 1, items: nil, want: []int64{}},
		{
			name:  "跨 O 去重并升序",
			actor: 1,
			items: []OkrBatchItem{
				{Title: "O1", KeyResults: []NewKeyResult{kr(id(9)), kr(id(4))}},
				{ObjectiveID: id(7), KeyResults: []NewKeyResult{kr(id(4)), kr(id(2))}},
			},
			want: []int64{2, 4, 9},
		},
		{
			name:  "操作者本人不收",
			actor: 4,
			items: []OkrBatchItem{
				{Title: "O1", KeyResults: []NewKeyResult{kr(id(4)), kr(id(6))}},
			},
			want: []int64{6},
		},
		{
			name:  "未指定负责人的行跳过",
			actor: 1,
			items: []OkrBatchItem{
				{Title: "O1", KeyResults: []NewKeyResult{kr(nil), kr(id(3))}},
			},
			want: []int64{3},
		},
		{
			name:  "全是本人则空",
			actor: 5,
			items: []OkrBatchItem{
				{Title: "O1", KeyResults: []NewKeyResult{kr(id(5)), kr(id(5))}},
			},
			want: []int64{},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := OkrNotifyTargets(c.actor, c.items)
			if got == nil {
				got = []int64{}
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("OkrNotifyTargets = %v, want %v", got, c.want)
			}
		})
	}
}
