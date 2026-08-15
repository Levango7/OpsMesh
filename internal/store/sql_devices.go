// sql_devices.go - SQLStore DeviceStore methods (device/agent CRUD).
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"strconv"
	"strings"
	"time"

	"opsmesh/internal/events"
	"opsmesh/internal/proto"
)

func (s *SQLStore) Register(a *proto.AgentInfo) *proto.AgentInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if a.AgentID == "" {
		// MVP 简易唯一 ID（避免依赖额外计数器表）。
		a.AgentID = "agent-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	if a.Status == "" {
		a.Status = "online"
	}
	now := time.Now().UTC()

	// task 81 gRPC agent 身份绑定：为该 agent 生成 HMAC 签名密钥。
	// 仅在该 agent 首次注册（agents 表无此 agent_id）时生成；复用已有 agent 不重置密钥，
	// 避免 agent 重启注册后旧密钥失效。secret 列可能不存在（老库未迁移），故容错处理。
	agentSecret := ""
	var existingSecret string
	hasSecretCol := true
	if qerr := s.db.QueryRowContext(ctx, `SELECT secret FROM agents WHERE agent_id=?`, a.AgentID).Scan(&existingSecret); qerr != nil {
		if qerr == sql.ErrNoRows {
			// 新 agent：生成新密钥
			agentSecret = mustRandHex(32)
		} else {
			// secret 列可能不存在（老库未迁移）或查询失败：降级为不生成密钥，签名验证降级放行
			agentSecret = ""
			hasSecretCol = false
		}
	} else {
		// 已存在 agent：复用原密钥（不重置，避免 agent 重启后旧密钥失效）
		agentSecret = existingSecret
	}

	var err error
	if hasSecretCol {
		_, err = s.db.ExecContext(ctx,
			"INSERT INTO agents (agent_id, hostname, segment, tenant_id, addr, grpc_port, metrics_port, status, `load`, last_seen, secret) " +
			"VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) " +
			"ON DUPLICATE KEY UPDATE hostname=VALUES(hostname), segment=VALUES(segment), tenant_id=VALUES(tenant_id), " +
			"addr=VALUES(addr), grpc_port=VALUES(grpc_port), metrics_port=VALUES(metrics_port), " +
			"status=VALUES(status), `load`=VALUES(`load`), last_seen=VALUES(last_seen)", a.AgentID, a.Hostname, a.Segment, a.TenantID, a.Addr, a.GRPCPort, a.MetricsPort, a.Status, 1, now, agentSecret)
		if err != nil {
			log.Printf("[store] Register upsert agents 失败 %s: %v", a.AgentID, err)
		}
	} else {
		_, err = s.db.ExecContext(ctx,
			"INSERT INTO agents (agent_id, hostname, segment, tenant_id, addr, grpc_port, metrics_port, status, `load`, last_seen) " +
			"VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) " +
			"ON DUPLICATE KEY UPDATE hostname=VALUES(hostname), segment=VALUES(segment), tenant_id=VALUES(tenant_id), " +
			"addr=VALUES(addr), grpc_port=VALUES(grpc_port), metrics_port=VALUES(metrics_port), " +
			"status=VALUES(status), `load`=VALUES(`load`), last_seen=VALUES(last_seen)",
			a.AgentID, a.Hostname, a.Segment, a.TenantID, a.Addr, a.GRPCPort, a.MetricsPort, a.Status, 1, now)
		if err != nil {
			log.Printf("[store] Register upsert agents 失败 %s: %v", a.AgentID, err)
		}
	}
	// 缓存 agent secret 供 AgentSecret O(1) 查询
	if agentSecret != "" {
		s.mu.Lock()
		s.agentSecretCache[a.AgentID] = agentSecret
		s.mu.Unlock()
	}

	// B1 自动纳管闭环：若携带 OnboardDeviceID（由 gRPC Register 校验 install token 后回填），
	// 翻转该「已发现候选设备」为已纳管（Managed=1, State=online, 绑定 agentID）；
	// 否则按 agent 即设备语义新建占位设备 dev-<agentID>。
	// 安全（P0-F1 纵深防御）：翻转前校验候选设备租户与 agent 租户一致，防越权翻转。
	if a.OnboardDeviceID != "" {
		// 先查候选设备当前租户，校验一致性后再翻转（agent 租户空=单租户放行）。
		var curTenant string
		if qerr := s.db.QueryRowContext(ctx, `SELECT tenant_id FROM devices WHERE device_id=?`, a.OnboardDeviceID).Scan(&curTenant); qerr != nil && qerr != sql.ErrNoRows {
			log.Printf("[store] Register onboard 查询候选设备 %s 租户失败: %v", a.OnboardDeviceID, qerr)
		}
		if a.TenantID != "" && curTenant != "" && curTenant != a.TenantID {
			log.Printf("[store] Register onboard 拒绝跨租户翻转 %s（device tenant=%q, agent tenant=%q）", a.OnboardDeviceID, curTenant, a.TenantID)
		} else {
			_, err = s.db.ExecContext(ctx, `
INSERT INTO devices (device_id, segment, tenant_id, ip, agent_id, state, task_state, managed, hostname, os, arch)
VALUES (?, ?, ?, ?, ?, 'online', 'idle', 1, ?, ?, ?)
ON DUPLICATE KEY UPDATE segment=VALUES(segment), tenant_id=VALUES(tenant_id), ip=VALUES(ip), agent_id=VALUES(agent_id), state='online', task_state='idle', managed=1, hostname=VALUES(hostname), os=VALUES(os), arch=VALUES(arch)
`, a.OnboardDeviceID, a.Segment, a.TenantID, a.Addr, a.AgentID, a.Hostname, a.OS, a.Arch)
			if err != nil {
				log.Printf("[store] Register onboard 设备失败 %s: %v", a.OnboardDeviceID, err)
			}
		}
	} else {
		_, err = s.db.ExecContext(ctx, `
INSERT INTO devices (device_id, segment, tenant_id, ip, agent_id, state, task_state, managed, hostname, os, arch)
VALUES (?, ?, ?, ?, ?, 'online', 'idle', 1, ?, ?, ?)
ON DUPLICATE KEY UPDATE
	segment=VALUES(segment), tenant_id=VALUES(tenant_id), ip=VALUES(ip), state=VALUES(state), task_state=VALUES(task_state), managed=1, hostname=VALUES(hostname), os=VALUES(os), arch=VALUES(arch)
`, "dev-"+a.AgentID, a.Segment, a.TenantID, a.Addr, a.AgentID, "online", "idle", a.Hostname, a.OS, a.Arch)
	}
	if err != nil {
		log.Printf("[store] Register insert devices 失败 %s: %v", a.AgentID, err)
	}

	// 演示模式（P0-5）：仅 --demo 开启时预置 uname -a 示例任务，避免污染生产。
	if s.demo {
		_, err = s.db.ExecContext(ctx, `
INSERT INTO tasks (task_id, agent_id, tenant_id, type, command, status, created_at)
VALUES (?, ?, ?, ?, ?, 'pending', ?)
ON DUPLICATE KEY UPDATE type=VALUES(type), command=VALUES(command), status=VALUES(status)
`, "task-"+a.AgentID+"-1", a.AgentID, a.TenantID, "shell", "uname -a", now)
		if err != nil {
			log.Printf("[store] Register insert tasks 失败 %s: %v", a.AgentID, err)
		}
	}

	s.cacheAgent(a)
	s.publish(events.Event{Action: "register", Target: a.AgentID, TenantID: a.TenantID, Level: events.LevelInfo})
	// 审计留痕（U-04 等保三级：注册 100% 入审计轨迹；存储层统一产出，避免与 handler 重复）。
	s.Audit(&proto.AuditEvent{
		TenantID: a.TenantID,
		Action:   "register",
		Target:   a.AgentID,
	})
	return a
}

// Heartbeat 更新 agents 状态/负载；并同步写 Redis 缓存（MVP 仅写）。

func (s *SQLStore) Heartbeat(agentID, status string, load int) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res, err := s.db.ExecContext(ctx,
		"UPDATE agents SET status=?, `load`=?, last_seen=? WHERE agent_id=?",
		status, load, time.Now().UTC(), agentID)
	if err != nil {
		log.Printf("[store] Heartbeat 更新失败 %s: %v", agentID, err)
		return false
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return false
	}

	if s.rdb != nil {
		c2, c2cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer c2cancel()
		b, _ := json.Marshal(map[string]interface{}{
			"agentID":  agentID,
			"status":   status,
			"load":     load,
			"lastSeen": time.Now().UTC(),
		})
		if err := s.rdb.HSet(c2, "opsmesh:agents", agentID, string(b)).Err(); err != nil {
			log.Printf("[store] redis 缓存 heartbeat 失败 %s: %v", agentID, err)
		}
	}
	return true
}

// GetTasks 返回指定 agent 的待执行任务（SELECT tasks WHERE agent_id）。

func (s *SQLStore) Snapshot(tenantID string) map[string][]proto.DeviceInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	q := `SELECT device_id, segment, tenant_id, ip, agent_id, state, task_state, managed, last_result, last_result_at, retired, hostname, os, arch FROM devices`
	var args []interface{}
	where := []string{`(retired IS NULL OR retired=0)`} // F5 退役设备不出现在活跃清单
	if tenantID != "" {
		where = append(where, `tenant_id=?`)
		args = append(args, tenantID)
	}
	q += ` WHERE ` + strings.Join(where, " AND ")
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] Snapshot 查询失败: %v", err)
		return nil
	}
	defer rows.Close()

	out := make(map[string][]proto.DeviceInfo)
	for rows.Next() {
		var d proto.DeviceInfo
		var managed, retired bool
		var lastResult sql.NullString
		var lastResultAt sql.NullTime
		var hostname, osName, arch sql.NullString
		if err := rows.Scan(&d.DeviceID, &d.Segment, &d.TenantID, &d.IP, &d.AgentID,
			&d.State, &d.TaskState, &managed, &lastResult, &lastResultAt, &retired, &hostname, &osName, &arch); err != nil {
			log.Printf("[store] Snapshot 扫描失败: %v", err)
			continue
		}
		d.Managed = managed
		d.Retired = retired
		if lastResult.Valid {
			d.LastResult = lastResult.String
		}
		if lastResultAt.Valid {
			d.LastResultAt = lastResultAt.Time
		}
		if hostname.Valid {
			d.Hostname = hostname.String
		}
		if osName.Valid {
			d.OS = osName.String
		}
		if arch.Valid {
			d.Arch = arch.String
		}
		out[d.Segment] = append(out[d.Segment], d)
	}
	return out
}

// AllTasks 返回全部任务（tenantID 非空时按租户过滤；供任务列表端点）。

func (s *SQLStore) Device(id string) *proto.DeviceInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := s.db.QueryRowContext(ctx,
		`SELECT device_id, segment, tenant_id, ip, agent_id, state, task_state, managed, last_result, last_result_at, retired, hostname, os, arch FROM devices WHERE device_id=?`, id)
	var d proto.DeviceInfo
	var managed, retired bool
	var lastResult sql.NullString
	var lastResultAt sql.NullTime
	var hostname, osName, arch sql.NullString
	if err := row.Scan(&d.DeviceID, &d.Segment, &d.TenantID, &d.IP, &d.AgentID,
		&d.State, &d.TaskState, &managed, &lastResult, &lastResultAt, &retired, &hostname, &osName, &arch); err != nil {
		if err != sql.ErrNoRows {
			log.Printf("[store] Device 查询失败 %s: %v", id, err)
		}
		return nil
	}
	d.Managed = managed
	d.Retired = retired
	if lastResult.Valid {
		d.LastResult = lastResult.String
	}
	if lastResultAt.Valid {
		d.LastResultAt = lastResultAt.Time
	}
	if hostname.Valid {
		d.Hostname = hostname.String
	}
	if osName.Valid {
		d.OS = osName.String
	}
	if arch.Valid {
		d.Arch = arch.String
	}
	return &d
}

// Results 返回某 agent 的上报结果（供设备详情端点）。

func (s *SQLStore) Agents(tenantID string) []*proto.AgentInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	q := "SELECT agent_id, hostname, segment, tenant_id, addr, grpc_port, metrics_port, status, `load`, last_seen FROM agents"
	var args []interface{}
	if tenantID != "" {
		q += ` WHERE tenant_id=?`
		args = append(args, tenantID)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] Agents 查询失败: %v", err)
		return nil
	}
	defer rows.Close()

	var out []*proto.AgentInfo
	for rows.Next() {
		var a proto.AgentInfo
		var lastSeen time.Time
		if err := rows.Scan(&a.AgentID, &a.Hostname, &a.Segment, &a.TenantID, &a.Addr,
			&a.GRPCPort, &a.MetricsPort, &a.Status, &a.Load, &lastSeen); err != nil {
			log.Printf("[store] Agents 扫描失败: %v", err)
			continue
		}
		a.LastSeen = lastSeen
		out = append(out, &a)
	}
	return out
}

// Agent 按 agentID 直接返回单台 agent（P2-17）。

func (s *SQLStore) Agent(id string) *proto.AgentInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := s.db.QueryRowContext(ctx,
		"SELECT agent_id, hostname, segment, tenant_id, addr, grpc_port, metrics_port, status, `load`, last_seen FROM agents WHERE agent_id=?", id)
	var a proto.AgentInfo
	var lastSeen time.Time
	if err := row.Scan(&a.AgentID, &a.Hostname, &a.Segment, &a.TenantID, &a.Addr,
		&a.GRPCPort, &a.MetricsPort, &a.Status, &a.Load, &lastSeen); err != nil {
		if err != sql.ErrNoRows {
			log.Printf("[store] Agent 查询失败 %s: %v", id, err)
		}
		return nil
	}
	a.LastSeen = lastSeen
	return &a
}

// AgentSecret 返回该 agent 的 HMAC 签名密钥（task 81 gRPC 身份绑定）。
// 优先查内存缓存（Register 时已填充），未命中则查 MySQL agents.secret 列。
// agent 不存在或密钥为空时返回空串（调用方据此判断是否需要签名验证）。

func (s *SQLStore) AgentSecret(agentID string) string {
	s.mu.Lock()
	if sec, ok := s.agentSecretCache[agentID]; ok {
		s.mu.Unlock()
		return sec
	}
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var sec string
	if err := s.db.QueryRowContext(ctx, `SELECT secret FROM agents WHERE agent_id=?`, agentID).Scan(&sec); err != nil {
		return ""
	}
	s.mu.Lock()
	s.agentSecretCache[agentID] = sec
	s.mu.Unlock()
	return sec
}

// CreateTask 下发任务给指定 agent（agentID 必填；TaskID 为空时分配；status=pending）。

func (s *SQLStore) UpsertDevice(d *proto.DeviceInfo) {
	if d == nil || d.DeviceID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO devices (device_id, segment, tenant_id, ip, agent_id, state, task_state, managed, last_result, last_result_at, retired)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE segment=VALUES(segment), tenant_id=VALUES(tenant_id), ip=VALUES(ip),
	agent_id=VALUES(agent_id), state=VALUES(state), task_state=VALUES(task_state),
	managed=VALUES(managed), last_result=VALUES(last_result), last_result_at=VALUES(last_result_at), retired=VALUES(retired)
`, d.DeviceID, d.Segment, d.TenantID, d.IP, d.AgentID, d.State, d.TaskState,
		boolToInt(d.Managed), nullString(d.LastResult), nullTime(d.LastResultAt), boolToInt(d.Retired)); err != nil {
		log.Printf("[store] UpsertDevice 失败 %s: %v", d.DeviceID, err)
	}
}

// PendingDepth 返回当前 pending 任务总数（观测队列深度 P2-1）。

func (s *SQLStore) RetireDevice(id, tenantID string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := s.db.ExecContext(ctx,
		`UPDATE devices SET retired=1, state='offline' WHERE device_id=? AND (tenant_id=? OR ?='')`,
		id, tenantID, tenantID)
	if err != nil {
		log.Printf("[store] RetireDevice 失败 %s: %v", id, err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// Provision B1 自动纳管闭环：为「已发现候选设备」签发一次性、限时的 install token
// （HMAC 签名，密钥来自 store 构造时注入的 ProvisionSecret），标记设备 provisioning，
// 并返回 token 与 bootstrap 提示。deviceID 不存在或租户不符时返回错误。

func (s *SQLStore) RetireStaleDevices(maxAge time.Duration) int {
	if maxAge <= 0 {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// A-2 leader fencing：非有效租约持有者跳过，防双主同时归档。
	if !s.checkLeaderFence(ctx, "RetireStaleDevices") {
		return 0
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE devices d LEFT JOIN agents a ON d.agent_id=a.agent_id
		SET d.retired=1, d.state='offline'
		WHERE (d.retired IS NULL OR d.retired=0)
		  AND (a.last_seen IS NULL OR a.last_seen < ?)`,
		time.Now().UTC().Add(-maxAge))
	if err != nil {
		log.Printf("[store] RetireStaleDevices 失败: %v", err)
		return 0
	}
	n, _ := res.RowsAffected()
	return int(n)
}

// StoreDeviceMetrics 缓存设备监控指标（agent 心跳上报，追加到环形缓冲保留最近 N 条历史，task 223）。
// 高频时序数据落库应由 Prometheus 承担，控制面仅缓存最近 2h 历史供 GET /api/v1/devices/{id}/metrics?range=2h 查询。

func (s *SQLStore) StoreDeviceMetrics(deviceID string, metrics *proto.DeviceMetrics) {
	if deviceID == "" || metrics == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.deviceMetrics[deviceID]
	if !ok || r == nil {
		r = newMetricsRing(metricsRingDefaultCap)
		s.deviceMetrics[deviceID] = r
	}
	r.add(metrics)
}

// DeviceMetrics 返回设备最新监控指标缓存（无数据时返回 nil）。

func (s *SQLStore) DeviceMetrics(deviceID string) *proto.DeviceMetrics {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.deviceMetrics[deviceID]
	if !ok || r == nil {
		return nil
	}
	return r.latest()
}

// DeviceMetricsHistory 返回设备监控指标历史时序（环形缓冲查询，task 223）。
// since 为零值时返回全部已存储历史；否则返回 CollectedAt >= since 的快照（按时间升序）。
// 无数据时返回 nil。

func (s *SQLStore) DeviceMetricsHistory(deviceID string, since time.Time) []proto.DeviceMetrics {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.deviceMetrics[deviceID]
	if !ok || r == nil {
		return nil
	}
	return r.since(since)
}

func (s *SQLStore) cacheAgent(a *proto.AgentInfo) {
	if s.rdb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	b, err := json.Marshal(a)
	if err != nil {
		return
	}
	if err := s.rdb.HSet(ctx, "opsmesh:agents", a.AgentID, string(b)).Err(); err != nil {
		log.Printf("[store] redis 缓存 agent 失败 %s: %v", a.AgentID, err)
	}
}

// RenewLeadership A3 选主：原子抢占或续租 leader_lease 单行（id=1）。
// 若当前无主或租约已过期（expires_at < now），本实例抢占为 holder 并写入新的过期时间；
// 若已是本实例持有且未过期，则仅续租（更新 expires_at）；否则保持现状（不抢占）。
// 通过 ON DUPLICATE KEY UPDATE + IF(expires_at < now, 抢, 留) 在单条 SQL 内完成原子判定，
// 避免多副本并发下的 read-modify-write 竞态。返回本实例当前是否持有租约。
