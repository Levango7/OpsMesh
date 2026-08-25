
// sql_webhook.go 实现 SQLStore 的 WebhookStore 子接口（Phase 5 Webhook 管理，桩实现）。
//
// TODO(p5): 接入 MySQL 持久化（webhooks 表：id PK + tenant_id + name + url +
// events JSON + headers JSON + body_template + enabled + retry_count +
// retry_interval_sec + created_at + updated_at；webhook_deliveries 表：id PK +
// tenant_id + webhook_id + event + payload + status_code + response + error +
// delivered_at）。
// MVP 用内存桩实现，保证接口齐全 + go build 通过；DB 不可用时返回零值，
// 不 panic，与 SQLStore 其他桩方法风格一致（参考 sql_ticket.go）。
package store

import "time"

// CreateWebhook 创建 Webhook（桩实现）。
// TODO(p5): 落库 webhooks 表（INSERT ... ON DUPLICATE KEY UPDATE）。
// MVP：DB 不可用时返回 wh（不持久化），保证接口齐全。
func (s *SQLStore) CreateWebhook(tenantID string, wh *Webhook) *Webhook {
	if wh == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	wh.TenantID = tenantID
	if wh.ID == "" {
		wh.ID = randWebhookID()
	}
	now := time.Now().UTC()
	if wh.CreatedAt.IsZero() {
		wh.CreatedAt = now
	}
	wh.UpdatedAt = now
	return wh
}

// GetWebhook 按 (tenantID, id) 返回单个 Webhook（桩实现）。
// TODO(p5): SELECT * FROM webhooks WHERE id=? AND tenant_id=?。
// MVP：DB 不可用时返回 (nil, false)。
func (s *SQLStore) GetWebhook(tenantID, id string) (*Webhook, bool) {
	return nil, false
}

// UpdateWebhook 更新 Webhook（桩实现）。
// TODO(p5): UPDATE webhooks SET ... WHERE id=? AND tenant_id=?。
// MVP：DB 不可用时返回 (nil, false)。
func (s *SQLStore) UpdateWebhook(tenantID string, wh *Webhook) (*Webhook, bool) {
	return nil, false
}

// ListWebhooks 返回指定租户的全部 Webhook（桩实现）。
// TODO(p5): SELECT * FROM webhooks WHERE tenant_id=? ORDER BY created_at DESC。
// MVP：DB 不可用时返回空 slice（非 nil，便于调用方 range）。
func (s *SQLStore) ListWebhooks(tenantID string) []*Webhook {
	return []*Webhook{}
}

// DeleteWebhook 删除 Webhook（桩实现）。
// TODO(p5): DELETE FROM webhooks WHERE id=? AND tenant_id=?。
// MVP：DB 不可用时返回 false。
func (s *SQLStore) DeleteWebhook(tenantID, id string) bool {
	return false
}

// ListWebhookDeliveries 返回指定 Webhook 的投递记录（桩实现）。
// TODO(p5): SELECT * FROM webhook_deliveries WHERE webhook_id=? AND tenant_id=?
// ORDER BY delivered_at DESC。
// MVP：DB 不可用时返回空 slice。
func (s *SQLStore) ListWebhookDeliveries(tenantID, webhookID string) []*WebhookDelivery {
	return []*WebhookDelivery{}
}