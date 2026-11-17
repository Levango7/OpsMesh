// memory_abac_decision_logs.go MemoryStore 的 ABACDecisionStore 实现（M3）。
//
// 容量语义（内存后端必须自证有界，否则离线 agent 补传会打爆控制面内存）：
//   - 单租户上限 ABACDecisionLogsPerTenantCap（10000 条）；
//   - 全局上限 ABACDecisionLogsGlobalCap（50000 条）。
//
// 超出时丢弃**最旧**的记录（按追加顺序），与保留期清理互补：容量上限防突发洪峰，
// DeleteABACDecisionLogsBefore 防长期堆积。
//
// ID 为追加序号（不从 0 重排），删除/淘汰后仍保持单调，便于排序与排障。
package memory

import (
	"sort"
	"time"
)

const (
	// ABACDecisionLogsPerTenantCap 单租户最多保留的决策条数。
	ABACDecisionLogsPerTenantCap = 10000
	// ABACDecisionLogsGlobalCap 内存后端全局最多保留的决策条数。
	ABACDecisionLogsGlobalCap = 50000
	// ABACDecisionLogsListCap 单次查询最多返回条数。
	ABACDecisionLogsListCap = 1000
)

// AppendABACDecisionLogs 批量追加决策日志。内存后端无 I/O，恒返回 (len(logs), nil)。
// ReceivedAt 为零时填当前 UTC；OccurredAt 为零时回填 ReceivedAt（handler 已校验，幂等兜底）。
func (m *MemoryStore) AppendABACDecisionLogs(logs []ABACDecisionLog) (int, error) {
	if len(logs) == 0 {
		return 0, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	for _, l := range logs {
		l.ID = m.abacDecisionSeq
		m.abacDecisionSeq++
		if l.ReceivedAt.IsZero() {
			l.ReceivedAt = now
		} else {
			l.ReceivedAt = l.ReceivedAt.UTC()
		}
		if l.OccurredAt.IsZero() {
			l.OccurredAt = l.ReceivedAt
		} else {
			l.OccurredAt = l.OccurredAt.UTC()
		}
		m.abacDecisionLogs = append(m.abacDecisionLogs, l)
	}
	m.trimABACDecisionLogsLocked()
	return len(logs), nil
}

// ListABACDecisionLogs 返回决策日志：tenantID 为空返回全部租户。
// 按 ReceivedAt 倒序、ID 倒序（最新在前）；limit<=0 或超过 1000 时按 1000 处理。
func (m *MemoryStore) ListABACDecisionLogs(tenantID string, limit int) []ABACDecisionLog {
	if limit <= 0 || limit > ABACDecisionLogsListCap {
		limit = ABACDecisionLogsListCap
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ABACDecisionLog, 0, limit)
	for _, l := range m.abacDecisionLogs {
		if tenantID != "" && l.TenantID != tenantID {
			continue
		}
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].ReceivedAt.Equal(out[j].ReceivedAt) {
			return out[i].ReceivedAt.After(out[j].ReceivedAt)
		}
		return out[i].ID > out[j].ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// DeleteABACDecisionLogsBefore 删除 ReceivedAt 早于 cutoff 的记录，返回删除条数。
func (m *MemoryStore) DeleteABACDecisionLogsBefore(cutoff time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.abacDecisionLogs) == 0 {
		return 0, nil
	}
	kept := make([]ABACDecisionLog, 0, len(m.abacDecisionLogs))
	deleted := 0
	for _, l := range m.abacDecisionLogs {
		if l.ReceivedAt.Before(cutoff) {
			deleted++
			continue
		}
		kept = append(kept, l)
	}
	m.abacDecisionLogs = kept
	return deleted, nil
}

// trimABACDecisionLogsLocked 执行容量淘汰：先按租户上限丢最旧，再按全局上限丢最旧。
// 追加顺序即时间顺序，因此顺序遍历删除前缀即可保留最新的 per-tenant 窗口。
func (m *MemoryStore) trimABACDecisionLogsLocked() {
	if len(m.abacDecisionLogs) == 0 {
		return
	}
	counts := make(map[string]int, len(m.abacDecisionLogs))
	for _, l := range m.abacDecisionLogs {
		counts[l.TenantID]++
	}
	overPerTenant := make(map[string]int, len(counts))
	for tenant, n := range counts {
		if n > ABACDecisionLogsPerTenantCap {
			overPerTenant[tenant] = n - ABACDecisionLogsPerTenantCap
		}
	}
	if len(overPerTenant) == 0 && len(m.abacDecisionLogs) <= ABACDecisionLogsGlobalCap {
		return
	}
	kept := make([]ABACDecisionLog, 0, len(m.abacDecisionLogs))
	for _, l := range m.abacDecisionLogs {
		if overPerTenant[l.TenantID] > 0 {
			overPerTenant[l.TenantID]--
			continue
		}
		kept = append(kept, l)
	}
	if len(kept) > ABACDecisionLogsGlobalCap {
		kept = append([]ABACDecisionLog(nil), kept[len(kept)-ABACDecisionLogsGlobalCap:]...)
	}
	m.abacDecisionLogs = kept
}
