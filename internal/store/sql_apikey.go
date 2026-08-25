
// sql_apikey.go 实现 SQLStore 的 APIKeyStore 子接口（Phase 6，桩实现）。
package store

import "time"

// CreateAPIKey 创建 API Key（桩实现）。
func (s *SQLStore) CreateAPIKey(tenantID string, key *APIKey) *APIKey {
	if key == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	key.TenantID = tenantID
	if key.ID == "" {
		key.ID = randAPIKeyID()
	}
	if key.CreatedAt.IsZero() {
		key.CreatedAt = time.Now().UTC()
	}
	return key
}

// GetAPIKey 按 (tenantID, id) 返回单个 API Key（桩实现）。
func (s *SQLStore) GetAPIKey(tenantID, id string) (*APIKey, bool) {
	return nil, false
}

// UpdateAPIKey 更新 API Key（桩实现）。
func (s *SQLStore) UpdateAPIKey(tenantID string, key *APIKey) (*APIKey, bool) {
	return nil, false
}

// ListAPIKeys 返回指定租户的全部 API Key（桩实现）。
func (s *SQLStore) ListAPIKeys(tenantID string) []*APIKey {
	return []*APIKey{}
}

// DeleteAPIKey 删除 API Key（桩实现）。
func (s *SQLStore) DeleteAPIKey(tenantID, id string) bool {
	return false
}