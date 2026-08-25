
// memory_apikey.go 实现 MemoryStore 的 APIKeyStore 子接口（Phase 6 API Key 管理）。
package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// randAPIKeyID 生成随机 API Key ID（"apikey-" + 16 字节 hex）。
func randAPIKeyID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("apikey-%d", time.Now().UnixNano())
	}
	return "apikey-" + hex.EncodeToString(b)
}

// cloneAPIKey 返回 k 的深拷贝（含 Scopes）。
func cloneAPIKey(k *APIKey) *APIKey {
	if k == nil {
		return nil
	}
	cp := *k
	if k.Scopes != nil {
		cp.Scopes = append([]string(nil), k.Scopes...)
	}
	return &cp
}

// CreateAPIKey 创建 API Key（按 ID 幂等；ID 为空时分配随机 ID）。
func (m *MemoryStore) CreateAPIKey(tenantID string, key *APIKey) *APIKey {
	if key == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	key.TenantID = tenantID
	m.mu.Lock()
	defer m.mu.Unlock()
	if key.ID == "" {
		key.ID = randAPIKeyID()
	}
	if key.CreatedAt.IsZero() {
		key.CreatedAt = time.Now()
	}
	m.apiKeys[key.ID] = key
	return cloneAPIKey(key)
}

// GetAPIKey 按 (tenantID, id) 返回单个 API Key（深拷贝；不存在或租户不匹配返回 (nil, false)）。
func (m *MemoryStore) GetAPIKey(tenantID, id string) (*APIKey, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	k, ok := m.apiKeys[id]
	if !ok {
		return nil, false
	}
	if tenantID != "" && k.TenantID != tenantID {
		return nil, false
	}
	return cloneAPIKey(k), true
}

// UpdateAPIKey 更新 API Key（按 key.ID 定位，校验 tenantID 归属）。
func (m *MemoryStore) UpdateAPIKey(tenantID string, key *APIKey) (*APIKey, bool) {
	if key == nil || key.ID == "" {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.apiKeys[key.ID]
	if !ok {
		return nil, false
	}
	if tenantID != "" && existing.TenantID != tenantID {
		return nil, false
	}
	key.ID = existing.ID
	key.TenantID = existing.TenantID
	key.CreatedAt = existing.CreatedAt
	m.apiKeys[key.ID] = key
	return cloneAPIKey(key), true
}

// ListAPIKeys 返回指定租户的全部 API Key（按创建时间降序）。
// tenantID 为空串时返回全部租户的 API Key。
func (m *MemoryStore) ListAPIKeys(tenantID string) []*APIKey {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*APIKey, 0, len(m.apiKeys))
	for _, k := range m.apiKeys {
		if tenantID != "" && k.TenantID != tenantID {
			continue
		}
		out = append(out, cloneAPIKey(k))
	}
	// 按创建时间降序。
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j].CreatedAt.After(out[j-1].CreatedAt) {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}

// DeleteAPIKey 删除 API Key，返回是否删除成功。
func (m *MemoryStore) DeleteAPIKey(tenantID, id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.apiKeys[id]
	if !ok {
		return false
	}
	if tenantID != "" && k.TenantID != tenantID {
		return false
	}
	delete(m.apiKeys, id)
	return true
}