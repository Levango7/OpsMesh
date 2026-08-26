package store

import (
	"sort"
	"time"
)

// sql_secret.go SQLStore 对 SecretStore 接口的桩实现。
//
// TODO(p0.3): 接入 MySQL 持久化：
//   - secrets 表：(tenant_id, key, version) PK + value（应用层加密后存储）+ key_type + created_at + updated_at
//   - 当前版本通过 MAX(version) 子查询定位。
//   - 生产环境 value 列须应用层加密（KMS/信封加密），DBA 不可见明文。
// MVP 用内存 map 做缓存，保证接口齐全 + go build 通过。

// GetSecret 按 (tenantID, key) 返回当前版本密钥明文项。
// TODO(p0.3): SELECT * FROM secrets WHERE tenant_id=? AND key=? AND version=(SELECT MAX(version) ...)。
func (s *SQLStore) GetSecret(tenantID, key string) (*SecretItem, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.secrets[secretKey(tenantID, key)]
	if !ok {
		return nil, false
	}
	return item, true
}

// SetSecret 写入/轮换密钥。
// TODO(p0.3): INSERT INTO secrets(tenant_id, key, version, value, key_type, ...) VALUES(?, ?, MAX+1, encrypt(?), ?, ...)。
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
	s.mu.Lock()
	defer s.mu.Unlock()
	sk := secretKey(tenantID, item.Key)
	now := time.Now()
	if oldMeta, ok := s.secretMetas[sk]; ok {
		oldMeta.Version++
		oldMeta.KeyType = item.KeyType
		oldMeta.UpdatedAt = now
		s.secrets[sk] = item
		metaCopy := *oldMeta
		s.secretVersions[sk] = append(s.secretVersions[sk], &metaCopy)
		return oldMeta
	}
	meta := &SecretMeta{
		Key:       item.Key,
		KeyType:   item.KeyType,
		Version:   1,
		TenantID:  tenantID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.secrets[sk] = item
	s.secretMetas[sk] = meta
	metaCopy := *meta
	s.secretVersions[sk] = append(s.secretVersions[sk], &metaCopy)
	return meta
}

// DeleteSecret 删除密钥（含全部历史版本）。
// TODO(p0.3): DELETE FROM secrets WHERE tenant_id=? AND key=?。
func (s *SQLStore) DeleteSecret(tenantID, key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	sk := secretKey(tenantID, key)
	if _, ok := s.secrets[sk]; !ok {
		return false
	}
	delete(s.secrets, sk)
	delete(s.secretMetas, sk)
	delete(s.secretVersions, sk)
	return true
}

// ListSecrets 列出指定租户的全部密钥元信息（脱敏视图）。
// TODO(p0.3): SELECT tenant_id, key, key_type, MAX(version), created_at, updated_at FROM secrets WHERE tenant_id=? GROUP BY ... ORDER BY key。
func (s *SQLStore) ListSecrets(tenantID string) []*SecretMeta {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*SecretMeta
	for _, meta := range s.secretMetas {
		if meta.TenantID != tenantID {
			continue
		}
		out = append(out, meta)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// RotateSecret 轮换密钥：用新值产生新版本。
// TODO(p0.3): 等价于 SetSecret（INSERT 新版本行）。
func (s *SQLStore) RotateSecret(tenantID, key, newValue string) *SecretMeta {
	s.mu.Lock()
	keyType := "passphrase"
	if existing, ok := s.secrets[secretKey(tenantID, key)]; ok {
		keyType = existing.KeyType
	}
	s.mu.Unlock()
	return s.SetSecret(&SecretItem{Key: key, Value: newValue, KeyType: keyType}, tenantID)
}

// SecretVersions 返回指定密钥的全部版本元信息。
// TODO(p0.3): SELECT tenant_id, key, key_type, version, created_at, updated_at FROM secrets WHERE tenant_id=? AND key=? ORDER BY version。
func (s *SQLStore) SecretVersions(tenantID, key string) []*SecretMeta {
	s.mu.Lock()
	defer s.mu.Unlock()
	versions := s.secretVersions[secretKey(tenantID, key)]
	if len(versions) == 0 {
		return nil
	}
	out := make([]*SecretMeta, len(versions))
	copy(out, versions)
	return out
}
