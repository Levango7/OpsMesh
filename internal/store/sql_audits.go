// sql_audits.go - SQLStore AuditStore methods (audit event CRUD).
//
// M1-4 分布式可观测性：audit_log 表持久化 trace_id 列，
// 使审计日志可与 OTel 链路追踪/日志/SSE 事件跨域关联检索。
package store

import (
	"context"

	"log"
	"time"

	"opsmesh/internal/proto"
)

func (s *SQLStore) Audit(e *proto.AuditEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	// M1-4：持久化 trace_id（列存在时写入，列不存在时回退到无 trace_id 写入）。
	// trace_id 列由 migrations/004_add_audit_trace_id.sql 添加；
	// 老库未迁移时 INSERT 会报错"Unknown column"，此时降级到无 trace_id 写入（向后兼容）。
	if s.columnExists(ctx, "audit_log", "trace_id") {
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO audit_log (tenant_id, user_id, action, target, detail, created_at, trace_id)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			e.TenantID, e.UserID, e.Action, e.Target, e.Detail, e.CreatedAt, e.TraceID); err != nil {
			log.Printf("[store] Audit 写入失败: %v", err)
		}
		return
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log (tenant_id, user_id, action, target, detail, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		e.TenantID, e.UserID, e.Action, e.Target, e.Detail, e.CreatedAt); err != nil {
		log.Printf("[store] Audit 写入失败: %v", err)
	}
}

// Audits 返回最近 100 条审计事件（MVP；生产可加时间窗/分页）。

func (s *SQLStore) Audits() []*proto.AuditEvent {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hasTrace := s.columnExists(ctx, "audit_log", "trace_id")
	var q string
	if hasTrace {
		q = `SELECT tenant_id, user_id, action, target, detail, created_at, trace_id FROM audit_log ORDER BY id DESC LIMIT 100`
	} else {
		q = `SELECT tenant_id, user_id, action, target, detail, created_at FROM audit_log ORDER BY id DESC LIMIT 100`
	}
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		log.Printf("[store] Audits 查询失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*proto.AuditEvent
	for rows.Next() {
		var e proto.AuditEvent
		var createdAt time.Time
		if hasTrace {
			if err := rows.Scan(&e.TenantID, &e.UserID, &e.Action, &e.Target, &e.Detail, &createdAt, &e.TraceID); err != nil {
				log.Printf("[store] Audits 扫描失败: %v", err)
				continue
			}
		} else {
			if err := rows.Scan(&e.TenantID, &e.UserID, &e.Action, &e.Target, &e.Detail, &createdAt); err != nil {
				log.Printf("[store] Audits 扫描失败: %v", err)
				continue
			}
		}
		e.CreatedAt = createdAt
		out = append(out, &e)
	}
	return out
}

// QueryAudits 按租户/动作/时间窗过滤审计事件（P0-4 审计可查；U-04 等保三级留痕必须可检索）。
// tenant/action 为空表示不限；since/until 为零值表示不限；limit<=0 表示不限制。返回按时间倒序。

func (s *SQLStore) QueryAudits(tenant, action string, since, until time.Time, limit int) []*proto.AuditEvent {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hasTrace := s.columnExists(ctx, "audit_log", "trace_id")
	var q string
	if hasTrace {
		q = `SELECT tenant_id, user_id, action, target, detail, created_at, trace_id FROM audit_log WHERE 1=1`
	} else {
		q = `SELECT tenant_id, user_id, action, target, detail, created_at FROM audit_log WHERE 1=1`
	}
	args := []interface{}{}
	if tenant != "" {
		q += ` AND tenant_id=?`
		args = append(args, tenant)
	}
	if action != "" {
		q += ` AND action=?`
		args = append(args, action)
	}
	if !since.IsZero() {
		q += ` AND created_at>=?`
		args = append(args, since)
	}
	if !until.IsZero() {
		q += ` AND created_at<=?`
		args = append(args, until)
	}
	q += ` ORDER BY id DESC`
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] QueryAudits 查询失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*proto.AuditEvent
	for rows.Next() {
		var e proto.AuditEvent
		var createdAt time.Time
		if hasTrace {
			if err := rows.Scan(&e.TenantID, &e.UserID, &e.Action, &e.Target, &e.Detail, &createdAt, &e.TraceID); err != nil {
				log.Printf("[store] QueryAudits 扫描失败: %v", err)
				continue
			}
		} else {
			if err := rows.Scan(&e.TenantID, &e.UserID, &e.Action, &e.Target, &e.Detail, &createdAt); err != nil {
				log.Printf("[store] QueryAudits 扫描失败: %v", err)
				continue
			}
		}
		e.CreatedAt = createdAt
		out = append(out, &e)
	}
	return out
}

// columnExists 检查表是否存在指定列（用于 M1-4 trace_id 列的向后兼容检测）。
// 返回 false 时不阻断业务，仅降级到无 trace_id 的 SQL（兼容老库未迁移场景）。
func (s *SQLStore) columnExists(ctx context.Context, table, column string) bool {
	var cnt int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name=? AND column_name=?`,
		table, column).Scan(&cnt); err != nil {
		return false
	}
	return cnt > 0
}

// cacheAgent 把 agent 状态写入 Redis HASH opsmesh:agents（field=agent_id, value=JSON）。
// MVP 仅写缓存；生产可将读取也改为走 Redis 降低 MySQL 压力。
