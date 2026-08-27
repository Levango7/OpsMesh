package store

// sql_automation.go 实现 SQLStore 的 AutomationStore 子接口（Phase 4 自动化闭环，生产就绪）。
//
// 表结构：
//   - automation_rules（id PK + tenant_id + name + description + trigger_type +
//     trigger_params JSON + actions JSON + enabled TINYINT(1) + created_at + updated_at）；
//   - automation_executions（id PK + tenant_id + rule_id + rule_name + status +
//     detail + started_at + ended_at 可空）。
// 迁移文件 migrations/013_p4_automation_network.sql 幂等建表。
//
// 设计要点（与 sql_secret.go / sql_k8s.go 风格一致）：
//   - JSON 列 trigger_params（map[string]string）+ actions（[]AutomationAction）用
//     encoding/json Marshal/Unmarshal 序列化为 TEXT；空值存空串，读取时空串跳过 Unmarshal；
//   - bool 列 enabled 用 TINYINT(1)，写入 0/1，读取扫描到 int 后转换；
//   - 可空 ended_at 用 sql.NullTime 处理；
//   - 租户隔离：Get/Update/Delete/Enable/Disable 均 WHERE id=? AND tenant_id=?，
//     List 均 WHERE tenant_id=?；
//   - ID 生成复用 memory_automation.go 的 randAutomationRuleID/randAutomationExecID；
//   - 接口签名无 error 返回值，SQL 错误时 log.Printf + 返回零值（nil/false/空 slice）；
//   - 全部查询使用 context.Background() + ? 占位符。

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"time"
)

// scanAutomationRule 从一行扫描出 *AutomationRule。
// 列顺序：id, tenant_id, name, description, trigger_type, trigger_params, actions,
//
//	enabled, created_at, updated_at。
func scanAutomationRule(row rowScanner) *AutomationRule {
	var r AutomationRule
	var triggerParamsJSON, actionsJSON []byte
	var enabled int
	var createdAt, updatedAt time.Time
	if err := row.Scan(
		&r.ID, &r.TenantID, &r.Name, &r.Description, &r.TriggerType,
		&triggerParamsJSON, &actionsJSON, &enabled, &createdAt, &updatedAt,
	); err != nil {
		return nil
	}
	r.Enabled = enabled != 0
	r.CreatedAt = createdAt
	r.UpdatedAt = updatedAt
	if len(triggerParamsJSON) > 0 {
		var tp map[string]string
		if err := json.Unmarshal(triggerParamsJSON, &tp); err == nil {
			r.TriggerParams = tp
		}
	}
	if len(actionsJSON) > 0 {
		var acts []AutomationAction
		if err := json.Unmarshal(actionsJSON, &acts); err == nil {
			r.Actions = acts
		}
	}
	return &r
}

// marshalTriggerParams 将 triggerParams map 序列化为 JSON 字节（nil 时返回空字串）。
func marshalTriggerParams(m map[string]string) []byte {
	if m == nil {
		return []byte{}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return []byte{}
	}
	return b
}

// marshalActions 将 actions 切片序列化为 JSON 字节（nil 时返回空字串）。
func marshalActions(acts []AutomationAction) []byte {
	if acts == nil {
		return []byte{}
	}
	b, err := json.Marshal(acts)
	if err != nil {
		return []byte{}
	}
	return b
}

// CreateAutomationRule 创建自动化规则（ID 为空时分配随机 ID）。
// TenantID 为空时归一为 default。返回持久化后的规则（含分配的 ID）。
func (s *SQLStore) CreateAutomationRule(tenantID string, r *AutomationRule) *AutomationRule {
	if r == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	r.TenantID = tenantID
	if r.ID == "" {
		r.ID = randAutomationRuleID()
	}
	now := time.Now().UTC()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	r.UpdatedAt = now
	enabled := 0
	if r.Enabled {
		enabled = 1
	}
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO automation_rules
		   (id, tenant_id, name, description, trigger_type, trigger_params, actions, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE
		   name=VALUES(name), description=VALUES(description), trigger_type=VALUES(trigger_type),
		   trigger_params=VALUES(trigger_params), actions=VALUES(actions),
		   enabled=VALUES(enabled), updated_at=VALUES(updated_at)`,
		r.ID, r.TenantID, r.Name, r.Description, r.TriggerType,
		marshalTriggerParams(r.TriggerParams), marshalActions(r.Actions),
		enabled, r.CreatedAt, r.UpdatedAt); err != nil {
		log.Printf("[store] CreateAutomationRule 插入失败 (tenant=%s id=%s): %v", tenantID, r.ID, err)
		return nil
	}
	return r
}

// GetAutomationRule 按 (tenantID, id) 返回单个规则（不存在返回 (nil, false)）。
func (s *SQLStore) GetAutomationRule(tenantID, id string) (*AutomationRule, bool) {
	row := s.db.QueryRowContext(context.Background(),
		`SELECT id, tenant_id, name, description, trigger_type, trigger_params, actions,
		         enabled, created_at, updated_at
		   FROM automation_rules WHERE id=? AND tenant_id=?`, id, tenantID)
	r := scanAutomationRule(row)
	if r == nil {
		return nil, false
	}
	return r, true
}

// ListAutomationRules 返回指定租户的全部规则（按创建时间降序）。
func (s *SQLStore) ListAutomationRules(tenantID string) []*AutomationRule {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT id, tenant_id, name, description, trigger_type, trigger_params, actions,
		         enabled, created_at, updated_at
		   FROM automation_rules WHERE tenant_id=? ORDER BY created_at DESC`, tenantID)
	if err != nil {
		log.Printf("[store] ListAutomationRules 查询失败 (tenant=%s): %v", tenantID, err)
		return []*AutomationRule{}
	}
	defer rows.Close()
	out := make([]*AutomationRule, 0)
	for rows.Next() {
		if r := scanAutomationRule(rows); r != nil {
			out = append(out, r)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] ListAutomationRules 遍历失败: %v", err)
	}
	return out
}

// UpdateAutomationRule 更新规则（按 r.ID 定位，校验 tenantID 归属）。
// 不存在或越权返回 (nil, false)。CreatedAt 保留原值，UpdatedAt 置 now。
func (s *SQLStore) UpdateAutomationRule(tenantID string, r *AutomationRule) (*AutomationRule, bool) {
	if r == nil || r.ID == "" {
		return nil, false
	}
	existing, ok := s.GetAutomationRule(tenantID, r.ID)
	if !ok {
		return nil, false
	}
	r.TenantID = existing.TenantID
	r.CreatedAt = existing.CreatedAt
	r.UpdatedAt = time.Now().UTC()
	enabled := 0
	if r.Enabled {
		enabled = 1
	}
	if _, err := s.db.ExecContext(context.Background(),
		`UPDATE automation_rules SET
		   name=?, description=?, trigger_type=?, trigger_params=?, actions=?,
		   enabled=?, updated_at=?
		 WHERE id=? AND tenant_id=?`,
		r.Name, r.Description, r.TriggerType,
		marshalTriggerParams(r.TriggerParams), marshalActions(r.Actions),
		enabled, r.UpdatedAt, r.ID, tenantID); err != nil {
		log.Printf("[store] UpdateAutomationRule 更新失败 (tenant=%s id=%s): %v", tenantID, r.ID, err)
		return nil, false
	}
	return r, true
}

// DeleteAutomationRule 删除规则，返回是否删除成功（不存在或租户不匹配返回 false）。
func (s *SQLStore) DeleteAutomationRule(tenantID, id string) bool {
	res, err := s.db.ExecContext(context.Background(),
		`DELETE FROM automation_rules WHERE id=? AND tenant_id=?`, id, tenantID)
	if err != nil {
		log.Printf("[store] DeleteAutomationRule 失败 (tenant=%s id=%s): %v", tenantID, id, err)
		return false
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		log.Printf("[store] DeleteAutomationRule RowsAffected 失败 (tenant=%s id=%s): %v", tenantID, id, rowsErr)
		return false
	}
	return n > 0
}

// EnableAutomationRule 启用规则（置 enabled=1 + updated_at=now）。
// 不存在或越权返回 (nil, false)。返回更新后的规则。
func (s *SQLStore) EnableAutomationRule(tenantID, id string) (*AutomationRule, bool) {
	return s.toggleAutomationRule(tenantID, id, true)
}

// DisableAutomationRule 禁用规则（置 enabled=0 + updated_at=now）。
// 不存在或越权返回 (nil, false)。返回更新后的规则。
func (s *SQLStore) DisableAutomationRule(tenantID, id string) (*AutomationRule, bool) {
	return s.toggleAutomationRule(tenantID, id, false)
}

// toggleAutomationRule 切换规则启用状态（enabled=1/0），并返回更新后的规则。
func (s *SQLStore) toggleAutomationRule(tenantID, id string, enable bool) (*AutomationRule, bool) {
	enabled := 0
	if enable {
		enabled = 1
	}
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(context.Background(),
		`UPDATE automation_rules SET enabled=?, updated_at=? WHERE id=? AND tenant_id=?`,
		enabled, now, id, tenantID); err != nil {
		log.Printf("[store] toggleAutomationRule 更新失败 (tenant=%s id=%s enable=%v): %v",
			tenantID, id, enable, err)
		return nil, false
	}
	return s.GetAutomationRule(tenantID, id)
}

// scanAutomationExecution 从一行扫描出 *AutomationExecution。
// 列顺序：id, tenant_id, rule_id, rule_name, status, detail, started_at, ended_at。
func scanAutomationExecution(row rowScanner) *AutomationExecution {
	var e AutomationExecution
	var startedAt time.Time
	var endedAt sql.NullTime
	if err := row.Scan(
		&e.ID, &e.TenantID, &e.RuleID, &e.RuleName, &e.Status, &e.Detail, &startedAt, &endedAt,
	); err != nil {
		return nil
	}
	e.StartedAt = startedAt
	if endedAt.Valid {
		t := endedAt.Time
		e.EndedAt = &t
	}
	return &e
}

// CreateAutomationExecution 创建执行记录（ID 为空时分配随机 ID）。
// TenantID 为空时归一为 default。Status 空时默认 pending；StartedAt 零值填 now。
func (s *SQLStore) CreateAutomationExecution(tenantID string, e *AutomationExecution) *AutomationExecution {
	if e == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	e.TenantID = tenantID
	if e.ID == "" {
		e.ID = randAutomationExecID()
	}
	if e.Status == "" {
		e.Status = "pending"
	}
	if e.StartedAt.IsZero() {
		e.StartedAt = time.Now().UTC()
	}
	var endedAt interface{}
	if e.EndedAt != nil {
		endedAt = *e.EndedAt
	} else {
		endedAt = nil
	}
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO automation_executions
		   (id, tenant_id, rule_id, rule_name, status, detail, started_at, ended_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE
		   rule_id=VALUES(rule_id), rule_name=VALUES(rule_name), status=VALUES(status),
		   detail=VALUES(detail), started_at=VALUES(started_at), ended_at=VALUES(ended_at)`,
		e.ID, e.TenantID, e.RuleID, e.RuleName, e.Status, e.Detail, e.StartedAt, endedAt); err != nil {
		log.Printf("[store] CreateAutomationExecution 插入失败 (tenant=%s id=%s): %v", tenantID, e.ID, err)
		return nil
	}
	return e
}

// GetAutomationExecution 按 (tenantID, id) 返回单条执行记录（不存在返回 (nil, false)）。
func (s *SQLStore) GetAutomationExecution(tenantID, id string) (*AutomationExecution, bool) {
	row := s.db.QueryRowContext(context.Background(),
		`SELECT id, tenant_id, rule_id, rule_name, status, detail, started_at, ended_at
		   FROM automation_executions WHERE id=? AND tenant_id=?`, id, tenantID)
	e := scanAutomationExecution(row)
	if e == nil {
		return nil, false
	}
	return e, true
}

// ListAutomationExecutions 返回指定租户的执行记录列表（按 started_at DESC）。
// limit > 0 时加 LIMIT ?；limit <= 0 时返回全部。
func (s *SQLStore) ListAutomationExecutions(tenantID string, limit int) []*AutomationExecution {
	var (
		rows *sql.Rows
		err  error
	)
	if limit > 0 {
		rows, err = s.db.QueryContext(context.Background(),
			`SELECT id, tenant_id, rule_id, rule_name, status, detail, started_at, ended_at
			   FROM automation_executions WHERE tenant_id=? ORDER BY started_at DESC LIMIT ?`,
			tenantID, limit)
	} else {
		rows, err = s.db.QueryContext(context.Background(),
			`SELECT id, tenant_id, rule_id, rule_name, status, detail, started_at, ended_at
			   FROM automation_executions WHERE tenant_id=? ORDER BY started_at DESC`,
			tenantID)
	}
	if err != nil {
		log.Printf("[store] ListAutomationExecutions 查询失败 (tenant=%s limit=%d): %v", tenantID, limit, err)
		return []*AutomationExecution{}
	}
	defer rows.Close()
	out := make([]*AutomationExecution, 0)
	for rows.Next() {
		if e := scanAutomationExecution(rows); e != nil {
			out = append(out, e)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] ListAutomationExecutions 遍历失败: %v", err)
	}
	return out
}
