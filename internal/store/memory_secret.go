package store

import (
	"sort"
	"time"
)

// memory_secret.go MemoryStore 对 SecretStore 接口的实现。
// 密钥管理领域：Get/Set/Delete/List + 轮换 + 版本历史。
// 复用 MemoryStore.mu（sync.RWMutex）保护并发安全。
//
// 设计要点：
//   - 按 (tenantID, key) 唯一当前版本；secretVersions 保留全部历史版本元信息。
//   - SetSecret 已存在则 Version+1；不存在则 Version=1。每次写入都把新版本元信息追加到 secretVersions。
//   - GetSecret 返回明文 SecretItem（仅在内部流转；API 须脱敏为 SecretMeta）。
//   - ListSecrets / SecretVersions 返回脱敏元信息（不含 Value）。
//   - RotateSecret 等价于用新值 SetSecret，语义上强调"产生新版本"。

// secretKey 拼装复合键。
func secretKey(tenantID, key string) string { return tenantID + "|" + key }

// cloneSecretItem 返回 item 的深拷贝。
// SecretItem 字段均为值类型，浅拷贝即可；nil 返回 nil。
// 用于读路径返回副本、写路径入 map 前拷贝，隔离外部修改对内部状态的影响。
func cloneSecretItem(item *SecretItem) *SecretItem {
	if item == nil {
		return nil
	}
	cp := *item
	return &cp
}

// cloneSecretMeta 返回 meta 的深拷贝。
// SecretMeta 字段均为值类型，浅拷贝即可；nil 返回 nil。
func cloneSecretMeta(meta *SecretMeta) *SecretMeta {
	if meta == nil {
		return nil
	}
	cp := *meta
	return &cp
}

// GetSecret 按 (tenantID, key) 返回当前版本密钥明文项；不存在返回 (nil, false)。
// 返回内部数据的深拷贝副本，外部修改不影响内部状态。
func (m *MemoryStore) GetSecret(tenantID, key string) (*SecretItem, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	item, ok := m.secrets[secretKey(tenantID, key)]
	if !ok {
		return nil, false
	}
	return cloneSecretItem(item), true
}

// SetSecret 写入/轮换密钥（按 key 幂等）。已存在则 Version+1；不存在则 Version=1。
// tenantID 从参数显式传入（与 SecretItem 无 TenantID 字段对应）。
// 返回更新后的密钥元信息（脱敏视图，不含 Value；深拷贝副本，外部修改不影响内部）。
func (m *MemoryStore) SetSecret(item *SecretItem, tenantID string) *SecretMeta {
	if item == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	if item.KeyType == "" {
		item.KeyType = "passphrase"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	sk := secretKey(tenantID, item.Key)
	now := time.Now()
	if oldMeta, ok := m.secretMetas[sk]; ok {
		// 轮换：版本+1，更新元信息时间。
		oldMeta.Version++
		oldMeta.KeyType = item.KeyType
		oldMeta.UpdatedAt = now
		// 更新明文值（入 map 前拷贝，隔离外部修改）。
		m.secrets[sk] = cloneSecretItem(item)
		// 追加新版本元信息到历史（深拷贝）。
		metaCopy := *oldMeta
		m.secretVersions[sk] = append(m.secretVersions[sk], &metaCopy)
		return cloneSecretMeta(oldMeta)
	}
	// 新建。
	meta := &SecretMeta{
		Key:       item.Key,
		KeyType:   item.KeyType,
		Version:   1,
		TenantID:  tenantID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	m.secrets[sk] = cloneSecretItem(item)
	m.secretMetas[sk] = meta
	metaCopy := *meta
	m.secretVersions[sk] = append(m.secretVersions[sk], &metaCopy)
	return cloneSecretMeta(meta)
}

// DeleteSecret 删除密钥（按 tenantID + key，含全部历史版本）。返回是否删除成功。
func (m *MemoryStore) DeleteSecret(tenantID, key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	sk := secretKey(tenantID, key)
	if _, ok := m.secrets[sk]; !ok {
		return false
	}
	delete(m.secrets, sk)
	delete(m.secretMetas, sk)
	delete(m.secretVersions, sk)
	return true
}

// ListSecrets 列出指定租户的全部密钥元信息（脱敏视图，按 key 升序）。
// 返回深拷贝副本，外部修改不影响内部状态。
func (m *MemoryStore) ListSecrets(tenantID string) []*SecretMeta {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*SecretMeta
	for _, meta := range m.secretMetas {
		if meta.TenantID != tenantID {
			continue
		}
		out = append(out, cloneSecretMeta(meta))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// RotateSecret 轮换密钥：用新值产生新版本。不存在则等价于 SetSecret（KeyType 默认 passphrase）。
// 返回新版本元信息。
func (m *MemoryStore) RotateSecret(tenantID, key, newValue string) *SecretMeta {
	m.mu.RLock()
	keyType := "passphrase"
	if existing, ok := m.secrets[secretKey(tenantID, key)]; ok {
		keyType = existing.KeyType
	}
	m.mu.RUnlock()
	return m.SetSecret(&SecretItem{Key: key, Value: newValue, KeyType: keyType}, tenantID)
}

// SecretVersions 返回指定密钥的全部版本元信息（按 version 升序）。
// 返回深拷贝副本，外部修改不影响内部状态。
func (m *MemoryStore) SecretVersions(tenantID, key string) []*SecretMeta {
	m.mu.RLock()
	defer m.mu.RUnlock()
	versions := m.secretVersions[secretKey(tenantID, key)]
	if len(versions) == 0 {
		return nil
	}
	out := make([]*SecretMeta, len(versions))
	for i, v := range versions {
		out[i] = cloneSecretMeta(v)
	}
	return out
}
