// sql_abac_policy.go SQLStore 的 ABACPolicyStore 实现（批次47：ABAC 策略持久化）。
//
// 表结构：abac_policies（migrations/035_abac_policies.sql），主键 (tenant_id, id)。
// 写入采用 INSERT ... ON DUPLICATE KEY UPDATE version=version+1：已存在即版本自增，
// created_at 保持不变；写后按主键回读，返回库内真实版本与时间戳（不靠 RowsAffected 猜）。
//
// 风格与 sql_gateway_route.go 一致：DB 不可用/无行返回零值；写失败经 storefail 记录；
// 时间统一 time.Now().UTC()，查询一律带 tenant_id（tenantID=="" 才是跨租户管理视图）。
package sqlstore

import (
	"context"
	"time"

	"github.com/Levango7/OpsMesh/internal/store/model"
	"github.com/Levango7/OpsMesh/internal/store/storefail"
)

// abacPolicyColumns 读路径统一列清单；列数与 scanABACPolicyRecord 的 Scan 目标数一一对应。
const abacPolicyColumns = "`tenant_id`, `id`, `subject`, `resource`, `action`, `effect`, `version`, `created_at`, `updated_at`"

// scanABACPolicyRecord 从一行扫描出记录（时间列为 DATETIME，DSN 已启用 parseTime）。
func scanABACPolicyRecord(row rowScanner) (model.ABACPolicyRecord, error) {
	var r model.ABACPolicyRecord
	err := row.Scan(&r.TenantID, &r.ID, &r.Subject, &r.Resource, &r.Action, &r.Effect,
		&r.Version, &r.CreatedAt, &r.UpdatedAt)
	return r, err
}

// ListABACPolicyRecords 返回策略记录：tenantID 为空返回全部租户。
// 确定性排序：tenant_id 升序，再按 id 升序（与控制面刷新比对口径一致）。
func (s *SQLStore) ListABACPolicyRecords(tenantID string) []model.ABACPolicyRecord {
	if s.db == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	query := "SELECT " + abacPolicyColumns + " FROM `abac_policies`"
	args := make([]interface{}, 0, 1)
	if tenantID != "" {
		query += " WHERE `tenant_id`=?"
		args = append(args, tenantID)
	}
	query += " ORDER BY `tenant_id` ASC, `id` ASC"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		storefail.Record("[store] ListABACPolicyRecords 查询失败: %v", err)
		return nil
	}
	defer rows.Close()
	out := make([]model.ABACPolicyRecord, 0)
	for rows.Next() {
		r, err := scanABACPolicyRecord(rows)
		if err != nil {
			storefail.Record("[store] ListABACPolicyRecords 扫描失败: %v", err)
			continue
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		storefail.Record("[store] ListABACPolicyRecords 迭代失败: %v", err)
	}
	return out
}

// UpsertABACPolicyRecord 按 (TenantID, ID) 幂等写穿。已存在则 version+1，否则 version=1。
// 返回落库后的记录与是否成功；失败（DB 不可用/执行失败/回读失败）返回 false。
func (s *SQLStore) UpsertABACPolicyRecord(p model.ABACPolicyRecord) (model.ABACPolicyRecord, bool) {
	if p.ID == "" || s.db == nil {
		return p, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	// 方言特例构建器见 upsert_special.go：MySQL 保留 `version`=`version`+1；
	// PG 输出 "version" = "abac_policies"."version" + 1，conflict 为
	// ("tenant_id", "id")。参数顺序与个数不变（8 占位）。
	if _, err := s.db.ExecContext(ctx, upsertABACPolicy(s.dialect),
		p.TenantID, p.ID, p.Subject, p.Resource, p.Action, p.Effect, p.CreatedAt, now); err != nil {
		storefail.Record("[store] UpsertABACPolicyRecord 失败 %s/%s: %v", p.TenantID, p.ID, err)
		return p, false
	}
	stored, err := scanABACPolicyRecord(s.db.QueryRowContext(ctx,
		"SELECT "+abacPolicyColumns+" FROM `abac_policies` WHERE `tenant_id`=? AND `id`=?",
		p.TenantID, p.ID))
	if err != nil {
		storefail.Record("[store] UpsertABACPolicyRecord 回读失败 %s/%s: %v", p.TenantID, p.ID, err)
		return p, false
	}
	return stored, true
}

// DeleteABACPolicyRecord 按 (tenantID, id) 删除。返回是否删除成功（不存在返回 false）。
func (s *SQLStore) DeleteABACPolicyRecord(tenantID, id string) bool {
	if s.db == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := s.db.ExecContext(ctx,
		"DELETE FROM `abac_policies` WHERE `tenant_id`=? AND `id`=?", tenantID, id)
	if err != nil {
		storefail.Record("[store] DeleteABACPolicyRecord 失败 %s/%s: %v", tenantID, id, err)
		return false
	}
	n, err := res.RowsAffected()
	if err != nil {
		storefail.Record("[store] DeleteABACPolicyRecord RowsAffected 失败: %v", err)
		return false
	}
	return n > 0
}
