package store

// memory_traffic.go 实现 MemoryStore 的 TrafficStore 子接口（Phase 2 流量治理）。
//
// 流量治理内存实现：
//   - trafficPolicies 字段在 MemoryStore struct 中定义（map[string]*TrafficPolicy）；
//   - NewMemoryStore 中初始化为空 map；
//   - 本文件实现 7 个方法，全部经 m.mu 互斥保护，并发安全。
//
// 设计要点（与 memory_ticket.go 风格一致）：
//   - ListPolicies 返回深拷贝避免外部修改破坏内部状态；
//   - CreatePolicy 分配随机 ID（"traffic-" + 16 字节 hex）；
//   - EnablePolicy/DisablePolicy 切换 Status 字段。

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// randTrafficID 生成随机流量策略 ID（"traffic-" + 16 字节 hex）。
func randTrafficID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("traffic-%d", time.Now().UnixNano())
	}
	return "traffic-" + hex.EncodeToString(b)
}

// cloneTrafficPolicy 返回 p 的深拷贝。
func cloneTrafficPolicy(p *TrafficPolicy) *TrafficPolicy {
	if p == nil {
		return nil
	}
	cp := *p
	if p.CanaryWeights != nil {
		cp.CanaryWeights = make(map[string]int, len(p.CanaryWeights))
		for k, v := range p.CanaryWeights {
			cp.CanaryWeights[k] = v
		}
	}
	return &cp
}

// CreatePolicy 创建流量策略（ID 为空时分配随机 ID）。
func (m *MemoryStore) CreatePolicy(tenantID string, p *TrafficPolicy) *TrafficPolicy {
	if p == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	p.TenantID = tenantID
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if p.ID == "" {
		p.ID = randTrafficID()
	}
	if p.Status == "" {
		p.Status = "inactive"
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	m.trafficPolicies[p.ID] = p
	return cloneTrafficPolicy(p)
}

// GetPolicy 按 (tenantID, id) 返回单个策略（深拷贝）。
func (m *MemoryStore) GetPolicy(tenantID, id string) (*TrafficPolicy, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.trafficPolicies[id]
	if !ok {
		return nil, false
	}
	if tenantID != "" && p.TenantID != tenantID {
		return nil, false
	}
	return cloneTrafficPolicy(p), true
}

// UpdatePolicy 更新策略（按 p.ID 定位，校验 tenantID 归属）。
func (m *MemoryStore) UpdatePolicy(tenantID string, p *TrafficPolicy) (*TrafficPolicy, bool) {
	if p == nil || p.ID == "" {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.trafficPolicies[p.ID]
	if !ok {
		return nil, false
	}
	if tenantID != "" && existing.TenantID != tenantID {
		return nil, false
	}
	p.TenantID = existing.TenantID
	p.CreatedAt = existing.CreatedAt
	p.UpdatedAt = time.Now()
	m.trafficPolicies[p.ID] = p
	return cloneTrafficPolicy(p), true
}

// ListPolicies 返回指定租户的全部策略（深拷贝，按创建时间降序）。
func (m *MemoryStore) ListPolicies(tenantID string) []*TrafficPolicy {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*TrafficPolicy, 0)
	for _, p := range m.trafficPolicies {
		if tenantID != "" && p.TenantID != tenantID {
			continue
		}
		out = append(out, cloneTrafficPolicy(p))
	}
	// 按创建时间降序
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j].CreatedAt.After(out[j-1].CreatedAt) {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}

// DeletePolicy 删除策略。
func (m *MemoryStore) DeletePolicy(tenantID, id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.trafficPolicies[id]
	if !ok {
		return false
	}
	if tenantID != "" && p.TenantID != tenantID {
		return false
	}
	delete(m.trafficPolicies, id)
	return true
}

// EnablePolicy 启用策略：置 Status="active"。
func (m *MemoryStore) EnablePolicy(tenantID, id string) (*TrafficPolicy, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.trafficPolicies[id]
	if !ok {
		return nil, false
	}
	if tenantID != "" && p.TenantID != tenantID {
		return nil, false
	}
	p.Status = "active"
	p.UpdatedAt = time.Now()
	return cloneTrafficPolicy(p), true
}

// DisablePolicy 禁用策略：置 Status="inactive"。
func (m *MemoryStore) DisablePolicy(tenantID, id string) (*TrafficPolicy, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.trafficPolicies[id]
	if !ok {
		return nil, false
	}
	if tenantID != "" && p.TenantID != tenantID {
		return nil, false
	}
	p.Status = "inactive"
	p.UpdatedAt = time.Now()
	return cloneTrafficPolicy(p), true
}
