
// memory_webhook.go 实现 MemoryStore 的 WebhookStore 子接口（Phase 5 Webhook 管理）。
//
// Webhook 内存实现：
//   - webhooks / webhookDeliveries 字段在 MemoryStore struct 中定义；
//   - NewMemoryStore 中初始化为空 map；
//   - 本文件实现 6 个方法，全部经 m.mu 互斥保护，并发安全。
//
// 设计要点（与 memory_ticket.go 风格一致）：
//   - ListWebhooks / ListWebhookDeliveries 返回深拷贝避免外部修改破坏内部状态；
//   - CreateWebhook 分配随机 ID（"webhook-" + 16 字节 hex）；
//   - ListWebhooks 按创建时间降序（最新优先）。
package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"time"
)

// randWebhookID 生成随机 Webhook ID（"webhook-" + 16 字节 hex）。
func randWebhookID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("webhook-%d", time.Now().UnixNano())
	}
	return "webhook-" + hex.EncodeToString(b)
}

// randWebhookDeliveryID 生成随机投递记录 ID（"wh-delivery-" + 16 字节 hex）。
func randWebhookDeliveryID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("wh-delivery-%d", time.Now().UnixNano())
	}
	return "wh-delivery-" + hex.EncodeToString(b)
}

// cloneWebhook 返回 wh 的深拷贝（含 Events / Headers）。
func cloneWebhook(wh *Webhook) *Webhook {
	if wh == nil {
		return nil
	}
	cp := *wh
	if wh.Events != nil {
		cp.Events = append([]string(nil), wh.Events...)
	}
	if wh.Headers != nil {
		cp.Headers = make(map[string]string, len(wh.Headers))
		for k, v := range wh.Headers {
			cp.Headers[k] = v
		}
	}
	return &cp
}

// cloneWebhookDelivery 返回 d 的深拷贝。
func cloneWebhookDelivery(d *WebhookDelivery) *WebhookDelivery {
	if d == nil {
		return nil
	}
	cp := *d
	return &cp
}

// CreateWebhook 创建 Webhook（按 ID 幂等；ID 为空时分配随机 ID）。
//
// 行为：
//   - ID 为空时分配随机 ID（新建场景）；
//   - TenantID 为空时归一为 default；
//   - CreatedAt 为空时填当前时间；
//   - UpdatedAt 始终刷新为当前时间。
func (m *MemoryStore) CreateWebhook(tenantID string, wh *Webhook) *Webhook {
	if wh == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	wh.TenantID = tenantID
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if wh.ID == "" {
		wh.ID = randWebhookID()
	}
	if wh.CreatedAt.IsZero() {
		wh.CreatedAt = now
	}
	wh.UpdatedAt = now
	m.webhooks[wh.ID] = wh
	return cloneWebhook(wh)
}

// GetWebhook 按 (tenantID, id) 返回单个 Webhook（深拷贝；不存在或租户不匹配返回 (nil, false)）。
func (m *MemoryStore) GetWebhook(tenantID, id string) (*Webhook, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	wh, ok := m.webhooks[id]
	if !ok {
		return nil, false
	}
	if tenantID != "" && wh.TenantID != tenantID {
		return nil, false
	}
	return cloneWebhook(wh), true
}

// UpdateWebhook 更新 Webhook（按 wh.ID 定位，校验 tenantID 归属）。
//
// 行为：
//   - 不存在或租户不匹配返回 (nil, false)；
//   - CreatedAt / TenantID 不可改（保留原值，防越权改归属）；
//   - UpdatedAt 始终刷新为当前时间；
//   - 返回更新后的 Webhook（深拷贝）。
func (m *MemoryStore) UpdateWebhook(tenantID string, wh *Webhook) (*Webhook, bool) {
	if wh == nil || wh.ID == "" {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.webhooks[wh.ID]
	if !ok {
		return nil, false
	}
	if tenantID != "" && existing.TenantID != tenantID {
		return nil, false
	}
	wh.ID = existing.ID
	wh.TenantID = existing.TenantID
	wh.CreatedAt = existing.CreatedAt
	wh.UpdatedAt = time.Now()
	m.webhooks[wh.ID] = wh
	return cloneWebhook(wh), true
}

// ListWebhooks 返回指定租户的全部 Webhook（按创建时间降序）。
func (m *MemoryStore) ListWebhooks(tenantID string) []*Webhook {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Webhook, 0, len(m.webhooks))
	for _, wh := range m.webhooks {
		if tenantID != "" && wh.TenantID != tenantID {
			continue
		}
		out = append(out, cloneWebhook(wh))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

// DeleteWebhook 删除 Webhook，返回是否删除成功（不存在或租户不匹配返回 false）。
func (m *MemoryStore) DeleteWebhook(tenantID, id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.webhooks[id]
	if !ok {
		return false
	}
	if tenantID != "" && existing.TenantID != tenantID {
		return false
	}
	delete(m.webhooks, id)
	return true
}

// ListWebhookDeliveries 返回指定 Webhook 的投递记录（按投递时间降序）。
// 不存在或租户不匹配返回空 slice。
func (m *MemoryStore) ListWebhookDeliveries(tenantID, webhookID string) []*WebhookDelivery {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// 校验 webhook 归属。
	wh, ok := m.webhooks[webhookID]
	if !ok {
		return []*WebhookDelivery{}
	}
	if tenantID != "" && wh.TenantID != tenantID {
		return []*WebhookDelivery{}
	}
	out := make([]*WebhookDelivery, 0)
	for _, d := range m.webhookDeliveries {
		if d.WebhookID != webhookID {
			continue
		}
		if tenantID != "" && d.TenantID != tenantID {
			continue
		}
		out = append(out, cloneWebhookDelivery(d))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].DeliveredAt.After(out[j].DeliveredAt)
	})
	return out
}

// recordWebhookDeliveryLocked 在 m.mu 保护下记录一条投递记录。
// 仅供 controlplane webhook test handler 经 store 内部调用（MVP 不暴露公共方法）。
// 调用方须已持有 m.mu。
func (m *MemoryStore) recordWebhookDeliveryLocked(tenantID, webhookID, event, payload string, statusCode int, response, errStr string) *WebhookDelivery {
	now := time.Now()
	d := &WebhookDelivery{
		ID:          randWebhookDeliveryID(),
		TenantID:    tenantID,
		WebhookID:   webhookID,
		Event:       event,
		Payload:     payload,
		StatusCode:  statusCode,
		Response:    response,
		Error:       errStr,
		DeliveredAt: now,
	}
	m.webhookDeliveries[d.ID] = d
	return cloneWebhookDelivery(d)
}

// RecordWebhookDelivery 记录一条 Webhook 投递记录（供 controlplane test handler 调用）。
// 返回深拷贝的投递记录。
func (m *MemoryStore) RecordWebhookDelivery(tenantID, webhookID, event, payload string, statusCode int, response, errStr string) *WebhookDelivery {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.recordWebhookDeliveryLocked(tenantID, webhookID, event, payload, statusCode, response, errStr)
}