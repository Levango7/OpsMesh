// sql_tenant.go 实现 SQLStore 的 TenantStore 子接口（Phase 6 租户管理，生产就绪）。
//
// 表结构：tenants（id PK + name UNIQUE + display_name + status + quota JSON +
// usage JSON + created_at + updated_at）。迁移文件
// migrations/015_p6_tenant_apikey_plugin_billing.sql 幂等建表。
//
// 设计要点（与 sql_webhook.go / sql_secret.go 风格一致）：
//   - 全局共享：tenants 表无 tenant_id 列，本身就是租户，所有方法不带 tenant_id 条件；
//   - JSON 列：quota（TenantQuota）+ usage（ResourceUsage），用 encoding/json 序列化为 TEXT；
//     空值存空串，读取时空串跳过 Unmarshal 得零值；
//   - name 唯一约束（UNIQUE KEY uq_tenants_name，URL-safe 租户标识唯一）；
//   - CreateTenant 按 ID 幂等（INSERT ... ON DUPLICATE KEY UPDATE），不更新 created_at；
//   - ListTenants 按创建时间升序（与 memory 一致）；
//   - UpdateTenant 先 SELECT 校验存在，再 UPDATE，保留原 CreatedAt；ID 不可改；
//   - DB 不可用时返回零值（nil/false/空 slice），不 panic；
//   - ID 生成复用 memory_tenant.go 的 randTenantID（"tenant-" + 16 字节 hex）。
package store

import (
	"context"
	"encoding/json"
	"log"
	"time"
)

// scanTenant 从一行扫描出 *Tenant（quota / usage 为 JSON 文本列）。
// 列顺序：id, name, display_name, status, quota, usage, created_at, updated_at。
// 无行或扫描失败返回 nil。
func scanTenant(row rowScanner) *Tenant {
	var t Tenant
	var quotaJSON, usageJSON string
	var createdAt, updatedAt time.Time
	if err := row.Scan(&t.ID, &t.Name, &t.DisplayName, &t.Status, &quotaJSON, &usageJSON,
		&createdAt, &updatedAt); err != nil {
		return nil
	}
	t.CreatedAt = createdAt
	t.UpdatedAt = updatedAt
	if quotaJSON != "" {
		if err := json.Unmarshal([]byte(quotaJSON), &t.Quota); err != nil {
			log.Printf("[store] scanTenant 解析 quota JSON 失败 (tenant=%s): %v", t.ID, err)
		}
	}
	if usageJSON != "" {
		if err := json.Unmarshal([]byte(usageJSON), &t.Usage); err != nil {
			log.Printf("[store] scanTenant 解析 usage JSON 失败 (tenant=%s): %v", t.ID, err)
		}
	}
	return &t
}

// marshalTenantQuota 将 Quota 序列化为 JSON 文本（零值存空串）。
func marshalTenantQuota(q TenantQuota) string {
	b, err := json.Marshal(q)
	if err != nil {
		return ""
	}
	s := string(b)
	if s == `{"maxDevices":0,"maxTasks":0,"maxActiveTasks":0,"maxAlerts":0,"maxAgents":0,"maxWebhooks":0,"maxAPIKeys":0}` {
		return ""
	}
	return s
}

// marshalResourceUsage 将 Usage 序列化为 JSON 文本（零值存空串）。
func marshalResourceUsage(u ResourceUsage) string {
	b, err := json.Marshal(u)
	if err != nil {
		return ""
	}
	s := string(b)
	if s == `{"devices":0,"tasks":0,"activeTasks":0,"alerts":0,"agents":0,"webhooks":0,"apiKeys":0}` {
		return ""
	}
	return s
}

// CreateTenant 创建租户（按 ID 幂等；ID 为空时分配随机 ID）。
//
// 行为：
//   - tenant == nil 返回 nil；
//   - ID 为空时分配随机 ID（新建场景）；
//   - Status 为空时归一为 active；
//   - CreatedAt 为零值时填当前时间（新建场景）；
//   - UpdatedAt 始终刷新为当前时间；
//   - INSERT ... ON DUPLICATE KEY UPDATE 实现 upsert（按 id 幂等），
//     created_at 仅插入不更新，防 upsert 改写创建时间；
//   - DB 失败时 log.Printf + 返回 nil。
func (s *SQLStore) CreateTenant(tenant *Tenant) *Tenant {
	if tenant == nil {
		return nil
	}
	if tenant.ID == "" {
		tenant.ID = randTenantID()
	}
	if tenant.Status == "" {
		tenant.Status = TenantStatusActive
	}
	now := time.Now().UTC()
	if tenant.CreatedAt.IsZero() {
		tenant.CreatedAt = now
	}
	tenant.UpdatedAt = now
	quotaJSON := marshalTenantQuota(tenant.Quota)
	usageJSON := marshalResourceUsage(tenant.Usage)
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO tenants (id, name, display_name, status, quota, usage, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE name=VALUES(name), display_name=VALUES(display_name),
		 status=VALUES(status), quota=VALUES(quota), usage=VALUES(usage), updated_at=VALUES(updated_at)`,
		tenant.ID, tenant.Name, tenant.DisplayName, string(tenant.Status),
		quotaJSON, usageJSON, tenant.CreatedAt, tenant.UpdatedAt); err != nil {
		log.Printf("[store] CreateTenant 插入失败 (tenant=%s): %v", tenant.ID, err)
		return nil
	}
	return cloneTenant(tenant)
}

// GetTenant 按 ID 返回单个租户（深拷贝；不存在返回 (nil, false)）。
func (s *SQLStore) GetTenant(id string) (*Tenant, bool) {
	row := s.db.QueryRowContext(context.Background(),
		`SELECT id, name, display_name, status, quota, usage, created_at, updated_at
		  FROM tenants WHERE id=?`, id)
	t := scanTenant(row)
	if t == nil {
		return nil, false
	}
	return t, true
}

// UpdateTenant 更新租户（按 tenant.ID 定位）。
//
// 行为：
//   - tenant == nil 或 ID 为空返回 (nil, false)；
//   - 先 GetTenant 校验存在，不存在返回 (nil, false)；
//   - CreatedAt 不可改（保留原值）；ID 不可改；
//   - UpdatedAt 始终刷新为当前时间；
//   - 返回更新后的 Tenant（深拷贝）。
func (s *SQLStore) UpdateTenant(tenant *Tenant) (*Tenant, bool) {
	if tenant == nil || tenant.ID == "" {
		return nil, false
	}
	// 先 SELECT 校验存在。
	existing, ok := s.GetTenant(tenant.ID)
	if !ok {
		return nil, false
	}
	// 保留不可改字段。
	tenant.ID = existing.ID
	tenant.CreatedAt = existing.CreatedAt
	tenant.UpdatedAt = time.Now().UTC()
	quotaJSON := marshalTenantQuota(tenant.Quota)
	usageJSON := marshalResourceUsage(tenant.Usage)
	if _, err := s.db.ExecContext(context.Background(),
		`UPDATE tenants SET name=?, display_name=?, status=?, quota=?, usage=?, updated_at=?
		 WHERE id=?`,
		tenant.Name, tenant.DisplayName, string(tenant.Status), quotaJSON, usageJSON,
		tenant.UpdatedAt, tenant.ID); err != nil {
		log.Printf("[store] UpdateTenant 更新失败 (tenant=%s): %v", tenant.ID, err)
		return nil, false
	}
	return cloneTenant(tenant), true
}

// ListTenants 返回全部租户（按创建时间升序；深拷贝）。
func (s *SQLStore) ListTenants() []*Tenant {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT id, name, display_name, status, quota, usage, created_at, updated_at
		  FROM tenants ORDER BY created_at ASC`)
	if err != nil {
		log.Printf("[store] ListTenants 查询失败: %v", err)
		return []*Tenant{}
	}
	defer rows.Close()
	out := make([]*Tenant, 0)
	for rows.Next() {
		if t := scanTenant(rows); t != nil {
			out = append(out, t)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] ListTenants 遍历失败: %v", err)
	}
	return out
}

// DeleteTenant 按 ID 删除租户。不存在返回 false。
func (s *SQLStore) DeleteTenant(id string) bool {
	res, err := s.db.ExecContext(context.Background(),
		`DELETE FROM tenants WHERE id=?`, id)
	if err != nil {
		log.Printf("[store] DeleteTenant 失败 (tenant=%s): %v", id, err)
		return false
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		log.Printf("[store] DeleteTenant RowsAffected 失败 (tenant=%s): %v", id, rowsErr)
		return false
	}
	return n > 0
}
