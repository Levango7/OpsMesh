
// sql_script.go 实现 SQLStore 的 ScriptStore 子接口（Phase 5 自定义脚本，桩实现）。
//
// TODO(p5): 接入 MySQL 持久化（scripts 表：id PK + tenant_id + name + language +
// content + params + timeout_sec + enabled + created_at + updated_at；
// script_executions 表：id PK + tenant_id + script_id + device_id + status +
// stdout + stderr + started_at + finished_at）。
// MVP 用内存桩实现，保证接口齐全 + go build 通过；DB 不可用时返回零值，
// 不 panic，与 SQLStore 其他桩方法风格一致（参考 sql_ticket.go）。
package store

import "time"

// CreateScript 创建脚本（桩实现）。
// TODO(p5): 落库 scripts 表（INSERT ... ON DUPLICATE KEY UPDATE）。
// MVP：DB 不可用时返回 s（不持久化），保证接口齐全。
func (s *SQLStore) CreateScript(tenantID string, sc *Script) *Script {
	if sc == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	sc.TenantID = tenantID
	if sc.ID == "" {
		sc.ID = randScriptID()
	}
	now := time.Now().UTC()
	if sc.CreatedAt.IsZero() {
		sc.CreatedAt = now
	}
	if sc.Language == "" {
		sc.Language = "shell"
	}
	sc.UpdatedAt = now
	return sc
}

// GetScript 按 (tenantID, id) 返回单个脚本（桩实现）。
// TODO(p5): SELECT * FROM scripts WHERE id=? AND tenant_id=?。
// MVP：DB 不可用时返回 (nil, false)。
func (s *SQLStore) GetScript(tenantID, id string) (*Script, bool) {
	return nil, false
}

// UpdateScript 更新脚本（桩实现）。
// TODO(p5): UPDATE scripts SET ... WHERE id=? AND tenant_id=?。
// MVP：DB 不可用时返回 (nil, false)。
func (s *SQLStore) UpdateScript(tenantID string, sc *Script) (*Script, bool) {
	return nil, false
}

// ListScripts 返回指定租户的全部脚本（桩实现）。
// TODO(p5): SELECT * FROM scripts WHERE tenant_id=? ORDER BY created_at DESC。
// MVP：DB 不可用时返回空 slice（非 nil，便于调用方 range）。
func (s *SQLStore) ListScripts(tenantID string) []*Script {
	return []*Script{}
}

// DeleteScript 删除脚本（桩实现）。
// TODO(p5): DELETE FROM scripts WHERE id=? AND tenant_id=?。
// MVP：DB 不可用时返回 false。
func (s *SQLStore) DeleteScript(tenantID, id string) bool {
	return false
}

// ListScriptExecutions 返回指定脚本的执行记录（桩实现）。
// TODO(p5): SELECT * FROM script_executions WHERE script_id=? AND tenant_id=?
// ORDER BY started_at DESC。
// MVP：DB 不可用时返回空 slice。
func (s *SQLStore) ListScriptExecutions(tenantID, scriptID string) []*ScriptExecution {
	return []*ScriptExecution{}
}