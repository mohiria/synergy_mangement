package domain

import "testing"

// AC-10：硬前置循环 → 互锁风险；循环部分暂停关键路径计算；
// 反馈边不参与（调用方只传硬前置边）；日期不足时只确认硬依赖链。
func TestAnalyzeHardEdges(t *testing.T) {
	dur := map[int64]int{1: 5, 2: 3, 3: 4, 4: 2, 5: 6}

	t.Run("无循环时计算关键路径", func(t *testing.T) {
		// 1→2→3（5+3+4=12） 与 1→4（5+2=7）：关键路径为 1→2→3。
		edges := []HardEdge{
			{ID: 11, Source: 1, Target: 2},
			{ID: 12, Source: 2, Target: 3},
			{ID: 13, Source: 1, Target: 4},
		}
		res := AnalyzeHardEdges(edges, dur)
		if !res.CriticalPathAvailable {
			t.Fatalf("日期齐备应可计算关键路径: %+v", res)
		}
		if len(res.Interlocked) != 0 {
			t.Fatalf("无循环不应有互锁: %+v", res.Interlocked)
		}
		if !res.OnCriticalPath[11] || !res.OnCriticalPath[12] || res.OnCriticalPath[13] {
			t.Fatalf("关键路径边集异常: %+v", res.OnCriticalPath)
		}
	})

	t.Run("硬前置循环标互锁并暂停该部分计算", func(t *testing.T) {
		// 2→3→2 构成循环；1→2 与 3→5 在循环外。
		edges := []HardEdge{
			{ID: 21, Source: 1, Target: 2},
			{ID: 22, Source: 2, Target: 3},
			{ID: 23, Source: 3, Target: 2},
			{ID: 24, Source: 3, Target: 5},
		}
		res := AnalyzeHardEdges(edges, dur)
		if !res.Interlocked[22] || !res.Interlocked[23] {
			t.Fatalf("循环边应标互锁: %+v", res.Interlocked)
		}
		if res.Interlocked[21] || res.Interlocked[24] {
			t.Fatalf("循环外的边不应标互锁: %+v", res.Interlocked)
		}
		// 循环部分暂停：互锁边不得进入关键路径。
		if res.OnCriticalPath[22] || res.OnCriticalPath[23] {
			t.Fatalf("互锁边不应进入关键路径: %+v", res.OnCriticalPath)
		}
	})

	t.Run("日期不足时只显示硬依赖链", func(t *testing.T) {
		edges := []HardEdge{{ID: 31, Source: 1, Target: 2}}
		partial := map[int64]int{1: 5} // 任务 2 无工期
		res := AnalyzeHardEdges(edges, partial)
		if res.CriticalPathAvailable {
			t.Fatalf("日期不足不应宣称关键路径: %+v", res)
		}
		if len(res.OnCriticalPath) != 0 {
			t.Fatalf("日期不足不应派生关键路径边: %+v", res.OnCriticalPath)
		}
	})
}
