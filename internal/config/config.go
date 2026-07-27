// Package config 提供统一配置：命令行 flag 优先，环境变量兜底。
// U-05: 控制面与 agent 共用同一份配置结构，通过 --mode 切换角色。
package config

import (
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"
)

// Config 启动时解析出的运行参数。地址/端口全部走 flag，不硬编码任何密钥。
type Config struct {
	Mode        string // controlplane | agent
	Addr        string // agent 自身地址（占位，供控制面感知）
	ControlAddr string // 控制面 HTTP 地址（agent 发起注册/心跳/拉任务用；gRPC 端口固定 9090）；单地址兼容写法
	// A3 多控制面 failover：逗号分隔的多个控制面地址，agent 依次重试（HA 真多副本前置）。
	// 为空时回退使用 ControlAddr 单地址（向后兼容）。
	ControlAddrs string
	Segment     string // agent 所属网段（U-02 分桶键）
	HTTPPort    int    // 控制面 HTTP(B/S) 端口（约定 8080）
	GRPCPort    int    // gRPC 端口（约定 9090，真实 gRPC 注册通道）
	MetricsPort int    // metrics 端口（约定 9091）
	// U-04 数据本地化：持久化后端选择。
	Store     string // 持久化后端: memory（默认） | mysql
	MySQLDSN  string // MySQL DSN（--store=mysql 时生效），如 user:pass@tcp(host:3306)/ops_device
	RedisAddr   string // Redis 地址（--store=mysql 时作 agent/device 状态缓存），如 redis:6379
	RequireAuth bool   // 生产模式：要求网关注入租户头，缺失则拒绝（MVP 默认 false=开发降级）
	// 运行健壮性
	TaskTimeout    time.Duration // agent 单任务执行超时（P0-3，默认 120s）
	ShutdownTimeout time.Duration // 收到 SIGTERM 后的优雅退出窗口（P0-3，默认 15s）
	// gRPC TLS / mTLS（P1-6，默认空=关闭，仅内网友好网用；等保生产建议开启）
	TLSCert  string // 服务端证书 / 客户端证书 文件路径
	TLSKey   string // 私钥 文件路径
	ClientCA string // 服务端要求客户端 CA（mTLS）；或客户端校验服务端 CA

	// 真实网段发现（P0-2）。默认关闭：采用“agent 即设备”的 MVP 降级纳管；
	// 开启后控制面按 SegmentCIDR 做存活扫描，为每台存活主机创建真实 DeviceInfo。
	Discover    bool   // 是否开启真实网段发现
	SegmentCIDR string // 待扫描网段（如 10.30.0.0/24）
	// B1 自动纳管：discover 扫描到存活主机后，自动登记候选设备并（配置 SSH 私钥时）推送 agent。
	AutoProvision bool // 是否在 discover 基础上自动纳管（需 --provision-ssh-key）

	// agent 进程资源限额（P0-3），0 表示不设置。
	MaxProcs    int   // RLIMIT_NPROC：最大进程/线程数
	MaxFiles    int   // RLIMIT_NOFILE：最大打开文件数
	MaxMemoryMB int64 // RLIMIT_AS：最大虚拟内存（MB）

	// agent 任务 worker 池并发度（P1-3），默认 4。
	WorkerConcurrency int

	// 事件总线（P1-5）：noop（默认）| log | kafka（-tags kafka 构建下生效）。
	EventBus     string // 事件总线类型
	KafkaBrokers string // Kafka brokers（env OPSMESH_KAFKA_BROKERS）
	KafkaTopic   string // Kafka topic（env OPSMESH_KAFKA_TOPIC）

	// agent 身份持久化（P0-2）：agent.id 落盘目录；空=内存生成（重启即新 ID，旧任务成孤儿）。
	DataDir string
	// 演示模式（P0-5）：开启时每个 agent 注册预置一条 uname -a 示例任务；生产默认关闭，避免污染。
	Demo bool
	// B1 自动纳管闭环：agent 经 bootstrap 安装时携带的一次性 install token。
	// 由控制面 Provision 签发（HMAC 签名、一次性、限时），agent 注册时回传以完成自动纳管闭环。
	InstallToken string
	// 任务租约租期秒（P0-1）：agent 领取任务后超过该时长未上报结果，视为失联并复位 pending 重新调度（任务必达）。
	TaskLeaseSec int

	// A3 控制面副本数（HA）：由部署平台注入（如 K8s replicas）。>1 时必须用 mysql store，
	// 否则 memory store 多副本数据分裂（默认 1，单机/开发）。
	Replicas int
	// A4 生产模式：开启后默认 require-auth=true 且对 store=memory 强告警（生产基线）。
	Production bool
	// F2 任务失败重试上限：SubmitResult 失败时按策略重入队，达上限置 failed（死信）。
	TaskMaxRetries int
	// A3 选主租约秒：本实例持有 leader 身份的时长；到期前需续租，否则被其他副本抢占。
	LeaderTTLSec int
	// A3 选主续租周期秒：leaderLoop 续租频率（应小于 LeaderTTLSec，建议 1/3）。
	LeaderTickSec int
	// F5 离线超龄自动归档阈值（分钟）：agent 最后心跳早于该时长的设备自动 retired。
	// <=0 表示关闭自动归档（仅手动 DELETE 退役）。
	ArchiveAgeMin int
	// B1 自动纳管：install token 的 HMAC 签名密钥（一次性、限时）。
	// 多副本共享同一 MySQL 时需一致（否则互不相认）；空则本实例随机生成（单实例 MVP）。
	ProvisionSecret string
	// B1 自动纳管：控制面对外可达的 HTTP 地址，用于拼接 bootstrap 安装命令。
	// 安全（P1-F4）：bootstrap 绝不能用请求方可控的 r.Host（Host 头注入→供应链 RCE），
	// 必须由运维显式配置可信地址。空则回退 http://127.0.0.1:<http-port>（仅本机开发）。
	AdvertiseAddr string
	// M7 监控告警：告警 Webhook 推送 URL（POST JSON）。
	// 空=不推送；非空时 critical 告警通过 HTTP POST 推送到此地址。
	AlertWebhookURL string
	// M7 告警通知类型：generic（默认，直接 POST Alert JSON）/ feishu（飞书卡片）/ dingtalk（钉钉 markdown）。
	// 仅当 AlertWebhookURL 非空时生效。
	AlertNotifierType string
	// B1 自动纳管推送：SSH 配置（空=关闭 SSH 自动推送，仅返回 bootstrap 文本）。
	// 推送时在候选设备上通过 SSH 执行 bootstrap 命令，自动安装 agent。
	ProvisionSSHUser string // SSH 用户（默认 "root"）
	ProvisionSSHKey  string // SSH 私钥路径（如 /etc/opsmesh/ssh/id_rsa）
	ProvisionSSHKP   string // SSH 密钥密码（可选；env OPSMESH_PROVISION_SSH_KEY_PASS 更安全）
	// B1 SSH KnownHosts 文件路径（等保安全加固）。非空时使用 ssh.KnownHosts 校验远程主机指纹；
	// 空（默认）时回退 InsecureIgnoreHostKey（MVP 便利，生产必须配置）。
	ProvisionSSHKnownHosts string
}

// Load 解析 flag 并用环境变量兜底，返回 *Config。
func Load() *Config {
	mode := flag.String("mode", "controlplane", "运行模式: controlplane | agent")
	addr := flag.String("addr", "127.0.0.1", "agent 自身地址（占位）")
	controlAddr := flag.String("control-addr", "http://127.0.0.1:8080", "控制面 HTTP 地址（agent 用）；单地址兼容写法")
	controlAddrs := flag.String("control-addrs", "", "A3 多控制面地址（逗号分隔，如 cp1:9090,cp2:9090）；agent 依次重试实现 HA failover；空则回退 --control-addr")
	segment := flag.String("segment", "default", "agent 所属网段")
	httpPort := flag.Int("http-port", 8080, "控制面 HTTP(B/S) 端口（约定 8080）")
	grpcPort := flag.Int("grpc-port", 9090, "gRPC 端口（约定 9090）")
	metricsPort := flag.Int("metrics-port", 9091, "metrics 端口（约定 9091）")
	// U-04 数据本地化：持久化后端选择（默认 memory，保证无外部依赖即可运行）。
	store := flag.String("store", "memory", "持久化后端: memory（默认） | mysql（U-04 数据本地化）")
	mysqlDSN := flag.String("mysql-dsn", "", "MySQL DSN（--store=mysql 时生效），如 user:pass@tcp(mysql:3306)/ops_device")
	redisAddr := flag.String("redis-addr", "", "Redis 地址（--store=mysql 时作状态缓存），如 redis:6379")
	requireAuth := flag.Bool("require-auth", false, "要求网关注入 X-Tenant-ID，缺失则拒绝（生产 hardening）")
	taskTimeout := flag.Duration("task-timeout", 120*time.Second, "agent 单任务执行超时（P0-3）")
	shutdownTimeout := flag.Duration("shutdown-timeout", 15*time.Second, "SIGTERM 优雅退出窗口（P0-3）")
	tlsCert := flag.String("tls-cert", "", "gRPC TLS 证书路径（P1-6；空=关闭）")
	tlsKey := flag.String("tls-key", "", "gRPC TLS 私钥路径")
	clientCA := flag.String("client-ca", "", "服务端要求客户端 CA（mTLS）/ 客户端校验服务端 CA")
	discover := flag.Bool("discover", false, "开启真实网段发现（P0-2）；关闭时采用 agent 即设备的 MVP 降级纳管")
	segmentCIDR := flag.String("segment-cidr", "", "待扫描网段（P0-2，如 10.30.0.0/24）；开启 --discover 时生效")
	autoProvision := flag.Bool("auto-provision", false, "B1 自动纳管：discover 扫描到存活主机后自动登记候选设备并（配置 --provision-ssh-key 时）推送 agent")
	maxProcs := flag.Int("max-procs", 0, "agent RLIMIT_NPROC 上限（P0-3；0=不限制）")
	maxFiles := flag.Int("max-files", 0, "agent RLIMIT_NOFILE 上限（P0-3；0=不限制）")
	maxMemoryMB := flag.Int64("max-memory-mb", 0, "agent RLIMIT_AS 上限 MB（P0-3；0=不限制）")
	workerConcurrency := flag.Int("worker-concurrency", 4, "agent 任务 worker 池并发度（P1-3）")
	eventBus := flag.String("event-bus", "noop", "事件总线类型（P1-5）：noop | log | kafka")
	kafkaBrokers := flag.String("kafka-brokers", "", "Kafka brokers（P1-5；或 env OPSMESH_KAFKA_BROKERS）")
	kafkaTopic := flag.String("kafka-topic", "", "Kafka topic（P1-5；或 env OPSMESH_KAFKA_TOPIC）")
	dataDir := flag.String("data-dir", "./data", "agent 身份文件目录（P0-2）；agent.id 落盘于此，重启沿用")
	demo := flag.Bool("demo", false, "演示模式（P0-5）：每个 agent 注册预置 uname -a 示例任务（生产务必关闭）")
	installToken := flag.String("install-token", "", "B1 自动纳管：agent 经 bootstrap 安装时携带的一次性 install token（空=无令牌闭环）")
	taskLeaseSec := flag.Int("task-lease-sec", 300, "任务租约租期秒（P0-1）；超期未上报结果则复位重调度")
	replicas := flag.Int("replicas", 1, "控制面副本数（A3 由部署平台注入）；>1 须用 --store=mysql，否则 memory 多副本分裂")
	production := flag.Bool("production", false, "生产模式（A4）：默认开启 require-auth，并对 store=memory 强告警")
	taskMaxRetries := flag.Int("task-max-retries", 3, "任务失败重试上限（F2）；超出置 failed（死信），需人工处置")
	leaderTTLSec := flag.Int("leader-ttl-sec", 15, "A3 选主租约秒；本实例持有 leader 身份的时长，到期前需续租")
	leaderTickSec := flag.Int("leader-tick-sec", 5, "A3 选主续租周期秒；leaderLoop 续租频率（应小于 leader-ttl-sec）")
	archiveAgeMin := flag.Int("archive-age-min", 1440, "F5 离线超龄自动归档阈值（分钟）；agent 最后心跳早于该时长的设备自动 retired（<=0 关闭）")
	provisionSecret := flag.String("provision-secret", "", "B1 自动纳管 install token 的 HMAC 签名密钥；空则本实例随机生成（多副本需一致）")
	advertiseAddr := flag.String("advertise-addr", "", "B1 自动纳管控制面对外 HTTP 地址（拼接 bootstrap 安装命令）；空则回退 127.0.0.1:<http-port>（仅本机开发）")
	alertWebhookURL := flag.String("alert-webhook-url", "", "M7 告警 Webhook 推送 URL（POST JSON critical 告警到此地址）；空=不推送")
	alertNotifierType := flag.String("alert-notifier-type", "generic", "M7 告警通知类型：generic(直接POST Alert JSON)/feishu(飞书卡片)/dingtalk(钉钉markdown)")
	provisionSSHUser := flag.String("provision-ssh-user", "root", "B1 SSH 自动推送：SSH 用户")
	provisionSSHKey := flag.String("provision-ssh-key", "", "B1 SSH 自动推送：SSH 私钥路径（空=关闭 SSH 推送）")
	provisionSSHKP := flag.String("provision-ssh-key-pass", "", "B1 SSH 自动推送：SSH 密钥密码（推荐 OPSMESH_PROVISION_SSH_KEY_PASS 环境变量）")
	provisionSSHKnownHosts := flag.String("provision-ssh-known-hosts", "", "B1 SSH KnownHosts 文件路径（等保加固）；空=InsecureIgnoreHostKey（生产务必配置）")
	flag.Parse()

	// 记录被显式设置的 flag，用于"flag 优先、env 兜底"的正确语义（P1-8 修复：原实现 env 会覆盖显式 flag）。
	explicit := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	val := func(name, fv, envKey string) string {
		if explicit[name] {
			return fv
		}
		if v, ok := os.LookupEnv(envKey); ok && v != "" {
			return v
		}
		return fv
	}
	valInt := func(name string, fv int, envKey string) int {
		if explicit[name] {
			return fv
		}
		return envInt(envKey, fv)
	}
	valInt64 := func(name string, fv int64, envKey string) int64 {
		if explicit[name] {
			return fv
		}
		return envInt64(envKey, fv)
	}
	valBool := func(name string, fv bool, envKey string) bool {
		if explicit[name] {
			return fv
		}
		return boolEnv(envKey, fv)
	}
	valDur := func(name string, fv time.Duration, envKey string) time.Duration {
		if explicit[name] {
			return fv
		}
		return durationEnv(envKey, fv)
	}

	cfg := &Config{
		Mode:        val("mode", *mode, "OPSMESH_MODE"),
		Addr:        val("addr", *addr, "OPSMESH_ADDR"),
		ControlAddr: val("control-addr", *controlAddr, "OPSMESH_CONTROL_ADDR"),
		ControlAddrs: val("control-addrs", *controlAddrs, "OPSMESH_CONTROL_ADDRS"),
		Segment:     val("segment", *segment, "OPSMESH_SEGMENT"),
		HTTPPort:    valInt("http-port", *httpPort, "OPSMESH_HTTP_PORT"),
		GRPCPort:    valInt("grpc-port", *grpcPort, "OPSMESH_GRPC_PORT"),
		MetricsPort: valInt("metrics-port", *metricsPort, "OPSMESH_METRICS_PORT"),
		Store:       val("store", *store, "OPSMESH_STORE"),
		MySQLDSN:    val("mysql-dsn", *mysqlDSN, "OPSMESH_MYSQL_DSN"),
		RedisAddr:      val("redis-addr", *redisAddr, "OPSMESH_REDIS_ADDR"),
		RequireAuth:    valBool("require-auth", *requireAuth, "OPSMESH_REQUIRE_AUTH"),
		TaskTimeout:    valDur("task-timeout", *taskTimeout, "OPSMESH_TASK_TIMEOUT"),
		ShutdownTimeout: valDur("shutdown-timeout", *shutdownTimeout, "OPSMESH_SHUTDOWN_TIMEOUT"),
		TLSCert:        val("tls-cert", *tlsCert, "OPSMESH_TLS_CERT"),
		TLSKey:         val("tls-key", *tlsKey, "OPSMESH_TLS_KEY"),
		ClientCA:       val("client-ca", *clientCA, "OPSMESH_CLIENT_CA"),
		Discover:        valBool("discover", *discover, "OPSMESH_DISCOVER"),
		SegmentCIDR:     val("segment-cidr", *segmentCIDR, "OPSMESH_SEGMENT_CIDR"),
		AutoProvision:   valBool("auto-provision", *autoProvision, "OPSMESH_AUTO_PROVISION"),
		MaxProcs:        valInt("max-procs", *maxProcs, "OPSMESH_MAX_PROCS"),
		MaxFiles:        valInt("max-files", *maxFiles, "OPSMESH_MAX_FILES"),
		MaxMemoryMB:     valInt64("max-memory-mb", *maxMemoryMB, "OPSMESH_MAX_MEMORY_MB"),
		WorkerConcurrency: valInt("worker-concurrency", *workerConcurrency, "OPSMESH_WORKER_CONCURRENCY"),
		EventBus:        val("event-bus", *eventBus, "OPSMESH_EVENT_BUS"),
		KafkaBrokers:    val("kafka-brokers", *kafkaBrokers, "OPSMESH_KAFKA_BROKERS"),
		KafkaTopic:      val("kafka-topic", *kafkaTopic, "OPSMESH_KAFKA_TOPIC"),
		DataDir:       val("data-dir", *dataDir, "OPSMESH_DATA_DIR"),
		Demo:          valBool("demo", *demo, "OPSMESH_DEMO"),
		InstallToken:  val("install-token", *installToken, "OPSMESH_INSTALL_TOKEN"),
		TaskLeaseSec:  valInt("task-lease-sec", *taskLeaseSec, "OPSMESH_TASK_LEASE_SEC"),
		Replicas:      valInt("replicas", *replicas, "OPSMESH_REPLICAS"),
		Production:    valBool("production", *production, "OPSMESH_PRODUCTION"),
		TaskMaxRetries: valInt("task-max-retries", *taskMaxRetries, "OPSMESH_TASK_MAX_RETRIES"),
		LeaderTTLSec:   valInt("leader-ttl-sec", *leaderTTLSec, "OPSMESH_LEADER_TTL_SEC"),
		LeaderTickSec:  valInt("leader-tick-sec", *leaderTickSec, "OPSMESH_LEADER_TICK_SEC"),
		ArchiveAgeMin:  valInt("archive-age-min", *archiveAgeMin, "OPSMESH_ARCHIVE_AGE_MIN"),
		ProvisionSecret: val("provision-secret", *provisionSecret, "OPSMESH_PROVISION_SECRET"),
		AdvertiseAddr:   val("advertise-addr", *advertiseAddr, "OPSMESH_ADVERTISE_ADDR"),
		AlertWebhookURL:   val("alert-webhook-url", *alertWebhookURL, "OPSMESH_ALERT_WEBHOOK_URL"),
		AlertNotifierType: val("alert-notifier-type", *alertNotifierType, "OPSMESH_ALERT_NOTIFIER_TYPE"),
		ProvisionSSHUser: val("provision-ssh-user", *provisionSSHUser, "OPSMESH_PROVISION_SSH_USER"),
		ProvisionSSHKey:  val("provision-ssh-key", *provisionSSHKey, "OPSMESH_PROVISION_SSH_KEY"),
		ProvisionSSHKP:   val("provision-ssh-key-pass", *provisionSSHKP, "OPSMESH_PROVISION_SSH_KEY_PASS"),
		ProvisionSSHKnownHosts: val("provision-ssh-known-hosts", *provisionSSHKnownHosts, "OPSMESH_PROVISION_SSH_KNOWN_HOSTS"),
	}
	// A4 生产模式：默认开启 require-auth（除非显式关闭），并强告警 memory store。
	if cfg.Production && !explicit["require-auth"] {
		cfg.RequireAuth = true
	}
	if cfg.Production && cfg.Store == "memory" {
		fmt.Fprintln(os.Stderr, "[config] 警告：生产模式但 store=memory（多副本数据分裂），请改用 --store=mysql（U-04 数据本地化）")
	}
	if cfg.Production && cfg.TLSCert == "" && cfg.TLSKey == "" {
		fmt.Fprintln(os.Stderr, "[config] 警告：生产模式但 TLS 未配置（--tls-cert, --tls-key），agent 与控制面之间通信为明文（等保三级生产建议开启 mTLS）")
	}
	if cfg.Production && !cfg.RequireAuth {
		fmt.Fprintln(os.Stderr, "[config] 警告：生产模式但 --require-auth=false，agent 注册不经过网关身份校验（仅开发/内网调试推荐）")
	}
	return cfg
}

// envInt64 解析 int64 环境变量，未设置或非法时返回默认。
func envInt64(key string, def int64) int64 {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// boolEnv 解析布尔环境变量（"true"/"1" 为真），未设置或非法时返回默认。
func boolEnv(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v == "true" || v == "1"
	}
	return def
}

// durationEnv 解析时长环境变量（如 "120s"），未设置或非法时返回默认。
func durationEnv(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

// Validate 启动期配置校验：把明显的非法配置在启动即失败，而非运行期诡异出错（P0-3 健壮性）。
// 返回 error 时调用方应 os.Exit(1)。
func (c *Config) Validate() error {
	switch c.Mode {
	case "controlplane", "agent":
	default:
		return fmt.Errorf("非法 --mode=%q（应为 controlplane 或 agent）", c.Mode)
	}
	// 端口范围基本校验（1-65535）。
	for _, p := range []struct {
		name string
		val  int
	}{
		{"http-port", c.HTTPPort}, {"grpc-port", c.GRPCPort}, {"metrics-port", c.MetricsPort},
	} {
		if p.val <= 0 || p.val > 65535 {
			return fmt.Errorf("非法 %s=%d（应在 1-65535）", p.name, p.val)
		}
	}
	if c.Mode == "agent" {
		if c.WorkerConcurrency <= 0 {
			return fmt.Errorf("非法 --worker-concurrency=%d（应 > 0）", c.WorkerConcurrency)
		}
		if c.TaskTimeout <= 0 {
			return fmt.Errorf("非法 --task-timeout=%v（应 > 0）", c.TaskTimeout)
		}
	}
	if c.Store == "mysql" && c.MySQLDSN == "" {
		return fmt.Errorf("--store=mysql 但 --mysql-dsn 为空（U-04 数据本地化需要 DSN）")
	}
	if c.TaskLeaseSec <= 0 {
		return fmt.Errorf("非法 --task-lease-sec=%d（应 > 0）", c.TaskLeaseSec)
	}
	// A3 控制面 HA：memory store 多副本数据分裂，>1 副本必须改用 mysql store（U-04 数据本地化）。
	if c.Store == "memory" && c.Replicas > 1 {
		return fmt.Errorf("store=memory 不支持多副本（replicas=%d）；请改用 --store=mysql（U-04 数据本地化）", c.Replicas)
	}
	if c.Discover {
		if c.SegmentCIDR == "" {
			return fmt.Errorf("--discover 开启但 --segment-cidr 为空（真实网段发现需要 CIDR）")
		}
		if _, _, err := net.ParseCIDR(c.SegmentCIDR); err != nil {
			return fmt.Errorf("非法 --segment-cidr=%q: %w", c.SegmentCIDR, err)
		}
	}
	return nil
}
