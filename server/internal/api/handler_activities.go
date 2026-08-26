package api

import (
	"context"
	"log"

	"github.com/jackc/pgx/v5/pgtype"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// 任务动态写入（词汇表「任务动态」；ADR 0002）。
// 动态是留痕不是业务前置条件：写失败只记日志，不回滚已经发生的业务动作。

// recordActivity 追加一条任务动态。actorID 为空表示系统派生事件；
// 卡点类动态带合成键，走去重写入（写触发 diff 与每小时 ticker 可能记同一条，ADR 0001）。
func (s *Server) recordActivity(ctx context.Context, a domain.TaskActivity) {
	if a.BlockerKey != "" {
		if err := s.recordBlockerActivity(ctx, a); err != nil {
			log.Printf("record blocker activity failed: task=%d key=%s err=%v", a.TaskID, a.BlockerKey, err)
		}
		return
	}
	if _, err := s.q.CreateTaskActivity(ctx, store.CreateTaskActivityParams{
		TaskID:     a.TaskID,
		Kind:       a.Kind,
		ActorID:    toPgInt8(a.ActorID),
		Summary:    a.Summary,
		OccurredAt: pgtype.Timestamptz{Time: a.OccurredAt, Valid: true},
	}); err != nil {
		log.Printf("record task activity failed: task=%d kind=%s err=%v", a.TaskID, a.Kind, err)
	}
}

// actionActivity 记录一条由人触发的动态；summary 由调用方按动作定型，退回类带上理由。
func (s *Server) actionActivity(ctx context.Context, taskID int64, kind string, actorID int64, detail string) {
	summary := domain.ActivityKindLabel(kind)
	if detail != "" {
		summary += "：" + detail
	}
	s.recordActivity(ctx, domain.TaskActivity{
		TaskID: taskID, Kind: kind, ActorID: &actorID, Summary: summary, OccurredAt: s.now(),
	})
}

// blockerSnapshot 取项目当前派生卡点，用于写操作前后对比（ADR 0001）。
// 取不到时返回 nil，调用方按「这次不比对」处理，不影响业务动作。
func (s *Server) blockerSnapshot(ctx context.Context, projectID int64) []domain.Blocker {
	bs, err := s.projectBlockers(ctx, projectID)
	if err != nil {
		log.Printf("blocker snapshot failed: project=%d err=%v", projectID, err)
		return nil
	}
	return bs
}

// recordBlockerChanges 比对写操作前后的卡点集合并把差异写成动态。
// before 为 nil 表示这次没取到快照，跳过比对——宁可缺一条留痕，也不产生假的「卡点出现」。
func (s *Server) recordBlockerChanges(ctx context.Context, projectID int64, before []domain.Blocker) {
	if before == nil {
		return
	}
	after := s.blockerSnapshot(ctx, projectID)
	if after == nil {
		return
	}
	for _, a := range domain.BlockerActivityDiff(before, after, s.now()) {
		s.recordActivity(ctx, a)
	}
}

// activityList 任务动态视图（最新在前）。
func (s *Server) activityList(ctx context.Context, taskID int64) ([]TaskActivity, error) {
	rows, err := s.q.ListTaskActivitiesByTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	out := make([]TaskActivity, 0, len(rows))
	for _, a := range rows {
		item := TaskActivity{
			Id:         a.ID,
			Kind:       TaskActivityKind(a.Kind),
			KindLabel:  domain.ActivityKindLabel(a.Kind),
			Summary:    a.Summary,
			OccurredAt: a.OccurredAt.Time,
		}
		item.ActorName = fromPgText(a.ActorName)
		out = append(out, item)
	}
	return out, nil
}
