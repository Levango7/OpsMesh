package store

// sql_network.go 实现 SQLStore 的 NetworkStore 子接口（Phase 4 网络管理，生产就绪）。
//
// 表结构：
//   - network_devices（id PK + tenant_id + name + type + vendor + model + ip + mask +
//     mac + location + snmp_community + status + config + created_at + updated_at）；
//   - network_metrics（id BIGINT AUTO_INCREMENT PK + device_id + tenant_id + timestamp +
//     cpu_usage + memory_usage + temperature + uptime）。
// 迁移文件 migrations/013_p4_automation_network.sql 幂等建表。
//
// 设计要点（与 sql_secret.go / sql_k8s.go 风格一致）：
//   - network_metrics 用自增 BIGINT 主键（时序追加写），无需应用层 ID；
//   - StoreNetworkMetrics 追加写，不保留最近 N 条（DB 容量由 DBA 定期清理；
//     memory 实现保留 100 条，SQL 层不模拟环形缓冲）；Timestamp 零值填 now；
//   - GetNetworkMetrics ORDER BY timestamp DESC LIMIT 1 取最近一条；
//   - UpdateNetworkConfig 仅更新 config + updated_at，先 SELECT 校验存在 + 租户归属，
//     返回更新后的 Device；
//   - 租户隔离：Get/Update/Delete/UpdateConfig 均 WHERE id=? AND tenant_id=?，
//     List 均 WHERE tenant_id=?；
//   - ID 生成复用 memory_network.go 的 randNetworkDeviceID；
//   - 接口签名无 error 返回值，SQL 错误时 log.Printf + 返回零值（nil/false/空 slice）；
//   - 全部查询使用 context.Background() + ? 占位符。

import (
	"context"
	"database/sql"
	"log"
	"time"
)

// scanNetworkDevice 从一行扫描出 *NetworkDevice。
// 列顺序：id, tenant_id, name, type, vendor, model, ip, mask, mac, location,
//
//	snmp_community, status, config, created_at, updated_at。
func scanNetworkDevice(row rowScanner) *NetworkDevice {
	var d NetworkDevice
	var createdAt, updatedAt time.Time
	if err := row.Scan(
		&d.ID, &d.TenantID, &d.Name, &d.Type, &d.Vendor, &d.Model,
		&d.IP, &d.Mask, &d.Mac, &d.Location, &d.SnmpCommunity,
		&d.Status, &d.Config, &createdAt, &updatedAt,
	); err != nil {
		return nil
	}
	d.CreatedAt = createdAt
	d.UpdatedAt = updatedAt
	return &d
}

// CreateNetworkDevice 创建网络设备（ID 为空时分配随机 ID）。
// TenantID 为空时归一为 default。Status 空时默认 unknown。返回持久化后的设备（含分配的 ID）。
func (s *SQLStore) CreateNetworkDevice(tenantID string, d *NetworkDevice) *NetworkDevice {
	if d == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	d.TenantID = tenantID
	if d.ID == "" {
		d.ID = randNetworkDeviceID()
	}
	if d.Status == "" {
		d.Status = "unknown"
	}
	now := time.Now().UTC()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	d.UpdatedAt = now
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO network_devices
		   (id, tenant_id, name, type, vendor, model, ip, mask, mac, location,
		    snmp_community, status, config, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE
		   name=VALUES(name), type=VALUES(type), vendor=VALUES(vendor), model=VALUES(model),
		   ip=VALUES(ip), mask=VALUES(mask), mac=VALUES(mac), location=VALUES(location),
		   snmp_community=VALUES(snmp_community), status=VALUES(status),
		   config=VALUES(config), updated_at=VALUES(updated_at)`,
		d.ID, d.TenantID, d.Name, d.Type, d.Vendor, d.Model,
		d.IP, d.Mask, d.Mac, d.Location, d.SnmpCommunity,
		d.Status, d.Config, d.CreatedAt, d.UpdatedAt); err != nil {
		log.Printf("[store] CreateNetworkDevice 插入失败 (tenant=%s id=%s): %v", tenantID, d.ID, err)
		return nil
	}
	return d
}

// GetNetworkDevice 按 (tenantID, id) 返回单个网络设备（不存在返回 (nil, false)）。
func (s *SQLStore) GetNetworkDevice(tenantID, id string) (*NetworkDevice, bool) {
	row := s.db.QueryRowContext(context.Background(),
		`SELECT id, tenant_id, name, type, vendor, model, ip, mask, mac, location,
		         snmp_community, status, config, created_at, updated_at
		   FROM network_devices WHERE id=? AND tenant_id=?`, id, tenantID)
	d := scanNetworkDevice(row)
	if d == nil {
		return nil, false
	}
	return d, true
}

// ListNetworkDevices 返回指定租户的全部网络设备（按创建时间降序）。
func (s *SQLStore) ListNetworkDevices(tenantID string) []*NetworkDevice {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT id, tenant_id, name, type, vendor, model, ip, mask, mac, location,
		         snmp_community, status, config, created_at, updated_at
		   FROM network_devices WHERE tenant_id=? ORDER BY created_at DESC`, tenantID)
	if err != nil {
		log.Printf("[store] ListNetworkDevices 查询失败 (tenant=%s): %v", tenantID, err)
		return []*NetworkDevice{}
	}
	defer rows.Close()
	out := make([]*NetworkDevice, 0)
	for rows.Next() {
		if d := scanNetworkDevice(rows); d != nil {
			out = append(out, d)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] ListNetworkDevices 遍历失败: %v", err)
	}
	return out
}

// UpdateNetworkDevice 更新网络设备（按 d.ID 定位，校验 tenantID 归属）。
// 不存在或越权返回 (nil, false)。CreatedAt 保留原值，UpdatedAt 置 now。
func (s *SQLStore) UpdateNetworkDevice(tenantID string, d *NetworkDevice) (*NetworkDevice, bool) {
	if d == nil || d.ID == "" {
		return nil, false
	}
	existing, ok := s.GetNetworkDevice(tenantID, d.ID)
	if !ok {
		return nil, false
	}
	d.TenantID = existing.TenantID
	d.CreatedAt = existing.CreatedAt
	d.UpdatedAt = time.Now().UTC()
	if _, err := s.db.ExecContext(context.Background(),
		`UPDATE network_devices SET
		   name=?, type=?, vendor=?, model=?, ip=?, mask=?, mac=?, location=?,
		   snmp_community=?, status=?, config=?, updated_at=?
		 WHERE id=? AND tenant_id=?`,
		d.Name, d.Type, d.Vendor, d.Model, d.IP, d.Mask, d.Mac, d.Location,
		d.SnmpCommunity, d.Status, d.Config, d.UpdatedAt, d.ID, tenantID); err != nil {
		log.Printf("[store] UpdateNetworkDevice 更新失败 (tenant=%s id=%s): %v", tenantID, d.ID, err)
		return nil, false
	}
	return d, true
}

// DeleteNetworkDevice 删除网络设备，返回是否删除成功（不存在或租户不匹配返回 false）。
func (s *SQLStore) DeleteNetworkDevice(tenantID, id string) bool {
	res, err := s.db.ExecContext(context.Background(),
		`DELETE FROM network_devices WHERE id=? AND tenant_id=?`, id, tenantID)
	if err != nil {
		log.Printf("[store] DeleteNetworkDevice 失败 (tenant=%s id=%s): %v", tenantID, id, err)
		return false
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		log.Printf("[store] DeleteNetworkDevice RowsAffected 失败 (tenant=%s id=%s): %v", tenantID, id, rowsErr)
		return false
	}
	return n > 0
}

// scanNetworkMetrics 从一行扫描出 *NetworkMetrics。
// 列顺序：device_id, tenant_id, timestamp, cpu_usage, memory_usage, temperature, uptime。
func scanNetworkMetrics(row rowScanner) *NetworkMetrics {
	var m NetworkMetrics
	var ts time.Time
	if err := row.Scan(
		&m.DeviceID, &m.TenantID, &ts, &m.CPUUsage, &m.MemoryUsage, &m.Temperature, &m.Uptime,
	); err != nil {
		return nil
	}
	m.Timestamp = ts
	return &m
}

// StoreNetworkMetrics 存储网络设备监控指标（追加写，按 deviceID 关联）。
// Timestamp 零值填 now。TenantID 一并写入便于按租户统计。
func (s *SQLStore) StoreNetworkMetrics(deviceID string, m *NetworkMetrics) {
	if deviceID == "" || m == nil {
		return
	}
	m.DeviceID = deviceID
	if m.Timestamp.IsZero() {
		m.Timestamp = time.Now().UTC()
	}
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO network_metrics
		   (device_id, tenant_id, timestamp, cpu_usage, memory_usage, temperature, uptime)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		m.DeviceID, m.TenantID, m.Timestamp, m.CPUUsage, m.MemoryUsage, m.Temperature, m.Uptime); err != nil {
		log.Printf("[store] StoreNetworkMetrics 插入失败 (device=%s): %v", deviceID, err)
	}
}

// GetNetworkMetrics 返回网络设备最近一次监控指标（按 timestamp DESC LIMIT 1）。
// 不存在返回 nil。
func (s *SQLStore) GetNetworkMetrics(deviceID string) *NetworkMetrics {
	row := s.db.QueryRowContext(context.Background(),
		`SELECT device_id, tenant_id, timestamp, cpu_usage, memory_usage, temperature, uptime
		   FROM network_metrics WHERE device_id=? ORDER BY timestamp DESC LIMIT 1`, deviceID)
	m := scanNetworkMetrics(row)
	if m == nil {
		return nil
	}
	return m
}

// UpdateNetworkConfig 下发网络配置（仅更新 config + updated_at）。
// 先 SELECT 校验存在 + 租户归属，再 UPDATE，返回更新后的设备。
// 不存在或越权返回 (nil, false)。
func (s *SQLStore) UpdateNetworkConfig(tenantID, id, config string) (*NetworkDevice, bool) {
	existing, ok := s.GetNetworkDevice(tenantID, id)
	if !ok {
		return nil, false
	}
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(context.Background(),
		`UPDATE network_devices SET config=?, updated_at=? WHERE id=? AND tenant_id=?`,
		config, now, id, tenantID); err != nil {
		log.Printf("[store] UpdateNetworkConfig 更新失败 (tenant=%s id=%s): %v", tenantID, id, err)
		return nil, false
	}
 	existing.Config = config
 	existing.UpdatedAt = now
 	return existing, true
 }

// QueryNetworkMetrics 查询指定租户最近时间窗口内的聚合指标均值。
func (s *SQLStore) QueryNetworkMetrics(tenantID string, since time.Time) map[string]float64 {
	if s.db == nil {
		return map[string]float64{"cpu_usage": 0, "memory_usage": 0, "temperature": 0}
	}
	query := `SELECT AVG(cpu_usage), AVG(memory_usage), AVG(temperature) FROM network_metrics WHERE tenant_id=? AND timestamp >= ?`
	var cpuAvg, memAvg, tempAvg sql.NullFloat64
	err := s.db.QueryRowContext(context.Background(), query, tenantID, since).Scan(&cpuAvg, &memAvg, &tempAvg)
	if err != nil {
		log.Printf("[store] QueryNetworkMetrics 查询失败: %v", err)
		return map[string]float64{"cpu_usage": 0, "memory_usage": 0, "temperature": 0}
	}
	return map[string]float64{
		"cpu_usage":    nullFloat(cpuAvg),
		"memory_usage": nullFloat(memAvg),
		"temperature":  nullFloat(tempAvg),
	}
}

// nullFloat 从 sql.NullFloat64 提取值，NULL 返回 0。
func nullFloat(n sql.NullFloat64) float64 {
	if n.Valid {
		return n.Float64
	}
	return 0
}
