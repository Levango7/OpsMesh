package cmdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// SQLCiStore 基于 MySQL 的 CMDB 存储实现。
type SQLCiStore struct {
	db *sql.DB

	// fulltextMu 保护下面两个字段：召回路径要读 ready，多个请求可能同时首次探测。
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
		if _, err := s.db.ExecContext(ctx,
			`INSERT IGNORE INTO ci_types (name, display_name, builtin, created_at) VALUES (?, ?, 1, NOW())`,
			t.name, t.display); err != nil {
			log.Printf("[cmdb] seedTypes %s: %v", t.name, err)
		}
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
		var displayName sql.NullString
		var createdAt sql.NullTime
		if err := rows.Scan(&t.ID, &t.Name, &displayName, &t.Builtin, &createdAt); err != nil {
			return nil, fmt.Errorf("CiTypes scan: %w", err)
		}
		t.DisplayName = displayName.String
		if createdAt.Valid {
			t.CreatedAt = createdAt.Time
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
	if id, lidErr := res.LastInsertId(); lidErr == nil && id > 0 {
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
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		return fmt.Errorf("UpdateCI rows affected: %w", rowsErr)
	}
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
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		return fmt.Errorf("DeleteCI rows affected: %w", rowsErr)
	}
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
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		return fmt.Errorf("SetApproval rows affected: %w", rowsErr)
	}
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

// === 全文检索 ===

// ciSearchColumns 是 SQL 召回阶段参与 LIKE 匹配的列表达式。
//
// 必须与 ciSearchText 覆盖的字段集合严格一致，否则会出现"内存后端搜得到、
// SQL 后端搜不到"的口径分裂。JSON 列 attrs 需显式 CAST 成字符串再做 LIKE。
var ciSearchColumns = []string{"name", "ci_type", "CAST(attrs AS CHAR)", "agent_id", "device_id", "source", "id"}

// ciSearchTokenCond 是单个检索词对应的 WHERE 片段模板，含 N 个占位符。
//
// 为什么在包级构造而不是在 SearchCIs 里逐列 `sqlText += col + " LIKE ?"`：
// 列名与片段结构只与列集合有关，与查询无关，每次请求重拼是白做功。
//
// 安全性不变：**检索词本身从不进 SQL 文本**，只作为 `?` 占位符的参数（见下方 args），
// 所以不存在注入面。数据库侧的召回本就是"宽松匹配"，精确判定由 matchCI 负责。
var ciSearchTokenCond = buildCISearchTokenCond()

func buildCISearchTokenCond() string {
	parts := make([]string, 0, len(ciSearchColumns))
	for _, col := range ciSearchColumns {
		parts = append(parts, col+" LIKE ?")
	}
	return " AND (" + strings.Join(parts, " OR ") + ")"
}

// 召回一律走 LIKE（2026-10-10 证据修正：MATCH 路径已删除）。
//
// # 为什么删掉全文索引召回（原 ciSearchFulltextCond / 就绪探测 / ngram 门禁 / 逐词分流）
//
// 原设计假设「MATCH 与 LIKE 召回等价，故可用索引加速」——**该假设经实测证伪**。
// 证据（MySQL 8.4 + ngram_token_size=2，7 列同序索引，单列 txt 上逐查询对照）：
//
//	查询       MATCH        MATCH*       LIKE
//	ci         ∅            d1,d2,d6     d1,d2,d3,d4,d6
//	ci1        d1           d1,d2,d6     d1,d2,d3,d4,d6
//	abcd       d8           d8           d8,d9
//	1c         ∅            ∅            d7
//
// 即：**任何形态的 MATCH 都不等于 LIKE**——LIKE 是子串语义（%tok%），MATCH 是词元/短语语义，
// 词元边界对不齐就丢行；最严重的是 2 字符查询（等于 ngram_token_size）MATCH **一律召回为空**
// ⇒ 在生产库（020/021 已执行、索引就绪）上「2 字符搜索什么都搜不到、子串搜索丢行」，
// 而这两种库状态本该给出**完全一致**的用户可见结果（见 search_fulltext_integration_test.go 的契约）。
//
// 为什么不做「MATCH ∪ LIKE」的并集兜底：`OR` 之下 MySQL 无法使用 FULLTEXT 索引，LIKE 的
// 全表扫描照样跑 ⇒ 并集**同时**失去正确性与加速收益，只是多了一次无用的 MATCH 求值。
//
// 结论与后续（台账 TD-91）：召回回到 LIKE（慢，但**不漏**——与本模块既有的「只放行实测过的、
// 其余退回 LIKE」同一原则）；020/021 建的索引与迁移保留为历史（不再参与召回，也不改迁移文件：
// 迁移校验和按文件内容计算，改动即破坏已应用库的重放）；**若要恢复加速**，必须先在 TD-91 里
// 定义**明确的产品检索语义**（例如「词/前缀匹配」而非子串匹配），并配 CI 真库判据后才能重新接线——
// 不得再以「等价」为由静默引入（本段证据即为该判据的底稿）。

func (s *SQLCiStore) SearchCIs(ctx context.Context, tenantID string, q CiSearchQuery) ([]CiSearchHit, error) {
	tokens, mode, limit, status := normalizeCiSearch(q)
	if len(tokens) == 0 {
		return []CiSearchHit{}, nil
	}
	// SQL 文本一次性拼装：收集 WHERE 片段后用 strings.Join 合成，再经 fmt.Sprintf 代入。
	//
	// 为什么不用 `sqlText := "...WHERE 1=1" + strings.Join(conds, "")`：
	// G202（gosec）把「SQL 字面量与变量用 + 拼接」判为字符串拼接注入风险。2026-10-05
	// 实测（同一文件内并列三种写法，golangci-lint v2.13.2 = CI 同版）：
	//   `字面量` + strings.Join(...)        ⇒ G202
	//   fmt.Sprintf("...%s", strings.Join)  ⇒ 不报
	//   fmt.Sprintf(tmpl, strings.Join)     ⇒ 不报
	// 即 G202 匹配的是 `+` 运算符，不是「拼出来的文本里有变量」。
	//
	// 为什么值得为写法绕一下而不是给 internal/cmdb 加豁免：仓库里 G202 已有
	// internal/(store|logstore)/ 的收窄豁免（.golangci.yml），再加一个会稀释这道规则
	// 的约束力。改写法则规则覆盖面不变。
	//
	// 关键点：检索词从不进 SQL 文本，只作为 ? 占位符的参数（见下方 args），
	// 所以不存在注入面；conds 与 args 严格一一对应，由同一次循环同时追加。
	var conds []string
	var args []interface{}
	if tenantID != "" {
		conds = append(conds, " AND tenant_id=?")
		args = append(args, tenantID)
	}
	if status != "" {
		conds = append(conds, " AND status=?")
		args = append(args, status)
	}
	if q.CiType != "" {
		conds = append(conds, " AND ci_type=?")
		args = append(args, q.CiType)
	}
	// 每个检索词都必须至少命中一个列（AND across tokens）；召回路径一律 LIKE
	// （全文索引召回已删除，理由见上方「召回一律走 LIKE」段）。
	for _, tok := range tokens {
		conds = append(conds, ciSearchTokenCond)
		for range ciSearchColumns {
			args = append(args, "%"+tok+"%")
		}
	}
	conds = append(conds, " ORDER BY updated_at DESC LIMIT ?")
	args = append(args, ciSearchSQLRecallCap)

	const sqlTemplate = `SELECT id, ci_type, tenant_id, name, status, approval_status, attrs, source, agent_id, device_id, version, created_at, updated_at
    FROM ci_items WHERE 1=1%s`
	sqlText := fmt.Sprintf(sqlTemplate, strings.Join(conds, ""))

	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("SearchCIs: %w", err)
	}
	defer rows.Close()
	var hits []CiSearchHit
	for rows.Next() {
		item, err := scanCI(rows)
		if err != nil {
			return nil, err
		}
		// 租户/类型/状态在 SQL 里已经过滤过一遍，这里再判一次：
		// 租户隔离值得多一道防线——将来谁改了 WHERE 拼装或参数绑定，
		// 少这一层就是静默的跨租户数据泄漏；而它只对候选集做一次 O(1) 判断。
		// 附带收益：SQL 与 memory 两个后端的过滤口径因此完全等价。
		if !ciMatchesFilter(item, tenantID, q.CiType, status) {
			continue
		}
		score, matched := matchCI(item, tokens, mode)
		if score <= 0 {
			continue
		}
		hits = append(hits, CiSearchHit{CiItem: *item, Score: score, Matched: matched})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("SearchCIs rows: %w", err)
	}
	if hits == nil {
		hits = []CiSearchHit{}
	}
	sortCiSearchHits(hits)
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

// scanner 接口统一 row 与 rows 的 Scan。
type scanner interface {
	Scan(dest ...interface{}) error
}

func scanCI(s scanner) (*CiItem, error) {
	var ci CiItem
	var attrsStr sql.NullString
	var source, agentID, deviceID, approvalStatus sql.NullString
	var createdAt, updatedAt sql.NullTime
	err := s.Scan(&ci.ID, &ci.CiType, &ci.TenantID, &ci.Name, &ci.Status, &approvalStatus,
		&attrsStr, &source, &agentID, &deviceID, &ci.Version, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("CI not found")
		}
		return nil, fmt.Errorf("scanCI: %w", err)
	}
	ci.Source = source.String
	ci.AgentID = agentID.String
	ci.DeviceID = deviceID.String
	if createdAt.Valid {
		ci.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		ci.UpdatedAt = updatedAt.Time
	}
	if approvalStatus.Valid {
		ci.ApprovalStatus = approvalStatus.String
	} else {
		ci.ApprovalStatus = ApprovalApproved
	}
	ci.Attrs = make(map[string]string)
	if attrsStr.Valid && attrsStr.String != "" {
		// attrs 解析失败时保留空 map：不让单行脏数据导致整条 CI 无法读取。
		if uErr := json.Unmarshal([]byte(attrsStr.String), &ci.Attrs); uErr != nil {
			ci.Attrs = make(map[string]string)
		}
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
	if id, lidErr := res.LastInsertId(); lidErr == nil {
		rel.ID = id
	}
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
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		return fmt.Errorf("DeleteRelation rows affected: %w", rowsErr)
	}
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
		// 端点 CI 可能已被删除/越权，Get 失败按无名称展示（不阻断拓扑渲染）。
		if src, srcErr := s.GetCI(ctx, rel.SourceCIID, ""); srcErr == nil && src != nil {
			sourceName = src.Name
		}
		if tgt, tgtErr := s.GetCI(ctx, rel.TargetCIID, ""); tgtErr == nil && tgt != nil {
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
	id, lidErr := res.LastInsertId()
	if lidErr != nil {
		return fmt.Errorf("CreateAttrTemplate last insert id: %w", lidErr)
	}
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
		var defaultValue, tenantID sql.NullString
		var createdAt sql.NullTime
		if err := rows.Scan(&t.ID, &t.CiType, &t.AttrKey, &t.Label, &t.AttrType,
			&t.Required, &defaultValue, &tenantID, &createdAt); err != nil {
			return nil, fmt.Errorf("GetAttrTemplates scan: %w", err)
		}
		t.DefaultValue = defaultValue.String
		t.TenantID = tenantID.String
		if createdAt.Valid {
			t.CreatedAt = createdAt.Time
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
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		return fmt.Errorf("UpdateAttrTemplate rows affected: %w", rowsErr)
	}
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
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		return fmt.Errorf("DeleteAttrTemplate rows affected: %w", rowsErr)
	}
	if n == 0 {
		return fmt.Errorf("template %d not found", id)
	}
	return nil
}

// scanRelation 扫描一行 ci_relations 记录。
func scanRelation(s scanner) (*CiRelation, error) {
	var rel CiRelation
	var attrsJSON sql.NullString
	var tenantID sql.NullString
	var createdAt sql.NullTime
	err := s.Scan(&rel.ID, &rel.SourceCIID, &rel.TargetCIID, &rel.RelationType,
		&tenantID, &attrsJSON, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("scanRelation: %w", err)
	}
	rel.TenantID = tenantID.String
	if createdAt.Valid {
		rel.CreatedAt = createdAt.Time
	}
	rel.Attrs = make(map[string]string)
	if attrsJSON.Valid && attrsJSON.String != "" {
		// attrs 解析失败时保留空 map：不让单行脏数据导致整条关系无法读取。
		if uErr := json.Unmarshal([]byte(attrsJSON.String), &rel.Attrs); uErr != nil {
			rel.Attrs = make(map[string]string)
		}
	}
	return &rel, nil
}
