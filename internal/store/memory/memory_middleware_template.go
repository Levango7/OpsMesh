// memory_middleware_template.go — 中间件部署模板的 memory 持久化。
package memory

import (
	"time"

	"github.com/Levango7/OpsMesh/internal/store/model"
)

// SaveMiddlewareTemplate 创建或更新中间件部署模板（按 ID 幂等）。
// ID 为空时分配随机 ID；TenantID 为空时归一为 default；
// CreatedAt 为空时填当前时间；UpdatedAt 始终刷新。
func (m *MemoryStore) SaveMiddlewareTemplate(t *MiddlewareTemplate) error {
	if t == nil {
		return nil
	}
	if t.TenantID == "" {
		t.TenantID = "default"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if t.ID == "" {
		t.ID = model.RandMiddlewareTemplateID()
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	// 深拷贝存储，避免调用方继续修改 t 影响 store 内部状态
	// （MiddlewareTemplate 字段均为值类型，浅拷贝即深拷贝）。
	stored := *t
	m.middlewareTemplates[t.ID] = &stored
	return nil
}

// ListMiddlewareTemplates 返回中间件部署模板（按创建时间升序；深拷贝）；tenantID 非空时按租户过滤。
func (m *MemoryStore) ListMiddlewareTemplates(tenantID string) []*MiddlewareTemplate {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*MiddlewareTemplate, 0, len(m.middlewareTemplates))
	for _, t := range m.middlewareTemplates {
		if tenantID != "" && t.TenantID != tenantID {
			continue
		}
		cp := *t
		out = append(out, &cp)
	}
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j].CreatedAt.Before(out[j-1].CreatedAt) {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}

// GetMiddlewareTemplate 按 ID 返回单个中间件部署模板（深拷贝；不存在返回 nil）。
func (m *MemoryStore) GetMiddlewareTemplate(id string) *MiddlewareTemplate {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.middlewareTemplates[id]
	if !ok {
		return nil
	}
	cp := *t
	return &cp
}

// DeleteMiddlewareTemplate 删除中间件部署模板，返回是否删除成功（不存在返回 false）。
func (m *MemoryStore) DeleteMiddlewareTemplate(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.middlewareTemplates[id]; !ok {
		return false
	}
	delete(m.middlewareTemplates, id)
	return true
}
