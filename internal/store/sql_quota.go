// sql_quota.go P2-B5 多租户资源配额 SQL 持久化：quota_configs 表。
//
// 替换原内存 map 实现（task 274），改为基于 MySQL 的持久化：
//   - quota_configs：租户级资源配额配置（MaxDevices/MaxTasks/MaxAlerts）。
//
// 设计要点（与 sql_m2.go 风格一致）：
//   - 参数化查询防 SQL 注入；
//   - 租户隔离：tenant_id 为主键，按租户 upsert；
//   - DB 不可用时返回零值（nil/error），不 panic，与 SQLStore 其他方法一致；
//   - 持久化失败仅日志提示，不向上抛（与 sql_alerts.go CreateAlertRule 范式一致）。
//
// 表结构由 migrations/006_quota_configs.sql 创建（幂等 CREATE TABLE IF NOT EXISTS）。
// 所有方法线程安全（database/sql 内部连接池保护，无需额外锁）。
package store

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"
)

// GetQuota 返回租户配额配置（不存在返回 nil + nil error，由调用方回退默认配额）。
// DB 不可用或查询失败时返回 nil + nil error（与 MemoryStore 行为一致，避免阻断配额检查）。
func (s *SQLStore) GetQuota(tenantID string) (*QuotaConfig, error) {
	if tenantID == "" {
		return nil, nil
	}
	if s.db == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var cfg QuotaConfig
	var updatedAt sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT max_devices, max_tasks, max_alerts, updated_at FROM quota_configs WHERE tenant_id=?`,
		tenantID).Scan(&cfg.MaxDevices, &cfg.MaxTasks, &cfg.MaxAlerts, &updatedAt)
	if err != nil {
		// sql.ErrNoRows 表示该租户未设置配额，返回 nil + nil error（回退默认配额）。
		if err == sql.ErrNoRows {
			return nil, nil
		}
		log.Printf("[store] GetQuota 查询失败 (tenant=%s): %v", tenantID, err)
		return nil, nil
	}
	return &cfg, nil
}

// SetQuota 设置或更新租户配额（按 tenantID 幂等 upsert）。
// cfg 为 nil 时等价于删除该租户配额（回退到默认配额）。
func (s *SQLStore) SetQuota(tenantID string, cfg *QuotaConfig) error {
	if tenantID == "" {
		return fmt.Errorf("tenantID must not be empty")
	}
	if s.db == nil {
		return fmt.Errorf("database not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// nil cfg = 删除该租户配额（回退到默认配额）。
	if cfg == nil {
		if _, err := s.db.ExecContext(ctx,
			`DELETE FROM quota_configs WHERE tenant_id=?`, tenantID); err != nil {
			log.Printf("[store] SetQuota 删除失败 (tenant=%s): %v", tenantID, err)
			return err
		}
		return nil
	}
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO quota_configs (tenant_id, max_devices, max_tasks, max_alerts, updated_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE max_devices=VALUES(max_devices), max_tasks=VALUES(max_tasks),
		   max_alerts=VALUES(max_alerts), updated_at=VALUES(updated_at)`,
		tenantID, cfg.MaxDevices, cfg.MaxTasks, cfg.MaxAlerts, now); err != nil {
		log.Printf("[store] SetQuota upsert 失败 (tenant=%s): %v", tenantID, err)
		return err
	}
	return nil
}