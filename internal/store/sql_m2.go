
// sql_m2.go task 241 M2 集成：SQLStore 的静默规则 / 通知渠道 / 通知模板 内存实现。
//
// 当前用内存 map 实现 API 闭环（与 alertRules 同范式 TODO）；
// 后续 migration 落库后多副本 HA 下数据一致。所有方法线程安全（s.mu 保护）。
package store

import (
	"fmt"
	"time"

	cryptoRand "crypto/rand"
	"encoding/hex"
)

// randSQLSilenceID 生成随机静默规则 ID。
func randSQLSilenceID() string {
	b := make([]byte, 16)
	if _, err := cryptoRand.Read(b); err != nil {
		return fmt.Sprintf("silence-%d", time.Now().UnixNano())
	}
	return "silence-" + hex.EncodeToString(b)
}

// CreateSilence 创建静默规则。
func (s *SQLStore) CreateSilence(sr *SilenceRule) *SilenceRule {
	if sr == nil {
		return nil
	}
	if sr.TenantID == "" {
		sr.TenantID = "default"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if sr.ID == "" {
		sr.ID = randSQLSilenceID()
	}
	if sr.CreatedAt.IsZero() {
		sr.CreatedAt = time.Now()
	}
	stored := *sr
	if sr.MatchLabels != nil {
		stored.MatchLabels = make(map[string]string, len(sr.MatchLabels))
		for k, v := range sr.MatchLabels {
			stored.MatchLabels[k] = v
		}
	}
	s.silences[sr.ID] = &stored
	ret := *sr
	if sr.MatchLabels != nil {
		ret.MatchLabels = make(map[string]string, len(sr.MatchLabels))
		for k, v := range sr.MatchLabels {
			ret.MatchLabels[k] = v
		}
	}
	return &ret
}

// DeleteSilence 删除静默规则。
func (s *SQLStore) DeleteSilence(id, tenantID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.silences[id]
	if !ok {
		return false
	}
	if tenantID != "" && r.TenantID != tenantID {
		return false
	}
	delete(s.silences, id)
	return true
}

// ListSilences 返回静默规则。
func (s *SQLStore) ListSilences(tenantID string) []*SilenceRule {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*SilenceRule, 0, len(s.silences))
	for _, r := range s.silences {
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

// randSQLChannelID 生成随机通知渠道 ID。
func randSQLChannelID() string {
	b := make([]byte, 16)
	if _, err := cryptoRand.Read(b); err != nil {
		return fmt.Sprintf("ch-%d", time.Now().UnixNano())
	}
	return "ch-" + hex.EncodeToString(b)
}

// CreateNotifyChannel 创建通知渠道。
func (s *SQLStore) CreateNotifyChannel(c *NotifyChannel) *NotifyChannel {
	if c == nil {
		return nil
	}
	if c.TenantID == "" {
		c.TenantID = "default"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.ID == "" {
		c.ID = randSQLChannelID()
	}
	now := time.Now()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	stored := *c
	s.notifyChannels[c.ID] = &stored
	ret := *c
	return &ret
}

// UpdateNotifyChannel 更新通知渠道。
func (s *SQLStore) UpdateNotifyChannel(c *NotifyChannel) bool {
	if c == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.notifyChannels[c.ID]
	if !ok {
		return false
	}
	stored := *c
	stored.CreatedAt = old.CreatedAt
	stored.UpdatedAt = time.Now()
	s.notifyChannels[c.ID] = &stored
	return true
}

// DeleteNotifyChannel 删除通知渠道。
func (s *SQLStore) DeleteNotifyChannel(id, tenantID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.notifyChannels[id]
	if !ok {
		return false
	}
	if tenantID != "" && c.TenantID != tenantID {
		return false
	}
	delete(s.notifyChannels, id)
	return true
}

// GetNotifyChannel 按 ID 返回单个通知渠道。
func (s *SQLStore) GetNotifyChannel(id string) *NotifyChannel {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.notifyChannels[id]
	if !ok {
		return nil
	}
	cp := *c
	return &cp
}

// ListNotifyChannels 返回通知渠道。
func (s *SQLStore) ListNotifyChannels(tenantID string) []*NotifyChannel {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*NotifyChannel, 0, len(s.notifyChannels))
	for _, c := range s.notifyChannels {
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

// randSQLTemplateID 生成随机通知模板 ID。
func randSQLTemplateID() string {
	b := make([]byte, 16)
	if _, err := cryptoRand.Read(b); err != nil {
		return fmt.Sprintf("tpl-%d", time.Now().UnixNano())
	}
	return "tpl-" + hex.EncodeToString(b)
}

// CreateNotifyTemplate 创建通知模板。
func (s *SQLStore) CreateNotifyTemplate(t *NotifyTemplate) *NotifyTemplate {
	if t == nil {
		return nil
	}
	if t.TenantID == "" {
		t.TenantID = "default"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if t.ID == "" {
		t.ID = randSQLTemplateID()
	}
	now := time.Now()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	stored := *t
	s.notifyTemplates[t.ID] = &stored
	ret := *t
	return &ret
}

// UpdateNotifyTemplate 更新通知模板。
func (s *SQLStore) UpdateNotifyTemplate(t *NotifyTemplate) bool {
	if t == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.notifyTemplates[t.ID]
	if !ok {
		return false
	}
	stored := *t
	stored.CreatedAt = old.CreatedAt
	stored.UpdatedAt = time.Now()
	s.notifyTemplates[t.ID] = &stored
	return true
}

// DeleteNotifyTemplate 删除通知模板。
func (s *SQLStore) DeleteNotifyTemplate(id, tenantID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.notifyTemplates[id]
	if !ok {
		return false
	}
	if tenantID != "" && t.TenantID != tenantID {
		return false
	}
	delete(s.notifyTemplates, id)
	return true
}

// GetNotifyTemplate 按 ID 返回单个通知模板。
func (s *SQLStore) GetNotifyTemplate(id string) *NotifyTemplate {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.notifyTemplates[id]
	if !ok {
		return nil
	}
	cp := *t
	return &cp
}

// ListNotifyTemplates 返回通知模板。
func (s *SQLStore) ListNotifyTemplates(tenantID string) []*NotifyTemplate {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*NotifyTemplate, 0, len(s.notifyTemplates))
	for _, t := range s.notifyTemplates {
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