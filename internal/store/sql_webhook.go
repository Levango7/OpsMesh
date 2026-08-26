// sql_webhook.go 实现 SQLStore 的 WebhookStore 子接口（Phase 5 Webhook 管理，生产就绪）。
//
// 表结构：webhooks（id PK + tenant_id + name + url + events JSON + headers JSON +
// body_template + enabled TINYINT(1) + retry_count + retry_interval_sec +
// created_at + updated_at）+ webhook_deliveries（id PK + tenant_id + webhook_id +
// event + payload + status_code + response + error + delivered_at）。迁移文件
// migrations/014_p5_script_webhook.sql 幂等建表。
//
// 设计要点（与 sql_slo.go / sql_ticket.go 风格一致）：
//   - JSON 列：webhooks.events（[]string）+ webhooks.headers（map[string]string），
//     用 encoding/json.Marshal/Unmarshal 序列化为 TEXT；空值存空串，读取时空串跳过 Unmarshal；
//   - bool 列 enabled 用 TINYINT(1)，默认 1；
//   - CreateWebhook 按 ID 幂等（INSERT ... ON DUPLICATE KEY UPDATE），tenant_id 仅插入
//     不更新（防 upsert 改写归属）；
//   - ListWebhooks 按创建时间降序（最新优先，与 memory 一致）；
//   - UpdateWebhook 先 SELECT 校验存在 + 租户归属，再 UPDATE，保留原 CreatedAt/TenantID；
//   - ListWebhookDeliveries 按投递时间降序；
//   - RecordWebhookDelivery：ID 由 store 分配（randWebhookDeliveryID()，前缀 wh-delivery-），
//     DeliveredAt 填 now；
//   - DB 不可用时返回零值（nil/false/空 slice），不 panic；
//   - ID 生成复用 memory_webhook.go 的 randWebhookID（"webhook-" + 16 字节 hex）+
//     randWebhookDeliveryID（"wh-delivery-" + 16 字节 hex）。
package store

import (
	"context"
	"encoding/json"
	"log"
	"time"
)

// scanWebhook 从一行扫描出 *Webhook（events / headers 为 JSON 文本列）。
// 列顺序：id, tenant_id, name, url, events, headers, body_template, enabled,
// retry_count, retry_interval_sec, created_at, updated_at。无行或扫描失败返回 nil。
func scanWebhook(row rowScanner) *Webhook {
	var wh Webhook
	var eventsJSON, headersJSON string
	var enabled int
	var createdAt, updatedAt time.Time
	if err := row.Scan(&wh.ID, &wh.TenantID, &wh.Name, &wh.URL, &eventsJSON, &headersJSON,
		&wh.BodyTemplate, &enabled, &wh.RetryCount, &wh.RetryIntervalSec, &createdAt, &updatedAt); err != nil {
		return nil
	}
	wh.Enabled = enabled != 0
	wh.CreatedAt = createdAt
	wh.UpdatedAt = updatedAt
	if eventsJSON != "" {
		if err := json.Unmarshal([]byte(eventsJSON), &wh.Events); err != nil {
			log.Printf("[store] scanWebhook 解析 events JSON 失败 (webhook=%s): %v", wh.ID, err)
		}
	}
	if headersJSON != "" {
		if err := json.Unmarshal([]byte(headersJSON), &wh.Headers); err != nil {
			log.Printf("[store] scanWebhook 解析 headers JSON 失败 (webhook=%s): %v", wh.ID, err)
		}
	}
	return &wh
}

// webhookEnabledInt 将 bool 转换为 TINYINT(1) 用的 int（true→1，false→0）。
func webhookEnabledInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// marshalWebhookEvents 将 Events 序列化为 JSON 文本（空切片存空串）。
func marshalWebhookEvents(events []string) string {
	if events == nil {
		return ""
	}
	b, err := json.Marshal(events)
	if err != nil {
		return ""
	}
	return string(b)
}

// marshalWebhookHeaders 将 Headers 序列化为 JSON 文本（空 map 存空串）。
func marshalWebhookHeaders(headers map[string]string) string {
	if headers == nil {
		return ""
	}
	b, err := json.Marshal(headers)
	if err != nil {
		return ""
	}
	return string(b)
}

// CreateWebhook 创建 Webhook（按 ID 幂等；ID 为空时分配随机 ID）。
//
// 行为：
//   - wh == nil 返回 nil；
//   - TenantID 为空时归一为 default；
//   - ID 为空时分配随机 ID（新建场景）；
//   - CreatedAt 为零值时填当前时间（新建场景）；
//   - UpdatedAt 始终刷新为当前时间；
//   - INSERT ... ON DUPLICATE KEY UPDATE 实现 upsert（按 id 幂等），
//     tenant_id 仅插入不更新，防 upsert 改写归属；
//   - DB 失败时 log.Printf + 返回 nil。
func (s *SQLStore) CreateWebhook(tenantID string, wh *Webhook) *Webhook {
	if wh == nil {
		return nil
	}
	// 租户隔离：空租户归一为 default。
	if tenantID == "" {
		tenantID = "default"
	}
	wh.TenantID = tenantID
	now := time.Now().UTC()
	if wh.ID == "" {
		wh.ID = randWebhookID()
	}
	if wh.CreatedAt.IsZero() {
		wh.CreatedAt = now
	}
	wh.UpdatedAt = now
	eventsJSON := marshalWebhookEvents(wh.Events)
	headersJSON := marshalWebhookHeaders(wh.Headers)
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO webhooks (id, tenant_id, name, url, events, headers, body_template, enabled, retry_count, retry_interval_sec, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE name=VALUES(name), url=VALUES(url), events=VALUES(events),
		 headers=VALUES(headers), body_template=VALUES(body_template), enabled=VALUES(enabled),
		 retry_count=VALUES(retry_count), retry_interval_sec=VALUES(retry_interval_sec), updated_at=VALUES(updated_at)`,
		wh.ID, wh.TenantID, wh.Name, wh.URL, eventsJSON, headersJSON, wh.BodyTemplate,
		webhookEnabledInt(wh.Enabled), wh.RetryCount, wh.RetryIntervalSec, wh.CreatedAt, wh.UpdatedAt); err != nil {
		log.Printf("[store] CreateWebhook 插入失败 (tenant=%s webhook=%s): %v", tenantID, wh.ID, err)
		return nil
	}
	return cloneWebhook(wh)
}

// GetWebhook 按 (tenantID, id) 返回单个 Webhook（深拷贝；不存在或租户不匹配返回 (nil, false)）。
func (s *SQLStore) GetWebhook(tenantID, id string) (*Webhook, bool) {
	row := s.db.QueryRowContext(context.Background(),
		`SELECT id, tenant_id, name, url, events, headers, body_template, enabled, retry_count, retry_interval_sec, created_at, updated_at
		  FROM webhooks WHERE id=? AND tenant_id=?`, id, tenantID)
	wh := scanWebhook(row)
	if wh == nil {
		return nil, false
	}
	return wh, true
}

// UpdateWebhook 更新 Webhook（按 wh.ID 定位，校验 tenantID 归属）。
//
// 行为：
//   - wh == nil 或 ID 为空返回 (nil, false)；
//   - 先 GetWebhook 校验存在 + 租户归属，不存在返回 (nil, false)；
//   - CreatedAt / TenantID 不可改（保留原值，防越权改归属）；
//   - UpdatedAt 始终刷新为当前时间；
//   - 返回更新后的 Webhook（深拷贝）。
func (s *SQLStore) UpdateWebhook(tenantID string, wh *Webhook) (*Webhook, bool) {
	if wh == nil || wh.ID == "" {
		return nil, false
	}
	// 先 SELECT 校验存在 + 租户归属。
	existing, ok := s.GetWebhook(tenantID, wh.ID)
	if !ok {
		return nil, false
	}
	// 保留不可改字段。
	wh.ID = existing.ID
	wh.TenantID = existing.TenantID
	wh.CreatedAt = existing.CreatedAt
	wh.UpdatedAt = time.Now().UTC()
	eventsJSON := marshalWebhookEvents(wh.Events)
	headersJSON := marshalWebhookHeaders(wh.Headers)
	if _, err := s.db.ExecContext(context.Background(),
		`UPDATE webhooks SET name=?, url=?, events=?, headers=?, body_template=?, enabled=?, retry_count=?, retry_interval_sec=?, updated_at=?
		 WHERE id=? AND tenant_id=?`,
		wh.Name, wh.URL, eventsJSON, headersJSON, wh.BodyTemplate,
		webhookEnabledInt(wh.Enabled), wh.RetryCount, wh.RetryIntervalSec, wh.UpdatedAt,
		wh.ID, wh.TenantID); err != nil {
		log.Printf("[store] UpdateWebhook 更新失败 (tenant=%s webhook=%s): %v", tenantID, wh.ID, err)
		return nil, false
	}
	return cloneWebhook(wh), true
}

// ListWebhooks 返回指定租户的全部 Webhook（按创建时间降序；深拷贝）。
func (s *SQLStore) ListWebhooks(tenantID string) []*Webhook {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT id, tenant_id, name, url, events, headers, body_template, enabled, retry_count, retry_interval_sec, created_at, updated_at
		  FROM webhooks WHERE tenant_id=? ORDER BY created_at DESC`, tenantID)
	if err != nil {
		log.Printf("[store] ListWebhooks 查询失败 (tenant=%s): %v", tenantID, err)
		return []*Webhook{}
	}
	defer rows.Close()
	out := make([]*Webhook, 0)
	for rows.Next() {
		if wh := scanWebhook(rows); wh != nil {
			out = append(out, wh)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] ListWebhooks 遍历失败: %v", err)
	}
	return out
}

// DeleteWebhook 删除 Webhook，返回是否删除成功（不存在或租户不匹配返回 false）。
func (s *SQLStore) DeleteWebhook(tenantID, id string) bool {
	res, err := s.db.ExecContext(context.Background(),
		`DELETE FROM webhooks WHERE id=? AND tenant_id=?`, id, tenantID)
	if err != nil {
		log.Printf("[store] DeleteWebhook 失败 (tenant=%s webhook=%s): %v", tenantID, id, err)
		return false
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		log.Printf("[store] DeleteWebhook RowsAffected 失败 (tenant=%s webhook=%s): %v", tenantID, id, rowsErr)
		return false
	}
	return n > 0
}

// scanWebhookDelivery 从一行扫描出 *WebhookDelivery。
// 列顺序：id, tenant_id, webhook_id, event, payload, status_code, response, error,
// delivered_at。无行或扫描失败返回 nil。
func scanWebhookDelivery(row rowScanner) *WebhookDelivery {
	var d WebhookDelivery
	var deliveredAt time.Time
	if err := row.Scan(&d.ID, &d.TenantID, &d.WebhookID, &d.Event, &d.Payload,
		&d.StatusCode, &d.Response, &d.Error, &deliveredAt); err != nil {
		return nil
	}
	d.DeliveredAt = deliveredAt
	return &d
}

// ListWebhookDeliveries 返回指定 Webhook 的投递记录（按投递时间降序；深拷贝）。
// 不存在或租户不匹配返回空 slice。
func (s *SQLStore) ListWebhookDeliveries(tenantID, webhookID string) []*WebhookDelivery {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT id, tenant_id, webhook_id, event, payload, status_code, response, error, delivered_at
		  FROM webhook_deliveries WHERE tenant_id=? AND webhook_id=? ORDER BY delivered_at DESC`,
		tenantID, webhookID)
	if err != nil {
		log.Printf("[store] ListWebhookDeliveries 查询失败 (tenant=%s webhook=%s): %v", tenantID, webhookID, err)
		return []*WebhookDelivery{}
	}
	defer rows.Close()
	out := make([]*WebhookDelivery, 0)
	for rows.Next() {
		if d := scanWebhookDelivery(rows); d != nil {
			out = append(out, d)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] ListWebhookDeliveries 遍历失败: %v", err)
	}
	return out
}

// RecordWebhookDelivery 记录一条 Webhook 投递记录（供 controlplane test handler 调用）。
//
// 行为：
//   - TenantID 为空时归一为 default；
//   - ID 由 store 分配（randWebhookDeliveryID()，前缀 wh-delivery-）；
//   - DeliveredAt 填当前时间；
//   - DB 失败时 log.Printf + 返回 nil。
func (s *SQLStore) RecordWebhookDelivery(tenantID, webhookID, event, payload string, statusCode int, response, errStr string) *WebhookDelivery {
	if tenantID == "" {
		tenantID = "default"
	}
	now := time.Now().UTC()
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
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO webhook_deliveries (id, tenant_id, webhook_id, event, payload, status_code, response, error, delivered_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.TenantID, d.WebhookID, d.Event, d.Payload, d.StatusCode, d.Response, d.Error, d.DeliveredAt); err != nil {
		log.Printf("[store] RecordWebhookDelivery 插入失败 (tenant=%s webhook=%s delivery=%s): %v", tenantID, webhookID, d.ID, err)
		return nil
	}
	return cloneWebhookDelivery(d)
}
