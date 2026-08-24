
// memory_ticket.go 实现 MemoryStore 的 TicketStore 子接口（Phase 1 工单管理）。
//
// 工单内存实现：
//   - tickets 字段在 MemoryStore struct 中定义（map[string]*Ticket）；
//   - NewMemoryStore 中初始化为空 map；
//   - 本文件实现 5 个方法，全部经 m.mu 互斥保护，并发安全。
//
// 设计要点（与 memory_k8s.go 风格一致）：
//   - ListTickets 返回深拷贝避免外部修改破坏内部状态；
//   - CreateTicket 分配随机 ID（"ticket-" + 16 字节 hex）；
//   - ListTickets 按 filter 过滤 + 按创建时间降序（最新优先）；
//   - CloseTicket 设置 Status="closed" + ResolvedAt=now。
package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// randTicketID 生成随机工单 ID（"ticket-" + 16 字节 hex，crypto/rand 密码学安全）。
// 用于 CreateTicket 分配 ID（调用方未填 ID 时）。
func randTicketID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// 熵源失败回退时间戳（降级但可容忍，唯一性由 tickets map key 兜底）。
		return fmt.Sprintf("ticket-%d", time.Now().UnixNano())
	}
	return "ticket-" + hex.EncodeToString(b)
}

// cloneTicket 返回 t 的深拷贝（含 Tags / ResolvedAt 指针）。
// 用于 GetTicket / ListTickets / CreateTicket / UpdateTicket / CloseTicket 返回，
// 避免外部修改破坏内部状态。
func cloneTicket(t *Ticket) *Ticket {
	if t == nil {
		return nil
	}
	cp := *t
	if t.Tags != nil {
		cp.Tags = append([]string(nil), t.Tags...)
	}
	if t.ResolvedAt != nil {
		rt := *t.ResolvedAt
		cp.ResolvedAt = &rt
	}
	return &cp
}

// CreateTicket 创建工单（按 ID 幂等；ID 为空时分配随机 ID）。
//
// 行为：
//   - ID 为空时分配随机 ID（新建场景）；
//   - TenantID 为空时归一为 default（与 K8s 集群一致）；
//   - CreatedAt 为空时填当前时间（新建场景）；
//   - UpdatedAt 始终刷新为当前时间；
//   - Status 为空时默认 "open"；
//   - Priority 为空时默认 "medium"；
//   - Category 为空时默认 "incident"。
func (m *MemoryStore) CreateTicket(tenantID string, t *Ticket) *Ticket {
	if t == nil {
		return nil
	}
	// 租户隔离：空租户归一为 default。
	if tenantID == "" {
		tenantID = "default"
	}
	t.TenantID = tenantID
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if t.ID == "" {
		t.ID = randTicketID()
	}
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
	m.tickets[t.ID] = t
	return cloneTicket(t)
}

// GetTicket 按 (tenantID, id) 返回单个工单（深拷贝；不存在或租户不匹配返回 (nil, false)）。
func (m *MemoryStore) GetTicket(tenantID, id string) (*Ticket, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tickets[id]
	if !ok {
		return nil, false
	}
	// 租户隔离：tenantID 非空时校验归属。
	if tenantID != "" && t.TenantID != tenantID {
		return nil, false
	}
	return cloneTicket(t), true
}

// UpdateTicket 更新工单（按 t.ID 定位，校验 tenantID 归属）。
//
// 行为：
//   - 不存在或租户不匹配返回 (nil, false)；
//   - CreatedAt / TenantID 不可改（保留原值，防越权改归属）；
//   - UpdatedAt 始终刷新为当前时间；
//   - 返回更新后的工单（深拷贝）。
func (m *MemoryStore) UpdateTicket(tenantID string, t *Ticket) (*Ticket, bool) {
	if t == nil || t.ID == "" {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.tickets[t.ID]
	if !ok {
		return nil, false
	}
	// 租户隔离：tenantID 非空时校验归属。
	if tenantID != "" && existing.TenantID != tenantID {
		return nil, false
	}
	// 保留不可改字段。
	t.ID = existing.ID
	t.TenantID = existing.TenantID
	t.CreatedAt = existing.CreatedAt
	t.UpdatedAt = time.Now()
	m.tickets[t.ID] = t
	return cloneTicket(t), true
}

// ListTickets 返回指定租户的工单列表（按 filter 过滤 + 按创建时间降序）。
//
// filter 字段为空串时表示不过滤该字段。返回深拷贝避免外部修改。
func (m *MemoryStore) ListTickets(tenantID string, filter TicketFilter) []*Ticket {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Ticket, 0, len(m.tickets))
	for _, t := range m.tickets {
		// 租户隔离：tenantID 非空时仅返回同租户工单。
		if tenantID != "" && t.TenantID != tenantID {
			continue
		}
		// filter 过滤。
		if filter.Status != "" && t.Status != filter.Status {
			continue
		}
		if filter.Priority != "" && t.Priority != filter.Priority {
			continue
		}
		if filter.Category != "" && t.Category != filter.Category {
			continue
		}
		if filter.AssigneeID != "" && t.AssigneeID != filter.AssigneeID {
			continue
		}
		out = append(out, cloneTicket(t))
	}
	// 按创建时间降序（最新优先，插入排序，与 ListK8sClusters 风格一致）。
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j].CreatedAt.After(out[j-1].CreatedAt) {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}

// CloseTicket 关闭工单：置 Status="closed" + ResolvedAt=now。
// 不存在或租户不匹配返回 (nil, false)。返回更新后的工单（深拷贝）。
func (m *MemoryStore) CloseTicket(tenantID, id string) (*Ticket, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.tickets[id]
	if !ok {
		return nil, false
	}
	// 租户隔离：tenantID 非空时校验归属。
	if tenantID != "" && existing.TenantID != tenantID {
		return nil, false
	}
	now := time.Now()
	existing.Status = "closed"
	existing.ResolvedAt = &now
	existing.UpdatedAt = now
	return cloneTicket(existing), true
}