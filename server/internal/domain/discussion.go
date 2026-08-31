package domain

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var (
	ErrDiscussionEmpty   = errors.New("讨论意见不能为空")
	ErrDiscussionTooLong = errors.New("讨论意见不能超过 2000 字")
	ErrMentionNotMember  = errors.New("只能 @ 项目内成员")
)

// 站内通知类型（词汇表「站内通知」）。
const (
	NotifyDiscussionMention = "discussion_mention"
	NotifyDiscussionOwner   = "discussion_owner"
)

// CanDiscuss 判定能否提交任务讨论：全体项目内成员（含只读）与项目负责人（AC-35、§3.3）。
// 公开项目的隐式访客除外（#111）：它不落成员表，发言会在任务里留下一条项目外的人写的事实，
// 这也是隐式访客与显式访客唯一的读写差别。
func CanDiscuss(a Actor) bool {
	if a.Implicit {
		return false
	}
	return a.IsOwner || a.Role != ""
}

// ValidateDiscussion 校验讨论内容与被 @ 成员（只能 @ 项目内成员）。
func ValidateDiscussion(content string, mentions []int64, isMember func(int64) bool) error {
	c := strings.TrimSpace(content)
	if c == "" {
		return ErrDiscussionEmpty
	}
	if utf8.RuneCountInString(c) > 2000 {
		return ErrDiscussionTooLong
	}
	for _, id := range mentions {
		if !isMember(id) {
			return ErrMentionNotMember
		}
	}
	return nil
}

// DiscussionNotifyTargets 派生通知对象：任务负责人在前、被 @ 成员随后，去重且不含作者本人（AC-36）。
func DiscussionNotifyTargets(authorID, taskOwnerID int64, mentions []int64) []int64 {
	seen := map[int64]bool{authorID: true}
	out := []int64{}
	for _, id := range append([]int64{taskOwnerID}, mentions...) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
