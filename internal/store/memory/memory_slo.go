// memory_slo.go 实现 MemoryStore 的 SLOStore 子接口（Phase 1 SLO 管理）。
//
// SLO 内存实现：
//   - slos 字段在 MemoryStore struct 中定义（map[string]*SLO）；
//   - NewMemoryStore 中初始化为空 map；
//   - 本文件实现 6 个方法，全部经 m.mu 互斥保护，并发安全。
//
// 设计要点（与 memory_ticket.go 风格一致）：
//   - ListSLOs 返回深拷贝避免外部修改破坏内部状态；
//   - CreateSLO 分配随机 ID（"slo-" + 16 字节 hex）；
//   - SLIStatus 按 networkMetricsHistory 真实聚合（无样本 ⇒ nodata），
//     指标支持集见 slo_eval.go。
package memory

import (
	"time"

	"github.com/Levango7/OpsMesh/internal/store/model"
)

// CreateSLO 创建 SLO（按 ID 幂等；ID 为空时分配随机 ID）。
//
// 行为：
//   - ID 为空时分配随机 ID（新建场景）；
//   - TenantID 为空时归一为 default（与 K8s 集群一致）；
//   - CreatedAt 为空时填当前时间（新建场景）；
//   - UpdatedAt 始终刷新为当前时间。
func (m *MemoryStore) CreateSLO(tenantID string, slo *SLO) *SLO {
	if slo == nil {
		return nil
	}
	// 租户隔离：空租户归一为 default。
	if tenantID == "" {
		tenantID = "default"
	}
	slo.TenantID = tenantID
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if slo.ID == "" {
		slo.ID = model.RandSLOID()
	}
	if slo.CreatedAt.IsZero() {
		slo.CreatedAt = now
	}
	slo.UpdatedAt = now
	m.slos[slo.ID] = slo
	return model.CloneSLO(slo)
}

// GetSLO 按 (tenantID, id) 返回单个 SLO（深拷贝；不存在或租户不匹配返回 (nil, false)）。
func (m *MemoryStore) GetSLO(tenantID, id string) (*SLO, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	slo, ok := m.slos[id]
	if !ok {
		return nil, false
	}
	// 租户隔离：tenantID 非空时校验归属。
	if tenantID != "" && slo.TenantID != tenantID {
		return nil, false
	}
	return model.CloneSLO(slo), true
}

// UpdateSLO 更新 SLO（按 slo.ID 定位，校验 tenantID 归属）。
//
// 行为：
//   - 不存在或租户不匹配返回 (nil, false)；
//   - CreatedAt / TenantID 不可改（保留原值，防越权改归属）；
//   - UpdatedAt 始终刷新为当前时间；
//   - 返回更新后的 SLO（深拷贝）。
func (m *MemoryStore) UpdateSLO(tenantID string, slo *SLO) (*SLO, bool) {
	if slo == nil || slo.ID == "" {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.slos[slo.ID]
	if !ok {
		return nil, false
	}
	// 租户隔离：tenantID 非空时校验归属。
	if tenantID != "" && existing.TenantID != tenantID {
		return nil, false
	}
	// 保留不可改字段。
	slo.ID = existing.ID
	slo.TenantID = existing.TenantID
	slo.CreatedAt = existing.CreatedAt
	slo.UpdatedAt = time.Now()
	m.slos[slo.ID] = slo
	return model.CloneSLO(slo), true
}

// ListSLOs 返回指定租户的全部 SLO（按创建时间升序；深拷贝）。
func (m *MemoryStore) ListSLOs(tenantID string) []*SLO {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*SLO, 0, len(m.slos))
	for _, slo := range m.slos {
		// 租户隔离：tenantID 非空时仅返回同租户 SLO。
		if tenantID != "" && slo.TenantID != tenantID {
			continue
		}
		out = append(out, model.CloneSLO(slo))
	}
	// 按创建时间升序（与 ListK8sClusters 风格一致）。
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j].CreatedAt.Before(out[j-1].CreatedAt) {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}

// DeleteSLO 删除 SLO，返回是否删除成功（不存在或租户不匹配返回 false）。
func (m *MemoryStore) DeleteSLO(tenantID, id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	slo, ok := m.slos[id]
	if !ok {
		return false
	}
	// 租户隔离：tenantID 非空时校验归属。
	if tenantID != "" && slo.TenantID != tenantID {
		return false
	}
	delete(m.slos, id)
	return true
}

// SLIStatus 返回指定 SLO 下各 SLI 的当前状态。
//
// 2026-10-03 修掉的假数据面：这里原先硬编码 `CurrentValue: 99.5` + `Status: "met"`
// （注释自认"MVP 模拟值 / 假定满足"），而 `/api/v1/slos/{id}/status` 是对外承诺的 SLA 口径
// ⇒ 内存后端下任何 SLO 都恒报达标，客户据此做的 SLA 复盘是编造出来的。
// 现在按真实聚合走：有样本就算均值、没样本就 nodata，判定与 SQL 后端共用 evaluateSLI。
func (m *MemoryStore) SLIStatus(tenantID, id string) []*SLIStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	slo, ok := m.slos[id]
	if !ok {
		return nil
	}
	// 租户隔离：tenantID 非空时校验归属。
	if tenantID != "" && slo.TenantID != tenantID {
		return nil
	}
	now := time.Now()
	const window = 5 * time.Minute
	since := now.Add(-window)
	out := make([]*SLIStatus, 0, len(slo.SLIs))
	for _, sli := range slo.SLIs {
		// -1 = 无数据；不支持的指标同样落到 nodata，绝不编值。
		current := m.avgSLIMetric(tenantID, sli.Metric, since)
		out = append(out, &SLIStatus{
			SLIName:       sli.Name,
			CurrentValue:  current,
			TargetValue:   sli.Target,
			Status:        model.EvaluateSLI(current, sli.Target, sli.Operator),
			LastEvaluated: now,
		})
	}
	return out
}

// avgSLIMetric 求租户在 since 之后所有网络指标里该字段的均值；无样本或不支持返回 -1。
//
// 过滤口径与 QueryNetworkMetrics 完全一致（设备登记的 tenantID + 样本自带的 tenantID
// 都要匹配），但不沿用它的"无数据返回 0"行为——那正是本次要消除的形态。
// uptime 在内存后端没有聚合来源，故落 -1（nodata）而不是猜一个值。
func (m *MemoryStore) avgSLIMetric(tenantID, metric string, since time.Time) float64 {
	field := model.MetricFieldFor(metric)
	if field != "cpu_usage" && field != "memory_usage" && field != "temperature" {
		return -1
	}
	var sum float64
	var count int
	for deviceID, hist := range m.networkMetricsHistory {
		if d, ok := m.networkDevices[deviceID]; ok && d.TenantID != "" && tenantID != "" && d.TenantID != tenantID {
			continue
		}
		for _, nm := range hist {
			if nm.Timestamp.Before(since) {
				continue
			}
			// 与 QueryNetworkMetrics 同一口径：样本自带 tenant_id 不匹配则不计入。
			if nm.TenantID != "" && tenantID != "" && nm.TenantID != tenantID {
				continue
			}
			// 与 QueryNetworkMetrics 同一口径：样本自带 tenant_id 不匹配则不计入。
			switch field {
			case "cpu_usage":
				sum += nm.CPUUsage
			case "memory_usage":
				sum += nm.MemoryUsage
			case "temperature":
				sum += nm.Temperature
			}
			count++
		}
	}
	if count == 0 {
		return -1
	}
	return sum / float64(count)
}
