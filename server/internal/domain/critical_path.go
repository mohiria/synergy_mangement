package domain

// 硬依赖分析（词汇表「互锁风险」「硬依赖链」；PRD §4.4、AC-10）。

// HardEdge 硬前置交付物边（有向）。
type HardEdge struct {
	ID     int64
	Source int64
	Target int64
}

// HardEdgeAnalysis 分析结果：互锁边集、关键路径边集与是否可计算。
type HardEdgeAnalysis struct {
	Interlocked           map[int64]bool
	OnCriticalPath        map[int64]bool
	CriticalPathAvailable bool
}

// AnalyzeHardEdges 分析硬前置边（AC-10）：
// 1) 循环检测——反复剥离入度或出度为 0 的节点，剩余节点即在循环中；两端都在循环中的边标互锁；
// 2) 互锁边被排除（循环部分暂停传统关键路径计算）；
// 3) 剩余 DAG 上以任务工期为权做最长路径；任一相关任务缺工期则只确认硬依赖链、不宣称关键路径。
func AnalyzeHardEdges(edges []HardEdge, durationDays map[int64]int) HardEdgeAnalysis {
	res := HardEdgeAnalysis{Interlocked: map[int64]bool{}, OnCriticalPath: map[int64]bool{}}
	if len(edges) == 0 {
		res.CriticalPathAvailable = true
		return res
	}

	// —— 循环检测 ——
	nodes := map[int64]bool{}
	for _, e := range edges {
		nodes[e.Source] = true
		nodes[e.Target] = true
	}
	alive := map[int64]bool{}
	for n := range nodes {
		alive[n] = true
	}
	for {
		indeg := map[int64]int{}
		outdeg := map[int64]int{}
		for _, e := range edges {
			if alive[e.Source] && alive[e.Target] {
				indeg[e.Target]++
				outdeg[e.Source]++
			}
		}
		removed := false
		for n := range alive {
			if indeg[n] == 0 || outdeg[n] == 0 {
				delete(alive, n)
				removed = true
			}
		}
		if !removed {
			break
		}
	}
	for _, e := range edges {
		if alive[e.Source] && alive[e.Target] {
			res.Interlocked[e.ID] = true
		}
	}

	// —— 关键路径（排除互锁边的 DAG 最长路径）——
	dag := make([]HardEdge, 0, len(edges))
	involved := map[int64]bool{}
	for _, e := range edges {
		if !res.Interlocked[e.ID] {
			dag = append(dag, e)
			involved[e.Source] = true
			involved[e.Target] = true
		}
	}
	for n := range involved {
		if _, ok := durationDays[n]; !ok {
			// 日期不足：只显示硬依赖链。
			res.CriticalPathAvailable = false
			return res
		}
	}
	res.CriticalPathAvailable = true
	// 拓扑序 DP：dist[n] = 以 n 结束的最长累计工期；记录前驱边回溯标记。
	indeg := map[int64]int{}
	next := map[int64][]HardEdge{}
	for _, e := range dag {
		indeg[e.Target]++
		next[e.Source] = append(next[e.Source], e)
	}
	queue := []int64{}
	dist := map[int64]int{}
	prevEdge := map[int64]*HardEdge{}
	for n := range involved {
		if indeg[n] == 0 {
			queue = append(queue, n)
			dist[n] = durationDays[n]
		}
	}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for i := range next[n] {
			e := next[n][i]
			cand := dist[n] + durationDays[e.Target]
			if cand > dist[e.Target] {
				dist[e.Target] = cand
				ec := e
				prevEdge[e.Target] = &ec
			}
			indeg[e.Target]--
			if indeg[e.Target] == 0 {
				queue = append(queue, e.Target)
			}
		}
	}
	// 终点取 dist 最大者，沿前驱边回溯标记关键路径。
	var endNode int64
	best := -1
	for n, d := range dist {
		if d > best {
			best = d
			endNode = n
		}
	}
	for cur := endNode; ; {
		e := prevEdge[cur]
		if e == nil {
			break
		}
		res.OnCriticalPath[e.ID] = true
		cur = e.Source
	}
	return res
}
