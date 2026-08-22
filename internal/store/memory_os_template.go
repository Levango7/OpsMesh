// memory_os_template.go — OS 优化模板的 memory 持久化。
package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// randOSTemplateID 生成随机 OS 安装模板 ID（16 字节十六进制，crypto/rand 密码学安全）。
func randOSTemplateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("os-tmpl-%d", time.Now().UnixNano())
	}
	return "os-tmpl-" + hex.EncodeToString(b)
}

// SaveOSTemplate 创建或更新 OS 安装模板（按 ID 幂等）。
// ID 为空时分配随机 ID；TenantID 为空时归一为 default；
// CreatedAt 为空时填当前时间；UpdatedAt 始终刷新。
func (m *MemoryStore) SaveOSTemplate(t *OSTemplate) error {
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
		t.ID = randOSTemplateID()
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	// 深拷贝存储，避免调用方继续修改 t 影响 store 内部状态
	// （OSTemplate 字段均为值类型，浅拷贝即深拷贝）。
	stored := *t
	m.osTemplates[t.ID] = &stored
	return nil
}

// ListOSTemplates 返回 OS 安装模板（按创建时间升序；深拷贝）；tenantID 非空时按租户过滤。
func (m *MemoryStore) ListOSTemplates(tenantID string) []*OSTemplate {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*OSTemplate, 0, len(m.osTemplates))
	for _, t := range m.osTemplates {
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

// GetOSTemplate 按 ID 返回单个 OS 安装模板（深拷贝；不存在返回 nil）。
func (m *MemoryStore) GetOSTemplate(id string) *OSTemplate {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.osTemplates[id]
	if !ok {
		return nil
	}
	cp := *t
	return &cp
}

// DeleteOSTemplate 删除 OS 安装模板，返回是否删除成功（不存在返回 false）。
func (m *MemoryStore) DeleteOSTemplate(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.osTemplates[id]; !ok {
		return false
	}
	delete(m.osTemplates, id)
	return true
}

// ============================================================================
// 中间件部署模板：SaveMiddlewareTemplate / ListMiddlewareTemplates / GetMiddlewareTemplate / DeleteMiddlewareTemplate
// ============================================================================

// randMiddlewareTemplateID 生成随机中间件部署模板 ID（16 字节十六进制，crypto/rand 密码学安全）。
