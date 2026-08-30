package store

// sql_pipeline.go 实现 SQLStore 的 PipelineStore 子接口（Phase 2 CI/CD 流水线，生产就绪）。
//
// 表结构：
//   - pipeline_templates（id PK + tenant_id + name + description + type + yaml +
//     agent_id + parameters JSON + created_at + updated_at）；
//   - pipeline_runs（id PK + tenant_id + template_id + template_name + status +
//     parameters JSON + logs + started_at 可空 + finished_at 可空 + created_at）。
// 迁移文件 migrations/011_p2_argocd_pipeline_traffic.sql 幂等建表；
// migrations/016_g2_pipeline_agentid.sql 为 pipeline_templates 补齐 agent_id 列。
//
// 设计要点（与 sql_k8s.go / sql_secret.go 风格一致）：
//   - 两张表分别承载模板与运行记录；
//   - JSON 列：pipeline_templates.parameters（[]PipelineParam）；
//     pipeline_runs.parameters（map[string]string），用 encoding/json 序列化为 TEXT；
//   - 可空时间：pipeline_runs.started_at / finished_at 用 sql.NullTime 处理；
//   - CreateTemplate / CreateRun 用 INSERT ... ON DUPLICATE KEY UPDATE 实现幂等 upsert；
//   - Get/Delete 均 WHERE id=? AND tenant_id=? 实现租户隔离；
//   - ListTemplates 按 created_at DESC；ListRuns 按 tenant_id + 可选 template_id 过滤后 created_at DESC；
//   - UpdateRun 先 SELECT 校验存在 + 租户归属，不存在返回 (nil, false)；
//   - ID 生成复用 memory_pipeline.go 的 randPipelineID()（前缀 pipeline-）+ randRunID()（前缀 run-）；
//   - DB 不可用时返回零值（nil/false/空 slice），不 panic。

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"time"
)

// scanPipelineTemplate 从一行扫描出 *PipelineTemplate。
// 列顺序：id, tenant_id, name, description, type, yaml, agent_id, parameters, created_at, updated_at。
func scanPipelineTemplate(row rowScanner) *PipelineTemplate {
	var t PipelineTemplate
	var paramsJSON []byte
	var createdAt, updatedAt time.Time
	if err := row.Scan(
		&t.ID, &t.TenantID, &t.Name, &t.Description, &t.Type, &t.YAML, &t.AgentID,
		&paramsJSON, &createdAt, &updatedAt,
	); err != nil {
		return nil
	}
	t.CreatedAt = createdAt
	t.UpdatedAt = updatedAt
	if len(paramsJSON) > 0 {
		// 反序列化失败不致命：保留空 Parameters，避免单条坏数据让整个 List 崩，但必须留痕。
		if err := json.Unmarshal(paramsJSON, &t.Parameters); err != nil {
			log.Printf("[store] scanPipelineTemplate 反序列化 parameters 失败（保留空 Parameters 继续）: %v", err)
		}
	}
	return &t
}

// pipelineTemplateColumns 是 pipeline_templates 表的查询列清单（与 scanPipelineTemplate 顺序一致）。
// agent_id 列由 migrations/016_g2_pipeline_agentid.sql 补齐（旧库升级后存在）。
const pipelineTemplateColumns = `id, tenant_id, name, description, type, yaml, agent_id, parameters, created_at, updated_at`

// scanPipelineRun 从一行扫描出 *PipelineRun。
// 列顺序：id, tenant_id, template_id, template_name, status, parameters, logs,
// started_at, finished_at, created_at。
func scanPipelineRun(row rowScanner) *PipelineRun {
	var r PipelineRun
	var paramsJSON []byte
	var startedAt, finishedAt sql.NullTime
	var createdAt time.Time
	if err := row.Scan(
		&r.ID, &r.TenantID, &r.TemplateID, &r.TemplateName, &r.Status,
		&paramsJSON, &r.Logs, &startedAt, &finishedAt, &createdAt,
	); err != nil {
		return nil
	}
	r.CreatedAt = createdAt
	if startedAt.Valid {
		st := startedAt.Time
		r.StartedAt = &st
	}
	if finishedAt.Valid {
		ft := finishedAt.Time
		r.FinishedAt = &ft
	}
	if len(paramsJSON) > 0 {
		// 反序列化失败不致命：保留空 Parameters，避免单条坏数据让整个 List 崩，但必须留痕。
		if err := json.Unmarshal(paramsJSON, &r.Parameters); err != nil {
			log.Printf("[store] scanPipelineRun 反序列化 parameters 失败（保留空 Parameters 继续）: %v", err)
		}
	}
	return &r
}

// pipelineRunColumns 是 pipeline_runs 表的查询列清单（与 scanPipelineRun 顺序一致）。
const pipelineRunColumns = `id, tenant_id, template_id, template_name, status, parameters, logs,
 started_at, finished_at, created_at`

// marshalPipelineParams 将 []PipelineParam 序列化为 JSON 字节串（nil 时返回 nil）。
func marshalPipelineParams(params []PipelineParam) []byte {
	if params == nil {
		return nil
	}
	b, err := json.Marshal(params)
	if err != nil {
		log.Printf("[store] marshalPipelineParams 失败: %v", err)
		return nil
	}
	return b
}

// marshalRunParams 将 map[string]string 序列化为 JSON 字节串（nil 时返回 nil）。
func marshalRunParams(params map[string]string) []byte {
	if params == nil {
		return nil
	}
	b, err := json.Marshal(params)
	if err != nil {
		log.Printf("[store] marshalRunParams 失败: %v", err)
		return nil
	}
	return b
}

// CreateTemplate 创建流水线模板（ID 为空时分配随机 ID）。
//
// 行为与 MemoryStore.CreateTemplate 一致：
//   - 空租户归一为 default；
//   - ID 为空时由 randPipelineID() 分配；
//   - CreatedAt 零值填 now；UpdatedAt 始终刷新为 now；
//   - INSERT ... ON DUPLICATE KEY UPDATE 实现幂等 upsert（按 id 唯一）。
func (s *SQLStore) CreateTemplate(tenantID string, t *PipelineTemplate) *PipelineTemplate {
	if t == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	t.TenantID = tenantID
	now := time.Now().UTC()
	if t.ID == "" {
		t.ID = randPipelineID()
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	paramsJSON := marshalPipelineParams(t.Parameters)
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO pipeline_templates (id, tenant_id, name, description, type, yaml, agent_id, parameters, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE name=VALUES(name), description=VALUES(description), type=VALUES(type),
		 yaml=VALUES(yaml), agent_id=VALUES(agent_id), parameters=VALUES(parameters), updated_at=VALUES(updated_at)`,
		t.ID, t.TenantID, t.Name, t.Description, t.Type, t.YAML, t.AgentID, paramsJSON, t.CreatedAt, t.UpdatedAt); err != nil {
		log.Printf("[store] CreateTemplate 插入失败 (tenant=%s id=%s): %v", tenantID, t.ID, err)
		return nil
	}
	return t
}

// GetTemplate 按 (tenantID, id) 返回单个模板。不存在返回 (nil, false)。
func (s *SQLStore) GetTemplate(tenantID, id string) (*PipelineTemplate, bool) {
	row := s.db.QueryRowContext(context.Background(),
		`SELECT `+pipelineTemplateColumns+` FROM pipeline_templates WHERE id=? AND tenant_id=?`, id, tenantID)
	t := scanPipelineTemplate(row)
	if t == nil {
		return nil, false
	}
	return t, true
}

// ListTemplates 返回指定租户的全部模板（按 created_at DESC）。
func (s *SQLStore) ListTemplates(tenantID string) []*PipelineTemplate {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT `+pipelineTemplateColumns+` FROM pipeline_templates WHERE tenant_id=? ORDER BY created_at DESC`, tenantID)
	if err != nil {
		log.Printf("[store] ListTemplates 查询失败 (tenant=%s): %v", tenantID, err)
		return []*PipelineTemplate{}
	}
	defer rows.Close()
	out := make([]*PipelineTemplate, 0)
	for rows.Next() {
		if t := scanPipelineTemplate(rows); t != nil {
			out = append(out, t)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] ListTemplates 遍历失败: %v", err)
	}
	return out
}

// DeleteTemplate 删除模板，返回是否删除成功（不存在或租户不匹配返回 false）。
func (s *SQLStore) DeleteTemplate(tenantID, id string) bool {
	res, err := s.db.ExecContext(context.Background(),
		`DELETE FROM pipeline_templates WHERE id=? AND tenant_id=?`, id, tenantID)
	if err != nil {
		log.Printf("[store] DeleteTemplate 失败 (tenant=%s id=%s): %v", tenantID, id, err)
		return false
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		log.Printf("[store] DeleteTemplate RowsAffected 失败 (tenant=%s id=%s): %v", tenantID, id, rowsErr)
		return false
	}
	return n > 0
}

// CreateRun 创建运行记录（ID 为空时分配随机 ID）。
//
// 行为与 MemoryStore.CreateRun 一致：
//   - 空租户归一为 default；
//   - ID 为空时由 randRunID() 分配；
//   - Status 空 → pending；CreatedAt 零值填 now；
//   - INSERT ... ON DUPLICATE KEY UPDATE 实现幂等 upsert（按 id 唯一）。
func (s *SQLStore) CreateRun(tenantID string, r *PipelineRun) *PipelineRun {
	if r == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	r.TenantID = tenantID
	now := time.Now().UTC()
	if r.ID == "" {
		r.ID = randRunID()
	}
	if r.Status == "" {
		r.Status = "pending"
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	paramsJSON := marshalRunParams(r.Parameters)
	var startedAt, finishedAt sql.NullTime
	if r.StartedAt != nil {
		startedAt = sql.NullTime{Time: *r.StartedAt, Valid: true}
	}
	if r.FinishedAt != nil {
		finishedAt = sql.NullTime{Time: *r.FinishedAt, Valid: true}
	}
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO pipeline_runs (id, tenant_id, template_id, template_name, status, parameters, logs,
		 started_at, finished_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE template_id=VALUES(template_id), template_name=VALUES(template_name),
		 status=VALUES(status), parameters=VALUES(parameters), logs=VALUES(logs),
		 started_at=VALUES(started_at), finished_at=VALUES(finished_at)`,
		r.ID, r.TenantID, r.TemplateID, r.TemplateName, r.Status, paramsJSON, r.Logs,
		startedAt, finishedAt, r.CreatedAt); err != nil {
		log.Printf("[store] CreateRun 插入失败 (tenant=%s id=%s): %v", tenantID, r.ID, err)
		return nil
	}
	return r
}

// GetRun 按 (tenantID, id) 返回单条运行记录。不存在返回 (nil, false)。
func (s *SQLStore) GetRun(tenantID, id string) (*PipelineRun, bool) {
	row := s.db.QueryRowContext(context.Background(),
		`SELECT `+pipelineRunColumns+` FROM pipeline_runs WHERE id=? AND tenant_id=?`, id, tenantID)
	r := scanPipelineRun(row)
	if r == nil {
		return nil, false
	}
	return r, true
}

// ListRuns 返回运行记录列表（按 tenant_id + 可选 template_id 过滤，created_at DESC）。
//
// templateID != "" 时加 AND template_id=?；空时返回该租户全部 Run。
func (s *SQLStore) ListRuns(tenantID string, templateID string) []*PipelineRun {
	q := `SELECT ` + pipelineRunColumns + ` FROM pipeline_runs WHERE tenant_id=?`
	args := []interface{}{tenantID}
	if templateID != "" {
		q += ` AND template_id=?`
		args = append(args, templateID)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := s.db.QueryContext(context.Background(), q, args...)
	if err != nil {
		log.Printf("[store] ListRuns 查询失败 (tenant=%s template=%s): %v", tenantID, templateID, err)
		return []*PipelineRun{}
	}
	defer rows.Close()
	out := make([]*PipelineRun, 0)
	for rows.Next() {
		if r := scanPipelineRun(rows); r != nil {
			out = append(out, r)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] ListRuns 遍历失败: %v", err)
	}
	return out
}

// UpdateRun 更新运行记录（按 r.ID 定位，校验 tenantID 归属）。
//
// 行为与 MemoryStore.UpdateRun 一致：
//   - r==nil 或 r.ID=="" 返回 (nil, false)；
//   - 先 SELECT 校验存在 + 租户归属，不存在返回 (nil, false)；
//   - CreatedAt 保留原值；
//   - TemplateID/TemplateName 入参为空时保留原值（运行记录创建后不可改关联模板）；
//   - StartedAt/FinishedAt 入参为 nil 时保留原值。
func (s *SQLStore) UpdateRun(tenantID string, r *PipelineRun) (*PipelineRun, bool) {
	if r == nil || r.ID == "" {
		return nil, false
	}
	existing, ok := s.GetRun(tenantID, r.ID)
	if !ok {
		return nil, false
	}
	r.TenantID = existing.TenantID
	r.CreatedAt = existing.CreatedAt
	// 保留原 TemplateID/TemplateName（运行记录创建后不可改关联模板）。
	if r.TemplateID == "" {
		r.TemplateID = existing.TemplateID
	}
	if r.TemplateName == "" {
		r.TemplateName = existing.TemplateName
	}
	// StartedAt/FinishedAt 入参为 nil 时保留原值。
	if r.StartedAt == nil {
		r.StartedAt = existing.StartedAt
	}
	if r.FinishedAt == nil {
		r.FinishedAt = existing.FinishedAt
	}
	paramsJSON := marshalRunParams(r.Parameters)
	var startedAt, finishedAt sql.NullTime
	if r.StartedAt != nil {
		startedAt = sql.NullTime{Time: *r.StartedAt, Valid: true}
	}
	if r.FinishedAt != nil {
		finishedAt = sql.NullTime{Time: *r.FinishedAt, Valid: true}
	}
	if _, err := s.db.ExecContext(context.Background(),
		`UPDATE pipeline_runs SET template_id=?, template_name=?, status=?, parameters=?, logs=?,
		 started_at=?, finished_at=? WHERE id=? AND tenant_id=?`,
		r.TemplateID, r.TemplateName, r.Status, paramsJSON, r.Logs,
		startedAt, finishedAt, r.ID, tenantID); err != nil {
		log.Printf("[store] UpdateRun 失败 (tenant=%s id=%s): %v", tenantID, r.ID, err)
		return nil, false
	}
	return r, true
}
