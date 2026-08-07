// Package config 提供统一配置：命令行 flag 优先，环境变量兜底。
// U-05: 控制面与 agent 共用同一份配置结构，通过 --mode 切换角色。
package config

import (
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
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
	Segment      string // agent 所属网段（U-02 分桶键）
	HTTPPort     int    // 控制面 HTTP(B/S) 端口（约定 8080）
	GRPCPort     int    // gRPC 端口（约定 9090，真实 gRPC 注册通道）
	MetricsPort  int    // metrics 端口（约定 9091）
	// U-04 数据本地化：持久化后端选择。
	Store       string // 持久化后端: memory（默认） | mysql
	MySQLDSN    string // MySQL DSN（--store=mysql 时生效），如 user:pass@tcp(host:3306)/ops_device
	RedisAddr   string // Redis 地址（--store=mysql 时作 agent/device 状态缓存），如 redis:6379
	RequireAuth bool   // 生产模式：要求网关注入租户头，缺失则拒绝（MVP 默认 false=开发降级）
	// 运行健壮性
	TaskTimeout     time.Duration // agent 单任务执行超时（P0-3，默认 120s）
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
	// 注册安全（P1-7）：是否允许公开注册。
	// true（默认，向后兼容）=允许 /api/v1/auth/register 公开调用，但新用户 Status="pending" 须管理员审批；
	// false=关闭公开注册接口（仅管理员可通过 POST /api/v1/users 创建用户）。
	// demo 模式下保持 true 且新用户 Status="active"（方便演示，无需审批）。
	PublicRegister bool
	// 注册安全（P1-7）：是否允许公开注册免审批（Status=active + 立即签发 token）。
	// false（默认，安全基线）=所有注册（包括 demo 模式）都走 pending 审批流程，须管理员激活后方可登录；
	// true=注册即激活并立即签发 token（仅演示/内网受信环境使用，生产务必关闭）。
	// 与 PublicRegister 解耦：PublicRegister 控制接口是否开放，AllowPublicRegister 控制是否免审批。
	AllowPublicRegister bool
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
	// 仅当 AlertWebhookURL 非空时生效。B7 扩展：Webhook URL 含 slack.com 自动走 Slack Block Kit，
	// 含 qyapi.weixin.qq.com 自动走企业微信 markdown（此时 AlertNotifierType 被忽略）。
	AlertNotifierType string
	// B7 告警邮件通道：SMTP 配置。Host/Port/From/To 任一为空视为关闭邮件通道（跳过发送）。
	AlertEmailHost string // SMTP 服务器地址（如 smtp.example.com）
	AlertEmailPort int    // SMTP 端口（如 25/465/587）
	AlertEmailUser string // SMTP 用户名（空=匿名发送）
	AlertEmailPass string // SMTP 密码（推荐 env OPSMESH_ALERT_EMAIL_PASS）
	AlertEmailFrom string // 发件人地址
	AlertEmailTo   string // 收件人列表（逗号分隔）
	// B1 自动纳管推送：SSH 配置（空=关闭 SSH 自动推送，仅返回 bootstrap 文本）。
	// 推送时在候选设备上通过 SSH 执行 bootstrap 命令，自动安装 agent。
	ProvisionSSHUser string // SSH 用户（默认 "root"）
	ProvisionSSHKey  string // SSH 私钥路径（如 /etc/opsmesh/ssh/id_rsa）
	ProvisionSSHKP   string // SSH 密钥密码（可选；env OPSMESH_PROVISION_SSH_KEY_PASS 更安全）
	// B1 SSH KnownHosts 文件路径（等保安全加固）。非空时使用 ssh.KnownHosts 校验远程主机指纹；
	// 空（默认）时回退 InsecureIgnoreHostKey（MVP 便利，生产必须配置）。
	ProvisionSSHKnownHosts string

	// M3-2A JWT 验签（网关公钥 RS256）：可选启用，启用后控制面自行校验 Authorization: Bearer <token>，
	// 从 claims 提取 tenant_id/user_id/user_roles，不再纯依赖网关注入的 X-Tenant-ID 等头。
	// 空（默认）=关闭，回退到 FromHTTPHeader 头注入模式（MVP 兼容）。
	// 生产模式（--production=true）下推荐配置，作为"网关剥离 + 内核二次校验"的纵深防御。
	JWTPublicKey string // PEM 格式 RSA 公钥文件路径（RS256 验签用）；空=关闭 JWT 验签
	JWTIssuer    string // 预期 JWT issuer（iss claim）；非空时校验 iss 必须匹配，空=不校验 iss

	// 用户中心 JWT 签发密钥（HS256）：内核自行签发 token 用于用户登录/注册后下发。
	// 与 JWTPublicKey（网关 RS256 验签）互补：JWTPublicKey 验签网关 token，JWTSecret 签发内核 token。
	// 空=随机生成（重启后旧 token 失效，仅开发/单实例适用）；生产多副本须注入一致密钥。
	JWTSecret string // HS256 对称密钥；空=随机生成（重启后旧 token 失效）

	// M4-4B 日志检索后端选择：memory（默认，环形缓冲） | sql（MySQL） | loki（Grafana Loki） | es（Elasticsearch）。
	// 与 Store（控制面持久化后端）解耦：Store 管 agent/device 状态，LogBackend 管日志检索。
	// loki/es 模式下日志由 agent 经 promtail/filebeat 直接推送，控制面仅做查询（Append 为 noop）。
	// LogStore 为 --log-store flag 的对应字段，与 LogBackend 同值（--log-store 作为 --log-backend 的别名，
	// 显式设置 --log-store 时覆盖 LogBackend；保持 --log-backend 向后兼容）。
	LogStore     string // memory | sql | loki | es（默认 memory）；--log-store flag 对应字段，与 LogBackend 同步
	LogBackend   string // memory | sql | loki | es（默认 memory）；--log-backend flag 对应字段（与 LogStore 别名）
	LokiEndpoint string // Loki API endpoint（如 http://loki:3100）；--log-backend=loki 时生效
	ESEndpoint   string // Elasticsearch endpoint（如 http://es:9200）；--log-backend=es 时生效
	ESIndex      string // Elasticsearch 索引名（默认 opsmesh-logs）；--log-backend=es 时生效

	// M4-4C 多租户 schema 隔离：每租户路由到独立 MySQL schema（database），物理级数据隔离。
	// 开启后 store 层使用 MultiSchemaStore 而非单个 SQLStore。
	// 仅 --store=mysql 时生效（Validate 中校验）。
	MultiSchema  bool   // 是否开启多租户 schema 隔离（默认 false）
	SchemaPrefix string // schema 名前缀（默认 "opsmesh_tenant_"），最终 schema 名 = SchemaPrefix + tenantID

	// M4-4D 控制面联邦：逗号分隔的 peer 控制面 HTTP 地址列表（如 http://peer1:8080,http://peer2:8080）。
	// 非空时启用联邦 API（跨网段任务转发 / 联邦设备视图），控制面之间通过 HTTP/JSON 复用现有 REST API 通信。
	// peer 不可达不影响本地服务（容错设计：联邦 API 返回可用部分 + 不可达标记）。
	FederationPeers []string

	// P1-5 metrics 访问控制：逗号分隔的 CIDR 白名单，仅允许这些来源访问 /metrics 端点。
	// 空（默认）=不限制（向后兼容 MVP），但生产建议显式配置内网监控网段（如 10.0.0.0/8）。
	MetricsAllowCIDR string

	// P1-6 联邦通道硬化：
	//   - FederationSecret：联邦 peer 间共享 HMAC 密钥，对转发的身份头签名/验签，防跨不可信网段伪造租户身份。
	//   - FederationTLSCert/Key/CA：联邦专用 mTLS 凭证（区别于 gRPC 的 --tls-cert/key/client-ca）。
	//   - FederationPort：联邦独立 mTLS 监听端口（>0 时启用，强制对端持证）；0=不启用独立监听（复用主 HTTP）。
	FederationSecret  string
	FederationTLSCert string
	FederationTLSKey  string
	FederationCA      string
	FederationPort    int

	// 安全加固（task 78）：agent 侧纵深防御，避免控制面被绕过/被攻陷时直接 RCE 目标机。
	//   - AgentShellWhitelist：逗号分隔的允许命令前缀列表（如 ls,cat,echo,ping,systemctl,docker,kubectl）。
	//     空（默认）=不限制（向后兼容，demo/受信内网环境）；非空=仅当命令第一个词匹配某前缀时放行。
	//     匹配按"命令第一个 token 的 basename"做前缀匹配，避免 "ls;rm -rf /" 这类拼接绕过（;后仍属同一 sh -c 命令，
	//     白名单仅做最佳努力防御，纵深防御应配合控制面侧 SubmitTask 校验 + IAM 鉴权）。
	//   - AgentFileRootWhitelist：逗号分隔的允许文件任务根目录列表（如 /var/opsmesh/files,/etc/opsmesh）。
	//     空（默认）=不限制根目录（仍拒绝 ../ 路径遍历与符号链接）；非空=目标路径必须落在某个根目录之下。
	AgentShellWhitelist    string
	AgentFileRootWhitelist string

	// task 81 gRPC agent 身份绑定：是否强制要求 agent 在 PullTasks/ReportResult/PollCancels/Heartbeat
	// 请求中携带 HMAC 签名（agent-signature metadata）。开启后，控制面用该 agentID 对应的 secret 重新
	// 计算 HMAC 并与 metadata 中的签名比对，签名不匹配或 timestamp 超过 5 分钟则拒绝。
	//   - demo 模式下强制关闭（向后兼容，demo 不需要签名）。
	//   - 生产模式（--production=true）下默认开启（除非显式 --grpc-require-signature=false）。
	//   - 已启用 mTLS（--tls-cert + --client-ca 均非空）时可不开启（mTLS 本身提供身份绑定）。
	GRPCRequireSignature bool
	// 反向代理信任（安全运行于 LB/网关后时）：开启后 clientIP 信任 X-Forwarded-For 首段取真实客户端 IP；
	// 默认 false=仅用 RemoteAddr（防止客户端伪造 XFF 绕过登录限流/审计）；仅当确有可信反代前置时才开启。
	TrustProxy bool
	// Cookie Secure 标志（task 112）：控制 at/rt HttpOnly Cookie 的 Secure 属性。
	// true=Cookie 仅经 HTTPS 传输（浏览器不会在明文 HTTP 连接上回传），防中间人窃取会话；
	// false=Cookie 亦经 HTTP 传输（内网明文部署/本地开发场景需要，否则会话丢失）。
	// 语义优先级：显式 --cookie-secure=true → true；否则回退到 TLSCert 非空（HTTPS 直连时自动启用）；
	// 生产模式（--production=true）下默认 true（HTTPS 反代终止 TLS 时控制面虽收 HTTP，但对外是 HTTPS，须显式开启）。
	CookieSecure bool
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
	maxProcs := flag.Int("max-procs", 256, "agent RLIMIT_NPROC 上限（P0-3，fork 炸弹防护；0=不限制）；或 env OPSMESH_MAX_PROCS")
	maxFiles := flag.Int("max-files", 4096, "agent RLIMIT_NOFILE 上限（P0-3，文件描述符耗尽防护；0=不限制）；或 env OPSMESH_MAX_FILES")
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
	publicRegister := flag.Bool("public-register", true, "允许公开注册（P1-7 注册安全）：true=开放 /api/v1/auth/register 但新用户须管理员审批；false=关闭公开注册（仅管理员可创建用户，返回 403）。注意：--demo 会强制覆盖为 true（接口开放），故企业版要关闭注册须【不带 --demo】并显式 --public-register=false；是否免审批另由 --allow-public-register 控制")
	allowPublicRegister := flag.Bool("allow-public-register", false, "允许公开注册免审批（P1-7 注册安全）：true=注册即激活并立即签发 token（仅演示/内网受信环境）；false=所有注册（含 demo 模式）都走 pending 审批流程（默认安全基线）；或 env OPSMESH_ALLOW_PUBLIC_REGISTER")
	taskMaxRetries := flag.Int("task-max-retries", 3, "任务失败重试上限（F2）；超出置 failed（死信），需人工处置")
	leaderTTLSec := flag.Int("leader-ttl-sec", 15, "A3 选主租约秒；本实例持有 leader 身份的时长，到期前需续租")
	leaderTickSec := flag.Int("leader-tick-sec", 5, "A3 选主续租周期秒；leaderLoop 续租频率（应小于 leader-ttl-sec）")
	archiveAgeMin := flag.Int("archive-age-min", 1440, "F5 离线超龄自动归档阈值（分钟）；agent 最后心跳早于该时长的设备自动 retired（<=0 关闭）")
	provisionSecret := flag.String("provision-secret", "", "B1 自动纳管 install token 的 HMAC 签名密钥；空则本实例随机生成（多副本需一致）")
	advertiseAddr := flag.String("advertise-addr", "", "B1 自动纳管控制面对外 HTTP 地址（拼接 bootstrap 安装命令）；空则回退 127.0.0.1:<http-port>（仅本机开发）")
	alertWebhookURL := flag.String("alert-webhook-url", "", "M7 告警 Webhook 推送 URL（POST JSON 告警到此地址）；空=不推送。B7：URL 含 slack.com 走 Slack Block Kit，含 qyapi.weixin.qq.com 走企业微信 markdown")
	alertNotifierType := flag.String("alert-notifier-type", "generic", "M7 告警通知类型：generic(直接POST Alert JSON)/feishu(飞书卡片)/dingtalk(钉钉markdown)；B7：Webhook URL 域名可识别时自动覆盖此值")
	alertEmailHost := flag.String("alert-email-host", "", "B7 告警邮件 SMTP 主机（如 smtp.example.com）；空=关闭邮件通道（或 env OPSMESH_ALERT_EMAIL_HOST）")
	alertEmailPort := flag.Int("alert-email-port", 25, "B7 告警邮件 SMTP 端口（默认 25；或 env OPSMESH_ALERT_EMAIL_PORT）")
	alertEmailUser := flag.String("alert-email-user", "", "B7 告警邮件 SMTP 用户名（空=匿名发送；或 env OPSMESH_ALERT_EMAIL_USER）")
	alertEmailPass := flag.String("alert-email-pass", "", "B7 告警邮件 SMTP 密码（推荐 env OPSMESH_ALERT_EMAIL_PASS）")
	alertEmailFrom := flag.String("alert-email-from", "", "B7 告警邮件发件人地址（如 opsmesh@example.com；或 env OPSMESH_ALERT_EMAIL_FROM）")
	alertEmailTo := flag.String("alert-email-to", "", "B7 告警邮件收件人列表（逗号分隔；或 env OPSMESH_ALERT_EMAIL_TO）")
	provisionSSHUser := flag.String("provision-ssh-user", "root", "B1 SSH 自动推送：SSH 用户")
	provisionSSHKey := flag.String("provision-ssh-key", "", "B1 SSH 自动推送：SSH 私钥路径（空=关闭 SSH 推送）")
	provisionSSHKP := flag.String("provision-ssh-key-pass", "", "B1 SSH 自动推送：SSH 密钥密码（推荐 OPSMESH_PROVISION_SSH_KEY_PASS 环境变量）")
	provisionSSHKnownHosts := flag.String("provision-ssh-known-hosts", "", "B1 SSH KnownHosts 文件路径（等保加固）；空=InsecureIgnoreHostKey（生产务必配置）")
	jwtPublicKey := flag.String("jwt-public-key", "", "M3-2A JWT 验签公钥 PEM 文件路径（RS256）；空=关闭 JWT 验签回退头注入模式（或 env OPSMESH_JWT_PUBLIC_KEY）")
	jwtIssuer := flag.String("jwt-issuer", "", "M3-2A 预期 JWT issuer（iss claim）；非空时校验 iss 必须匹配（或 env OPSMESH_JWT_ISSUER）")
	jwtSecret := flag.String("jwt-secret", "", "用户中心 JWT 签发密钥（HS256）；空=随机生成（重启后旧 token 失效）（或 env OPSMESH_JWT_SECRET）")
	// M4-4B 日志检索后端：memory（默认） | sql | loki | es。
	logBackend := flag.String("log-backend", "memory", "日志检索后端: memory | sql | loki | es（M4-4B；loki/es 模式下日志由 agent 直接推送，控制面仅查询）")
	// --log-store 作为 --log-backend 的别名（task 97）：显式设置 --log-store 时覆盖 log-backend，
	// 保持 --log-backend 向后兼容；env OPSMESH_LOG_STORE 同样兜底。
	logStore := flag.String("log-store", "memory", "日志后端选择: memory | sql | loki | es（--log-backend 别名，task 97；显式设置时覆盖 --log-backend；或 env OPSMESH_LOG_STORE）")
	lokiEndpoint := flag.String("loki-endpoint", "", "Loki API endpoint（如 http://loki:3100）；--log-backend=loki 时生效（或 env OPSMESH_LOKI_ENDPOINT）")
	esEndpoint := flag.String("es-endpoint", "", "Elasticsearch endpoint（如 http://es:9200）；--log-backend=es 时生效（或 env OPSMESH_ES_ENDPOINT）")
	esIndex := flag.String("es-index", "opsmesh-logs", "Elasticsearch 索引名（--log-backend=es 时生效，默认 opsmesh-logs；或 env OPSMESH_ES_INDEX）")
	// M4-4C 多租户 schema 隔离：每租户独立 MySQL schema（database），物理级数据隔离。
	multiSchema := flag.Bool("multi-schema", false, "开启多租户 schema 隔离（M4-4C）：每租户路由到独立 MySQL schema；仅 --store=mysql 时生效（或 env OPSMESH_MULTI_SCHEMA）")
	schemaPrefix := flag.String("schema-prefix", "opsmesh_tenant_", "schema 名前缀（M4-4C）；最终 schema 名 = 前缀 + tenantID（或 env OPSMESH_SCHEMA_PREFIX）")
	// M4-4D 控制面联邦：逗号分隔的 peer 控制面 HTTP 地址列表。
	federationPeers := flag.String("federation-peers", "", "M4-4D 控制面联邦 peer 地址列表（逗号分隔，如 http://peer1:8080,http://peer2:8080）；非空时启用联邦 API（跨网段任务转发/联邦设备视图）；或 env OPSMESH_FEDERATION_PEERS")
	// P1-5 metrics 访问控制：逗号分隔的 CIDR 白名单。
	metricsAllowCIDR := flag.String("metrics-allow-cidr", "", "P1-5 metrics(/metrics) 访问控制：逗号分隔的 CIDR 白名单；空=不限制（生产建议配置内网监控网段，如 10.0.0.0/8）；或 env OPSMESH_METRICS_ALLOW_CIDR")
	// P1-6 联邦通道硬化配置。
	federationSecret := flag.String("federation-secret", "", "P1-6 联邦共享 HMAC 密钥（所有 peer 须一致）；签名/验签转发的身份头，防跨不可信网段伪造租户身份；空=不签名（仅内网信任）；或 env OPSMESH_FEDERATION_SECRET")
	federationTLSCert := flag.String("federation-tls-cert", "", "P1-6 联邦 mTLS 服务端/客户端证书（独立于 --tls-cert）；空=明文联邦（仅内网）；或 env OPSMESH_FEDERATION_TLS_CERT")
	federationTLSKey := flag.String("federation-tls-key", "", "P1-6 联邦 mTLS 私钥；或 env OPSMESH_FEDERATION_TLS_KEY")
	federationCA := flag.String("federation-ca", "", "P1-6 联邦 mTLS 对端 CA（校验证书链/要求客户端持证）；或 env OPSMESH_FEDERATION_CA")
	federationPort := flag.Int("federation-port", 0, "P1-6 联邦独立 mTLS 监听端口（>0 启用，强制对端持证）；0=不启用独立监听（复用主 HTTP）；或 env OPSMESH_FEDERATION_PORT")
	// 安全加固（task 78）：agent 侧命令白名单与文件任务根目录白名单。
	agentShellWhitelist := flag.String("agent-shell-whitelist", "", "安全加固：agent shell 任务允许的命令前缀列表（逗号分隔，如 ls,cat,echo,ping,systemctl,docker,kubectl）；空=不限制（向后兼容，demo/受信内网）；非空=仅当命令第一个词匹配某前缀时放行；或 env OPSMESH_AGENT_SHELL_WHITELIST")
	agentFileRootWhitelist := flag.String("agent-file-root-whitelist", "", "安全加固：agent 文件任务允许的根目录白名单（逗号分隔，如 /var/opsmesh/files,/etc/opsmesh）；空=不限制根目录（仍拒绝 ../ 路径遍历与符号链接）；非空=目标路径必须落在某个根目录之下；或 env OPSMESH_AGENT_FILE_ROOT_WHITELIST")
	// task 81 gRPC agent 身份绑定：强制要求 agent 请求携带 HMAC 签名。
	grpcRequireSignature := flag.Bool("grpc-require-signature", false, "gRPC agent 身份绑定：强制要求 agent 在 PullTasks/ReportResult/PollCancels/Heartbeat 携带 HMAC 签名（防冒领任务/伪造上报）；demo 模式强制关闭；生产模式默认开启（除非显式 false）；或 env OPSMESH_GRPC_REQUIRE_SIGNATURE")
	// P2 安全运行于反向代理/LB 后：开启后 clientIP 信任 X-Forwarded-For 首段；默认 false 仅用 RemoteAddr，
	// 防止客户端伪造 XFF 绕过登录限流与审计。仅当确有可信反代（如 APISIX/Nginx 注入真实 IP）前置时启用。
	trustProxy := flag.Bool("trust-proxy", false, "信任反向代理：开启后 clientIP 取 X-Forwarded-For 首段（仅当有可信 LB/网关前置时启用）；默认 false=仅用 RemoteAddr 防 XFF 伪造绕过限流；或 env OPSMESH_TRUST_PROXY")
	// task 112 Cookie Secure：控制 at/rt HttpOnly Cookie 的 Secure 属性。true=仅经 HTTPS 传输；
	// 默认 false（明文内网/本地开发需要）；生产模式（--production=true）下默认 true（除非显式 false）。
	// 或 env OPSMESH_COOKIE_SECURE。
	cookieSecure := flag.Bool("cookie-secure", false, "Cookie Secure 标志：true=at/rt Cookie 仅经 HTTPS 传输（防中间人窃取）；默认 false（明文内网/开发需要）；生产模式默认 true（HTTPS 反代终止 TLS 时须显式开启）；或 env OPSMESH_COOKIE_SECURE")
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
		Mode:                   val("mode", *mode, "OPSMESH_MODE"),
		Addr:                   val("addr", *addr, "OPSMESH_ADDR"),
		ControlAddr:            val("control-addr", *controlAddr, "OPSMESH_CONTROL_ADDR"),
		ControlAddrs:           val("control-addrs", *controlAddrs, "OPSMESH_CONTROL_ADDRS"),
		Segment:                val("segment", *segment, "OPSMESH_SEGMENT"),
		HTTPPort:               valInt("http-port", *httpPort, "OPSMESH_HTTP_PORT"),
		GRPCPort:               valInt("grpc-port", *grpcPort, "OPSMESH_GRPC_PORT"),
		MetricsPort:            valInt("metrics-port", *metricsPort, "OPSMESH_METRICS_PORT"),
		Store:                  val("store", *store, "OPSMESH_STORE"),
		MySQLDSN:               val("mysql-dsn", *mysqlDSN, "OPSMESH_MYSQL_DSN"),
		RedisAddr:              val("redis-addr", *redisAddr, "OPSMESH_REDIS_ADDR"),
		RequireAuth:            valBool("require-auth", *requireAuth, "OPSMESH_REQUIRE_AUTH"),
		TaskTimeout:            valDur("task-timeout", *taskTimeout, "OPSMESH_TASK_TIMEOUT"),
		ShutdownTimeout:        valDur("shutdown-timeout", *shutdownTimeout, "OPSMESH_SHUTDOWN_TIMEOUT"),
		TLSCert:                val("tls-cert", *tlsCert, "OPSMESH_TLS_CERT"),
		TLSKey:                 val("tls-key", *tlsKey, "OPSMESH_TLS_KEY"),
		ClientCA:               val("client-ca", *clientCA, "OPSMESH_CLIENT_CA"),
		Discover:               valBool("discover", *discover, "OPSMESH_DISCOVER"),
		SegmentCIDR:            val("segment-cidr", *segmentCIDR, "OPSMESH_SEGMENT_CIDR"),
		AutoProvision:          valBool("auto-provision", *autoProvision, "OPSMESH_AUTO_PROVISION"),
		MaxProcs:               valInt("max-procs", *maxProcs, "OPSMESH_MAX_PROCS"),
		MaxFiles:               valInt("max-files", *maxFiles, "OPSMESH_MAX_FILES"),
		MaxMemoryMB:            valInt64("max-memory-mb", *maxMemoryMB, "OPSMESH_MAX_MEMORY_MB"),
		WorkerConcurrency:      valInt("worker-concurrency", *workerConcurrency, "OPSMESH_WORKER_CONCURRENCY"),
		EventBus:               val("event-bus", *eventBus, "OPSMESH_EVENT_BUS"),
		KafkaBrokers:           val("kafka-brokers", *kafkaBrokers, "OPSMESH_KAFKA_BROKERS"),
		KafkaTopic:             val("kafka-topic", *kafkaTopic, "OPSMESH_KAFKA_TOPIC"),
		DataDir:                val("data-dir", *dataDir, "OPSMESH_DATA_DIR"),
		Demo:                   valBool("demo", *demo, "OPSMESH_DEMO"),
		InstallToken:           val("install-token", *installToken, "OPSMESH_INSTALL_TOKEN"),
		TaskLeaseSec:           valInt("task-lease-sec", *taskLeaseSec, "OPSMESH_TASK_LEASE_SEC"),
		Replicas:               valInt("replicas", *replicas, "OPSMESH_REPLICAS"),
		Production:             valBool("production", *production, "OPSMESH_PRODUCTION"),
		PublicRegister:         valBool("public-register", *publicRegister, "OPSMESH_PUBLIC_REGISTER"),
		AllowPublicRegister:    valBool("allow-public-register", *allowPublicRegister, "OPSMESH_ALLOW_PUBLIC_REGISTER"),
		TaskMaxRetries:         valInt("task-max-retries", *taskMaxRetries, "OPSMESH_TASK_MAX_RETRIES"),
		LeaderTTLSec:           valInt("leader-ttl-sec", *leaderTTLSec, "OPSMESH_LEADER_TTL_SEC"),
		LeaderTickSec:          valInt("leader-tick-sec", *leaderTickSec, "OPSMESH_LEADER_TICK_SEC"),
		ArchiveAgeMin:          valInt("archive-age-min", *archiveAgeMin, "OPSMESH_ARCHIVE_AGE_MIN"),
		ProvisionSecret:        val("provision-secret", *provisionSecret, "OPSMESH_PROVISION_SECRET"),
		AdvertiseAddr:          val("advertise-addr", *advertiseAddr, "OPSMESH_ADVERTISE_ADDR"),
		AlertWebhookURL:        val("alert-webhook-url", *alertWebhookURL, "OPSMESH_ALERT_WEBHOOK_URL"),
		AlertNotifierType:      val("alert-notifier-type", *alertNotifierType, "OPSMESH_ALERT_NOTIFIER_TYPE"),
		AlertEmailHost:         val("alert-email-host", *alertEmailHost, "OPSMESH_ALERT_EMAIL_HOST"),
		AlertEmailPort:         valInt("alert-email-port", *alertEmailPort, "OPSMESH_ALERT_EMAIL_PORT"),
		AlertEmailUser:         val("alert-email-user", *alertEmailUser, "OPSMESH_ALERT_EMAIL_USER"),
		AlertEmailPass:         val("alert-email-pass", *alertEmailPass, "OPSMESH_ALERT_EMAIL_PASS"),
		AlertEmailFrom:         val("alert-email-from", *alertEmailFrom, "OPSMESH_ALERT_EMAIL_FROM"),
		AlertEmailTo:           val("alert-email-to", *alertEmailTo, "OPSMESH_ALERT_EMAIL_TO"),
		ProvisionSSHUser:       val("provision-ssh-user", *provisionSSHUser, "OPSMESH_PROVISION_SSH_USER"),
		ProvisionSSHKey:        val("provision-ssh-key", *provisionSSHKey, "OPSMESH_PROVISION_SSH_KEY"),
		ProvisionSSHKP:         val("provision-ssh-key-pass", *provisionSSHKP, "OPSMESH_PROVISION_SSH_KEY_PASS"),
		ProvisionSSHKnownHosts: val("provision-ssh-known-hosts", *provisionSSHKnownHosts, "OPSMESH_PROVISION_SSH_KNOWN_HOSTS"),
		JWTPublicKey:           val("jwt-public-key", *jwtPublicKey, "OPSMESH_JWT_PUBLIC_KEY"),
		JWTIssuer:              val("jwt-issuer", *jwtIssuer, "OPSMESH_JWT_ISSUER"),
		JWTSecret:              val("jwt-secret", *jwtSecret, "OPSMESH_JWT_SECRET"),
		LogStore:               val("log-store", *logStore, "OPSMESH_LOG_STORE"),
		LogBackend:             val("log-backend", *logBackend, "OPSMESH_LOG_BACKEND"),
		LokiEndpoint:           val("loki-endpoint", *lokiEndpoint, "OPSMESH_LOKI_ENDPOINT"),
		ESEndpoint:             val("es-endpoint", *esEndpoint, "OPSMESH_ES_ENDPOINT"),
		ESIndex:                val("es-index", *esIndex, "OPSMESH_ES_INDEX"),

		MultiSchema:            valBool("multi-schema", *multiSchema, "OPSMESH_MULTI_SCHEMA"),
		SchemaPrefix:           val("schema-prefix", *schemaPrefix, "OPSMESH_SCHEMA_PREFIX"),
		FederationPeers:        parseFederationPeers(val("federation-peers", *federationPeers, "OPSMESH_FEDERATION_PEERS")),
		MetricsAllowCIDR:       val("metrics-allow-cidr", *metricsAllowCIDR, "OPSMESH_METRICS_ALLOW_CIDR"),
		FederationSecret:       val("federation-secret", *federationSecret, "OPSMESH_FEDERATION_SECRET"),
		FederationTLSCert:      val("federation-tls-cert", *federationTLSCert, "OPSMESH_FEDERATION_TLS_CERT"),
		FederationTLSKey:       val("federation-tls-key", *federationTLSKey, "OPSMESH_FEDERATION_TLS_KEY"),
		FederationCA:           val("federation-ca", *federationCA, "OPSMESH_FEDERATION_CA"),
		FederationPort:         valInt("federation-port", *federationPort, "OPSMESH_FEDERATION_PORT"),
		AgentShellWhitelist:    val("agent-shell-whitelist", *agentShellWhitelist, "OPSMESH_AGENT_SHELL_WHITELIST"),
		AgentFileRootWhitelist: val("agent-file-root-whitelist", *agentFileRootWhitelist, "OPSMESH_AGENT_FILE_ROOT_WHITELIST"),
		GRPCRequireSignature:   valBool("grpc-require-signature", *grpcRequireSignature, "OPSMESH_GRPC_REQUIRE_SIGNATURE"),
		TrustProxy:             valBool("trust-proxy", *trustProxy, "OPSMESH_TRUST_PROXY"),
		CookieSecure:           valBool("cookie-secure", *cookieSecure, "OPSMESH_COOKIE_SECURE"),
	}
	// task 97 --log-store 作为 --log-backend 别名：显式设置 --log-store（或 OPSMESH_LOG_STORE）时覆盖 LogBackend，
	// 使现有 LogBackend 校验/路由逻辑无缝复用；最终 LogStore 与 LogBackend 保持同值。
	// 优先级：显式 --log-store > 显式 --log-backend > env > 默认 "memory"。
	if explicit["log-store"] {
		cfg.LogBackend = cfg.LogStore
	}
	cfg.LogStore = cfg.LogBackend
	// A4 生产模式：默认开启 require-auth（除非显式关闭），并强告警 memory store。
	if cfg.Production && !explicit["require-auth"] {
		cfg.RequireAuth = true
	}
	// P1-7 注册安全：生产模式但未显式设置 --public-register 时，默认关闭公开注册（安全基线）。
	// demo 模式始终强制 PublicRegister=true（接口开放，方便演示），但是否免审批由 AllowPublicRegister 控制。
	// 默认 AllowPublicRegister=false：demo 模式下注册也走 pending 审批流程（安全基线）；
	// 显式 --allow-public-register=true 时 demo 模式注册才免审批（Status=active + 立即签发 token）。
	if cfg.Demo {
		cfg.PublicRegister = true
	} else if cfg.Production && !explicit["public-register"] {
		cfg.PublicRegister = false
	}
	// task 81 gRPC agent 身份绑定：
	//   - demo 模式强制关闭签名验证（向后兼容，demo 不需要签名）。
	//   - 生产模式但未显式设置 --grpc-require-signature 时默认开启（纵深防御）。
	//   - 已启用 mTLS（tls-cert + client-ca 均非空）时可不开启（mTLS 本身提供身份绑定），但仍可显式开启叠加防御。
	if cfg.Demo {
		cfg.GRPCRequireSignature = false
	} else if cfg.Production && !explicit["grpc-require-signature"] {
		cfg.GRPCRequireSignature = true
	}
	// task 112 Cookie Secure：生产模式但未显式设置 --cookie-secure 时默认开启（HTTPS 反代终止 TLS
	// 时控制面虽收 HTTP，但对外是 HTTPS，Cookie 须 Secure 防中间人窃取）。开发/演示模式保持 false
	//（明文 HTTP 下 Secure Cookie 会被浏览器拒绝回传，导致会话丢失）。
	if cfg.Production && !explicit["cookie-secure"] {
		cfg.CookieSecure = true
	}
	if cfg.Production && cfg.PublicRegister {
		fmt.Fprintln(os.Stderr, "[config] 警告：生产模式开启 --public-register=true，公开注册开放但新用户须管理员审批（建议生产关闭 --public-register=false）")
	}
	if cfg.AllowPublicRegister {
		fmt.Fprintln(os.Stderr, "[config] 警告：--allow-public-register=true 已启用，公开注册将免审批（Status=active + 立即签发 token），仅演示/内网受信环境推荐；生产务必关闭")
	}
	if cfg.Production && cfg.Store == "memory" {
		fmt.Fprintln(os.Stderr, "[config] 警告：生产模式但 store=memory（多副本数据分裂），请改用 --store=mysql（U-04 数据本地化）")
	}
	// H6: 生产模式 TLS 未配置的拒绝逻辑已移至 Validate()（启动即 fail-fast），
	// 此处保留 require-auth 告警以便运维感知。
	if cfg.Production && !cfg.RequireAuth {
		fmt.Fprintln(os.Stderr, "[config] 警告：生产模式但 --require-auth=false，agent 注册不经过网关身份校验（仅开发/内网调试推荐）")
	}
	// M3-2A：生产模式但未启用 JWT 二次验签时友好告警（不强制，向后兼容纯头注入模式）。
	// 启用 JWT 验签可作为"网关注入 + 内核二次校验"的纵深防御，降低网关被绕过时的越权面。
	if cfg.Production && cfg.JWTPublicKey == "" {
		fmt.Fprintln(os.Stderr, "[config] 提示：生产模式未配置 --jwt-public-key，仅依赖网关注入的 X-Tenant-ID 头（建议启用 JWT 二次验签作为纵深防御）")
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

// parseFederationPeers 解析逗号分隔的 peer 地址列表为 []string，去除空白项与首尾空格。
// 输入空串返回 nil（不启用联邦），保证 NewServer 中 `if cfg.FederationPeers != nil` 判空可用。
func parseFederationPeers(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
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
	// H6 生产模式 TLS 强制：Production==true 且未配置 TLS 证书时直接拒绝启动，
	// 避免 agent↔控制面明文通信（等保三级要求）。agent 与 controlplane 同样适用：
	// agent 模式下 Production=true 也必须持证与控制面建立 mTLS。
	// 非 Production 模式不校验（开发/内网友好网络降级）。
	if c.Production && c.TLSCert == "" {
		return fmt.Errorf("生产模式（--production=true）必须配置 TLS（--tls-cert 为空），明文通信不满足等保三级要求；请提供证书或关闭 --production")
	}
	// M3-2Ab 生产控制面必须配置稳定 JWT 密钥（task 96）。
	// 语义：控机用户中心 JWT 签发密钥为空则重启丢会话、多副本各自独立随机密钥互不相认、用户间歇 401。
	// 生产直接 fail-fast，与 H6 生产强 TLS 同风格。dev 随机兜底语义（config.JWTSecret 空=随机）保留。
	if c.Production && c.JWTSecret == "" {
		return fmt.Errorf("生产模式（--production=true）controlplane 必须设置 --jwt-secret（或环境变量 OPSMESH_JWT_SECRET）；否则各副本独立随机密钥互相不认、重启后会话全部失效")
	}
	if c.Production && len([]byte(c.JWTSecret)) < 32 {
		return fmt.Errorf("生产模式 --jwt-secret 长度不足（%d 字节 < 32）：需强随机 256-bit 对称密钥（建议 openssl rand -hex 32）", len([]byte(c.JWTSecret)))
	}
	// M4-4B 日志检索后端校验：非法值或缺失必要 endpoint 直接 fail-fast。
	switch c.LogBackend {
	case "memory", "sql", "loki", "es":
	default:
		return fmt.Errorf("非法 --log-backend=%q（应为 memory | sql | loki | es）", c.LogBackend)
	}
	if c.LogBackend == "loki" && c.LokiEndpoint == "" {
		return fmt.Errorf("--log-backend=loki 但 --loki-endpoint 为空（M4-4B 需要 Loki API 地址）")
	}
	if c.LogBackend == "es" {
		if c.ESEndpoint == "" {
			return fmt.Errorf("--log-backend=es 但 --es-endpoint 为空（M4-4B 需要 Elasticsearch API 地址）")
		}
		if c.ESIndex == "" {
			return fmt.Errorf("--log-backend=es 但 --es-index 为空（M4-4B 需要索引名）")
		}
	}
	// M4-4C 多租户 schema 隔离：仅支持 mysql store（MultiSchemaStore 内部用 *SQLStore）。
	if c.MultiSchema && c.Store != "mysql" {
		return fmt.Errorf("--multi-schema=true 但 --store=%q（多 schema 隔离仅支持 mysql 后端）", c.Store)
	}
	// M4-4D 控制面联邦：peer 地址必须是合法 URL（含 scheme + host），启动期 fail-fast 避免运行期诡异失败。
	for i, p := range c.FederationPeers {
		u, err := url.Parse(p)
		if err != nil {
			return fmt.Errorf("非法 --federation-peers[%d]=%q: %w", i, p, err)
		}
		if u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("非法 --federation-peers[%d]=%q（需含 scheme 与 host，如 http://peer:8080）", i, p)
		}
	}
	// P1-5 metrics CIDR 白名单格式校验：每项必须是合法 CIDR，启动 fail-fast。
	if c.MetricsAllowCIDR != "" {
		for _, item := range strings.Split(c.MetricsAllowCIDR, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if _, _, err := net.ParseCIDR(item); err != nil {
				return fmt.Errorf("非法 --metrics-allow-cidr 项 %q: %w", item, err)
			}
		}
	}
	// P1-6 联邦通道硬化校验：独立 mTLS 监听需证书；启用联邦但缺失共享密钥时告警（身份不可验签）。
	if c.FederationPort > 65535 {
		return fmt.Errorf("非法 --federation-port=%d（应 ≤ 65535）", c.FederationPort)
	}
	if c.FederationPort > 0 && (c.FederationTLSCert == "" || c.FederationTLSKey == "") {
		return fmt.Errorf("--federation-port>0 但 --federation-tls-cert/key 为空（独立 mTLS 监听需要服务端证书）")
	}
	// P1-6 联邦通道硬化校验（task 97 强校验）：启用联邦但缺失共享密钥时直接拒绝启动，
	// 防止跨不可信网段伪造租户身份头（原告警改为 fail-fast）。
	if len(c.FederationPeers) > 0 && c.FederationSecret == "" {
		return fmt.Errorf("federation-secret is required when federation-peers is set")
	}
	return nil
}
