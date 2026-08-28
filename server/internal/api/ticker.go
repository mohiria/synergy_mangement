package api

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// 时间型卡点的动态留痕（ADR 0001）。
//
// 审批超时（N×24h）与任务超期只随时间流逝出现，没有对应的业务写操作，写触发的卡点 diff
// 抓不到它们的「出现」。按 ADR 0001：进程内单 time.Ticker 每小时扫描活跃项目补记动态，
// 事件时间戳取计算出的真实发生时刻。界面显示始终读时派生，不依赖本 ticker。

// BlockerSweepInterval ticker 扫描间隔（ADR 0001 定为每小时）。
const BlockerSweepInterval = time.Hour

// SweepBlockerActivities 扫描一遍活跃项目：补记时间型卡点的「卡点出现」，
// 并补记所有悬空的「卡点解除」（R9）。
// 出现的补记幂等靠「自上次解除以来只记一条」判定，不再依赖含发生时刻的唯一键——
// 上游未就绪取任务开始日、任务超期取截止日，都是常量，旧口径会让二次出现被静默丢弃。
// 解除的补记则解决另一半问题：时间型卡点消失时没有任何写操作可依附，
// 不在这里补就永远留下「出现却无解除」的悬空条目。
func (s *Server) SweepBlockerActivities(ctx context.Context) {
	ids, err := s.q.ListActiveProjectIDs(ctx)
	if err != nil {
		log.Printf("blocker sweep: list active projects failed: %v", err)
		return
	}
	for _, projectID := range ids {
		if ctx.Err() != nil {
			return
		}
		blockers, err := s.projectBlockers(ctx, projectID)
		if err != nil {
			log.Printf("blocker sweep: derive project=%d failed: %v", projectID, err)
			continue
		}
		for _, a := range domain.TimeTriggeredBlockerActivities(blockers) {
			s.recordActivity(ctx, a)
		}
		s.sweepStaleBlockerResolutions(ctx, projectID, blockers)
	}
}

// sweepStaleBlockerResolutions 把「留痕里还记着出现、但卡点已经不成立」的那些补记为解除。
func (s *Server) sweepStaleBlockerResolutions(ctx context.Context, projectID int64, current []domain.Blocker) {
	rows, err := s.q.ListOpenBlockerActivities(ctx, projectID)
	if err != nil {
		log.Printf("blocker sweep: list open activities project=%d failed: %v", projectID, err)
		return
	}
	open := make([]domain.OpenBlockerFact, 0, len(rows))
	for _, row := range rows {
		if row.Kind != domain.ActivityBlockerOpened {
			continue // 最近一条已经是解除，说明这条卡点已经收尾
		}
		open = append(open, domain.OpenBlockerFact{
			TaskID: row.TaskID, Key: row.BlockerKey.String, Summary: row.Summary,
		})
	}
	for _, a := range domain.StaleBlockerResolutions(open, current, s.now()) {
		s.recordActivity(ctx, a)
	}
}

// staleUploadAge 待上传记录的存活上限：预签名地址过期后客户端不会再来确认，留着只会占住
// 「同一交付物项至多一条待上传」的名额。
const staleUploadAge = 2 * presignExpiry

// SweepStaleUploads 清理迟迟未确认的两阶段上传（R4）：删掉过期的待上传记录与其占位对象，
// 输入请求回到已接受状态等对接人重提。
func (s *Server) SweepStaleUploads(ctx context.Context) {
	interval := pgtype.Interval{Microseconds: staleUploadAge.Microseconds(), Valid: true}
	keys, err := s.q.DeleteStaleUploadingFiles(ctx, interval)
	if err != nil {
		log.Printf("upload sweep: 清理候选待上传记录失败: %v", err)
	}
	inputKeys, err := s.q.ResetStaleInputUploads(ctx, interval)
	if err != nil {
		log.Printf("upload sweep: 重置输入待上传记录失败: %v", err)
	}
	for _, key := range append(keys, inputKeys...) {
		if key == "" {
			continue
		}
		s.removeObject(ctx, key)
	}
}

// pendingDeletionBatch 每轮补偿删除的处理条数上限，避免单次扫描占住 ticker。
const pendingDeletionBatch = 200

// SweepPendingObjectDeletions 重试此前失败的对象删除（E3）：
// 删成功就出队，仍失败就累加次数留到下一轮——「永久删除」因此是可验证的最终状态，
// 而不是一次尽力而为。
func (s *Server) SweepPendingObjectDeletions(ctx context.Context) {
	rows, err := s.q.ListPendingObjectDeletions(ctx, pendingDeletionBatch)
	if err != nil {
		log.Printf("object deletion sweep: list failed: %v", err)
		return
	}
	for _, row := range rows {
		if ctx.Err() != nil {
			return
		}
		if err := s.files.Remove(ctx, row.ObjectKey); err != nil {
			if qerr := s.q.EnqueueObjectDeletion(ctx, store.EnqueueObjectDeletionParams{
				ObjectKey: row.ObjectKey, LastError: err.Error(),
			}); qerr != nil {
				log.Printf("object deletion sweep: requeue failed: key=%s err=%v", row.ObjectKey, qerr)
			}
			continue
		}
		if err := s.q.DeletePendingObjectDeletion(ctx, row.ObjectKey); err != nil {
			log.Printf("object deletion sweep: dequeue failed: key=%s err=%v", row.ObjectKey, err)
		}
	}
}

// SweepAuthState 清理认证侧的过期残留（S3）：登录失败计数只活在锁定窗口内，
// 留着只会让 map 随随机用户名无限膨胀；过期会话行同理，登出与滑动续期都不会删它们。
func (s *Server) SweepAuthState(ctx context.Context) {
	s.throttle.Sweep(s.now())
	if _, err := s.q.DeleteExpiredSessions(ctx); err != nil {
		log.Printf("auth sweep: 清理过期会话失败: %v", err)
	}
}

// StartBlockerActivityTicker 启动进程内单 ticker（ADR 0001：无外部定时设施）。
// 启动时立即扫一次——进程停机期间出现的时间型卡点因此不会漏记。返回的函数停止并等待退出。
func (s *Server) StartBlockerActivityTicker(ctx context.Context, interval time.Duration) func() {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		s.SweepBlockerActivities(ctx)
		s.SweepStaleUploads(ctx)
		s.SweepPendingObjectDeletions(ctx)
		s.SweepAuthState(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.SweepBlockerActivities(ctx)
				s.SweepStaleUploads(ctx)
				s.SweepPendingObjectDeletions(ctx)
				s.SweepAuthState(ctx)
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

// recordBlockerActivity 落一条卡点动态，按「自上次解除以来只记一条出现」去重（R9）：
// 同一条卡点解除后再次出现要能记上，所以去重看的是该键最近一条动态是出现还是解除，
// 而不是发生时刻——上游未就绪与任务超期的发生时刻都是常量，按时刻去重会丢掉二次出现。
func (s *Server) recordBlockerActivity(ctx context.Context, a domain.TaskActivity) error {
	isOpen, err := s.q.BlockerActivityOpen(ctx, store.BlockerActivityOpenParams{
		TaskID: a.TaskID, BlockerKey: toPgText(a.BlockerKey),
	})
	if err != nil {
		return err
	}
	switch a.Kind {
	case domain.ActivityBlockerOpened:
		if isOpen {
			return nil // 已经记过出现且尚未解除
		}
	case domain.ActivityBlockerResolved:
		if !isOpen {
			return nil // 没有待解除的出现事件，不记孤立的解除
		}
	}
	_, err = s.q.CreateBlockerActivity(ctx, store.CreateBlockerActivityParams{
		TaskID:     a.TaskID,
		Kind:       a.Kind,
		Summary:    a.Summary,
		OccurredAt: pgtype.Timestamptz{Time: a.OccurredAt, Valid: true},
		BlockerKey: toPgText(a.BlockerKey),
	})
	return err
}
