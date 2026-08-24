
// sql_slo.go 实现 SQLStore 的 SLOStore 子接口（Phase 1 SLO 管理，桩实现）。
//
// TODO(p1): 接入 MySQL 持久化（slos 表：id PK + tenant_id + name + description +
// service_name + target + window + slis JSON + created_at + updated_at）。
// MVP 用内存桩实现，保证接口齐全 + go build 通过；DB 不可用时返回零值，
// 不 panic，与 SQLStore 其他桩方法风格一致（参考 sql_discovery.go）。
//
// 与 MemoryStore 实现逻辑等价，仅锁类型不同（SQLStore.mu 为 sync.Mutex）。
package store

import "time"

// CreateSLO 创建 SLO（桩实现）。
// TODO(p1): 落库 slos 表（INSERT ... ON DUPLICATE KEY UPDATE）。
// MVP：DB 不可用时返回 slo（不持久化），保证接口齐全。
func (s *SQLStore) CreateSLO(tenantID string, slo *SLO) *SLO {
	if slo == nil {
		return nil
	}
	// 租户隔离：空租户归一为 default（与 MemoryStore 一致）。
	if tenantID == "" {
		tenantID = "default"
	}
	slo.TenantID = tenantID
	if slo.ID == "" {
		slo.ID = randSLOID()
	}
	now := time.Now().UTC()
	if slo.CreatedAt.IsZero() {
		slo.CreatedAt = now
	}
	slo.UpdatedAt = now
	return slo
}

// GetSLO 按 (tenantID, id) 返回单个 SLO（桩实现）。
// TODO(p1): SELECT * FROM slos WHERE id=? AND tenant_id=?。
// MVP：DB 不可用时返回 (nil, false)。
func (s *SQLStore) GetSLO(tenantID, id string) (*SLO, bool) {
	return nil, false
}

// UpdateSLO 更新 SLO（桩实现）。
// TODO(p1): UPDATE slos SET ... WHERE id=? AND tenant_id=?。
// MVP：DB 不可用时返回 (nil, false)。
func (s *SQLStore) UpdateSLO(tenantID string, slo *SLO) (*SLO, bool) {
	return nil, false
}

// ListSLOs 返回指定租户的全部 SLO（桩实现）。
// TODO(p1): SELECT * FROM slos WHERE tenant_id=? ORDER BY created_at ASC。
// MVP：DB 不可用时返回空 slice（非 nil，便于调用方 range）。
func (s *SQLStore) ListSLOs(tenantID string) []*SLO {
	return []*SLO{}
}

// DeleteSLO 删除 SLO（桩实现）。
// TODO(p1): DELETE FROM slos WHERE id=? AND tenant_id=?。
// MVP：DB 不可用时返回 false。
func (s *SQLStore) DeleteSLO(tenantID, id string) bool {
	return false
}

// SLIStatus 返回指定 SLO 下各 SLI 的当前状态（桩实现）。
// TODO(p1): 接入 Prometheus 真实评估，按 SLI.Metric 查询当前值并比对 Target。
// MVP：DB 不可用时返回 nil。
func (s *SQLStore) SLIStatus(tenantID, id string) []*SLIStatus {
	return nil
}