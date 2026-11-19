// multi_schema_abac_policy.go MultiSchemaStore 对 ABACPolicyStore 的实现（批次47）。
//
// 委派模式同 GatewayRoute：策略是租户级资源，按记录自带 TenantID 路由到该租户的 schema。
// 跨租户视图（tenantID==""）遍历已创建的 store 合并结果，并按 (tenant_id, id) 排序，
// 与控制面启动装载/刷新比对的确定性口径一致。
// ============================================================================

package multischema

import "sort"

// ListABACPolicyRecords 列出策略记录：tenantID 非空时只查对应租户 schema。
func (m *MultiSchemaStore) ListABACPolicyRecords(tenantID string) []ABACPolicyRecord {
	if tenantID != "" {
		s, err := m.storeFor(tenantID)
		if err != nil {
			return nil
		}
		return s.ListABACPolicyRecords(tenantID)
	}
	var out []ABACPolicyRecord
	for _, s := range m.allStores() {
		out = append(out, s.ListABACPolicyRecords("")...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TenantID != out[j].TenantID {
			return out[i].TenantID < out[j].TenantID
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// UpsertABACPolicyRecord 按记录 TenantID 路由后写穿。
func (m *MultiSchemaStore) UpsertABACPolicyRecord(p ABACPolicyRecord) (ABACPolicyRecord, bool) {
	if p.TenantID == "" {
		return p, false
	}
	s, err := m.storeFor(p.TenantID)
	if err != nil {
		return p, false
	}
	return s.UpsertABACPolicyRecord(p)
}

// DeleteABACPolicyRecord 按租户路由后删除。
func (m *MultiSchemaStore) DeleteABACPolicyRecord(tenantID, id string) bool {
	if tenantID == "" {
		return false
	}
	s, err := m.storeFor(tenantID)
	if err != nil {
		return false
	}
	return s.DeleteABACPolicyRecord(tenantID, id)
}
