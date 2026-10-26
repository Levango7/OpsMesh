package sqlstore

import (
	"context"
	"database/sql"
	"time"

	"github.com/Levango7/OpsMesh/internal/store/model"
	"github.com/Levango7/OpsMesh/internal/store/storefail"
	"github.com/Levango7/OpsMesh/internal/store/storekit"
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

// scanSecretAccessLog 从一行扫描出 *SecretAccessLog。
// 列顺序：key_name, tenant_id, accessed_by, action, accessed_at。
func scanSecretAccessLog(row rowScanner) *SecretAccessLog {
	var l SecretAccessLog
	var accessedAt time.Time
	if err := row.Scan(&l.Key, &l.TenantID, &l.AccessedBy, &l.Action, &accessedAt); err != nil {
		return nil
	}
	l.AccessedAt = accessedAt
	return &l
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
		storefail.Record("[store] GetSecret 查询失败 (tenant=%s key=%s): %v", tenantID, key, err)
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
	// TD-88：secrets 表只存 ${provider:key} 引用（明文拒绝；契约见 model.RequireSecretReference）。
	if err := model.RequireSecretReference(item.Value); err != nil {
		storefail.Record("[store] SetSecret 拒绝非引用值 (tenant=%s key=%s): %v", tenantID, item.Key, err)
		return nil
	}
	// 查当前最大版本号（无历史则 0，新版本 = max+1）。
	// MAX(version) 在无任何行时返回一行 NULL（而非 ErrNoRows），须用 NullInt64 承接；
	// 此前直接 Scan 到 int 在首次写入（空表）时报 converting NULL to int 导致
	// SetSecret 返回 nil（CI integration 实测捕获）。
	var maxVersion sql.NullInt64
	if err := s.db.QueryRowContext(context.Background(),
		`SELECT MAX(version) FROM secrets WHERE tenant_id=? AND key_name=?`,
		tenantID, item.Key).Scan(&maxVersion); err != nil {
		storefail.Record("[store] SetSecret 查询最大版本失败 (tenant=%s key=%s): %v", tenantID, item.Key, err)
		return nil
	}
	newVersion := int(maxVersion.Int64) + 1
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO secrets (tenant_id, key_name, version, value, key_type, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		tenantID, item.Key, newVersion, item.Value, item.KeyType, now, now); err != nil {
		storefail.Record("[store] SetSecret 插入失败 (tenant=%s key=%s version=%d): %v", tenantID, item.Key, newVersion, err)
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
		storefail.Record("[store] DeleteSecret 失败 (tenant=%s key=%s): %v", tenantID, key, err)
		return false
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		storefail.Record("[store] DeleteSecret RowsAffected 失败 (tenant=%s key=%s): %v", tenantID, key, rowsErr)
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
		storefail.Record("[store] ListSecrets 查询失败 (tenant=%s): %v", tenantID, err)
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
		storefail.Record("[store] ListSecrets 遍历失败: %v", err)
	}
	return out
}

// RotateSecret 轮换密钥：SQL 事务内插入新版本行（version=MAX+1）并写入访问日志。
// keyType 为空时沿用已有 KeyType（不存在则 passphrase）。rotatedBy 记入访问日志。
// 不存在则等价于 SetSecret（Version=1）。返回新版本元信息；失败返回 error。
func (s *SQLStore) RotateSecret(tenantID, key, newValue, keyType, rotatedBy string) (*SecretMeta, error) {
	if tenantID == "" {
		tenantID = "default"
	}
	if keyType == "" {
		if existing, ok := s.GetSecret(tenantID, key); ok {
			keyType = existing.KeyType
		} else {
			keyType = "passphrase"
		}
	}
	// TD-88：只存引用，拒绝明文。
	if err := model.RequireSecretReference(newValue); err != nil {
		storefail.Record("[store] RotateSecret 拒绝非引用值 (tenant=%s key=%s): %v", tenantID, key, err)
		return nil, err
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		storefail.Record("[store] RotateSecret 开启事务失败 (tenant=%s key=%s): %v", tenantID, key, err)
		return nil, err
	}
	defer tx.Rollback()
	// 查当前最大版本号（无历史则 0，新版本 = max+1）。
	var maxVersion sql.NullInt64
	if err := tx.QueryRowContext(context.Background(),
		`SELECT MAX(version) FROM secrets WHERE tenant_id=? AND key_name=?`,
		tenantID, key).Scan(&maxVersion); err != nil {
		storefail.Record("[store] RotateSecret 查询最大版本失败 (tenant=%s key=%s): %v", tenantID, key, err)
		return nil, err
	}
	newVersion := int(maxVersion.Int64) + 1
	now := time.Now().UTC()
	if _, err := tx.ExecContext(context.Background(),
		`INSERT INTO secrets (tenant_id, key_name, version, value, key_type, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		tenantID, key, newVersion, newValue, keyType, now, now); err != nil {
		storefail.Record("[store] RotateSecret 插入新版本失败 (tenant=%s key=%s version=%d): %v", tenantID, key, newVersion, err)
		return nil, err
	}
	// 写入访问审计日志。
	logID := storekit.MustRandHex(32)
	if _, err := tx.ExecContext(context.Background(),
		`INSERT INTO secret_access_logs (id, tenant_id, key_name, accessed_by, action, accessed_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		logID, tenantID, key, rotatedBy, "rotate", now); err != nil {
		storefail.Record("[store] RotateSecret 写入访问日志失败 (tenant=%s key=%s): %v", tenantID, key, err)
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		storefail.Record("[store] RotateSecret 提交事务失败 (tenant=%s key=%s): %v", tenantID, key, err)
		return nil, err
	}
	return &SecretMeta{
		Key:       key,
		KeyType:   keyType,
		Version:   newVersion,
		TenantID:  tenantID,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// AddSecretAccessLog 记录一次密钥访问审计（get/rotate/delete）。
func (s *SQLStore) AddSecretAccessLog(tenantID, key, accessedBy, action string) error {
	if tenantID == "" {
		tenantID = "default"
	}
	if action == "" {
		action = "get"
	}
	id := storekit.MustRandHex(32)
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO secret_access_logs (id, tenant_id, key_name, accessed_by, action, accessed_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, tenantID, key, accessedBy, action, now); err != nil {
		storefail.Record("[store] AddSecretAccessLog 插入失败 (tenant=%s key=%s): %v", tenantID, key, err)
		return err
	}
	return nil
}

// GetSecretAccessLog 查询指定密钥的访问审计日志（按租户隔离，accessed_at 降序，分页）。
// key 为空时返回该租户全部密钥的访问日志；limit<=0 表示不限制。
func (s *SQLStore) GetSecretAccessLog(tenantID, key string, offset, limit int) ([]*SecretAccessLog, int, error) {
	if tenantID == "" {
		tenantID = "default"
	}
	// 查总数。
	var total int
	if err := s.db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM secret_access_logs WHERE tenant_id=? AND (? = '' OR key_name=?)`,
		tenantID, key, key).Scan(&total); err != nil {
		storefail.Record("[store] GetSecretAccessLog 计数失败 (tenant=%s key=%s): %v", tenantID, key, err)
		return nil, 0, err
	}
	// 查分页列表（accessed_at 降序）。
	query := `SELECT key_name, tenant_id, accessed_by, action, accessed_at
		  FROM secret_access_logs
		  WHERE tenant_id=? AND (? = '' OR key_name=?)
		  ORDER BY accessed_at DESC`
	args := []interface{}{tenantID, key, key}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
		if offset > 0 {
			query += ` OFFSET ?`
			args = append(args, offset)
		}
	}
	rows, err := s.db.QueryContext(context.Background(), query, args...)
	if err != nil {
		storefail.Record("[store] GetSecretAccessLog 查询失败 (tenant=%s key=%s): %v", tenantID, key, err)
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]*SecretAccessLog, 0)
	for rows.Next() {
		if l := scanSecretAccessLog(rows); l != nil {
			out = append(out, l)
		}
	}
	if err := rows.Err(); err != nil {
		storefail.Record("[store] GetSecretAccessLog 遍历失败: %v", err)
		return nil, 0, err
	}
	return out, total, nil
}

// SecretVersions 返回指定密钥的全部版本元信息（按 version 升序）。
func (s *SQLStore) SecretVersions(tenantID, key string) []*SecretMeta {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT tenant_id, key_name, key_type, version, created_at, updated_at
		  FROM secrets WHERE tenant_id=? AND key_name=? ORDER BY version`, tenantID, key)
	if err != nil {
		storefail.Record("[store] SecretVersions 查询失败 (tenant=%s key=%s): %v", tenantID, key, err)
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
		storefail.Record("[store] SecretVersions 遍历失败: %v", err)
	}
	return out
}
