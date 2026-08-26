// sql_apikey.go 实现 SQLStore 的 APIKeyStore 子接口（Phase 6 API Key 管理）。
//
// 【未实现的桩】本文件全部方法未接入 MySQL 持久化，经 stub_guard.StubNotImplemented
// 显式告警（每 (domain,method) 首次必打、60s 限频），并按统一约定返回零值：
//   - CreateAPIKey 返回 nil（不返回填充后的假对象——杜绝「201 假成功 → GET 404」链路；
//     SQL 后端下 ListAPIKeys 恒为空切片，认证扫描将恒 401，符合「桩显式失效」总原则）；
//   - Get/Update/Delete 返回 (nil, false) / false；List 返回非 nil 空切片。
package store

// CreateAPIKey 创建 API Key（未实现的桩）。
func (s *SQLStore) CreateAPIKey(tenantID string, key *APIKey) *APIKey {
	StubNotImplemented("apikey", "CreateAPIKey")
	return nil
}

// GetAPIKey 按 (tenantID, id) 返回单个 API Key（未实现的桩）。
func (s *SQLStore) GetAPIKey(tenantID, id string) (*APIKey, bool) {
	StubNotImplemented("apikey", "GetAPIKey")
	return nil, false
}

// UpdateAPIKey 更新 API Key（未实现的桩）。
func (s *SQLStore) UpdateAPIKey(tenantID string, key *APIKey) (*APIKey, bool) {
	StubNotImplemented("apikey", "UpdateAPIKey")
	return nil, false
}

// ListAPIKeys 返回指定租户的全部 API Key（未实现的桩；返回非 nil 空切片防上层 range panic）。
func (s *SQLStore) ListAPIKeys(tenantID string) []*APIKey {
	StubNotImplemented("apikey", "ListAPIKeys")
	return []*APIKey{}
}

// DeleteAPIKey 删除 API Key（未实现的桩）。
func (s *SQLStore) DeleteAPIKey(tenantID, id string) bool {
	StubNotImplemented("apikey", "DeleteAPIKey")
	return false
}
