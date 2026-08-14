package cmdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// SQLCiStore 基于 MySQL 的 CMDB 存储实现。
type SQLCiStore struct {
	db *sql.DB
}

// NewSQLCiStore 构造 MySQL CMDB 存储，同时种子内置 CI 类型。
func NewSQLCiStore(db *sql.DB) *SQLCiStore {
	s := &SQLCiStore{db: db}
	s.seedTypes()
	return s
}

// seedTypes 确保内置 CI 类型存在（幂等 INSERT IGNORE）。
func (s *SQLCiStore) seedTypes() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, t := range []struct{ name, display string }{
		{"machine", "物理机"},
		{"os", "操作系统"},
		{"service", "系统服务"},
		{"app", "应用"},
		{"cluster", "集群"},
	} {
		_, _ = s.db.ExecContext(ctx,
			`INSERT IGNORE INTO ci_types (name, display_name, builtin, created_at) VALUES (?, ?, 1, NOW())`,
			t.name, t.display)
	}
}

func (s *SQLCiStore) CiTypes(ctx context.Context, tenantID string) ([]CiType, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, display_name, builtin, created_at FROM ci_types ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("CiTypes: %w", err)
	}
	defer rows.Close()
	var out []CiType
	for rows.Next() {
		var t CiType
		if err := rows.Scan(&t.ID, &t.Name, &t.DisplayName, &t.Builtin, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("CiTypes scan: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CreateCiType 创建自定义（非内置）CI 类型（builtin=0）。
func (s *SQLCiStore) CreateCiType(ctx context.Context, t *CiType) error {
	if t.Name == "" {
		return fmt.Errorf("ci type name required")
	}
	if t.DisplayName == "" {
		t.DisplayName = t.Name
	}
	now := time.Now()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO ci_types (name, display_name, builtin, created_at)
		VALUES (?, ?, 0, ?)
		ON DUPLICATE KEY UPDATE display_name=VALUES(display_name)`,
		t.Name, t.DisplayName, now)
	if err != nil {
		return fmt.Errorf("CreateCiType: %w", err)
	}
	if id, _ := res.LastInsertId(); id > 0 {
		t.ID = int(id)
	}
	t.Builtin = false
	t.CreatedAt = now
	return nil
}

func (s *SQLCiStore) GetCIs(ctx context.Context, ciType, status, tenantID string) ([]CiItem, error) {
	q := `SELECT id, ci_type, tenant_id, name, status, approval_status, attrs, source, agent_id, device_id, version, created_at, updated_at
	FROM ci_items WHERE 1=1`
	var args []interface{}
	if ciType != "" {
		q += " AND ci_type=?"
		args = append(args, ciType)
	}
	if status != "" {
		q += " AND status=?"
		args = append(args, status)
	}
	if tenantID != "" {
		q += " AND tenant_id=?"
		args = append(args, tenantID)
	}
	q += " ORDER BY updated_at DESC"
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("GetCIs: %w", err)
	}
	defer rows.Close()
	var out []CiItem
	for rows.Next() {
		item, err := scanCI(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

func (s *SQLCiStore) GetCI(ctx context.Context, id, tenantID string) (*CiItem, error) {
	q := `SELECT id, ci_type, tenant_id, name, status, approval_status, attrs, source, agent_id, device_id, version, created_at, updated_at
	FROM ci_items WHERE id=?`
	args := []interface{}{id}
	if tenantID != "" {
		q += " AND tenant_id=?"
		args = append(args, tenantID)
	}
	row := s.db.QueryRowContext(ctx, q, args...)
	return scanCI(row)
}

func (s *SQLCiStore) CreateCI(ctx context.Context, ci *CiItem) error {
	attrsJSON, _ := json.Marshal(ci.Attrs)
	now := time.Now()
	if ci.ApprovalStatus == "" {
		ci.ApprovalStatus = ApprovalApproved
	}
	_, err := s.db.ExecContext(ctx, `
	INSERT INTO ci_items (id, ci_type, tenant_id, name, status, approval_status, attrs, source, agent_id, device_id, version, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		ci.ID, ci.CiType, ci.TenantID, ci.Name, ci.Status, ci.ApprovalStatus, string(attrsJSON),
		ci.Source, ci.AgentID, ci.DeviceID, now, now)
	if err != nil {
		return fmt.Errorf("CreateCI: %w", err)
	}
	return nil
}

func (s *SQLCiStore) UpdateCI(ctx context.Context, ci *CiItem) error {
	attrsJSON, _ := json.Marshal(ci.Attrs)
	res, err := s.db.ExecContext(ctx, `
	UPDATE ci_items SET ci_type=?, name=?, status=?, approval_status=?, attrs=?, source=?, agent_id=?, device_id=?,
		version=version+1, updated_at=NOW() WHERE id=? AND (tenant_id=? OR ?='')`,
		ci.CiType, ci.Name, ci.Status, ci.ApprovalStatus, string(attrsJSON), ci.Source, ci.AgentID, ci.DeviceID,
		ci.ID, ci.TenantID, ci.TenantID)
	if err != nil {
		return fmt.Errorf("UpdateCI: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("CI %s not found", ci.ID)
	}
	return nil
}

func (s *SQLCiStore) DeleteCI(ctx context.Context, id, tenantID string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE ci_items SET status='deleted', updated_at=NOW() WHERE id=? AND (tenant_id=? OR ?='')`,
		id, tenantID, tenantID)
	if err != nil {
		return fmt.Errorf("DeleteCI: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("CI %s not found", id)
	}
	return nil
}

// GetCIsByApproval 按审批状态列出 CI（Phase-3 待审列表）。
func (s *SQLCiStore) GetCIsByApproval(ctx context.Context, approvalStatus, tenantID string) ([]CiItem, error) {
	q := `SELECT id, ci_type, tenant_id, name, status, approval_status, attrs, source, agent_id, device_id, version, created_at, updated_at
	FROM ci_items WHERE 1=1`
	var args []interface{}
	if approvalStatus != "" {
		q += " AND approval_status=?"
		args = append(args, approvalStatus)
	}
	if tenantID != "" {
		q += " AND tenant_id=?"
		args = append(args, tenantID)
	}
	q += " ORDER BY updated_at DESC"
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("GetCIsByApproval: %w", err)
	}
	defer rows.Close()
	var out []CiItem
	for rows.Next() {
		item, err := scanCI(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

// SetApproval 设置单个 CI 的审批状态。
func (s *SQLCiStore) SetApproval(ctx context.Context, id, tenantID, approvalStatus string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE ci_items SET approval_status=?, updated_at=NOW() WHERE id=? AND (tenant_id=? OR ?='')`,
		approvalStatus, id, tenantID, tenantID)
	if err != nil {
		return fmt.Errorf("SetApproval: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("CI %s not found", id)
	}
	return nil
}

func (s *SQLCiStore) GetCIHistory(ctx context.Context, ciID, tenantID string, limit int) ([]CiItem, error) {
	// MVP：SQL cmdb 不存储历史版本，返回当前版本
	ci, err := s.GetCI(ctx, ciID, tenantID)
	if err != nil {
		return nil, err
	}
	return []CiItem{*ci}, nil
}

// scanner 接口统一 row 与 rows 的 Scan。
type scanner interface {
	Scan(dest ...interface{}) error
}

func scanCI(s scanner) (*CiItem, error) {
	var ci CiItem
	var attrsStr sql.NullString
	var source, agentID, deviceID, approvalStatus sql.NullString
	err := s.Scan(&ci.ID, &ci.CiType, &ci.TenantID, &ci.Name, &ci.Status, &approvalStatus,
		&attrsStr, &source, &agentID, &deviceID, &ci.Version, &ci.CreatedAt, &ci.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("CI not found")
		}
		return nil, fmt.Errorf("scanCI: %w", err)
	}
	ci.Source = source.String
	ci.AgentID = agentID.String
	ci.DeviceID = deviceID.String
	if approvalStatus.Valid {
		ci.ApprovalStatus = approvalStatus.String
	} else {
		ci.ApprovalStatus = ApprovalApproved
	}
	ci.Attrs = make(map[string]string)
	if attrsStr.Valid && attrsStr.String != "" {
		_ = json.Unmarshal([]byte(attrsStr.String), &ci.Attrs)
	}
	return &ci, nil
}

// === Phase 2: 关系拓扑 ===

func (s *SQLCiStore) CreateRelation(ctx context.Context, rel *CiRelation) error {
	attrsJSON, _ := json.Marshal(rel.Attrs)
	now := time.Now()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO ci_relations (source_ci_id, target_ci_id, relation_type, tenant_id, attributes, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		rel.SourceCIID, rel.TargetCIID, rel.RelationType, rel.TenantID, string(attrsJSON), now)
	if err != nil {
		return fmt.Errorf("CreateRelation: %w", err)
	}
	rel.ID, _ = res.LastInsertId()
	rel.CreatedAt = now
	return nil
}

func (s *SQLCiStore) DeleteRelation(ctx context.Context, id int64, tenantID string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM ci_relations WHERE id=? AND (tenant_id=? OR ?='')`,
		id, tenantID, tenantID)
	if err != nil {
		return fmt.Errorf("DeleteRelation: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("relation %d not found", id)
	}
	return nil
}

func (s *SQLCiStore) GetCIRelations(ctx context.Context, ciID, tenantID string) ([]CiRelation, error) {
	q := `SELECT id, source_ci_id, target_ci_id, relation_type, tenant_id, attributes, created_at
		FROM ci_relations WHERE (source_ci_id=? OR target_ci_id=?)`
	args := []interface{}{ciID, ciID}
	if tenantID != "" {
		q += " AND tenant_id=?"
		args = append(args, tenantID)
	}
	q += " ORDER BY created_at"
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("GetCIRelations: %w", err)
	}
	defer rows.Close()
	var out []CiRelation
	for rows.Next() {
		rel, err := scanRelation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rel)
	}
	return out, rows.Err()
}

func (s *SQLCiStore) GetCIRelationGraph(ctx context.Context, ciID, tenantID string) (*CIRelationGraph, error) {
	center, err := s.GetCI(ctx, ciID, tenantID)
	if err != nil {
		return nil, err
	}
	rels, err := s.GetCIRelations(ctx, ciID, tenantID)
	if err != nil {
		return nil, err
	}
	withTargets := make([]RelationWithTarget, 0, len(rels))
	for _, rel := range rels {
		var sourceName, targetName, targetType string
		if src, _ := s.GetCI(ctx, rel.SourceCIID, ""); src != nil {
			sourceName = src.Name
		}
		if tgt, _ := s.GetCI(ctx, rel.TargetCIID, ""); tgt != nil {
			targetName = tgt.Name
			targetType = tgt.CiType
		}
		withTargets = append(withTargets, RelationWithTarget{
			CiRelation: rel,
			SourceName: sourceName,
			TargetName: targetName,
			TargetType: targetType,
		})
	}
	return &CIRelationGraph{CenterCI: center, Relations: withTargets}, nil
}

// === Phase 2: 属性模板 ===

func (s *SQLCiStore) CreateAttrTemplate(ctx context.Context, tmpl *CiAttrTemplate) error {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO ci_attr_templates (ci_type, attr_key, label, attr_type, required, default_value, tenant_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, NOW())`,
		tmpl.CiType, tmpl.AttrKey, tmpl.Label, tmpl.AttrType, tmpl.Required, tmpl.DefaultValue, tmpl.TenantID)
	if err != nil {
		return fmt.Errorf("CreateAttrTemplate: %w", err)
	}
	id, _ := res.LastInsertId()
	tmpl.ID = int(id)
	return nil
}

func (s *SQLCiStore) GetAttrTemplates(ctx context.Context, ciType, tenantID string) ([]CiAttrTemplate, error) {
	q := `SELECT id, ci_type, attr_key, label, attr_type, required, default_value, tenant_id, created_at
		FROM ci_attr_templates WHERE 1=1`
	var args []interface{}
	if ciType != "" {
		q += " AND ci_type=?"
		args = append(args, ciType)
	}
	if tenantID != "" {
		q += " AND tenant_id=?"
		args = append(args, tenantID)
	}
	q += " ORDER BY ci_type, id"
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("GetAttrTemplates: %w", err)
	}
	defer rows.Close()
	var out []CiAttrTemplate
	for rows.Next() {
		var t CiAttrTemplate
		if err := rows.Scan(&t.ID, &t.CiType, &t.AttrKey, &t.Label, &t.AttrType,
			&t.Required, &t.DefaultValue, &t.TenantID, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("GetAttrTemplates scan: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *SQLCiStore) UpdateAttrTemplate(ctx context.Context, tmpl *CiAttrTemplate) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE ci_attr_templates SET ci_type=?, attr_key=?, label=?, attr_type=?, required=?, default_value=?
		WHERE id=? AND (tenant_id=? OR ?='')`,
		tmpl.CiType, tmpl.AttrKey, tmpl.Label, tmpl.AttrType, tmpl.Required, tmpl.DefaultValue,
		tmpl.ID, tmpl.TenantID, tmpl.TenantID)
	if err != nil {
		return fmt.Errorf("UpdateAttrTemplate: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("template %d not found", tmpl.ID)
	}
	return nil
}

func (s *SQLCiStore) DeleteAttrTemplate(ctx context.Context, id int, tenantID string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM ci_attr_templates WHERE id=? AND (tenant_id=? OR ?='')`,
		id, tenantID, tenantID)
	if err != nil {
		return fmt.Errorf("DeleteAttrTemplate: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("template %d not found", id)
	}
	return nil
}

// scanRelation 扫描一行 ci_relations 记录。
func scanRelation(s scanner) (*CiRelation, error) {
	var rel CiRelation
	var attrsJSON sql.NullString
	err := s.Scan(&rel.ID, &rel.SourceCIID, &rel.TargetCIID, &rel.RelationType,
		&rel.TenantID, &attrsJSON, &rel.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("scanRelation: %w", err)
	}
	rel.Attrs = make(map[string]string)
	if attrsJSON.Valid && attrsJSON.String != "" {
		_ = json.Unmarshal([]byte(attrsJSON.String), &rel.Attrs)
	}
	return &rel, nil
}
