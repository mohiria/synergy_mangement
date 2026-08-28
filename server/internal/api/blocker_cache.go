package api

import (
	"context"
	"sync"

	"synergy/server/internal/domain"
)

// 请求内的卡点派生记忆化（P1）。
//
// 一次任务写请求此前要跑 30+ 条项目级查询，其中一半来自同一份卡点被算了两遍：
// 写路径装饰器在写后取「变更后快照」，紧接着 writeTask → taskList 又整套重算一次。
// 两次都发生在同一次提交之后、看到的是同一份事实，没有理由算两遍。
//
// 记忆化只在一次请求内有效，并且只覆盖「写之后」这一段：装饰器取完变更前快照就把
// 缓存清空，handler 自己的写入不会读到过期结果。跨请求不缓存——卡点是读时派生的，
// 时间一走结论就可能变。

type blockerCacheKey struct{}

// blockerCache 一次请求内按项目缓存派生卡点。
type blockerCache struct {
	mu   sync.Mutex
	byID map[int64][]domain.Blocker
}

func newBlockerCache() *blockerCache {
	return &blockerCache{byID: map[int64][]domain.Blocker{}}
}

func (c *blockerCache) get(projectID int64) ([]domain.Blocker, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	bs, ok := c.byID[projectID]
	return bs, ok
}

func (c *blockerCache) put(projectID int64, bs []domain.Blocker) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byID[projectID] = bs
}

func (c *blockerCache) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byID = map[int64][]domain.Blocker{}
}

// withBlockerCache 给请求上下文挂一个记忆化容器。
func withBlockerCache(ctx context.Context) (context.Context, *blockerCache) {
	c := newBlockerCache()
	return context.WithValue(ctx, blockerCacheKey{}, c), c
}

func blockerCacheFrom(ctx context.Context) *blockerCache {
	c, _ := ctx.Value(blockerCacheKey{}).(*blockerCache)
	return c
}
