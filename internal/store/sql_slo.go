// sql_slo.go 实现 SQLStore 的 SLOStore 子接口（Phase 1 SLO 管理，生产就绪）。
//
// 表结构：slos（id PK + tenant_id + name + description + service_name + target +
// window + slis JSON + created_at + updated_at）。迁移文件
// migrations/010_p1_slo_ticket.sql 幂等建表。
//
// 设计要点（与 sql_k8s.go / sql_secret.go 风格一致）：
//   - SLIs 以 JSON 数组存储在 slis TEXT 列；空切片存空串，读取时空串跳过 Unmarshal；
//   - CreateSLO 按 ID 幂等（INSERT ... ON DUPLICATE KEY UPDATE），tenant_id 仅插入
//     不更新（防 upsert 改写归属）；
//   - ListSLOs 按创建时间升序返回（与 ListK8sClusters 一致）；
//   - UpdateSLO 先 SELECT 校验存在 + 租户归属，再 UPDATE，保留原 CreatedAt/TenantID；
//   - SLIStatus 复用 GetSLO 取 SLO，对每个 SLI 返回模拟状态（MVP，未接入 Prometheus）；
//   - DB 不可用时返回零值（nil/false/空 slice），不 panic；
//   - ID 生成复用 memory_slo.go 的 randSLOID（"slo-" + 16 字节 hex）。
package store

import (
	"context"

	"encoding/json"
	"log"
	"time"
)

// scanSLO 从一行扫描出 *SLO（slis 为 JSON 文本列）。
// 列顺序：id, tenant_id, name, description, service_name, target, window, slis,
// created_at, updated_at。无行或扫描失败返回 nil。
func scanSLO(row rowScanner) *SLO {
	var s SLO
	var slisJSON string
	var createdAt, updatedAt time.Time
	if err := row.Scan(&s.ID, &s.TenantID, &s.Name, &s.Description, &s.ServiceName,
		&s.Target, &s.Window, &slisJSON, &createdAt, &updatedAt); err != nil {
		return nil
	}
	s.CreatedAt = createdAt
	s.UpdatedAt = updatedAt
	if slisJSON != "" {
		if err := json.Unmarshal([]byte(slisJSON), &s.SLIs); err != nil {
			log.Printf("[store] scanSLO 解析 slis JSON 失败 (slo=%s): %v", s.ID, err)
		}
	}
	return &s
}

// CreateSLO 创建 SLO（按 ID 幂等；ID 为空时分配随机 ID）。
//
// 行为：
//   - slo == nil 返回 nil；
//   - TenantID 为空时归一为 default（与 K8s 集群一致）；
//   - ID 为空时分配随机 ID（新建场景）；
//   - CreatedAt 为零值时填当前时间（新建场景）；
//   - UpdatedAt 始终刷新为当前时间；
//   - INSERT ... ON DUPLICATE KEY UPDATE 实现 upsert（按 id 幂等），
//     tenant_id 仅插入不更新，防 upsert 改写归属；
//   - DB 失败时 log.Printf + 返回 nil。
func (s *SQLStore) CreateSLO(tenantID string, slo *SLO) *SLO {
	if slo == nil {
		return nil
	}
	// 租户隔离：空租户归一为 default。
	if tenantID == "" {
		tenantID = "default"
	}
	slo.TenantID = tenantID
	now := time.Now().UTC()
	if slo.ID == "" {
		slo.ID = randSLOID()
	}
	if slo.CreatedAt.IsZero() {
		slo.CreatedAt = now
	}
	slo.UpdatedAt = now
	// SLIs 序列化为 JSON 文本（空切片存空串）。
	var slisJSON string
	if slo.SLIs != nil {
		b, err := json.Marshal(slo.SLIs)
		if err != nil {
			log.Printf("[store] CreateSLO 序列化 slis 失败 (slo=%s): %v", slo.ID, err)
			return nil
		}
		slisJSON = string(b)
	}
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO slos (id, tenant_id, name, description, service_name, target, window, slis, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE name=VALUES(name), description=VALUES(description),
		 service_name=VALUES(service_name), target=VALUES(target), window=VALUES(window),
		 slis=VALUES(slis), updated_at=VALUES(updated_at)`,
		slo.ID, slo.TenantID, slo.Name, slo.Description, slo.ServiceName,
		slo.Target, slo.Window, slisJSON, slo.CreatedAt, slo.UpdatedAt); err != nil {
		log.Printf("[store] CreateSLO 插入失败 (tenant=%s slo=%s): %v", tenantID, slo.ID, err)
		return nil
	}
	return cloneSLO(slo)
}

// GetSLO 按 (tenantID, id) 返回单个 SLO（深拷贝；不存在或租户不匹配返回 (nil, false)）。
func (s *SQLStore) GetSLO(tenantID, id string) (*SLO, bool) {
	row := s.db.QueryRowContext(context.Background(),
		`SELECT id, tenant_id, name, description, service_name, target, window, slis, created_at, updated_at
		  FROM slos WHERE id=? AND tenant_id=?`, id, tenantID)
	slo := scanSLO(row)
	if slo == nil {
		return nil, false
	}
	return slo, true
}

// UpdateSLO 更新 SLO（按 slo.ID 定位，校验 tenantID 归属）。
//
// 行为：
//   - slo == nil 或 ID 为空返回 (nil, false)；
//   - 先 GetSLO 校验存在 + 租户归属，不存在返回 (nil, false)；
//   - CreatedAt / TenantID 不可改（保留原值，防越权改归属）；
//   - UpdatedAt 始终刷新为当前时间；
//   - 返回更新后的 SLO（深拷贝）。
func (s *SQLStore) UpdateSLO(tenantID string, slo *SLO) (*SLO, bool) {
	if slo == nil || slo.ID == "" {
		return nil, false
	}
	// 先 SELECT 校验存在 + 租户归属。
	existing, ok := s.GetSLO(tenantID, slo.ID)
	if !ok {
		return nil, false
	}
	// 保留不可改字段。
	slo.ID = existing.ID
	slo.TenantID = existing.TenantID
	slo.CreatedAt = existing.CreatedAt
	slo.UpdatedAt = time.Now().UTC()
	// SLIs 序列化为 JSON 文本。
	var slisJSON string
	if slo.SLIs != nil {
		b, err := json.Marshal(slo.SLIs)
		if err != nil {
			log.Printf("[store] UpdateSLO 序列化 slis 失败 (slo=%s): %v", slo.ID, err)
			return nil, false
		}
		slisJSON = string(b)
	}
	if _, err := s.db.ExecContext(context.Background(),
		`UPDATE slos SET name=?, description=?, service_name=?, target=?, window=?, slis=?, updated_at=?
		 WHERE id=? AND tenant_id=?`,
		slo.Name, slo.Description, slo.ServiceName, slo.Target, slo.Window, slisJSON,
		slo.UpdatedAt, slo.ID, slo.TenantID); err != nil {
		log.Printf("[store] UpdateSLO 更新失败 (tenant=%s slo=%s): %v", tenantID, slo.ID, err)
		return nil, false
	}
	return cloneSLO(slo), true
}

// ListSLOs 返回指定租户的全部 SLO（按创建时间升序；深拷贝）。
// tenantID 为空时返回空切片（与 memory 实现一致：tenantID 非空时仅返回同租户 SLO）。
func (s *SQLStore) ListSLOs(tenantID string) []*SLO {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT id, tenant_id, name, description, service_name, target, window, slis, created_at, updated_at
		  FROM slos WHERE tenant_id=? ORDER BY created_at ASC`, tenantID)
	if err != nil {
		log.Printf("[store] ListSLOs 查询失败 (tenant=%s): %v", tenantID, err)
		return []*SLO{}
	}
	defer rows.Close()
	out := make([]*SLO, 0)
	for rows.Next() {
		if slo := scanSLO(rows); slo != nil {
			out = append(out, slo)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] ListSLOs 遍历失败: %v", err)
	}
	return out
}

// DeleteSLO 删除 SLO，返回是否删除成功（不存在或租户不匹配返回 false）。
func (s *SQLStore) DeleteSLO(tenantID, id string) bool {
	res, err := s.db.ExecContext(context.Background(),
		`DELETE FROM slos WHERE id=? AND tenant_id=?`, id, tenantID)
	if err != nil {
		log.Printf("[store] DeleteSLO 失败 (tenant=%s slo=%s): %v", tenantID, id, err)
		return false
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		log.Printf("[store] DeleteSLO RowsAffected 失败 (tenant=%s slo=%s): %v", tenantID, id, rowsErr)
		return false
	}
	return n > 0
}

// SLIStatus 返回指定 SLO 下各 SLI 的当前状态（MVP 返回模拟状态）。
//
// MVP 行为：
//   - SLO 不存在或租户不匹配返回 nil；
//   - 对每个 SLI 返回模拟状态：CurrentValue=99.5, Status="met"（满足目标），
//     LastEvaluated=now。后续可接入 Prometheus 真实评估。
func (s *SQLStore) SLIStatus(tenantID, id string) []*SLIStatus {
	slo, ok := s.GetSLO(tenantID, id)
	if !ok {
		return nil
	}
	now := time.Now().UTC()
	out := make([]*SLIStatus, 0, len(slo.SLIs))
	for _, sli := range slo.SLIs {
		out = append(out, &SLIStatus{
			SLIName:       sli.Name,
			CurrentValue:  99.5, // MVP 模拟值
			TargetValue:   sli.Target,
			Status:        "met", // MVP 假定满足
			LastEvaluated: now,
		})
	}
	return out
}
