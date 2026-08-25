
package store

// multi_schema_p5.go MultiSchemaStore 对 Phase 5 两个新接口（WebhookStore / ScriptStore）的委托实现。
//
// MultiSchemaStore 按 tenantID 路由到 per-tenant Store（SQLStore 或测试 mock），
// 各方法委托给底层 store。新接口方法签名与 MemoryStore/SQLStore 一致，
// 仅在路由层做租户隔离分发。
//
// 设计要点（与 multi_schema_p4.go 风格一致）：
//   - 带 tenantID 参数的方法直接用 storeFor(tenantID) 路由。
//   - 路由失败返回零值（nil/false），与现有方法风格一致。

// ============================================================================
// WebhookStore 实现（6 方法）
// ============================================================================

// CreateWebhook 创建 Webhook：用参数 tenantID 路由（空串归一为 default）。
func (m *MultiSchemaStore) CreateWebhook(tenantID string, wh *Webhook) *Webhook {
	if wh == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.CreateWebhook(tenantID, wh)
}

// GetWebhook 按 (tenantID, id) 返回单个 Webhook：用 tenantID 路由。
func (m *MultiSchemaStore) GetWebhook(tenantID, id string) (*Webhook, bool) {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.GetWebhook(tenantID, id)
}

// UpdateWebhook 更新 Webhook：用 tenantID 路由。
func (m *MultiSchemaStore) UpdateWebhook(tenantID string, wh *Webhook) (*Webhook, bool) {
	if wh == nil {
		return nil, false
	}
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.UpdateWebhook(tenantID, wh)
}

// ListWebhooks 返回指定租户的全部 Webhook：用 tenantID 路由。
func (m *MultiSchemaStore) ListWebhooks(tenantID string) []*Webhook {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.ListWebhooks(tenantID)
}

// DeleteWebhook 删除 Webhook：用 tenantID 路由。
func (m *MultiSchemaStore) DeleteWebhook(tenantID, id string) bool {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return false
	}
	return s.DeleteWebhook(tenantID, id)
}

// ListWebhookDeliveries 返回指定 Webhook 的投递记录：用 tenantID 路由。
func (m *MultiSchemaStore) ListWebhookDeliveries(tenantID, webhookID string) []*WebhookDelivery {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.ListWebhookDeliveries(tenantID, webhookID)
}

// ============================================================================
// ScriptStore 实现（6 方法）
// ============================================================================

// CreateScript 创建脚本：用参数 tenantID 路由（空串归一为 default）。
func (m *MultiSchemaStore) CreateScript(tenantID string, sc *Script) *Script {
	if sc == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.CreateScript(tenantID, sc)
}

// GetScript 按 (tenantID, id) 返回单个脚本：用 tenantID 路由。
func (m *MultiSchemaStore) GetScript(tenantID, id string) (*Script, bool) {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.GetScript(tenantID, id)
}

// UpdateScript 更新脚本：用 tenantID 路由。
func (m *MultiSchemaStore) UpdateScript(tenantID string, sc *Script) (*Script, bool) {
	if sc == nil {
		return nil, false
	}
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.UpdateScript(tenantID, sc)
}

// ListScripts 返回指定租户的全部脚本：用 tenantID 路由。
func (m *MultiSchemaStore) ListScripts(tenantID string) []*Script {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.ListScripts(tenantID)
}

// DeleteScript 删除脚本：用 tenantID 路由。
func (m *MultiSchemaStore) DeleteScript(tenantID, id string) bool {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return false
	}
	return s.DeleteScript(tenantID, id)
}

// ListScriptExecutions 返回指定脚本的执行记录：用 tenantID 路由。
func (m *MultiSchemaStore) ListScriptExecutions(tenantID, scriptID string) []*ScriptExecution {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.ListScriptExecutions(tenantID, scriptID)
}