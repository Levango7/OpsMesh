package store

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"opsmesh/internal/dag"

	// 匿名导入以注册 MySQL 驱动（database/sql 的 "mysql" 驱动名）。
	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"

	"opsmesh/internal/cron"
	"opsmesh/internal/events"
	"opsmesh/internal/proto"
)

// SQLStore 基于 MySQL + Redis 的持久化实现（U-04 数据本地化，私有部署）。
//   - MySQL 为权威存储（四张表：agents / devices / tasks / task_results）。
//   - Redis 作 agent/device 状态缓存（MVP 仅写缓存，读取仍走 MySQL；
//     生产可改为读 Redis 以减 MySQL 压力，见各方法注释）。
//
// 即便运行期连不上库，也不会让 go build 失败：连接错误只在运行期日志提示并返回零值，
// 不会 panic。
//
// A3 真 HA：多副本控制面共享同一 MySQL；通过 leader_lease 表做分布式选主，
// 仅 leader 执行周期性协调任务（reclaim / schedule / provision / 离线归档），
// 避免重复派生/回收。每个进程实例持唯一 instanceID 参与抢占。
type SQLStore struct {
	db  *sql.DB
	rdb *redis.Client
	bus events.Bus // 事件总线（P1-5）；可 nil（测试/默认 noop）
	demo bool       // 演示模式（P0-5）：开启时注册预置 uname -a

	instanceID string    // 本进程实例唯一标识（A3 选主参与方）
	mu         sync.Mutex // 保护 isLeader / leaseUntil 的读写
	isLeader   bool       // 本实例当前是否自认为 leader
	leaseUntil time.Time // 当前租约过期时间（UTC）
	secret     string      // B1 install token 的 HMAC 签名密钥（WithSecret 注入；空则构造时随机）
}

// DB 暴露底层 *sql.DB 给 CMDB 等需要同库连接的子系统复用连接池。
func (s *SQLStore) DB() *sql.DB { return s.db }

// WithBus 注入事件总线（store 构造后由控制面注入，避免改动所有构造调用点）。
// 线程安全：必须在 Start/首次并发访问前调用，非并发安全。
func (s *SQLStore) WithBus(b events.Bus) *SQLStore {
	s.bus = b
	return s
}

// WithDemo 设置演示模式（P0-5）：开启时 Register 预置 uname -a 示例任务。
// 线程安全：必须在 Start/首次并发访问前调用，非并发安全。
func (s *SQLStore) WithDemo(b bool) Store {
	s.demo = b
	return s
}

// WithSecret 注入 B1 install token 的 HMAC 签名密钥（空则保留构造时随机密钥）。
// 多副本控制面共享同一 MySQL 时须注入一致密钥，否则互不相认。
// 线程安全：必须在 Start/首次并发访问前调用，非并发安全。
func (s *SQLStore) WithSecret(secret string) *SQLStore {
	if secret != "" {
		s.secret = secret
	}
	return s
}

// publish 在总线非空时发布领域事件（审计/告警可接 Kafka）。
func (s *SQLStore) publish(e events.Event) {
	if s.bus != nil {
		_ = s.bus.Publish(context.Background(), e)
	}
}

// NewSQLStore 打开 MySQL 连接并建表（幂等）。redisAddr 为空则跳过 Redis。
func NewSQLStore(dsn, redisAddr string) (*SQLStore, error) {
	// 必须开启 parseTime，否则 DATETIME 列无法 Scan 进 time.Time。
	db, err := sql.Open("mysql", ensureParseTime(dsn))
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	// Ping 失败不阻塞启动（MVP 允许延迟连接），仅日志提示。
	if err := db.Ping(); err != nil {
		log.Printf("[store] mysql ping 失败（将延迟重连）: %v", err)
	}

	var rdb *redis.Client
	if redisAddr != "" {
		rdb = redis.NewClient(&redis.Options{Addr: redisAddr})
	}

	// A3 选主：生成本进程唯一实例 ID（hostname + pid + 纳秒），用于 leader_lease 抢占标识。
	host, herr := os.Hostname()
	if herr != nil || host == "" {
		host = "opsmesh"
	}
	instID := fmt.Sprintf("%s-%d-%d", host, os.Getpid(), time.Now().UnixNano())

	s := &SQLStore{db: db, rdb: rdb, instanceID: instID, secret: mustRandHex(32)}
	if err := s.initSchema(); err != nil {
		log.Printf("[store] 建表失败（运行期可能不可用）: %v", err)
	}
	return s, nil
}

// ensureParseTime 在 DSN 中保证 parseTime=true，便于 time.Time 直接 Scan。
func ensureParseTime(dsn string) string {
	if strings.Contains(dsn, "parseTime=true") {
		return dsn
	}
	if strings.Contains(dsn, "?") {
		return dsn + "&parseTime=true"
	}
	return dsn + "?parseTime=true"
}

// initSchema 幂等建表（CREATE TABLE IF NOT EXISTS）。
func (s *SQLStore) initSchema() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS agents (
			agent_id VARCHAR(64) PRIMARY KEY,
			hostname VARCHAR(255),
			segment VARCHAR(64),
			tenant_id VARCHAR(64),
			addr VARCHAR(255),
			grpc_port INT,
			metrics_port INT,
			status VARCHAR(16),
			load INT,
			last_seen DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS devices (
			device_id VARCHAR(64) PRIMARY KEY,
			segment VARCHAR(64),
			tenant_id VARCHAR(64),
			ip VARCHAR(64),
			agent_id VARCHAR(64),
			state VARCHAR(16),
			task_state VARCHAR(16),
			managed BOOLEAN DEFAULT 0,
			last_result VARCHAR(16),
			last_result_at DATETIME,
			retired BOOLEAN DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS tasks (
			task_id VARCHAR(64) PRIMARY KEY,
			agent_id VARCHAR(64),
			tenant_id VARCHAR(64),
			type VARCHAR(32),
			command TEXT,
			status VARCHAR(16),
			claimed_by VARCHAR(64),
			claimed_at DATETIME,
			created_at DATETIME,
			retry_count INT DEFAULT 0,
			max_retries INT DEFAULT 0,
			dead_letter BOOLEAN DEFAULT 0,
			schedule VARCHAR(64),
			parent_id VARCHAR(64),
			last_fired_at DATETIME,
			depends_on TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS task_results (
			task_id VARCHAR(64) PRIMARY KEY,
			agent_id VARCHAR(64),
			exit_code INT,
			stdout MEDIUMTEXT,
			stderr MEDIUMTEXT,
			finished_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INT PRIMARY KEY,
			applied_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS audit_log (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			tenant_id VARCHAR(64),
			user_id VARCHAR(64),
			action VARCHAR(64),
			target VARCHAR(128),
			detail TEXT,
			created_at DATETIME
		)`,
	// A3 选主租约表：单行（id=1）记录当前 leader 实例与租约过期时间。
	// 多副本控制面通过原子 INSERT ... ON DUPLICATE KEY UPDATE 抢占/续租。
	`CREATE TABLE IF NOT EXISTS leader_lease (
			id INT PRIMARY KEY DEFAULT 1,
			holder VARCHAR(128),
			expires_at DATETIME,
			updated_at DATETIME
		)`,
		// B1 自动纳管：一次性、限时 install token 登记表（HMAC 签名，密钥来自 ProvisionSecret）。
		// 多副本控制面共享同一 MySQL，故 token 落库以实现跨副本消费一致性。
		`CREATE TABLE IF NOT EXISTS install_tokens (
			token VARCHAR(512) PRIMARY KEY,
			device_id VARCHAR(64),
			tenant_id VARCHAR(64),
			expires_at DATETIME,
			consumed BOOLEAN DEFAULT 0
		)`,
	}
	for _, q := range stmts {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("exec %q: %w", q, err)
		}
	}
	// 记录当前 schema 版本（P2-11：为后续演进留迁移锚点）。
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (1, ?)
		 ON DUPLICATE KEY UPDATE applied_at=VALUES(applied_at)`, time.Now().UTC()); err != nil {
		log.Printf("[store] 写入 schema_migrations 失败（非致命）: %v", err)
	}
	// 增量迁移：为已存在但缺 tenant_id 的 tasks 表补列（MySQL 不支持 ADD COLUMN IF NOT EXISTS，
	// 故检查 information_schema 后按需 ALTER，重复列名错误忽略）。
	var cnt int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='tasks' AND column_name='tenant_id'`,
	).Scan(&cnt); err == nil && cnt == 0 {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE tasks ADD COLUMN tenant_id VARCHAR(64)`); err != nil {
			log.Printf("[store] 迁移 tasks.tenant_id 失败（非致命）: %v", err)
		}
	}
	// 后续列迁移（F2/F3/F4/F5/B1/B2 新增字段）统一走 alterColumnIfMissing，避免破坏已存在库。
	s.alterColumnIfMissing(ctx, "devices", "managed", "BOOLEAN DEFAULT 0")
	s.alterColumnIfMissing(ctx, "devices", "last_result", "VARCHAR(16)")
	s.alterColumnIfMissing(ctx, "devices", "last_result_at", "DATETIME")
	s.alterColumnIfMissing(ctx, "devices", "retired", "BOOLEAN DEFAULT 0")
	s.alterColumnIfMissing(ctx, "tasks", "retry_count", "INT DEFAULT 0")
	s.alterColumnIfMissing(ctx, "tasks", "max_retries", "INT DEFAULT 0")
	s.alterColumnIfMissing(ctx, "tasks", "dead_letter", "BOOLEAN DEFAULT 0")
	s.alterColumnIfMissing(ctx, "tasks", "schedule", "VARCHAR(64)")
	s.alterColumnIfMissing(ctx, "tasks", "parent_id", "VARCHAR(64)")
	s.alterColumnIfMissing(ctx, "tasks", "last_fired_at", "DATETIME")
	s.alterColumnIfMissing(ctx, "tasks", "depends_on", "TEXT")
	// A3 leader_lease 表：仅当表已存在时补列（全新库由 CREATE TABLE 保证结构）。
	var llCnt int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='leader_lease'`,
	).Scan(&llCnt); err == nil && llCnt > 0 {
		s.alterColumnIfMissing(ctx, "leader_lease", "holder", "VARCHAR(128)")
		s.alterColumnIfMissing(ctx, "leader_lease", "expires_at", "DATETIME")
		s.alterColumnIfMissing(ctx, "leader_lease", "updated_at", "DATETIME")
	}
	// 告警表（M7）。
	if _, err := s.db.ExecContext(ctx, `
	CREATE TABLE IF NOT EXISTS alerts (
		id BIGINT AUTO_INCREMENT PRIMARY KEY,
		tenant_id VARCHAR(64),
		device_id VARCHAR(64),
		agent_id VARCHAR(64),
		severity VARCHAR(16),
		message TEXT,
		created_at DATETIME,
		alert_id VARCHAR(64),
		status VARCHAR(16),
		acknowledged_by VARCHAR(64),
		silenced_until DATETIME,
		comment TEXT,
		updated_at DATETIME
	)`); err != nil {
		log.Printf("[store] 建 alerts 表失败（非致命）: %v", err)
	}
	// 告警状态扩展（M7 ack/silence）：向后兼容补列（老表缺列不报错）。
	s.alterColumnIfMissing(ctx, "alerts", "alert_id", "VARCHAR(64)")
	s.alterColumnIfMissing(ctx, "alerts", "status", "VARCHAR(16)")
	s.alterColumnIfMissing(ctx, "alerts", "acknowledged_by", "VARCHAR(64)")
	s.alterColumnIfMissing(ctx, "alerts", "silenced_until", "DATETIME")
	s.alterColumnIfMissing(ctx, "alerts", "comment", "TEXT")
	s.alterColumnIfMissing(ctx, "alerts", "updated_at", "DATETIME")
	// CMDB 表（Phase 1）：CI 类型字典、CI 实例、CI 关系。
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS ci_types (
			id INT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(64) NOT NULL UNIQUE,
			display_name VARCHAR(64),
			builtin BOOLEAN DEFAULT 1,
			created_at DATETIME
		)`); err != nil {
		log.Printf("[store] 建 ci_types 表失败（非致命）: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `
	CREATE TABLE IF NOT EXISTS ci_items (
		id VARCHAR(64) PRIMARY KEY,
		ci_type VARCHAR(64) NOT NULL,
		tenant_id VARCHAR(64) NOT NULL,
		name VARCHAR(255) NOT NULL,
		status VARCHAR(32) DEFAULT 'active',
		approval_status VARCHAR(16) DEFAULT 'approved',
		attrs JSON,
		source VARCHAR(32) DEFAULT 'manual',
		agent_id VARCHAR(64),
		device_id VARCHAR(64),
		version INT DEFAULT 1,
		created_at DATETIME,
		updated_at DATETIME
	)`); err != nil {
		log.Printf("[store] 建 ci_items 表失败（非致命）: %v", err)
	}
	// Phase-3 轻量审批流：ci_items 增补审批状态列（向后兼容，默认 approved）。
	s.alterColumnIfMissing(ctx, "ci_items", "approval_status", "VARCHAR(16) DEFAULT 'approved'")
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS ci_relations (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			source_ci_id VARCHAR(64) NOT NULL,
			target_ci_id VARCHAR(64) NOT NULL,
			relation_type VARCHAR(32) NOT NULL,
			tenant_id VARCHAR(64),
			attributes JSON,
			created_at DATETIME,
			UNIQUE KEY uq_rel (source_ci_id, target_ci_id, relation_type)
		)`); err != nil {
		log.Printf("[store] 建 ci_relations 表失败（非致命）: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS ci_attr_templates (
			id INT AUTO_INCREMENT PRIMARY KEY,
			ci_type VARCHAR(64) NOT NULL,
			attr_key VARCHAR(64) NOT NULL,
			label VARCHAR(128) NOT NULL,
			attr_type VARCHAR(32) DEFAULT 'string',
			required BOOLEAN DEFAULT 0,
			default_value TEXT,
			tenant_id VARCHAR(64),
			created_at DATETIME,
			UNIQUE KEY uq_tmpl (ci_type, attr_key)
		)`); err != nil {
		log.Printf("[store] 建 ci_attr_templates 表失败（非致命）: %v", err)
	}
	return nil
}

// alterColumnIfMissing 当列不存在时 ALTER TABLE 补列（MySQL 无 ADD COLUMN IF NOT EXISTS）。
func (s *SQLStore) alterColumnIfMissing(ctx context.Context, table, column, def string) {
	var cnt int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name=? AND column_name=?`,
		table, column).Scan(&cnt); err != nil || cnt > 0 {
		return
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column+` `+def); err != nil {
		log.Printf("[store] 迁移 %s.%s 失败（非致命）: %v", table, column, err)
	}
}

// boolToInt 把 Go bool 转为 MySQL TINYINT（0/1）。
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// nullString 空串转 NULL（避免空串写入可空文本列）。
func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// nullTime 零值 time.Time 转 NULL。
func nullTime(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t
}

// Register 注册 agent：upsert agents + 插 devices + 预置示例 task，并写 Redis 缓存。
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

	_, err := s.db.ExecContext(ctx, `
INSERT INTO agents (agent_id, hostname, segment, tenant_id, addr, grpc_port, metrics_port, status, load, last_seen)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
	hostname=VALUES(hostname), segment=VALUES(segment), tenant_id=VALUES(tenant_id), addr=VALUES(addr),
	grpc_port=VALUES(grpc_port), metrics_port=VALUES(metrics_port),
	status=VALUES(status), load=VALUES(load), last_seen=VALUES(last_seen)
`, a.AgentID, a.Hostname, a.Segment, a.TenantID, a.Addr, a.GRPCPort, a.MetricsPort, a.Status, 1, now)
	if err != nil {
		log.Printf("[store] Register upsert agents 失败 %s: %v", a.AgentID, err)
	}


	// B1 自动纳管闭环：若携带 OnboardDeviceID（由 gRPC Register 校验 install token 后回填），
	// 翻转该「已发现候选设备」为已纳管（Managed=1, State=online, 绑定 agentID）；
	// 否则按 agent 即设备语义新建占位设备 dev-<agentID>。
	// 安全（P0-F1 纵深防御）：翻转前校验候选设备租户与 agent 租户一致，防越权翻转。
	if a.OnboardDeviceID != "" {
		// 先查候选设备当前租户，校验一致性后再翻转（agent 租户空=单租户放行）。
		var curTenant string
		_ = s.db.QueryRowContext(ctx, `SELECT tenant_id FROM devices WHERE device_id=?`, a.OnboardDeviceID).Scan(&curTenant)
		if a.TenantID != "" && curTenant != "" && curTenant != a.TenantID {
			log.Printf("[store] Register onboard 拒绝跨租户翻转 %s（device tenant=%q, agent tenant=%q）", a.OnboardDeviceID, curTenant, a.TenantID)
		} else {
			_, err = s.db.ExecContext(ctx, `
INSERT INTO devices (device_id, segment, tenant_id, ip, agent_id, state, task_state, managed)
VALUES (?, ?, ?, ?, ?, 'online', 'idle', 1)
ON DUPLICATE KEY UPDATE segment=VALUES(segment), tenant_id=VALUES(tenant_id), ip=VALUES(ip), agent_id=VALUES(agent_id), state='online', task_state='idle', managed=1
`, a.OnboardDeviceID, a.Segment, a.TenantID, a.Addr, a.AgentID)
			if err != nil {
				log.Printf("[store] Register onboard 设备失败 %s: %v", a.OnboardDeviceID, err)
			}
		}
	} else {
		_, err = s.db.ExecContext(ctx, `
INSERT INTO devices (device_id, segment, tenant_id, ip, agent_id, state, task_state, managed)
VALUES (?, ?, ?, ?, ?, 'online', 'idle', 1)
ON DUPLICATE KEY UPDATE
	segment=VALUES(segment), tenant_id=VALUES(tenant_id), ip=VALUES(ip), state=VALUES(state), task_state=VALUES(task_state), managed=1
`, "dev-"+a.AgentID, a.Segment, a.TenantID, a.Addr, a.AgentID, "online", "idle")
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
		`UPDATE agents SET status=?, load=?, last_seen=? WHERE agent_id=?`,
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
			"agentID": agentID,
			"status":  status,
			"load":    load,
			"lastSeen": time.Now().UTC(),
		})
		if err := s.rdb.HSet(c2, "opsmesh:agents", agentID, string(b)).Err(); err != nil {
			log.Printf("[store] redis 缓存 heartbeat 失败 %s: %v", agentID, err)
		}
	}
	return true
}

// GetTasks 返回指定 agent 的待执行任务（SELECT tasks WHERE agent_id）。
func (s *SQLStore) GetTasks(agentID string) []*proto.Task {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rows, err := s.db.QueryContext(ctx,
		`SELECT task_id, agent_id, type, command, status, created_at FROM tasks WHERE agent_id=? AND (status IS NULL OR status='pending')`, agentID)
	if err != nil {
		log.Printf("[store] GetTasks 查询失败 %s: %v", agentID, err)
		return nil
	}
	defer rows.Close()

	var out []*proto.Task
	for rows.Next() {
		var t proto.Task
		var createdAt time.Time
		if err := rows.Scan(&t.TaskID, &t.AgentID, &t.Type, &t.Command, &t.Status, &createdAt); err != nil {
			log.Printf("[store] GetTasks 扫描失败: %v", err)
			continue
		}
		t.CreatedAt = createdAt
		out = append(out, &t)
	}
	return out
}

// TasksByParent 返回指定 parent_id 的全部任务（跨状态），用于 M5 工作流运行归组 / F4 模板血缘。
func (s *SQLStore) TasksByParent(parentID string) []*proto.Task {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx,
		`SELECT task_id, agent_id, tenant_id, type, command, status, parent_id FROM tasks WHERE parent_id=?`, parentID)
	if err != nil {
		log.Printf("[store] TasksByParent 查询失败 %s: %v", parentID, err)
		return nil
	}
	defer rows.Close()
	var out []*proto.Task
	for rows.Next() {
		var t proto.Task
		if err := rows.Scan(&t.TaskID, &t.AgentID, &t.TenantID, &t.Type, &t.Command, &t.Status, &t.ParentID); err != nil {
			log.Printf("[store] TasksByParent 扫描失败: %v", err)
			continue
		}
		out = append(out, &t)
	}
	return out
}

// SubmitResult 写入 task_results，按成功/失败处理任务终态（F2 重试/死信）并回写设备看板（B2）。
func (s *SQLStore) SubmitResult(res *proto.TaskResult) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := s.db.ExecContext(ctx, `
INSERT INTO task_results (task_id, agent_id, exit_code, stdout, stderr, finished_at)
VALUES (?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
	exit_code=VALUES(exit_code), stdout=VALUES(stdout), stderr=VALUES(stderr), finished_at=VALUES(finished_at)
`, res.TaskID, res.AgentID, res.ExitCode, res.Stdout, res.Stderr, res.FinishedAt.UTC()); err != nil {
		log.Printf("[store] SubmitResult 写入 task_results 失败 %s: %v", res.TaskID, err)
	}

	success := res.ExitCode == 0
	// 读取任务当前重试计数 / 上限，决定终态。
	var tid, tenantID string
	var rc, mr int
	if err := s.db.QueryRowContext(ctx,
		`SELECT task_id, tenant_id, retry_count, max_retries FROM tasks WHERE task_id=?`, res.TaskID,
	).Scan(&tid, &tenantID, &rc, &mr); err == nil && tid != "" {
		if success {
			s.db.ExecContext(ctx, `UPDATE tasks SET status='done' WHERE task_id=?`, res.TaskID)
		} else if rc < mr {
			s.db.ExecContext(ctx,
				`UPDATE tasks SET status='pending', claimed_by=NULL, claimed_at=NULL, retry_count=retry_count+1 WHERE task_id=?`,
				res.TaskID)
			s.publish(events.Event{Action: "task_retry", Target: res.TaskID, TenantID: tenantID,
				Detail: fmt.Sprintf("retry %d/%d", rc+1, mr), Level: events.LevelWarn})
		} else {
			s.db.ExecContext(ctx, `UPDATE tasks SET status='failed', dead_letter=1 WHERE task_id=?`, res.TaskID)
			s.addAlert(ctx, &proto.Alert{
				AlertID:   "alert-" + res.TaskID,
				TenantID:  tenantID,
				DeviceID:  "dev-" + res.AgentID,
				AgentID:   res.AgentID,
				Severity: "critical",
				Message:   fmt.Sprintf("task %s dead-letter after %d retries (exitCode=%d)", res.TaskID, rc, res.ExitCode),
				CreatedAt: time.Now().UTC(),
			})
		}
	}

	// 回写设备 TaskState + LastResult（B2 失败回写看板）。
	lastResult := "success"
	taskState := "done"
	if !success {
		lastResult = "failed"
		taskState = "idle"
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE devices SET task_state=?, last_result=?, last_result_at=? WHERE agent_id=?`,
		taskState, lastResult, time.Now().UTC(), res.AgentID); err != nil {
		log.Printf("[store] SubmitResult 更新 devices 失败 %s: %v", res.AgentID, err)
	}

	lvl := events.LevelInfo
	if !success {
		lvl = events.LevelWarn
	}
	s.publish(events.Event{Action: "report_result", Target: res.TaskID, TenantID: "", Detail: fmt.Sprintf("exitCode=%d", res.ExitCode), Level: lvl})

	// M5 作业编排：本任务 done 后释放依赖它的 blocked 任务（同 agent 任务图内）。
	if success {
		s.releaseDeps(ctx, res.AgentID, res.TaskID)
	}
}

// releaseDeps M5 作业编排：任务 done 后，释放同 agent 下依赖它的 blocked 任务。
// 仅当某 blocked 任务的全部 DependsOn 均已 done 时，将其置为 pending（进入可下发队列）。
func (s *SQLStore) releaseDeps(ctx context.Context, agentID, doneTaskID string) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT task_id, depends_on FROM tasks WHERE agent_id=? AND status='blocked'`, agentID)
	if err != nil {
		return
	}
	type rec struct {
		id  string
		deps string
	}
	var blocked []rec
	for rows.Next() {
		var r rec
		if err := rows.Scan(&r.id, &r.deps); err != nil {
			continue
		}
		blocked = append(blocked, r)
	}
	rows.Close()
	if len(blocked) == 0 {
		return
	}
	// 取该 agent 全部任务状态，构建 byID 索引供 dag 判定。
	all, err := s.db.QueryContext(ctx,
		`SELECT task_id, status FROM tasks WHERE agent_id=?`, agentID)
	if err != nil {
		return
	}
	byID := make(map[string]*proto.Task)
	for all.Next() {
		var id, st string
		if err := all.Scan(&id, &st); err != nil {
			continue
		}
		byID[id] = &proto.Task{TaskID: id, Status: st}
	}
	all.Close()

	for _, b := range blocked {
		var deps []string
		if b.deps != "" {
			_ = json.Unmarshal([]byte(b.deps), &deps)
		}
		t := &proto.Task{TaskID: b.id, DependsOn: deps, Status: "blocked"}
		if dag.AllDepsDone(t, byID) {
			if _, err := s.db.ExecContext(ctx,
				`UPDATE tasks SET status='pending', claimed_by=NULL, claimed_at=NULL WHERE task_id=?`, b.id); err == nil {
				s.publish(events.Event{Action: "task_released", Target: b.id, TenantID: "",
					Detail: "deps done (trigger=" + doneTaskID + ")", Level: events.LevelInfo})
			}
		}
	}
}

// addAlert 写入一条告警（M7）。
func (s *SQLStore) addAlert(ctx context.Context, a *proto.Alert) {
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	if a.Status == "" {
		a.Status = proto.AlertStatusFiring
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO alerts (tenant_id, device_id, agent_id, severity, message, created_at, alert_id, status, acknowledged_by, silenced_until, comment, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullString(a.TenantID), nullString(a.DeviceID), nullString(a.AgentID),
		nullString(a.Severity), nullString(a.Message), nullTime(a.CreatedAt),
		nullString(a.AlertID), nullString(a.Status), nullString(a.AcknowledgedBy),
		nullTime(a.SilencedUntil), nullString(a.Comment), nullTime(a.UpdatedAt)); err != nil {
		log.Printf("[store] addAlert 失败: %v", err)
	}
}

// Snapshot 返回 segment -> 设备列表（SELECT devices GROUP BY segment 在应用层分组）。
func (s *SQLStore) Snapshot(tenantID string) map[string][]proto.DeviceInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	q := `SELECT device_id, segment, tenant_id, ip, agent_id, state, task_state, managed, last_result, last_result_at, retired FROM devices`
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
		if err := rows.Scan(&d.DeviceID, &d.Segment, &d.TenantID, &d.IP, &d.AgentID,
			&d.State, &d.TaskState, &managed, &lastResult, &lastResultAt, &retired); err != nil {
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
		out[d.Segment] = append(out[d.Segment], d)
	}
	return out
}

// AllTasks 返回全部任务（tenantID 非空时按租户过滤；供任务列表端点）。
func (s *SQLStore) AllTasks(tenantID string) []*proto.Task {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT task_id, agent_id, tenant_id, type, command, status, created_at FROM tasks`
	var args []interface{}
	if tenantID != "" {
		q += ` WHERE tenant_id=?`
		args = append(args, tenantID)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] AllTasks 查询失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*proto.Task
	for rows.Next() {
		var t proto.Task
		var createdAt time.Time
		if err := rows.Scan(&t.TaskID, &t.AgentID, &t.TenantID, &t.Type, &t.Command, &t.Status, &createdAt); err != nil {
			log.Printf("[store] AllTasks 扫描失败: %v", err)
			continue
		}
		t.CreatedAt = createdAt
		out = append(out, &t)
	}
	return out
}

// Device 按 deviceID 返回单台设备（供设备详情端点）。
func (s *SQLStore) Device(id string) *proto.DeviceInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := s.db.QueryRowContext(ctx,
		`SELECT device_id, segment, tenant_id, ip, agent_id, state, task_state, managed, last_result, last_result_at, retired FROM devices WHERE device_id=?`, id)
	var d proto.DeviceInfo
	var managed, retired bool
	var lastResult sql.NullString
	var lastResultAt sql.NullTime
	if err := row.Scan(&d.DeviceID, &d.Segment, &d.TenantID, &d.IP, &d.AgentID,
		&d.State, &d.TaskState, &managed, &lastResult, &lastResultAt, &retired); err != nil {
		if err != sql.ErrNoRows {
			log.Printf("[store] Device 查询失败 %s: %v", id, err)
		}
		return nil
	}
	return &d
}

// Results 返回某 agent 的上报结果（供设备详情端点）。
func (s *SQLStore) Results(agentID string) []*proto.TaskResult {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx,
		`SELECT task_id, agent_id, exit_code, stdout, stderr, finished_at FROM task_results WHERE agent_id=? ORDER BY finished_at DESC`, agentID)
	if err != nil {
		log.Printf("[store] Results 查询失败 %s: %v", agentID, err)
		return nil
	}
	defer rows.Close()
	var out []*proto.TaskResult
	for rows.Next() {
		var r proto.TaskResult
		var finishedAt time.Time
		if err := rows.Scan(&r.TaskID, &r.AgentID, &r.ExitCode, &r.Stdout, &r.Stderr, &finishedAt); err != nil {
			log.Printf("[store] Results 扫描失败: %v", err)
			continue
		}
		r.FinishedAt = finishedAt
		out = append(out, &r)
	}
	return out
}

// Agents 返回已注册 agent（tenantID 非空时按租户过滤）。
func (s *SQLStore) Agents(tenantID string) []*proto.AgentInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	q := `SELECT agent_id, hostname, segment, tenant_id, addr, grpc_port, metrics_port, status, load, last_seen FROM agents`
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
		`SELECT agent_id, hostname, segment, tenant_id, addr, grpc_port, metrics_port, status, load, last_seen FROM agents WHERE agent_id=?`, id)
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

// CreateTask 下发任务给指定 agent（agentID 必填；TaskID 为空时分配；status=pending）。
func (s *SQLStore) CreateTask(t *proto.Task) *proto.Task {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if t.AgentID == "" {
		return t // 调用方需保证 agentID 非空
	}
	if t.TaskID == "" {
		t.TaskID = "task-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	if t.Status == "" {
		t.Status = "pending"
	}
	// M5 作业编排：含前置依赖的任务初始为 blocked，待依赖 done 后释放为 pending。
	if len(t.DependsOn) > 0 {
		t.Status = "blocked"
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO tasks (task_id, agent_id, tenant_id, type, command, content, path, status, retry_count, max_retries, dead_letter, schedule, parent_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE type=VALUES(type), command=VALUES(command), content=VALUES(content),
		   path=VALUES(path), status=VALUES(status), schedule=VALUES(schedule), parent_id=VALUES(parent_id), max_retries=VALUES(max_retries)`,
		t.TaskID, t.AgentID, t.TenantID, t.Type, t.Command, t.Content, t.Path, t.Status,
		t.RetryCount, t.MaxRetries, t.DeadLetter, t.Schedule, t.ParentID, t.CreatedAt); err != nil {
		log.Printf("[store] CreateTask 失败 %s: %v", t.TaskID, err)
	}
	s.publish(events.Event{Action: "create_task", Target: t.TaskID, TenantID: t.TenantID, Detail: t.Command, Level: events.LevelInfo})
	return t
}

// ClaimTask 原子领取该 agent 的下一条 pending 任务（FOR UPDATE 行锁保证多副本不双领，P1-1）。
func (s *SQLStore) ClaimTask(agentID string) *proto.Task {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("[store] ClaimTask begin 失败 %s: %v", agentID, err)
		return nil
	}
	defer tx.Rollback()

	var taskID, typ, command string
	var createdAt time.Time
	if err := tx.QueryRowContext(ctx,
		`SELECT task_id, type, command, created_at FROM tasks
		 WHERE agent_id=? AND (status IS NULL OR status='pending') AND (schedule IS NULL OR schedule='') ORDER BY created_at LIMIT 1 FOR UPDATE`,
		agentID).Scan(&taskID, &typ, &command, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		log.Printf("[store] ClaimTask 查询失败 %s: %v", agentID, err)
		return nil
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE tasks SET status='running', claimed_by='controlplane', claimed_at=? WHERE task_id=?`,
		time.Now().UTC(), taskID); err != nil {
		log.Printf("[store] ClaimTask 更新失败 %s: %v", taskID, err)
		return nil
	}
	if err := tx.Commit(); err != nil {
		log.Printf("[store] ClaimTask commit 失败 %s: %v", taskID, err)
		return nil
	}
	return &proto.Task{
		TaskID: taskID, AgentID: agentID, Type: typ, Command: command,
		Status: "running", CreatedAt: createdAt, ClaimedBy: "controlplane", ClaimedAt: time.Now().UTC(),
	}
}

// ReclaimStaleTasks 复位超期未完成的 running 任务为 pending（P0-1 任务必达）。
// agent 经 ClaimTask 领取（claimed_at 写当前时间）后若失联、超过 maxAge 仍未上报结果，
// 该任务将永远卡在 running；此处周期性调用把它复位，重新进入调度队列。
func (s *SQLStore) ReclaimStaleTasks(maxAge time.Duration) int {
	if maxAge <= 0 {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET status='pending', claimed_by=NULL, claimed_at=NULL WHERE status='running' AND claimed_at < ?`,
		time.Now().UTC().Add(-maxAge))
	if err != nil {
		log.Printf("[store] ReclaimStaleTasks 失败: %v", err)
		return 0
	}
	n, _ := res.RowsAffected()
	return int(n)
}

// FireDueSchedules 评估所有模板任务（parent_id 为空且 schedule 非空），
// 对到点（cron 匹配 now 且 last_fired_at 早于本分钟）的模板派生一个 pending 实例并回写 last_fired_at。
// 返回本批次派生的实例数（F4 定时/周期调度；控制面 scheduleLoop 周期调用）。
// 注意：SQL 路径依赖 live MySQL，仅在 CI 集成测试（env 门控）中真正运行。
func (s *SQLStore) FireDueSchedules(now time.Time) int {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	minuteStart := now.Truncate(time.Minute)
	rows, err := s.db.QueryContext(ctx,
		`SELECT task_id, agent_id, tenant_id, type, command, content, path, max_retries, schedule, last_fired_at
		 FROM tasks WHERE (parent_id IS NULL OR parent_id='') AND schedule <> '' AND (status IS NULL OR status='pending')`)
	if err != nil {
		log.Printf("[store] FireDueSchedules 查询失败: %v", err)
		return 0
	}
	defer rows.Close()
	type tpl struct {
		id, agentID, tenantID, typ, command, content, path, schedule string
		maxRetries                                                            int
		lastFiredAt                                                            time.Time
	}
	due := make([]tpl, 0)
	for rows.Next() {
		var tp tpl
		var lf sql.NullTime
		if err := rows.Scan(&tp.id, &tp.agentID, &tp.tenantID, &tp.typ, &tp.command, &tp.content, &tp.path, &tp.maxRetries, &tp.schedule, &lf); err != nil {
			log.Printf("[store] FireDueSchedules 扫描失败: %v", err)
			continue
		}
		if lf.Valid {
			tp.lastFiredAt = lf.Time
		}
		ok, merr := cron.Match(tp.schedule, now)
		if merr != nil || !ok {
			continue
		}
		// 本分钟已触发过则跳过，避免重复派生。
		if !tp.lastFiredAt.IsZero() && !tp.lastFiredAt.Before(minuteStart) {
			continue
		}
		due = append(due, tp)
	}
	fired := 0
	for _, tp := range due {
		instID := "task-" + strconv.FormatInt(now.UnixNano(), 10)
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO tasks (task_id, agent_id, tenant_id, type, command, content, path, status, retry_count, max_retries, schedule, parent_id, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', 0, ?, '', ?, ?)`,
			instID, tp.agentID, tp.tenantID, tp.typ, tp.command, tp.content, tp.path, tp.maxRetries, tp.id, now.UTC()); err != nil {
			log.Printf("[store] FireDueSchedules 派生实例失败 %s: %v", instID, err)
			continue
		}
		if _, err := s.db.ExecContext(ctx,
			`UPDATE tasks SET last_fired_at=? WHERE task_id=?`, now.UTC(), tp.id); err != nil {
			log.Printf("[store] FireDueSchedules 回写 last_fired_at 失败 %s: %v", tp.id, err)
			continue
		}
		fired++
		s.publish(events.Event{Action: "schedule_fire", Target: instID, TenantID: tp.tenantID,
			Detail: "parent=" + tp.id + " cron=" + tp.schedule, Level: events.LevelInfo})
	}
	return fired
}

// UpsertDevice 写入/更新一台纳管设备（真实网段发现 P0-2 用；按 device_id 幂等）。
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
func (s *SQLStore) PendingDepth() int {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks WHERE status IS NULL OR status='pending'`).Scan(&n); err != nil {
		log.Printf("[store] PendingDepth 失败: %v", err)
		return 0
	}
	return n
}

// TaskResult 按 taskID 返回单条执行结果（A5/F7 结果查询 API）。
func (s *SQLStore) TaskResult(taskID string) *proto.TaskResult {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := s.db.QueryRowContext(ctx,
		`SELECT task_id, agent_id, exit_code, stdout, stderr, finished_at FROM task_results WHERE task_id=?`, taskID)
	var r proto.TaskResult
	var finishedAt time.Time
	if err := row.Scan(&r.TaskID, &r.AgentID, &r.ExitCode, &r.Stdout, &r.Stderr, &finishedAt); err != nil {
		if err != sql.ErrNoRows {
			log.Printf("[store] TaskResult 查询失败 %s: %v", taskID, err)
		}
		return nil
	}
	r.FinishedAt = finishedAt
	return &r
}

// CancelTask 取消任务（F3）：pending/running -> cancelled；已 done/failed/cancelled 不可取消。
func (s *SQLStore) CancelTask(id, tenantID string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET status='cancelled' WHERE task_id=? AND (status='pending' OR status='running')
		 AND (tenant_id=? OR ?='')`,
		id, tenantID, tenantID)
	if err != nil {
		log.Printf("[store] CancelTask 失败 %s: %v", id, err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// RetireDevice 退役/下线设备（F5）：标记 retired；tenantID 非空时校验租户。
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
func (s *SQLStore) Provision(deviceID, host, tenantID string) (token, bootstrap string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// 安全（F15）：payload 用 | 分隔，deviceID/tenantID 含 | 导致解析歧义，直接拒绝。
	if strings.Contains(deviceID, "|") || strings.Contains(tenantID, "|") {
		return "", "", fmt.Errorf("deviceID 或 tenantID 含非法字符 |")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE devices SET state='provisioning', ip=? WHERE device_id=? AND (tenant_id=? OR ?='')`,
		host, deviceID, tenantID, tenantID)
	if err != nil {
		return "", "", fmt.Errorf("Provision 失败 %s: %w", deviceID, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return "", "", fmt.Errorf("device %s not found or tenant mismatch", deviceID)
	}
	tok, e := s.issueToken(ctx, deviceID, tenantID, 15*time.Minute)
	if e != nil {
		return "", "", e
	}
	// bootstrap 为占位模板，真实控制面地址由 HTTP handler 按请求 host 重写。
	boot := fmt.Sprintf("curl -sSL http://<control-plane>:8080/install.sh | sh -s -- --token=%s", tok)
	return tok, boot, nil
}

// issueToken 在已持 ctx 下签发一个一次性 install token（HMAC(deviceID|tenantID|expiry|nonce)），
// 落 install_tokens 表（ON DUPLICATE 重置消费态，幂等重推）。
// 安全（P1-F7）：token 列只存 SHA-256 摘要，不存明文 token——DB 只读账号/备份泄露不等于活体 token 泄露。
func (s *SQLStore) issueToken(ctx context.Context, deviceID, tenantID string, ttl time.Duration) (string, error) {
	if s.secret == "" {
		s.secret = mustRandHex(32) // 兜底，正常构造时已置随机密钥
	}
	nonce := randHex(16)
	expiresAt := time.Now().UTC().Add(ttl)
	payload := strings.Join([]string{tenantID, deviceID, strconv.FormatInt(expiresAt.Unix(), 10), nonce}, "|")
	mac := hmac.New(sha256.New, []byte(s.secret))
	mac.Write([]byte(payload))
	tok := hex.EncodeToString(mac.Sum(nil)) + "." + payload
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO install_tokens (token, device_id, tenant_id, expires_at, consumed)
		 VALUES (?, ?, ?, ?, 0)
		 ON DUPLICATE KEY UPDATE device_id=VALUES(device_id), tenant_id=VALUES(tenant_id), expires_at=VALUES(expires_at), consumed=0`,
		hashToken(tok), deviceID, tenantID, expiresAt); err != nil {
		return "", fmt.Errorf("issueToken 失败: %w", err)
	}
	return tok, nil
}

// IssueToken 生成并登记一个一次性 install token（B1）。
func (s *SQLStore) IssueToken(deviceID, tenantID string, ttl time.Duration) (token string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if deviceID == "" {
		return "", fmt.Errorf("deviceID required")
	}
	return s.issueToken(ctx, deviceID, tenantID, ttl)
}

// ConsumeToken 校验并消费 token：限时、未用过才返回设备与租户并置 consumed；否则返回 ok=false。
// 安全（P0-F2）：原子条件 UPDATE（consumed=0 AND 未过期）+ RowsAffected==1 判定，
// 消除 check-then-act TOCTOU 竞态，多副本并发下同一 token 只会被消费一次。
func (s *SQLStore) ConsumeToken(token string) (deviceID, tenantID string, ok bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// 安全（F8）：先验 HMAC 签名（防 DB 写权限伪造），签名不对直接拒绝。
	if !verifyTokenMAC(s.secret, token) {
		return "", "", false
	}
	hash := hashToken(token) // P1-F7：库存摘要，按摘要匹配
	// 原子抢占：仅当未被消费且未过期时翻转 consumed=0→1，RowsAffected==1 即消费成功。
	res, err := s.db.ExecContext(ctx,
		`UPDATE install_tokens SET consumed=1 WHERE token=? AND consumed=0 AND expires_at > ?`,
		hash, time.Now().UTC())
	if err != nil {
		log.Printf("[store] ConsumeToken 抢占失败: %v", err)
		return "", "", false
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return "", "", false // 已被消费 / 已过期 / 不存在
	}
	// 消费成功后读回设备与租户（token 行此时已唯一锁定为本实例）。
	if err := s.db.QueryRowContext(ctx,
		`SELECT device_id, tenant_id FROM install_tokens WHERE token=?`, hash,
	).Scan(&deviceID, &tenantID); err != nil {
		log.Printf("[store] ConsumeToken 读回失败: %v", err)
		return "", "", false
	}
	return deviceID, tenantID, true
}

// Alerts 返回活跃告警（M7）；tenantID 非空时按租户过滤。
func (s *SQLStore) Alerts(tenantID string) []*proto.Alert {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT tenant_id, device_id, agent_id, severity, message, created_at, alert_id, status, acknowledged_by, silenced_until, comment, updated_at FROM alerts`
	var args []interface{}
	if tenantID != "" {
		q += ` WHERE tenant_id=?`
		args = append(args, tenantID)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] Alerts 查询失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*proto.Alert
	for rows.Next() {
		var a proto.Alert
		var createdAt, silencedUntil, updatedAt time.Time
		var alertID, status, ackBy, comment sql.NullString
		if err := rows.Scan(&a.TenantID, &a.DeviceID, &a.AgentID, &a.Severity, &a.Message, &createdAt,
			&alertID, &status, &ackBy, &silencedUntil, &comment, &updatedAt); err != nil {
			log.Printf("[store] Alerts 扫描失败: %v", err)
			continue
		}
		a.CreatedAt = createdAt
		a.AlertID = alertID.String
		a.Status = status.String
		a.AcknowledgedBy = ackBy.String
		a.SilencedUntil = silencedUntil
		a.Comment = comment.String
		a.UpdatedAt = updatedAt
		out = append(out, &a)
	}
	return out
}

// AddAlert 记录一条告警（M7）。
func (s *SQLStore) AddAlert(a *proto.Alert) {
	s.addAlert(context.Background(), a)
}

// Alert 按 alertID 返回单条告警（M7；供 ack/silence 定位）。
func (s *SQLStore) Alert(id string) *proto.Alert {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := s.db.QueryRowContext(ctx,
		`SELECT tenant_id, device_id, agent_id, severity, message, created_at, alert_id, status, acknowledged_by, silenced_until, comment, updated_at FROM alerts WHERE alert_id=?`,
		id)
	var a proto.Alert
	var createdAt, silencedUntil, updatedAt time.Time
	var alertID, status, ackBy, comment sql.NullString
	if err := row.Scan(&a.TenantID, &a.DeviceID, &a.AgentID, &a.Severity, &a.Message, &createdAt,
		&alertID, &status, &ackBy, &silencedUntil, &comment, &updatedAt); err != nil {
		if err != sql.ErrNoRows {
			log.Printf("[store] Alert 查询失败: %v", err)
		}
		return nil
	}
	a.CreatedAt = createdAt
	a.AlertID = alertID.String
	a.Status = status.String
	a.AcknowledgedBy = ackBy.String
	a.SilencedUntil = silencedUntil
	a.Comment = comment.String
	a.UpdatedAt = updatedAt
	return &a
}

// AckAlert 确认告警（M7）；tenantID 非空时校验归属，越权返回 false。
func (s *SQLStore) AckAlert(id, tenantID, by string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := s.db.ExecContext(ctx,
		`UPDATE alerts SET status=?, acknowledged_by=?, updated_at=? WHERE alert_id=? AND (tenant_id=? OR ?='')`,
		proto.AlertStatusAcknowledged, by, time.Now().UTC(), id, tenantID, tenantID)
	if err != nil {
		log.Printf("[store] AckAlert 失败: %v", err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// SilenceAlert 静默告警（M7）；until 为零值默认静默 24h；tenantID 非空时校验归属，越权返回 false。
func (s *SQLStore) SilenceAlert(id, tenantID, by string, until time.Time, comment string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if until.IsZero() {
		until = time.Now().UTC().Add(24 * time.Hour)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE alerts SET status=?, acknowledged_by=?, silenced_until=?, comment=?, updated_at=? WHERE alert_id=? AND (tenant_id=? OR ?='')`,
		proto.AlertStatusSilenced, by, until, comment, time.Now().UTC(), id, tenantID, tenantID)
	if err != nil {
		log.Printf("[store] SilenceAlert 失败: %v", err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// Audit 记录一条审计事件（U-04 等保三级：操作 100% 留痕）。
func (s *SQLStore) Audit(e *proto.AuditEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log (tenant_id, user_id, action, target, detail, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		e.TenantID, e.UserID, e.Action, e.Target, e.Detail, e.CreatedAt); err != nil {
		log.Printf("[store] Audit 写入失败: %v", err)
	}
}

// Audits 返回最近 100 条审计事件（MVP；生产可加时间窗/分页）。
func (s *SQLStore) Audits() []*proto.AuditEvent {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx,
		`SELECT tenant_id, user_id, action, target, detail, created_at FROM audit_log ORDER BY id DESC LIMIT 100`)
	if err != nil {
		log.Printf("[store] Audits 查询失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*proto.AuditEvent
	for rows.Next() {
		var e proto.AuditEvent
		var createdAt time.Time
		if err := rows.Scan(&e.TenantID, &e.UserID, &e.Action, &e.Target, &e.Detail, &createdAt); err != nil {
			log.Printf("[store] Audits 扫描失败: %v", err)
			continue
		}
		e.CreatedAt = createdAt
		out = append(out, &e)
	}
	return out
}

// QueryAudits 按租户/动作/时间窗过滤审计事件（P0-4 审计可查；U-04 等保三级留痕必须可检索）。
// tenant/action 为空表示不限；since/until 为零值表示不限；limit<=0 表示不限制。返回按时间倒序。
func (s *SQLStore) QueryAudits(tenant, action string, since, until time.Time, limit int) []*proto.AuditEvent {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT tenant_id, user_id, action, target, detail, created_at FROM audit_log WHERE 1=1`
	args := []interface{}{}
	if tenant != "" {
		q += ` AND tenant_id=?`
		args = append(args, tenant)
	}
	if action != "" {
		q += ` AND action=?`
		args = append(args, action)
	}
	if !since.IsZero() {
		q += ` AND created_at>=?`
		args = append(args, since)
	}
	if !until.IsZero() {
		q += ` AND created_at<=?`
		args = append(args, until)
	}
	q += ` ORDER BY id DESC`
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] QueryAudits 查询失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*proto.AuditEvent
	for rows.Next() {
		var e proto.AuditEvent
		var createdAt time.Time
		if err := rows.Scan(&e.TenantID, &e.UserID, &e.Action, &e.Target, &e.Detail, &createdAt); err != nil {
			log.Printf("[store] QueryAudits 扫描失败: %v", err)
			continue
		}
		e.CreatedAt = createdAt
		out = append(out, &e)
	}
	return out
}

// cacheAgent 把 agent 状态写入 Redis HASH opsmesh:agents（field=agent_id, value=JSON）。
// MVP 仅写缓存；生产可将读取也改为走 Redis 降低 MySQL 压力。
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
func (s *SQLStore) RenewLeadership(ttl time.Duration) bool {
	if ttl <= 0 {
		ttl = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	now := time.Now().UTC()
	exp := now.Add(ttl)
	// 4 个插入占位（id, holder, expires_at, updated_at）+ 6 个 ON DUPLICATE 条件占位。
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO leader_lease (id, holder, expires_at, updated_at)
		VALUES (1, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			holder=IF(expires_at < ?, VALUES(holder), holder),
			expires_at=IF(expires_at < ?, VALUES(expires_at), expires_at),
			updated_at=IF(expires_at < ?, VALUES(updated_at), updated_at)
	`, s.instanceID, exp, now, now, s.instanceID, exp, now); err != nil {
		log.Printf("[store] RenewLeadership 抢占失败: %v", err)
		s.mu.Lock()
		s.isLeader = false
		s.mu.Unlock()
		return false
	}
	// 读取当前 holder 以确认本实例是否为主。
	var holder string
	var expiresAt time.Time
	if err := s.db.QueryRowContext(ctx,
		`SELECT holder, expires_at FROM leader_lease WHERE id=1`).Scan(&holder, &expiresAt); err != nil {
		log.Printf("[store] RenewLeadership 读取失败: %v", err)
		s.mu.Lock()
		s.isLeader = false
		s.mu.Unlock()
		return false
	}
	leader := holder == s.instanceID && expiresAt.After(now)
	s.mu.Lock()
	s.isLeader = leader
	s.leaseUntil = expiresAt
	s.mu.Unlock()
	return leader
}

// IsLeader 返回本实例当前是否自认为 leader（租约未过期）。
func (s *SQLStore) IsLeader() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isLeader && s.leaseUntil.After(time.Now().UTC())
}

// CancelledTaskIDs 返回该 agent 当前 cancelled 状态的任务 ID（F3 取消信号下发用）。
func (s *SQLStore) CancelledTaskIDs(agentID string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx,
		`SELECT task_id FROM tasks WHERE agent_id=? AND status='cancelled'`, agentID)
	if err != nil {
		log.Printf("[store] CancelledTaskIDs 失败 %s: %v", agentID, err)
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		out = append(out, id)
	}
	return out
}

// CleanupTokens 清理过期/已消费的 install token（F9 无界增长防护）。
func (s *SQLStore) CleanupTokens(batch int) int {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `DELETE FROM install_tokens WHERE expires_at < ?`
	var args []interface{}
	args = append(args, time.Now().UTC())
	if batch > 0 {
		q += ` LIMIT ?`
		args = append(args, batch)
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] CleanupTokens 失败: %v", err)
		return 0
	}
	n, _ := res.RowsAffected()
	return int(n)
}

// RetireStaleDevices F5 离线超龄自动归档：最后心跳早于 maxAge 的 agent 所对应设备
// （或已无 agent 的孤儿设备）批量标记 retired。返回归档数。
func (s *SQLStore) RetireStaleDevices(maxAge time.Duration) int {
	if maxAge <= 0 {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
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
