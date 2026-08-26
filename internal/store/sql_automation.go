package store

// sql_automation.go 实现 SQLStore 的 AutomationStore 子接口（Phase 4 自动化闭环）。
//
// 【未实现的桩】本文件全部方法未接入 MySQL 持久化，经 stub_guard.StubNotImplemented
// 显式告警（每 (domain,method) 首次必打、60s 限频），并按统一约定返回零值：
//   - Create/Enable/Disable 返回 nil（不返回填充后的假对象）；
//   - Get/Update/Delete 返回 (nil, false) / false；List 返回非 nil 空切片。
//
// TODO(p4): 接入 MySQL 持久化（automation_rules 表：id PK + tenant_id + name +
// description + trigger_type + trigger_params JSON + actions JSON + enabled +
// created_at + updated_at；automation_executions 表：id PK + tenant_id + rule_id +
// rule_name + status + detail + started_at + ended_at）。

// CreateAutomationRule 创建自动化规则（未实现的桩）。
func (s *SQLStore) CreateAutomationRule(tenantID string, r *AutomationRule) *AutomationRule {
	StubNotImplemented("automation", "CreateAutomationRule")
	return nil
}

// GetAutomationRule 按 (tenantID, id) 返回单个规则（未实现的桩）。
func (s *SQLStore) GetAutomationRule(tenantID, id string) (*AutomationRule, bool) {
	StubNotImplemented("automation", "GetAutomationRule")
	return nil, false
}

// ListAutomationRules 返回指定租户的全部规则（未实现的桩；返回非 nil 空切片防上层 range panic）。
func (s *SQLStore) ListAutomationRules(tenantID string) []*AutomationRule {
	StubNotImplemented("automation", "ListAutomationRules")
	return []*AutomationRule{}
}

// UpdateAutomationRule 更新规则（未实现的桩）。
func (s *SQLStore) UpdateAutomationRule(tenantID string, r *AutomationRule) (*AutomationRule, bool) {
	StubNotImplemented("automation", "UpdateAutomationRule")
	return nil, false
}

// DeleteAutomationRule 删除规则（未实现的桩）。
func (s *SQLStore) DeleteAutomationRule(tenantID, id string) bool {
	StubNotImplemented("automation", "DeleteAutomationRule")
	return false
}

// EnableAutomationRule 启用规则（未实现的桩）。
func (s *SQLStore) EnableAutomationRule(tenantID, id string) (*AutomationRule, bool) {
	StubNotImplemented("automation", "EnableAutomationRule")
	return nil, false
}

// DisableAutomationRule 禁用规则（未实现的桩）。
func (s *SQLStore) DisableAutomationRule(tenantID, id string) (*AutomationRule, bool) {
	StubNotImplemented("automation", "DisableAutomationRule")
	return nil, false
}

// CreateAutomationExecution 创建执行记录（未实现的桩）。
func (s *SQLStore) CreateAutomationExecution(tenantID string, e *AutomationExecution) *AutomationExecution {
	StubNotImplemented("automation", "CreateAutomationExecution")
	return nil
}

// GetAutomationExecution 按 (tenantID, id) 返回单条执行记录（未实现的桩）。
func (s *SQLStore) GetAutomationExecution(tenantID, id string) (*AutomationExecution, bool) {
	StubNotImplemented("automation", "GetAutomationExecution")
	return nil, false
}

// ListAutomationExecutions 返回指定租户的执行记录列表（未实现的桩；返回非 nil 空切片防上层 range panic）。
func (s *SQLStore) ListAutomationExecutions(tenantID string, limit int) []*AutomationExecution {
	StubNotImplemented("automation", "ListAutomationExecutions")
	return []*AutomationExecution{}
}
