package domain

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// 参与人名单校验（词汇表「参与人」；主 PRD §4.1、§9.2）：
// 须为项目成员；负责人已单列，不重复出现在参与人里。
func TestValidateParticipants(t *testing.T) {
	members := map[int64]bool{1: true, 2: true, 3: true, 5: true}
	isMember := func(id int64) bool { return members[id] }
	const ownerID = int64(5)
	cases := []struct {
		name string
		ids  []int64
		want error
	}{
		{"不配置参与人", nil, nil},
		{"空名单等于清空", []int64{}, nil},
		{"若干项目成员", []int64{1, 3}, nil},
		{"访客也可以是参与人", []int64{2}, nil},
		{"参与人不是项目成员", []int64{1, 9}, ErrParticipantNotMember},
		{"负责人不再列为参与人", []int64{1, ownerID}, ErrParticipantIsOwner},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := ValidateParticipants(ownerID, c.ids, isMember); !errors.Is(err, c.want) {
				t.Fatalf("ValidateParticipants = %v, want %v", err, c.want)
			}
		})
	}
}

// 名单去重并保持选择顺序：同一人重复勾选不算错，按一人记。
func TestNormalizeParticipants(t *testing.T) {
	got := NormalizeParticipants([]int64{3, 1, 3, 1, 7})
	want := []int64{3, 1, 7}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeParticipants = %v, want %v", got, want)
	}
	if got := NormalizeParticipants(nil); len(got) != 0 {
		t.Fatalf("空名单应归一为空，得到 %v", got)
	}
}

// 配置权限与交付物项、成果审核人同口径：负责人／创建人／可编辑项目者，终态不可。
func TestCanManageParticipants(t *testing.T) {
	facts := TaskFacts{Status: TaskInProgress, CreatorID: 3, OwnerID: 5}
	cases := []struct {
		name  string
		actor Actor
		user  int64
		facts TaskFacts
		want  bool
	}{
		{"负责人可配置", Actor{Role: RoleMember}, 5, facts, true},
		// 裁决 D2（#137）：创建人只在草稿期保留编辑权。
		{"创建人草稿期可配置", Actor{Role: RoleMember}, 3, TaskFacts{Status: TaskDraft, CreatorID: 3, OwnerID: 5}, true},
		{"创建人入池后不可配置", Actor{Role: RoleMember}, 3, facts, false},
		{"可编辑项目者可配置", Actor{Role: RoleAdmin}, 9, facts, true},
		{"无关成员不可配置", Actor{Role: RoleMember}, 9, facts, false},
		{"访客不可配置", Actor{Role: RoleViewer}, 5, facts, false},
		{"已完成任务不可配置", Actor{Role: RoleMember}, 5, TaskFacts{Status: TaskCompleted, CreatorID: 3, OwnerID: 5}, false},
		{"已关闭任务不可配置", Actor{Role: RoleMember}, 5, TaskFacts{Status: TaskCancelled, CreatorID: 3, OwnerID: 5}, false},
		{"审核中仍可配置（不属关键字段，不影响审批）", Actor{Role: RoleMember}, 5, TaskFacts{Status: TaskPendingFinalReview, CreatorID: 3, OwnerID: 5}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CanManageParticipants(c.actor, c.user, c.facts); got != c.want {
				t.Fatalf("CanManageParticipants = %v, want %v", got, c.want)
			}
		})
	}
}

// 负向约束（词汇表「参与人」：不产生待办、不进审批链、不影响权限、不参与归组与排序）。
// 这些口径靠「参与人根本不是判定的输入」来保证，所以直接对派生入口的字段集下断言：
// 一旦有人把参与人塞进这些事实结构，本测试立刻变红。
func TestParticipantsAreNotDerivationInput(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeOf(TaskFacts{}),    // 权限、审批链与状态派生
		reflect.TypeOf(MyWorkFacts{}),  // 我的工作五组归类与排序
		reflect.TypeOf(WorkTaskFact{}), // 单张卡片
		reflect.TypeOf(BlockerFacts{}), // 结构化卡点
	} {
		for i := 0; i < typ.NumField(); i++ {
			if name := typ.Field(i).Name; strings.Contains(strings.ToLower(name), "participant") {
				t.Fatalf("%s.%s：参与人不得进入派生输入", typ.Name(), name)
			}
		}
	}
}

// 只是某个任务的参与人，不会因此在「我的工作」里收到任何事项。
func TestParticipantGetsNoWorkItems(t *testing.T) {
	const participantID = int64(7)
	krOwner := int64(3)
	groups := MyWork(MyWorkFacts{
		UserID: participantID,
		Actor:  Actor{Role: RoleMember},
		Now:    time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC),
		Tasks: []WorkTaskFact{{
			ID:            1,
			Name:          "整理评审材料",
			DisplayStatus: TaskInProgress,
			OwnerID:       9,
			CreatorID:     9,
			KrOwnerID:     &krOwner,
		}},
	})
	n := len(groups.Pending) + len(groups.Approvals) + len(groups.Receipts) + len(groups.Waiting) + len(groups.Blockers)
	if n != 0 {
		t.Fatalf("参与人不应收到任何事项，得到 %d 条", n)
	}
}
