// gateway_route.go — API 网关路由规则的持久化模型（TD-66 P1）。
//
// 为什么 store 层自持类型而不直接复用 `internal/extension.RouteRule`：
// store 是各服务模块的共享层，`internal/extension` 是控制面自己的扩展机制，两者不应互相依赖。
// 转换薄薄一层（To/From 各一个函数），换来分层的干净——domain 形态归 extension，persistence 形态归 store。
package model

import "time"

// GatewayRoute API 网关路由规则（持久化形态）。
// 与 extension.RouteRule 字段一一对应；转换见 model.RouteRuleToGateway / model.GatewayToRouteRule。
type GatewayRoute struct {
	ID              string    `json:"id"`
	TenantID        string    `json:"tenantID"`
	Name            string    `json:"name"`
	PathPrefix      string    `json:"pathPrefix"`
	TargetBackend   string    `json:"targetBackend"`
	Methods         []string  `json:"methods"`
	RateLimitPerSec int       `json:"rateLimitPerSec"`
	TimeoutMs       int       `json:"timeoutMs"` // TD-66 P4：单路由转发超时（0=用默认 30s）
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// CloneGatewayRoute 深拷贝（读路径对外一律给副本，防外部修改内部状态）。
func CloneGatewayRoute(r *GatewayRoute) *GatewayRoute {
	if r == nil {
		return nil
	}
	cp := *r
	if r.Methods != nil {
		cp.Methods = append([]string(nil), r.Methods...)
	}
	return &cp
}

// CloneGatewayRoutes 批量深拷贝。
func CloneGatewayRoutes(rs []*GatewayRoute) []*GatewayRoute {
	if len(rs) == 0 {
		return nil
	}
	out := make([]*GatewayRoute, len(rs))
	for i, r := range rs {
		out[i] = CloneGatewayRoute(r)
	}
	return out
}
