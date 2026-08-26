// sql_apikey.go 实现 SQLStore 的 APIKeyStore 子接口（Phase 6 API Key 管理，生产就绪）。
//
// 表结构：api_keys（id PK + tenant_id + name + key_hash + scopes JSON +
// rate_limit_per_sec + expires_at NULL + last_used_at NULL + enabled TINYINT(-1) +
// created_at）。迁移文件 migrations/015_p6_tenant_apikey_plugin_billing.sql 幂等建表。
//
// 设计要点（与 sql_webhook.go / sql_secret.go 风格一致）：
//   - JSON 列：scopes（[]string），用 encoding/json 序列化为 TEXT；空值存空串，
//     读取时空串跳过 Unmarshal 得零值；
//   - 敏感列 key_hash：存 SHA-256 hash（明文仅在创建时返回一次）；列名用 key_hash
//     避免与 SQL 关键字 key 冲突；
//   - bool 列 enabled 用 TINYINT(1)，默认 1；
//   - 可空 expires_at / last_used_at：零值 time.Time 视为"永不/未用"，用 sql.NullTime
//     处理；读取时 NULL → 零值 time.Time{}；
//   - CreateAPIKey 按 ID 幂等（INSERT ... ON DUPLICATE KEY UPDATE），tenant_id 仅插入
//     不更新（防 upsert 改写归属）；
//   - ListAPIKeys：tenantID == "" 时返回全部租户的 API Key（供 ValidateKey 全租户扫描），
//     省略 WHERE tenant_id=? 条件（与 memory 一致）；按创建时间降序；
//   - UpdateAPIKey 先 SELECT 校验存在 + 租户归属，再 UPDATE，保留原 CreatedAt/TenantID；
//   - DB 不可用时返回零值（nil/false/空 slice），不 panic；
//   - ID 生成复用 memory_apikey.go 的 randAPIKeyID（"apikey-" + 16 字节 hex）。
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"time"
)

// scanAPIKey 从一行扫描出 *APIKey（scopes 为 JSON 文本列；expires_at/last_used_at 可空）。
// 列顺序：id, tenant_id, name, key_hash, scopes, rate_limit_per_sec, expires_at,
// last_used_at, enabled, created_at。无行或扫描失败返回 nil。
func scanAPIKey(row rowScanner) *APIKey {
	var k APIKey
	var scopesJSON string
	var expiresAt, lastUsedAt sql.NullTime
	var enabled int
	var createdAt time.Time
	if err := row.Scan(&k.ID, &k.TenantID, &k.Name, &k.Key, &scopesJSON, &k.RateLimitPerSec,
		&expiresAt, &lastUsedAt, &enabled, &createdAt); err != nil {
		return nil
	}
	k.Enabled = enabled != 0
	k.CreatedAt = createdAt
	if expiresAt.Valid {
		k.ExpiresAt = expiresAt.Time
	}
	if lastUsedAt.Valid {
		k.LastUsedAt = lastUsedAt.Time
	}
	if scopesJSON != "" {
		if err := json.Unmarshal([]byte(scopesJSON), &k.Scopes); err != nil {
			log.Printf("[store] scanAPIKey 解析 scopes JSON 失败 (apikey=%s): %v", k.ID, err)
		}
	}
	return &k
}

// apiKeyEnabledInt 将 bool 转换为 TINYINT(1) 用的 int（true→1，false→0）。
func apiKeyEnabledInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// marshalAPIKeyScopes 将 Scopes 序列化为 JSON 文本（空切片存空串）。
func marshalAPIKeyScopes(scopes []string) string {
	if scopes == nil {
		return ""
	}
	b, err := json.Marshal(scopes)
	if err != nil {
		return ""
	}
	return string(b)
}

// timeToNullTime 将 time.Time 转换为 sql.NullTime（零值 → Invalid）。
func timeToNullTime(t time.Time) sql.NullTime {
	if t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t, Valid: true}
}

// CreateAPIKey 创建 API Key（按 ID 幂等；ID 为空时分配随机 ID）。
//
// 行为：
//   - key == nil 返回 nil；
//   - TenantID 为空时归一为 default；
//   - ID 为空时分配随机 ID（新建场景）；
//   - CreatedAt 为零值时填当前时间（新建场景）；
//   - INSERT ... ON DUPLICATE KEY UPDATE 实现 upsert（按 id 幂等），
//     tenant_id / created_at 仅插入不更新，防 upsert 改写归属/创建时间；
//   - DB 失败时 log.Printf + 返回 nil。
func (s *SQLStore) CreateAPIKey(tenantID string, key *APIKey) *APIKey {
	if key == nil {
		return nil
	}
	// 租户隔离：空租户归一为 default。
	if tenantID == "" {
		tenantID = "default"
	}
	key.TenantID = tenantID
	if key.ID == "" {
		key.ID = randAPIKeyID()
	}
	now := time.Now().UTC()
	if key.CreatedAt.IsZero() {
		key.CreatedAt = now
	}
	scopesJSON := marshalAPIKeyScopes(key.Scopes)
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO api_keys (id, tenant_id, name, key_hash, scopes, rate_limit_per_sec, expires_at, last_used_at, enabled, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE name=VALUES(name), key_hash=VALUES(key_hash), scopes=VALUES(scopes),
		 rate_limit_per_sec=VALUES(rate_limit_per_sec), expires_at=VALUES(expires_at),
		 last_used_at=VALUES(last_used_at), enabled=VALUES(enabled)`,
		key.ID, key.TenantID, key.Name, key.Key, scopesJSON, key.RateLimitPerSec,
		timeToNullTime(key.ExpiresAt), timeToNullTime(key.LastUsedAt),
		apiKeyEnabledInt(key.Enabled), key.CreatedAt); err != nil {
		log.Printf("[store] CreateAPIKey 插入失败 (tenant=%s apikey=%s): %v", tenantID, key.ID, err)
		return nil
	}
	return cloneAPIKey(key)
}

// GetAPIKey 按 (tenantID, id) 返回单个 API Key（深拷贝；不存在或租户不匹配返回 (nil, false)）。
// tenantID 为空串时不带 tenant_id 条件（与 memory 一致，供 ValidateKey 全租户扫描）。
func (s *SQLStore) GetAPIKey(tenantID, id string) (*APIKey, bool) {
	var row interface {
		Scan(dest ...interface{}) error
	}
	if tenantID == "" {
		row = s.db.QueryRowContext(context.Background(),
			`SELECT id, tenant_id, name, key_hash, scopes, rate_limit_per_sec, expires_at, last_used_at, enabled, created_at
			  FROM api_keys WHERE id=?`, id)
	} else {
		row = s.db.QueryRowContext(context.Background(),
			`SELECT id, tenant_id, name, key_hash, scopes, rate_limit_per_sec, expires_at, last_used_at, enabled, created_at
			  FROM api_keys WHERE id=? AND tenant_id=?`, id, tenantID)
	}
	k := scanAPIKey(row)
	if k == nil {
		return nil, false
	}
	return k, true
}

// UpdateAPIKey 更新 API Key（按 key.ID 定位，校验 tenantID 归属）。
//
// 行为：
//   - key == nil 或 ID 为空返回 (nil, false)；
//   - 先 GetAPIKey 校验存在 + 租户归属，不存在或越权返回 (nil, false)；
//   - CreatedAt / TenantID 不可改（保留原值，防越权改归属）；
//   - 返回更新后的 APIKey（深拷贝）。
func (s *SQLStore) UpdateAPIKey(tenantID string, key *APIKey) (*APIKey, bool) {
	if key == nil || key.ID == "" {
		return nil, false
	}
	// 先 SELECT 校验存在 + 租户归属。
	existing, ok := s.GetAPIKey(tenantID, key.ID)
	if !ok {
		return nil, false
	}
	// 保留不可改字段。
	key.ID = existing.ID
	key.TenantID = existing.TenantID
	key.CreatedAt = existing.CreatedAt
	scopesJSON := marshalAPIKeyScopes(key.Scopes)
	if _, err := s.db.ExecContext(context.Background(),
		`UPDATE api_keys SET name=?, key_hash=?, scopes=?, rate_limit_per_sec=?, expires_at=?, last_used_at=?, enabled=?
		 WHERE id=? AND tenant_id=?`,
		key.Name, key.Key, scopesJSON, key.RateLimitPerSec,
		timeToNullTime(key.ExpiresAt), timeToNullTime(key.LastUsedAt),
		apiKeyEnabledInt(key.Enabled), key.ID, key.TenantID); err != nil {
		log.Printf("[store] UpdateAPIKey 更新失败 (tenant=%s apikey=%s): %v", tenantID, key.ID, err)
		return nil, false
	}
	return cloneAPIKey(key), true
}

// ListAPIKeys 返回指定租户的全部 API Key（按创建时间降序；深拷贝）。
// tenantID 为空串时返回全部租户的 API Key（供 platform.APIKeyManager.ValidateKey 全租户扫描）。
func (s *SQLStore) ListAPIKeys(tenantID string) []*APIKey {
	var rows *sql.Rows
	var err error
	if tenantID == "" {
		rows, err = s.db.QueryContext(context.Background(),
			`SELECT id, tenant_id, name, key_hash, scopes, rate_limit_per_sec, expires_at, last_used_at, enabled, created_at
			  FROM api_keys ORDER BY created_at DESC`)
	} else {
		rows, err = s.db.QueryContext(context.Background(),
			`SELECT id, tenant_id, name, key_hash, scopes, rate_limit_per_sec, expires_at, last_used_at, enabled, created_at
			  FROM api_keys WHERE tenant_id=? ORDER BY created_at DESC`, tenantID)
	}
	if err != nil {
		log.Printf("[store] ListAPIKeys 查询失败 (tenant=%s): %v", tenantID, err)
		return []*APIKey{}
	}
	defer rows.Close()
	out := make([]*APIKey, 0)
	for rows.Next() {
		if k := scanAPIKey(rows); k != nil {
			out = append(out, k)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] ListAPIKeys 遍历失败: %v", err)
	}
	return out
}

// DeleteAPIKey 删除 API Key，返回是否删除成功（不存在或租户不匹配返回 false）。
// tenantID 为空串时按 id 全局删除（与 Get/List 一致，供管理后台清理）。
func (s *SQLStore) DeleteAPIKey(tenantID, id string) bool {
	var res sql.Result
	var err error
	if tenantID == "" {
		res, err = s.db.ExecContext(context.Background(),
			`DELETE FROM api_keys WHERE id=?`, id)
	} else {
		res, err = s.db.ExecContext(context.Background(),
			`DELETE FROM api_keys WHERE id=? AND tenant_id=?`, id, tenantID)
	}
	if err != nil {
		log.Printf("[store] DeleteAPIKey 失败 (tenant=%s apikey=%s): %v", tenantID, id, err)
		return false
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		log.Printf("[store] DeleteAPIKey RowsAffected 失败 (tenant=%s apikey=%s): %v", tenantID, id, rowsErr)
		return false
	}
	return n > 0
}
