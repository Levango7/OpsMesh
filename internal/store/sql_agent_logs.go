package store

import "opsmesh/internal/proto"

// ============================================================================
// task 247 agent 日志上报：SaveLogs / AgentLogs（SQLStore 内存暂存实现）
// ============================================================================

// SaveLogs task 247：把 agent 上报的日志批次暂存到内存 slice。
// SQLStore 对 agent 日志采用内存暂存策略：agent 上报日志的高频写入（每 30s/agent 一次）
// 不宜直接落 MySQL，检索侧由 logstore.SQLLogStore 走独立表/连接池承担。
// tenantID 为 agent 归属租户（由控制面回填，agent 不可伪造）；强制覆盖 report.TenantID 保证行级隔离。
// report 为 nil 时直接返回。深拷贝 report 及其 Lines 避免外部并发修改。
func (s *SQLStore) SaveLogs(tenantID string, report *proto.LogReport) error {
	if report == nil {
		return nil
	}
	cp := *report
	cp.TenantID = tenantID
	if len(report.Lines) > 0 {
		cp.Lines = make([]proto.LogLine, len(report.Lines))
		copy(cp.Lines, report.Lines)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agentLogs = append(s.agentLogs, cp)
	return nil
}

// AgentLogs task 247：查询已暂存的 agent 日志批次。
// tenantID 非空时按租户过滤（行级隔离），agentID 非空时按 agent 过滤，
// logName 非空时按日志标识过滤。返回深拷贝避免外部并发修改。
func (s *SQLStore) AgentLogs(tenantID, agentID, logName string) []proto.LogReport {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]proto.LogReport, 0, len(s.agentLogs))
	for _, r := range s.agentLogs {
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
