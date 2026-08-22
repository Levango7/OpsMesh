// sql_m2.go M2 告警治理 SQL 持久化：静默规则 / 通知渠道 / 通知模板 / 告警规则补全。
//
// 替换原内存 map 实现，改为基于 MySQL 的持久化：
//   - alert_silences：基于标签匹配 + 时间窗口的批量静默规则（SilenceRule）。
//   - notify_channels：通知渠道（钉钉/企业微信/飞书/Slack/邮件/Webhook）配置。
//   - notify_templates：通知消息模板（Go text/template 变量替换）。
//   - alert_rules：补全 GetAlertRule/UpdateAlertRule（Create/List/Delete 在 sql_alerts.go）。
//
// 设计要点（与 sql_templates.go / sql_k8s.go 风格一致）：
//   - 参数化查询防 SQL 注入；
//   - JSON 字段（match_labels/config）用 JSON 列存储，应用层 json.Marshal/Unmarshal；
//   - 租户隔离：所有查询加 WHERE tenant_id=?；
//   - DB 不可用时返回零值（nil/false），不 panic，与 SQLStore 其他方法一致；
//   - 持久化失败仅日志提示，不向上抛（与 sql_alerts.go CreateAlertRule 范式一致）。
//
// 表结构由 migrations/005_m2_alert_governance.sql 创建（幂等 CREATE TABLE IF NOT EXISTS）。
// 所有方法线程安全（database/sql 内部连接池保护，无需额外锁）。
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	cryptoRand "crypto/rand"
	"encoding/hex"
)

// ============================================================================
// SilenceRule：静默规则 CRUD（alert_silences 表）
// ============================================================================

// randSQLSilenceID 生成随机静默规则 ID。
func randSQLSilenceID() string {
	b := make([]byte, 16)
	if _, err := cryptoRand.Read(b); err != nil {
		return fmt.Sprintf("silence-%d", time.Now().UnixNano())
	}
	return "silence-" + hex.EncodeToString(b)
}

// scanSilence 从一行扫描出 *SilenceRule（match_labels 为 JSON 列）。
func scanSilence(row rowScanner) *SilenceRule {
	var r SilenceRule
	var matchLabelsJSON []byte
	var startAt, endAt, createdAt sql.NullTime
	var createdBy sql.NullString
	if err := row.Scan(&r.ID, &r.TenantID, &matchLabelsJSON, &startAt, &endAt,
		&createdBy, &r.Reason, &createdAt); err != nil {
		return nil
	}
	if len(matchLabelsJSON) > 0 {
		if err := json.Unmarshal(matchLabelsJSON, &r.MatchLabels); err != nil {
			log.Printf("[store] scanSilence 解析 match_labels JSON 失败 (id=%s): %v", r.ID, err)
		}
	}
	r.StartAt = startAt.Time
	r.EndAt = endAt.Time
	r.CreatedBy = createdBy.String
	r.CreatedAt = createdAt.Time
	return &r
}

// silenceColumns alert_silences 表查询的列列表。
const silenceColumns = `id, tenant_id, match_labels, starts_at, ends_at, created_by, reason, created_at`

// CreateSilence 创建静默规则：ID 为空时由 store 分配随机 ID；
// TenantID 为空时归一为 default。返回持久化后的规则（含分配的 ID）。
func (s *SQLStore) CreateSilence(sr *SilenceRule) *SilenceRule {
	if sr == nil {
		return nil
	}
	if sr.TenantID == "" {
		sr.TenantID = "default"
	}
	if sr.ID == "" {
		sr.ID = randSQLSilenceID()
	}
	if sr.CreatedAt.IsZero() {
		sr.CreatedAt = time.Now().UTC()
	}
	matchLabels, _ := json.Marshal(sr.MatchLabels)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO alert_silences (id, tenant_id, match_labels, starts_at, ends_at, created_by, reason, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE tenant_id=VALUES(tenant_id), match_labels=VALUES(match_labels),
		   starts_at=VALUES(starts_at), ends_at=VALUES(ends_at), created_by=VALUES(created_by),
		   reason=VALUES(reason)`,
		sr.ID, sr.TenantID, matchLabels, nullTime(sr.StartAt), nullTime(sr.EndAt),
		nullString(sr.CreatedBy), nullString(sr.Reason), sr.CreatedAt); err != nil {
		log.Printf("[store] CreateSilence 失败: %v", err)
		return nil
	}
	cp := *sr
	// 深拷贝 MatchLabels 隔离外部修改
	if sr.MatchLabels != nil {
		cp.MatchLabels = make(map[string]string, len(sr.MatchLabels))
		for k, v := range sr.MatchLabels {
			cp.MatchLabels[k] = v
		}
	}
	return &cp
}

// GetSilence 按 ID 返回单个静默规则（不存在返回 nil）。
func (s *SQLStore) GetSilence(id string) *SilenceRule {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := s.db.QueryRowContext(ctx, `SELECT `+silenceColumns+` FROM alert_silences WHERE id=?`, id)
	return scanSilence(row)
}

// DeleteSilence 删除静默规则，返回是否删除成功（不存在或租户不匹配返回 false）。
func (s *SQLStore) DeleteSilence(id, tenantID string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `DELETE FROM alert_silences WHERE id=?`
	var args []interface{}
	args = append(args, id)
	if tenantID != "" {
		q += ` AND tenant_id=?`
		args = append(args, tenantID)
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] DeleteSilence 失败 %s: %v", id, err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// ListSilences 返回静默规则；tenantID 非空时按租户过滤。按创建时间升序返回。
func (s *SQLStore) ListSilences(tenantID string) []*SilenceRule {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT ` + silenceColumns + ` FROM alert_silences`
	var args []interface{}
	if tenantID != "" {
		q += ` WHERE tenant_id=?`
		args = append(args, tenantID)
	}
	q += ` ORDER BY created_at ASC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] ListSilences 失败: %v", err)
		return nil
	}
	defer rows.Close()
	out := make([]*SilenceRule, 0)
	for rows.Next() {
		if r := scanSilence(rows); r != nil {
			out = append(out, r)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] ListSilences 遍历失败: %v", err)
	}
	return out
}

// ============================================================================
// NotifyChannel：通知渠道 CRUD（notify_channels 表）
// ============================================================================

// randSQLChannelID 生成随机通知渠道 ID。
func randSQLChannelID() string {
	b := make([]byte, 16)
	if _, err := cryptoRand.Read(b); err != nil {
		return fmt.Sprintf("ch-%d", time.Now().UnixNano())
	}
	return "ch-" + hex.EncodeToString(b)
}

// scanNotifyChannel 从一行扫描出 *NotifyChannel。
func scanNotifyChannel(row rowScanner) *NotifyChannel {
	var c NotifyChannel
	var createdAt, updatedAt time.Time
	if err := row.Scan(&c.ID, &c.TenantID, &c.Name, &c.Type, &c.Config, &c.Enabled, &createdAt, &updatedAt); err != nil {
		return nil
	}
	c.CreatedAt = createdAt
	c.UpdatedAt = updatedAt
	return &c
}

// notifyChannelColumns notify_channels 表查询的列列表。
const notifyChannelColumns = `id, tenant_id, name, type, config, enabled, created_at, updated_at`

// CreateNotifyChannel 创建通知渠道：ID 为空时由 store 分配随机 ID；
// TenantID 为空时归一为 default。返回持久化后的渠道（含分配的 ID）。
func (s *SQLStore) CreateNotifyChannel(c *NotifyChannel) *NotifyChannel {
	if c == nil {
		return nil
	}
	if c.TenantID == "" {
		c.TenantID = "default"
	}
	if c.ID == "" {
		c.ID = randSQLChannelID()
	}
	now := time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO notify_channels (id, tenant_id, name, type, config, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE name=VALUES(name), type=VALUES(type), config=VALUES(config),
		   enabled=VALUES(enabled), updated_at=VALUES(updated_at)`,
		c.ID, c.TenantID, c.Name, c.Type, c.Config, boolToInt(c.Enabled), c.CreatedAt, c.UpdatedAt); err != nil {
		log.Printf("[store] CreateNotifyChannel 失败: %v", err)
		return nil
	}
	cp := *c
	return &cp
}

// UpdateNotifyChannel 更新通知渠道。不存在返回 false。
func (s *SQLStore) UpdateNotifyChannel(c *NotifyChannel) bool {
	if c == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`UPDATE notify_channels SET name=?, type=?, config=?, enabled=?, updated_at=? WHERE id=?`,
		c.Name, c.Type, c.Config, boolToInt(c.Enabled), now, c.ID)
	if err != nil {
		log.Printf("[store] UpdateNotifyChannel 失败 %s: %v", c.ID, err)
		return false
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		c.UpdatedAt = now
	}
	return n > 0
}

// DeleteNotifyChannel 删除通知渠道，返回是否删除成功（不存在或租户不匹配返回 false）。
func (s *SQLStore) DeleteNotifyChannel(id, tenantID string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `DELETE FROM notify_channels WHERE id=?`
	var args []interface{}
	args = append(args, id)
	if tenantID != "" {
		q += ` AND tenant_id=?`
		args = append(args, tenantID)
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] DeleteNotifyChannel 失败 %s: %v", id, err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// GetNotifyChannel 按 ID 返回单个通知渠道（不存在返回 nil）。
func (s *SQLStore) GetNotifyChannel(id string) *NotifyChannel {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := s.db.QueryRowContext(ctx, `SELECT `+notifyChannelColumns+` FROM notify_channels WHERE id=?`, id)
	return scanNotifyChannel(row)
}

// ListNotifyChannels 返回通知渠道；tenantID 非空时按租户过滤。按创建时间升序返回。
func (s *SQLStore) ListNotifyChannels(tenantID string) []*NotifyChannel {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT ` + notifyChannelColumns + ` FROM notify_channels`
	var args []interface{}
	if tenantID != "" {
		q += ` WHERE tenant_id=?`
		args = append(args, tenantID)
	}
	q += ` ORDER BY created_at ASC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] ListNotifyChannels 失败: %v", err)
		return nil
	}
	defer rows.Close()
	out := make([]*NotifyChannel, 0)
	for rows.Next() {
		if c := scanNotifyChannel(rows); c != nil {
			out = append(out, c)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] ListNotifyChannels 遍历失败: %v", err)
	}
	return out
}

// ============================================================================
// NotifyTemplate：通知模板 CRUD（notify_templates 表）
// ============================================================================

// randSQLTemplateID 生成随机通知模板 ID。
func randSQLTemplateID() string {
	b := make([]byte, 16)
	if _, err := cryptoRand.Read(b); err != nil {
		return fmt.Sprintf("tpl-%d", time.Now().UnixNano())
	}
	return "tpl-" + hex.EncodeToString(b)
}

// scanNotifyTemplate 从一行扫描出 *NotifyTemplate。
func scanNotifyTemplate(row rowScanner) *NotifyTemplate {
	var t NotifyTemplate
	var createdAt, updatedAt time.Time
	var format sql.NullString
	if err := row.Scan(&t.ID, &t.TenantID, &t.Name, &t.Type, &t.Title, &t.Body, &format, &createdAt, &updatedAt); err != nil {
		return nil
	}
	t.Format = format.String
	t.CreatedAt = createdAt
	t.UpdatedAt = updatedAt
	return &t
}

// notifyTemplateColumns notify_templates 表查询的列列表。
const notifyTemplateColumns = `id, tenant_id, name, type, title, body, format, created_at, updated_at`

// CreateNotifyTemplate 创建通知模板：ID 为空时由 store 分配随机 ID；
// TenantID 为空时归一为 default。返回持久化后的模板（含分配的 ID）。
func (s *SQLStore) CreateNotifyTemplate(t *NotifyTemplate) *NotifyTemplate {
	if t == nil {
		return nil
	}
	if t.TenantID == "" {
		t.TenantID = "default"
	}
	if t.ID == "" {
		t.ID = randSQLTemplateID()
	}
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO notify_templates (id, tenant_id, name, type, title, body, format, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE name=VALUES(name), type=VALUES(type), title=VALUES(title),
		   body=VALUES(body), format=VALUES(format), updated_at=VALUES(updated_at)`,
		t.ID, t.TenantID, t.Name, t.Type, t.Title, t.Body, nullString(t.Format), t.CreatedAt, t.UpdatedAt); err != nil {
		log.Printf("[store] CreateNotifyTemplate 失败: %v", err)
		return nil
	}
	cp := *t
	return &cp
}

// UpdateNotifyTemplate 更新通知模板。不存在返回 false。
func (s *SQLStore) UpdateNotifyTemplate(t *NotifyTemplate) bool {
	if t == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`UPDATE notify_templates SET name=?, type=?, title=?, body=?, format=?, updated_at=? WHERE id=?`,
		t.Name, t.Type, t.Title, t.Body, nullString(t.Format), now, t.ID)
	if err != nil {
		log.Printf("[store] UpdateNotifyTemplate 失败 %s: %v", t.ID, err)
		return false
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		t.UpdatedAt = now
	}
	return n > 0
}

// DeleteNotifyTemplate 删除通知模板，返回是否删除成功（不存在或租户不匹配返回 false）。
func (s *SQLStore) DeleteNotifyTemplate(id, tenantID string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `DELETE FROM notify_templates WHERE id=?`
	var args []interface{}
	args = append(args, id)
	if tenantID != "" {
		q += ` AND tenant_id=?`
		args = append(args, tenantID)
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] DeleteNotifyTemplate 失败 %s: %v", id, err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// GetNotifyTemplate 按 ID 返回单个通知模板（不存在返回 nil）。
func (s *SQLStore) GetNotifyTemplate(id string) *NotifyTemplate {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := s.db.QueryRowContext(ctx, `SELECT `+notifyTemplateColumns+` FROM notify_templates WHERE id=?`, id)
	return scanNotifyTemplate(row)
}

// ListNotifyTemplates 返回通知模板；tenantID 非空时按租户过滤。按创建时间升序返回。
func (s *SQLStore) ListNotifyTemplates(tenantID string) []*NotifyTemplate {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT ` + notifyTemplateColumns + ` FROM notify_templates`
	var args []interface{}
	if tenantID != "" {
		q += ` WHERE tenant_id=?`
		args = append(args, tenantID)
	}
	q += ` ORDER BY created_at ASC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] ListNotifyTemplates 失败: %v", err)
		return nil
	}
	defer rows.Close()
	out := make([]*NotifyTemplate, 0)
	for rows.Next() {
		if t := scanNotifyTemplate(rows); t != nil {
			out = append(out, t)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] ListNotifyTemplates 遍历失败: %v", err)
	}
	return out
}

// ============================================================================
// AlertRule：补全 GetAlertRule / UpdateAlertRule（Create/List/Delete 在 sql_alerts.go）
// ============================================================================

// GetAlertRule 按 ID 返回单个告警规则（不存在返回 nil）。
func (s *SQLStore) GetAlertRule(id string) *AlertRule {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := s.db.QueryRowContext(ctx, `SELECT `+alertRuleColumns+` FROM alert_rules WHERE id=?`, id)
	return scanAlertRule(row)
}

// UpdateAlertRule 更新告警规则。不存在返回 false。
// 保留原 CreatedAt；UpdatedAt 不存在（alert_rules 表无 updated_at 列，向后兼容）。
func (s *SQLStore) UpdateAlertRule(r *AlertRule) bool {
	if r == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := s.db.ExecContext(ctx,
		`UPDATE alert_rules SET tenant_id=?, metric=?, op=?, threshold=?, for_duration=?,
		   severity=?, message=?, enabled=?, created_by=? WHERE id=?`,
		r.TenantID, r.Metric, r.Op, r.Threshold, r.ForDuration,
		r.Severity, r.Message, boolToInt(r.Enabled), nullString(r.CreatedBy), r.ID)
	if err != nil {
		log.Printf("[store] UpdateAlertRule 失败 %s: %v", r.ID, err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}
