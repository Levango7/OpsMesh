// memory_alertgov.go — 告警治理：静默规则 / 通知渠道 / 通知模板（M2/M7）。
package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// ============================================================================

// randSilenceID 生成随机静默规则 ID（16 字节十六进制，crypto/rand 密码学安全）。
func randSilenceID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("silence-%d", time.Now().UnixNano())
	}
	return "silence-" + hex.EncodeToString(b)
}

// CreateSilence 创建静默规则：ID 为空时由 store 分配随机 ID；
// TenantID 为空时归一为 default。返回持久化后的规则（含分配的 ID）。
func (m *MemoryStore) CreateSilence(s *SilenceRule) *SilenceRule {
	if s == nil {
		return nil
	}
	if s.TenantID == "" {
		s.TenantID = "default"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.ID == "" {
		s.ID = randSilenceID()
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now()
	}
	stored := *s
	// 深拷贝 MatchLabels 隔离外部修改
	if s.MatchLabels != nil {
		stored.MatchLabels = make(map[string]string, len(s.MatchLabels))
		for k, v := range s.MatchLabels {
			stored.MatchLabels[k] = v
		}
	}
	m.silences[s.ID] = &stored
	ret := *s
	if s.MatchLabels != nil {
		ret.MatchLabels = make(map[string]string, len(s.MatchLabels))
		for k, v := range s.MatchLabels {
			ret.MatchLabels[k] = v
		}
	}
	return &ret
}

// DeleteSilence 删除静默规则，返回是否删除成功（不存在或租户不匹配返回 false）。
func (m *MemoryStore) DeleteSilence(id, tenantID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.silences[id]
	if !ok {
		return false
	}
	if tenantID != "" && r.TenantID != tenantID {
		return false
	}
	delete(m.silences, id)
	return true
}

// ListSilences 返回静默规则；tenantID 非空时按租户过滤。按创建时间升序返回深拷贝。
func (m *MemoryStore) ListSilences(tenantID string) []*SilenceRule {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*SilenceRule, 0, len(m.silences))
	for _, r := range m.silences {
		if tenantID != "" && r.TenantID != tenantID {
			continue
		}
		cp := *r
		if r.MatchLabels != nil {
			cp.MatchLabels = make(map[string]string, len(r.MatchLabels))
			for k, v := range r.MatchLabels {
				cp.MatchLabels[k] = v
			}
		}
		out = append(out, &cp)
	}
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j].CreatedAt.Before(out[j-1].CreatedAt) {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}

// ---------- NotifyChannel ----------

// randNotifyChannelID 生成随机通知渠道 ID。
func randNotifyChannelID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("ch-%d", time.Now().UnixNano())
	}
	return "ch-" + hex.EncodeToString(b)
}

// CreateNotifyChannel 创建通知渠道：ID 为空时由 store 分配随机 ID；
// TenantID 为空时归一为 default。返回持久化后的渠道（含分配的 ID）。
func (m *MemoryStore) CreateNotifyChannel(c *NotifyChannel) *NotifyChannel {
	if c == nil {
		return nil
	}
	if c.TenantID == "" {
		c.TenantID = "default"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if c.ID == "" {
		c.ID = randNotifyChannelID()
	}
	now := time.Now()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	stored := *c
	m.notifyChannels[c.ID] = &stored
	ret := *c
	return &ret
}

// UpdateNotifyChannel 更新通知渠道。不存在返回 false。
func (m *MemoryStore) UpdateNotifyChannel(c *NotifyChannel) bool {
	if c == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.notifyChannels[c.ID]
	if !ok {
		return false
	}
	stored := *c
	stored.CreatedAt = old.CreatedAt // 保留原创建时间
	stored.UpdatedAt = time.Now()
	m.notifyChannels[c.ID] = &stored
	return true
}

// DeleteNotifyChannel 删除通知渠道，返回是否删除成功（不存在或租户不匹配返回 false）。
func (m *MemoryStore) DeleteNotifyChannel(id, tenantID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.notifyChannels[id]
	if !ok {
		return false
	}
	if tenantID != "" && c.TenantID != tenantID {
		return false
	}
	delete(m.notifyChannels, id)
	return true
}

// GetNotifyChannel 按 ID 返回单个通知渠道（深拷贝；不存在返回 nil）。
func (m *MemoryStore) GetNotifyChannel(id string) *NotifyChannel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.notifyChannels[id]
	if !ok {
		return nil
	}
	cp := *c
	return &cp
}

// ListNotifyChannels 返回通知渠道；tenantID 非空时按租户过滤。按创建时间升序返回深拷贝。
func (m *MemoryStore) ListNotifyChannels(tenantID string) []*NotifyChannel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*NotifyChannel, 0, len(m.notifyChannels))
	for _, c := range m.notifyChannels {
		if tenantID != "" && c.TenantID != tenantID {
			continue
		}
		cp := *c
		out = append(out, &cp)
	}
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j].CreatedAt.Before(out[j-1].CreatedAt) {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}

// ---------- NotifyTemplate ----------

// randNotifyTemplateID 生成随机通知模板 ID。
func randNotifyTemplateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("tpl-%d", time.Now().UnixNano())
	}
	return "tpl-" + hex.EncodeToString(b)
}

// CreateNotifyTemplate 创建通知模板：ID 为空时由 store 分配随机 ID；
// TenantID 为空时归一为 default。返回持久化后的模板（含分配的 ID）。
func (m *MemoryStore) CreateNotifyTemplate(t *NotifyTemplate) *NotifyTemplate {
	if t == nil {
		return nil
	}
	if t.TenantID == "" {
		t.TenantID = "default"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if t.ID == "" {
		t.ID = randNotifyTemplateID()
	}
	now := time.Now()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	stored := *t
	m.notifyTemplates[t.ID] = &stored
	ret := *t
	return &ret
}

// UpdateNotifyTemplate 更新通知模板。不存在返回 false。
func (m *MemoryStore) UpdateNotifyTemplate(t *NotifyTemplate) bool {
	if t == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.notifyTemplates[t.ID]
	if !ok {
		return false
	}
	stored := *t
	stored.CreatedAt = old.CreatedAt
	stored.UpdatedAt = time.Now()
	m.notifyTemplates[t.ID] = &stored
	return true
}

// DeleteNotifyTemplate 删除通知模板，返回是否删除成功（不存在或租户不匹配返回 false）。
func (m *MemoryStore) DeleteNotifyTemplate(id, tenantID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.notifyTemplates[id]
	if !ok {
		return false
	}
	if tenantID != "" && t.TenantID != tenantID {
		return false
	}
	delete(m.notifyTemplates, id)
	return true
}

// GetNotifyTemplate 按 ID 返回单个通知模板（深拷贝；不存在返回 nil）。
func (m *MemoryStore) GetNotifyTemplate(id string) *NotifyTemplate {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.notifyTemplates[id]
	if !ok {
		return nil
	}
	cp := *t
	return &cp
}

// ListNotifyTemplates 返回通知模板；tenantID 非空时按租户过滤。按创建时间升序返回深拷贝。
func (m *MemoryStore) ListNotifyTemplates(tenantID string) []*NotifyTemplate {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*NotifyTemplate, 0, len(m.notifyTemplates))
	for _, t := range m.notifyTemplates {
		if tenantID != "" && t.TenantID != tenantID {
			continue
		}
		cp := *t
		out = append(out, &cp)
	}
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j].CreatedAt.Before(out[j-1].CreatedAt) {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}
