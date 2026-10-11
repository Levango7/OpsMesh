// memory_gateway_route.go MemoryStore 对 GatewayRouteStore 接口的实现（TD-66 P1）。
//
// 与 discovery 等同域的模式一致：复用 MemoryStore.mu 保护并发；按 (tenantID, ID) 双键索引
// （外层 map 是为了 ListGatewayRoutes 的租户隔离——与 gateway_routes 表的 tenant_id 隔离同语义）。
//
// memory 后端下路由本就是进程内的，此实现是"照 SQL 语义把同样的契约补齐"：
// 好处是控制面不需要区分后端读同一套 store 方法；坏处（重启仍丢）是 memory 的既有语义，可接受
// （存储层文档已声明 memory 不保证持久化）。
package memory

import (
	"sort"
	"time"

	"github.com/Levango7/OpsMesh/internal/store/model"
)

// SaveGatewayRoute 幂等 upsert（按 tenantID+ID）。新建填 CreatedAt，已存在保留 CreatedAt、刷新 UpdatedAt。
func (m *MemoryStore) SaveGatewayRoute(rule *model.GatewayRoute) error {
	if rule == nil || rule.TenantID == "" || rule.ID == "" {
		return nil // 无主键可持久化即静默忽略（上游 handler 已校验，这里兜底不 panic）
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if m.gatewayRoutes == nil {
		m.gatewayRoutes = make(map[string]map[string]*model.GatewayRoute)
	}
	if m.gatewayRoutes[rule.TenantID] == nil {
		m.gatewayRoutes[rule.TenantID] = make(map[string]*model.GatewayRoute)
	}
	cp := model.CloneGatewayRoute(rule)
	if existing, ok := m.gatewayRoutes[rule.TenantID][rule.ID]; ok {
		cp.CreatedAt = existing.CreatedAt // upsert 保留创建时间
	}
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = now
	}
	cp.UpdatedAt = now
	m.gatewayRoutes[rule.TenantID][rule.ID] = cp
	return nil
}

// ListGatewayRoutes 返回该租户全部路由（ID 升序，深拷贝）。
func (m *MemoryStore) ListGatewayRoutes(tenantID string) []*model.GatewayRoute {
	m.mu.RLock()
	defer m.mu.RUnlock()
	byID := m.gatewayRoutes[tenantID]
	if len(byID) == 0 {
		return nil
	}
	out := make([]*model.GatewayRoute, 0, len(byID))
	for _, r := range byID {
		out = append(out, model.CloneGatewayRoute(r))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// GetGatewayRoute 按租户 + ID 取单条（不存在返回 nil，深拷贝）。
func (m *MemoryStore) GetGatewayRoute(tenantID, id string) *model.GatewayRoute {
	m.mu.RLock()
	defer m.mu.RUnlock()
	byID := m.gatewayRoutes[tenantID]
	if len(byID) == 0 {
		return nil
	}
	return model.CloneGatewayRoute(byID[id])
}

// DeleteGatewayRoute 按租户 + ID 删除。返回是否删除成功。
func (m *MemoryStore) DeleteGatewayRoute(tenantID, id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	byID := m.gatewayRoutes[tenantID]
	if len(byID) == 0 {
		return false
	}
	if _, ok := byID[id]; !ok {
		return false
	}
	delete(byID, id)
	return true
}
