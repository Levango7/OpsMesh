// gateway_persistence.go TD-66 P1：网关路由的 store 持久化接线。
//
// 设计：**store 是事实源，gatewayState 是按租户的缓存**。
//   - 写路径（create/update/delete/toggle）先落 store、成功后再更新缓存；store 失败返回 5xx
//     而不是假成功（否则用户以为配好了，重启后发现丢了）。
//   - 读路径（list 与网关匹配）按需从 store 加载：租户的缓存未建立时先 load 再匹配。
//   - 缓存失效：任何写操作后该租户缓存即最新，不需要显式清理。
//
// 转换：extension.RouteRule（域形态）↔ model.GatewayRoute（持久化形态）。
// 两者字段一一对应；转换薄薄一层，换来 store 不依赖 extension 的分层干净。
package controlplane

import (
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
// 调用方**必须持有 gw.mu 写锁**（handler 内均在 Lock 后调用；handleListGatewayRoutes
// 是唯一的例外——它内联了同样的逻辑，因为需要在同一把锁内读缓存）。
// 幂等：已加载（gw.loaded[tenantID]）的租户直接返回，不重复打 store。
func (s *Server) loadTenantRoutesLocked(gw *gatewayState, tenantID string) error {
	if gw.loaded[tenantID] {
		return nil
	}
	rules := s.store.ListGatewayRoutes(tenantID)
	if gw.routes[tenantID] == nil {
		gw.routes[tenantID] = make(map[string]*gatewayRouteEntry, len(rules))
	}
	for _, g := range rules {
		rule := gatewayToRouteRule(g)
		gw.routes[tenantID][rule.ID] = &gatewayRouteEntry{
			rule:    rule,
			limiter: extension.NewRateLimiter(rule.RateLimitPerSec),
		}
	}
	gw.loaded[tenantID] = true
	return nil
}
