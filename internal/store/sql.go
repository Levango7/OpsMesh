package store

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	// 匿名导入以注册 MySQL 驱动（database/sql 的 "mysql" 驱动名）。
	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"

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
	db   *sql.DB
	rdb  *redis.Client
	bus  events.Bus // 事件总线（P1-5）；可 nil（测试/默认 noop）
	demo bool       // 演示模式（P0-5）：开启时注册预置 uname -a

	instanceID string     // 本进程实例唯一标识（A3 选主参与方）
	mu         sync.Mutex // 保护 isLeader / leaseUntil / deviceMetrics 的读写
	isLeader   bool       // 本实例当前是否自认为 leader
	leaseUntil time.Time  // 当前租约过期时间（UTC）
	secret     string     // B1 install token 的 HMAC 签名密钥（WithSecret 注入；空则构造时随机）

	// 设备实时监控指标缓存：deviceID -> 最新指标。
	// 高频时序数据落库应由 Prometheus/InfluxDB 承担，控制面仅缓存最新值供 API 查询，
	// 避免给 MySQL 写入压力（每 30s/agent 一次写）。
	deviceMetrics map[string]*proto.DeviceMetrics

	// task 81 gRPC agent 身份绑定：agentID -> HMAC 签名密钥缓存（避免每次请求都查 MySQL）。
	// 权威存储在 agents.secret 列；此处仅缓存已查询过的 agent 密钥（首次查询后填充）。
	agentSecretCache map[string]string
}

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
		if err := s.bus.Publish(context.Background(), e); err != nil {
			log.Printf("store: 发布事件 %s 失败: %v", e.Action, err)
		}
	}
}

// NewSQLStore 打开 MySQL 连接并建表（幂等）。redisAddr 为空则跳过 Redis。

func NewSQLStore(dsn, redisAddr string) (*SQLStore, error) {
	// 必须开启 parseTime，否则 DATETIME 列无法 Scan 进 time.Time。
	db, err := sql.Open("mysql", ensureParseTime(dsn))
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	// 工程债治理：连接池上限（多租户 schema 隔离下每租户独立 *sql.DB，
	// 无上限会导致连接数随租户数无界增长，最终打满 MySQL max_connections）。
	// 50/10/30min 为单 schema 上限：多租户总连接数 = 租户数 × 50，须配合
	// MySQL server 端 max_connections 容量规划。
	db.SetMaxOpenConns(50)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)
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

	s := &SQLStore{db: db, rdb: rdb, instanceID: instID, secret: mustRandHex(32), deviceMetrics: make(map[string]*proto.DeviceMetrics), agentSecretCache: make(map[string]string)}
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
			content MEDIUMTEXT,
			path VARCHAR(512),
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
		// P0-1 用户中心（RBAC）：users / roles / permissions 三表。
		// 用户名唯一索引兜底（CreateUser 重复校验 + INSERT 失败均返回 nil）。
		`CREATE TABLE IF NOT EXISTS users (
			id VARCHAR(64) PRIMARY KEY,
			username VARCHAR(64) NOT NULL UNIQUE,
			email VARCHAR(255),
			password_hash VARCHAR(255),
			status VARCHAR(16) DEFAULT 'active',
			role_ids JSON,
			created_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS roles (
			id VARCHAR(64) PRIMARY KEY,
			name VARCHAR(64) NOT NULL UNIQUE,
			description VARCHAR(255),
			permissions JSON,
			created_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS permissions (
			id VARCHAR(64) PRIMARY KEY,
			name VARCHAR(64) NOT NULL UNIQUE,
			description VARCHAR(255),
			group_name VARCHAR(64)
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
	// 设备基础元信息列（agent 注册时上报，设备列表/详情展示用）。
	s.alterColumnIfMissing(ctx, "devices", "hostname", "VARCHAR(255)")
	s.alterColumnIfMissing(ctx, "devices", "os", "VARCHAR(32)")
	s.alterColumnIfMissing(ctx, "devices", "arch", "VARCHAR(32)")
	// 安全债 85：users 表增加 must_change_password 列，预置弱口令首登强制改密。
	s.alterColumnIfMissing(ctx, "users", "must_change_password", "BOOLEAN DEFAULT 0")
	// task 81 gRPC agent 身份绑定：agents 表增加 secret 列存储 HMAC 签名密钥。
	s.alterColumnIfMissing(ctx, "agents", "secret", "VARCHAR(64)")
	s.alterColumnIfMissing(ctx, "tasks", "retry_count", "INT DEFAULT 0")
	s.alterColumnIfMissing(ctx, "tasks", "max_retries", "INT DEFAULT 0")
	s.alterColumnIfMissing(ctx, "tasks", "dead_letter", "BOOLEAN DEFAULT 0")
	s.alterColumnIfMissing(ctx, "tasks", "schedule", "VARCHAR(64)")
	s.alterColumnIfMissing(ctx, "tasks", "parent_id", "VARCHAR(64)")
	s.alterColumnIfMissing(ctx, "tasks", "last_fired_at", "DATETIME")
	s.alterColumnIfMissing(ctx, "tasks", "depends_on", "TEXT")
	s.alterColumnIfMissing(ctx, "tasks", "content", "MEDIUMTEXT")
	s.alterColumnIfMissing(ctx, "tasks", "path", "VARCHAR(512)")
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
	// P0-1 用户中心：幂等 seed 默认权限/角色/用户（与 MemoryStore 一致，保 HA 多副本身份一致）。
	if err := s.seedRBAC(ctx); err != nil {
		log.Printf("[store] seedRBAC 失败（非致命）: %v", err)
	}
	// Phase 3 K8s 集群管理：k8s_clusters 表（clusterID/name/server/kubeconfig/status）。
	// Kubeconfig 为敏感内容（TEXT 列），API 层负责脱敏后返回前端。
	if _, err := s.db.ExecContext(ctx, `
	CREATE TABLE IF NOT EXISTS k8s_clusters (
		id VARCHAR(64) PRIMARY KEY,
		tenant_id VARCHAR(64),
		name VARCHAR(255) NOT NULL,
		server VARCHAR(255),
		kubeconfig TEXT,
		status VARCHAR(16) DEFAULT 'unknown',
		created_at DATETIME,
		updated_at DATETIME
	)`); err != nil {
		log.Printf("[store] 建 k8s_clusters 表失败（非致命）: %v", err)
	}
	// task 88 租户隔离：存量 k8s_clusters 表补 tenant_id 列（全新库由 CREATE TABLE 保证）。
	s.alterColumnIfMissing(ctx, "k8s_clusters", "tenant_id", "VARCHAR(64)")
	// task 100/111 建表（详见 sql_legacy.go initSchemaExtra）
	s.initSchemaExtra(ctx)
	// 工程债治理：补二级索引，避免 ClaimTask 的 FOR UPDATE 全表扫描加锁，
	// 以及按租户分页查询（tenant_id + created_at DESC）回表全扫。
	// MySQL 不支持 CREATE INDEX IF NOT EXISTS，故用 createIndexIfMissing 兼容已有库。
	s.createIndexIfMissing(ctx, "tasks", "idx_tasks_tenant_created", "(tenant_id, created_at DESC)")
	s.createIndexIfMissing(ctx, "tasks", "idx_tasks_agent", "(agent_id, status)")
	s.createIndexIfMissing(ctx, "audit_log", "idx_audit_tenant_created", "(tenant_id, created_at DESC)")
	return nil
}

// createIndexIfMissing 当索引不存在时 CREATE INDEX（MySQL 无 CREATE INDEX IF NOT EXISTS）。
// 通过 information_schema.statistics 查询索引名是否已存在，避免重复建索引报错。
// 索引不存在或列缺失时仅日志提示，不阻断启动（兼容老库缺列场景）。

func (s *SQLStore) createIndexIfMissing(ctx context.Context, table, indexName, indexSpec string) {
	var cnt int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM information_schema.statistics
		 WHERE table_schema=DATABASE() AND table_name=? AND index_name=?`,
		table, indexName).Scan(&cnt); err != nil || cnt > 0 {
		return
	}
	if _, err := s.db.ExecContext(ctx, `CREATE INDEX `+indexName+` ON `+table+` `+indexSpec); err != nil {
		log.Printf("[store] 建索引 %s.%s 失败（非致命，可能缺列）: %v", table, indexName, err)
	}
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

func (s *SQLStore) RenewLeadership(ttl time.Duration) bool {
	if ttl <= 0 {
		ttl = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	now := time.Now().UTC()
	exp := now.Add(ttl)
	// 3 个 VALUES 占位（holder, expires_at, updated_at；id 为常量 1）+ 3 个 ON DUPLICATE 条件占位（均为 now）。
	// 条件为 (租约已过期 OR 当前即本实例持有)：过期则抢占；本实例持有且未过期则续租（刷新 expires_at）。
	// 修复：原逻辑仅 IF(expires_at < now)，本实例持有时租约不续期，每 TTL 周期被迫易主一次。
	// 注意 ON DUPLICATE KEY UPDATE 从左到右求值：后续 IF 引用 holder 时取第 1 行赋值后的新值。
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO leader_lease (id, holder, expires_at, updated_at)
		VALUES (1, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			holder=IF(expires_at < ? OR holder=VALUES(holder), VALUES(holder), holder),
			expires_at=IF(expires_at < ? OR holder=VALUES(holder), VALUES(expires_at), expires_at),
			updated_at=IF(expires_at < ? OR holder=VALUES(holder), VALUES(updated_at), updated_at)
	`, s.instanceID, exp, now, now, now, now); err != nil {
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
