package store

// memory_automation.go 实现 MemoryStore 的 AutomationStore 子接口（Phase 4 自动化闭环）。
//
// 自动化闭环内存实现：
//   - automationRules 字段在 MemoryStore struct 中定义（map[string]*AutomationRule）；
//   - automationExecutions 字段在 MemoryStore struct 中定义（map[string]*AutomationExecution）；
//   - NewMemoryStore 中初始化为空 map；
//   - 本文件实现 10 个方法，全部经 m.mu 互斥保护，并发安全。
//
// 设计要点（与 memory_traffic.go 风格一致）：
//   - ListAutomationRules/ListAutomationExecutions 返回深拷贝；
//   - CreateAutomationRule 分配随机 ID（"rule-" + 16 字节 hex）；
//   - EnableAutomationRule/DisableAutomationRule 切换 Enabled 字段。

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// randAutomationRuleID 生成随机自动化规则 ID（"rule-" + 16 字节 hex）。
func randAutomationRuleID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("rule-%d", time.Now().UnixNano())
	}
	return "rule-" + hex.EncodeToString(b)
}

// randAutomationExecID 生成随机自动化执行记录 ID（"exec-" + 16 字节 hex）。
func randAutomationExecID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("exec-%d", time.Now().UnixNano())
	}
	return "exec-" + hex.EncodeToString(b)
}

// cloneAutomationRule 返回 r 的深拷贝。
func cloneAutomationRule(r *AutomationRule) *AutomationRule {
	if r == nil {
		return nil
	}
	cp := *r
	if r.TriggerParams != nil {
		cp.TriggerParams = make(map[string]string, len(r.TriggerParams))
		for k, v := range r.TriggerParams {
			cp.TriggerParams[k] = v
		}
	}
	if r.Actions != nil {
		cp.Actions = make([]AutomationAction, len(r.Actions))
		for i, a := range r.Actions {
			cp.Actions[i] = a
			if a.Params != nil {
				cp.Actions[i].Params = make(map[string]string, len(a.Params))
				for k, v := range a.Params {
					cp.Actions[i].Params[k] = v
				}
			}
		}
	}
	return &cp
}

// cloneAutomationExecution 返回 e 的深拷贝。
func cloneAutomationExecution(e *AutomationExecution) *AutomationExecution {
	if e == nil {
		return nil
	}
	cp := *e
	return &cp
}

// CreateAutomationRule 创建自动化规则（ID 为空时分配随机 ID）。
func (m *MemoryStore) CreateAutomationRule(tenantID string, r *AutomationRule) *AutomationRule {
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
		r.ID = randAutomationRuleID()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	r.UpdatedAt = now
	m.automationRules[r.ID] = r
	return cloneAutomationRule(r)
}

// GetAutomationRule 按 (tenantID, id) 返回单个规则（深拷贝）。
func (m *MemoryStore) GetAutomationRule(tenantID, id string) (*AutomationRule, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.automationRules[id]
	if !ok {
		return nil, false
	}
	if tenantID != "" && r.TenantID != tenantID {
		return nil, false
	}
	return cloneAutomationRule(r), true
}

// ListAutomationRules 返回指定租户的全部规则（深拷贝，按创建时间降序）。
func (m *MemoryStore) ListAutomationRules(tenantID string) []*AutomationRule {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*AutomationRule, 0)
	for _, r := range m.automationRules {
		if tenantID != "" && r.TenantID != tenantID {
			continue
		}
		out = append(out, cloneAutomationRule(r))
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

// UpdateAutomationRule 更新规则（按 r.ID 定位，校验 tenantID 归属）。
func (m *MemoryStore) UpdateAutomationRule(tenantID string, r *AutomationRule) (*AutomationRule, bool) {
	if r == nil || r.ID == "" {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.automationRules[r.ID]
	if !ok {
		return nil, false
	}
	if tenantID != "" && existing.TenantID != tenantID {
		return nil, false
	}
	r.TenantID = existing.TenantID
	r.CreatedAt = existing.CreatedAt
	r.UpdatedAt = time.Now()
	m.automationRules[r.ID] = r
	return cloneAutomationRule(r), true
}

// DeleteAutomationRule 删除规则。
func (m *MemoryStore) DeleteAutomationRule(tenantID, id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.automationRules[id]
	if !ok {
		return false
	}
	if tenantID != "" && r.TenantID != tenantID {
		return false
	}
	delete(m.automationRules, id)
	return true
}

// EnableAutomationRule 启用规则（置 Enabled=true）。
func (m *MemoryStore) EnableAutomationRule(tenantID, id string) (*AutomationRule, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.automationRules[id]
	if !ok {
		return nil, false
	}
	if tenantID != "" && r.TenantID != tenantID {
		return nil, false
	}
	r.Enabled = true
	r.UpdatedAt = time.Now()
	return cloneAutomationRule(r), true
}

// DisableAutomationRule 禁用规则（置 Enabled=false）。
func (m *MemoryStore) DisableAutomationRule(tenantID, id string) (*AutomationRule, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.automationRules[id]
	if !ok {
		return nil, false
	}
	if tenantID != "" && r.TenantID != tenantID {
		return nil, false
	}
	r.Enabled = false
	r.UpdatedAt = time.Now()
	return cloneAutomationRule(r), true
}

// CreateAutomationExecution 创建执行记录（ID 为空时分配随机 ID）。
func (m *MemoryStore) CreateAutomationExecution(tenantID string, e *AutomationExecution) *AutomationExecution {
	if e == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	e.TenantID = tenantID
	m.mu.Lock()
	defer m.mu.Unlock()
	if e.ID == "" {
		e.ID = randAutomationExecID()
	}
	if e.StartedAt.IsZero() {
		e.StartedAt = time.Now()
	}
	if e.Status == "" {
		e.Status = "pending"
	}
	m.automationExecutions[e.ID] = e
	return cloneAutomationExecution(e)
}

// GetAutomationExecution 按 (tenantID, id) 返回单条执行记录（深拷贝）。
func (m *MemoryStore) GetAutomationExecution(tenantID, id string) (*AutomationExecution, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.automationExecutions[id]
	if !ok {
		return nil, false
	}
	if tenantID != "" && e.TenantID != tenantID {
		return nil, false
	}
	return cloneAutomationExecution(e), true
}

// ListAutomationExecutions 返回指定租户的执行记录列表（按开始时间降序，limit<=0 时返回全部）。
func (m *MemoryStore) ListAutomationExecutions(tenantID string, limit int) []*AutomationExecution {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*AutomationExecution, 0)
	for _, e := range m.automationExecutions {
		if tenantID != "" && e.TenantID != tenantID {
			continue
		}
		out = append(out, cloneAutomationExecution(e))
	}
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j].StartedAt.After(out[j-1].StartedAt) {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}
