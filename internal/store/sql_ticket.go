// sql_ticket.go 实现 SQLStore 的 TicketStore 子接口（Phase 1 工单管理，桩实现）。
//
// TODO(p1): 接入 MySQL 持久化（tickets 表：id PK + tenant_id + title + description +
// status + priority + category + assignee_id + creator_id + related_device +
// related_task + tags JSON + created_at + updated_at + resolved_at）。
// MVP 用内存桩实现，保证接口齐全 + go build 通过；DB 不可用时返回零值，
// 不 panic，与 SQLStore 其他桩方法风格一致（参考 sql_discovery.go）。
//
// 与 MemoryStore 实现逻辑等价，仅锁类型不同（SQLStore.mu 为 sync.Mutex）。
package store

import "time"

// CreateTicket 创建工单（桩实现）。
// TODO(p1): 落库 tickets 表（INSERT ... ON DUPLICATE KEY UPDATE）。
// MVP：DB 不可用时返回 t（不持久化），保证接口齐全。
func (s *SQLStore) CreateTicket(tenantID string, t *Ticket) *Ticket {
	if t == nil {
		return nil
	}
	// 租户隔离：空租户归一为 default（与 MemoryStore 一致）。
	if tenantID == "" {
		tenantID = "default"
	}
	t.TenantID = tenantID
	if t.ID == "" {
		t.ID = randTicketID()
	}
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	if t.Status == "" {
		t.Status = "open"
	}
	if t.Priority == "" {
		t.Priority = "medium"
	}
	if t.Category == "" {
		t.Category = "incident"
	}
	t.UpdatedAt = now
	return t
}

// GetTicket 按 (tenantID, id) 返回单个工单（桩实现）。
// TODO(p1): SELECT * FROM tickets WHERE id=? AND tenant_id=?。
// MVP：DB 不可用时返回 (nil, false)。
func (s *SQLStore) GetTicket(tenantID, id string) (*Ticket, bool) {
	return nil, false
}

// UpdateTicket 更新工单（桩实现）。
// TODO(p1): UPDATE tickets SET ... WHERE id=? AND tenant_id=?。
// MVP：DB 不可用时返回 (nil, false)。
func (s *SQLStore) UpdateTicket(tenantID string, t *Ticket) (*Ticket, bool) {
	return nil, false
}

// ListTickets 返回指定租户的工单列表（桩实现）。
// TODO(p1): SELECT * FROM tickets WHERE tenant_id=? [AND status=? AND priority=?
// AND category=? AND assignee_id=?] ORDER BY created_at DESC。
// MVP：DB 不可用时返回空 slice（非 nil，便于调用方 range）。
func (s *SQLStore) ListTickets(tenantID string, filter TicketFilter) []*Ticket {
	return []*Ticket{}
}

// CloseTicket 关闭工单（桩实现）。
// TODO(p1): UPDATE tickets SET status='closed', resolved_at=NOW(), updated_at=NOW()
// WHERE id=? AND tenant_id=?。
// MVP：DB 不可用时返回 (nil, false)。
func (s *SQLStore) CloseTicket(tenantID, id string) (*Ticket, bool) {
	return nil, false
}
