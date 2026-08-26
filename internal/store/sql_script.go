// sql_script.go 实现 SQLStore 的 ScriptStore 子接口（Phase 5 自定义脚本，生产就绪）。
//
// 表结构：scripts（id PK + tenant_id + name + language + content + params +
// timeout_sec + enabled TINYINT(1) + created_at + updated_at）+
// script_executions（id PK + tenant_id + script_id + device_id + status +
// stdout + stderr + started_at + finished_at 可空）。迁移文件
// migrations/014_p5_script_webhook.sql 幂等建表。
//
// 设计要点（与 sql_slo.go / sql_ticket.go 风格一致）：
//   - bool 列 enabled 用 TINYINT(1)，默认 1（新建脚本默认启用，与 memory 一致，
//     避免零值 false 导致 execute 全部被拒）；
//   - 可空 finished_at 用 sql.NullTime 读写（NULL 表示未结束）；
//   - CreateScript 按 ID 幂等（INSERT ... ON DUPLICATE KEY UPDATE），tenant_id 仅插入
//     不更新（防 upsert 改写归属）；
//   - ListScripts 按创建时间降序（最新优先，与 memory 一致）；
//   - UpdateScript 先 SELECT 校验存在 + 租户归属，再 UPDATE，保留原 CreatedAt/TenantID；
//   - ListScriptExecutions 先校验 scriptID 归属（GetScript），不匹配返回空 slice；
//   - RecordScriptExecution：ID 由 store 分配（randScriptExecutionID()，前缀 script-exec-），
//     StartedAt 零值填 now；
//   - DB 不可用时返回零值（nil/false/空 slice），不 panic；
//   - ID 生成复用 memory_script.go 的 randScriptID（"script-" + 16 字节 hex）+
//     randScriptExecutionID（"script-exec-" + 16 字节 hex）。
package store

import (
	"context"
	"database/sql"
	"log"
	"time"
)

// scanScript 从一行扫描出 *Script。
// 列顺序：id, tenant_id, name, language, content, params, timeout_sec, enabled,
// created_at, updated_at。无行或扫描失败返回 nil。
func scanScript(row rowScanner) *Script {
	var sc Script
	var enabled int
	var createdAt, updatedAt time.Time
	if err := row.Scan(&sc.ID, &sc.TenantID, &sc.Name, &sc.Language, &sc.Content,
		&sc.Params, &sc.TimeoutSec, &enabled, &createdAt, &updatedAt); err != nil {
		return nil
	}
	sc.Enabled = enabled != 0
	sc.CreatedAt = createdAt
	sc.UpdatedAt = updatedAt
	return &sc
}

// scriptEnabledInt 将 bool 转换为 TINYINT(1) 用的 int（true→1，false→0）。
func scriptEnabledInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// CreateScript 创建脚本（按 ID 幂等；ID 为空时分配随机 ID）。
//
// 行为：
//   - sc == nil 返回 nil；
//   - TenantID 为空时归一为 default；
//   - ID 为空时分配随机 ID（新建场景）；
//   - CreatedAt 为零值时填当前时间（新建场景）；
//   - UpdatedAt 始终刷新为当前时间；
//   - Language 为空时默认 "shell"；
//   - Enabled 零值时默认 true（新建即启用，与 memory 一致）；
//   - INSERT ... ON DUPLICATE KEY UPDATE 实现 upsert（按 id 幂等），
//     tenant_id 仅插入不更新，防 upsert 改写归属；
//   - DB 失败时 log.Printf + 返回 nil。
func (s *SQLStore) CreateScript(tenantID string, sc *Script) *Script {
	if sc == nil {
		return nil
	}
	// 租户隔离：空租户归一为 default。
	if tenantID == "" {
		tenantID = "default"
	}
	sc.TenantID = tenantID
	now := time.Now().UTC()
	if sc.ID == "" {
		sc.ID = randScriptID()
	}
	if sc.CreatedAt.IsZero() {
		sc.CreatedAt = now
	}
	if sc.Language == "" {
		sc.Language = "shell"
	}
	// 新建脚本默认启用（Enabled=true）：创建即为可执行，
	// 禁用需显式 UpdateScript 设 Enabled=false。避免零值 false 导致 execute 全部被拒。
	if !sc.Enabled {
		sc.Enabled = true
	}
	sc.UpdatedAt = now
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO scripts (id, tenant_id, name, language, content, params, timeout_sec, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE name=VALUES(name), language=VALUES(language),
		 content=VALUES(content), params=VALUES(params), timeout_sec=VALUES(timeout_sec),
		 enabled=VALUES(enabled), updated_at=VALUES(updated_at)`,
		sc.ID, sc.TenantID, sc.Name, sc.Language, sc.Content, sc.Params,
		sc.TimeoutSec, scriptEnabledInt(sc.Enabled), sc.CreatedAt, sc.UpdatedAt); err != nil {
		log.Printf("[store] CreateScript 插入失败 (tenant=%s script=%s): %v", tenantID, sc.ID, err)
		return nil
	}
	return cloneScript(sc)
}

// GetScript 按 (tenantID, id) 返回单个脚本（深拷贝；不存在或租户不匹配返回 (nil, false)）。
func (s *SQLStore) GetScript(tenantID, id string) (*Script, bool) {
	row := s.db.QueryRowContext(context.Background(),
		`SELECT id, tenant_id, name, language, content, params, timeout_sec, enabled, created_at, updated_at
		  FROM scripts WHERE id=? AND tenant_id=?`, id, tenantID)
	sc := scanScript(row)
	if sc == nil {
		return nil, false
	}
	return sc, true
}

// UpdateScript 更新脚本（按 sc.ID 定位，校验 tenantID 归属）。
//
// 行为：
//   - sc == nil 或 ID 为空返回 (nil, false)；
//   - 先 GetScript 校验存在 + 租户归属，不存在返回 (nil, false)；
//   - CreatedAt / TenantID 不可改（保留原值，防越权改归属）；
//   - UpdatedAt 始终刷新为当前时间；
//   - 返回更新后的脚本（深拷贝）。
func (s *SQLStore) UpdateScript(tenantID string, sc *Script) (*Script, bool) {
	if sc == nil || sc.ID == "" {
		return nil, false
	}
	// 先 SELECT 校验存在 + 租户归属。
	existing, ok := s.GetScript(tenantID, sc.ID)
	if !ok {
		return nil, false
	}
	// 保留不可改字段。
	sc.ID = existing.ID
	sc.TenantID = existing.TenantID
	sc.CreatedAt = existing.CreatedAt
	sc.UpdatedAt = time.Now().UTC()
	if _, err := s.db.ExecContext(context.Background(),
		`UPDATE scripts SET name=?, language=?, content=?, params=?, timeout_sec=?, enabled=?, updated_at=?
		 WHERE id=? AND tenant_id=?`,
		sc.Name, sc.Language, sc.Content, sc.Params, sc.TimeoutSec,
		scriptEnabledInt(sc.Enabled), sc.UpdatedAt, sc.ID, sc.TenantID); err != nil {
		log.Printf("[store] UpdateScript 更新失败 (tenant=%s script=%s): %v", tenantID, sc.ID, err)
		return nil, false
	}
	return cloneScript(sc), true
}

// ListScripts 返回指定租户的全部脚本（按创建时间降序；深拷贝）。
func (s *SQLStore) ListScripts(tenantID string) []*Script {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT id, tenant_id, name, language, content, params, timeout_sec, enabled, created_at, updated_at
		  FROM scripts WHERE tenant_id=? ORDER BY created_at DESC`, tenantID)
	if err != nil {
		log.Printf("[store] ListScripts 查询失败 (tenant=%s): %v", tenantID, err)
		return []*Script{}
	}
	defer rows.Close()
	out := make([]*Script, 0)
	for rows.Next() {
		if sc := scanScript(rows); sc != nil {
			out = append(out, sc)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] ListScripts 遍历失败: %v", err)
	}
	return out
}

// DeleteScript 删除脚本，返回是否删除成功（不存在或租户不匹配返回 false）。
func (s *SQLStore) DeleteScript(tenantID, id string) bool {
	res, err := s.db.ExecContext(context.Background(),
		`DELETE FROM scripts WHERE id=? AND tenant_id=?`, id, tenantID)
	if err != nil {
		log.Printf("[store] DeleteScript 失败 (tenant=%s script=%s): %v", tenantID, id, err)
		return false
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		log.Printf("[store] DeleteScript RowsAffected 失败 (tenant=%s script=%s): %v", tenantID, id, rowsErr)
		return false
	}
	return n > 0
}

// scanScriptExecution 从一行扫描出 *ScriptExecution（finished_at 可空）。
// 列顺序：id, tenant_id, script_id, device_id, status, stdout, stderr,
// started_at, finished_at。无行或扫描失败返回 nil。
func scanScriptExecution(row rowScanner) *ScriptExecution {
	var e ScriptExecution
	var startedAt time.Time
	var finishedAt sql.NullTime
	if err := row.Scan(&e.ID, &e.TenantID, &e.ScriptID, &e.DeviceID, &e.Status,
		&e.Stdout, &e.Stderr, &startedAt, &finishedAt); err != nil {
		return nil
	}
	e.StartedAt = startedAt
	if finishedAt.Valid {
		ft := finishedAt.Time
		e.FinishedAt = &ft
	}
	return &e
}

// scriptFinishedAtValue 将 *time.Time 转换为 sql.NullTime（nil → Invalid）。
func scriptFinishedAtValue(ft *time.Time) sql.NullTime {
	if ft == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *ft, Valid: true}
}

// ListScriptExecutions 返回指定脚本的执行记录（按开始时间降序；深拷贝）。
// 先校验 scriptID 归属（GetScript），不存在或租户不匹配返回空 slice。
func (s *SQLStore) ListScriptExecutions(tenantID, scriptID string) []*ScriptExecution {
	// 校验脚本归属。
	if _, ok := s.GetScript(tenantID, scriptID); !ok {
		return []*ScriptExecution{}
	}
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT id, tenant_id, script_id, device_id, status, stdout, stderr, started_at, finished_at
		  FROM script_executions WHERE tenant_id=? AND script_id=? ORDER BY started_at DESC`,
		tenantID, scriptID)
	if err != nil {
		log.Printf("[store] ListScriptExecutions 查询失败 (tenant=%s script=%s): %v", tenantID, scriptID, err)
		return []*ScriptExecution{}
	}
	defer rows.Close()
	out := make([]*ScriptExecution, 0)
	for rows.Next() {
		if e := scanScriptExecution(rows); e != nil {
			out = append(out, e)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] ListScriptExecutions 遍历失败: %v", err)
	}
	return out
}

// RecordScriptExecution 记录一条脚本执行记录（供 controlplane execute handler 调用）。
//
// 行为：
//   - TenantID 为空时归一为 default；
//   - ID 由 store 分配（randScriptExecutionID()，前缀 script-exec-）；
//   - StartedAt 零值时填当前时间；
//   - FinishedAt 为 nil 时落 NULL（未结束）；
//   - DB 失败时 log.Printf + 返回 nil。
func (s *SQLStore) RecordScriptExecution(tenantID, scriptID, deviceID, status, stdout, stderr string, startedAt time.Time, finishedAt *time.Time) *ScriptExecution {
	if tenantID == "" {
		tenantID = "default"
	}
	now := time.Now().UTC()
	if startedAt.IsZero() {
		startedAt = now
	}
	e := &ScriptExecution{
		ID:         randScriptExecutionID(),
		TenantID:   tenantID,
		ScriptID:   scriptID,
		DeviceID:   deviceID,
		Status:     status,
		Stdout:     stdout,
		Stderr:     stderr,
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
	}
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO script_executions (id, tenant_id, script_id, device_id, status, stdout, stderr, started_at, finished_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.TenantID, e.ScriptID, e.DeviceID, e.Status, e.Stdout, e.Stderr,
		e.StartedAt, scriptFinishedAtValue(e.FinishedAt)); err != nil {
		log.Printf("[store] RecordScriptExecution 插入失败 (tenant=%s script=%s exec=%s): %v", tenantID, scriptID, e.ID, err)
		return nil
	}
	return cloneScriptExecution(e)
}
