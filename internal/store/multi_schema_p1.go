// multi_schema_p1.go MultiSchemaStore 对 Phase 1 两个新接口（TicketStore / SLOStore）的委托实现。
//
// MultiSchemaStore 按 tenantID 路由到 per-tenant Store（SQLStore 或测试 mock），
// 各方法委托给底层 store。新接口方法签名与 MemoryStore/SQLStore 一致，
// 仅在路由层做租户隔离分发。
//
// 设计要点（与 multi_schema_p03.go 风格一致）：
//   - 带 tenantID 参数的方法直接用 storeFor(tenantID) 路由。
//   - CreateTicket(tenantID, t) 用参数 tenantID 路由（空串归一为 default）。
//   - CreateSLO(tenantID, slo) 用参数 tenantID 路由（空串归一为 default）。
//   - 路由失败返回零值（nil/false），与现有方法风格一致。
package store

// ============================================================================
// TicketStore 实现（5 方法）
// ============================================================================

// CreateTicket 创建工单：用参数 tenantID 路由（空串归一为 default）。
func (m *MultiSchemaStore) CreateTicket(tenantID string, t *Ticket) *Ticket {
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
	return s.CreateTicket(tenantID, t)
}

// GetTicket 按 (tenantID, id) 返回单个工单：用 tenantID 路由。
func (m *MultiSchemaStore) GetTicket(tenantID, id string) (*Ticket, bool) {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.GetTicket(tenantID, id)
}

// UpdateTicket 更新工单：用 tenantID 路由。
func (m *MultiSchemaStore) UpdateTicket(tenantID string, t *Ticket) (*Ticket, bool) {
	if t == nil {
		return nil, false
	}
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.UpdateTicket(tenantID, t)
}

// ListTickets 返回指定租户的工单列表：用 tenantID 路由。
func (m *MultiSchemaStore) ListTickets(tenantID string, filter TicketFilter) []*Ticket {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.ListTickets(tenantID, filter)
}

// CloseTicket 关闭工单：用 tenantID 路由。
func (m *MultiSchemaStore) CloseTicket(tenantID, id string) (*Ticket, bool) {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.CloseTicket(tenantID, id)
}

// ============================================================================
// SLOStore 实现（6 方法）
// ============================================================================

// CreateSLO 创建 SLO：用参数 tenantID 路由（空串归一为 default）。
func (m *MultiSchemaStore) CreateSLO(tenantID string, slo *SLO) *SLO {
	if slo == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.CreateSLO(tenantID, slo)
}

// GetSLO 按 (tenantID, id) 返回单个 SLO：用 tenantID 路由。
func (m *MultiSchemaStore) GetSLO(tenantID, id string) (*SLO, bool) {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.GetSLO(tenantID, id)
}

// UpdateSLO 更新 SLO：用 tenantID 路由。
func (m *MultiSchemaStore) UpdateSLO(tenantID string, slo *SLO) (*SLO, bool) {
	if slo == nil {
		return nil, false
	}
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.UpdateSLO(tenantID, slo)
}

// ListSLOs 返回指定租户的全部 SLO：用 tenantID 路由。
func (m *MultiSchemaStore) ListSLOs(tenantID string) []*SLO {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.ListSLOs(tenantID)
}

// DeleteSLO 删除 SLO：用 tenantID 路由。
func (m *MultiSchemaStore) DeleteSLO(tenantID, id string) bool {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return false
	}
	return s.DeleteSLO(tenantID, id)
}

// SLIStatus 返回指定 SLO 下各 SLI 的当前状态：用 tenantID 路由。
func (m *MultiSchemaStore) SLIStatus(tenantID, id string) []*SLIStatus {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.SLIStatus(tenantID, id)
}
