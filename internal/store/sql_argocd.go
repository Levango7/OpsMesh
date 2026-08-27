package store

// sql_argocd.go 实现 SQLStore 的 ArgoCDStore 子接口（Phase 2 ArgoCD 应用管理，生产就绪）。
//
// 表结构：argocd_apps（id PK + tenant_id + name + namespace + repo_url + path +
// target_revision + cluster_url + sync_policy + status + health_status +
// created_at + updated_at）。迁移文件 migrations/011_p2_argocd_pipeline_traffic.sql 幂等建表。
//
// 设计要点（与 sql_k8s.go / sql_secret.go 风格一致）：
//   - 纯标量字段，无 JSON 列；
//   - CreateApp 用 INSERT ... ON DUPLICATE KEY UPDATE 实现幂等 upsert；
//   - Get/Update/Delete 均 WHERE id=? AND tenant_id=? 实现租户隔离；
//   - ListApps 按 created_at DESC 返回；
//   - SyncApp 置 status='synced' + health_status='healthy' + updated_at=now，
//     先 SELECT 校验存在 + 租户归属，不存在返回 (nil, false)；
//   - ID 生成复用 memory_argocd.go 的 randArgoCDID()（前缀 argocd-）；
//   - DB 不可用时返回零值（nil/false/空 slice），不 panic，与 SQLStore 其他方法一致。

import (
	"context"

	"log"
	"time"
)

// scanArgoCDApp 从一行扫描出 *ArgoCDApp。
// 列顺序：id, tenant_id, name, namespace, repo_url, path, target_revision,
// cluster_url, sync_policy, status, health_status, created_at, updated_at。
func scanArgoCDApp(row rowScanner) *ArgoCDApp {
	var a ArgoCDApp
	var createdAt, updatedAt time.Time
	if err := row.Scan(
		&a.ID, &a.TenantID, &a.Name, &a.Namespace, &a.RepoURL, &a.Path,
		&a.TargetRevision, &a.ClusterURL, &a.SyncPolicy, &a.Status, &a.HealthStatus,
		&createdAt, &updatedAt,
	); err != nil {
		return nil
	}
	a.CreatedAt = createdAt
	a.UpdatedAt = updatedAt
	return &a
}

// argocdAppColumns 是 argocd_apps 表的查询列清单（与 scanArgoCDApp 顺序一致）。
const argocdAppColumns = `id, tenant_id, name, namespace, repo_url, path, target_revision,
 cluster_url, sync_policy, status, health_status, created_at, updated_at`

// CreateApp 创建 ArgoCD 应用（ID 为空时分配随机 ID）。
//
// 行为与 MemoryStore.CreateApp 一致：
//   - 空租户归一为 default；
//   - ID 为空时由 randArgoCDID() 分配；
//   - SyncPolicy 空 → manual；Status 空 → unknown；HealthStatus 空 → unknown；
//   - CreatedAt 零值填 now；UpdatedAt 始终刷新为 now；
//   - INSERT ... ON DUPLICATE KEY UPDATE 实现幂等 upsert（按 id 唯一）。
func (s *SQLStore) CreateApp(tenantID string, a *ArgoCDApp) *ArgoCDApp {
	if a == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	a.TenantID = tenantID
	now := time.Now().UTC()
	if a.ID == "" {
		a.ID = randArgoCDID()
	}
	if a.SyncPolicy == "" {
		a.SyncPolicy = "manual"
	}
	if a.Status == "" {
		a.Status = "unknown"
	}
	if a.HealthStatus == "" {
		a.HealthStatus = "unknown"
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	a.UpdatedAt = now
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO argocd_apps (id, tenant_id, name, namespace, repo_url, path, target_revision,
		 cluster_url, sync_policy, status, health_status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE name=VALUES(name), namespace=VALUES(namespace), repo_url=VALUES(repo_url),
		 path=VALUES(path), target_revision=VALUES(target_revision), cluster_url=VALUES(cluster_url),
		 sync_policy=VALUES(sync_policy), status=VALUES(status), health_status=VALUES(health_status),
		 updated_at=VALUES(updated_at)`,
		a.ID, a.TenantID, a.Name, a.Namespace, a.RepoURL, a.Path, a.TargetRevision,
		a.ClusterURL, a.SyncPolicy, a.Status, a.HealthStatus, a.CreatedAt, a.UpdatedAt); err != nil {
		log.Printf("[store] CreateApp 插入失败 (tenant=%s id=%s): %v", tenantID, a.ID, err)
		return nil
	}
	return a
}

// GetApp 按 (tenantID, id) 返回单个应用。不存在返回 (nil, false)。
func (s *SQLStore) GetApp(tenantID, id string) (*ArgoCDApp, bool) {
	row := s.db.QueryRowContext(context.Background(),
		`SELECT `+argocdAppColumns+` FROM argocd_apps WHERE id=? AND tenant_id=?`, id, tenantID)
	a := scanArgoCDApp(row)
	if a == nil {
		return nil, false
	}
	return a, true
}

// UpdateApp 更新应用（按 a.ID 定位，校验 tenantID 归属）。
//
// 行为与 MemoryStore.UpdateApp 一致：
//   - a==nil 或 a.ID=="" 返回 (nil, false)；
//   - 不存在或租户不匹配返回 (nil, false)；
//   - CreatedAt 保留原值，UpdatedAt 刷新为 now。
func (s *SQLStore) UpdateApp(tenantID string, a *ArgoCDApp) (*ArgoCDApp, bool) {
	if a == nil || a.ID == "" {
		return nil, false
	}
	existing, ok := s.GetApp(tenantID, a.ID)
	if !ok {
		return nil, false
	}
	a.TenantID = existing.TenantID
	a.CreatedAt = existing.CreatedAt
	a.UpdatedAt = time.Now().UTC()
	if _, err := s.db.ExecContext(context.Background(),
		`UPDATE argocd_apps SET name=?, namespace=?, repo_url=?, path=?, target_revision=?,
		 cluster_url=?, sync_policy=?, status=?, health_status=?, updated_at=?
		 WHERE id=? AND tenant_id=?`,
		a.Name, a.Namespace, a.RepoURL, a.Path, a.TargetRevision,
		a.ClusterURL, a.SyncPolicy, a.Status, a.HealthStatus, a.UpdatedAt,
		a.ID, tenantID); err != nil {
		log.Printf("[store] UpdateApp 失败 (tenant=%s id=%s): %v", tenantID, a.ID, err)
		return nil, false
	}
	return a, true
}

// ListApps 返回指定租户的全部应用（按 created_at DESC）。
func (s *SQLStore) ListApps(tenantID string) []*ArgoCDApp {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT `+argocdAppColumns+` FROM argocd_apps WHERE tenant_id=? ORDER BY created_at DESC`, tenantID)
	if err != nil {
		log.Printf("[store] ListApps 查询失败 (tenant=%s): %v", tenantID, err)
		return []*ArgoCDApp{}
	}
	defer rows.Close()
	out := make([]*ArgoCDApp, 0)
	for rows.Next() {
		if a := scanArgoCDApp(rows); a != nil {
			out = append(out, a)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] ListApps 遍历失败: %v", err)
	}
	return out
}

// DeleteApp 删除应用，返回是否删除成功（不存在或租户不匹配返回 false）。
func (s *SQLStore) DeleteApp(tenantID, id string) bool {
	res, err := s.db.ExecContext(context.Background(),
		`DELETE FROM argocd_apps WHERE id=? AND tenant_id=?`, id, tenantID)
	if err != nil {
		log.Printf("[store] DeleteApp 失败 (tenant=%s id=%s): %v", tenantID, id, err)
		return false
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		log.Printf("[store] DeleteApp RowsAffected 失败 (tenant=%s id=%s): %v", tenantID, id, rowsErr)
		return false
	}
	return n > 0
}

// SyncApp 触发同步：置 status='synced' + health_status='healthy' + updated_at=now。
//
// 行为与 MemoryStore.SyncApp 一致：
//   - 先 SELECT 校验存在 + 租户归属，不存在返回 (nil, false)；
//   - 模拟同步成功（status=synced, health_status=healthy）；
//   - 返回更新后的 App。
func (s *SQLStore) SyncApp(tenantID, id string) (*ArgoCDApp, bool) {
	existing, ok := s.GetApp(tenantID, id)
	if !ok {
		return nil, false
	}
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(context.Background(),
		`UPDATE argocd_apps SET status='synced', health_status='healthy', updated_at=?
		 WHERE id=? AND tenant_id=?`, now, id, tenantID); err != nil {
		log.Printf("[store] SyncApp 失败 (tenant=%s id=%s): %v", tenantID, id, err)
		return nil, false
	}
	existing.Status = "synced"
	existing.HealthStatus = "healthy"
	existing.UpdatedAt = now
	return existing, true
}
