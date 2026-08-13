package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	// 匿名导入以注册 MySQL 驱动（database/sql 的 "mysql" 驱动名）。
	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"

	"opsmesh/internal/events"
	"opsmesh/internal/proto"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

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

	// 设备实时监控指标缓存：deviceID -> 环形缓冲（保留最近 N 条历史，task 223）。
	// 高频时序数据落库应由 Prometheus/InfluxDB 承担，控制面仅缓存最近 2h 历史供 API 查询，
	// 避免给 MySQL 写入压力（每 30s/agent 一次写）。
	deviceMetrics map[string]*metricsRing

	// task 81 gRPC agent 身份绑定：agentID -> HMAC 签名密钥缓存（避免每次请求都查 MySQL）。
	// 权威存储在 agents.secret 列；此处仅缓存已查询过的 agent 密钥（首次查询后填充）。
	agentSecretCache map[string]string

	// task 247 agent 日志上报：已落库的 LogReport 批次列表（内存暂存）。
	// agent 上报日志的高频写入不宜直接落 MySQL（每 30s/agent 一次写），
	// 检索侧由 logstore.SQLLogStore 走独立表/连接池承担；此处仅承接上报并暂存供 API 查询。
	// 由 s.mu 保护并发安全。
	agentLogs []proto.LogReport

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

	s := &SQLStore{db: db, rdb: rdb, instanceID: instID, secret: mustRandHex(32), deviceMetrics: make(map[string]*metricsRing), agentSecretCache: make(map[string]string)}
	if err := s.runMigrations(); err != nil {
		log.Printf("[store] 迁移失败（运行期可能不可用）: %v", err)
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

// migrationFile 描述一个待应用的迁移文件。
type migrationFile struct {
	version int    // 从文件名前缀解析的版本号（001 → 1）
	name    string // 文件名（如 001_initial.sql）
	content string // SQL 文件全文
}

// runMigrations 执行版本化 schema 迁移。
//
// 流程：
//  1. 确保 schema_migrations 表存在（用于记录已应用版本号）。
//  2. 读取已应用版本号集合。
//  3. 从 embed.FS 读取 migrations/*.sql，按版本号升序排序。
//  4. 对每个未应用的迁移：BEGIN TX → 逐条执行 SQL → INSERT schema_migrations → COMMIT；
//     任一语句失败则回滚整批并返回错误。
//  5. applyLegacyColumnFixups：兼容老库的增量补列/补索引（历史遗留，待后续转为正式迁移）。
//  6. seedRBAC：幂等预置默认权限/角色/用户。
//
// 幂等性：已应用的迁移在 step 4 被跳过；step 5/6 内部均为 IF NOT EXISTS 语义。
// 事务边界：每个迁移文件独立事务，保证 schema_migrations 记录与表结构变更原子化。
func (s *SQLStore) runMigrations() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. 确保 schema_migrations 表存在（复用历史定义，sql.go 原 initSchema 已建此表）。
	//    G5 / C-1：增加 checksum 列记录迁移文件 sha256 摘要，启动时校验防篡改。
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INT PRIMARY KEY,
		applied_at DATETIME,
		checksum VARCHAR(64) NOT NULL DEFAULT ''
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	// 兼容老库：schema_migrations 已存在但缺 checksum 列时补列（G5 / C-1）。
	s.alterColumnIfMissing(ctx, "schema_migrations", "checksum", "VARCHAR(64) NOT NULL DEFAULT ''")

	// 2. 读取已应用版本号及其 checksum。
	applied, err := s.appliedMigrations(ctx)
	if err != nil {
		return fmt.Errorf("read applied migrations: %w", err)
	}

	// 3. 读取嵌入的迁移文件并按版本号排序。
	files, err := migrationFiles()
	if err != nil {
		return fmt.Errorf("list migration files: %w", err)
	}

	// 3.5 G5 / C-1 防篡改校验：已应用迁移的 checksum 必须与当前文件 sha256 一致，
	//     不一致则拒绝启动（避免迁移文件被静默篡改导致 schema 漂移）。
	for _, mf := range files {
		recorded, ok := applied[mf.version]
		if !ok {
			continue
		}
		expected := sha256Hex(mf.content)
		if recorded.checksum != "" && recorded.checksum != expected {
			return fmt.Errorf("migration %d (%s) checksum mismatch: recorded=%s expected=%s (迁移文件已被篡改，拒绝启动)",
				mf.version, mf.name, recorded.checksum, expected)
		}
	}

	// 4. 逐个执行未应用的迁移（事务化）。
	for _, mf := range files {
		if _, ok := applied[mf.version]; ok {
			continue
		}
		if err := s.applyMigration(ctx, mf); err != nil {
			return fmt.Errorf("apply migration %d (%s): %w", mf.version, mf.name, err)
		}
		log.Printf("[store] 迁移 %d (%s) 已应用 (checksum=%s)", mf.version, mf.name, sha256Hex(mf.content))
	}

	// 5. 兼容老库增量补列/补索引（历史遗留，待后续转为正式 002+ 迁移）。
	s.applyLegacyColumnFixups(ctx)

	// 6. 数据 seed：幂等预置默认权限/角色/用户（与 MemoryStore 一致，保 HA 多副本身份一致）。
	if err := s.seedRBAC(ctx); err != nil {
		log.Printf("[store] seedRBAC 失败（非致命）: %v", err)
	}
	return nil
}

// migrationRecord 描述一个已应用迁移的版本记录（含 checksum，G5 / C-1）。
type migrationRecord struct {
	version  int
	checksum string
}

// appliedMigrations 返回 schema_migrations 表中已记录的版本号及其 checksum。
// G5 / C-1：返回 map[version] -> migrationRecord，供 runMigrations 校验防篡改。
func (s *SQLStore) appliedMigrations(ctx context.Context) (map[int]migrationRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT version, checksum FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	applied := make(map[int]migrationRecord)
	for rows.Next() {
		var r migrationRecord
		if err := rows.Scan(&r.version, &r.checksum); err != nil {
			return nil, err
		}
		applied[r.version] = r
	}
	return applied, rows.Err()
}

// migrationFiles 从 embed.FS 读取 migrations/*.sql，解析文件名前缀版本号，按版本升序排序。
// 文件名约定：NNN_description.sql，其中 NNN 为零填充版本号（001、002...）。
//
// G5 / C-1：跳过 NNN_*.down.sql 回滚占位文件（仅作为未来回滚接口的占位，
// 不参与正向迁移执行；embed 指令 migrations/*.sql 会同时嵌入 .down.sql，
// 此处显式过滤避免被误当作正向迁移）。
func migrationFiles() ([]migrationFile, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	var files []migrationFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		// 跳过回滚占位文件（.down.sql）。
		if strings.HasSuffix(e.Name(), ".down.sql") {
			continue
		}
		base := strings.TrimSuffix(e.Name(), ".sql")
		// 解析开头连续数字部分作为版本号。
		i := 0
		for i < len(base) && base[i] >= '0' && base[i] <= '9' {
			i++
		}
		if i == 0 {
			return nil, fmt.Errorf("migration file %q 缺少版本号前缀", e.Name())
		}
		v, err := strconv.Atoi(base[:i])
		if err != nil {
			return nil, fmt.Errorf("parse version %q from %q: %w", base[:i], e.Name(), err)
		}
		content, err := migrationFS.ReadFile(path.Join("migrations", e.Name()))
		if err != nil {
			return nil, err
		}
		files = append(files, migrationFile{version: v, name: e.Name(), content: string(content)})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].version < files[j].version })
	return files, nil
}

// applyMigration 在单个事务中执行一个迁移文件的全部 SQL，并记录版本号与 checksum
// 到 schema_migrations。任一语句失败则回滚整批，保证 schema_migrations 记录与表结构
// 变更原子化。
//
// G5 / C-1：执行前计算迁移文件内容的 sha256 摘要，执行后随版本号一并存入
// schema_migrations.checksum 列，供后续启动时校验防篡改。
func (s *SQLStore) applyMigration(ctx context.Context, mf migrationFile) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() // Commit 后 Rollback 为 no-op；失败时回滚。

	for _, stmt := range splitSQLStatements(mf.content) {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("exec stmt: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, applied_at, checksum) VALUES (?, ?, ?)`,
		mf.version, time.Now().UTC(), sha256Hex(mf.content)); err != nil {
		return fmt.Errorf("record migration: %w", err)
	}
	return tx.Commit()
}

// sha256Hex 返回给定内容的 sha256 摘要的十六进制小写表示（64 字符）。
// 用于 schema_migrations.checksum 列记录迁移文件指纹，启动时校验防篡改（G5 / C-1）。
func sha256Hex(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// splitSQLStatements 将多语句 SQL 文件按分号拆分为可逐条 Exec 的语句列表。
// 跳过空行与 -- 行注释；不处理块注释/字符串内分号（当前迁移文件仅含简单 DDL，无需）。
func splitSQLStatements(content string) []string {
	var stmts []string
	var buf strings.Builder
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		buf.WriteString(line)
		buf.WriteString("\n")
		if strings.HasSuffix(trimmed, ";") {
			stmt := strings.TrimSpace(buf.String())
			if stmt != "" {
				stmts = append(stmts, stmt)
			}
			buf.Reset()
		}
	}
	if buf.Len() > 0 {
		stmt := strings.TrimSpace(buf.String())
		if stmt != "" {
			stmts = append(stmts, stmt)
		}
	}
	return stmts
}

// applyLegacyColumnFixups 兼容老库的增量补列/补索引。
//
// 历史上 initSchema 通过 alterColumnIfMissing/createIndexIfMissing 为已存在但缺列/缺索引
// 的老库补结构。迁移框架上线后，新库由 001_initial.sql 一次性建齐；老库仍需这些补丁
// 才能升级到最新结构。此处保留全部补丁逻辑，待后续以 002+ 正式迁移形式纳入后可移除。
func (s *SQLStore) applyLegacyColumnFixups(ctx context.Context) {
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
	// 告警状态扩展（M7 ack/silence）：向后兼容补列（老表缺列不报错）。
	s.alterColumnIfMissing(ctx, "alerts", "alert_id", "VARCHAR(64)")
	s.alterColumnIfMissing(ctx, "alerts", "status", "VARCHAR(16)")
	s.alterColumnIfMissing(ctx, "alerts", "acknowledged_by", "VARCHAR(64)")
	s.alterColumnIfMissing(ctx, "alerts", "silenced_until", "DATETIME")
	s.alterColumnIfMissing(ctx, "alerts", "comment", "TEXT")
	s.alterColumnIfMissing(ctx, "alerts", "updated_at", "DATETIME")
	// Phase-3 轻量审批流：ci_items 增补审批状态列（向后兼容，默认 approved）。
	s.alterColumnIfMissing(ctx, "ci_items", "approval_status", "VARCHAR(16) DEFAULT 'approved'")
	// task 88 租户隔离：存量 k8s_clusters 表补 tenant_id 列（全新库由 CREATE TABLE 保证）。
	s.alterColumnIfMissing(ctx, "k8s_clusters", "tenant_id", "VARCHAR(64)")
	// task 246 M2 告警治理：alert_rules 表补 created_by 列（全新库由 005_m2_alert_governance.sql 保证）。
	// 兼容老库（MySQL < 8.0 不支持 ADD COLUMN IF NOT EXISTS，005 迁移可能失败）。
	s.alterColumnIfMissing(ctx, "alert_rules", "created_by", "VARCHAR(64)")
	// task 100/111 增量补列/补索引（详见 sql_legacy.go initSchemaExtra）。
	s.initSchemaExtra(ctx)
	// 工程债治理：补二级索引，避免 ClaimTask 的 FOR UPDATE 全表扫描加锁，
	// 以及按租户分页查询（tenant_id + created_at DESC）回表全扫。
	// MySQL 不支持 CREATE INDEX IF NOT EXISTS，故用 createIndexIfMissing 兼容已有库。
	s.createIndexIfMissing(ctx, "tasks", "idx_tasks_tenant_created", "(tenant_id, created_at DESC)")
	s.createIndexIfMissing(ctx, "tasks", "idx_tasks_agent", "(agent_id, status)")
	s.createIndexIfMissing(ctx, "audit_log", "idx_audit_tenant_created", "(tenant_id, created_at DESC)")
}

// initSchema 幂等建表（CREATE TABLE IF NOT EXISTS）。
//
// Deprecated: 由 runMigrations 替代，保留供向后兼容。新代码应直接调用 runMigrations。
// 本函数现在仅转发到 runMigrations，原建表/补列/索引逻辑已分别移入
// migrations/001_initial.sql 与 applyLegacyColumnFixups。
func (s *SQLStore) initSchema() error {
	return s.runMigrations()
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
