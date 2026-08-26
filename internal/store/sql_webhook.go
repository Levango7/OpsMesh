// sql_webhook.go 实现 SQLStore 的 WebhookStore 子接口（Phase 5 Webhook 管理）。
//
// 【未实现的桩】本文件全部方法未接入 MySQL 持久化，经 stub_guard.StubNotImplemented
// 显式告警（每 (domain,method) 首次必打、60s 限频），并按统一约定返回零值：
//   - CreateWebhook 返回 nil（不返回填充后的假对象——杜绝「201 假成功 → GET 404」链路）；
//   - Get/Update/Delete 返回 (nil, false) / false；List 类返回非 nil 空切片。
//
// TODO(p5): 接入 MySQL 持久化（webhooks 表：id PK + tenant_id + name + url +
// events JSON + headers JSON + body_template + enabled + retry_count +
// retry_interval_sec + created_at + updated_at；webhook_deliveries 表：id PK +
// tenant_id + webhook_id + event + payload + status_code + response + error +
// delivered_at）。
package store

// CreateWebhook 创建 Webhook（未实现的桩）。
// TODO(p5): 落库 webhooks 表（INSERT ... ON DUPLICATE KEY UPDATE）。
func (s *SQLStore) CreateWebhook(tenantID string, wh *Webhook) *Webhook {
	StubNotImplemented("webhook", "CreateWebhook")
	return nil
}

// GetWebhook 按 (tenantID, id) 返回单个 Webhook（未实现的桩）。
// TODO(p5): SELECT * FROM webhooks WHERE id=? AND tenant_id=?。
func (s *SQLStore) GetWebhook(tenantID, id string) (*Webhook, bool) {
	StubNotImplemented("webhook", "GetWebhook")
	return nil, false
}

// UpdateWebhook 更新 Webhook（未实现的桩）。
// TODO(p5): UPDATE webhooks SET ... WHERE id=? AND tenant_id=?。
func (s *SQLStore) UpdateWebhook(tenantID string, wh *Webhook) (*Webhook, bool) {
	StubNotImplemented("webhook", "UpdateWebhook")
	return nil, false
}

// ListWebhooks 返回指定租户的全部 Webhook（未实现的桩；返回非 nil 空切片防上层 range panic）。
// TODO(p5): SELECT * FROM webhooks WHERE tenant_id=? ORDER BY created_at DESC。
func (s *SQLStore) ListWebhooks(tenantID string) []*Webhook {
	StubNotImplemented("webhook", "ListWebhooks")
	return []*Webhook{}
}

// DeleteWebhook 删除 Webhook（未实现的桩）。
// TODO(p5): DELETE FROM webhooks WHERE id=? AND tenant_id=?。
func (s *SQLStore) DeleteWebhook(tenantID, id string) bool {
	StubNotImplemented("webhook", "DeleteWebhook")
	return false
}

// ListWebhookDeliveries 返回指定 Webhook 的投递记录（未实现的桩；返回非 nil 空切片）。
// TODO(p5): SELECT * FROM webhook_deliveries WHERE webhook_id=? AND tenant_id=?
// ORDER BY delivered_at DESC。
func (s *SQLStore) ListWebhookDeliveries(tenantID, webhookID string) []*WebhookDelivery {
	StubNotImplemented("webhook", "ListWebhookDeliveries")
	return []*WebhookDelivery{}
}

// RecordWebhookDelivery 记录一条 Webhook 投递记录（未实现的桩；返回 nil）。
// controlplane webhook test handler 据此降级为模拟响应（不落库）。
// TODO(p5): INSERT INTO webhook_deliveries (id, tenant_id, webhook_id, event, payload,
// status_code, response, error, delivered_at) VALUES (...)。
func (s *SQLStore) RecordWebhookDelivery(tenantID, webhookID, event, payload string, statusCode int, response, errStr string) *WebhookDelivery {
	StubNotImplemented("webhook", "RecordWebhookDelivery")
	return nil
}
