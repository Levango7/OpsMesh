// multi_schema_abac_decisions.go MultiSchemaStore 对 ABACDecisionStore 的实现（M3）。
//
// 路由原则同 ABACPolicy：决策日志自带 TenantID（控制面认证后回填），按该值路由到
// 对应租户 schema。批量追加按租户分组，逐 schema 提交（跨 schema 无分布式事务；
// 端点每批来自单个 agent，实际只会命中一个 schema）。跨租户查询遍历全部 schema
// 合并后按 received_at 倒序截断；保留期清理遍历全部 schema 累加删除数。
package multischema

import (
	"sort"
	"time"
)

// AppendABACDecisionLogs 按 TenantID 分组路由后批量写入，返回成功写入条数。
// 某租户写入失败时返回此前已成功的条数与错误，调用方按未完全持久化处理。
func (m *MultiSchemaStore) AppendABACDecisionLogs(logs []ABACDecisionLog) (int, error) {
	if len(logs) == 0 {
		return 0, nil
	}
	grouped := make(map[string][]ABACDecisionLog)
	order := make([]string, 0, 1)
	for _, l := range logs {
		if l.TenantID == "" {
			return 0, errEmptyTenant
		}
		if _, ok := grouped[l.TenantID]; !ok {
			order = append(order, l.TenantID)
		}
		grouped[l.TenantID] = append(grouped[l.TenantID], l)
	}
	accepted := 0
	for _, tenantID := range order {
		s, err := m.storeFor(tenantID)
		if err != nil {
			return accepted, err
		}
		n, err := s.AppendABACDecisionLogs(grouped[tenantID])
		accepted += n
		if err != nil {
			return accepted, err
		}
	}
	return accepted, nil
}

// ListABACDecisionLogs 返回决策日志：tenantID 非空时只查对应租户 schema。
func (m *MultiSchemaStore) ListABACDecisionLogs(tenantID string, limit int) []ABACDecisionLog {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	if tenantID != "" {
		s, err := m.storeFor(tenantID)
		if err != nil {
			return nil
		}
		return s.ListABACDecisionLogs(tenantID, limit)
	}
	out := make([]ABACDecisionLog, 0)
	for _, s := range m.allStores() {
		out = append(out, s.ListABACDecisionLogs("", limit)...)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].ReceivedAt.Equal(out[j].ReceivedAt) {
			return out[i].ReceivedAt.After(out[j].ReceivedAt)
		}
		if out[i].TenantID != out[j].TenantID {
			return out[i].TenantID < out[j].TenantID
		}
		return out[i].ID > out[j].ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// DeleteABACDecisionLogsBefore 在每个已创建的租户 schema 上执行保留期清理，
// 返回累计删除条数；任一 schema 失败时返回已删除条数与首个错误。
func (m *MultiSchemaStore) DeleteABACDecisionLogsBefore(cutoff time.Time) (int, error) {
	total := 0
	var firstErr error
	for _, s := range m.allStores() {
		n, err := s.DeleteABACDecisionLogsBefore(cutoff)
		total += n
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return total, firstErr
}
