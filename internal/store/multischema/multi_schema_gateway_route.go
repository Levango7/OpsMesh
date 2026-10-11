// multi_schema_gateway_route.go MultiSchemaStore 对 GatewayRouteStore 的实现（TD-66 P1）。
//
// 委派模式与 multi_schema_p03.go 的服务发现完全一致：路由规则是**租户级**资源，
// 用规则自带的 TenantID 路由到该租户的后端实现，不引入二次语义。
// ============================================================================

package multischema

// ListGatewayRoutes 列出指定租户的路由规则（按租户路由到对应后端）。
func (m *MultiSchemaStore) ListGatewayRoutes(tenantID string) []*GatewayRoute {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.ListGatewayRoutes(tenantID)
}

// GetGatewayRoute 按租户 + ID 取单条。
func (m *MultiSchemaStore) GetGatewayRoute(tenantID, id string) *GatewayRoute {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.GetGatewayRoute(tenantID, id)
}

// SaveGatewayRoute 幂等 upsert（用规则的 TenantID 路由）。
func (m *MultiSchemaStore) SaveGatewayRoute(rule *GatewayRoute) error {
	if rule == nil {
		return nil
	}
	if rule.TenantID == "" {
		rule.TenantID = "default"
	}
	s, err := m.storeFor(rule.TenantID)
	if err != nil {
		return err
	}
	return s.SaveGatewayRoute(rule)
}

// DeleteGatewayRoute 按租户 + ID 删除（用租户路由）。
func (m *MultiSchemaStore) DeleteGatewayRoute(tenantID, id string) bool {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return false
	}
	return s.DeleteGatewayRoute(tenantID, id)
}
