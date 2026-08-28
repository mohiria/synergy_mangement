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

// SweepBlockerActivities 扫描一遍活跃项目，补记时间型卡点的「卡点出现」动态。
// 补记是幂等的：同一条卡点的时间戳与合成键不随扫描时刻变化，落库唯一键挡住重复记账，
// 因此重复扫描、进程重启后重扫都不会重记；只要卡点还在，漏掉的一次扫描下次会补上。
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
		_ = s.files.Remove(ctx, key)
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
		s.SweepAuthState(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.SweepBlockerActivities(ctx)
				s.SweepStaleUploads(ctx)
				s.SweepAuthState(ctx)
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

// recordBlockerActivity 落一条卡点动态；重复的（同任务、同类型、同合成键、同发生时刻）忽略。
func (s *Server) recordBlockerActivity(ctx context.Context, a domain.TaskActivity) error {
	_, err := s.q.CreateBlockerActivity(ctx, store.CreateBlockerActivityParams{
		TaskID:     a.TaskID,
		Kind:       a.Kind,
		Summary:    a.Summary,
		OccurredAt: pgtype.Timestamptz{Time: a.OccurredAt, Valid: true},
		BlockerKey: toPgText(a.BlockerKey),
	})
	return err
}
