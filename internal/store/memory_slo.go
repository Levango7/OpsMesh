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
//   - SLIStatus 返回模拟状态（CurrentValue=99.5, Status="met" 作为 MVP），
//     后续可接入 Prometheus 真实评估。
package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// randSLOID 生成随机 SLO ID（"slo-" + 16 字节 hex，crypto/rand 密码学安全）。
// 用于 CreateSLO 分配 ID（调用方未填 ID 时）。
func randSLOID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// 熵源失败回退时间戳（降级但可容忍，唯一性由 slos map key 兜底）。
		return fmt.Sprintf("slo-%d", time.Now().UnixNano())
	}
	return "slo-" + hex.EncodeToString(b)
}

// cloneSLO 返回 slo 的深拷贝（含 SLIs 切片）。
// 用于 GetSLO / ListSLOs / CreateSLO / UpdateSLO 返回，
// 避免外部修改破坏内部状态。
func cloneSLO(slo *SLO) *SLO {
	if slo == nil {
		return nil
	}
	cp := *slo
	if slo.SLIs != nil {
		cp.SLIs = append([]SLI(nil), slo.SLIs...)
	}
	return &cp
}

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
		slo.ID = randSLOID()
	}
	if slo.CreatedAt.IsZero() {
		slo.CreatedAt = now
	}
	slo.UpdatedAt = now
	m.slos[slo.ID] = slo
	return cloneSLO(slo)
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
	return cloneSLO(slo), true
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
	return cloneSLO(slo), true
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
		out = append(out, cloneSLO(slo))
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

// SLIStatus 返回指定 SLO 下各 SLI 的当前状态（MVP 返回模拟状态）。
//
// MVP 行为：
//   - SLO 不存在或租户不匹配返回 nil；
//   - 对每个 SLI 返回模拟状态：CurrentValue=99.5, Status="met"（满足目标），
//     LastEvaluated=now。后续可接入 Prometheus 真实评估。
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
	out := make([]*SLIStatus, 0, len(slo.SLIs))
	for _, sli := range slo.SLIs {
		out = append(out, &SLIStatus{
			SLIName:       sli.Name,
			CurrentValue:  99.5, // MVP 模拟值
			TargetValue:   sli.Target,
			Status:        "met", // MVP 假定满足
			LastEvaluated: now,
		})
	}
	return out
}
