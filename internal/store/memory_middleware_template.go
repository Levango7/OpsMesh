// memory_middleware_template.go — 中间件部署模板的 memory 持久化。
package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"opsmesh/internal/proto"
)

func randMiddlewareTemplateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("mw-tmpl-%d", time.Now().UnixNano())
	}
	return "mw-tmpl-" + hex.EncodeToString(b)
}

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
		t.ID = randMiddlewareTemplateID()
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

// ============================================================================
// 设备监控指标环形缓冲：metricsRing
// ============================================================================

// metricsRingDefaultCap 环形缓冲默认容量：2h * 120 samples/h（30s 采样间隔）= 240 条。
// 每条 DeviceMetrics 约 1KB，总 ~240KB/设备。
const metricsRingDefaultCap = 240

// metricsRing 设备监控指标环形缓冲：保留最近 N 条历史快照。
// 用 slice + head index 实现，O(1) 追加 O(n) 读取。
// 自身无线程安全，由外层 MemoryStore.mu（或 SQLStore.mu）统一保护并发。
type metricsRing struct {
	samples  []proto.DeviceMetrics // 环形缓冲 slice（固定容量，覆写最旧）
	head     int                   // 下一个写入位置（0..capacity-1）
	size     int                   // 当前已写入数量（<= capacity）
	capacity int                   // 缓冲容量
}

// newMetricsRing 创建环形缓冲。capacity<=0 时用 metricsRingDefaultCap（240）。
