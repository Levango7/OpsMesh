package store

// multi_schema_p2.go MultiSchemaStore 对 Phase 2 三个新接口（TrafficStore / PipelineStore / ArgoCDStore）的委托实现。
//
// MultiSchemaStore 按 tenantID 路由到 per-tenant Store（SQLStore 或测试 mock），
// 各方法委托给底层 store。新接口方法签名与 MemoryStore/SQLStore 一致，
// 仅在路由层做租户隔离分发。
//
// 设计要点（与 multi_schema_p1.go 风格一致）：
//   - 带 tenantID 参数的方法直接用 storeFor(tenantID) 路由。
//   - 路由失败返回零值（nil/false），与现有方法风格一致。

// ============================================================================
// TrafficStore 实现（7 方法）
// ============================================================================

// CreatePolicy 创建流量策略：用参数 tenantID 路由（空串归一为 default）。
func (m *MultiSchemaStore) CreatePolicy(tenantID string, p *TrafficPolicy) *TrafficPolicy {
	if p == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.CreatePolicy(tenantID, p)
}

// GetPolicy 按 (tenantID, id) 返回单个策略：用 tenantID 路由。
func (m *MultiSchemaStore) GetPolicy(tenantID, id string) (*TrafficPolicy, bool) {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.GetPolicy(tenantID, id)
}

// UpdatePolicy 更新策略：用 tenantID 路由。
func (m *MultiSchemaStore) UpdatePolicy(tenantID string, p *TrafficPolicy) (*TrafficPolicy, bool) {
	if p == nil {
		return nil, false
	}
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.UpdatePolicy(tenantID, p)
}

// ListPolicies 返回指定租户的全部策略：用 tenantID 路由。
func (m *MultiSchemaStore) ListPolicies(tenantID string) []*TrafficPolicy {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.ListPolicies(tenantID)
}

// DeletePolicy 删除策略：用 tenantID 路由。
func (m *MultiSchemaStore) DeletePolicy(tenantID, id string) bool {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return false
	}
	return s.DeletePolicy(tenantID, id)
}

// EnablePolicy 启用策略：用 tenantID 路由。
func (m *MultiSchemaStore) EnablePolicy(tenantID, id string) (*TrafficPolicy, bool) {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.EnablePolicy(tenantID, id)
}

// DisablePolicy 禁用策略：用 tenantID 路由。
func (m *MultiSchemaStore) DisablePolicy(tenantID, id string) (*TrafficPolicy, bool) {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.DisablePolicy(tenantID, id)
}

// ============================================================================
// PipelineStore 实现（8 方法）
// ============================================================================

// CreateTemplate 创建流水线模板：用参数 tenantID 路由（空串归一为 default）。
func (m *MultiSchemaStore) CreateTemplate(tenantID string, t *PipelineTemplate) *PipelineTemplate {
	if t == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.CreateTemplate(tenantID, t)
}

// GetTemplate 按 (tenantID, id) 返回单个模板：用 tenantID 路由。
func (m *MultiSchemaStore) GetTemplate(tenantID, id string) (*PipelineTemplate, bool) {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.GetTemplate(tenantID, id)
}

// ListTemplates 返回指定租户的全部模板：用 tenantID 路由。
func (m *MultiSchemaStore) ListTemplates(tenantID string) []*PipelineTemplate {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.ListTemplates(tenantID)
}

// DeleteTemplate 删除模板：用 tenantID 路由。
func (m *MultiSchemaStore) DeleteTemplate(tenantID, id string) bool {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return false
	}
	return s.DeleteTemplate(tenantID, id)
}

// CreateRun 创建运行记录：用参数 tenantID 路由（空串归一为 default）。
func (m *MultiSchemaStore) CreateRun(tenantID string, r *PipelineRun) *PipelineRun {
	if r == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.CreateRun(tenantID, r)
}

// GetRun 按 (tenantID, id) 返回单条运行记录：用 tenantID 路由。
func (m *MultiSchemaStore) GetRun(tenantID, id string) (*PipelineRun, bool) {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.GetRun(tenantID, id)
}

// ListRuns 返回运行记录列表：用 tenantID 路由。
func (m *MultiSchemaStore) ListRuns(tenantID string, templateID string) []*PipelineRun {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.ListRuns(tenantID, templateID)
}

// UpdateRun 更新运行记录：用 tenantID 路由。
func (m *MultiSchemaStore) UpdateRun(tenantID string, r *PipelineRun) (*PipelineRun, bool) {
	if r == nil {
		return nil, false
	}
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.UpdateRun(tenantID, r)
}

// ============================================================================
// ArgoCDStore 实现（6 方法）
// ============================================================================

// CreateApp 创建 ArgoCD 应用：用参数 tenantID 路由（空串归一为 default）。
func (m *MultiSchemaStore) CreateApp(tenantID string, a *ArgoCDApp) *ArgoCDApp {
	if a == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.CreateApp(tenantID, a)
}

// GetApp 按 (tenantID, id) 返回单个应用：用 tenantID 路由。
func (m *MultiSchemaStore) GetApp(tenantID, id string) (*ArgoCDApp, bool) {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.GetApp(tenantID, id)
}

// UpdateApp 更新应用：用 tenantID 路由。
func (m *MultiSchemaStore) UpdateApp(tenantID string, a *ArgoCDApp) (*ArgoCDApp, bool) {
	if a == nil {
		return nil, false
	}
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.UpdateApp(tenantID, a)
}

// ListApps 返回指定租户的全部应用：用 tenantID 路由。
func (m *MultiSchemaStore) ListApps(tenantID string) []*ArgoCDApp {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.ListApps(tenantID)
}

// DeleteApp 删除应用：用 tenantID 路由。
func (m *MultiSchemaStore) DeleteApp(tenantID, id string) bool {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return false
	}
	return s.DeleteApp(tenantID, id)
}

// SyncApp 触发同步：用 tenantID 路由。
func (m *MultiSchemaStore) SyncApp(tenantID, id string) (*ArgoCDApp, bool) {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.SyncApp(tenantID, id)
}
