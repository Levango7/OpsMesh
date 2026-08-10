package store

import (
	"context"
)

// initSchemaExtra 建 task 100/111 新增的表（alert_rules/os_templates/middleware_templates/
// refresh_tokens）+ tasks 表审批字段列。由 sql.go initSchema 末尾调用。
//
// 拆分到独立文件避免 sql.go 被外部工具重置时丢失建表语句（task 112 工程债）。
//
// G5 / B-7 建表逻辑双写治理：
//
//	历史上本函数同时承担 (1) 四张表的 CREATE TABLE IF NOT EXISTS 建表 与 (2) 对老库的
//	ALTER TABLE 补列 / CREATE INDEX 兼容补丁。其中 (1) 与 migrations/001_initial.sql
//	重复定义，违反单一真相原则，且绕过 schema_migrations 版本记录与 checksum 校验。
//	已将四张表的 CREATE TABLE 语句迁入 migrations/003_legacy_tables.sql，由版本化
//	迁移框架统一管理。本函数仅保留 (2) 增量补列/补索引逻辑，兼容已存在但缺列/缺索引
//	的老库升级到最新结构。
func (s *SQLStore) initSchemaExtra(ctx context.Context) {
	// task 100 任务审批：tasks 表增加审批字段列（向后兼容，老库无列时补列）。
	s.alterColumnIfMissing(ctx, "tasks", "approval_required", "BOOLEAN DEFAULT 0")
	s.alterColumnIfMissing(ctx, "tasks", "approved_by", "VARCHAR(64)")
	s.alterColumnIfMissing(ctx, "tasks", "approved_at", "DATETIME")
	// task 100 alert_rules 表 tenant_id 列兼容补丁（表本身由 003_legacy_tables.sql 创建）。
	s.alterColumnIfMissing(ctx, "alert_rules", "tenant_id", "VARCHAR(64)")
	// task 100 os_templates 表 tenant_id 列兼容补丁（表本身由 003_legacy_tables.sql 创建）。
	s.alterColumnIfMissing(ctx, "os_templates", "tenant_id", "VARCHAR(64)")
	// task 100 middleware_templates 表 tenant_id 列兼容补丁（表本身由 003_legacy_tables.sql 创建）。
	s.alterColumnIfMissing(ctx, "middleware_templates", "tenant_id", "VARCHAR(64)")
	// task 111 refresh_tokens 表 tenant_id 列兼容补丁与索引补丁
	// （表本身由 003_legacy_tables.sql 创建）。
	s.alterColumnIfMissing(ctx, "refresh_tokens", "tenant_id", "VARCHAR(64)")
	s.createIndexIfMissing(ctx, "refresh_tokens", "idx_refresh_tokens_user", "(user_id)")
	s.createIndexIfMissing(ctx, "refresh_tokens", "idx_refresh_tokens_expires", "(expires_at)")
}
