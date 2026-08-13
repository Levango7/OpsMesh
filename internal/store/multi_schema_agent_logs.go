package store

import "opsmesh/internal/proto"

// ============================================================================
// task 247 agent 日志上报：SaveLogs / AgentLogs（MultiSchemaStore 路由实现）
// ============================================================================
//
// MultiSchemaStore 按 tenantID 路由到对应 schema 的子 store（SQLStore），
// 复用子 store 的 SaveLogs/AgentLogs 实现。
//
// 注：此文件为 task 247 遗漏的 MultiSchemaStore 实现补全（task 248 验证编译时发现），
// 路由策略与 multi_schema.go 中其他方法一致（globalStore 兜底）。

// SaveLogs task 247：把 agent 上报的日志批次落库（路由到全局 store）。
// tenantID 为 agent 归属租户（由控制面回填，agent 不可伪造）；
// 强制覆盖 report.TenantID 保证行级隔离。report 为 nil 时直接返回。
func (m *MultiSchemaStore) SaveLogs(tenantID string, report *proto.LogReport) error {
	s, err := m.globalStore()
	if err != nil {
		return err
	}
	return s.SaveLogs(tenantID, report)
}

// AgentLogs task 247：查询已落库的 agent 日志批次（路由到全局 store）。
// tenantID 非空时按租户过滤（行级隔离），agentID 非空时按 agent 过滤，
// logName 非空时按日志标识过滤。
func (m *MultiSchemaStore) AgentLogs(tenantID, agentID, logName string) []proto.LogReport {
	s, err := m.globalStore()
	if err != nil {
		return nil
	}
	return s.AgentLogs(tenantID, agentID, logName)
}
