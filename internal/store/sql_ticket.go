// sql_ticket.go 实现 SQLStore 的 TicketStore 子接口（Phase 1 工单管理）。
//
// 【未实现的桩】本文件全部方法未接入 MySQL 持久化，经 stub_guard.StubNotImplemented
// 显式告警（每 (domain,method) 首次必打、60s 限频），并按统一约定返回零值：
//   - CreateTicket 返回 nil（不返回填充后的假对象——杜绝「201 假成功 → GET 404」链路）；
//   - Get/Update/Close 返回 (nil, false)；List 返回非 nil 空切片。
//
// TODO(p1): 接入 MySQL 持久化（tickets 表：id PK + tenant_id + title + description +
// status + priority + category + assignee_id + creator_id + related_device +
// related_task + tags JSON + created_at + updated_at + resolved_at）。
package store

// CreateTicket 创建工单（未实现的桩）。
// TODO(p1): 落库 tickets 表（INSERT ... ON DUPLICATE KEY UPDATE）。
func (s *SQLStore) CreateTicket(tenantID string, t *Ticket) *Ticket {
	StubNotImplemented("ticket", "CreateTicket")

	return nil
}

// GetTicket 按 (tenantID, id) 返回单个工单（未实现的桩）。
// TODO(p1): SELECT * FROM tickets WHERE id=? AND tenant_id=?。
func (s *SQLStore) GetTicket(tenantID, id string) (*Ticket, bool) {
	StubNotImplemented("ticket", "GetTicket")
	return nil, false
}

// UpdateTicket 更新工单（未实现的桩）。
// TODO(p1): UPDATE tickets SET ... WHERE id=? AND tenant_id=?。
func (s *SQLStore) UpdateTicket(tenantID string, t *Ticket) (*Ticket, bool) {
	StubNotImplemented("ticket", "UpdateTicket")
	return nil, false
}

// ListTickets 返回指定租户的工单列表（未实现的桩；返回非 nil 空切片防上层 range panic）。
// TODO(p1): SELECT * FROM tickets WHERE tenant_id=? [AND status=? AND priority=?
// AND category=? AND assignee_id=?] ORDER BY created_at DESC。
func (s *SQLStore) ListTickets(tenantID string, filter TicketFilter) []*Ticket {
	StubNotImplemented("ticket", "ListTickets")
	return []*Ticket{}
}

// CloseTicket 关闭工单（未实现的桩）。
// TODO(p1): UPDATE tickets SET status='closed', resolved_at=NOW(), updated_at=NOW()
// WHERE id=? AND tenant_id=?。
func (s *SQLStore) CloseTicket(tenantID, id string) (*Ticket, bool) {
	StubNotImplemented("ticket", "CloseTicket")
	return nil, false
}
