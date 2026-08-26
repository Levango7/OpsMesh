// sql_script.go 实现 SQLStore 的 ScriptStore 子接口（Phase 5 自定义脚本）。
//
// 【未实现的桩】本文件全部方法未接入 MySQL 持久化，经 stub_guard.StubNotImplemented
// 显式告警（每 (domain,method) 首次必打、60s 限频），并按统一约定返回零值：
//   - CreateScript 返回 nil（不返回填充后的假对象——杜绝「201 假成功 → GET 404」链路）；
//   - Get/Update/Delete 返回 (nil, false) / false；List 类返回非 nil 空切片。
//
// TODO(p5): 接入 MySQL 持久化（scripts 表：id PK + tenant_id + name + language +
// content + params + timeout_sec + enabled + created_at + updated_at；
// script_executions 表：id PK + tenant_id + script_id + device_id + status +
// stdout + stderr + started_at + finished_at）。
package store

import "time"

// CreateScript 创建脚本（未实现的桩）。
// TODO(p5): 落库 scripts 表（INSERT ... ON DUPLICATE KEY UPDATE）。
func (s *SQLStore) CreateScript(tenantID string, sc *Script) *Script {
	StubNotImplemented("script", "CreateScript")
	return nil
}

// GetScript 按 (tenantID, id) 返回单个脚本（未实现的桩）。
// TODO(p5): SELECT * FROM scripts WHERE id=? AND tenant_id=?。
func (s *SQLStore) GetScript(tenantID, id string) (*Script, bool) {
	StubNotImplemented("script", "GetScript")
	return nil, false
}

// UpdateScript 更新脚本（未实现的桩）。
// TODO(p5): UPDATE scripts SET ... WHERE id=? AND tenant_id=?。
func (s *SQLStore) UpdateScript(tenantID string, sc *Script) (*Script, bool) {
	StubNotImplemented("script", "UpdateScript")
	return nil, false
}

// ListScripts 返回指定租户的全部脚本（未实现的桩；返回非 nil 空切片防上层 range panic）。
// TODO(p5): SELECT * FROM scripts WHERE tenant_id=? ORDER BY created_at DESC。
func (s *SQLStore) ListScripts(tenantID string) []*Script {
	StubNotImplemented("script", "ListScripts")
	return []*Script{}
}

// DeleteScript 删除脚本（未实现的桩）。
// TODO(p5): DELETE FROM scripts WHERE id=? AND tenant_id=?。
func (s *SQLStore) DeleteScript(tenantID, id string) bool {
	StubNotImplemented("script", "DeleteScript")
	return false
}

// ListScriptExecutions 返回指定脚本的执行记录（未实现的桩；返回非 nil 空切片）。
// TODO(p5): SELECT * FROM script_executions WHERE script_id=? AND tenant_id=?
// ORDER BY started_at DESC。
func (s *SQLStore) ListScriptExecutions(tenantID, scriptID string) []*ScriptExecution {
	StubNotImplemented("script", "ListScriptExecutions")
	return []*ScriptExecution{}
}

// RecordScriptExecution 记录一条脚本执行记录（未实现的桩；返回 nil）。
// controlplane script execute handler 据此降级为模拟响应（不落库）。
// TODO(p5): INSERT INTO script_executions (id, tenant_id, script_id, device_id, status,
// stdout, stderr, started_at, finished_at) VALUES (...)。
func (s *SQLStore) RecordScriptExecution(tenantID, scriptID, deviceID, status, stdout, stderr string, startedAt time.Time, finishedAt *time.Time) *ScriptExecution {
	StubNotImplemented("script", "RecordScriptExecution")
	return nil
}
