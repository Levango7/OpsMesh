// sql_templates.go 实现 SQLStore 的 TemplateStore 子接口（OS/中间件部署模板领域 task 100，P0-1 生产就绪）。
//
// 涵盖：OSTemplate CRUD（kickstart/preseed 安装模板，B1 自动纳管裸机→OS→agent 链路）、
// MiddlewareTemplate CRUD（MySQL/Redis/Kafka/... 标准化部署配置，应用编排复用）。
//
// 设计要点（与 sql_k8s.go 风格一致）：
//   - 按 ID 幂等（INSERT ... ON DUPLICATE KEY UPDATE）；
//   - TenantID 为空时归一为 default；CreatedAt 为空时填当前时间；UpdatedAt 始终刷新；
//   - Config 为敏感内容（含 root 密码/连接串等），API 层负责脱敏后返回前端；
//   - DB 不可用时返回零值（nil/false/error），不 panic，与 SQLStore 其他方法一致；
//   - 持久化失败上抛错误（task 92 范式：DB 失败不再假装成功）。
//
// 表结构：os_templates / middleware_templates；initSchema 中幂等建表 + alterColumnIfMissing 兼容旧库。
package store

import (
	"context"
	"fmt"
	"log"
	"time"
)

// ============================================================================
// task 100 OS 安装模板：SaveOSTemplate / ListOSTemplates / GetOSTemplate / DeleteOSTemplate
// ============================================================================

// scanOSTemplate 从一行扫描出 *OSTemplate。
func scanOSTemplate(row rowScanner) *OSTemplate {
	var t OSTemplate
	var createdAt, updatedAt time.Time
	if err := row.Scan(&t.ID, &t.TenantID, &t.Name, &t.OS, &t.Version, &t.Arch,
		&t.InstallURL, &t.Config, &createdAt, &updatedAt); err != nil {
		return nil
	}
	t.CreatedAt = createdAt
	t.UpdatedAt = updatedAt
	return &t
}

// osTemplateColumns os_templates 表查询的列列表。
const osTemplateColumns = `id, tenant_id, name, os, version, arch, install_url, config, created_at, updated_at`

// SaveOSTemplate 创建或更新 OS 安装模板（按 ID 幂等）。
// ID 为空时分配随机 ID；TenantID 为空时归一为 default；
// CreatedAt 为空时填当前时间；UpdatedAt 始终刷新。
func (s *SQLStore) SaveOSTemplate(t *OSTemplate) error {
	if t == nil {
		return nil
	}
	if t.TenantID == "" {
		t.TenantID = "default"
	}
	now := time.Now().UTC()
	if t.ID == "" {
		t.ID = randOSTemplateID()
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO os_templates (id, tenant_id, name, os, version, arch, install_url, config, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE name=VALUES(name), os=VALUES(os), version=VALUES(version), arch=VALUES(arch),
		   install_url=VALUES(install_url), config=VALUES(config), updated_at=VALUES(updated_at)`,
		t.ID, t.TenantID, t.Name, t.OS, t.Version, t.Arch, t.InstallURL, t.Config, t.CreatedAt, t.UpdatedAt); err != nil {
		log.Printf("[store] SaveOSTemplate 失败: %v", err)
		return fmt.Errorf("os template: save: %w", err)
	}
	return nil
}

// ListOSTemplates 返回 OS 安装模板（按创建时间升序）；tenantID 非空时按租户过滤。
func (s *SQLStore) ListOSTemplates(tenantID string) []*OSTemplate {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT ` + osTemplateColumns + ` FROM os_templates`
	var args []interface{}
	if tenantID != "" {
		q += ` WHERE tenant_id=?`
		args = append(args, tenantID)
	}
	q += ` ORDER BY created_at ASC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] ListOSTemplates 失败: %v", err)
		return nil
	}
	defer rows.Close()
	out := make([]*OSTemplate, 0)
	for rows.Next() {
		if t := scanOSTemplate(rows); t != nil {
			out = append(out, t)
		}
	}
	return out
}

// GetOSTemplate 按 ID 返回单个 OS 安装模板（不存在返回 nil）。
func (s *SQLStore) GetOSTemplate(id string) *OSTemplate {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := s.db.QueryRowContext(ctx, `SELECT `+osTemplateColumns+` FROM os_templates WHERE id=?`, id)
	return scanOSTemplate(row)
}

// DeleteOSTemplate 删除 OS 安装模板，返回是否删除成功（不存在返回 false）。
func (s *SQLStore) DeleteOSTemplate(id string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := s.db.ExecContext(ctx, `DELETE FROM os_templates WHERE id=?`, id)
	if err != nil {
		log.Printf("[store] DeleteOSTemplate 失败 %s: %v", id, err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// ============================================================================
// task 100 中间件部署模板：SaveMiddlewareTemplate / ListMiddlewareTemplates / GetMiddlewareTemplate / DeleteMiddlewareTemplate
// ============================================================================

// scanMiddlewareTemplate 从一行扫描出 *MiddlewareTemplate。
func scanMiddlewareTemplate(row rowScanner) *MiddlewareTemplate {
	var t MiddlewareTemplate
	var createdAt, updatedAt time.Time
	if err := row.Scan(&t.ID, &t.TenantID, &t.Name, &t.Type, &t.Version, &t.Config, &createdAt, &updatedAt); err != nil {
		return nil
	}
	t.CreatedAt = createdAt
	t.UpdatedAt = updatedAt
	return &t
}

// middlewareTemplateColumns middleware_templates 表查询的列列表。
const middlewareTemplateColumns = `id, tenant_id, name, type, version, config, created_at, updated_at`

// SaveMiddlewareTemplate 创建或更新中间件部署模板（按 ID 幂等）。
// ID 为空时分配随机 ID；TenantID 为空时归一为 default；
// CreatedAt 为空时填当前时间；UpdatedAt 始终刷新。
func (s *SQLStore) SaveMiddlewareTemplate(t *MiddlewareTemplate) error {
	if t == nil {
		return nil
	}
	if t.TenantID == "" {
		t.TenantID = "default"
	}
	now := time.Now().UTC()
	if t.ID == "" {
		t.ID = randMiddlewareTemplateID()
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO middleware_templates (id, tenant_id, name, type, version, config, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE name=VALUES(name), type=VALUES(type), version=VALUES(version),
		   config=VALUES(config), updated_at=VALUES(updated_at)`,
		t.ID, t.TenantID, t.Name, t.Type, t.Version, t.Config, t.CreatedAt, t.UpdatedAt); err != nil {
		log.Printf("[store] SaveMiddlewareTemplate 失败: %v", err)
		return fmt.Errorf("middleware template: save: %w", err)
	}
	return nil
}

// ListMiddlewareTemplates 返回中间件部署模板（按创建时间升序）；tenantID 非空时按租户过滤。
func (s *SQLStore) ListMiddlewareTemplates(tenantID string) []*MiddlewareTemplate {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT ` + middlewareTemplateColumns + ` FROM middleware_templates`
	var args []interface{}
	if tenantID != "" {
		q += ` WHERE tenant_id=?`
		args = append(args, tenantID)
	}
	q += ` ORDER BY created_at ASC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] ListMiddlewareTemplates 失败: %v", err)
		return nil
	}
	defer rows.Close()
	out := make([]*MiddlewareTemplate, 0)
	for rows.Next() {
		if t := scanMiddlewareTemplate(rows); t != nil {
			out = append(out, t)
		}
	}
	return out
}

// GetMiddlewareTemplate 按 ID 返回单个中间件部署模板（不存在返回 nil）。
func (s *SQLStore) GetMiddlewareTemplate(id string) *MiddlewareTemplate {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := s.db.QueryRowContext(ctx, `SELECT `+middlewareTemplateColumns+` FROM middleware_templates WHERE id=?`, id)
	return scanMiddlewareTemplate(row)
}

// DeleteMiddlewareTemplate 删除中间件部署模板，返回是否删除成功（不存在返回 false）。
func (s *SQLStore) DeleteMiddlewareTemplate(id string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := s.db.ExecContext(ctx, `DELETE FROM middleware_templates WHERE id=?`, id)
	if err != nil {
		log.Printf("[store] DeleteMiddlewareTemplate 失败 %s: %v", id, err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}
