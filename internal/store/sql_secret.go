package store

import (
	"context"
	"database/sql"
	"log"
	"time"
)

// sql_secret.go 实现 SQLStore 的 SecretStore 子接口（P0.3 密钥管理，生产就绪）。
//
// 表结构：secrets（tenant_id/key_name/version 联合主键 + value + key_type + created_at + updated_at）。
// 迁移文件 migrations/007_p03_secrets.sql 幂等建表。
//
// 设计要点（与 sql_k8s.go 风格一致）：
//   - 每次写入都 INSERT 新版本行，version 单调递增（MAX(version)+1）；
//   - GetSecret / ListSecrets 通过 MAX(version) 子查询定位当前版本；
//   - 生产环境 value 列须应用层加密（KMS/信封加密），DBA 不可见明文；
//   - DB 不可用时返回零值（nil/false/空 slice），不 panic。

// scanSecretMeta 从一行扫描出 *SecretMeta（不含 value，脱敏视图）。
// 列顺序：tenant_id, key_name, key_type, version, created_at, updated_at。
func scanSecretMeta(row rowScanner) *SecretMeta {
	var m SecretMeta
	var createdAt, updatedAt time.Time
	if err := row.Scan(&m.TenantID, &m.Key, &m.KeyType, &m.Version, &createdAt, &updatedAt); err != nil {
		return nil
	}
	m.CreatedAt = createdAt
	m.UpdatedAt = updatedAt
	return &m
}

// GetSecret 按 (tenantID, key) 返回当前版本密钥明文项。
// 通过 MAX(version) 子查询定位当前版本；不存在返回 (nil, false)。
func (s *SQLStore) GetSecret(tenantID, key string) (*SecretItem, bool) {
	var value, keyType string
	err := s.db.QueryRowContext(context.Background(),
		`SELECT value, key_type FROM secrets
		  WHERE tenant_id=? AND key_name=?
		    AND version=(SELECT MAX(version) FROM secrets WHERE tenant_id=? AND key_name=?)`,
		tenantID, key, tenantID, key).Scan(&value, &keyType)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, false
		}
		log.Printf("[store] GetSecret 查询失败 (tenant=%s key=%s): %v", tenantID, key, err)
		return nil, false
	}
	return &SecretItem{Key: key, Value: value, KeyType: keyType}, true
}

// SetSecret 写入/轮换密钥：每次产生新版本行（MAX(version)+1）。
// item==nil 返回 nil；空租户归一为 default；空 KeyType 默认 passphrase。
func (s *SQLStore) SetSecret(item *SecretItem, tenantID string) *SecretMeta {
	if item == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	if item.KeyType == "" {
		item.KeyType = "passphrase"
	}
	// 查当前最大版本号（无历史则 0，新版本 = max+1）。
	// MAX(version) 在无任何行时返回一行 NULL（而非 ErrNoRows），须用 NullInt64 承接；
	// 此前直接 Scan 到 int 在首次写入（空表）时报 converting NULL to int 导致
	// SetSecret 返回 nil（CI integration 实测捕获）。
	var maxVersion sql.NullInt64
	if err := s.db.QueryRowContext(context.Background(),
		`SELECT MAX(version) FROM secrets WHERE tenant_id=? AND key_name=?`,
		tenantID, item.Key).Scan(&maxVersion); err != nil {
		log.Printf("[store] SetSecret 查询最大版本失败 (tenant=%s key=%s): %v", tenantID, item.Key, err)
		return nil
	}
	newVersion := int(maxVersion.Int64) + 1
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO secrets (tenant_id, key_name, version, value, key_type, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		tenantID, item.Key, newVersion, item.Value, item.KeyType, now, now); err != nil {
		log.Printf("[store] SetSecret 插入失败 (tenant=%s key=%s version=%d): %v", tenantID, item.Key, newVersion, err)
		return nil
	}
	return &SecretMeta{
		Key:       item.Key,
		KeyType:   item.KeyType,
		Version:   newVersion,
		TenantID:  tenantID,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// DeleteSecret 删除密钥（含全部历史版本），返回是否删除成功。
func (s *SQLStore) DeleteSecret(tenantID, key string) bool {
	res, err := s.db.ExecContext(context.Background(),
		`DELETE FROM secrets WHERE tenant_id=? AND key_name=?`, tenantID, key)
	if err != nil {
		log.Printf("[store] DeleteSecret 失败 (tenant=%s key=%s): %v", tenantID, key, err)
		return false
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		log.Printf("[store] DeleteSecret RowsAffected 失败 (tenant=%s key=%s): %v", tenantID, key, rowsErr)
		return false
	}
	return n > 0
}

// ListSecrets 列出指定租户的全部密钥元信息（脱敏视图，每个 key 仅返回最新版本）。
// 通过 INNER JOIN MAX(version) 子查询取每个 (tenant_id, key_name) 的当前版本，按 key_name 升序。
func (s *SQLStore) ListSecrets(tenantID string) []*SecretMeta {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT s.tenant_id, s.key_name, s.key_type, s.version, s.created_at, s.updated_at
		  FROM secrets s
		  INNER JOIN (SELECT tenant_id, key_name, MAX(version) AS max_ver
		               FROM secrets WHERE tenant_id=? GROUP BY tenant_id, key_name) m
		    ON s.tenant_id=m.tenant_id AND s.key_name=m.key_name AND s.version=m.max_ver
		 ORDER BY s.key_name`, tenantID)
	if err != nil {
		log.Printf("[store] ListSecrets 查询失败 (tenant=%s): %v", tenantID, err)
		return nil
	}
	defer rows.Close()
	out := make([]*SecretMeta, 0)
	for rows.Next() {
		if m := scanSecretMeta(rows); m != nil {
			out = append(out, m)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] ListSecrets 遍历失败: %v", err)
	}
	return out
}

// RotateSecret 轮换密钥：用新值产生新版本，保留已有 KeyType。
// 不存在则 KeyType 默认 passphrase。
func (s *SQLStore) RotateSecret(tenantID, key, newValue string) *SecretMeta {
	keyType := "passphrase"
	if existing, ok := s.GetSecret(tenantID, key); ok {
		keyType = existing.KeyType
	}
	return s.SetSecret(&SecretItem{Key: key, Value: newValue, KeyType: keyType}, tenantID)
}

// SecretVersions 返回指定密钥的全部版本元信息（按 version 升序）。
func (s *SQLStore) SecretVersions(tenantID, key string) []*SecretMeta {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT tenant_id, key_name, key_type, version, created_at, updated_at
		  FROM secrets WHERE tenant_id=? AND key_name=? ORDER BY version`, tenantID, key)
	if err != nil {
		log.Printf("[store] SecretVersions 查询失败 (tenant=%s key=%s): %v", tenantID, key, err)
		return nil
	}
	defer rows.Close()
	out := make([]*SecretMeta, 0)
	for rows.Next() {
		if m := scanSecretMeta(rows); m != nil {
			out = append(out, m)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] SecretVersions 遍历失败: %v", err)
	}
	return out
}
