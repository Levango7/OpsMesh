// sql_ticket.go 实现 SQLStore 的 TicketStore 子接口（Phase 1 工单管理，生产就绪）。
//
// 表结构：tickets（id PK + tenant_id + title + description + status + priority +
// category + assignee_id + creator_id + related_device + related_task + tags JSON +
// created_at + updated_at + resolved_at）。迁移文件
// migrations/010_p1_slo_ticket.sql 幂等建表。
//
// 设计要点（与 sql_k8s.go / sql_secret.go 风格一致）：
//   - Tags 以 JSON 数组存储在 tags TEXT 列；空切片存空串，读取时空串跳过 Unmarshal；
//   - resolved_at 可空（NULL 表示未解决），用 sql.NullTime 读写；
//   - CreateTicket 按 ID 幂等（INSERT ... ON DUPLICATE KEY UPDATE），tenant_id 仅插入
//     不更新（防 upsert 改写归属）；
//   - ListTickets 按 filter 动态拼接 WHERE 子句，按创建时间降序返回（最新优先）；
//   - UpdateTicket 先 SELECT 校验存在 + 租户归属，再 UPDATE，保留原 CreatedAt/TenantID；
//   - CloseTicket 置 status='closed' + resolved_at=now + updated_at=now；
//   - DB 不可用时返回零值（nil/false/空 slice），不 panic；
//   - ID 生成复用 memory_ticket.go 的 randTicketID（"ticket-" + 16 字节 hex）。
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"time"
)

// scanTicket 从一行扫描出 *Ticket（tags 为 JSON 文本列，resolved_at 可空）。
// 列顺序：id, tenant_id, title, description, status, priority, category, assignee_id,
// creator_id, related_device, related_task, tags, created_at, updated_at, resolved_at。
// 无行或扫描失败返回 nil。
func scanTicket(row rowScanner) *Ticket {
	var t Ticket
	var tagsJSON string
	var createdAt, updatedAt time.Time
	var resolvedAt sql.NullTime
	if err := row.Scan(&t.ID, &t.TenantID, &t.Title, &t.Description, &t.Status,
		&t.Priority, &t.Category, &t.AssigneeID, &t.CreatorID, &t.RelatedDevice,
		&t.RelatedTask, &tagsJSON, &createdAt, &updatedAt, &resolvedAt); err != nil {
		return nil
	}
	t.CreatedAt = createdAt
	t.UpdatedAt = updatedAt
	if resolvedAt.Valid {
		rt := resolvedAt.Time
		t.ResolvedAt = &rt
	}
	if tagsJSON != "" {
		if err := json.Unmarshal([]byte(tagsJSON), &t.Tags); err != nil {
			log.Printf("[store] scanTicket 解析 tags JSON 失败 (ticket=%s): %v", t.ID, err)
		}
	}
	return &t
}

// marshalTags 将 Tags 序列化为 JSON 文本（空切片存空串）。
func marshalTags(tags []string) string {
	if tags == nil {
		return ""
	}
	b, err := json.Marshal(tags)
	if err != nil {
		return ""
	}
	return string(b)
}

// resolvedAtValue 将 *time.Time 转换为 sql.NullTime（nil → Invalid）。
func resolvedAtValue(rt *time.Time) sql.NullTime {
	if rt == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *rt, Valid: true}
}

// CreateTicket 创建工单（按 ID 幂等；ID 为空时分配随机 ID）。
//
// 行为：
//   - t == nil 返回 nil；
//   - TenantID 为空时归一为 default（与 K8s 集群一致）；
//   - ID 为空时分配随机 ID（新建场景）；
//   - CreatedAt 为零值时填当前时间（新建场景）；
//   - UpdatedAt 始终刷新为当前时间；
//   - Status 为空时默认 "open"；
//   - Priority 为空时默认 "medium"；
//   - Category 为空时默认 "incident"；
//   - INSERT ... ON DUPLICATE KEY UPDATE 实现 upsert（按 id 幂等），
//     tenant_id 仅插入不更新，防 upsert 改写归属；
//   - DB 失败时 log.Printf + 返回 nil。
func (s *SQLStore) CreateTicket(tenantID string, t *Ticket) *Ticket {
	if t == nil {
		return nil
	}
	// 租户隔离：空租户归一为 default。
	if tenantID == "" {
		tenantID = "default"
	}
	t.TenantID = tenantID
	now := time.Now().UTC()
	if t.ID == "" {
		t.ID = randTicketID()
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	if t.Status == "" {
		t.Status = "open"
	}
	if t.Priority == "" {
		t.Priority = "medium"
	}
	if t.Category == "" {
		t.Category = "incident"
	}
	t.UpdatedAt = now
	tagsJSON := marshalTags(t.Tags)
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO tickets (id, tenant_id, title, description, status, priority, category,
		 assignee_id, creator_id, related_device, related_task, tags, created_at, updated_at, resolved_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE title=VALUES(title), description=VALUES(description),
		 status=VALUES(status), priority=VALUES(priority), category=VALUES(category),
		 assignee_id=VALUES(assignee_id), creator_id=VALUES(creator_id),
		 related_device=VALUES(related_device), related_task=VALUES(related_task),
		 tags=VALUES(tags), updated_at=VALUES(updated_at), resolved_at=VALUES(resolved_at)`,
		t.ID, t.TenantID, t.Title, t.Description, t.Status, t.Priority, t.Category,
		t.AssigneeID, t.CreatorID, t.RelatedDevice, t.RelatedTask, tagsJSON,
		t.CreatedAt, t.UpdatedAt, resolvedAtValue(t.ResolvedAt)); err != nil {
		log.Printf("[store] CreateTicket 插入失败 (tenant=%s ticket=%s): %v", tenantID, t.ID, err)
		return nil
	}
	return cloneTicket(t)
}

// GetTicket 按 (tenantID, id) 返回单个工单（深拷贝；不存在或租户不匹配返回 (nil, false)）。
func (s *SQLStore) GetTicket(tenantID, id string) (*Ticket, bool) {
	row := s.db.QueryRowContext(context.Background(),
		`SELECT id, tenant_id, title, description, status, priority, category, assignee_id,
		 creator_id, related_device, related_task, tags, created_at, updated_at, resolved_at
		  FROM tickets WHERE id=? AND tenant_id=?`, id, tenantID)
	t := scanTicket(row)
	if t == nil {
		return nil, false
	}
	return t, true
}

// UpdateTicket 更新工单（按 t.ID 定位，校验 tenantID 归属）。
//
// 行为：
//   - t == nil 或 ID 为空返回 (nil, false)；
//   - 先 GetTicket 校验存在 + 租户归属，不存在返回 (nil, false)；
//   - CreatedAt / TenantID 不可改（保留原值，防越权改归属）；
//   - UpdatedAt 始终刷新为当前时间；
//   - 返回更新后的工单（深拷贝）。
func (s *SQLStore) UpdateTicket(tenantID string, t *Ticket) (*Ticket, bool) {
	if t == nil || t.ID == "" {
		return nil, false
	}
	// 先 SELECT 校验存在 + 租户归属。
	existing, ok := s.GetTicket(tenantID, t.ID)
	if !ok {
		return nil, false
	}
	// 保留不可改字段。
	t.ID = existing.ID
	t.TenantID = existing.TenantID
	t.CreatedAt = existing.CreatedAt
	t.UpdatedAt = time.Now().UTC()
	tagsJSON := marshalTags(t.Tags)
	if _, err := s.db.ExecContext(context.Background(),
		`UPDATE tickets SET title=?, description=?, status=?, priority=?, category=?,
		 assignee_id=?, creator_id=?, related_device=?, related_task=?, tags=?,
		 updated_at=?, resolved_at=? WHERE id=? AND tenant_id=?`,
		t.Title, t.Description, t.Status, t.Priority, t.Category, t.AssigneeID,
		t.CreatorID, t.RelatedDevice, t.RelatedTask, tagsJSON, t.UpdatedAt,
		resolvedAtValue(t.ResolvedAt), t.ID, t.TenantID); err != nil {
		log.Printf("[store] UpdateTicket 更新失败 (tenant=%s ticket=%s): %v", tenantID, t.ID, err)
		return nil, false
	}
	return cloneTicket(t), true
}

// ListTickets 返回指定租户的工单列表（按 filter 过滤 + 按创建时间降序）。
//
// filter 字段为空串时表示不过滤该字段。返回深拷贝避免外部修改。
// 动态拼接 WHERE 子句 + args（与 sql_k8s.go ListK8sClusters 风格一致）。
func (s *SQLStore) ListTickets(tenantID string, filter TicketFilter) []*Ticket {
	q := `SELECT id, tenant_id, title, description, status, priority, category, assignee_id,
	 creator_id, related_device, related_task, tags, created_at, updated_at, resolved_at
	  FROM tickets WHERE tenant_id=?`
	args := []interface{}{tenantID}
	if filter.Status != "" {
		q += ` AND status=?`
		args = append(args, filter.Status)
	}
	if filter.Priority != "" {
		q += ` AND priority=?`
		args = append(args, filter.Priority)
	}
	if filter.Category != "" {
		q += ` AND category=?`
		args = append(args, filter.Category)
	}
	if filter.AssigneeID != "" {
		q += ` AND assignee_id=?`
		args = append(args, filter.AssigneeID)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := s.db.QueryContext(context.Background(), q, args...)
	if err != nil {
		log.Printf("[store] ListTickets 查询失败 (tenant=%s): %v", tenantID, err)
		return []*Ticket{}
	}
	defer rows.Close()
	out := make([]*Ticket, 0)
	for rows.Next() {
		if t := scanTicket(rows); t != nil {
			out = append(out, t)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] ListTickets 遍历失败: %v", err)
	}
	return out
}

// CloseTicket 关闭工单：置 Status="closed" + ResolvedAt=now + UpdatedAt=now。
// 不存在或租户不匹配返回 (nil, false)。返回更新后的工单（深拷贝）。
func (s *SQLStore) CloseTicket(tenantID, id string) (*Ticket, bool) {
	// 先 SELECT 校验存在 + 租户归属。
	existing, ok := s.GetTicket(tenantID, id)
	if !ok {
		return nil, false
	}
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(context.Background(),
		`UPDATE tickets SET status='closed', resolved_at=?, updated_at=?
		 WHERE id=? AND tenant_id=?`, now, now, id, tenantID); err != nil {
		log.Printf("[store] CloseTicket 更新失败 (tenant=%s ticket=%s): %v", tenantID, id, err)
		return nil, false
	}
	// 返回更新后的工单（在 existing 基础上应用关闭语义，避免再次查询）。
	existing.Status = "closed"
	existing.ResolvedAt = &now
	existing.UpdatedAt = now
	return cloneTicket(existing), true
}
