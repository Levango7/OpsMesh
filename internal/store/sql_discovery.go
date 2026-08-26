package store

import (
	"context"
	"encoding/json"
	"log"
	"time"
)

// sql_discovery.go 实现 SQLStore 的 ServiceDiscoveryStore 子接口（P0.3 服务发现，MySQL 持久化）。
//
// 表结构：services（service_id PK + tenant_id + service_name + address + port +
// metadata JSON + status + last_heartbeat + created_at），见 migrations/009_p03_services.sql。
//
// 设计要点（与 sql_k8s.go 风格一致）：
//   - RegisterService 用 INSERT ... ON DUPLICATE KEY UPDATE 做 upsert，保留首次 created_at；
//   - Metadata map[string]string 以 JSON 字符串存储在 metadata 列；
//   - DB 不可用时返回零值（nil/false/空 slice），不 panic；
//   - sql.ErrNoRows 视为"不存在"，返回 nil/false 而非错误；
//   - 时间统一使用 time.Now().UTC()。

// scanServiceInstance 从一行扫描出 *ServiceInstance（metadata 列为 JSON 文本）。
// 无行或扫描失败返回 nil。
func scanServiceInstance(row rowScanner) *ServiceInstance {
	var s ServiceInstance
	var metadataStr string
	var lastHeartbeat, createdAt time.Time
	if err := row.Scan(&s.ServiceID, &s.TenantID, &s.ServiceName, &s.Address, &s.Port, &metadataStr, &s.Status, &lastHeartbeat, &createdAt); err != nil {
		return nil
	}
	if metadataStr != "" {
		// 反序列化失败时保留 nil Metadata，不阻断读取。
		var metadata map[string]string
		if err := json.Unmarshal([]byte(metadataStr), &metadata); err == nil {
			s.Metadata = metadata
		}
	}
	s.LastHeartbeat = lastHeartbeat
	s.CreatedAt = createdAt
	return &s
}

// RegisterService 注册一个服务实例（按 ServiceID 幂等 upsert）。
// 已存在则更新可变字段（service_name/address/port/metadata/status/tenant_id/last_heartbeat），
// 不存在则新建并填充 CreatedAt/LastHeartbeat/Status 默认值。
// ON DUPLICATE KEY UPDATE 不更新 created_at，保留首次注册时间。
// 返回更新后的 ServiceInstance 深拷贝副本。
func (s *SQLStore) RegisterService(inst *ServiceInstance) *ServiceInstance {
	if inst == nil {
		return nil
	}
	now := time.Now().UTC()
	if inst.CreatedAt.IsZero() {
		inst.CreatedAt = now
	}
	if inst.LastHeartbeat.IsZero() {
		inst.LastHeartbeat = now
	}
	if inst.Status == "" {
		inst.Status = "healthy"
	}
	// Metadata 序列化为 JSON 字符串；空 map/nil 存空串。
	var metadataJSON string
	if len(inst.Metadata) > 0 {
		if b, err := json.Marshal(inst.Metadata); err == nil {
			metadataJSON = string(b)
		} else {
			log.Printf("[store] RegisterService 序列化 metadata 失败 service_id=%s: %v", inst.ServiceID, err)
		}
	}
	// INSERT ... ON DUPLICATE KEY UPDATE 实现 upsert（按 service_id 幂等）。
	// created_at 仅插入不更新，保留首次注册时间。
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO services (service_id, tenant_id, service_name, address, port, metadata, status, last_heartbeat, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE service_name=VALUES(service_name), address=VALUES(address), port=VALUES(port),
		 metadata=VALUES(metadata), status=VALUES(status), tenant_id=VALUES(tenant_id), last_heartbeat=VALUES(last_heartbeat)`,
		inst.ServiceID, inst.TenantID, inst.ServiceName, inst.Address, inst.Port, metadataJSON, inst.Status, inst.LastHeartbeat, inst.CreatedAt); err != nil {
		log.Printf("[store] RegisterService 失败 service_id=%s: %v", inst.ServiceID, err)
		return nil
	}
	return cloneServiceInstance(inst)
}

// DeregisterService 反注册服务实例。返回是否删除成功（不存在返回 false）。
// tenantID 非空时附加租户隔离条件，避免跨租户误删。
func (s *SQLStore) DeregisterService(tenantID, serviceID string) bool {
	q := `DELETE FROM services WHERE service_id=?`
	args := []interface{}{serviceID}
	if tenantID != "" {
		q += ` AND tenant_id=?`
		args = append(args, tenantID)
	}
	res, err := s.db.ExecContext(context.Background(), q, args...)
	if err != nil {
		log.Printf("[store] DeregisterService 失败 service_id=%s: %v", serviceID, err)
		return false
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		log.Printf("[store] DeregisterService RowsAffected 失败 service_id=%s: %v", serviceID, rowsErr)
		return false
	}
	return n > 0
}

// ServiceInstances 返回指定服务名下的全部实例（按 tenantID 隔离）。
// tenantID 空串=全部租户。结果按 service_id 升序稳定输出。
func (s *SQLStore) ServiceInstances(tenantID, serviceName string) []*ServiceInstance {
	q :=
		`
SELECT service_id, tenant_id, service_name, address, port, metadata, status, last_heartbeat, created_at FROM services WHERE service_name=?
`
	args := []interface{}{serviceName}
	if tenantID != "" {
		q +=
			`
 AND tenant_id=?
`
		args = append(args, tenantID)
	}
	q +=
		`
 ORDER BY service_id
`
	rows, err := s.db.QueryContext(context.Background(), q, args...)
	if err != nil {
		log.Printf("[store] ServiceInstances 失败 service_name=%s: %v", serviceName, err)
		return nil
	}
	defer rows.Close()
	out := make([]*ServiceInstance, 0)
	for rows.Next() {
		if inst := scanServiceInstance(rows); inst != nil {
			out = append(out, inst)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] ServiceInstances 遍历失败 service_name=%s: %v", serviceName, err)
	}
	sortServiceInstances(out)
	return out
}

// AllServices 返回全部服务实例（按 tenantID 隔离；空串=全部租户）。
// 结果按 service_id 升序稳定输出。
func (s *SQLStore) AllServices(tenantID string) []*ServiceInstance {
	q :=
		`
SELECT service_id, tenant_id, service_name, address, port, metadata, status, last_heartbeat, created_at FROM services
`
	var args []interface{}
	if tenantID != "" {
		q +=
			`
 WHERE tenant_id=?
`
		args = append(args, tenantID)
	}
	q +=
		`
 ORDER BY service_id
`
	rows, err := s.db.QueryContext(context.Background(), q, args...)
	if err != nil {
		log.Printf("[store] AllServices 失败 tenant_id=%s: %v", tenantID, err)
		return nil
	}
	defer rows.Close()
	out := make([]*ServiceInstance, 0)
	for rows.Next() {
		if inst := scanServiceInstance(rows); inst != nil {
			out = append(out, inst)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] AllServices 遍历失败 tenant_id=%s: %v", tenantID, err)
	}
	sortServiceInstances(out)
	return out
}

// HeartbeatService 服务实例心跳：刷新 last_heartbeat，可选更新 status。
// status 为空时仅更新 last_heartbeat，不改动 status 列。
// tenantID 非空时附加租户隔离条件。返回是否更新成功（不存在返回 false）。
func (s *SQLStore) HeartbeatService(tenantID, serviceID, status string) bool {
	now := time.Now().UTC()
	var q string
	var args []interface{}
	if status == "" {
		q = `UPDATE services SET last_heartbeat=? WHERE service_id=?`
		args = []interface{}{now, serviceID}
	} else {
		q = `UPDATE services SET last_heartbeat=?, status=? WHERE service_id=?`
		args = []interface{}{now, status, serviceID}
	}
	if tenantID != "" {
		q += ` AND tenant_id=?`
		args = append(args, tenantID)
	}
	res, err := s.db.ExecContext(context.Background(), q, args...)
	if err != nil {
		log.Printf("[store] HeartbeatService 失败 service_id=%s: %v", serviceID, err)
		return false
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		log.Printf("[store] HeartbeatService RowsAffected 失败 service_id=%s: %v", serviceID, rowsErr)
		return false
	}
	return n > 0
}

// StaleServices 返回最后心跳早于 maxAge 的过期实例（按 tenantID 隔离）。
// LastHeartbeat 早于 now-maxAge 视为过期；maxAge<=0 时不返回任何实例。
// tenantID 空串=全部租户。结果按 service_id 升序稳定输出。
func (s *SQLStore) StaleServices(tenantID string, maxAge time.Duration) []*ServiceInstance {
	if maxAge <= 0 {
		return nil
	}
	threshold := time.Now().Add(-maxAge)
	q :=
		`
SELECT service_id, tenant_id, service_name, address, port, metadata, status, last_heartbeat, created_at FROM services WHERE last_heartbeat < ?
`
	args := []interface{}{threshold}
	if tenantID != "" {
		q +=
			`
 AND tenant_id=?
`
		args = append(args, tenantID)
	}
	q +=
		`
 ORDER BY service_id
`
	rows, err := s.db.QueryContext(context.Background(), q, args...)
	if err != nil {
		log.Printf("[store] StaleServices 失败 tenant_id=%s: %v", tenantID, err)
		return nil
	}
	defer rows.Close()
	out := make([]*ServiceInstance, 0)
	for rows.Next() {
		if inst := scanServiceInstance(rows); inst != nil {
			out = append(out, inst)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] StaleServices 遍历失败 tenant_id=%s: %v", tenantID, err)
	}
	sortServiceInstances(out)
	return out
}
