package store

import "opsmesh/internal/proto"

// ============================================================================
// task 247 agent 日志上报：SaveLogs / AgentLogs
// ============================================================================

// SaveLogs task 247：把 agent 上报的日志批次落库到内存 slice。
// tenantID 为 agent 归属租户（由控制面 gRPC handler 按 agent 注册时盖章回填，agent 不可伪造）；
// 强制覆盖 report.TenantID 以保证行级隔离。report 为 nil 时直接返回。
// 深拷贝 report 及其 Lines 避免外部并发修改破坏内部状态。
func (m *MemoryStore) SaveLogs(tenantID string, report *proto.LogReport) error {
	if report == nil {
		return nil
	}
	// 深拷贝 report 及 Lines 切片，隔离外部修改。
	cp := *report
	cp.TenantID = tenantID // 强制租户隔离：以控制面回填为准
	if len(report.Lines) > 0 {
		cp.Lines = make([]proto.LogLine, len(report.Lines))
		copy(cp.Lines, report.Lines)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.agentLogs = append(m.agentLogs, cp)
	return nil
}

// AgentLogs task 247：查询已落库的 agent 日志批次。
// tenantID 非空时按租户过滤（行级隔离），agentID 非空时按 agent 过滤，
// logName 非空时按日志标识过滤。返回深拷贝避免外部并发修改。
func (m *MemoryStore) AgentLogs(tenantID, agentID, logName string) []proto.LogReport {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]proto.LogReport, 0, len(m.agentLogs))
	for _, r := range m.agentLogs {
		if tenantID != "" && r.TenantID != tenantID {
			continue
		}
		if agentID != "" && r.AgentID != agentID {
			continue
		}
		if logName != "" && r.LogName != logName {
			continue
		}
		cp := r
		if len(r.Lines) > 0 {
			cp.Lines = make([]proto.LogLine, len(r.Lines))
			copy(cp.Lines, r.Lines)
		}
		out = append(out, cp)
	}
	return out
}
