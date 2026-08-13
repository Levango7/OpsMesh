// sql_alerts.go 实现 SQLStore 的 AlertStore 子接口（告警 + 告警规则 CRUD）。
//
// 涵盖：Alerts/AddAlert/Alert/AckAlert/SilenceAlert/addAlert helper +
// task 100 新增的 CreateAlertRule/ListAlertRules/DeleteAlertRule/scanAlertRule。
//
// 表结构：alerts（告警事件）、alert_rules（task 100，sql.go initSchema 中幂等建表）。
package store

import (
	"context"
	"database/sql"

	"log"
	"time"

	"opsmesh/internal/proto"
)

// scanAlertRule 从一行扫描出 *AlertRule。
func scanAlertRule(row rowScanner) *AlertRule {
	var r AlertRule
	var createdAt time.Time
	var createdBy sql.NullString
	if err := row.Scan(&r.ID, &r.TenantID, &r.Metric, &r.Op, &r.Threshold,
		&r.ForDuration, &r.Severity, &r.Message, &r.Enabled, &createdAt, &createdBy); err != nil {
		return nil
	}
	r.CreatedAt = createdAt
	r.CreatedBy = createdBy.String
	return &r
}

// alertRuleColumns alert_rules 表查询的列列表（含 created_by，task 246 M2 持久化）。
const alertRuleColumns = `id, tenant_id, metric, op, threshold, for_duration, severity, message, enabled, created_at, created_by`

// CreateAlertRule 创建告警规则（task 100）：ID 为空时由 store 分配随机 ID；
// TenantID 为空时归一为 default。返回持久化后的规则（含分配的 ID）。
func (s *SQLStore) CreateAlertRule(r *AlertRule) *AlertRule {
	if r == nil {
		return nil
	}
	if r.TenantID == "" {
		r.TenantID = "default"
	}
	if r.ID == "" {
		r.ID = randAlertRuleID()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO alert_rules (id, tenant_id, metric, op, threshold, for_duration, severity, message, enabled, created_at, created_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE tenant_id=VALUES(tenant_id), metric=VALUES(metric), op=VALUES(op),
		   threshold=VALUES(threshold), for_duration=VALUES(for_duration), severity=VALUES(severity),
		   message=VALUES(message), enabled=VALUES(enabled), created_by=VALUES(created_by)`,
		r.ID, r.TenantID, r.Metric, r.Op, r.Threshold, r.ForDuration, r.Severity, r.Message,
		boolToInt(r.Enabled), r.CreatedAt, nullString(r.CreatedBy)); err != nil {
		log.Printf("[store] CreateAlertRule 失败: %v", err)
		return nil
	}
	cp := *r
	return &cp
}

// ListAlertRules 返回告警规则（task 100）；tenantID 非空时按租户过滤。按创建时间升序返回。
func (s *SQLStore) ListAlertRules(tenantID string) []*AlertRule {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT ` + alertRuleColumns + ` FROM alert_rules`
	var args []interface{}
	if tenantID != "" {
		q += ` WHERE tenant_id=?`
		args = append(args, tenantID)
	}
	q += ` ORDER BY created_at ASC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] ListAlertRules 失败: %v", err)
		return nil
	}
	defer rows.Close()
	out := make([]*AlertRule, 0)
	for rows.Next() {
		if r := scanAlertRule(rows); r != nil {
			out = append(out, r)
		}
	}
	return out
}

// DeleteAlertRule 删除告警规则（task 100），返回是否删除成功（不存在返回 false）。
func (s *SQLStore) DeleteAlertRule(id string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := s.db.ExecContext(ctx, `DELETE FROM alert_rules WHERE id=?`, id)
	if err != nil {
		log.Printf("[store] DeleteAlertRule 失败 %s: %v", id, err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

func (s *SQLStore) addAlert(ctx context.Context, a *proto.Alert) {
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	if a.Status == "" {
		a.Status = proto.AlertStatusFiring
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO alerts (tenant_id, device_id, agent_id, severity, message, created_at, alert_id, status, acknowledged_by, silenced_until, comment, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullString(a.TenantID), nullString(a.DeviceID), nullString(a.AgentID),
		nullString(a.Severity), nullString(a.Message), nullTime(a.CreatedAt),
		nullString(a.AlertID), nullString(a.Status), nullString(a.AcknowledgedBy),
		nullTime(a.SilencedUntil), nullString(a.Comment), nullTime(a.UpdatedAt)); err != nil {
		log.Printf("[store] addAlert 失败: %v", err)
	}
}

// Snapshot 返回 segment -> 设备列表（SELECT devices GROUP BY segment 在应用层分组）。

func (s *SQLStore) Alerts(tenantID string) []*proto.Alert {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT tenant_id, device_id, agent_id, severity, message, created_at, alert_id, status, acknowledged_by, silenced_until, comment, updated_at FROM alerts`
	var args []interface{}
	if tenantID != "" {
		q += ` WHERE tenant_id=?`
		args = append(args, tenantID)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] Alerts 查询失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*proto.Alert
	for rows.Next() {
		var a proto.Alert
		var createdAt, silencedUntil, updatedAt time.Time
		var alertID, status, ackBy, comment sql.NullString
		if err := rows.Scan(&a.TenantID, &a.DeviceID, &a.AgentID, &a.Severity, &a.Message, &createdAt,
			&alertID, &status, &ackBy, &silencedUntil, &comment, &updatedAt); err != nil {
			log.Printf("[store] Alerts 扫描失败: %v", err)
			continue
		}
		a.CreatedAt = createdAt
		a.AlertID = alertID.String
		a.Status = status.String
		a.AcknowledgedBy = ackBy.String
		a.SilencedUntil = silencedUntil
		a.Comment = comment.String
		a.UpdatedAt = updatedAt
		out = append(out, &a)
	}
	return out
}

// AddAlert 记录一条告警（M7）。

func (s *SQLStore) AddAlert(a *proto.Alert) {
	s.addAlert(context.Background(), a)
}

// Alert 按 alertID 返回单条告警（M7；供 ack/silence 定位）。

func (s *SQLStore) Alert(id string) *proto.Alert {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := s.db.QueryRowContext(ctx,
		`SELECT tenant_id, device_id, agent_id, severity, message, created_at, alert_id, status, acknowledged_by, silenced_until, comment, updated_at FROM alerts WHERE alert_id=?`,
		id)
	var a proto.Alert
	var createdAt, silencedUntil, updatedAt time.Time
	var alertID, status, ackBy, comment sql.NullString
	if err := row.Scan(&a.TenantID, &a.DeviceID, &a.AgentID, &a.Severity, &a.Message, &createdAt,
		&alertID, &status, &ackBy, &silencedUntil, &comment, &updatedAt); err != nil {
		if err != sql.ErrNoRows {
			log.Printf("[store] Alert 查询失败: %v", err)
		}
		return nil
	}
	a.CreatedAt = createdAt
	a.AlertID = alertID.String
	a.Status = status.String
	a.AcknowledgedBy = ackBy.String
	a.SilencedUntil = silencedUntil
	a.Comment = comment.String
	a.UpdatedAt = updatedAt
	return &a
}

// AckAlert 确认告警（M7）；tenantID 非空时校验归属，越权返回 false。

func (s *SQLStore) AckAlert(id, tenantID, by string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := s.db.ExecContext(ctx,
		`UPDATE alerts SET status=?, acknowledged_by=?, updated_at=? WHERE alert_id=? AND (tenant_id=? OR ?='')`,
		proto.AlertStatusAcknowledged, by, time.Now().UTC(), id, tenantID, tenantID)
	if err != nil {
		log.Printf("[store] AckAlert 失败: %v", err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// SilenceAlert 静默告警（M7）；until 为零值默认静默 24h；tenantID 非空时校验归属，越权返回 false。

func (s *SQLStore) SilenceAlert(id, tenantID, by string, until time.Time, comment string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if until.IsZero() {
		until = time.Now().UTC().Add(24 * time.Hour)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE alerts SET status=?, acknowledged_by=?, silenced_until=?, comment=?, updated_at=? WHERE alert_id=? AND (tenant_id=? OR ?='')`,
		proto.AlertStatusSilenced, by, until, comment, time.Now().UTC(), id, tenantID, tenantID)
	if err != nil {
		log.Printf("[store] SilenceAlert 失败: %v", err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// Audit 记录一条审计事件（U-04 等保三级：操作 100% 留痕）。
