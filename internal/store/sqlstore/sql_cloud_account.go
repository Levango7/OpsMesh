// sql_cloud_account.go 实现 SQLStore 的 CloudAccountStore 子接口（Wave 2-B 云账号）。
//
// 表结构：cloud_accounts（id PK + tenant_id/workspace_id/name/provider/region/auth_type/
// credentials_ref/status/sync_status/last_sync_at/discovered_count/created_at/updated_at/
// created_by）。迁移文件 migrations/024_cloud_accounts.sql 幂等建表。
//
// 设计要点：
//   - last_sync_at 可空（未同步前为 NULL），扫描用 sql.NullTime；
//   - CreateCloudAccount 按 ID 幂等（INSERT ... ON DUPLICATE KEY UPDATE），tenant_id 仅插入
//     不更新，防 upsert 改写归属；
//   - UpdateCloudAccount 先 SELECT 校验存在，再按字段映射 UPDATE；UpdatedAt 始终刷新；
//   - DeleteCloudAccount / SyncCloudAccount 按 id 定位（ID 是全局唯一，控制面层已做租户隔离）；
//   - ListCloudAccounts 按 tenant_id 过滤，分页（LIMIT/OFFSET），返回总数；
//   - DB 不可用时返回 error，不 panic；
//   - ID 生成复用 model.RandCloudAccountID（"cloud-acct-" + 16 字节 hex）。
package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Levango7/OpsMesh/internal/store/model"
	"github.com/Levango7/OpsMesh/internal/store/storefail"
)

var (
	// errCloudAccountNotFound 云账号不存在。
	errCloudAccountNotFound = errors.New("cloud account not found")
	// errCloudAccountInvalidInput 非法入参。
	errCloudAccountInvalidInput = errors.New("invalid input")
)

// scanCloudAccount 从一行扫描出 *CloudAccount（last_sync_at 可空）。
// 列顺序：id, tenant_id, workspace_id, name, provider, region, auth_type, credentials_ref,
// status, sync_status, last_sync_at, discovered_count, created_at, updated_at, created_by。
// 无行或扫描失败返回 nil。
func scanCloudAccount(row rowScanner) *CloudAccount {
	var a CloudAccount
	var lastSyncAt sql.NullTime
	var createdAt, updatedAt time.Time
	if err := row.Scan(&a.ID, &a.TenantID, &a.WorkspaceID, &a.Name, &a.Provider, &a.Region,
		&a.AuthType, &a.CredentialsRef, &a.Status, &a.SyncStatus, &lastSyncAt,
		&a.DiscoveredCount, &createdAt, &updatedAt, &a.CreatedBy); err != nil {
		return nil
	}
	if lastSyncAt.Valid {
		a.LastSyncAt = lastSyncAt.Time
	}
	a.CreatedAt = createdAt
	a.UpdatedAt = updatedAt
	return &a
}

// CreateCloudAccount 创建云账号（按 ID 幂等；ID 为空时分配随机 ID）。
//
// 行为：
//   - acct == nil 返回 error；
//   - TenantID 为空时归一为 default；
//   - ID 为空时分配随机 ID；
//   - CreatedAt 为零值时填当前时间；
//   - Status 为空时默认 "active"；
//   - SyncStatus 为空时默认 "pending";
//   - UpdatedAt 始终刷新为当前时间；
//   - INSERT ... ON DUPLICATE KEY UPDATE 实现 upsert，tenant_id 仅插入不更新。
func (s *SQLStore) CreateCloudAccount(ctx context.Context, acct *CloudAccount) error {
	if acct == nil {
		return errCloudAccountInvalidInput
	}
	if acct.TenantID == "" {
		acct.TenantID = "default"
	}
	now := time.Now().UTC()
	if acct.ID == "" {
		acct.ID = model.RandCloudAccountID()
	}
	if acct.CreatedAt.IsZero() {
		acct.CreatedAt = now
	}
	if acct.Status == "" {
		acct.Status = "active"
	}
	if acct.SyncStatus == "" {
		acct.SyncStatus = "pending"
	}
	acct.UpdatedAt = now
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO cloud_accounts (id, tenant_id, workspace_id, name, provider, region,
		 auth_type, credentials_ref, status, sync_status, last_sync_at, discovered_count,
		 created_at, updated_at, created_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE workspace_id=VALUES(workspace_id), name=VALUES(name),
		 provider=VALUES(provider), region=VALUES(region), auth_type=VALUES(auth_type),
		 credentials_ref=VALUES(credentials_ref), status=VALUES(status),
		 sync_status=VALUES(sync_status), last_sync_at=VALUES(last_sync_at),
		 discovered_count=VALUES(discovered_count), updated_at=VALUES(updated_at),
		 created_by=VALUES(created_by)`,
		acct.ID, acct.TenantID, acct.WorkspaceID, acct.Name, acct.Provider, acct.Region,
		acct.AuthType, acct.CredentialsRef, acct.Status, acct.SyncStatus,
		lastSyncAtValue(acct.LastSyncAt), acct.DiscoveredCount, acct.CreatedAt,
		acct.UpdatedAt, acct.CreatedBy); err != nil {
		storefail.Record("[store] CreateCloudAccount 插入失败 (tenant=%s account=%s): %v",
			acct.TenantID, acct.ID, err)
		return err
	}
	return nil
}

// GetCloudAccount 按 ID 返回单个云账号（不存在返回 (nil, nil)）。
func (s *SQLStore) GetCloudAccount(ctx context.Context, id string) (*CloudAccount, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, workspace_id, name, provider, region, auth_type, credentials_ref,
		 status, sync_status, last_sync_at, discovered_count, created_at, updated_at, created_by
		  FROM cloud_accounts WHERE id=?`, id)
	return scanCloudAccount(row), nil
}

// ListCloudAccounts 返回指定租户的云账号列表（按 tenant_id 隔离，分页，总数）。
func (s *SQLStore) ListCloudAccounts(ctx context.Context, tenantID string, offset, limit int) ([]*CloudAccount, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM cloud_accounts WHERE tenant_id=?`, tenantID).Scan(&total); err != nil {
		storefail.Record("[store] ListCloudAccounts 计数失败 (tenant=%s): %v", tenantID, err)
		return nil, 0, err
	}
	q := `SELECT id, tenant_id, workspace_id, name, provider, region, auth_type, credentials_ref,
		status, sync_status, last_sync_at, discovered_count, created_at, updated_at, created_by
	  FROM cloud_accounts WHERE tenant_id=? ORDER BY created_at DESC`
	args := []interface{}{tenantID}
	if offset > 0 {
		q += ` LIMIT ? OFFSET ?`
		args = append(args, limit, offset)
	} else if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		storefail.Record("[store] ListCloudAccounts 查询失败 (tenant=%s): %v", tenantID, err)
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]*CloudAccount, 0)
	for rows.Next() {
		if a := scanCloudAccount(rows); a != nil {
			out = append(out, a)
		}
	}
	if err := rows.Err(); err != nil {
		storefail.Record("[store] ListCloudAccounts 遍历失败: %v", err)
		return nil, 0, err
	}
	return out, total, nil
}

// UpdateCloudAccount 按 ID 部分更新云账号（updates 为字段->值映射）。
//
// 行为：
//   - 先 SELECT 校验存在，不存在返回 error；
//   - CreatedAt / TenantID / ID 不可改（保留原值）；
//   - UpdatedAt 始终刷新为当前时间；
//   - 只更新 updates 中出现的字段（动态拼接 SET 子句）。
func (s *SQLStore) UpdateCloudAccount(ctx context.Context, id string, updates map[string]interface{}) error {
	if id == "" {
		return errCloudAccountInvalidInput
	}
	if len(updates) == 0 {
		return nil
	}
	acct, err := s.GetCloudAccount(ctx, id)
	if err != nil || acct == nil {
		return errCloudAccountNotFound
	}
	now := time.Now().UTC()
	sets := []string{"updated_at=?"}
	args := []interface{}{now}
	fieldCols := map[string]string{
		"workspace_id": "workspace_id", "name": "name", "provider": "provider",
		"region": "region", "auth_type": "auth_type", "credentials_ref": "credentials_ref",
		"status": "status", "sync_status": "sync_status", "discovered_count": "discovered_count",
		"created_by": "created_by",
	}
	for k, v := range updates {
		col, ok := fieldCols[k]
		if !ok {
			continue // 忽略非法字段，防越权/注入
		}
		sets = append(sets, col+"=?")
		args = append(args, v)
	}
	args = append(args, id)
	q := `UPDATE cloud_accounts SET ` + joinSets(sets) + ` WHERE id=?`
	if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
		storefail.Record("[store] UpdateCloudAccount 更新失败 (account=%s): %v", id, err)
		return err
	}
	return nil
}

// DeleteCloudAccount 按 ID 删除云账号，返回是否删除成功（不存在返回 error）。
func (s *SQLStore) DeleteCloudAccount(ctx context.Context, id string) error {
	if id == "" {
		return errCloudAccountInvalidInput
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM cloud_accounts WHERE id=?`, id)
	if err != nil {
		storefail.Record("[store] DeleteCloudAccount 删除失败 (account=%s): %v", id, err)
		return err
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		storefail.Record("[store] DeleteCloudAccount RowsAffected 失败 (account=%s): %v", id, rowsErr)
		return rowsErr
	}
	if n == 0 {
		return errCloudAccountNotFound
	}
	return nil
}

// SyncCloudAccount 回写同步状态（sync_status/discovered_count/last_sync_at/updated_at）。
// 不存在返回 error。
func (s *SQLStore) SyncCloudAccount(ctx context.Context, id string, syncStatus string, discoveredCount int) error {
	if id == "" {
		return errCloudAccountInvalidInput
	}
	acct, err := s.GetCloudAccount(ctx, id)
	if err != nil || acct == nil {
		return errCloudAccountNotFound
	}
	now := time.Now().UTC()
	var lastSyncAt interface{}
	if syncStatus == "synced" {
		lastSyncAt = now
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE cloud_accounts SET sync_status=?, discovered_count=?, last_sync_at=?,
		 updated_at=? WHERE id=?`,
		syncStatus, discoveredCount, lastSyncAt, now, id); err != nil {
		storefail.Record("[store] SyncCloudAccount 更新失败 (account=%s): %v", id, err)
		return err
	}
	return nil
}

// lastSyncAtValue 将 time.Time 转为可写入 last_sync_at 的值（零值 → nil）。
func lastSyncAtValue(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t
}

// joinSets 用逗号连接 SET 子句片段。
func joinSets(sets []string) string {
	out := ""
	for i, s := range sets {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}
