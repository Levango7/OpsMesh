package store

import (
	"context"
	"database/sql"
	"log"
	"sort"
	"time"
)

// sql_config.go SQLStore 对 ConfigStore 接口的 MySQL 持久化实现。
//
// 表结构（见 migrations/008_p03_configs.sql）：
//   - configs：(tenant_id, key_name) PK + value + format + version + description + updated_by + updated_at + published_at
//   - config_history：(tenant_id, key_name, version) PK + value + format + description + updated_by + updated_at
//
// 设计要点（与 sql_k8s.go 风格一致）：
//   - SetConfig 用事务：先把旧版本 INSERT 到 config_history，再 UPSERT configs（version = 旧版本 + 1）；
//   - sql.ErrNoRows 视为"不存在"，返回 (nil, false) 而非错误；
//   - SQL 错误时 log.Printf 记录错误 + 返回零值（nil / nil,false / false / 空 slice），不 panic；
//   - 时间用 time.Now().UTC()；
//   - SQL 实现不限历史版本数（configMaxHistory 仅 MemoryStore 使用）。
//
// RegisterService 等方法在 sql_discovery.go；此处仅 ConfigStore 方法。

// scanConfigItem 从一行扫描出 *ConfigItem（列顺序：key_name, value, format, version, description, tenant_id, updated_by, updated_at）。
func scanConfigItem(row rowScanner) *ConfigItem {
	var c ConfigItem
	var updatedAt time.Time
	if err := row.Scan(&c.Key, &c.Value, &c.Format, &c.Version, &c.Description, &c.TenantID, &c.UpdatedBy, &updatedAt); err != nil {
		return nil
	}
	c.UpdatedAt = updatedAt
	return &c
}

// GetConfig 按 (tenantID, key) 返回当前配置项；不存在返回 (nil, false)。
func (s *SQLStore) GetConfig(tenantID, key string) (*ConfigItem, bool) {
	row := s.db.QueryRowContext(context.Background(),
		`SELECT key_name, value, format, version, description, tenant_id, updated_by, updated_at
		 FROM configs WHERE tenant_id=? AND key_name=?`, tenantID, key)
	item := scanConfigItem(row)
	if item == nil {
		return nil, false
	}
	return item, true
}

// SetConfig 写入/更新配置（事务：旧版本写入 config_history + UPSERT configs）。
//
// 行为与 MemoryStore.SetConfig 一致：
//   - item==nil 返回 nil；
//   - item.TenantID=="" 归一为 "default"；
//   - 已存在配置：version = 旧版本 + 1，旧版本写入 config_history；
//   - 不存在配置：version = 1；
//   - 返回更新后的 ConfigItem（含新版本号）。
func (s *SQLStore) SetConfig(item *ConfigItem) *ConfigItem {
	if item == nil {
		return nil
	}
	if item.TenantID == "" {
		item.TenantID = "default"
	}
	now := time.Now().UTC()

	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		log.Printf("[store] SetConfig 开启事务失败: %v", err)
		return nil
	}
	defer tx.Rollback() // 提交后 Rollback 为 no-op

	// 先查现有配置（用于推导新版本号 + 写历史）。
	var (
		oldValue, oldFormat, oldDesc, oldBy string
		oldVersion                          int
		oldUpdatedAt                        time.Time
	)
	err = tx.QueryRowContext(context.Background(),
		`SELECT value, format, version, description, updated_by, updated_at
		 FROM configs WHERE tenant_id=? AND key_name=?`, item.TenantID, item.Key).
		Scan(&oldValue, &oldFormat, &oldVersion, &oldDesc, &oldBy, &oldUpdatedAt)

	newVersion := 1
	if err == nil {
		// 已存在：把旧版本写入 config_history，新版本号 = 旧版本 + 1。
		if _, hErr := tx.ExecContext(context.Background(),
			`INSERT INTO config_history (tenant_id, key_name, version, value, format, description, updated_by, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			item.TenantID, item.Key, oldVersion, oldValue, oldFormat, oldDesc, oldBy, oldUpdatedAt); hErr != nil {
			log.Printf("[store] SetConfig 写历史版本失败: %v", hErr)
			return nil
		}
		newVersion = oldVersion + 1
	} else if err != sql.ErrNoRows {
		// 非"不存在"的真实错误。
		log.Printf("[store] SetConfig 查现有配置失败: %v", err)
		return nil
	}

	// UPSERT configs（按 tenant_id + key_name 幂等）。
	if _, uErr := tx.ExecContext(context.Background(),
		`INSERT INTO configs (tenant_id, key_name, value, format, version, description, updated_by, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE value=VALUES(value), format=VALUES(format), version=VALUES(version),
		 description=VALUES(description), updated_by=VALUES(updated_by), updated_at=VALUES(updated_at)`,
		item.TenantID, item.Key, item.Value, item.Format, newVersion, item.Description, item.UpdatedBy, now); uErr != nil {
		log.Printf("[store] SetConfig UPSERT configs 失败: %v", uErr)
		return nil
	}

	if cErr := tx.Commit(); cErr != nil {
		log.Printf("[store] SetConfig 提交事务失败: %v", cErr)
		return nil
	}

	item.Version = newVersion
	item.UpdatedAt = now
	return item
}

// DeleteConfig 删除配置（含版本历史）；返回是否删除成功（configs 行数 > 0）。
func (s *SQLStore) DeleteConfig(tenantID, key string) bool {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		log.Printf("[store] DeleteConfig 开启事务失败: %v", err)
		return false
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(context.Background(),
		`DELETE FROM configs WHERE tenant_id=? AND key_name=?`, tenantID, key)
	if err != nil {
		log.Printf("[store] DeleteConfig 删除 configs 失败: %v", err)
		return false
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		log.Printf("[store] DeleteConfig 取行数失败: %v", rowsErr)
		return false
	}

	if _, err := tx.ExecContext(context.Background(),
		`DELETE FROM config_history WHERE tenant_id=? AND key_name=?`, tenantID, key); err != nil {
		log.Printf("[store] DeleteConfig 删除 config_history 失败: %v", err)
		return false
	}

	if cErr := tx.Commit(); cErr != nil {
		log.Printf("[store] DeleteConfig 提交事务失败: %v", cErr)
		return false
	}
	return n > 0
}

// ListConfigs 列出指定租户的全部配置（按 key_name 升序）。
func (s *SQLStore) ListConfigs(tenantID string) []*ConfigItem {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT key_name, value, format, version, description, tenant_id, updated_by, updated_at
		 FROM configs WHERE tenant_id=? ORDER BY key_name`, tenantID)
	if err != nil {
		log.Printf("[store] ListConfigs 查询失败: %v", err)
		return nil
	}
	defer rows.Close()

	out := make([]*ConfigItem, 0)
	for rows.Next() {
		if c := scanConfigItem(rows); c != nil {
			out = append(out, c)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] ListConfigs 遍历失败: %v", err)
	}
	// SQL 已 ORDER BY key_name；此处排序为防御性保证（与 MemoryStore 语义一致）。
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// ConfigHistory 返回版本历史（不含当前版本，与 MemoryStore 语义一致；按 version 升序）。
func (s *SQLStore) ConfigHistory(tenantID, key string) []*ConfigItem {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT key_name, value, format, version, description, tenant_id, updated_by, updated_at
		 FROM config_history WHERE tenant_id=? AND key_name=? ORDER BY version`, tenantID, key)
	if err != nil {
		log.Printf("[store] ConfigHistory 查询失败: %v", err)
		return nil
	}
	defer rows.Close()

	out := make([]*ConfigItem, 0)
	for rows.Next() {
		if c := scanConfigItem(rows); c != nil {
			out = append(out, c)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] ConfigHistory 遍历失败: %v", err)
	}
	return out
}

// PublishConfig 发布配置变更（标记 published_at = NOW()）。
// 不存在返回 (nil, false)；存在返回 (configItem, true)。
func (s *SQLStore) PublishConfig(tenantID, key string) (*ConfigItem, bool) {
	item, ok := s.GetConfig(tenantID, key)
	if !ok {
		return nil, false
	}
	if _, err := s.db.ExecContext(context.Background(),
		`UPDATE configs SET published_at=NOW() WHERE tenant_id=? AND key_name=?`, tenantID, key); err != nil {
		log.Printf("[store] PublishConfig 更新发布时间失败: %v", err)
		return nil, false
	}
	return item, true
}
