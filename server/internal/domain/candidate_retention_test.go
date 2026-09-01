package domain

import "testing"

// 裁决 #165：完成申请退回后保留候选附件。
// 负责人可逐个删除候选文件（口径与登记候选一致）；成果更新退回后进程回到「已发起」，
// 候选保留、可继续删除／重传／重新提交。

func TestCanDeleteCandidate(t *testing.T) {
	owner := int64(5)
	kr := int64(7)
	facts := func(status, resultUpdate string) TaskFacts {
		return TaskFacts{Status: status, OwnerID: owner, KrOwnerID: &kr, ResultUpdate: resultUpdate}
	}
	cases := []struct {
		name  string
		actor Actor
		user  int64
		t     TaskFacts
		want  bool
	}{
		{"负责人进行中可删", Actor{Role: RoleMember}, owner, facts(TaskInProgress, ""), true},
		{"负责人未开始可删", Actor{Role: RoleMember}, owner, facts(TaskNotStarted, ""), true},
		{"负责人等待输入可删", Actor{Role: RoleMember}, owner, facts(TaskWaitingInput, ""), true},
		{"管理员可删（纠错口径）", Actor{Role: RoleAdmin}, 9, facts(TaskInProgress, ""), true},
		{"无关成员不可删", Actor{Role: RoleMember}, 9, facts(TaskInProgress, ""), false},
		{"完成申请在审不可删（候选已随申请快照）", Actor{Role: RoleMember}, owner, facts(TaskPendingFinalReview, ""), false},
		{"或签在审不可删", Actor{Role: RoleMember}, owner, facts(TaskPendingIntermediateReview, ""), false},
		{"成果更新已发起可删", Actor{Role: RoleMember}, owner, facts(TaskCompleted, ResultUpdateOpen), true},
		{"成果更新在审不可删", Actor{Role: RoleMember}, owner, facts(TaskCompleted, ResultUpdateReviewing), false},
		{"已完成且无成果更新不可删", Actor{Role: RoleMember}, owner, facts(TaskCompleted, ""), false},
		{"已关闭不可删", Actor{Role: RoleMember}, owner, facts(TaskCancelled, ""), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanDeleteCandidate(tc.actor, tc.user, tc.t); got != tc.want {
				t.Fatalf("CanDeleteCandidate() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResultUpdateStateAfterDecision(t *testing.T) {
	if got := ResultUpdateStateAfterDecision(true); got != ResultUpdateNone {
		t.Fatalf("通过后成果更新进程应结束: %q", got)
	}
	if got := ResultUpdateStateAfterDecision(false); got != ResultUpdateOpen {
		t.Fatalf("退回后应回到已发起（候选保留，可删改后重新提交）: %q", got)
	}
}
