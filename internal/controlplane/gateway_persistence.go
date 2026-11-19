// gateway_persistence.go TD-66 P1：网关路由的 store 持久化接线。
//
// 设计：**store 是事实源，gatewayState 是按租户的缓存**。
//   - 写路径（create/update/delete/toggle）先落 store、成功后再更新缓存；store 失败返回 5xx
//     而不是假成功（否则用户以为配好了，重启后发现丢了）。
//   - 读路径（list 与网关匹配）按需从 store 加载：租户的缓存未建立时先 load 再匹配。
//   - 多副本失效：单副本写不到别的副本的内存，故由 startGatewayRouteRefresh 周期
//     （默认 5s）回读已加载租户；指纹（UpdatedAt + 内容）未变则跳过重建，
//     变了则经 newGatewayRouteEntry 重建条目，保证限流器/熔断器与首次加载一致。
//
// 转换：extension.RouteRule（域形态）↔ model.GatewayRoute（持久化形态）。
// 两者字段一一对应；转换薄薄一层，换来 store 不依赖 extension 的分层干净。
package controlplane

import (
	"context"
	"hash/fnv"
	"sort"
	"strconv"
	"time"

	"github.com/Levango7/OpsMesh/internal/extension"
	"github.com/Levango7/OpsMesh/internal/store/model"
)

// routeRuleToGateway 域形态 → 持久化形态。
func routeRuleToGateway(r *extension.RouteRule) *model.GatewayRoute {
	if r == nil {
		return nil
	}
	return &model.GatewayRoute{
		ID:              r.ID,
		TenantID:        r.TenantID,
		Name:            r.Name,
		PathPrefix:      r.PathPrefix,
		TargetBackend:   r.TargetBackend,
		Methods:         r.Methods,
		RateLimitPerSec: r.RateLimitPerSec,
		TimeoutMs:       r.TimeoutMs,
		Enabled:         r.Enabled,
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
	}
}

// gatewayToRouteRule 持久化形态 → 域形态。
func gatewayToRouteRule(g *model.GatewayRoute) *extension.RouteRule {
	if g == nil {
		return nil
	}
	return &extension.RouteRule{
		ID:              g.ID,
		TenantID:        g.TenantID,
		Name:            g.Name,
		PathPrefix:      g.PathPrefix,
		TargetBackend:   g.TargetBackend,
		Methods:         g.Methods,
		RateLimitPerSec: g.RateLimitPerSec,
		TimeoutMs:       g.TimeoutMs,
		Enabled:         g.Enabled,
		CreatedAt:       g.CreatedAt,
		UpdatedAt:       g.UpdatedAt,
	}
}

// loadTenantRoutesLocked 从 store 加载指定租户的全部路由到进程内缓存（幂等）。
//
// 调用方**必须持有 gw.mu 写锁**。
// 幂等：已加载（gw.loaded[tenantID]）的租户直接返回，不重复打 store。
// **单一构造路径**：条目一律经 buildGatewayRouteEntries/newGatewayRouteEntry 构造，
// 与刷新、创建、更新路径共用同一工厂（熔断器/限流器不会漏接）。
func (s *Server) loadTenantRoutesLocked(gw *gatewayState, tenantID string) error {
	if gw.loaded[tenantID] {
		return nil
	}
	rules := s.store.ListGatewayRoutes(tenantID)
	gw.routes[tenantID] = buildGatewayRouteEntries(gw.routes[tenantID], rules)
	gw.sigs[tenantID] = gatewayRoutesSignature(rules)
	gw.loaded[tenantID] = true
	return nil
}

// gatewayRoutesSignature 计算一批持久化路由的指纹（含 UpdatedAt/内容）。
//
// 用途：周期刷新时判断 store 侧是否有变化——指纹相同即省掉整轮条目重建，
// 保住限流器令牌与熔断器状态；指纹不同则重建。store 的 Save 会刷新
// UpdatedAt（memory/sqlstore 皆然），故"另一副本改过"必然改变指纹。
func gatewayRoutesSignature(rules []*model.GatewayRoute) uint64 {
	h := fnv.New64a()
	writeString := func(v string) {
		_, _ = h.Write([]byte(v))
		_, _ = h.Write([]byte{0})
	}
	// 用十进制文本编码整数：指纹只求确定性与无截断，位模式序列化会被
	// gosec G115（int64→uint64、uint64→byte）判为高危，文本编码无此问题。
	writeInt := func(v int64) {
		_, _ = h.Write([]byte(strconv.FormatInt(v, 10)))
		_, _ = h.Write([]byte{0})
	}
	for _, r := range rules {
		if r == nil {
			continue
		}
		writeString(r.ID)
		writeInt(r.UpdatedAt.UnixNano())
		writeString(r.Name)
		writeString(r.PathPrefix)
		writeString(r.TargetBackend)
		for _, m := range r.Methods {
			writeString(m)
		}
		writeInt(int64(r.RateLimitPerSec))
		writeInt(int64(r.TimeoutMs))
		if r.Enabled {
			writeInt(1)
		} else {
			writeInt(0)
		}
	}
	return h.Sum64()
}

// buildGatewayRouteEntries 由持久化规则构造租户条目表。
//
// prev 中内容未变的规则**沿用原 limiter/breaker 实例**（只换 rule 指针刷新时间戳），
// 避免每 5s 刷新一次就把熔断计数与令牌桶清零；内容变了才用工厂重建。
func buildGatewayRouteEntries(prev map[string]*gatewayRouteEntry, rules []*model.GatewayRoute) map[string]*gatewayRouteEntry {
	entries := make(map[string]*gatewayRouteEntry, len(rules))
	for _, g := range rules {
		rule := gatewayToRouteRule(g)
		if rule == nil {
			continue
		}
		old := prev[rule.ID]
		if old != nil && sameGatewayRouteContent(old.rule, rule) {
			entries[rule.ID] = &gatewayRouteEntry{rule: rule, limiter: old.limiter, breaker: old.breaker}
			continue
		}
		entries[rule.ID] = newGatewayRouteEntry(rule)
	}
	return entries
}

// sameGatewayRouteContent 比较两条规则的可变业务字段（不含时间戳）。
// 仅 UpdatedAt 变化（重复保存同样内容）不重建，保留运行期状态。
func sameGatewayRouteContent(a, b *extension.RouteRule) bool {
	if a == nil || b == nil {
		return false
	}
	if a.ID != b.ID || a.TenantID != b.TenantID || a.Name != b.Name ||
		a.PathPrefix != b.PathPrefix || a.TargetBackend != b.TargetBackend ||
		a.RateLimitPerSec != b.RateLimitPerSec || a.TimeoutMs != b.TimeoutMs ||
		a.Enabled != b.Enabled {
		return false
	}
	if len(a.Methods) != len(b.Methods) {
		return false
	}
	for i := range a.Methods {
		if a.Methods[i] != b.Methods[i] {
			return false
		}
	}
	return true
}

// startGatewayRouteRefresh 启动多副本路由缓存的周期刷新（单 goroutine）。
//
// 生命周期：ctx 由 server_lifecycle.go Start 的 signal.NotifyContext 提供，
// 取消即退出；ticker 随 defer 释放。同一时刻只有这一条 goroutine 做刷新
// （天然合并，不会叠加读取），且只扫已加载租户、指纹未变即跳过。
func (s *Server) startGatewayRouteRefresh(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = gatewayRouteRefreshInterval
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.refreshGatewayRoutes(ctx)
			}
		}
	}()
}

// refreshGatewayRoutes 从 store 回读所有已加载租户的路由并替换缓存（幂等、可跳过）。
//
// 并发语义：
//   - 读 store 在 gw.mu 之外进行（SQL 后端可能慢，不阻塞请求路径）；
//   - 应用快照前校验 gw.revision 未变——期间有本地写则本轮快照可能过期，直接放弃，
//     下一周期重试（本地写自己已经把缓存更新了）；
//   - 只替换 map 指针，不原地改 rule/entry：请求路径在锁外持有的 rule 指针保持不可变，
//     并发读不会撞上写（race-free）。
func (s *Server) refreshGatewayRoutes(ctx context.Context) {
	gw := s.ensureGateway()
	gw.mu.RLock()
	tenants := make([]string, 0, len(gw.loaded))
	for tenant := range gw.loaded {
		tenants = append(tenants, tenant)
	}
	revision := gw.revision
	gw.mu.RUnlock()
	sort.Strings(tenants)

	for _, tenant := range tenants {
		if ctx.Err() != nil {
			return
		}
		rules := s.store.ListGatewayRoutes(tenant)
		sig := gatewayRoutesSignature(rules)

		gw.mu.Lock()
		if !gw.loaded[tenant] || gw.revision != revision {
			gw.mu.Unlock()
			return
		}
		if gw.sigs[tenant] != sig {
			gw.routes[tenant] = buildGatewayRouteEntries(gw.routes[tenant], rules)
			gw.sigs[tenant] = sig
		}
		gw.mu.Unlock()
	}
}
