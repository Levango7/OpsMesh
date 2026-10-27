// sql_cloud_resource.go 实现 SQLStore 的 CloudResourceStore 子接口（Wave 2-B 云资源）。
//
// 表结构：cloud_resources（id PK + tenant_id/cloud_account_id/provider/region/
// resource_type/resource_id/resource_name/status/tags/private_ip/public_ip/
// created_at/updated_at）。迁移文件 migrations/024_cloud_accounts.sql 幂等建表。
//
// 设计要点：
//   - tags 为 JSON map 文本，空切片/空 map 存 "{}"；
//   - CreateCloudResource 按 ID 幂等（INSERT ... ON DUPLICATE KEY UPDATE）；
//   - ListCloudResources 按 tenant_id 过滤，可按 cloud_account_id / resource_type 附加过滤，
//     分页（LIMIT/OFFSET），返回总数；
//   - DeleteCloudResourcesByAccount 按 cloud_account_id 批量删除，返回删除条数；
//   - ID 生成复用 model.RandCloudResourceID（"cloud-res-" + 16 字节 hex）。
package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Levango7/OpsMesh/internal/store/model"
	"github.com/Levango7/OpsMesh/internal/store/storefail"
)

var (
	errCloudResourceInvalid = errors.New("invalid input")
)

// scanCloudResource 从一行扫描出 *CloudResource（tags 为 JSON 文本列）。
// 列顺序：id, tenant_id, cloud_account_id, provider, region, resource_type, resource_id,
// resource_name, status, tags, private_ip, public_ip, created_at, updated_at。
// 无行或扫描失败返回 nil。
func scanCloudResource(row rowScanner) *CloudResource {
	var r CloudResource
	var tagsJSON sql.NullString
	var createdAt, updatedAt time.Time
	if err := row.Scan(&r.ID, &r.TenantID, &r.CloudAccountID, &r.Provider, &r.Region,
		&r.ResourceType, &r.ResourceID, &r.ResourceName, &r.Status, &tagsJSON,
		&r.PrivateIP, &r.PublicIP, &createdAt, &updatedAt); err != nil {
		return nil
	}
	if tagsJSON.String != "" {
		if err := json.Unmarshal([]byte(tagsJSON.String), &map[string]string{}); err != nil {
			storefail.Record("[store] scanCloudResource 解析 tags JSON 失败 (resource=%s): %v", r.ID, err)
		}
	}
	r.CreatedAt = createdAt
	r.UpdatedAt = updatedAt
	return &r
}

// marshalTags 将资源的 Tags（JSON map 文本）保持原样；空串存 "{}"。
func marshalResourceTags(tags string) string {
	if tags == "" {
		return "{}"
	}
	return tags
}

// CreateCloudResource 创建云资源（按 ID 幂等；ID 为空时分配随机 ID）。
//
// 行为：
//   - res == nil 返回 error；
//   - TenantID 为空时归一为 default；
//   - ID 为空时分配随机 ID；
//   - Status 为空时默认 "unknown";
//   - Tags 为空时存 "{}"；
//   - CreatedAt/UpdatedAt 为零值时填当前时间；
//   - INSERT ... ON DUPLICATE KEY UPDATE 实现 upsert。
func (s *SQLStore) CreateCloudResource(ctx context.Context, res *CloudResource) error {
	if res == nil {
		return errCloudResourceInvalid
	}
	if res.TenantID == "" {
		res.TenantID = "default"
	}
	now := time.Now().UTC()
	if res.ID == "" {
		res.ID = model.RandCloudResourceID()
	}
	if res.CreatedAt.IsZero() {
		res.CreatedAt = now
	}
	if res.Status == "" {
		res.Status = "unknown"
	}
	res.UpdatedAt = now
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO cloud_resources (id, tenant_id, cloud_account_id, provider, region,
		 resource_type, resource_id, resource_name, status, tags, private_ip, public_ip,
		 created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE tenant_id=VALUES(tenant_id), cloud_account_id=VALUES(cloud_account_id),
		 provider=VALUES(provider), region=VALUES(region), resource_type=VALUES(resource_type),
		 resource_id=VALUES(resource_id), resource_name=VALUES(resource_name), status=VALUES(status),
		 tags=VALUES(tags), private_ip=VALUES(private_ip), public_ip=VALUES(public_ip),
		 updated_at=VALUES(updated_at)`,
		res.ID, res.TenantID, res.CloudAccountID, res.Provider, res.Region,
		res.ResourceType, res.ResourceID, res.ResourceName, res.Status,
		marshalResourceTags(res.Tags), res.PrivateIP, res.PublicIP,
		res.CreatedAt, res.UpdatedAt); err != nil {
		storefail.Record("[store] CreateCloudResource 插入失败 (tenant=%s account=%s resource=%s): %v",
			res.TenantID, res.CloudAccountID, res.ID, err)
		return err
	}
	return nil
}

// GetCloudResource 按 ID 返回单个云资源（不存在返回 (nil, error)）。
func (s *SQLStore) GetCloudResource(ctx context.Context, id string) (*CloudResource, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, cloud_account_id, provider, region, resource_type, resource_id,
		 resource_name, status, tags, private_ip, public_ip, created_at, updated_at
		  FROM cloud_resources WHERE id=?`, id)
	return scanCloudResource(row), nil
}

// ListCloudResources 返回指定租户的云资源列表（按 tenant_id 隔离，可按 accountID /
// resourceType 过滤，分页，总数）。
func (s *SQLStore) ListCloudResources(ctx context.Context, tenantID string, accountID string, resourceType string, offset, limit int) ([]*CloudResource, int, error) {
	var total int
	q := `SELECT COUNT(*) FROM cloud_resources WHERE tenant_id=?`
	args := []interface{}{tenantID}
	if accountID != "" {
		q += ` AND cloud_account_id=?`
		args = append(args, accountID)
	}
	if resourceType != "" {
		q += ` AND resource_type=?`
		args = append(args, resourceType)
	}
	if err := s.db.QueryRowContext(ctx, q, args...).Scan(&total); err != nil {
		storefail.Record("[store] ListCloudResources 计数失败 (tenant=%s): %v", tenantID, err)
		return nil, 0, err
	}
	q = `SELECT id, tenant_id, cloud_account_id, provider, region, resource_type, resource_id,
		resource_name, status, tags, private_ip, public_ip, created_at, updated_at
	  FROM cloud_resources WHERE tenant_id=?`
	args = []interface{}{tenantID}
	if accountID != "" {
		q += ` AND cloud_account_id=?`
		args = append(args, accountID)
	}
	if resourceType != "" {
		q += ` AND resource_type=?`
		args = append(args, resourceType)
	}
	q += ` ORDER BY created_at DESC`
	if offset > 0 {
		q += ` LIMIT ? OFFSET ?`
		args = append(args, limit, offset)
	} else if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		storefail.Record("[store] ListCloudResources 查询失败 (tenant=%s): %v", tenantID, err)
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]*CloudResource, 0)
	for rows.Next() {
		if r := scanCloudResource(rows); r != nil {
			out = append(out, r)
		}
	}
	if err := rows.Err(); err != nil {
		storefail.Record("[store] ListCloudResources 遍历失败: %v", err)
		return nil, 0, err
	}
	return out, total, nil
}

// DeleteCloudResourcesByAccount 删除指定云账号下全部云资源，返回删除条数。
func (s *SQLStore) DeleteCloudResourcesByAccount(ctx context.Context, accountID string) error {
	if accountID == "" {
		return errCloudResourceInvalid
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM cloud_resources WHERE cloud_account_id=?`, accountID)
	if err != nil {
		storefail.Record("[store] DeleteCloudResourcesByAccount 删除失败 (account=%s): %v", accountID, err)
		return err
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		storefail.Record("[store] DeleteCloudResourcesByAccount RowsAffected 失败 (account=%s): %v", accountID, rowsErr)
		return rowsErr
	}
	storefail.Record("[store] DeleteCloudResourcesByAccount 删除 (account=%s) 条数=%d", accountID, n)
	return nil
}
