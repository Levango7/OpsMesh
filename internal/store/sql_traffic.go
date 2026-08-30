package store

// sql_traffic.go 实现 SQLStore 的 TrafficStore 子接口（Phase 2 流量治理，生产就绪）。
//
// 表结构：traffic_policies（id PK + tenant_id + name + service_name + type +
// canary_weights JSON + mirror_percent + timeout + retries + retry_timeout +
// max_conns + max_requests + status + created_at + updated_at）。
// 迁移文件 migrations/011_p2_argocd_pipeline_traffic.sql 幂等建表。
//
// 设计要点（与 sql_k8s.go / sql_secret.go 风格一致）：
//   - JSON 列 canary_weights（map[string]int）以 TEXT 存储，用 encoding/json 序列化；
//   - CreatePolicy 用 INSERT ... ON DUPLICATE KEY UPDATE 实现幂等 upsert；
//   - Get/Update/Delete 均 WHERE id=? AND tenant_id=? 实现租户隔离；
//   - ListPolicies 按 created_at DESC；
//   - EnablePolicy/DisablePolicy 置 status='active'/'inactive' + updated_at=now，
//     先 SELECT 校验存在 + 租户归属，不存在返回 (nil, false)，返回更新后的 Policy；
//   - ID 生成复用 memory_traffic.go 的 randTrafficID()（前缀 traffic-）；
//   - DB 不可用时返回零值（nil/false/空 slice），不 panic。

import (
	"context"
	"encoding/json"
	"log"
	"time"
)

// scanTrafficPolicy 从一行扫描出 *TrafficPolicy。
// 列顺序：id, tenant_id, name, service_name, type, canary_weights, mirror_percent,
// timeout, retries, retry_timeout, max_conns, max_requests, status, created_at, updated_at。
func scanTrafficPolicy(row rowScanner) *TrafficPolicy {
	var p TrafficPolicy
	var canaryJSON []byte
	var createdAt, updatedAt time.Time
	if err := row.Scan(
		&p.ID, &p.TenantID, &p.Name, &p.ServiceName, &p.Type, &canaryJSON,
		&p.MirrorPercent, &p.Timeout, &p.Retries, &p.RetryTimeout,
		&p.MaxConns, &p.MaxRequests, &p.Status, &createdAt, &updatedAt,
	); err != nil {
		return nil
	}
	p.CreatedAt = createdAt
	p.UpdatedAt = updatedAt
	if len(canaryJSON) > 0 {
		// 反序列化失败不致命：保留空 CanaryWeights，避免单条坏数据让整个 List 崩，但必须留痕。
		if err := json.Unmarshal(canaryJSON, &p.CanaryWeights); err != nil {
			log.Printf("[store] scanTrafficPolicy 反序列化 canary_weights 失败（保留空 CanaryWeights 继续）: %v", err)
		}
	}
	return &p
}

// trafficPolicyColumns 是 traffic_policies 表的查询列清单（与 scanTrafficPolicy 顺序一致）。
const trafficPolicyColumns = `id, tenant_id, name, service_name, type, canary_weights, mirror_percent,
 timeout, retries, retry_timeout, max_conns, max_requests, status, created_at, updated_at`

// marshalCanaryWeights 将 map[string]int 序列化为 JSON 字节串（nil 时返回 nil）。
func marshalCanaryWeights(w map[string]int) []byte {
	if w == nil {
		return nil
	}
	b, err := json.Marshal(w)
	if err != nil {
		log.Printf("[store] marshalCanaryWeights 失败: %v", err)
		return nil
	}
	return b
}

// CreatePolicy 创建流量策略（ID 为空时分配随机 ID）。
//
// 行为与 MemoryStore.CreatePolicy 一致：
//   - 空租户归一为 default；
//   - ID 为空时由 randTrafficID() 分配；
//   - Status 空 → inactive（与 memory 一致）；
//   - CreatedAt 零值填 now；UpdatedAt 始终刷新为 now；
//   - INSERT ... ON DUPLICATE KEY UPDATE 实现幂等 upsert（按 id 唯一）。
func (s *SQLStore) CreatePolicy(tenantID string, p *TrafficPolicy) *TrafficPolicy {
	if p == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	p.TenantID = tenantID
	now := time.Now().UTC()
	if p.ID == "" {
		p.ID = randTrafficID()
	}
	if p.Status == "" {
		p.Status = "inactive"
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	canaryJSON := marshalCanaryWeights(p.CanaryWeights)
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO traffic_policies (id, tenant_id, name, service_name, type, canary_weights,
		 mirror_percent, timeout, retries, retry_timeout, max_conns, max_requests, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE name=VALUES(name), service_name=VALUES(service_name), type=VALUES(type),
		 canary_weights=VALUES(canary_weights), mirror_percent=VALUES(mirror_percent), timeout=VALUES(timeout),
		 retries=VALUES(retries), retry_timeout=VALUES(retry_timeout), max_conns=VALUES(max_conns),
		 max_requests=VALUES(max_requests), status=VALUES(status), updated_at=VALUES(updated_at)`,
		p.ID, p.TenantID, p.Name, p.ServiceName, p.Type, canaryJSON,
		p.MirrorPercent, p.Timeout, p.Retries, p.RetryTimeout,
		p.MaxConns, p.MaxRequests, p.Status, p.CreatedAt, p.UpdatedAt); err != nil {
		log.Printf("[store] CreatePolicy 插入失败 (tenant=%s id=%s): %v", tenantID, p.ID, err)
		return nil
	}
	return p
}

// GetPolicy 按 (tenantID, id) 返回单个策略。不存在返回 (nil, false)。
func (s *SQLStore) GetPolicy(tenantID, id string) (*TrafficPolicy, bool) {
	row := s.db.QueryRowContext(context.Background(),
		`SELECT `+trafficPolicyColumns+` FROM traffic_policies WHERE id=? AND tenant_id=?`, id, tenantID)
	p := scanTrafficPolicy(row)
	if p == nil {
		return nil, false
	}
	return p, true
}

// UpdatePolicy 更新策略（按 p.ID 定位，校验 tenantID 归属）。
//
// 行为与 MemoryStore.UpdatePolicy 一致：
//   - p==nil 或 p.ID=="" 返回 (nil, false)；
//   - 不存在或租户不匹配返回 (nil, false)；
//   - CreatedAt 保留原值，UpdatedAt 刷新为 now。
func (s *SQLStore) UpdatePolicy(tenantID string, p *TrafficPolicy) (*TrafficPolicy, bool) {
	if p == nil || p.ID == "" {
		return nil, false
	}
	existing, ok := s.GetPolicy(tenantID, p.ID)
	if !ok {
		return nil, false
	}
	p.TenantID = existing.TenantID
	p.CreatedAt = existing.CreatedAt
	p.UpdatedAt = time.Now().UTC()
	canaryJSON := marshalCanaryWeights(p.CanaryWeights)
	if _, err := s.db.ExecContext(context.Background(),
		`UPDATE traffic_policies SET name=?, service_name=?, type=?, canary_weights=?, mirror_percent=?,
		 timeout=?, retries=?, retry_timeout=?, max_conns=?, max_requests=?, status=?, updated_at=?
		 WHERE id=? AND tenant_id=?`,
		p.Name, p.ServiceName, p.Type, canaryJSON, p.MirrorPercent,
		p.Timeout, p.Retries, p.RetryTimeout, p.MaxConns, p.MaxRequests, p.Status, p.UpdatedAt,
		p.ID, tenantID); err != nil {
		log.Printf("[store] UpdatePolicy 失败 (tenant=%s id=%s): %v", tenantID, p.ID, err)
		return nil, false
	}
	return p, true
}

// ListPolicies 返回指定租户的全部策略（按 created_at DESC）。
func (s *SQLStore) ListPolicies(tenantID string) []*TrafficPolicy {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT `+trafficPolicyColumns+` FROM traffic_policies WHERE tenant_id=? ORDER BY created_at DESC`, tenantID)
	if err != nil {
		log.Printf("[store] ListPolicies 查询失败 (tenant=%s): %v", tenantID, err)
		return []*TrafficPolicy{}
	}
	defer rows.Close()
	out := make([]*TrafficPolicy, 0)
	for rows.Next() {
		if p := scanTrafficPolicy(rows); p != nil {
			out = append(out, p)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] ListPolicies 遍历失败: %v", err)
	}
	return out
}

// DeletePolicy 删除策略，返回是否删除成功（不存在或租户不匹配返回 false）。
func (s *SQLStore) DeletePolicy(tenantID, id string) bool {
	res, err := s.db.ExecContext(context.Background(),
		`DELETE FROM traffic_policies WHERE id=? AND tenant_id=?`, id, tenantID)
	if err != nil {
		log.Printf("[store] DeletePolicy 失败 (tenant=%s id=%s): %v", tenantID, id, err)
		return false
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		log.Printf("[store] DeletePolicy RowsAffected 失败 (tenant=%s id=%s): %v", tenantID, id, rowsErr)
		return false
	}
	return n > 0
}

// EnablePolicy 启用策略：置 status='active' + updated_at=now。
//
// 行为与 MemoryStore.EnablePolicy 一致：
//   - 先 SELECT 校验存在 + 租户归属，不存在返回 (nil, false)；
//   - 返回更新后的 Policy。
func (s *SQLStore) EnablePolicy(tenantID, id string) (*TrafficPolicy, bool) {
	existing, ok := s.GetPolicy(tenantID, id)
	if !ok {
		return nil, false
	}
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(context.Background(),
		`UPDATE traffic_policies SET status='active', updated_at=? WHERE id=? AND tenant_id=?`,
		now, id, tenantID); err != nil {
		log.Printf("[store] EnablePolicy 失败 (tenant=%s id=%s): %v", tenantID, id, err)
		return nil, false
	}
	existing.Status = "active"
	existing.UpdatedAt = now
	return existing, true
}

// DisablePolicy 禁用策略：置 status='inactive' + updated_at=now。
//
// 行为与 MemoryStore.DisablePolicy 一致：
//   - 先 SELECT 校验存在 + 租户归属，不存在返回 (nil, false)；
//   - 返回更新后的 Policy。
func (s *SQLStore) DisablePolicy(tenantID, id string) (*TrafficPolicy, bool) {
	existing, ok := s.GetPolicy(tenantID, id)
	if !ok {
		return nil, false
	}
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(context.Background(),
		`UPDATE traffic_policies SET status='inactive', updated_at=? WHERE id=? AND tenant_id=?`,
		now, id, tenantID); err != nil {
		log.Printf("[store] DisablePolicy 失败 (tenant=%s id=%s): %v", tenantID, id, err)
		return nil, false
	}
	existing.Status = "inactive"
	existing.UpdatedAt = now
	return existing, true
}
