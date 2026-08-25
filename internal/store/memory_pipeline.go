package store

// memory_pipeline.go 实现 MemoryStore 的 PipelineStore 子接口（Phase 2 CI/CD 流水线）。
//
// CI/CD 流水线内存实现：
//   - pipelineTemplates / pipelineRuns 字段在 MemoryStore struct 中定义；
//   - NewMemoryStore 中初始化为空 map；
//   - 本文件实现 8 个方法，全部经 m.mu 互斥保护，并发安全。
//
// 设计要点（与 memory_ticket.go 风格一致）：
//   - 返回深拷贝避免外部修改破坏内部状态；
//   - CreateTemplate 分配随机 ID（"pipeline-" + 16 字节 hex）；
//   - CreateRun 分配随机 ID（"run-" + 16 字节 hex）；


import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// randPipelineID 生成随机流水线模板 ID（"pipeline-" + 16 字节 hex）。
func randPipelineID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("pipeline-%d", time.Now().UnixNano())
	}
	return "pipeline-" + hex.EncodeToString(b)
}

// randRunID 生成随机运行记录 ID（"run-" + 16 字节 hex）。
func randRunID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("run-%d", time.Now().UnixNano())
	}
	return "run-" + hex.EncodeToString(b)
}

// clonePipelineTemplate 返回 t 的深拷贝。
func clonePipelineTemplate(t *PipelineTemplate) *PipelineTemplate {
	if t == nil {
		return nil
	}
	cp := *t
	if t.Parameters != nil {
		cp.Parameters = make([]PipelineParam, len(t.Parameters))
		copy(cp.Parameters, t.Parameters)
	}
	return &cp
}

// clonePipelineRun 返回 r 的深拷贝。
func clonePipelineRun(r *PipelineRun) *PipelineRun {
	if r == nil {
		return nil
	}
	cp := *r
	if r.Parameters != nil {
		cp.Parameters = make(map[string]string, len(r.Parameters))
		for k, v := range r.Parameters {
			cp.Parameters[k] = v
		}
	}
	if r.StartedAt != nil {
		st := *r.StartedAt
		cp.StartedAt = &st
	}
	if r.FinishedAt != nil {
		ft := *r.FinishedAt
		cp.FinishedAt = &ft
	}
	return &cp
}

// CreateTemplate 创建流水线模板（ID 为空时分配随机 ID）。
func (m *MemoryStore) CreateTemplate(tenantID string, t *PipelineTemplate) *PipelineTemplate {
	if t == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	t.TenantID = tenantID
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if t.ID == "" {
		t.ID = randPipelineID()
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	m.pipelineTemplates[t.ID] = t
	return clonePipelineTemplate(t)
}

// GetTemplate 按 (tenantID, id) 返回单个模板（深拷贝）。
func (m *MemoryStore) GetTemplate(tenantID, id string) (*PipelineTemplate, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.pipelineTemplates[id]
	if !ok {
		return nil, false
	}
	if tenantID != "" && t.TenantID != tenantID {
		return nil, false
	}
	return clonePipelineTemplate(t), true
}

// ListTemplates 返回指定租户的全部模板（深拷贝，按创建时间降序）。
func (m *MemoryStore) ListTemplates(tenantID string) []*PipelineTemplate {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*PipelineTemplate, 0)
	for _, t := range m.pipelineTemplates {
		if tenantID != "" && t.TenantID != tenantID {
			continue
		}
		out = append(out, clonePipelineTemplate(t))
	}
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j].CreatedAt.After(out[j-1].CreatedAt) {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}

// DeleteTemplate 删除模板。
func (m *MemoryStore) DeleteTemplate(tenantID, id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.pipelineTemplates[id]
	if !ok {
		return false
	}
	if tenantID != "" && t.TenantID != tenantID {
		return false
	}
	delete(m.pipelineTemplates, id)
	return true
}

// CreateRun 创建运行记录（ID 为空时分配随机 ID）。
func (m *MemoryStore) CreateRun(tenantID string, r *PipelineRun) *PipelineRun {
	if r == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	r.TenantID = tenantID
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if r.ID == "" {
		r.ID = randRunID()
	}
	if r.Status == "" {
		r.Status = "pending"
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	m.pipelineRuns[r.ID] = r
	return clonePipelineRun(r)
}

// GetRun 按 (tenantID, id) 返回单条运行记录（深拷贝）。
func (m *MemoryStore) GetRun(tenantID, id string) (*PipelineRun, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.pipelineRuns[id]
	if !ok {
		return nil, false
	}
	if tenantID != "" && r.TenantID != tenantID {
		return nil, false
	}
	return clonePipelineRun(r), true
}

// ListRuns 返回运行记录列表（按 tenantID + 可选 templateID 过滤，按创建时间降序）。
func (m *MemoryStore) ListRuns(tenantID string, templateID string) []*PipelineRun {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*PipelineRun, 0)
	for _, r := range m.pipelineRuns {
		if tenantID != "" && r.TenantID != tenantID {
			continue
		}
		if templateID != "" && r.TemplateID != templateID {
			continue
		}
		out = append(out, clonePipelineRun(r))
	}
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j].CreatedAt.After(out[j-1].CreatedAt) {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}

// UpdateRun 更新运行记录（按 r.ID 定位，校验 tenantID 归属）。
func (m *MemoryStore) UpdateRun(tenantID string, r *PipelineRun) (*PipelineRun, bool) {
	if r == nil || r.ID == "" {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.pipelineRuns[r.ID]
	if !ok {
		return nil, false
	}
	if tenantID != "" && existing.TenantID != tenantID {
		return nil, false
	}
	r.TenantID = existing.TenantID
	r.CreatedAt = existing.CreatedAt
	// 保留原 TemplateID/TemplateName（运行记录创建后不可改关联模板）
	if r.TemplateID == "" {
		r.TemplateID = existing.TemplateID
	}
	if r.TemplateName == "" {
		r.TemplateName = existing.TemplateName
	}
	// StartedAt/FinishedAt 保留原值如果入参未提供
	if r.StartedAt == nil {
		r.StartedAt = existing.StartedAt
	}
	if r.FinishedAt == nil {
		r.FinishedAt = existing.FinishedAt
	}
	m.pipelineRuns[r.ID] = r
	return clonePipelineRun(r), true
}