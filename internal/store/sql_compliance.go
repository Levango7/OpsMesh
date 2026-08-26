package store

// sql_compliance.go 实现 SQLStore 的 ComplianceStore 子接口（Phase 3 安全合规，生产就绪）。
//
// 表结构：compliance_reports（id PK + tenant_id + device_id + results JSON + score + created_at）。
// 迁移文件 migrations/012_p3_backup_compliance.sql 幂等建表。
//
// 设计要点（与 sql_pipeline.go 风格一致）：
//   - results 为 JSON 文本列（[]ComplianceResult 数组），应用层 encoding/json 序列化/反序列化；
//   - 无 updated_at（只有 created_at，报告不可改，重新扫描生成新 ID）；
//   - SaveReport 用 INSERT ... ON DUPLICATE KEY UPDATE 实现幂等 upsert（按 id 唯一）；
//   - Get/Delete 均 WHERE id=? AND tenant_id=? 实现租户隔离；
//   - ListReports 按 created_at DESC 返回；
//   - ID 生成复用 memory_compliance.go 的 randComplianceID()（前缀 compliance-）；
//   - DB 不可用时返回零值（nil/false/空 slice），不 panic，与 SQLStore 其他方法一致。

import (
	"context"
	"encoding/json"
	"log"
	"time"
)

// complianceReportColumns 是 compliance_reports 表的查询列清单（与 scanComplianceReport 顺序一致）。
const complianceReportColumns = `id, tenant_id, device_id, results, score, created_at`

// scanComplianceReport 从一行扫描出 *ComplianceReport（results 为 JSON 文本列）。
// 列顺序：id, tenant_id, device_id, results, score, created_at。
// 无行或扫描失败返回 nil（含 sql.ErrNoRows，由调用方解释为不存在）。
// results 反序列化失败不致命：保留空 Results，避免单条坏数据让整个 List 崩。
func scanComplianceReport(row rowScanner) *ComplianceReport {
	var r ComplianceReport
	var resultsJSON []byte
	var createdAt time.Time
	if err := row.Scan(&r.ID, &r.TenantID, &r.DeviceID, &resultsJSON, &r.Score, &createdAt); err != nil {
		return nil
	}
	r.CreatedAt = createdAt
	if len(resultsJSON) > 0 {
		_ = json.Unmarshal(resultsJSON, &r.Results)
	}
	return &r
}

// marshalComplianceResults 将 []ComplianceResult 序列化为 JSON 字节串（nil 时返回 nil）。
func marshalComplianceResults(results []ComplianceResult) []byte {
	if results == nil {
		return nil
	}
	b, err := json.Marshal(results)
	if err != nil {
		log.Printf("[store] marshalComplianceResults 失败: %v", err)
		return nil
	}
	return b
}

// SaveReport 保存合规报告（ID 为空时分配随机 ID）。
//
// 行为与 MemoryStore.SaveReport 一致：
//   - r==nil 返回 nil；
//   - 空租户归一为 default；
//   - ID 为空时由 randComplianceID() 分配；
//   - CreatedAt 零值填 now；
//   - INSERT ... ON DUPLICATE KEY UPDATE 实现幂等 upsert（按 id 唯一）；
//   - 无 updated_at（报告不可改，重新扫描生成新 ID）。
func (s *SQLStore) SaveReport(tenantID string, r *ComplianceReport) *ComplianceReport {
	if r == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	r.TenantID = tenantID
	now := time.Now().UTC()
	if r.ID == "" {
		r.ID = randComplianceID()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	resultsJSON := marshalComplianceResults(r.Results)
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO compliance_reports (id, tenant_id, device_id, results, score, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE device_id=VALUES(device_id), results=VALUES(results), score=VALUES(score)`,
		r.ID, r.TenantID, r.DeviceID, resultsJSON, r.Score, r.CreatedAt); err != nil {
		log.Printf("[store] SaveReport 插入失败 (tenant=%s id=%s): %v", tenantID, r.ID, err)
		return nil
	}
	return r
}

// GetReport 按 (tenantID, id) 返回单个合规报告。不存在返回 (nil, false)。
func (s *SQLStore) GetReport(tenantID, id string) (*ComplianceReport, bool) {
	row := s.db.QueryRowContext(context.Background(),
		`SELECT `+complianceReportColumns+` FROM compliance_reports WHERE id=? AND tenant_id=?`, id, tenantID)
	r := scanComplianceReport(row)
	if r == nil {
		return nil, false
	}
	return r, true
}

// ListReports 返回指定租户的全部合规报告（按 created_at DESC）。
func (s *SQLStore) ListReports(tenantID string) []*ComplianceReport {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT `+complianceReportColumns+` FROM compliance_reports WHERE tenant_id=? ORDER BY created_at DESC`, tenantID)
	if err != nil {
		log.Printf("[store] ListReports 查询失败 (tenant=%s): %v", tenantID, err)
		return []*ComplianceReport{}
	}
	defer rows.Close()
	out := make([]*ComplianceReport, 0)
	for rows.Next() {
		if r := scanComplianceReport(rows); r != nil {
			out = append(out, r)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] ListReports 遍历失败: %v", err)
	}
	return out
}

// DeleteReport 删除合规报告，返回是否删除成功（不存在或租户不匹配返回 false）。
func (s *SQLStore) DeleteReport(tenantID, id string) bool {
	res, err := s.db.ExecContext(context.Background(),
		`DELETE FROM compliance_reports WHERE id=? AND tenant_id=?`, id, tenantID)
	if err != nil {
		log.Printf("[store] DeleteReport 失败 (tenant=%s id=%s): %v", tenantID, id, err)
		return false
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		log.Printf("[store] DeleteReport RowsAffected 失败 (tenant=%s id=%s): %v", tenantID, id, rowsErr)
		return false
	}
	return n > 0
}
