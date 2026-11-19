// memory_abac_policy.go MemoryStore 的 ABACPolicyStore 实现（批次47）。
//
// 结构与 GatewayRoute 持久化同构：按 (tenantID, id) 两段键分区，租户 map 隔离保证
// A 租户的策略查询/删除不会命中 B 租户；version 在每次 upsert 时自增，
// CreatedAt 首次写入后不再变化（与 SQL 实现的 ON DUPLICATE KEY 语义一致）。
package memory

import (
	"sort"
	"time"
)

// ListABACPolicyRecords 返回策略记录：tenantID 为空返回全部租户。
// 确定性排序：tenant_id 升序，再按 id 升序。
func (m *MemoryStore) ListABACPolicyRecords(tenantID string) []ABACPolicyRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ABACPolicyRecord, 0)
	appendTenant := func(tenant string) {
		policies := m.abacPolicies[tenant]
		ids := make([]string, 0, len(policies))
		for id := range policies {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			out = append(out, policies[id])
		}
	}
	if tenantID != "" {
		appendTenant(tenantID)
		return out
	}
	tenants := make([]string, 0, len(m.abacPolicies))
	for tenant := range m.abacPolicies {
		tenants = append(tenants, tenant)
	}
	sort.Strings(tenants)
	for _, tenant := range tenants {
		appendTenant(tenant)
	}
	return out
}

// UpsertABACPolicyRecord 按 (TenantID, ID) 幂等写入：已存在则 version+1 并保留 CreatedAt，
// 不存在则 version=1。返回落库后的记录（含真实版本与时间戳）与是否成功。
func (m *MemoryStore) UpsertABACPolicyRecord(p ABACPolicyRecord) (ABACPolicyRecord, bool) {
	if p.ID == "" {
		return p, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.abacPolicies == nil {
		m.abacPolicies = make(map[string]map[string]ABACPolicyRecord)
	}
	policies := m.abacPolicies[p.TenantID]
	if policies == nil {
		policies = make(map[string]ABACPolicyRecord)
		m.abacPolicies[p.TenantID] = policies
	}
	now := time.Now().UTC()
	if prev, ok := policies[p.ID]; ok {
		p.Version = prev.Version + 1
		p.CreatedAt = prev.CreatedAt
	} else {
		p.Version = 1
		if p.CreatedAt.IsZero() {
			p.CreatedAt = now
		}
	}
	p.UpdatedAt = now
	policies[p.ID] = p
	return p, true
}

// DeleteABACPolicyRecord 按 (tenantID, id) 删除。不存在返回 false。
func (m *MemoryStore) DeleteABACPolicyRecord(tenantID, id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	policies, ok := m.abacPolicies[tenantID]
	if !ok {
		return false
	}
	if _, ok := policies[id]; !ok {
		return false
	}
	delete(policies, id)
	if len(policies) == 0 {
		delete(m.abacPolicies, tenantID)
	}
	return true
}
