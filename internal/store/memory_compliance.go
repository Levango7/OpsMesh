package store

// memory_compliance.go 实现 MemoryStore 的 ComplianceStore 子接口（Phase 3 安全合规）。
//
// 合规报告内存实现：
//   - complianceReports 字段在 MemoryStore struct 中定义（map[string]*ComplianceReport）；
//   - NewMemoryStore 中初始化为空 map；
//   - 本文件实现 4 个方法，全部经 m.mu 互斥保护，并发安全。
//
// 设计要点（与 memory_traffic.go 风格一致）：
//   - ListReports 返回深拷贝避免外部修改破坏内部状态；
//   - SaveReport 分配随机 ID（"compliance-" + 16 字节 hex）；
//   - Results slice 深拷贝避免外部修改。

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"time"
)

// randComplianceID 生成随机合规报告 ID（"compliance-" + 16 字节 hex）。
func randComplianceID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("compliance-%d", time.Now().UnixNano())
	}
	return "compliance-" + hex.EncodeToString(b)
}

// cloneComplianceReport 返回 r 的深拷贝（含 Results slice）。
func cloneComplianceReport(r *ComplianceReport) *ComplianceReport {
	if r == nil {
		return nil
	}
	cp := *r
	if r.Results != nil {
		cp.Results = make([]ComplianceResult, len(r.Results))
		copy(cp.Results, r.Results)
	}
	return &cp
}

// SaveReport 保存合规报告（ID 为空时分配随机 ID）。
func (m *MemoryStore) SaveReport(tenantID string, r *ComplianceReport) *ComplianceReport {
	if r == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	r.TenantID = tenantID
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.ID == "" {
		r.ID = randComplianceID()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now()
	}
	m.complianceReports[r.ID] = r
	return cloneComplianceReport(r)
}

// GetReport 按 (tenantID, id) 返回单个合规报告（深拷贝）。
func (m *MemoryStore) GetReport(tenantID, id string) (*ComplianceReport, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.complianceReports[id]
	if !ok {
		return nil, false
	}
	if tenantID != "" && r.TenantID != tenantID {
		return nil, false
	}
	return cloneComplianceReport(r), true
}

// ListReports 返回指定租户的全部合规报告（深拷贝，按创建时间降序）。
func (m *MemoryStore) ListReports(tenantID string) []*ComplianceReport {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*ComplianceReport, 0)
	for _, r := range m.complianceReports {
		if tenantID != "" && r.TenantID != tenantID {
			continue
		}
		out = append(out, cloneComplianceReport(r))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

// DeleteReport 删除合规报告。
func (m *MemoryStore) DeleteReport(tenantID, id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.complianceReports[id]
	if !ok {
		return false
	}
	if tenantID != "" && r.TenantID != tenantID {
		return false
	}
	delete(m.complianceReports, id)
	return true
}