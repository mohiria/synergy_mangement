package domain

import (
	"errors"
	"reflect"
	"testing"
)

// AC-35：全体项目成员（含只读）可提交讨论；非项目成员不可。
func TestCanDiscuss(t *testing.T) {
	cases := []struct {
		name  string
		actor Actor
		want  bool
	}{
		{"管理员可讨论", Actor{Role: RoleAdmin}, true},
		{"项目成员可讨论", Actor{Role: RoleMember}, true},
		{"访客可讨论", Actor{Role: RoleViewer}, true},
		{"项目负责人非成员可讨论", Actor{IsOwner: true}, true},
		{"非成员不可讨论", Actor{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanDiscuss(tc.actor); got != tc.want {
				t.Fatalf("CanDiscuss(%+v) = %v, want %v", tc.actor, got, tc.want)
			}
		})
	}
}

// 讨论内容与 @ 成员校验。
func TestValidateDiscussion(t *testing.T) {
	members := map[int64]bool{3: true, 5: true}
	isMember := func(id int64) bool { return members[id] }
	cases := []struct {
		name     string
		content  string
		mentions []int64
		want     error
	}{
		{"合法意见", "建议补充回退场景。", []int64{3}, nil},
		{"空内容", "   ", nil, ErrDiscussionEmpty},
		{"超长内容", repeat("长", 2001), nil, ErrDiscussionTooLong},
		{"@ 非项目成员", "见附件", []int64{99}, ErrMentionNotMember},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidateDiscussion(tc.content, tc.mentions, isMember); !errors.Is(got, tc.want) {
				t.Fatalf("ValidateDiscussion() = %v, want %v", got, tc.want)
			}
		})
	}
}

// AC-36：新增意见只通知任务负责人和被 @ 成员；去重且不通知作者本人。
func TestDiscussionNotifyTargets(t *testing.T) {
	cases := []struct {
		name     string
		author   int64
		owner    int64
		mentions []int64
		want     []int64
	}{
		{"负责人加被 @ 成员", 3, 5, []int64{7}, []int64{5, 7}},
		{"负责人即作者时不通知自己", 5, 5, []int64{7}, []int64{7}},
		{"被 @ 含负责人时去重", 3, 5, []int64{5, 7}, []int64{5, 7}},
		{"被 @ 含作者时不通知作者", 3, 5, []int64{3}, []int64{5}},
		{"无 @ 只通知负责人", 3, 5, nil, []int64{5}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DiscussionNotifyTargets(tc.author, tc.owner, tc.mentions)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("DiscussionNotifyTargets() = %v, want %v", got, tc.want)
			}
		})
	}
}
