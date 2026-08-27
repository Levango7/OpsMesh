// sql_plugin.go 实现 SQLStore 的 PluginStore 子接口（Phase 6 插件市场，生产就绪）。
//
// 表结构：plugins（id PK + name + version + description + author + type +
// download_url + checksum + installed TINYINT(1) + enabled TINYINT(1) + created_at）。
// 迁移文件 migrations/015_p6_tenant_apikey_plugin_billing.sql 幂等建表。
//
// 设计要点（与 sql_webhook.go / sql_secret.go 风格一致）：
//   - 全局共享：plugins 表无 tenant_id 列，所有方法不带 tenant_id 条件；
//   - bool 列 installed / enabled 用 TINYINT(1)，默认 0；
//   - 纯标量字段，无 JSON 列；无 updated_at（只有 created_at）；
//   - CreatePlugin 按 ID 幂等（INSERT ... ON DUPLICATE KEY UPDATE），不更新 created_at；
//   - ListPlugins 按创建时间升序（与 memory 一致）；
//   - UpdatePlugin 先 SELECT 校验存在，再 UPDATE，保留原 CreatedAt；ID 不可改；
//   - DB 不可用时返回零值（nil/false/空 slice），不 panic；
//   - ID 生成复用 memory_plugin.go 的 randPluginID（"plugin-" + 16 字节 hex）。
package store

import (
	"context"

	"log"
	"time"
)

// scanPlugin 从一行扫描出 *Plugin。
// 列顺序：id, name, version, description, author, type, download_url, checksum,
// installed, enabled, created_at。无行或扫描失败返回 nil。
func scanPlugin(row rowScanner) *Plugin {
	var p Plugin
	var installed, enabled int
	var createdAt time.Time
	if err := row.Scan(&p.ID, &p.Name, &p.Version, &p.Description, &p.Author, &p.Type,
		&p.DownloadURL, &p.Checksum, &installed, &enabled, &createdAt); err != nil {
		return nil
	}
	p.Installed = installed != 0
	p.Enabled = enabled != 0
	p.CreatedAt = createdAt
	return &p
}

// pluginBoolInt 将 bool 转换为 TINYINT(1) 用的 int（true→1，false→0）。
func pluginBoolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// CreatePlugin 创建插件（按 ID 幂等；ID 为空时分配随机 ID）。
//
// 行为：
//   - plugin == nil 返回 nil；
//   - ID 为空时分配随机 ID（新建场景）；
//   - Type 为空时归一为 agent；
//   - CreatedAt 为零值时填当前时间（新建场景）；
//   - INSERT ... ON DUPLICATE KEY UPDATE 实现 upsert（按 id 幂等），
//     created_at 仅插入不更新，防 upsert 改写创建时间；
//   - DB 失败时 log.Printf + 返回 nil。
func (s *SQLStore) CreatePlugin(plugin *Plugin) *Plugin {
	if plugin == nil {
		return nil
	}
	if plugin.ID == "" {
		plugin.ID = randPluginID()
	}
	if plugin.Type == "" {
		plugin.Type = "agent"
	}
	if plugin.CreatedAt.IsZero() {
		plugin.CreatedAt = time.Now().UTC()
	}
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO plugins (id, name, version, description, author, type, download_url, checksum, installed, enabled, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE name=VALUES(name), version=VALUES(version), description=VALUES(description),
		 author=VALUES(author), type=VALUES(type), download_url=VALUES(download_url),
		 checksum=VALUES(checksum), installed=VALUES(installed), enabled=VALUES(enabled)`,
		plugin.ID, plugin.Name, plugin.Version, plugin.Description, plugin.Author, plugin.Type,
		plugin.DownloadURL, plugin.Checksum, pluginBoolInt(plugin.Installed),
		pluginBoolInt(plugin.Enabled), plugin.CreatedAt); err != nil {
		log.Printf("[store] CreatePlugin 插入失败 (plugin=%s): %v", plugin.ID, err)
		return nil
	}
	return clonePlugin(plugin)
}

// GetPlugin 按 ID 返回单个插件（深拷贝；不存在返回 (nil, false)）。
func (s *SQLStore) GetPlugin(id string) (*Plugin, bool) {
	row := s.db.QueryRowContext(context.Background(),
		`SELECT id, name, version, description, author, type, download_url, checksum, installed, enabled, created_at
		  FROM plugins WHERE id=?`, id)
	p := scanPlugin(row)
	if p == nil {
		return nil, false
	}
	return p, true
}

// UpdatePlugin 更新插件（按 plugin.ID 定位）。
//
// 行为：
//   - plugin == nil 或 ID 为空返回 (nil, false)；
//   - 先 GetPlugin 校验存在，不存在返回 (nil, false)；
//   - CreatedAt 不可改（保留原值）；ID 不可改；
//   - 返回更新后的 Plugin（深拷贝）。
func (s *SQLStore) UpdatePlugin(plugin *Plugin) (*Plugin, bool) {
	if plugin == nil || plugin.ID == "" {
		return nil, false
	}
	// 先 SELECT 校验存在。
	existing, ok := s.GetPlugin(plugin.ID)
	if !ok {
		return nil, false
	}
	// 保留不可改字段。
	plugin.ID = existing.ID
	plugin.CreatedAt = existing.CreatedAt
	if _, err := s.db.ExecContext(context.Background(),
		`UPDATE plugins SET name=?, version=?, description=?, author=?, type=?, download_url=?, checksum=?, installed=?, enabled=?
		 WHERE id=?`,
		plugin.Name, plugin.Version, plugin.Description, plugin.Author, plugin.Type,
		plugin.DownloadURL, plugin.Checksum, pluginBoolInt(plugin.Installed),
		pluginBoolInt(plugin.Enabled), plugin.ID); err != nil {
		log.Printf("[store] UpdatePlugin 更新失败 (plugin=%s): %v", plugin.ID, err)
		return nil, false
	}
	return clonePlugin(plugin), true
}

// ListPlugins 返回全部插件（按创建时间升序；深拷贝）。
func (s *SQLStore) ListPlugins() []*Plugin {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT id, name, version, description, author, type, download_url, checksum, installed, enabled, created_at
		  FROM plugins ORDER BY created_at ASC`)
	if err != nil {
		log.Printf("[store] ListPlugins 查询失败: %v", err)
		return []*Plugin{}
	}
	defer rows.Close()
	out := make([]*Plugin, 0)
	for rows.Next() {
		if p := scanPlugin(rows); p != nil {
			out = append(out, p)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] ListPlugins 遍历失败: %v", err)
	}
	return out
}

// DeletePlugin 按 ID 删除插件。不存在返回 false。
func (s *SQLStore) DeletePlugin(id string) bool {
	res, err := s.db.ExecContext(context.Background(),
		`DELETE FROM plugins WHERE id=?`, id)
	if err != nil {
		log.Printf("[store] DeletePlugin 失败 (plugin=%s): %v", id, err)
		return false
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		log.Printf("[store] DeletePlugin RowsAffected 失败 (plugin=%s): %v", id, rowsErr)
		return false
	}
	return n > 0
}
