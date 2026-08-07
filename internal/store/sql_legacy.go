package store

import (
	"context"
	"log"
)

// initSchemaExtra 建 task 100/111 新增的表（alert_rules/os_templates/middleware_templates/
// refresh_tokens）+ tasks 表审批字段列。由 sql.go initSchema 末尾调用。
//
// 拆分到独立文件避免 sql.go 被外部工具重置时丢失建表语句（task 112 工程债）。
func (s *SQLStore) initSchemaExtra(ctx context.Context) {
	// task 100 任务审批：tasks 表增加审批字段列（向后兼容，老库无列时补列）。
	s.alterColumnIfMissing(ctx, "tasks", "approval_required", "BOOLEAN DEFAULT 0")
	s.alterColumnIfMissing(ctx, "tasks", "approved_by", "VARCHAR(64)")
	s.alterColumnIfMissing(ctx, "tasks", "approved_at", "DATETIME")
	// task 100 告警规则表：alert_rules。
	if _, err := s.db.ExecContext(ctx, `
	CREATE TABLE IF NOT EXISTS alert_rules (
		id VARCHAR(64) PRIMARY KEY,
		tenant_id VARCHAR(64),
		metric VARCHAR(128),
		op VARCHAR(8),
		threshold DOUBLE,
		for_duration INT DEFAULT 0,
		severity VARCHAR(16),
		message TEXT,
		enabled BOOLEAN DEFAULT 1,
		created_at DATETIME
	)`); err != nil {
		log.Printf("[store] 建 alert_rules 表失败（非致命）: %v", err)
	}
	s.alterColumnIfMissing(ctx, "alert_rules", "tenant_id", "VARCHAR(64)")
	// task 100 OS 安装模板表：os_templates。
	if _, err := s.db.ExecContext(ctx, `
	CREATE TABLE IF NOT EXISTS os_templates (
		id VARCHAR(64) PRIMARY KEY,
		tenant_id VARCHAR(64),
		name VARCHAR(255) NOT NULL,
		os VARCHAR(64),
		version VARCHAR(64),
		arch VARCHAR(32),
		install_url VARCHAR(512),
		config TEXT,
		created_at DATETIME,
		updated_at DATETIME
	)`); err != nil {
		log.Printf("[store] 建 os_templates 表失败（非致命）: %v", err)
	}
	s.alterColumnIfMissing(ctx, "os_templates", "tenant_id", "VARCHAR(64)")
	// task 100 中间件部署模板表：middleware_templates。
	if _, err := s.db.ExecContext(ctx, `
	CREATE TABLE IF NOT EXISTS middleware_templates (
		id VARCHAR(64) PRIMARY KEY,
		tenant_id VARCHAR(64),
		name VARCHAR(255) NOT NULL,
		type VARCHAR(64),
		version VARCHAR(64),
		config TEXT,
		created_at DATETIME,
		updated_at DATETIME
	)`); err != nil {
		log.Printf("[store] 建 middleware_templates 表失败（非致命）: %v", err)
	}
	s.alterColumnIfMissing(ctx, "middleware_templates", "tenant_id", "VARCHAR(64)")
	// task 111 刷新令牌表：refresh_tokens（tokenHash/user/tenant/deviceFP/expires/created）。
	if _, err := s.db.ExecContext(ctx, `
	CREATE TABLE IF NOT EXISTS refresh_tokens (
		token_hash VARCHAR(64) PRIMARY KEY,
		user_id VARCHAR(64),
		tenant_id VARCHAR(64),
		device_fp VARCHAR(255),
		expires_at DATETIME,
		created_at DATETIME
	)`); err != nil {
		log.Printf("[store] 建 refresh_tokens 表失败（非致命）: %v", err)
	}
	s.alterColumnIfMissing(ctx, "refresh_tokens", "tenant_id", "VARCHAR(64)")
	s.createIndexIfMissing(ctx, "refresh_tokens", "idx_refresh_tokens_user", "(user_id)")
	s.createIndexIfMissing(ctx, "refresh_tokens", "idx_refresh_tokens_expires", "(expires_at)")
}
