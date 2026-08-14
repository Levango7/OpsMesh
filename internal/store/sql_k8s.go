// sql_k8s.go 实现 SQLStore 的 K8sClusterStore 子接口（Phase 3 K8s 集群管理，P0-1 生产就绪）。
//
// 表结构：k8s_clusters（id/name/server/kubeconfig/status/created_at/updated_at）。
// initSchema 中幂等建表（CREATE TABLE IF NOT EXISTS）+ alterColumnIfMissing 兼容旧库。
//
// 设计要点（与 sql_rbac.go 风格一致）：
//   - Kubeconfig 以 TEXT 列存储（敏感；API 层负责脱敏后返回前端）；
//   - ListK8sClusters 按创建时间升序返回；
//   - SaveK8sCluster 按 ID 幂等（INSERT ... ON DUPLICATE KEY UPDATE）；
//   - DB 不可用时返回零值（nil/false），不 panic，与 SQLStore 其他方法一致。
package store

import (
	"context"
	"fmt"
	"log"
	"time"
)

// scanK8sCluster 从一行扫描出 *K8sCluster。
func scanK8sCluster(row rowScanner) *K8sCluster {
	var c K8sCluster
	var createdAt, updatedAt time.Time
	if err := row.Scan(&c.ID, &c.TenantID, &c.Name, &c.Server, &c.Kubeconfig, &c.Status, &createdAt, &updatedAt); err != nil {
		return nil
	}
	c.CreatedAt = createdAt
	c.UpdatedAt = updatedAt
	return &c
}

// ListK8sClusters 返回 K8s 集群配置（按创建时间升序）；tenantID 非空时仅返回同租户集群（task 88 租户隔离）。
func (s *SQLStore) ListK8sClusters(tenantID string) []*K8sCluster {
	q :=
		`
SELECT id, tenant_id, name, server, kubeconfig, status, created_at, updated_at FROM k8s_clusters
`
	var args []interface{}
	if tenantID != "" {
		q +=
			`
 WHERE tenant_id=?
`
		args = append(args, tenantID)
	}
	q +=
		`
 ORDER BY created_at ASC
`
	rows, err := s.db.QueryContext(context.Background(), q, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := make([]*K8sCluster, 0)
	for rows.Next() {
		if c := scanK8sCluster(rows); c != nil {
			out = append(out, c)
		}
	}
	return out
}

// GetK8sCluster 按 ID 返回单个集群配置（不存在返回 nil）。
func (s *SQLStore) GetK8sCluster(id string) *K8sCluster {
	row := s.db.QueryRowContext(context.Background(),
		`SELECT id, name, server, kubeconfig, status, created_at, updated_at FROM k8s_clusters WHERE id=?`, id)
	return scanK8sCluster(row)
}

// SaveK8sCluster 创建或更新集群配置（按 ID 幂等）。
//
// 行为与 MemoryStore.SaveK8sCluster 一致：
//   - ID 为空时分配随机 ID（新建场景，走 INSERT 分支）；
//   - CreatedAt 为空时填当前时间；
//   - UpdatedAt 始终刷新为当前时间；
//   - Status 为空时默认 "unknown"。
func (s *SQLStore) SaveK8sCluster(c *K8sCluster) error {
	if c == nil {
		return nil
	}
	// task 88 租户隔离：空租户归一为 default（与 MemoryStore 一致）。
	if c.TenantID == "" {
		c.TenantID = "default"
	}
	now := time.Now().UTC()
	if c.ID == "" {
		c.ID = randK8sClusterID()
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	if c.Status == "" {
		c.Status = "unknown"
	}
	c.UpdatedAt = now
	// INSERT ... ON DUPLICATE KEY UPDATE 实现 upsert（按 id 幂等）。
	// task 88：tenant_id 仅插入不更新，防 upsert 改写集群租户归属。
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO k8s_clusters (id, tenant_id, name, server, kubeconfig, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE name=VALUES(name), server=VALUES(server), kubeconfig=VALUES(kubeconfig),
		 status=VALUES(status), updated_at=VALUES(updated_at)`,
		c.ID, c.TenantID, c.Name, c.Server, c.Kubeconfig, c.Status, c.CreatedAt, c.UpdatedAt); err != nil {
		// task 92：DB 持久化失败上抛错误（调用方据此返回非 2xx，不再假装成功）。
		log.Printf("k8s: SaveK8sCluster 失败: %v", err)
		return fmt.Errorf("k8s: save cluster: %w", err)
	}
	return nil
}

// DeleteK8sCluster 删除集群配置，返回是否删除成功（不存在返回 false）。
func (s *SQLStore) DeleteK8sCluster(id string) bool {
	res, err := s.db.ExecContext(context.Background(), `DELETE FROM k8s_clusters WHERE id=?`, id)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}
