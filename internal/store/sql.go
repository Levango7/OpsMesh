package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	// 具名导入：既注册 database/sql 的 "mysql" 驱动，又供迁移幂等判定使用
	// mysqlDriver.MySQLError 错误码（1050/1060/1061/1091）。
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"

	"github.com/Levango7/OpsMesh/internal/events"
	"github.com/Levango7/OpsMesh/internal/proto"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// SQLStore 基于 MySQL + Redis 的持久化实现（数据本地化，私有部署）。
//   - MySQL 为权威存储（四张表：agents / devices / tasks / task_results）。
//   - Redis 作 agent/device 状态缓存（MVP 仅写缓存，读取仍走 MySQL；
//     生产可改为读 Redis 以减 MySQL 压力，见各方法注释）。
//
// 即便运行期连不上库，也不会让 go build 失败：连接错误只在运行期日志提示并返回零值，
// 不会 panic。
//
// 真 HA：多副本控制面共享同一 MySQL；通过 leader_lease 表做分布式选主，
// 仅 leader 执行周期性协调任务（reclaim / schedule / provision / 离线归档），
// 避免重复派生/回收。每个进程实例持唯一 instanceID 参与抢占。
type SQLStore struct {
	db   *sql.DB
	rdb  *redis.Client
	bus  events.Bus // 事件总线；可 nil（测试/默认 noop）
	demo bool       // 演示模式：开启时注册预置 uname -a

	instanceID string     // 本进程实例唯一标识（选主参与方）
	mu         sync.Mutex // 保护 isLeader / leaseUntil / deviceMetrics 的读写
	isLeader   bool       // 本实例当前是否自认为 leader
	leaseUntil time.Time  // 当前租约过期时间（UTC）
	secret     string     // B1 install token 的 HMAC 签名密钥（WithSecret 注入；空则构造时随机）

	// 设备实时监控指标缓存：deviceID -> 环形缓冲（保留最近 N 条历史）。
	// 高频时序数据落库应由 Prometheus/InfluxDB 承担，控制面仅缓存最近 2h 历史供 API 查询，
	// 避免给 MySQL 写入压力（每 30s/agent 一次写）。
	deviceMetrics map[string]*metricsRing

	// gRPC agent 身份绑定：agentID -> HMAC 签名密钥缓存（避免每次请求都查 MySQL）。
	// 权威存储在 agents.secret 列；此处仅缓存已查询过的 agent 密钥（首次查询后填充）。
	agentSecretCache map[string]string

	// agent 日志上报：已落库的 LogReport 批次列表（内存暂存）。
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

// WithDemo 设置演示模式：开启时 Register 预置 uname -a 示例任务。
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
// redisPassword 为空表示 Redis 未设 --requirepass（不发送 AUTH）。
func NewSQLStore(dsn, redisAddr, redisPassword string) (*SQLStore, error) {
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
		rdb = redis.NewClient(&redis.Options{Addr: redisAddr, Password: redisPassword})
	}

	// 选主：生成本进程唯一实例 ID（hostname + pid + 纳秒），用于 leader_lease 抢占标识。
	host, herr := os.Hostname()
	if herr != nil || host == "" {
		host = "opsmesh"
	}
	instID := fmt.Sprintf("%s-%d-%d", host, os.Getpid(), time.Now().UnixNano())

	s := &SQLStore{
		db: db, rdb: rdb, instanceID: instID, secret: mustRandHex(32),
		deviceMetrics:    make(map[string]*metricsRing),
		agentSecretCache: make(map[string]string),
	}
	// 启动时序竞态防护：MySQL 容器可能尚未就绪（compose 起栈时 mysql 与 controlplane
	// 并发启动，Ping 失败不阻塞是 MVP 延迟连接语义）。迁移+seedRBAC 依赖 schema 存在，
	// 此处最多等待 migrationInitAttempts × migrationInitDelay（默认 60s）后重试。
	//
	// P0-5 fail-fast：超过重试窗口仍失败则拒绝启动，而不是「带着半迁移的 schema 提供服务」。
	// 半迁移库的危害远大于启动失败——服务能起但表/列不齐，读写静默失败或写坏数据，
	// 且健康检查仍显示健康（曾发生：015 迁移失败后控制面照常运行，设备注册报 1054 Unknown column）。
	// 交付形态下由容器编排（restart: unless-stopped / K8s restartPolicy）负责重启收敛。
	if err := s.initWithRetry(); err != nil {
		// 关闭连接池避免句柄泄漏（构造失败不应留悬挂连接）。
		_ = db.Close()
		if rdb != nil {
			_ = rdb.Close()
		}
		return nil, fmt.Errorf("store: 数据库初始化失败（迁移/预置未完成，拒绝以不完整 schema 启动）: %w", err)
	}
	return s, nil
}

// initWithRetry 重试执行 runMigrations（内含 seedRBAC），等待 MySQL 就绪。
// runMigrations 内部 step 6 已调用 seedRBAC（幂等），无需重复调用。
//
// 重试策略（P0-5）：
//   - 连接类错误（MySQL 未就绪/网络抖动）重试，窗口 = migrationInitAttempts × migrationInitDelay；
//   - fatalMigrationError（checksum 篡改、版本门禁不通过）立即返回——这类错误重试不会自愈，
//     继续等待只会把「迁移文件被改动」这类明确故障伪装成「数据库慢」，延长排障时间。
func (s *SQLStore) initWithRetry() error {
	var lastErr error
	var fatal *fatalMigrationError
	for i := 0; i < migrationInitAttempts; i++ {
		err := s.runMigrations()
		if err == nil {
			return nil
		}
		lastErr = err
		if errors.As(err, &fatal) {
			log.Printf("[store] 迁移致命错误（不重试，立即拒绝启动）: %v", err)
			return err
		}
		log.Printf("[store] 迁移失败（第 %d/%d 次，%.0fs 后重试）: %v", i+1, migrationInitAttempts, migrationInitDelay.Seconds(), err)
		time.Sleep(migrationInitDelay)
	}
	return lastErr
}

// ensureParseTime 在 DSN 中保证 parseTime=true（time.Time 直接 Scan）与
// clientFoundRows=true（UPDATE 语义按"匹配行数"而非"实际改变行数"计 RowsAffected）。
//
// clientFoundRows 的必要性（CI integration 实测捕获）：MySQL 默认 UPDATE 的
// RowsAffected 只数"值发生实际变化的行"——当更新值与现值完全相同（如秒级精度
// DATETIME 下同秒两次心跳 + 同状态）时返回 0。HeartbeatService/EnableXxx 等
// "确认存在性"语义的接口据此误判为"实例不存在"（false）。开启后 RowsAffected
// 数匹配行（与 PostgreSQL 等其它数据库语义一致），存在性判断恢复正确。
func ensureParseTime(dsn string) string {
	dsn = ensureDSNParam(dsn, "parseTime=true")
	return ensureDSNParam(dsn, "clientFoundRows=true")
}

// ensureDSNParam 在 DSN 中保证某 k=v 参数存在（已含则不重复加）。
func ensureDSNParam(dsn, param string) string {
	kv := strings.SplitN(param, "=", 2)
	if len(kv) != 2 {
		return dsn
	}
	// 已含该参数名（k=...）则视为已配置。
	if strings.Contains(dsn, kv[0]+"=") {
		return dsn
	}
	if strings.Contains(dsn, "?") {
		return dsn + "&" + param
	}
	return dsn + "?" + param
}

// migrationFile 描述一个待应用的迁移文件。
type migrationFile struct {
	version int    // 从文件名前缀解析的版本号（001 → 1）
	name    string // 文件名（如 001_initial.sql）
	content string // SQL 文件全文
}

const (
	// migrationLockTimeoutSec 迁移咨询锁（GET_LOCK）等待上限（秒）。
	// 多副本控制面与各微服务可能同时首连同一 schema 触发迁移，后到者在此上限内等待
	// 前者完成，而不是并发执行同一 DDL（并发 ADD COLUMN 必然一方报 1060 而失败）。
	migrationLockTimeoutSec = 60
	// migrationWorkBudgetSec 持锁后的迁移工作预算（秒）。
	migrationWorkBudgetSec = 60
	// migrationInitAttempts / migrationInitDelay：MySQL 未就绪时的重试窗口（默认 60s）。
	// 容器化部署下 MySQL 首次初始化（init 脚本 + InnoDB 恢复）可能超过 30s，故给足窗口。
	migrationInitAttempts = 20
	migrationInitDelay    = 3 * time.Second
)

// fatalMigrationError 标记「重试不会自愈」的迁移错误（迁移文件 checksum 被篡改、
// 二进制与 schema 版本门禁不通过）。initWithRetry 对其立即返回，不做退避重试。
type fatalMigrationError struct{ err error }

func (e *fatalMigrationError) Error() string { return e.err.Error() }
func (e *fatalMigrationError) Unwrap() error { return e.err }

func fatalMigration(err error) error { return &fatalMigrationError{err: err} }

// acquireMigrationLock 用 MySQL 咨询锁（GET_LOCK）串行化迁移（P0-5）。
//
// 为什么需要：runMigrations 在每个 *SQLStore 构造时执行（控制面 + 各微服务 + 每租户 schema），
// 多副本/多服务并发启动时会同时读到「未应用」并同时执行同一 DDL。MySQL 的 DDL 不是事务性的，
// 并发 ADD COLUMN 的结果是一方 1060 失败——旧实现直接判定迁移失败（且失败还被静默降级）。
//
// 锁名含库名：同一 MySQL 实例可能承载多个 schema（MultiSchemaStore 的租户库），
// 不同库的迁移互不阻塞。MySQL 咨询锁是会话级的，故获取与释放必须在同一连接上。
// 返回的 release 幂等且自带超时（调用方 ctx 可能已取消/超时，锁仍须释放）。
func (s *SQLStore) acquireMigrationLock(ctx context.Context) (func(), error) {
	return s.acquireMigrationLockTimeout(ctx, migrationLockTimeoutSec)
}

// acquireMigrationLockTimeout 是 acquireMigrationLock 的显式超时版本（超时秒数作为参数，
// 便于测试用极短超时验证互斥性，无需等待生产用 60s）。
func (s *SQLStore) acquireMigrationLockTimeout(ctx context.Context, timeoutSec int) (func(), error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("get conn for migration lock: %w", err)
	}
	lockName := migrationLockName(s.databaseName(ctx, conn))
	var got sql.NullInt64
	if err := conn.QueryRowContext(ctx,
		`SELECT GET_LOCK(?, ?)`, lockName, timeoutSec).Scan(&got); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("acquire migration lock %s: %w", lockName, err)
	}
	// GET_LOCK 语义：1=获得，0=超时，NULL=出错。
	if !got.Valid || got.Int64 != 1 {
		_ = conn.Close()
		return nil, fmt.Errorf("acquire migration lock %s: 等待 %ds 未获得（另一实例正在迁移）", lockName, timeoutSec)
	}
	released := false
	release := func() {
		if released {
			return
		}
		released = true
		rctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.ExecContext(rctx, `SELECT RELEASE_LOCK(?)`, lockName); err != nil {
			log.Printf("[store] 释放迁移锁 %s 失败（连接关闭时由 MySQL 自动释放）: %v", lockName, err)
		}
		_ = conn.Close()
	}
	return release, nil
}

// migrationLockName 构造按库隔离的迁移锁名。
// MySQL 5.7+ 限制锁名 ≤64 字符：超长库名退化为 sha256 前缀（仍按库唯一，只是不可读）。
func migrationLockName(dbName string) string {
	name := "opsmesh_mig_" + dbName
	if len(name) > 64 {
		name = "opsmesh_mig_" + sha256Hex(dbName)[:16]
	}
	return name
}

// databaseName 返回当前连接选中的库名（用于构造按库隔离的迁移锁名）。
// 查询失败或未选库时返回 "unknown"（锁名退化为全局单锁，语义仍正确——只是跨库串行）。
func (s *SQLStore) databaseName(ctx context.Context, conn *sql.Conn) string {
	var name sql.NullString
	if err := conn.QueryRowContext(ctx, `SELECT DATABASE()`).Scan(&name); err != nil || !name.Valid || name.String == "" {
		return "unknown"
	}
	return name.String
}

// runMigrations 执行版本化 schema 迁移。
//
// 流程（P0-5 加固后）：
//  0. 获取 MySQL 咨询锁（GET_LOCK），串行化多副本/多服务对同一 schema 的迁移。
//  1. 确保 schema_migrations 表存在（用于记录已应用版本号）。
//  2. 读取已应用版本号集合。
//  3. 从 embed.FS 读取 migrations/*.sql，按版本号升序排序。
//     3.5 防篡改：已应用迁移的 checksum 必须与当前文件一致，不一致拒绝启动（fatal，不重试）。
//     3.6 版本门禁：库内已应用版本高于本二进制已知最高版本 → 拒绝启动（fatal，不重试），
//     防止旧二进制回滚到新 schema 后继续读写。
//  4. 对每个未应用的迁移：逐条执行 SQL（幂等重放）→ 记录 schema_migrations。
//  5. applyLegacyColumnFixups：兼容老库的增量补列/补索引（历史遗留，待后续转为正式迁移）。
//  6. seedRBAC：幂等预置默认权限/角色/用户；失败即返回错误（缺 admin 用户等于不可登录）。
//
// 幂等性（P0-5 核心）：MySQL 的 DDL 会隐式提交，「单文件事务回滚」对 DDL 不成立
// （见 applyMigration 注释）。因此安全性来自**可重放**而非回滚：迁移执行到一半失败
// （进程退出/连接中断）后，下次启动重放同一文件时，「对象已存在」类错误经
// information_schema 二次核实后放行（见 applyMigration / idempotentDDL）。
func (s *SQLStore) runMigrations() error {
	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(migrationLockTimeoutSec+migrationWorkBudgetSec)*time.Second)
	defer cancel()

	// 0. 串行化：并发迁移是「第一次带 replicas>1 的滚动升级」最常见的事故源。
	release, err := s.acquireMigrationLock(ctx)
	if err != nil {
		return err
	}
	defer release()

	// 1. 确保 schema_migrations 表存在（复用历史定义，sql.go 原 initSchema 已建此表）。
	//    G5 / ：增加 checksum 列记录迁移文件 sha256 摘要，启动时校验防篡改。
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INT PRIMARY KEY,
		applied_at DATETIME,
		checksum VARCHAR(64) NOT NULL DEFAULT ''
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	// 兼容老库：schema_migrations 已存在但缺 checksum 列时补列（G5 /）。
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

	// 3.5 G5 / 防篡改校验：已应用迁移的 checksum 必须与当前文件 sha256 一致，
	//     不一致则拒绝启动（避免迁移文件被静默篡改导致 schema 漂移）。
	//     这是确定性故障（文件被改动或版本不符），标记 fatal 立即返回，不做退避重试。
	for _, mf := range files {
		recorded, ok := applied[mf.version]
		if !ok {
			continue
		}
		expected := sha256Hex(mf.content)
		if recorded.checksum != "" && recorded.checksum != expected {
			return fatalMigration(fmt.Errorf("migration %d (%s) checksum mismatch: recorded=%s expected=%s (迁移文件已被篡改，拒绝启动)",
				mf.version, mf.name, recorded.checksum, expected))
		}
	}

	// 3.6 版本门禁（P0-5）：库中已应用的版本高于本二进制已知的最高版本，
	//     说明该库被更新版本迁移过（二进制回滚到旧版）。旧二进制不认识新列/新表语义，
	//     继续运行可能写坏数据；此处拒绝启动，要求按「先升二进制、再升 schema」的
	//     顺序人工处置（或恢复备份）。同为确定性故障 → fatal，不重试。
	if len(files) > 0 {
		maxKnown := files[len(files)-1].version
		for v := range applied {
			if v > maxKnown {
				return fatalMigration(fmt.Errorf(
					"schema 版本门禁：数据库已应用迁移版本 %d 高于本二进制已知最高版本 %d（库由更新版本迁移过，禁止旧二进制操作新 schema；请升级二进制或恢复备份）",
					v, maxKnown))
			}
		}
	}

	// 4. 逐个执行未应用的迁移（幂等重放，见 applyMigration）。
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
	// P0-5：seedRBAC 失败不再是「记日志继续」——预置角色/admin 用户缺失意味着
	// 无人能登录、RBAC 闸全拒，服务虽起但不可用；交由 initWithRetry 重试后失败即拒绝启动。
	if err := s.seedRBAC(ctx); err != nil {
		return fmt.Errorf("seedRBAC: %w", err)
	}
	return nil
}

// migrationRecord 描述一个已应用迁移的版本记录（含 checksum，G5 /）。
type migrationRecord struct {
	version  int
	checksum string
}

// appliedMigrations 返回 schema_migrations 表中已记录的版本号及其 checksum。
// G5 / ：返回 map[version] -> migrationRecord，供 runMigrations 校验防篡改。
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
// G5 / ：跳过 NNN_*.down.sql 回滚占位文件（仅作为未来回滚接口的占位，
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

// applyMigration 执行一个迁移文件的全部 SQL，并记录版本号与 checksum 到 schema_migrations。
//
// 为什么不用事务（P0-5）：MySQL 的 DDL 会**隐式提交**当前事务，BEGIN/ROLLBACK 对
// CREATE/ALTER 不构成原子边界——旧实现的「失败回滚整批」是假象：失败时已执行的 DDL
// 早已落库，回滚只能撤掉未执行的语句，反而让 schema_migrations 与真实 schema 不一致。
// 因此本函数改为「逐条执行 + 可重放」：
//   - 每条语句失败时，若为「对象已存在/已不存在」类错误（1050/1060/1061/1091），
//     先查 information_schema 二次核实目标状态确实已达成（而非对象定义错误等真故障），
//     是则记日志放行（重放语义），否则原样返回错误；
//   - 全部语句执行成功后，单独 INSERT 版本记录。若进程在 INSERT 前退出，下次启动
//     重放本文件——此时重复 DDL 走上面的幂等放行，最终收敛到一致状态。
//
// G5 / ：执行前计算迁移文件内容的 sha256 摘要，执行后随版本号一并存入
// schema_migrations.checksum 列，供后续启动时校验防篡改。
func (s *SQLStore) applyMigration(ctx context.Context, mf migrationFile) error {
	for _, stmt := range splitSQLStatements(mf.content) {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			handled, hErr := s.tolerateIdempotentDDLError(ctx, mf, stmt, err)
			if hErr != nil {
				return hErr
			}
			if !handled {
				return fmt.Errorf("exec stmt: %w", err)
			}
		}
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, applied_at, checksum) VALUES (?, ?, ?)`,
		mf.version, time.Now().UTC(), sha256Hex(mf.content)); err != nil {
		return fmt.Errorf("record migration: %w", err)
	}
	return nil
}

// tolerateIdempotentDDLError 判定一条失败的 DDL 语句是否属于「重放已达目标状态」。
//
// 返回 (handled, err)：
//   - handled=true, err=nil：已核实目标状态达成，可安全跳过（幂等重放）；
//   - handled=false, err=nil：非幂等类错误（交由调用方原样返回）；
//   - err!=nil：核实过程本身失败（或目标状态与预期不符），按致命错误返回。
//
// 严谨性：不盲目吞掉 1060/1061/1050/1091——必须解析出语句的目标对象并用
// information_schema 确认它确实存在（ADD/CREATE）或确实不存在（DROP）。
// 例如 ALTER TABLE ADD COLUMN 报 1060 但列不存在（不该发生）时返回错误而不是放行。
func (s *SQLStore) tolerateIdempotentDDLError(ctx context.Context, mf migrationFile, stmt string, err error) (bool, error) {
	var me *mysqlDriver.MySQLError
	if !errors.As(err, &me) {
		return false, nil
	}
	if !idempotentDDLErrorCodes[me.Number] {
		return false, nil
	}
	target, ok := parseIdempotentDDL(stmt)
	if !ok {
		// 语句形态不认识（如复合 ALTER）→ 不吞错，避免把真故障当幂等放行。
		return false, nil
	}
	exists, vErr := s.ddlTargetExists(ctx, target)
	if vErr != nil {
		return true, fmt.Errorf("核实幂等 DDL 目标失败（迁移 %d %s, %v）: %w", mf.version, mf.name, err, vErr)
	}
	if exists != target.expectExists {
		return true, fmt.Errorf("迁移 %d (%s) 语句报 %v，但目标 %s %s.%s 状态与预期不符（存在=%v 期望=%v）：拒绝按幂等放行",
			mf.version, mf.name, err, target.kind, target.table, target.name, exists, target.expectExists)
	}
	log.Printf("[store] 迁移 %d (%s) 幂等放行（%s %s.%s 已处于目标状态）: %v",
		mf.version, mf.name, target.kind, target.table, target.name, err)
	return true, nil
}

// ddlTargetKind 幂等 DDL 校验的目标对象类型。
type ddlTargetKind string

const (
	ddlColumn ddlTargetKind = "column"
	ddlIndex  ddlTargetKind = "index"
	ddlTable  ddlTargetKind = "table"
)

// idempotentDDLErrorCodes 可经二次核实后放行的 MySQL 错误码。
//
//	1050 ER_TABLE_EXISTS_ERROR  表已存在
//	1060 ER_DUP_FIELDNAME       列已存在
//	1061 ER_DUP_KEYNAME         索引/键名已存在
//	1091 ER_CANT_DROP_FIELD_OR_KEY 待删除的列/键不存在（DROP 已达目标状态）
var idempotentDDLErrorCodes = map[uint16]bool{1050: true, 1060: true, 1061: true, 1091: true}

// idempotentDDL 描述一条 DDL 的目标对象与执行后的期望存在性，供幂等核实使用。
type idempotentDDL struct {
	kind         ddlTargetKind
	table        string
	name         string
	expectExists bool
}

// ddlAddNonColumnKeywords ALTER TABLE ... ADD <关键字> —— 说明 ADD 的不是列，
// 本函数不解析（返回 false，不吞错），交由后续按需扩展。
var ddlAddNonColumnKeywords = map[string]bool{
	"INDEX": true, "UNIQUE": true, "KEY": true, "PRIMARY": true,
	"FOREIGN": true, "CONSTRAINT": true, "CHECK": true, "PARTITION": true,
}

// parseIdempotentDDL 从 DDL 语句解析目标对象（仅覆盖本仓迁移实际使用的形态）：
//
//	CREATE TABLE [IF NOT EXISTS] t ...      → table t（期望存在）
//	ALTER TABLE t ADD [COLUMN] c ...        → column t.c（期望存在）
//	ALTER TABLE t DROP [COLUMN] c           → column t.c（期望不存在）
//	ALTER TABLE t DROP INDEX|KEY i          → index t.i（期望不存在）
//	CREATE [UNIQUE] INDEX i ON t ...        → index t.i（期望存在）
//
// 无法识别的形态返回 false（调用方按原错误处理，不吞错）。
func parseIdempotentDDL(stmt string) (idempotentDDL, bool) {
	toks := ddlTokens(stmt)
	at := func(i int) string {
		if i >= len(toks) {
			return ""
		}
		return strings.ToUpper(toks[i])
	}
	switch {
	case at(0) == "ALTER" && at(1) == "TABLE" && len(toks) >= 4:
		table := toks[2]
		switch at(3) {
		case "ADD":
			if at(4) == "COLUMN" {
				if len(toks) < 6 {
					return idempotentDDL{}, false
				}
				return idempotentDDL{kind: ddlColumn, table: table, name: toks[5], expectExists: true}, true
			}
			if len(toks) < 5 || ddlAddNonColumnKeywords[at(4)] {
				return idempotentDDL{}, false
			}
			return idempotentDDL{kind: ddlColumn, table: table, name: toks[4], expectExists: true}, true
		case "DROP":
			if at(4) == "COLUMN" {
				if len(toks) < 6 {
					return idempotentDDL{}, false
				}
				return idempotentDDL{kind: ddlColumn, table: table, name: toks[5], expectExists: false}, true
			}
			if at(4) == "INDEX" || at(4) == "KEY" {
				if len(toks) < 6 {
					return idempotentDDL{}, false
				}
				return idempotentDDL{kind: ddlIndex, table: table, name: toks[5], expectExists: false}, true
			}
			return idempotentDDL{}, false
		}
		return idempotentDDL{}, false
	case at(0) == "CREATE" && at(1) == "TABLE":
		i := 2
		if at(i) == "IF" && at(i+1) == "NOT" && at(i+2) == "EXISTS" {
			i += 3
		}
		if i >= len(toks) {
			return idempotentDDL{}, false
		}
		return idempotentDDL{kind: ddlTable, table: toks[i], expectExists: true}, true
	case at(0) == "CREATE" && (at(1) == "INDEX" || (at(1) == "UNIQUE" && at(2) == "INDEX")):
		i := 1
		if at(1) == "UNIQUE" {
			i = 2
		}
		// CREATE [UNIQUE] INDEX <name> ON <table> ...
		if len(toks) < i+4 || at(i+2) != "ON" {
			return idempotentDDL{}, false
		}
		return idempotentDDL{kind: ddlIndex, table: toks[i+3], name: toks[i+1], expectExists: true}, true
	}
	return idempotentDDL{}, false
}

// ddlTokens 把语句切成去反引号/去分号的 token 列表（大小写不敏感比较在调用侧做）。
func ddlTokens(stmt string) []string {
	raw := strings.Fields(stmt)
	out := make([]string, 0, len(raw))
	for _, t := range raw {
		t = strings.Trim(t, "`;")
		if t == "" {
			continue
		}
		out = append(out, t)
	}
	return out
}

// ddlTargetExists 用 information_schema 核实目标对象当前是否存在。
func (s *SQLStore) ddlTargetExists(ctx context.Context, t idempotentDDL) (bool, error) {
	const (
		qColumn = `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name=? AND column_name=?`
		qIndex  = `SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema=DATABASE() AND table_name=? AND index_name=?`
		qTable  = `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=?`
	)
	var (
		cnt int
		err error
	)
	switch t.kind {
	case ddlColumn:
		err = s.db.QueryRowContext(ctx, qColumn, t.table, t.name).Scan(&cnt)
	case ddlIndex:
		err = s.db.QueryRowContext(ctx, qIndex, t.table, t.name).Scan(&cnt)
	case ddlTable:
		err = s.db.QueryRowContext(ctx, qTable, t.table).Scan(&cnt)
	default:
		return false, fmt.Errorf("unknown ddl target kind %q", t.kind)
	}
	if err != nil {
		return false, err
	}
	return cnt > 0, nil
}

// sha256Hex 返回给定内容的 sha256 摘要的十六进制小写表示（64 字符）。
// 用于 schema_migrations.checksum 列记录迁移文件指纹，启动时校验防篡改（G5 /）。
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
	// 后续列迁移（F2/F3/F4/F5/B1/新增字段）统一走 alterColumnIfMissing，避免破坏已存在库。
	s.alterColumnIfMissing(ctx, "devices", "managed", "BOOLEAN DEFAULT 0")
	s.alterColumnIfMissing(ctx, "devices", "last_result", "VARCHAR(16)")
	s.alterColumnIfMissing(ctx, "devices", "last_result_at", "DATETIME")
	s.alterColumnIfMissing(ctx, "devices", "retired", "BOOLEAN DEFAULT 0")
	// 设备基础元信息列（agent 注册时上报，设备列表/详情展示用）。
	s.alterColumnIfMissing(ctx, "devices", "hostname", "VARCHAR(255)")
	s.alterColumnIfMissing(ctx, "devices", "os", "VARCHAR(32)")
	s.alterColumnIfMissing(ctx, "devices", "arch", "VARCHAR(32)")
	// 安全债：users 表增加 must_change_password 列，预置弱口令首登强制改密。
	s.alterColumnIfMissing(ctx, "users", "must_change_password", "BOOLEAN DEFAULT 0")
	// gRPC agent 身份绑定：agents 表增加 secret 列存储 HMAC 签名密钥。
	s.alterColumnIfMissing(ctx, "agents", "secret", "VARCHAR(64)")
	s.alterColumnIfMissing(ctx, "tasks", "retry_count", "INT DEFAULT 0")
	s.alterColumnIfMissing(ctx, "tasks", "max_retries", "INT DEFAULT 0")
	s.alterColumnIfMissing(ctx, "tasks", "dead_letter", "BOOLEAN DEFAULT 0")
	// 节点级超时与重试：tasks 表增加 timeout / retry_delay 列。
	s.alterColumnIfMissing(ctx, "tasks", "timeout", "INT DEFAULT 0")
	s.alterColumnIfMissing(ctx, "tasks", "retry_delay", "INT DEFAULT 0")
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
	// 租户隔离：存量 k8s_clusters 表补 tenant_id 列（全新库由 CREATE TABLE 保证）。
	s.alterColumnIfMissing(ctx, "k8s_clusters", "tenant_id", "VARCHAR(64)")
	// M2 告警治理：alert_rules 表补 created_by 列（全新库由 005_m2_alert_governance.sql 保证）。
	// 兼容老库（MySQL < 8.0 不支持 ADD COLUMN IF NOT EXISTS，005 迁移可能失败）。
	s.alterColumnIfMissing(ctx, "alert_rules", "created_by", "VARCHAR(64)")
	// /111 增量补列/补索引（详见 sql_legacy.go initSchemaExtra）。
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
