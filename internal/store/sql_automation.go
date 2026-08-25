package store

// sql_automation.go 实现 SQLStore 的 AutomationStore 子接口（Phase 4 自动化闭环，桩实现）。
//
// TODO(p4): 接入 MySQL 持久化（automation_rules 表：id PK + tenant_id + name +
// description + trigger_type + trigger_params JSON + actions JSON + enabled +
// created_at + updated_at；automation_executions 表：id PK + tenant_id + rule_id +
// rule_name + status + detail + started_at + ended_at）。
// MVP 用内存桩实现，保证接口齐全 + go build 通过；DB 不可用时返回零值，
// 不 panic，与 SQLStore 其他桩方法风格一致（参考 sql_traffic.go）。

// CreateAutomationRule 创建自动化规则（桩实现）。
func (s *SQLStore) CreateAutomationRule(tenantID string, r *AutomationRule) *AutomationRule {
	return nil
}

// GetAutomationRule 按 (tenantID, id) 返回单个规则（桩实现）。
func (s *SQLStore) GetAutomationRule(tenantID, id string) (*AutomationRule, bool) {
	return nil, false
}

// ListAutomationRules 返回指定租户的全部规则（桩实现）。
func (s *SQLStore) ListAutomationRules(tenantID string) []*AutomationRule {
	return []*AutomationRule{}
}

// UpdateAutomationRule 更新规则（桩实现）。
func (s *SQLStore) UpdateAutomationRule(tenantID string, r *AutomationRule) (*AutomationRule, bool) {
	return nil, false
}

// DeleteAutomationRule 删除规则（桩实现）。
func (s *SQLStore) DeleteAutomationRule(tenantID, id string) bool {
	return false
}

// EnableAutomationRule 启用规则（桩实现）。
func (s *SQLStore) EnableAutomationRule(tenantID, id string) (*AutomationRule, bool) {
	return nil, false
}

// DisableAutomationRule 禁用规则（桩实现）。
func (s *SQLStore) DisableAutomationRule(tenantID, id string) (*AutomationRule, bool) {
	return nil, false
}

// CreateAutomationExecution 创建执行记录（桩实现）。
func (s *SQLStore) CreateAutomationExecution(tenantID string, e *AutomationExecution) *AutomationExecution {
	return nil
}

// GetAutomationExecution 按 (tenantID, id) 返回单条执行记录（桩实现）。
func (s *SQLStore) GetAutomationExecution(tenantID, id string) (*AutomationExecution, bool) {
	return nil, false
}

// ListAutomationExecutions 返回指定租户的执行记录列表（桩实现）。
func (s *SQLStore) ListAutomationExecutions(tenantID string, limit int) []*AutomationExecution {
	return []*AutomationExecution{}
}