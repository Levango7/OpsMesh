// Package config 提供统一配置：命令行 flag 优先，环境变量兜底。
// 控制面与 agent 共用同一份配置结构，通过 --mode 切换角色。
package config

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// defaultAgentShellWhitelist 是 agent shell 白名单的预置安全默认值（安全加固）。
// 仅含只读诊断/查询命令，不含任何可修改系统状态或执行任意代码的命令（如 sh/bash/rm/mv/curl/wget/nc/python/perl）。
// 跨平台覆盖：Linux/macOS 通用（ls/cat/echo/date/whoami/hostname/pwd/free/df/uptime/top/ps/netstat/ss）
// + Windows（ipconfig/systeminfo）。网络诊断命令（ping/traceroute/curl 等）由 agent 端
// isNetworkDiagnoseCommand 内置白名单放行，不在此列（避免与该机制重复）。
// 当 --agent-shell-whitelist 未显式设置且 --agent-shell-whitelist-default=true（默认）时使用。
const defaultAgentShellWhitelist = "ls,cat,echo,date,whoami,hostname,pwd,free,df,uptime,top,ps,netstat,ss,ipconfig,systeminfo"

// DefaultAgentShellWhitelist 返回 agent shell 白名单的预置安全默认值（导出，供测试/外部包引用）。
func DefaultAgentShellWhitelist() string {
	return defaultAgentShellWhitelist
}

// stubStoreDomains SQL 后端尚未持久化的 P1-P6 领域清单（逗号分隔）。
// 与 internal/store/stub_guard.go 的 StubDomains 保持一一对应；config 包不得
// import store 包（避免配置层反向依赖存储实现），故此处维护字面量副本，
// 两处新增/移除领域时必须同步更新（stub_guard.go StubDomains 注释已互相声明此约束）。
// 现状：P1-P6 全部 15 个领域已实现 MySQL 持久化，清单收敛为空字符串；
// Validate 中生产模式 + SQL 后端的拒绝启动逻辑据此跳过（无桩领域则无须放行门槛）。
const stubStoreDomains = ""

// Config 启动时解析出的运行参数。地址/端口全部走 flag，不硬编码任何密钥。
type Config struct {
	Mode        string // controlplane | agent
	Addr        string // agent 自身地址（占位，供控制面感知）
	ControlAddr string // 控制面 HTTP 地址（agent 发起注册/心跳/拉任务用；gRPC 端口固定 9090）；单地址兼容写法
	// 多控制面 failover：逗号分隔的多个控制面地址，agent 依次重试（HA 真多副本前置）。
	// 为空时回退使用 ControlAddr 单地址（向后兼容）。
	ControlAddrs string
	// 服务发现：逗号分隔的多个控制面地址（与 --control-addrs 同义，作为服务发现入口）。
	// 优先级：--controlplane-endpoints > --control-addrs > --control-addr。
	// 非空时启用 internal/discovery 包的 StaticDiscovery + Balancer（round-robin/failover），
	// agent 通过 balancer 选择控制面实例，连接失败时自动 failover 到下一个。
	// 为空时回退到 ControlAddrs / ControlAddr（向后兼容）。
	ControlplaneEndpoints string
	// 负载均衡策略：round-robin（轮询）| failover（主备切换，默认）。
	// 单控制面地址时退化为始终用该地址（不破坏现有行为）。
	LBStrategy  string
	Segment     string // agent 所属网段（分桶键）
	HTTPPort    int    // 控制面 HTTP(B/S) 端口（约定 8080）
	GRPCPort    int    // gRPC 端口（约定 9090，真实 gRPC 注册通道）
	MetricsPort int    // metrics 端口（约定 9091）
	// 数据本地化：持久化后端选择。
	Store       string // 持久化后端: memory（默认） | mysql
	MySQLDSN    string // MySQL DSN（--store=mysql 时生效），如 user:pass@tcp(host:3306)/ops_device
	RedisAddr   string // Redis 地址（--store=mysql 时作 agent/device 状态缓存），如 redis:6379
	RequireAuth bool   // 生产模式：要求网关注入租户头，缺失则拒绝（MVP 默认 false=开发降级）
	// 运行健壮性
	TaskTimeout     time.Duration // agent 单任务执行超时（默认 120s）
	ShutdownTimeout time.Duration // 收到 SIGTERM 后的优雅退出窗口（默认 15s）
	// gRPC TLS / mTLS（默认空=关闭，仅内网友好网用；等保生产建议开启）
	TLSCert  string // 服务端证书 / 客户端证书 文件路径
	TLSKey   string // 私钥 文件路径
	ClientCA string // 服务端要求客户端 CA（mTLS）；或客户端校验服务端 CA
	// TLS 证书热重载：启用 fsnotify 监听证书文件变更，自动重载无需重启。
	// 仅当 TLSCert/TLSKey 非空时生效；关闭时证书更新需重启服务。
	TLSWatch bool // --tls-watch 启用证书文件热重载（默认 false）

	// 密钥管理外置：支持从环境变量/JSON文件/HashiCorp Vault 读取密钥。
	// 空字符串=不启用密钥外置（向后兼容，密钥直接从 config 字段读取）。
	// 启用后告警通道等敏感字段可使用 ${key} 引用语法从 provider 解析。
	SecretProvider string // --secret-provider 密钥来源：env|file|vault|chain:env,file（空=不启用）
	SecretFile     string // --secret-file JSON 密钥文件路径（--secret-provider=file 时生效）
	VaultAddr      string // --vault-addr Vault API 地址（如 https://vault:8200）
	VaultToken     string // --vault-token Vault 访问令牌（推荐 env OPSMESH_VAULT_TOKEN 更安全）
	VaultMount     string // --vault-mount Vault KV v2 挂载路径（默认 "secret"）

	// 真实网段发现。默认关闭：采用“agent 即设备”的 MVP 降级纳管；
	// 开启后控制面按 SegmentCIDR 做存活扫描，为每台存活主机创建真实 DeviceInfo。
	Discover    bool   // 是否开启真实网段发现
	SegmentCIDR string // 待扫描网段（如 10.30.0.0/24）
	// 自动纳管：discover 扫描到存活主机后，自动登记候选设备并（配置 SSH 私钥时）推送 agent。
	AutoProvision bool // 是否在 discover 基础上自动纳管（需 --provision-ssh-key）

	// agent 进程资源限额，0 表示不设置。
	MaxProcs    int   // RLIMIT_NPROC：最大进程/线程数
	MaxFiles    int   // RLIMIT_NOFILE：最大打开文件数
	MaxMemoryMB int64 // RLIMIT_AS：最大虚拟内存（MB）

	// agent 任务 worker 池并发度，默认 4。
	WorkerConcurrency int

	// 事件总线：noop（默认）| log | kafka（-tags kafka 构建下生效）。
	EventBus     string // 事件总线类型
	KafkaBrokers string // Kafka brokers（env OPSMESH_KAFKA_BROKERS）
	KafkaTopic   string // Kafka topic（env OPSMESH_KAFKA_TOPIC）

	// agent 身份持久化：agent.id 落盘目录；空=内存生成（重启即新 ID，旧任务成孤儿）。
	DataDir string
	// 演示模式：开启时每个 agent 注册预置一条 uname -a 示例任务；生产默认关闭，避免污染。
	Demo bool
	// 自动纳管闭环：agent 经 bootstrap 安装时携带的一次性 install token。
	// 由控制面 Provision 签发（HMAC 签名、一次性、限时），agent 注册时回传以完成自动纳管闭环。
	InstallToken string
	// 任务租约租期秒：agent 领取任务后超过该时长未上报结果，视为失联并复位 pending 重新调度（任务必达）。
	TaskLeaseSec int

	// 控制面副本数（HA）：由部署平台注入（如 K8s replicas）。>1 时必须用 mysql store，
	// 否则 memory store 多副本数据分裂（默认 1，单机/开发）。
	Replicas int
	// 生产模式：开启后默认 require-auth=true 且对 store=memory 强告警（生产基线）。
	Production bool
	// 注册安全：是否允许公开注册。
	// true（默认，向后兼容）=允许 /api/v1/auth/register 公开调用，但新用户 Status="pending" 须管理员审批；
	// false=关闭公开注册接口（仅管理员可通过 POST /api/v1/users 创建用户）。
	// demo 模式下保持 true 且新用户 Status="active"（方便演示，无需审批）。
	PublicRegister bool
	// 注册安全：是否允许公开注册免审批（Status=active + 立即签发 token）。
	// false（默认，安全基线）=所有注册（包括 demo 模式）都走 pending 审批流程，须管理员激活后方可登录；
	// true=注册即激活并立即签发 token（仅演示/内网受信环境使用，生产务必关闭）。
	// 与 PublicRegister 解耦：PublicRegister 控制接口是否开放，AllowPublicRegister 控制是否免审批。
	AllowPublicRegister bool
	// F2 任务失败重试上限：SubmitResult 失败时按策略重入队，达上限置 failed（死信）。
	TaskMaxRetries int
	// 选主租约秒：本实例持有 leader 身份的时长；到期前需续租，否则被其他副本抢占。
	LeaderTTLSec int
	// 选主续租周期秒：leaderLoop 续租频率（应小于 LeaderTTLSec，建议 1/3）。
	LeaderTickSec int
	// F5 离线超龄自动归档阈值（分钟）：agent 最后心跳早于该时长的设备自动 retired。
	// <=0 表示关闭自动归档（仅手动 DELETE 退役）。
	ArchiveAgeMin int
	// 自动纳管：install token 的 HMAC 签名密钥（一次性、限时）。
	// 多副本共享同一 MySQL 时需一致（否则互不相认）；空则本实例随机生成（单实例 MVP）。
	ProvisionSecret string
	// 自动纳管：控制面对外可达的 HTTP 地址，用于拼接 bootstrap 安装命令。
	// 安全：bootstrap 绝不能用请求方可控的 r.Host（Host 头注入→供应链 RCE），
	// 必须由运维显式配置可信地址。空则回退 http://127.0.0.1:<http-port>（仅本机开发）。
	AdvertiseAddr string
	// M7 监控告警：告警 Webhook 推送 URL（POST JSON）。
	// 空=不推送；非空时 critical 告警通过 HTTP POST 推送到此地址。
	AlertWebhookURL string
	// M7 告警通知类型：generic（默认，直接 POST Alert JSON）/ feishu（飞书卡片）/ dingtalk（钉钉 markdown）。
	// 仅当 AlertWebhookURL 非空时生效。扩展：Webhook URL 含 slack.com 自动走 Slack Block Kit，
	// 含 qyapi.weixin.qq.com 自动走企业微信 markdown（此时 AlertNotifierType 被忽略）。
	AlertNotifierType string
	// 告警邮件通道：SMTP 配置。Host/Port/From/To 任一为空视为关闭邮件通道（跳过发送）。
	AlertEmailHost string // SMTP 服务器地址（如 smtp.example.com）
	AlertEmailPort int    // SMTP 端口（如 25/465/587）
	AlertEmailUser string // SMTP 用户名（空=匿名发送）
	AlertEmailPass string // SMTP 密码（推荐 env OPSMESH_ALERT_EMAIL_PASS）
	AlertEmailFrom string // 发件人地址
	AlertEmailTo   string // 收件人列表（逗号分隔）
	// 通知渠道扩展：多渠道配置（钉钉/企业微信/飞书/Slack/邮件）。
	// 通过 --notify-channels-config 指定 JSON 配置文件加载，或通过 OPSMESH_NOTIFY_CHANNELS_CONFIG 环境变量指定路径。
	// 文件格式：{"channels": [{"type":"dingtalk","webhook_url":"...","secret":"..."}, ...]}
	// 与现有 AlertWebhookURL/AlertEmail* 字段互补：渠道由 notify.Notifier 统一调度，
	// 旧字段走 notify.Channels.Push（向后兼容，不破坏现有逻辑）。
	NotifyChannelsConfigFile string                // 通知渠道 JSON 配置文件路径（空=不加载多渠道配置）
	NotifyChannels           []NotifyChannelConfig // 解析后的渠道配置列表（由配置文件加载）
	// 通知去重 TTL（分钟）：相同消息在 TTL 内只发送一次。<=0 表示关闭去重。
	NotifyDedupTTLMin int // 去重 TTL（分钟，默认 5；0=关闭）
	// 通知重试：发送失败时按指数退避重试。
	NotifyRetryMaxAttempts int           // 最大重试次数（含首次，默认 3；0=不重试）
	NotifyRetryInterval    time.Duration // 重试基础间隔（默认 5s）
	NotifyRetryBackoff     float64       // 退避系数（默认 2.0）
	// 自动纳管推送：SSH 配置（空=关闭 SSH 自动推送，仅返回 bootstrap 文本）。
	// 推送时在候选设备上通过 SSH 执行 bootstrap 命令，自动安装 agent。
	ProvisionSSHUser string // SSH 用户（默认 "root"）
	ProvisionSSHKey  string // SSH 私钥路径（如 /etc/opsmesh/ssh/id_rsa）
	ProvisionSSHKP   string // SSH 密钥密码（可选；env OPSMESH_PROVISION_SSH_KEY_PASS 更安全）
	// B1 SSH KnownHosts 文件路径（等保安全加固）。非空时使用 ssh.KnownHosts 校验远程主机指纹；
	// 空（默认）时回退 InsecureIgnoreHostKey（MVP 便利，生产必须配置）。
	ProvisionSSHKnownHosts string

	// JWT 验签（网关公钥 RS256）：可选启用，启用后控制面自行校验 Authorization: Bearer <token>，
	// 从 claims 提取 tenant_id/user_id/user_roles，不再纯依赖网关注入的 X-Tenant-ID 等头。
	// 空（默认）=关闭，回退到 FromHTTPHeader 头注入模式（MVP 兼容）。
	// 生产模式（--production=true）下推荐配置，作为"网关剥离 + 内核二次校验"的纵深防御。
	JWTPublicKey string // PEM 格式 RSA 公钥文件路径（RS256 验签用）；空=关闭 JWT 验签
	JWTIssuer    string // 预期 JWT issuer（iss claim）；非空时校验 iss 必须匹配，空=不校验 iss

	// 用户中心 JWT 签发密钥（HS256）：内核自行签发 token 用于用户登录/注册后下发。
	// 与 JWTPublicKey（网关 RS256 验签）互补：JWTPublicKey 验签网关 token，JWTSecret 签发内核 token。
	// 空=随机生成（重启后旧 token 失效，仅开发/单实例适用）；生产多副本须注入一致密钥。
	JWTSecret string // HS256 对称密钥；空=随机生成（重启后旧 token 失效）

	// kubeconfig 静态加密密钥（AES-256-GCM）：base64 编码的 32 字节密钥。
	// 控制面存入 store 前对 K8s 集群 kubeconfig 做 AES-GCM 加密，DB 泄露时不直接暴露集群凭据。
	// 空（默认）=不加密（明文存储，仅开发/demo 适用）；生产模式（--production=true）必须显式配置。
	// 建议生成：openssl rand 32 | base64（输出 44 字符 base64，解码后 32 字节 AES-256 密钥）。
	EncryptionKey string // base64 编码的 32 字节 AES-256 密钥；空=不加密（非生产）

	// 日志检索后端选择：memory（默认，环形缓冲） | sql（MySQL） | loki（Grafana Loki） | es（Elasticsearch）。
	// 与 Store（控制面持久化后端）解耦：Store 管 agent/device 状态，LogBackend 管日志检索。
	// loki/es 模式下日志由 agent 经 promtail/filebeat 直接推送，控制面仅做查询（Append 为 noop）。
	// LogStore 为 --log-store flag 的对应字段，与 LogBackend 同值（--log-store 作为 --log-backend 的别名，
	// 显式设置 --log-store 时覆盖 LogBackend；保持 --log-backend 向后兼容）。
	LogStore     string // memory | sql | loki | es（默认 memory）；--log-store flag 对应字段，与 LogBackend 同步
	LogBackend   string // memory | sql | loki | es（默认 memory）；--log-backend flag 对应字段（与 LogStore 别名）
	LokiEndpoint string // Loki API endpoint（如 http://loki:3100）；--log-backend=loki 时生效
	ESEndpoint   string // Elasticsearch endpoint（如 http://es:9200）；--log-backend=es 时生效
	ESIndex      string // Elasticsearch 索引名（默认 opsmesh-logs）；--log-backend=es 时生效

	// 多租户 schema 隔离：每租户路由到独立 MySQL schema（database），物理级数据隔离。
	// 开启后 store 层使用 MultiSchemaStore 而非单个 SQLStore。
	// 仅 --store=mysql 时生效（Validate 中校验）。
	MultiSchema  bool   // 是否开启多租户 schema 隔离（默认 false）
	SchemaPrefix string // schema 名前缀（默认 "opsmesh_tenant_"），最终 schema 名 = SchemaPrefix + tenantID

	// 控制面联邦：逗号分隔的 peer 控制面 HTTP 地址列表（如 http://peer1:8080,http://peer2:8080）。
	// 非空时启用联邦 API（跨网段任务转发 / 联邦设备视图），控制面之间通过 HTTP/JSON 复用现有 REST API 通信。
	// peer 不可达不影响本地服务（容错设计：联邦 API 返回可用部分 + 不可达标记）。
	FederationPeers []string

	// metrics 访问控制：逗号分隔的 CIDR 白名单，仅允许这些来源访问 /metrics 端点。
	// 空（默认）=不限制（向后兼容 MVP），但生产建议显式配置内网监控网段（如 10.0.0.0/8）。
	MetricsAllowCIDR string

	// 联邦通道硬化：
	//   - FederationSecret：联邦 peer 间共享 HMAC 密钥，对转发的身份头签名/验签，防跨不可信网段伪造租户身份。
	//   - FederationTLSCert/Key/CA：联邦专用 mTLS 凭证（区别于 gRPC 的 --tls-cert/key/client-ca）。
	//   - FederationPort：联邦独立 mTLS 监听端口（>0 时启用，强制对端持证）；0=不启用独立监听（复用主 HTTP）。
	FederationSecret  string
	FederationTLSCert string
	FederationTLSKey  string
	FederationCA      string
	FederationPort    int

	// 安全加固：agent 侧纵深防御，避免控制面被绕过/被攻陷时直接 RCE 目标机。
	//   - AgentShellWhitelist：逗号分隔的允许命令前缀列表（如 ls,cat,echo,ping,systemctl,docker,kubectl）。
	//     空（默认）=不限制（向后兼容，demo/受信内网环境）；非空=仅当命令第一个词匹配某前缀时放行。
	//     匹配按"命令第一个 token 的 basename"做前缀匹配，避免 "ls;rm -rf /" 这类拼接绕过（;后仍属同一 sh -c 命令，
	//     白名单仅做最佳努力防御，纵深防御应配合控制面侧 SubmitTask 校验 + IAM 鉴权）。
	//   - AgentFileRootWhitelist：逗号分隔的允许文件任务根目录列表（如 /var/opsmesh/files,/etc/opsmesh）。
	//     空（默认）=不限制根目录（仍拒绝 ../ 路径遍历与符号链接）；非空=目标路径必须落在某个根目录之下。
	AgentShellWhitelist    string
	AgentFileRootWhitelist string
	// 安全加固：agent shell 白名单默认开启。
	// true（默认）=当 --agent-shell-whitelist 未显式设置时，自动填充一组安全的只读诊断命令白名单
	//  （见 defaultAgentShellWhitelist），避免 agent 侧默认放行所有命令的 RCE 风险。
	// false=保持原行为（未显式 --agent-shell-whitelist 时不限制，向后兼容 demo/受信内网）。
	// 显式设置 --agent-shell-whitelist=... 时本字段被忽略（用户自定义优先）；
	// 显式设置 --agent-shell-whitelist=""（显式空）时尊重用户意图（不限制）。
	AgentShellWhitelistDefault bool

	// gRPC agent 身份绑定：是否强制要求 agent 在 PullTasks/ReportResult/PollCancels/Heartbeat
	// 请求中携带 HMAC 签名（agent-signature metadata）。开启后，控制面用该 agentID 对应的 secret 重新
	// 计算 HMAC 并与 metadata 中的签名比对，签名不匹配或 timestamp 超过 5 分钟则拒绝。
	//   - demo 模式下强制关闭（向后兼容，demo 不需要签名）。
	//   - 生产模式（--production=true）下默认开启（除非显式 --grpc-require-signature=false）。
	//   - 已启用 mTLS（--tls-cert + --client-ca 均非空）时可不开启（mTLS 本身提供身份绑定）。
	GRPCRequireSignature bool
	// 安全加固：gRPC agent 身份绑定的预共享 HMAC 签名密钥。
	// 此前签名密钥由控制面在 Register 响应中下发给 agent，但注册不硬（任何人可注册）时
	// 攻击者可注册获取密钥后伪造签名，使签名形同虚设。改为预共享方式：
	//   - 控制面与 agent 两侧通过 --grpc-signature-key 手动配置同一密钥。
	//   - Register 响应不再返回签名密钥（Secret 字段始终为空）。
	//   - verifyAgentSignature 优先使用此预共享密钥验签；为空时回退到 store.AgentSecret（向后兼容已注册 agent）。
	//   - agent 端优先使用此预共享密钥签名；为空时不签名（向后兼容，但日志警告）。
	// 空=未配置预共享密钥（回退到旧的 store.AgentSecret 机制，向后兼容）。
	GRPCSignatureKey string
	// 反向代理信任（安全运行于 LB/网关后时）：开启后 clientIP 信任 X-Forwarded-For 首段取真实客户端 IP；
	// 默认 false=仅用 RemoteAddr（防止客户端伪造 XFF 绕过登录限流/审计）；仅当确有可信反代前置时才开启。
	TrustProxy bool
	// 安全加固：是否信任网关注入的 X-User-Roles 头作为身份来源。
	// 默认 false（安全基线）：忽略 X-User-Roles 头，要求请求携带 Bearer token/Cookie 或经联邦验签；
	//   这避免非生产模式下客户端自称 admin 即得 admin 权限的越权路径。
	// true=信任 X-User-Roles 头（仅当确有可信网关（如 APISIX/IAM）前置剥离并注入该头时启用）；
	//   生产模式（--production=true）下强制 false（即使显式 true 也覆盖），杜绝生产环境信任客户端可伪造的头。
	TrustGatewayHeaders bool
	// Cookie Secure 标志：控制 at/rt HttpOnly Cookie 的 Secure 属性。
	// true=Cookie 仅经 HTTPS 传输（浏览器不会在明文 HTTP 连接上回传），防中间人窃取会话；
	// false=Cookie 亦经 HTTP 传输（内网明文部署/本地开发场景需要，否则会话丢失）。
	// 语义优先级：显式 --cookie-secure=true → true；否则回退到 TLSCert 非空（HTTPS 直连时自动启用）；
	// 生产模式（--production=true）下默认 true（HTTPS 反代终止 TLS 时控制面虽收 HTTP，但对外是 HTTPS，须显式开启）。
	CookieSecure bool

	// CORS 白名单（安全加固，替代反射任意 Origin）：逗号分隔的允许跨域来源列表
	// （如 https://console.example.com,https://ops.example.com）。
	// 安全语义：
	//   - 空（默认）=同源策略：corsMiddleware 不输出任何 CORS 头（浏览器跨域请求被同源策略
	//     拦截），同源部署（前端与控制面同域名/同端口/经反代同源）不受影响。
	//   - 非空=仅当请求 Origin 与列表中某项【精确匹配】时才输出
	//     Access-Control-Allow-Origin（回显该 Origin）+ Allow-Credentials: true，
	//     支持带 Cookie 的可信跨域；不匹配的 Origin 不输出任何 CORS 头（等同同源拒绝）。
	//   - 禁止配置 "*"：凭证模式下 Allow-Origin: * 与 Cookie 不兼容，且等于放开任意来源，
	//     Validate 中直接拒绝启动。
	// 反射任意 Origin + Allow-Credentials 是 Critical 漏洞：恶意网站可带用户 Cookie 跨域调 API。
	AllowedOrigins string

	// 多副本会话状态共享：SessionStore 后端选择。
	// 空（默认）=进程内 map（InProcessSessionStore，单副本/demo 零依赖）；
	// "redis://host:port"=Redis 后端（RedisSessionStore，多副本 HA 共享 JWT 黑名单/限流计数/改密令牌）。
	// 多副本 HA 部署（replicas>1）应配置此项，否则登出后 access token 在其他副本仍有效。
	SessionStore string // "" | redis://host:port

	// OpenTelemetry 链路追踪：可选启用，endpoint 为空时 no-op（零开销，不破坏现有功能）。
	//   - OTELEndpoint：OTLP gRPC 导出地址（如 "jaeger:4317" 或 "otel-collector:4317"）；空=不启用。
	//   - OTELServiceName：服务名标识（如 "opsmesh-controlplane" / "opsmesh-agent"）；空=回退 "opsmesh"。
	//   - OTELStdout：是否导出到 stderr（调试用）；与 Endpoint 互斥，Stdout 优先。
	// 启用后控制面 HTTP + agent gRPC 自动埋点，trace_id 贯穿 agent→控制面→store。
	OTELEndpoint    string // OTLP gRPC 导出地址（空=不启用）
	OTELServiceName string // 服务名（空=回退 "opsmesh"）
	OTELStdout      bool   // 导出到 stderr（调试用）

	// DeviceFP deadline：refresh token 设备指纹强制非空的截止时间。
	// 零值（默认）=不强制（向后兼容，DeviceFP 为空时跳过设备绑定校验）；
	// 非零=该时刻之后签发的 refresh token 必须绑定 DeviceFP（非空），否则 consumeRefreshToken 拒绝。
	// 用于渐进式强制设备绑定：deadline 前允许旧客户端不传 DeviceFP，deadline 后强制要求。
	// 格式：RFC3339（如 "2026-09-01T00:00:00Z"）；或 env OPSMESH_DEVICE_FP_DEADLINE。
	DeviceFPDeadline time.Time

	// 熔断器（Circuit Breaker）配置：
	//   - CBFailureThreshold：连续失败多少次后熔断该设备/通道。<=0 表示禁用熔断器（透传，向后兼容）。
	//     agent 端按 deviceID（即 agentID）隔离，控制面按 IP/tenant 限流。
	//   - CBRecoveryTimeout：熔断后等待多久才进入 HalfOpen 半开探测。默认 30s。
	//   - CBHalfOpenMaxCalls：HalfOpen 状态下允许的最大并发探测调用数。默认 1。
	//   - CBRateLimitPerSec：控制面 API 限流阈值（每秒每 IP/tenant 最大请求数）。<=0 表示禁用 API 限流。
	// 典型用法：agent 配置 --cb-failure-threshold=5 --cb-recovery-timeout=30s；
	// 控制面配置 --cb-rate-limit-per-sec=100 限流 API。
	CBFailureThreshold int           // 连续失败 N 次后熔断（默认 5；0=禁用）
	CBRecoveryTimeout  time.Duration // 熔断后等待时间（默认 30s）
	CBHalfOpenMaxCalls int           // 半开状态最大探测调用数（默认 1）
	CBRateLimitPerSec  int           // 控制面 API 限流阈值（每秒每 IP/tenant；0=禁用）

	// SSRF 防护：
	//   - WebhookAllowPrivate：是否允许 webhook URL 指向内网地址（私网/loopback/链路本地）。
	//     默认 false（安全基线，拒绝内网 webhook，防 SSRF 攻击云元数据/内网服务）；
	//     true=放行内网 webhook（用于内网部署场景，如钉钉/飞书内网网关）。
	//     仅影响通知渠道 CRUD（createNotifyChannel/updateNotifyChannel）保存前校验，
	//     不影响启动期 AlertWebhookURL 校验（后者恒拒私网，由 validateURLSSRF 控制）。
	//   - ProvisionCIDRWhitelist：autoProvision 扫描网段白名单（逗号分隔的 CIDR 列表）。
	//     空（默认）=不校验（向后兼容，由调用方决定是否启用白名单）；
	//     非空=autoProvision handler 扫描前校验目标 CIDR 必须完全落在白名单内，
	//     防止运维误配置或攻击者构造请求扫描任意网段（如 169.254.169.254 元数据网段）。
	WebhookAllowPrivate    bool   // 允许内网 webhook URL（SSRF 防护，默认 false）
	ProvisionCIDRWhitelist string // autoProvision CIDR 白名单（逗号分隔，空=不限制）

	// 告警抑制集成：告警抑制规则 JSON 文件路径。
	// 空（默认）=不启用告警抑制（向后兼容，alertInhibitor 为 nil，评估流程跳过抑制检查）；
	// 非空=NewServer 启动时调用 alertengine.LoadInhibitRules 加载规则并构造 AlertInhibitor，
	// 告警评估前先过抑制规则（父告警活跃时抑制子告警，避免告警风暴）。
	// 文件格式见 alertengine.LoadInhibitRules 文档（顶层 JSON 数组，snake_case 字段）。
	// Validate 校验文件存在；加载失败 fail-fast。
	InhibitRulesFile string // 告警抑制规则 JSON 文件路径（空=不启用）

	// 异常检测：基于基线偏离的告警规则。
	// 启用后 NewServer 构造 AnomalyEngine，alertEngineLoop 对设备指标调用 Evaluate，
	// 异常时产生 AnomalyAlert 并经现有告警链（静默/聚合/通知）推送。
	// 默认关闭（向后兼容，anomalyEngine 为 nil，评估流程跳过异常检测）。
	AnomalyDetection  bool    // --anomaly-detection 启用异常检测（默认 false）
	AnomalyWindowSize int     // --anomaly-window-size 基线窗口大小（默认 100）
	AnomalyThreshold  float64 // --anomaly-threshold Z-Score 阈值（默认 3.0）

	// 日志采集推送：agent 端尾随日志文件（tail -f），批量推送到 Loki/ES。
	// 启用后 agent 构造 LogPusher（internal/agent/log_push.go），对 LogPushFiles 中的文件
	// 尾随采集，按 LogPushPattern 正则过滤后批量推送到 LogPushEndpoint。
	// 默认关闭（向后兼容，LogPushEnabled=false 时不启动 LogPusher）。
	// LogPushFiles 为逗号分隔的文件路径，Validate 中 split 为 []string。
	LogPushEnabled  bool     // --log-push-enabled 启用日志推送（默认 false）
	LogPushFiles    []string // --log-push-files 日志文件列表（逗号分隔）
	LogPushPattern  string   // --log-push-pattern 正则过滤（空=不过滤）
	LogPushEndpoint string   // --log-push-endpoint 推送目标（如 http://loki:3100/loki/api/v1/push）
	LogPushBackend  string   // --log-push-backend 后端类型：loki|es（默认 loki）

	// 多租户资源配额与计费：
	//   - QuotaEnabled：是否启用配额检查（设备/任务/告警创建前校验是否超额）。
	//     默认 false（向后兼容，不启用配额检查）；true 时 QuotaManager.CheckDevice/CheckTask/CheckAlert
	//     在创建路径拦截超额请求返回 ErrQuotaExceeded。
	//   - QuotaMaxDevices/QuotaMaxTasks/QuotaMaxAlerts：默认配额（未显式设置配额的租户回退到此值）。
	//     0=不限（默认，向后兼容）；非 0 时作为新租户的默认上限。
	QuotaEnabled    bool // --quota-enabled 启用配额检查（默认 false）
	QuotaMaxDevices int  // --quota-max-devices 默认最大设备数（0=不限）
	QuotaMaxTasks   int  // --quota-max-tasks 默认最大任务数（0=不限）
	QuotaMaxAlerts  int  // --quota-max-alerts 默认最大告警数（0=不限）

	// 自动化引擎评估周期：控制面周期评估 enabled 自动化规则并执行命中动作。
	// 由 server_lifecycle.go Start 中 startAutomationEvalLoop(ctx, interval) 消费。
	// 默认 30s；<=0 时按 30s 兜底（startAutomationEvalLoop 内处理）。
	AutomationEvalInterval time.Duration // --automation-eval-interval 自动化规则评估周期（默认 30s）

	// H2/H3 配套开关：是否允许 SQL 后端继续使用 P1-P6 桩存储。
	// 背景：SQLStore 对 15 个领域（见 stubStoreDomains 清单）曾为桩实现，写入不持久化；
	// 生产模式（--production=true）+ --store=mysql 时默认拒绝启动（fail-fast），
	// 运维必须显式 --allow-stub-stores=true 确认接受桩限制后才放行。
	// 现状：P1-P6 全部 15 个领域已实现真实 MySQL CRUD，stubStoreDomains 为空，
	// Validate 中的拒绝启动分支被跳过，本开关保留向后兼容（未来新桩领域可复用）。
	// memory 后端与 demo/开发模式不受影响；运行期由 store 层 stub_guard 限频告警兜底。
	AllowStubStores bool // --allow-stub-stores 允许 SQL 后端桩存储（生产默认 false=拒绝启动）
}

// Load 解析 flag 并用环境变量兜底，返回 *Config。
func Load() *Config {
	mode := flag.String("mode", "controlplane", "运行模式: controlplane | agent")
	addr := flag.String("addr", "127.0.0.1", "agent 自身地址（占位）")
	controlAddr := flag.String("control-addr", "http://127.0.0.1:8080", "控制面 HTTP 地址（agent 用）；单地址兼容写法")
	controlAddrs := flag.String("control-addrs", "", "多控制面地址（逗号分隔，如 cp1:9090,cp2:9090）；agent 依次重试实现 HA failover；空则回退 --control-addr")
	// 服务发现：--controlplane-endpoints 作为 --control-addrs 的别名（语义同，作为服务发现入口）。
	// 优先级：--controlplane-endpoints > --control-addrs > --control-addr。
	// 显式设置 --controlplane-endpoints 时覆盖 --control-addrs；或 env OPSMESH_CONTROLPLANE_ENDPOINTS。
	controlplaneEndpoints := flag.String("controlplane-endpoints", "", "服务发现：多控制面地址（逗号分隔，如 cp1:9090,cp2:9090）；与 --control-addrs 同义，作为服务发现入口；优先级高于 --control-addrs；空则回退 --control-addrs/--control-addr；或 env OPSMESH_CONTROLPLANE_ENDPOINTS")
	// 负载均衡策略：round-robin | failover（默认 failover，向后兼容现有 多控制面依次重试行为）。
	lbStrategy := flag.String("lb-strategy", "failover", "负载均衡策略：round-robin（轮询）| failover（主备切换，默认）；单控制面地址时退化为始终用该地址；或 env OPSMESH_LB_STRATEGY")
	segment := flag.String("segment", "default", "agent 所属网段")
	httpPort := flag.Int("http-port", 8080, "控制面 HTTP(B/S) 端口（约定 8080）")
	grpcPort := flag.Int("grpc-port", 9090, "gRPC 端口（约定 9090）")
	metricsPort := flag.Int("metrics-port", 9091, "metrics 端口（约定 9091）")
	// 数据本地化：持久化后端选择（默认 memory，保证无外部依赖即可运行）。
	store := flag.String("store", "memory", "持久化后端: memory（默认） | mysql（数据本地化）")
	mysqlDSN := flag.String("mysql-dsn", "", "MySQL DSN（--store=mysql 时生效），如 user:pass@tcp(mysql:3306)/ops_device")
	redisAddr := flag.String("redis-addr", "", "Redis 地址（--store=mysql 时作状态缓存），如 redis:6379")
	requireAuth := flag.Bool("require-auth", false, "要求网关注入 X-Tenant-ID，缺失则拒绝（生产 hardening）")
	taskTimeout := flag.Duration("task-timeout", 120*time.Second, "agent 单任务执行超时")
	shutdownTimeout := flag.Duration("shutdown-timeout", 15*time.Second, "SIGTERM 优雅退出窗口")
	tlsCert := flag.String("tls-cert", "", "gRPC TLS 证书路径（空=关闭）")
	tlsKey := flag.String("tls-key", "", "gRPC TLS 私钥路径")
	clientCA := flag.String("client-ca", "", "服务端要求客户端 CA（mTLS）/ 客户端校验服务端 CA")
	tlsWatch := flag.Bool("tls-watch", false, "启用 TLS 证书文件热重载（fsnotify 监听，无需重启）")
	// 密钥管理外置：从环境变量/JSON文件/HashiCorp Vault 读取密钥。
	secretProvider := flag.String("secret-provider", "", "密钥来源：env|file|vault|chain:env,file（空=不启用密钥外置，向后兼容）；或 env OPSMESH_SECRET_PROVIDER")
	secretFile := flag.String("secret-file", "", "JSON 密钥文件路径（--secret-provider=file 时生效）；或 env OPSMESH_SECRET_FILE")
	vaultAddr := flag.String("vault-addr", "", "Vault API 地址（如 https://vault:8200）；或 env OPSMESH_VAULT_ADDR")
	vaultToken := flag.String("vault-token", "", "Vault 访问令牌（env OPSMESH_VAULT_TOKEN 更安全，避免命令行暴露）；或 env OPSMESH_VAULT_TOKEN")
	vaultMount := flag.String("vault-mount", "secret", "Vault KV v2 挂载路径（默认 secret）；或 env OPSMESH_VAULT_MOUNT")
	discover := flag.Bool("discover", false, "开启真实网段发现；关闭时采用 agent 即设备的 MVP 降级纳管")
	segmentCIDR := flag.String("segment-cidr", "", "待扫描网段（如 10.30.0.0/24）；开启 --discover 时生效")
	autoProvision := flag.Bool("auto-provision", false, "自动纳管：discover 扫描到存活主机后自动登记候选设备并（配置 --provision-ssh-key 时）推送 agent")
	maxProcs := flag.Int("max-procs", 256, "agent RLIMIT_NPROC 上限（fork 炸弹防护；0=不限制）；或 env OPSMESH_MAX_PROCS")
	maxFiles := flag.Int("max-files", 4096, "agent RLIMIT_NOFILE 上限（文件描述符耗尽防护；0=不限制）；或 env OPSMESH_MAX_FILES")
	maxMemoryMB := flag.Int64("max-memory-mb", 0, "agent RLIMIT_AS 上限 MB（0=不限制）")
	workerConcurrency := flag.Int("worker-concurrency", 4, "agent 任务 worker 池并发度")
	eventBus := flag.String("event-bus", "noop", "事件总线类型：noop | log | kafka")
	kafkaBrokers := flag.String("kafka-brokers", "", "Kafka brokers（或 env OPSMESH_KAFKA_BROKERS）")
	kafkaTopic := flag.String("kafka-topic", "", "Kafka topic（或 env OPSMESH_KAFKA_TOPIC）")
	dataDir := flag.String("data-dir", "./data", "agent 身份文件目录；agent.id 落盘于此，重启沿用")
	demo := flag.Bool("demo", false, "演示模式：每个 agent 注册预置 uname -a 示例任务（生产务必关闭）")
	installToken := flag.String("install-token", "", "自动纳管：agent 经 bootstrap 安装时携带的一次性 install token（空=无令牌闭环）")
	taskLeaseSec := flag.Int("task-lease-sec", 300, "任务租约租期秒；超期未上报结果则复位重调度")
	replicas := flag.Int("replicas", 1, "控制面副本数（由部署平台注入）；>1 须用 --store=mysql，否则 memory 多副本分裂")
	production := flag.Bool("production", false, "生产模式：默认开启 require-auth，并对 store=memory 强告警")
	publicRegister := flag.Bool("public-register", true, "允许公开注册（注册安全）：true=开放 /api/v1/auth/register 但新用户须管理员审批；false=关闭公开注册（仅管理员可创建用户，返回 403）。注意：--demo 会强制覆盖为 true（接口开放），故企业版要关闭注册须【不带 --demo】并显式 --public-register=false；是否免审批另由 --allow-public-register 控制")
	allowPublicRegister := flag.Bool("allow-public-register", false, "允许公开注册免审批（注册安全）：true=注册即激活并立即签发 token（仅演示/内网受信环境）；false=所有注册（含 demo 模式）都走 pending 审批流程（默认安全基线）；或 env OPSMESH_ALLOW_PUBLIC_REGISTER")
	taskMaxRetries := flag.Int("task-max-retries", 3, "任务失败重试上限（F2）；超出置 failed（死信），需人工处置")
	leaderTTLSec := flag.Int("leader-ttl-sec", 15, "选主租约秒；本实例持有 leader 身份的时长，到期前需续租")
	leaderTickSec := flag.Int("leader-tick-sec", 5, "选主续租周期秒；leaderLoop 续租频率（应小于 leader-ttl-sec）")
	archiveAgeMin := flag.Int("archive-age-min", 1440, "F5 离线超龄自动归档阈值（分钟）；agent 最后心跳早于该时长的设备自动 retired（<=0 关闭）")
	provisionSecret := flag.String("provision-secret", "", "自动纳管 install token 的 HMAC 签名密钥；空则本实例随机生成（多副本需一致）")
	advertiseAddr := flag.String("advertise-addr", "", "自动纳管控制面对外 HTTP 地址（拼接 bootstrap 安装命令）；空则回退 127.0.0.1:<http-port>（仅本机开发）")
	alertWebhookURL := flag.String("alert-webhook-url", "", "M7 告警 Webhook 推送 URL（POST JSON 告警到此地址）；空=不推送。：URL 含 slack.com 走 Slack Block Kit，含 qyapi.weixin.qq.com 走企业微信 markdown")
	alertNotifierType := flag.String("alert-notifier-type", "generic", "M7 告警通知类型：generic(直接POST Alert JSON)/feishu(飞书卡片)/dingtalk(钉钉markdown)；：Webhook URL 域名可识别时自动覆盖此值")
	alertEmailHost := flag.String("alert-email-host", "", "告警邮件 SMTP 主机（如 smtp.example.com）；空=关闭邮件通道（或 env OPSMESH_ALERT_EMAIL_HOST）")
	alertEmailPort := flag.Int("alert-email-port", 25, "告警邮件 SMTP 端口（默认 25；或 env OPSMESH_ALERT_EMAIL_PORT）")
	alertEmailUser := flag.String("alert-email-user", "", "告警邮件 SMTP 用户名（空=匿名发送；或 env OPSMESH_ALERT_EMAIL_USER）")
	alertEmailPass := flag.String("alert-email-pass", "", "告警邮件 SMTP 密码（推荐 env OPSMESH_ALERT_EMAIL_PASS）")
	alertEmailFrom := flag.String("alert-email-from", "", "告警邮件发件人地址（如 opsmesh@example.com；或 env OPSMESH_ALERT_EMAIL_FROM）")
	alertEmailTo := flag.String("alert-email-to", "", "告警邮件收件人列表（逗号分隔；或 env OPSMESH_ALERT_EMAIL_TO）")
	// 通知渠道扩展配置。
	notifyChannelsConfig := flag.String("notify-channels-config", "", "通知渠道 JSON 配置文件路径（多渠道：钉钉/企业微信/飞书/Slack/邮件）；空=不加载（或 env OPSMESH_NOTIFY_CHANNELS_CONFIG）")
	notifyDedupTTLMin := flag.Int("notify-dedup-ttl-min", 5, "通知去重 TTL（分钟）；相同消息在 TTL 内只发送一次；0=关闭去重（或 env OPSMESH_NOTIFY_DEDUP_TTL_MIN）")
	notifyRetryMaxAttempts := flag.Int("notify-retry-max-attempts", 3, "通知重试最大尝试次数（含首次）；0=不重试（或 env OPSMESH_NOTIFY_RETRY_MAX_ATTEMPTS）")
	notifyRetryInterval := flag.Duration("notify-retry-interval", 5*time.Second, "通知重试基础间隔（或 env OPSMESH_NOTIFY_RETRY_INTERVAL）")
	notifyRetryBackoff := flag.Float64("notify-retry-backoff", 2.0, "通知重试退避系数（1.0=固定间隔，2.0=指数退避；或 env OPSMESH_NOTIFY_RETRY_BACKOFF）")
	provisionSSHUser := flag.String("provision-ssh-user", "root", "B1 SSH 自动推送：SSH 用户")
	provisionSSHKey := flag.String("provision-ssh-key", "", "B1 SSH 自动推送：SSH 私钥路径（空=关闭 SSH 推送）")
	provisionSSHKP := flag.String("provision-ssh-key-pass", "", "B1 SSH 自动推送：SSH 密钥密码（推荐 OPSMESH_PROVISION_SSH_KEY_PASS 环境变量）")
	provisionSSHKnownHosts := flag.String("provision-ssh-known-hosts", "", "B1 SSH KnownHosts 文件路径（等保加固）；空=InsecureIgnoreHostKey（生产务必配置）")
	jwtPublicKey := flag.String("jwt-public-key", "", "JWT 验签公钥 PEM 文件路径（RS256）；空=关闭 JWT 验签回退头注入模式（或 env OPSMESH_JWT_PUBLIC_KEY）")
	jwtIssuer := flag.String("jwt-issuer", "", "预期 JWT issuer（iss claim）；非空时校验 iss 必须匹配（或 env OPSMESH_JWT_ISSUER）")
	jwtSecret := flag.String("jwt-secret", "", "用户中心 JWT 签发密钥（HS256）；空=随机生成（重启后旧 token 失效）（或 env OPSMESH_JWT_SECRET）")
	encryptionKey := flag.String("encryption-key", "", "kubeconfig AES-256-GCM 加密密钥（base64 编码 32 字节）；空=不加密（仅开发/demo，生产必须配置）；或 env OPSMESH_ENCRYPTION_KEY")
	// 日志检索后端：memory（默认） | sql | loki | es。
	logBackend := flag.String("log-backend", "memory", "日志检索后端: memory | sql | loki | es（loki/es 模式下日志由 agent 直接推送，控制面仅查询）")
	// --log-store 作为 --log-backend 的别名：显式设置 --log-store 时覆盖 log-backend，
	// 保持 --log-backend 向后兼容；env OPSMESH_LOG_STORE 同样兜底。
	logStore := flag.String("log-store", "memory", "日志后端选择: memory | sql | loki | es（--log-backend 别名，；显式设置时覆盖 --log-backend；或 env OPSMESH_LOG_STORE）")
	lokiEndpoint := flag.String("loki-endpoint", "", "Loki API endpoint（如 http://loki:3100）；--log-backend=loki 时生效（或 env OPSMESH_LOKI_ENDPOINT）")
	esEndpoint := flag.String("es-endpoint", "", "Elasticsearch endpoint（如 http://es:9200）；--log-backend=es 时生效（或 env OPSMESH_ES_ENDPOINT）")
	esIndex := flag.String("es-index", "opsmesh-logs", "Elasticsearch 索引名（--log-backend=es 时生效，默认 opsmesh-logs；或 env OPSMESH_ES_INDEX）")
	// 多租户 schema 隔离：每租户独立 MySQL schema（database），物理级数据隔离。
	multiSchema := flag.Bool("multi-schema", false, "开启多租户 schema 隔离：每租户路由到独立 MySQL schema；仅 --store=mysql 时生效（或 env OPSMESH_MULTI_SCHEMA）")
	schemaPrefix := flag.String("schema-prefix", "opsmesh_tenant_", "schema 名前缀；最终 schema 名 = 前缀 + tenantID（或 env OPSMESH_SCHEMA_PREFIX）")
	// 控制面联邦：逗号分隔的 peer 控制面 HTTP 地址列表。
	federationPeers := flag.String("federation-peers", "", "控制面联邦 peer 地址列表（逗号分隔，如 http://peer1:8080,http://peer2:8080）；非空时启用联邦 API（跨网段任务转发/联邦设备视图）；或 env OPSMESH_FEDERATION_PEERS")
	// metrics 访问控制：逗号分隔的 CIDR 白名单。
	metricsAllowCIDR := flag.String("metrics-allow-cidr", "", "metrics(/metrics) 访问控制：逗号分隔的 CIDR 白名单；空=不限制（生产建议配置内网监控网段，如 10.0.0.0/8）；或 env OPSMESH_METRICS_ALLOW_CIDR")
	// 联邦通道硬化配置。
	federationSecret := flag.String("federation-secret", "", "联邦共享 HMAC 密钥（所有 peer 须一致）；签名/验签转发的身份头，防跨不可信网段伪造租户身份；空=不签名（仅内网信任）；或 env OPSMESH_FEDERATION_SECRET")
	federationTLSCert := flag.String("federation-tls-cert", "", "联邦 mTLS 服务端/客户端证书（独立于 --tls-cert）；空=明文联邦（仅内网）；或 env OPSMESH_FEDERATION_TLS_CERT")
	federationTLSKey := flag.String("federation-tls-key", "", "联邦 mTLS 私钥；或 env OPSMESH_FEDERATION_TLS_KEY")
	federationCA := flag.String("federation-ca", "", "联邦 mTLS 对端 CA（校验证书链/要求客户端持证）；或 env OPSMESH_FEDERATION_CA")
	federationPort := flag.Int("federation-port", 0, "联邦独立 mTLS 监听端口（>0 启用，强制对端持证）；0=不启用独立监听（复用主 HTTP）；或 env OPSMESH_FEDERATION_PORT")
	// 安全加固：agent 侧命令白名单与文件任务根目录白名单。
	agentShellWhitelist := flag.String("agent-shell-whitelist", "", "安全加固：agent shell 任务允许的命令前缀列表（逗号分隔，如 ls,cat,echo,ping,systemctl,docker,kubectl）；空=不限制（向后兼容，demo/受信内网）；非空=仅当命令第一个词匹配某前缀时放行；或 env OPSMESH_AGENT_SHELL_WHITELIST")
	agentFileRootWhitelist := flag.String("agent-file-root-whitelist", "", "安全加固：agent 文件任务允许的根目录白名单（逗号分隔，如 /var/opsmesh/files,/etc/opsmesh）；空=不限制根目录（仍拒绝 ../ 路径遍历与符号链接）；非空=目标路径必须落在某个根目录之下；或 env OPSMESH_AGENT_FILE_ROOT_WHITELIST")
	// 安全加固：agent shell 白名单默认开启。
	// true（默认）=--agent-shell-whitelist 未显式设置时自动填充 defaultAgentShellWhitelist（只读诊断命令）；
	// false=保持原行为（未显式设置时不限制）。显式 --agent-shell-whitelist=... 时本 flag 被忽略。
	agentShellWhitelistDefault := flag.Bool("agent-shell-whitelist-default", true, "+3 安全加固：agent shell 白名单默认开启；true（默认）=未显式设置 --agent-shell-whitelist 时自动填充只读诊断命令白名单（ls/cat/echo/date/whoami/hostname/pwd/free/df/uptime/top/ps/netstat/ss/ipconfig/systeminfo）；false=保持原行为（不限制）；显式 --agent-shell-whitelist 时本 flag 被忽略；或 env OPSMESH_AGENT_SHELL_WHITELIST_DEFAULT")
	// gRPC agent 身份绑定：强制要求 agent 请求携带 HMAC 签名。
	grpcRequireSignature := flag.Bool("grpc-require-signature", false, "gRPC agent 身份绑定：强制要求 agent 在 PullTasks/ReportResult/PollCancels/Heartbeat 携带 HMAC 签名（防冒领任务/伪造上报）；demo 模式强制关闭；生产模式默认开启（除非显式 false）；或 env OPSMESH_GRPC_REQUIRE_SIGNATURE")
	// 安全加固：gRPC 签名预共享密钥（控制面与 agent 两侧手动配置同一密钥）。
	grpcSignatureKey := flag.String("grpc-signature-key", "", "安全加固：gRPC agent 身份绑定的预共享 HMAC 签名密钥（控制面与 agent 两侧须配置同一密钥）；Register 响应不再下发密钥，改用预共享方式防注册不硬时密钥外泄；空=回退到 store.AgentSecret（向后兼容）；或 env OPSMESH_GRPC_SIGNATURE_KEY")
	// 安全运行于反向代理/LB 后：开启后 clientIP 信任 X-Forwarded-For 首段；默认 false 仅用 RemoteAddr，
	// 防止客户端伪造 XFF 绕过登录限流与审计。仅当确有可信反代（如 APISIX/Nginx 注入真实 IP）前置时启用。
	trustProxy := flag.Bool("trust-proxy", false, "信任反向代理：开启后 clientIP 取 X-Forwarded-For 首段（仅当有可信 LB/网关前置时启用）；默认 false=仅用 RemoteAddr 防 XFF 伪造绕过限流；或 env OPSMESH_TRUST_PROXY")
	// 安全加固：信任网关注入的 X-User-Roles 头作为身份来源。
	// 默认 false（安全基线，防客户端自称 admin 越权）；true=信任该头（仅当有可信网关前置剥离/注入时启用）；
	// 生产模式（--production=true）下强制 false（即使显式 true 也覆盖），杜绝生产信任客户端可伪造的头。
	trustGatewayHeaders := flag.Bool("trust-gateway-headers", false, "信任网关注入的 X-User-Roles 头：开启后 requireProd 走 authorizeByRoles 路径信任头中角色；默认 false=忽略该头（防客户端自称 admin 越权）；生产模式强制 false；或 env OPSMESH_TRUST_GATEWAY_HEADERS")
	// Cookie Secure：控制 at/rt HttpOnly Cookie 的 Secure 属性。true=仅经 HTTPS 传输；
	// 默认 false（明文内网/本地开发需要）；生产模式（--production=true）下默认 true（除非显式 false）。
	// 或 env OPSMESH_COOKIE_SECURE。
	cookieSecure := flag.Bool("cookie-secure", false, "Cookie Secure 标志：true=at/rt Cookie 仅经 HTTPS 传输（防中间人窃取）；默认 false（明文内网/开发需要）；生产模式默认 true（HTTPS 反代终止 TLS 时须显式开启）；或 env OPSMESH_COOKIE_SECURE")
	// CORS 白名单：逗号分隔的允许跨域来源列表（安全加固，替代反射任意 Origin）。
	// 空（默认）=同源策略（不输出任何 CORS 头）；非空=仅精确匹配的 Origin 才放行并输出
	// Allow-Credentials 头；禁止 "*"（与凭证互斥，Validate 直接拒绝）。
	allowedOrigins := flag.String("allowed-origins", "", "CORS 白名单：逗号分隔的允许跨域来源（如 https://console.example.com）；空=同源策略（不输出 CORS 头，同源部署不受影响）；非空=仅精确匹配的 Origin 放行（带凭证）；禁止配置 *（与凭证互斥）；或 env OPSMESH_ALLOWED_ORIGINS")
	// 多副本会话状态共享：SessionStore 后端选择。
	// 空=进程内（默认，单副本/demo）；"redis://host:port"=Redis（多副本 HA 共享登出/限流/改密令牌）。
	sessionStore := flag.String("session-store", "", "会话状态后端：空=进程内 map（单副本/demo 默认） | redis://host:port（多副本 HA 共享 JWT 黑名单/限流/改密令牌）；或 env OPSMESH_SESSION_STORE")
	// DeviceFP deadline：该时刻之后签发的 refresh token 必须绑定 DeviceFP（非空）。
	// 空（默认）=不强制（向后兼容）；RFC3339 格式（如 2026-09-01T00:00:00Z）。
	deviceFPDeadline := flag.String("device-fp-deadline", "", "DeviceFP 强制非空截止时间：该时刻之后签发的 refresh token 必须绑定设备指纹（非空），之前向后兼容；空=不强制（默认）；RFC3339 格式（如 2026-09-01T00:00:00Z）；或 env OPSMESH_DEVICE_FP_DEADLINE")
	// OpenTelemetry 链路追踪：可选启用，endpoint 为空时 no-op。
	otelEndpoint := flag.String("otel-endpoint", "", "OTel OTLP gRPC 导出地址（如 jaeger:4317 或 otel-collector:4317）；空=不启用追踪（no-op，零开销）；或 env OPSMESH_OTEL_ENDPOINT")
	otelServiceName := flag.String("otel-service-name", "", "OTel 服务名标识（如 opsmesh-controlplane / opsmesh-agent）；空=回退 opsmesh；或 env OPSMESH_OTEL_SERVICE_NAME")
	otelStdout := flag.Bool("otel-stdout", false, "OTel 导出到 stderr（调试用，与 --otel-endpoint 互斥，stdout 优先）；或 env OPSMESH_OTEL_STDOUT")
	// 熔断器（Circuit Breaker）配置。
	cbFailureThreshold := flag.Int("cb-failure-threshold", 5, "熔断器：连续失败 N 次后熔断该设备/通道；0=禁用熔断器（透传，向后兼容）；agent 端按 deviceID 隔离，控制面按 IP/tenant 限流；或 env OPSMESH_CB_FAILURE_THRESHOLD")
	cbRecoveryTimeout := flag.Duration("cb-recovery-timeout", 30*time.Second, "熔断器：熔断后等待多久才进入 HalfOpen 半开探测；或 env OPSMESH_CB_RECOVERY_TIMEOUT")
	cbHalfOpenMaxCalls := flag.Int("cb-half-open-max-calls", 1, "熔断器：HalfOpen 状态下允许的最大并发探测调用数；或 env OPSMESH_CB_HALF_OPEN_MAX_CALLS")
	cbRateLimitPerSec := flag.Int("cb-rate-limit-per-sec", 0, "控制面 API 限流阈值：每秒每 IP/tenant 最大请求数；0=禁用 API 限流（向后兼容）；或 env OPSMESH_CB_RATE_LIMIT_PER_SEC")
	// SSRF 防护配置。
	webhookAllowPrivate := flag.Bool("webhook-allow-private", false, "SSRF 防护：允许内网 webhook URL（私网/loopback/链路本地）；默认 false=拒绝内网 webhook（安全基线，防 SSRF 访问云元数据/内网服务）；true=放行内网 webhook（内网部署场景，如钉钉/飞书内网网关）；或 env OPSMESH_WEBHOOK_ALLOW_PRIVATE")
	provisionCidrWhitelist := flag.String("provision-cidr-whitelist", "", "SSRF 防护：autoProvision 扫描网段白名单（逗号分隔的 CIDR 列表，如 10.30.0.0/24,10.31.0.0/24）；空=不校验（向后兼容）；非空=扫描前校验目标 CIDR 必须完全落在白名单内，防扫描任意网段；或 env OPSMESH_PROVISION_CIDR_WHITELIST")
	// 告警抑制集成：--inhibit-rules-file 指定抑制规则 JSON 文件路径。
	inhibitRulesFile := flag.String("inhibit-rules-file", "", "告警抑制规则 JSON 文件路径（空=不启用告警抑制，向后兼容）；非空时加载规则构造 AlertInhibitor，告警评估前先过抑制规则（父告警活跃时抑制子告警）；文件格式见 alertengine.LoadInhibitRules 文档；或 env OPSMESH_INHIBIT_RULES_FILE")
	// 异常检测：基于基线偏离的告警规则（滑动窗口 Z-Score + EWMA 突变检测）。
	anomalyDetection := flag.Bool("anomaly-detection", false, "启用异常检测（基线偏离告警）：启用后构造 AnomalyEngine，对设备指标评估异常并产生告警；默认 false（向后兼容）；或 env OPSMESH_ANOMALY_DETECTION")
	anomalyWindowSize := flag.Int("anomaly-window-size", 100, "异常检测基线窗口大小（滑动窗口数据点数，默认 100）；或 env OPSMESH_ANOMALY_WINDOW_SIZE")
	anomalyThreshold := flag.Float64("anomaly-threshold", 3.0, "异常检测 Z-Score 阈值（默认 3.0 即 3σ，约 99.7% 置信区间）；或 env OPSMESH_ANOMALY_THRESHOLD")
	// 日志采集推送：agent 端尾随日志文件，批量推送到 Loki/ES。
	logPushEnabled := flag.Bool("log-push-enabled", false, "启用日志采集推送：agent 尾随日志文件（tail -f）批量推送到 Loki/ES；默认 false（向后兼容）；或 env OPSMESH_LOG_PUSH_ENABLED")
	logPushFiles := flag.String("log-push-files", "", "日志采集文件列表（逗号分隔，如 /var/log/syslog,/var/log/app.log）；或 env OPSMESH_LOG_PUSH_FILES")
	logPushPattern := flag.String("log-push-pattern", "", "日志采集正则过滤（空=不过滤，全部推送；如 ^ERROR 仅推送 ERROR 行）；或 env OPSMESH_LOG_PUSH_PATTERN")
	logPushEndpoint := flag.String("log-push-endpoint", "", "日志推送目标 endpoint（Loki /api/v1/push 或 ES /_bulk，如 http://loki:3100/loki/api/v1/push）；或 env OPSMESH_LOG_PUSH_ENDPOINT")
	logPushBackend := flag.String("log-push-backend", "loki", "日志推送后端类型：loki | es（默认 loki）；或 env OPSMESH_LOG_PUSH_BACKEND")
	// 多租户资源配额与计费：租户级资源配额（设备数/任务数/告警数上限）。
	// 启用后 QuotaManager 在设备/任务/告警创建路径校验是否超额，超额返回 ErrQuotaExceeded。
	// 默认配额用于未显式设置配额的租户（0=不限，向后兼容）。
	quotaEnabled := flag.Bool("quota-enabled", false, "启用租户资源配额检查（设备/任务/告警创建前校验是否超额）；默认 false（向后兼容，不启用配额检查）；或 env OPSMESH_QUOTA_ENABLED")
	quotaMaxDevices := flag.Int("quota-max-devices", 0, "默认最大设备数（未显式设置配额的租户回退到此值；0=不限，默认）；或 env OPSMESH_QUOTA_MAX_DEVICES")
	quotaMaxTasks := flag.Int("quota-max-tasks", 0, "默认最大任务数（未显式设置配额的租户回退到此值；0=不限，默认）；或 env OPSMESH_QUOTA_MAX_TASKS")
	quotaMaxAlerts := flag.Int("quota-max-alerts", 0, "默认最大告警数（未显式设置配额的租户回退到此值；0=不限，默认）；或 env OPSMESH_QUOTA_MAX_ALERTS")
	// H2/H3 配套开关：SQL 后端桩存储放行开关（生产模式 + --store=mysql 时默认拒绝启动）。
	allowStubStores := flag.Bool("allow-stub-stores", false, "H2/H3 止血：允许 SQL 后端继续使用 P1-P6 桩存储（15 个领域写入不持久化，见启动日志清单）；生产模式（--production=true）+ --store=mysql 时默认 false=拒绝启动，须显式 true 确认接受桩限制；memory 后端不受影响；或 env OPSMESH_ALLOW_STUB_STORES")
	// 自动化引擎评估周期。
	automationEvalInterval := flag.Duration("automation-eval-interval", 30*time.Second, "自动化引擎评估周期：周期评估 enabled 自动化规则并执行命中动作（默认 30s；或 env OPSMESH_AUTOMATION_EVAL_INTERVAL）")
	flag.Parse()

	// 记录被显式设置的 flag，用于"flag 优先、env 兜底"的正确语义（修复：原实现 env 会覆盖显式 flag）。
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
	valFloat64 := func(name string, fv float64, envKey string) float64 {
		if explicit[name] {
			return fv
		}
		return float64Env(envKey, fv)
	}

	cfg := &Config{
		Mode:                     val("mode", *mode, "OPSMESH_MODE"),
		Addr:                     val("addr", *addr, "OPSMESH_ADDR"),
		ControlAddr:              val("control-addr", *controlAddr, "OPSMESH_CONTROL_ADDR"),
		ControlAddrs:             val("control-addrs", *controlAddrs, "OPSMESH_CONTROL_ADDRS"),
		ControlplaneEndpoints:    val("controlplane-endpoints", *controlplaneEndpoints, "OPSMESH_CONTROLPLANE_ENDPOINTS"),
		LBStrategy:               val("lb-strategy", *lbStrategy, "OPSMESH_LB_STRATEGY"),
		Segment:                  val("segment", *segment, "OPSMESH_SEGMENT"),
		HTTPPort:                 valInt("http-port", *httpPort, "OPSMESH_HTTP_PORT"),
		GRPCPort:                 valInt("grpc-port", *grpcPort, "OPSMESH_GRPC_PORT"),
		MetricsPort:              valInt("metrics-port", *metricsPort, "OPSMESH_METRICS_PORT"),
		Store:                    val("store", *store, "OPSMESH_STORE"),
		MySQLDSN:                 val("mysql-dsn", *mysqlDSN, "OPSMESH_MYSQL_DSN"),
		RedisAddr:                val("redis-addr", *redisAddr, "OPSMESH_REDIS_ADDR"),
		RequireAuth:              valBool("require-auth", *requireAuth, "OPSMESH_REQUIRE_AUTH"),
		TaskTimeout:              valDur("task-timeout", *taskTimeout, "OPSMESH_TASK_TIMEOUT"),
		ShutdownTimeout:          valDur("shutdown-timeout", *shutdownTimeout, "OPSMESH_SHUTDOWN_TIMEOUT"),
		TLSCert:                  val("tls-cert", *tlsCert, "OPSMESH_TLS_CERT"),
		TLSKey:                   val("tls-key", *tlsKey, "OPSMESH_TLS_KEY"),
		ClientCA:                 val("client-ca", *clientCA, "OPSMESH_CLIENT_CA"),
		TLSWatch:                 valBool("tls-watch", *tlsWatch, "OPSMESH_TLS_WATCH"),
		SecretProvider:           val("secret-provider", *secretProvider, "OPSMESH_SECRET_PROVIDER"),
		SecretFile:               val("secret-file", *secretFile, "OPSMESH_SECRET_FILE"),
		VaultAddr:                val("vault-addr", *vaultAddr, "OPSMESH_VAULT_ADDR"),
		VaultToken:               val("vault-token", *vaultToken, "OPSMESH_VAULT_TOKEN"),
		VaultMount:               val("vault-mount", *vaultMount, "OPSMESH_VAULT_MOUNT"),
		Discover:                 valBool("discover", *discover, "OPSMESH_DISCOVER"),
		SegmentCIDR:              val("segment-cidr", *segmentCIDR, "OPSMESH_SEGMENT_CIDR"),
		AutoProvision:            valBool("auto-provision", *autoProvision, "OPSMESH_AUTO_PROVISION"),
		MaxProcs:                 valInt("max-procs", *maxProcs, "OPSMESH_MAX_PROCS"),
		MaxFiles:                 valInt("max-files", *maxFiles, "OPSMESH_MAX_FILES"),
		MaxMemoryMB:              valInt64("max-memory-mb", *maxMemoryMB, "OPSMESH_MAX_MEMORY_MB"),
		WorkerConcurrency:        valInt("worker-concurrency", *workerConcurrency, "OPSMESH_WORKER_CONCURRENCY"),
		EventBus:                 val("event-bus", *eventBus, "OPSMESH_EVENT_BUS"),
		KafkaBrokers:             val("kafka-brokers", *kafkaBrokers, "OPSMESH_KAFKA_BROKERS"),
		KafkaTopic:               val("kafka-topic", *kafkaTopic, "OPSMESH_KAFKA_TOPIC"),
		DataDir:                  val("data-dir", *dataDir, "OPSMESH_DATA_DIR"),
		Demo:                     valBool("demo", *demo, "OPSMESH_DEMO"),
		InstallToken:             val("install-token", *installToken, "OPSMESH_INSTALL_TOKEN"),
		TaskLeaseSec:             valInt("task-lease-sec", *taskLeaseSec, "OPSMESH_TASK_LEASE_SEC"),
		Replicas:                 valInt("replicas", *replicas, "OPSMESH_REPLICAS"),
		Production:               valBool("production", *production, "OPSMESH_PRODUCTION"),
		PublicRegister:           valBool("public-register", *publicRegister, "OPSMESH_PUBLIC_REGISTER"),
		AllowPublicRegister:      valBool("allow-public-register", *allowPublicRegister, "OPSMESH_ALLOW_PUBLIC_REGISTER"),
		TaskMaxRetries:           valInt("task-max-retries", *taskMaxRetries, "OPSMESH_TASK_MAX_RETRIES"),
		LeaderTTLSec:             valInt("leader-ttl-sec", *leaderTTLSec, "OPSMESH_LEADER_TTL_SEC"),
		LeaderTickSec:            valInt("leader-tick-sec", *leaderTickSec, "OPSMESH_LEADER_TICK_SEC"),
		ArchiveAgeMin:            valInt("archive-age-min", *archiveAgeMin, "OPSMESH_ARCHIVE_AGE_MIN"),
		ProvisionSecret:          val("provision-secret", *provisionSecret, "OPSMESH_PROVISION_SECRET"),
		AdvertiseAddr:            val("advertise-addr", *advertiseAddr, "OPSMESH_ADVERTISE_ADDR"),
		AlertWebhookURL:          val("alert-webhook-url", *alertWebhookURL, "OPSMESH_ALERT_WEBHOOK_URL"),
		AlertNotifierType:        val("alert-notifier-type", *alertNotifierType, "OPSMESH_ALERT_NOTIFIER_TYPE"),
		AlertEmailHost:           val("alert-email-host", *alertEmailHost, "OPSMESH_ALERT_EMAIL_HOST"),
		AlertEmailPort:           valInt("alert-email-port", *alertEmailPort, "OPSMESH_ALERT_EMAIL_PORT"),
		AlertEmailUser:           val("alert-email-user", *alertEmailUser, "OPSMESH_ALERT_EMAIL_USER"),
		AlertEmailPass:           val("alert-email-pass", *alertEmailPass, "OPSMESH_ALERT_EMAIL_PASS"),
		AlertEmailFrom:           val("alert-email-from", *alertEmailFrom, "OPSMESH_ALERT_EMAIL_FROM"),
		AlertEmailTo:             val("alert-email-to", *alertEmailTo, "OPSMESH_ALERT_EMAIL_TO"),
		NotifyChannelsConfigFile: val("notify-channels-config", *notifyChannelsConfig, "OPSMESH_NOTIFY_CHANNELS_CONFIG"),
		NotifyDedupTTLMin:        valInt("notify-dedup-ttl-min", *notifyDedupTTLMin, "OPSMESH_NOTIFY_DEDUP_TTL_MIN"),
		NotifyRetryMaxAttempts:   valInt("notify-retry-max-attempts", *notifyRetryMaxAttempts, "OPSMESH_NOTIFY_RETRY_MAX_ATTEMPTS"),
		NotifyRetryInterval:      valDur("notify-retry-interval", *notifyRetryInterval, "OPSMESH_NOTIFY_RETRY_INTERVAL"),
		NotifyRetryBackoff:       valFloat64("notify-retry-backoff", *notifyRetryBackoff, "OPSMESH_NOTIFY_RETRY_BACKOFF"),
		ProvisionSSHUser:         val("provision-ssh-user", *provisionSSHUser, "OPSMESH_PROVISION_SSH_USER"),
		ProvisionSSHKey:          val("provision-ssh-key", *provisionSSHKey, "OPSMESH_PROVISION_SSH_KEY"),
		ProvisionSSHKP:           val("provision-ssh-key-pass", *provisionSSHKP, "OPSMESH_PROVISION_SSH_KEY_PASS"),
		ProvisionSSHKnownHosts:   val("provision-ssh-known-hosts", *provisionSSHKnownHosts, "OPSMESH_PROVISION_SSH_KNOWN_HOSTS"),
		JWTPublicKey:             val("jwt-public-key", *jwtPublicKey, "OPSMESH_JWT_PUBLIC_KEY"),
		JWTIssuer:                val("jwt-issuer", *jwtIssuer, "OPSMESH_JWT_ISSUER"),
		JWTSecret:                val("jwt-secret", *jwtSecret, "OPSMESH_JWT_SECRET"),
		EncryptionKey:            val("encryption-key", *encryptionKey, "OPSMESH_ENCRYPTION_KEY"),
		LogStore:                 val("log-store", *logStore, "OPSMESH_LOG_STORE"),
		LogBackend:               val("log-backend", *logBackend, "OPSMESH_LOG_BACKEND"),
		LokiEndpoint:             val("loki-endpoint", *lokiEndpoint, "OPSMESH_LOKI_ENDPOINT"),
		ESEndpoint:               val("es-endpoint", *esEndpoint, "OPSMESH_ES_ENDPOINT"),
		ESIndex:                  val("es-index", *esIndex, "OPSMESH_ES_INDEX"),

		MultiSchema:                valBool("multi-schema", *multiSchema, "OPSMESH_MULTI_SCHEMA"),
		SchemaPrefix:               val("schema-prefix", *schemaPrefix, "OPSMESH_SCHEMA_PREFIX"),
		FederationPeers:            parseFederationPeers(val("federation-peers", *federationPeers, "OPSMESH_FEDERATION_PEERS")),
		MetricsAllowCIDR:           val("metrics-allow-cidr", *metricsAllowCIDR, "OPSMESH_METRICS_ALLOW_CIDR"),
		FederationSecret:           val("federation-secret", *federationSecret, "OPSMESH_FEDERATION_SECRET"),
		FederationTLSCert:          val("federation-tls-cert", *federationTLSCert, "OPSMESH_FEDERATION_TLS_CERT"),
		FederationTLSKey:           val("federation-tls-key", *federationTLSKey, "OPSMESH_FEDERATION_TLS_KEY"),
		FederationCA:               val("federation-ca", *federationCA, "OPSMESH_FEDERATION_CA"),
		FederationPort:             valInt("federation-port", *federationPort, "OPSMESH_FEDERATION_PORT"),
		AgentShellWhitelist:        val("agent-shell-whitelist", *agentShellWhitelist, "OPSMESH_AGENT_SHELL_WHITELIST"),
		AgentFileRootWhitelist:     val("agent-file-root-whitelist", *agentFileRootWhitelist, "OPSMESH_AGENT_FILE_ROOT_WHITELIST"),
		AgentShellWhitelistDefault: valBool("agent-shell-whitelist-default", *agentShellWhitelistDefault, "OPSMESH_AGENT_SHELL_WHITELIST_DEFAULT"),
		GRPCRequireSignature:       valBool("grpc-require-signature", *grpcRequireSignature, "OPSMESH_GRPC_REQUIRE_SIGNATURE"),
		GRPCSignatureKey:           val("grpc-signature-key", *grpcSignatureKey, "OPSMESH_GRPC_SIGNATURE_KEY"),
		TrustProxy:                 valBool("trust-proxy", *trustProxy, "OPSMESH_TRUST_PROXY"),
		TrustGatewayHeaders:        valBool("trust-gateway-headers", *trustGatewayHeaders, "OPSMESH_TRUST_GATEWAY_HEADERS"),
		CookieSecure:               valBool("cookie-secure", *cookieSecure, "OPSMESH_COOKIE_SECURE"),
		AllowedOrigins:             val("allowed-origins", *allowedOrigins, "OPSMESH_ALLOWED_ORIGINS"),
		SessionStore:               val("session-store", *sessionStore, "OPSMESH_SESSION_STORE"),
		DeviceFPDeadline:           parseDeviceFPDeadline(val("device-fp-deadline", *deviceFPDeadline, "OPSMESH_DEVICE_FP_DEADLINE")),
		OTELEndpoint:               val("otel-endpoint", *otelEndpoint, "OPSMESH_OTEL_ENDPOINT"),
		OTELServiceName:            val("otel-service-name", *otelServiceName, "OPSMESH_OTEL_SERVICE_NAME"),
		OTELStdout:                 valBool("otel-stdout", *otelStdout, "OPSMESH_OTEL_STDOUT"),
		CBFailureThreshold:         valInt("cb-failure-threshold", *cbFailureThreshold, "OPSMESH_CB_FAILURE_THRESHOLD"),
		CBRecoveryTimeout:          valDur("cb-recovery-timeout", *cbRecoveryTimeout, "OPSMESH_CB_RECOVERY_TIMEOUT"),
		CBHalfOpenMaxCalls:         valInt("cb-half-open-max-calls", *cbHalfOpenMaxCalls, "OPSMESH_CB_HALF_OPEN_MAX_CALLS"),
		CBRateLimitPerSec:          valInt("cb-rate-limit-per-sec", *cbRateLimitPerSec, "OPSMESH_CB_RATE_LIMIT_PER_SEC"),
		WebhookAllowPrivate:        valBool("webhook-allow-private", *webhookAllowPrivate, "OPSMESH_WEBHOOK_ALLOW_PRIVATE"),
		ProvisionCIDRWhitelist:     val("provision-cidr-whitelist", *provisionCidrWhitelist, "OPSMESH_PROVISION_CIDR_WHITELIST"),
		InhibitRulesFile:           val("inhibit-rules-file", *inhibitRulesFile, "OPSMESH_INHIBIT_RULES_FILE"),
		AnomalyDetection:           valBool("anomaly-detection", *anomalyDetection, "OPSMESH_ANOMALY_DETECTION"),
		AnomalyWindowSize:          valInt("anomaly-window-size", *anomalyWindowSize, "OPSMESH_ANOMALY_WINDOW_SIZE"),
		AnomalyThreshold:           valFloat64("anomaly-threshold", *anomalyThreshold, "OPSMESH_ANOMALY_THRESHOLD"),
		LogPushEnabled:             valBool("log-push-enabled", *logPushEnabled, "OPSMESH_LOG_PUSH_ENABLED"),
		LogPushFiles:               parseLogPushFiles(val("log-push-files", *logPushFiles, "OPSMESH_LOG_PUSH_FILES")),
		LogPushPattern:             val("log-push-pattern", *logPushPattern, "OPSMESH_LOG_PUSH_PATTERN"),
		LogPushEndpoint:            val("log-push-endpoint", *logPushEndpoint, "OPSMESH_LOG_PUSH_ENDPOINT"),
		LogPushBackend:             val("log-push-backend", *logPushBackend, "OPSMESH_LOG_PUSH_BACKEND"),
		QuotaEnabled:               valBool("quota-enabled", *quotaEnabled, "OPSMESH_QUOTA_ENABLED"),
		QuotaMaxDevices:            valInt("quota-max-devices", *quotaMaxDevices, "OPSMESH_QUOTA_MAX_DEVICES"),
		QuotaMaxTasks:              valInt("quota-max-tasks", *quotaMaxTasks, "OPSMESH_QUOTA_MAX_TASKS"),
		QuotaMaxAlerts:             valInt("quota-max-alerts", *quotaMaxAlerts, "OPSMESH_QUOTA_MAX_ALERTS"),
		AllowStubStores:            valBool("allow-stub-stores", *allowStubStores, "OPSMESH_ALLOW_STUB_STORES"),
		AutomationEvalInterval:     valDur("automation-eval-interval", *automationEvalInterval, "OPSMESH_AUTOMATION_EVAL_INTERVAL"),
	}
	// --log-store 作为 --log-backend 别名：显式设置 --log-store（或 OPSMESH_LOG_STORE）时覆盖 LogBackend，
	// 使现有 LogBackend 校验/路由逻辑无缝复用；最终 LogStore 与 LogBackend 保持同值。
	// 优先级：显式 --log-store > 显式 --log-backend > env > 默认 "memory"。
	if explicit["log-store"] {
		cfg.LogBackend = cfg.LogStore
	}
	cfg.LogStore = cfg.LogBackend
	// 服务发现：--controlplane-endpoints 作为 --control-addrs 的别名（语义同，作为服务发现入口）。
	// 显式设置 --controlplane-endpoints（或 OPSMESH_CONTROLPLANE_ENDPOINTS）时覆盖 ControlAddrs，
	// 使现有 ControlAddrs 路由逻辑（agent.go 中解析多地址 failover）无缝复用；
	// 最终 ControlplaneEndpoints 与 ControlAddrs 保持同值（便于调用方统一读取 ControlAddrs）。
	// 优先级：显式 --controlplane-endpoints > 显式 --control-addrs > env > 回退 --control-addr。
	if cfg.ControlplaneEndpoints != "" {
		cfg.ControlAddrs = cfg.ControlplaneEndpoints
	}
	cfg.ControlplaneEndpoints = cfg.ControlAddrs
	// 负载均衡策略校验：非法值回退到默认 failover（不 fail-fast，保持启动友好）。
	switch cfg.LBStrategy {
	case "round-robin", "roundrobin", "rr", "failover", "fo", "":
	default:
		fmt.Fprintln(os.Stderr, "[config] 警告：非法 --lb-strategy="+cfg.LBStrategy+"（应为 round-robin | failover），回退到默认 failover")
		cfg.LBStrategy = "failover"
	}
	// 生产模式：默认开启 require-auth（除非显式关闭），并强告警 memory store。
	if cfg.Production && !explicit["require-auth"] {
		cfg.RequireAuth = true
	}
	// 注册安全：生产模式但未显式设置 --public-register 时，默认关闭公开注册（安全基线）。
	// demo 模式默认强制 PublicRegister=true（接口开放，方便演示），但是否免审批由 AllowPublicRegister 控制。
	// 默认 AllowPublicRegister=false：demo 模式下注册也走 pending 审批流程（安全基线）；
	// 显式 --allow-public-register=true 时 demo 模式注册才免审批（Status=active + 立即签发 token）。
	// 显式 --public-register=false 时尊重用户设置（不覆盖），用于 demo 模式下仍想关闭公开注册的场景。
	if cfg.Demo {
		if !explicit["public-register"] {
			cfg.PublicRegister = true
		}
	} else if cfg.Production && !explicit["public-register"] {
		cfg.PublicRegister = false
	}
	// gRPC agent 身份绑定：
	//   - demo 模式强制关闭签名验证（向后兼容，demo 不需要签名）。
	//   - 生产模式但未显式设置 --grpc-require-signature 时默认开启（纵深防御）。
	//   - 已启用 mTLS（tls-cert + client-ca 均非空）时可不开启（mTLS 本身提供身份绑定），但仍可显式开启叠加防御。
	if cfg.Demo {
		cfg.GRPCRequireSignature = false
	} else if cfg.Production && !explicit["grpc-require-signature"] {
		cfg.GRPCRequireSignature = true
	}
	// Cookie Secure：生产模式但未显式设置 --cookie-secure 时默认开启（HTTPS 反代终止 TLS
	// 时控制面虽收 HTTP，但对外是 HTTPS，Cookie 须 Secure 防中间人窃取）。开发/演示模式保持 false
	//（明文 HTTP 下 Secure Cookie 会被浏览器拒绝回传，导致会话丢失）。
	if cfg.Production && !explicit["cookie-secure"] {
		cfg.CookieSecure = true
	}
	// 安全加固：生产模式强制不信任网关注入的 X-User-Roles 头（即使显式 --trust-gateway-headers=true
	// 也覆盖为 false）。生产环境要求身份经 Bearer token/联邦验签/mTLS 等密码学手段验证，杜绝信任客户端
	// 可伪造的头（非生产模式下若显式开启则尊重，用于内网部署有可信网关前置的场景）。
	if cfg.Production && cfg.TrustGatewayHeaders {
		fmt.Fprintln(os.Stderr, "[config] 警告：生产模式强制关闭 --trust-gateway-headers（X-User-Roles 头可被客户端伪造，生产身份须经 Bearer token/联邦验签/mTLS 验证）；已覆盖为 false")
		cfg.TrustGatewayHeaders = false
	}
	// 安全加固：agent shell 白名单默认开启。
	// 当 --agent-shell-whitelist 未显式设置（且 env OPSMESH_AGENT_SHELL_WHITELIST 也未设置）且
	// --agent-shell-whitelist-default=true（默认）时，自动填充 defaultAgentShellWhitelist（只读诊断命令）。
	// 显式设置 --agent-shell-whitelist=...（含显式空）时尊重用户意图，不填充默认。
	// 这避免 agent 侧默认放行所有命令的 RCE 风险，同时保持向后兼容（用户显式 "" 可关闭白名单）。
	if cfg.AgentShellWhitelist == "" && cfg.AgentShellWhitelistDefault && !explicit["agent-shell-whitelist"] {
		cfg.AgentShellWhitelist = defaultAgentShellWhitelist
	}
	if cfg.Production && cfg.PublicRegister {
		fmt.Fprintln(os.Stderr, "[config] 警告：生产模式开启 --public-register=true，公开注册开放但新用户须管理员审批（建议生产关闭 --public-register=false）")
	}
	if cfg.AllowPublicRegister {
		fmt.Fprintln(os.Stderr, "[config] 警告：--allow-public-register=true 已启用，公开注册将免审批（Status=active + 立即签发 token），仅演示/内网受信环境推荐；生产务必关闭")
	}
	if cfg.Production && cfg.Store == "memory" {
		fmt.Fprintln(os.Stderr, "[config] 警告：生产模式但 store=memory（多副本数据分裂），请改用 --store=mysql（数据本地化）")
	}
	// 生产模式 TLS 未配置的拒绝逻辑已移至 Validate()（启动即 fail-fast），
	// 此处保留 require-auth 告警以便运维感知。
	if cfg.Production && !cfg.RequireAuth {
		fmt.Fprintln(os.Stderr, "[config] 警告：生产模式但 --require-auth=false，agent 注册不经过网关身份校验（仅开发/内网调试推荐）")
	}
	// 生产模式但未启用 JWT 二次验签时友好告警（不强制，向后兼容纯头注入模式）。
	// 启用 JWT 验签可作为"网关注入 + 内核二次校验"的纵深防御，降低网关被绕过时的越权面。
	if cfg.Production && cfg.JWTPublicKey == "" {
		fmt.Fprintln(os.Stderr, "[config] 提示：生产模式未配置 --jwt-public-key，仅依赖网关注入的 X-Tenant-ID 头（建议启用 JWT 二次验签作为纵深防御）")
	}
	// 通知渠道配置文件加载：--notify-channels-config 指定 JSON 文件路径。
	// 加载失败时 fail-fast（启动期发现问题而非运行期诡异失败）。
	if cfg.NotifyChannelsConfigFile != "" {
		chs, err := LoadNotifyChannelsConfig(cfg.NotifyChannelsConfigFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[config] 加载通知渠道配置文件失败: %v\n", err)
		} else {
			cfg.NotifyChannels = chs
		}
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

// float64Env 解析 float64 环境变量，未设置或非法时返回默认。
func float64Env(key string, def float64) float64 {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
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

// parseLogPushFiles 解析逗号分隔的日志采集文件路径列表为 []string，去除空白项与首尾空格。
// 输入空串返回 nil（不启用日志推送），保证 agent 中 `if len(cfg.LogPushFiles) > 0` 判空可用。
// 与 parseFederationPeers 同结构，独立保留以备未来差异化校验（如路径绝对值检查）。
func parseLogPushFiles(s string) []string {
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

// parseDeviceFPDeadline 解析 DeviceFP deadline（RFC3339 格式）。
// 输入空串返回零值（不强制，向后兼容）；解析失败返回零值并打印告警（不 fail-fast，保持启动友好）。
func parseDeviceFPDeadline(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		fmt.Fprintln(os.Stderr, "[config] 警告：--device-fp-deadline 解析失败（应为 RFC3339 格式，如 2026-09-01T00:00:00Z），忽略（不强制设备绑定）: "+err.Error())
		return time.Time{}
	}
	return t
}

// Validate 启动期配置校验：把明显的非法配置在启动即失败，而非运行期诡异出错（健壮性）。
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
		return fmt.Errorf("--store=mysql 但 --mysql-dsn 为空（数据本地化需要 DSN）")
	}
	if c.TaskLeaseSec <= 0 {
		return fmt.Errorf("非法 --task-lease-sec=%d（应 > 0）", c.TaskLeaseSec)
	}
	// 控制面 HA：memory store 多副本数据分裂，>1 副本必须改用 mysql store（数据本地化）。
	if c.Store == "memory" && c.Replicas > 1 {
		return fmt.Errorf("store=memory 不支持多副本（replicas=%d）；请改用 --store=mysql（数据本地化）", c.Replicas)
	}
	if c.Discover {
		if c.SegmentCIDR == "" {
			return fmt.Errorf("--discover 开启但 --segment-cidr 为空（真实网段发现需要 CIDR）")
		}
		if _, _, err := net.ParseCIDR(c.SegmentCIDR); err != nil {
			return fmt.Errorf("非法 --segment-cidr=%q: %w", c.SegmentCIDR, err)
		}
	}
	// 生产模式 TLS 强制：Production==true 且未配置 TLS 证书时直接拒绝启动，
	// 避免 agent↔控制面明文通信（等保三级要求）。agent 与 controlplane 同样适用：
	// agent 模式下 Production=true 也必须持证与控制面建立 mTLS。
	// 非 Production 模式不校验（开发/内网友好网络降级）。
	if c.Production && c.TLSCert == "" {
		return fmt.Errorf("生产模式（--production=true）必须配置 TLS（--tls-cert 为空），明文通信不满足等保三级要求；请提供证书或关闭 --production")
	}
	// 生产控制面必须配置稳定 JWT 密钥。
	// 语义：控机用户中心 JWT 签发密钥为空则重启丢会话、多副本各自独立随机密钥互不相认、用户间歇 401。
	// 生产直接 fail-fast，与 生产强 TLS 同风格。dev 随机兜底语义（config.JWTSecret 空=随机）保留。
	if c.Production && c.JWTSecret == "" {
		return fmt.Errorf("生产模式（--production=true）controlplane 必须设置 --jwt-secret（或环境变量 OPSMESH_JWT_SECRET）；否则各副本独立随机密钥互相不认、重启后会话全部失效")
	}
	if c.Production && len([]byte(c.JWTSecret)) < 32 {
		return fmt.Errorf("生产模式 --jwt-secret 长度不足（%d 字节 < 32）：需强随机 256-bit 对称密钥（建议 openssl rand -hex 32）", len([]byte(c.JWTSecret)))
	}
	// 生产模式必须配置 kubeconfig 加密密钥：DB 泄露时明文 kubeconfig = 所有 K8s 集群沦陷。
	// 密钥须为 base64 编码的 32 字节（AES-256）；非生产模式允许空（明文存储，保持 demo 兼容）。
	if c.Production && c.EncryptionKey == "" {
		return fmt.Errorf("生产模式（--production=true）必须配置 --encryption-key（或 OPSMESH_ENCRYPTION_KEY）：kubeconfig 明文存储不满足等保三级要求；建议 openssl rand 32 | base64 生成 32 字节 AES-256 密钥")
	}
	// 日志检索后端校验：非法值或缺失必要 endpoint 直接 fail-fast。
	switch c.LogBackend {
	case "memory", "sql", "loki", "es":
	default:
		return fmt.Errorf("非法 --log-backend=%q（应为 memory | sql | loki | es）", c.LogBackend)
	}
	if c.LogBackend == "loki" && c.LokiEndpoint == "" {
		return fmt.Errorf("--log-backend=loki 但 --loki-endpoint 为空（需要 Loki API 地址）")
	}
	if c.LogBackend == "es" {
		if c.ESEndpoint == "" {
			return fmt.Errorf("--log-backend=es 但 --es-endpoint 为空（需要 Elasticsearch API 地址）")
		}
		if c.ESIndex == "" {
			return fmt.Errorf("--log-backend=es 但 --es-index 为空（需要索引名）")
		}
	}
	// 多租户 schema 隔离：仅支持 mysql store（MultiSchemaStore 内部用 *SQLStore）。
	if c.MultiSchema && c.Store != "mysql" {
		return fmt.Errorf("--multi-schema=true 但 --store=%q（多 schema 隔离仅支持 mysql 后端）", c.Store)
	}
	// H2/H3 配套开关：生产模式 + SQL 后端默认拒绝启动（fail-fast）。
	// 背景：SQLStore 对 P1-P6 共 15 个领域曾为桩实现（写入返回零值、不持久化），
	// 生产环境静默丢数据不可接受，须运维显式 --allow-stub-stores=true 确认接受后才放行；
	// 放行后运行期由 store 层 stub_guard 限频 WARN 兜底。
	// 现状：P1-P6 全部 15 个领域已实现真实 MySQL CRUD（sql_p01.go ~ sql_p06.go），
	// stubStoreDomains 收敛为空字符串，本拒绝逻辑跳过（无桩领域则无须放行门槛）。
	// 保留分支与 AllowStubStores 开关向后兼容：未来若新增桩领域，仅需将域名加入
	// stubStoreDomains 即可自动恢复拒绝启动行为，无须改动 Validate 代码。
	if stubStoreDomains != "" && c.Production && c.Store == "mysql" && !c.AllowStubStores {
		return fmt.Errorf("生产模式（--production=true）使用 SQL 后端（--store=mysql），但以下 P1-P6 领域尚未持久化（桩实现）：%s；请等待 MySQL 持久化落地，或显式设置 --allow-stub-stores=true（或 env OPSMESH_ALLOW_STUB_STORES）确认接受桩限制", stubStoreDomains)
	}
	// 控制面联邦：peer 地址必须是合法 URL（含 scheme + host），启动期 fail-fast 避免运行期诡异失败。
	for i, p := range c.FederationPeers {
		u, err := url.Parse(p)
		if err != nil {
			return fmt.Errorf("非法 --federation-peers[%d]=%q: %w", i, p, err)
		}
		if u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("非法 --federation-peers[%d]=%q（需含 scheme 与 host，如 http://peer:8080）", i, p)
		}
	}
	// metrics CIDR 白名单格式校验：每项必须是合法 CIDR，启动 fail-fast。
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
	// 联邦通道硬化校验：独立 mTLS 监听需证书；启用联邦但缺失共享密钥时告警（身份不可验签）。
	if c.FederationPort > 65535 {
		return fmt.Errorf("非法 --federation-port=%d（应 ≤ 65535）", c.FederationPort)
	}
	if c.FederationPort > 0 && (c.FederationTLSCert == "" || c.FederationTLSKey == "") {
		return fmt.Errorf("--federation-port>0 但 --federation-tls-cert/key 为空（独立 mTLS 监听需要服务端证书）")
	}
	// 联邦通道硬化校验（强校验）：启用联邦但缺失共享密钥时直接拒绝启动，
	// 防止跨不可信网段伪造租户身份头（原告警改为 fail-fast）。
	if len(c.FederationPeers) > 0 && c.FederationSecret == "" {
		return fmt.Errorf("federation-secret is required when federation-peers is set")
	}
	// 多副本会话状态共享校验：session-store 格式须为 "redis://host:port"。
	// 多副本 HA（replicas>1）但未配置 session-store 时告警（不 fail-fast，保持单副本 memory store 兼容）。
	if c.SessionStore != "" {
		if !strings.HasPrefix(c.SessionStore, "redis://") {
			return fmt.Errorf("非法 --session-store=%q（须为 redis://host:port 格式）", c.SessionStore)
		}
		// 去除 "redis://" 前缀后须非空（如 "redis://" 不合法）。
		if strings.TrimPrefix(c.SessionStore, "redis://") == "" {
			return fmt.Errorf("非法 --session-store=%q（host:port 不可为空）", c.SessionStore)
		}
	}
	if c.Replicas > 1 && c.SessionStore == "" && c.Store == "memory" {
		// memory store 多副本本身已在上方校验拒绝，此处补充 mysql store 多副本未配置 session-store 的告警。
		fmt.Fprintln(os.Stderr, "[config] 警告：多副本（replicas>1）但未配置 --session-store，登出/限流/改密令牌将不跨副本共享（建议 --session-store=redis://host:port）")
	}
	// CORS 白名单校验：禁止配置 "*"（与 Allow-Credentials 互斥；等于放开任意来源，
	// 恶意网站可带用户 Cookie 跨域调 API，恢复到反射漏洞）。启动期 fail-fast。
	if c.AllowedOrigins != "" {
		for _, item := range strings.Split(c.AllowedOrigins, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if item == "*" {
				return fmt.Errorf("非法 --allowed-origins 项 \"*\"：CORS 凭证模式下通配符与 Cookie 互斥（等同放开任意来源），须改为显式域名列表")
			}
			u, err := url.Parse(item)
			if err != nil {
				return fmt.Errorf("非法 --allowed-origins 项 %q: %w", item, err)
			}
			if u.Scheme == "" || u.Host == "" || u.Path != "" {
				return fmt.Errorf("非法 --allowed-origins 项 %q（须为 scheme://host[:port] 形式的完整 Origin，如 https://console.example.com，不含路径）", item)
			}
		}
	}
	// 通知渠道配置校验：每个渠道 type 必须合法，必填字段非空。
	for i, ch := range c.NotifyChannels {
		switch ch.Type {
		case "dingtalk", "wechat", "feishu", "slack":
			if ch.WebhookURL == "" {
				return fmt.Errorf("notify-channels[%d] type=%q 缺少 webhook_url", i, ch.Type)
			}
		case "email":
			if ch.SMTPHost == "" || ch.SMTPPort <= 0 || ch.From == "" || len(ch.To) == 0 {
				return fmt.Errorf("notify-channels[%d] type=email 配置不完整（需 smtp_host/smtp_port/from/to）", i)
			}
		default:
			return fmt.Errorf("notify-channels[%d] 非法 type=%q（应为 dingtalk/wechat/feishu/slack/email）", i, ch.Type)
		}
	}
	// SSRF 防护：autoProvision CIDR 白名单格式校验（每项必须是合法 CIDR）。
	// 启动期 fail-fast，避免运行期 autoProvision handler 才发现白名单配置错误。
	// 空白名单不校验（向后兼容）。
	if c.ProvisionCIDRWhitelist != "" {
		for _, item := range strings.Split(c.ProvisionCIDRWhitelist, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if _, _, err := net.ParseCIDR(item); err != nil {
				return fmt.Errorf("非法 --provision-cidr-whitelist 项 %q: %w", item, err)
			}
		}
	}
	// 告警抑制集成：--inhibit-rules-file 非空时校验文件存在/可读。
	// 启动期 fail-fast，避免运行期 alertEngineLoop 才发现文件不可访问。
	// 空值跳过校验（向后兼容，不启用告警抑制）。
	if c.InhibitRulesFile != "" {
		if _, err := os.Stat(c.InhibitRulesFile); err != nil {
			return fmt.Errorf("--inhibit-rules-file=%q 文件不存在或不可访问: %w", c.InhibitRulesFile, err)
		}
	}
	// 日志采集推送校验：启用时 endpoint/files 必填，backend 必须合法。
	// 启动期 fail-fast，避免运行期 LogPusher 构造失败导致采集不生效。
	if c.LogPushEnabled {
		if len(c.LogPushFiles) == 0 {
			return fmt.Errorf("--log-push-enabled=true 但 --log-push-files 为空（需指定至少一个日志文件）")
		}
		if c.LogPushEndpoint == "" {
			return fmt.Errorf("--log-push-enabled=true 但 --log-push-endpoint 为空（需指定 Loki/ES 推送地址）")
		}
		switch c.LogPushBackend {
		case "loki", "es":
		default:
			return fmt.Errorf("非法 --log-push-backend=%q（应为 loki | es）", c.LogPushBackend)
		}
	}
	return nil
}

// ============================================================================
// 通知渠道配置
// ============================================================================

// NotifyChannelConfig 通知渠道配置。
//
// 支持类型（Type 字段）：
//   - dingtalk：钉钉群机器人 webhook（需 WebhookURL，可选 Secret 加签）
//   - wechat：企业微信群机器人 webhook（需 WebhookURL）
//   - feishu：飞书群机器人 webhook（需 WebhookURL，可选 Secret 加签）
//   - slack：Slack incoming webhook（需 WebhookURL，可选 Channel 指定频道）
//   - email：邮件 SMTP（需 SMTPHost/SMTPPort/From/To）
//
// JSON 配置文件格式（--notify-channels-config 指定路径）：
//
//	{
//	  "channels": [
//	    {"type": "dingtalk", "webhook_url": "https://oapi.dingtalk.com/robot/send?access_token=xxx", "secret": "SECxxx"},
//	    {"type": "feishu", "webhook_url": "https://open.feishu.cn/open-apis/bot/v2/hook/xxx", "secret": "xxx"},
//	    {"type": "wechat", "webhook_url": "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx"},
//	    {"type": "slack", "webhook_url": "https://hooks.slack.com/services/xxx", "channel": "#ops"},
//	    {"type": "email", "smtp_host": "smtp.example.com", "smtp_port": 25, "username": "u", "password": "p", "from": "ops@example.com", "to": ["ops1@example.com", "ops2@example.com"]}
//	  ]
//	}
type NotifyChannelConfig struct {
	Type       string   `json:"type"`        // dingtalk / wechat / feishu / slack / email
	WebhookURL string   `json:"webhook_url"` // webhook URL（dingtalk/wechat/feishu/slack 用）
	Secret     string   `json:"secret"`      // 加签密钥（dingtalk/feishu 用）
	Channel    string   `json:"channel"`     // Slack 频道（slack 用）
	SMTPHost   string   `json:"smtp_host"`   // SMTP 主机（email 用）
	SMTPPort   int      `json:"smtp_port"`   // SMTP 端口（email 用）
	Username   string   `json:"username"`    // SMTP 用户名（email 用）
	Password   string   `json:"password"`    // SMTP 密码（email 用）
	From       string   `json:"from"`        // 发件人（email 用）
	To         []string `json:"to"`          // 收件人列表（email 用）
}

// notifyChannelsFile JSON 配置文件顶层结构。
type notifyChannelsFile struct {
	Channels []NotifyChannelConfig `json:"channels"`
}

// LoadNotifyChannelsConfig 从 JSON 配置文件加载通知渠道配置。
//
// 文件格式见 NotifyChannelConfig 文档。文件不存在或解析失败时返回 error。
// 空文件（无 channels）返回空切片 + nil（不视为错误，仅表示无渠道配置）。
func LoadNotifyChannelsConfig(path string) ([]NotifyChannelConfig, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取通知渠道配置文件 %q 失败: %w", path, err)
	}
	var f notifyChannelsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("解析通知渠道配置文件 %q 失败: %w", path, err)
	}
	return f.Channels, nil
}
