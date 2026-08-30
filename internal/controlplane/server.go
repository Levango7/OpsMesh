// Package controlplane 实现控制面：HTTP(B/S) 仪表盘 + gRPC 注册通道 + metrics。
// 通过 --mode=controlplane 启动。
//   - gRPC 9090：承载 agent 注册/心跳/拉任务/上报结果（真实 gRPC，JSON codec，见 grpc.go）。
//   - HTTP 8080：B/S 仪表盘 + GET /api/v1/devices（人工查看）+ POST /api/v1/tasks（内部下发入口）。
//   - metrics 9091：极简文本指标（观测）。
//
// 持久化后端由 --store 选择（默认 memory，可选 mysql）。
package controlplane

import (
	"context"
	cryptoRand "crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	"opsmesh/internal/alertengine"
	"opsmesh/internal/approval"
	"opsmesh/internal/cmdb"
	"opsmesh/internal/config"
	"opsmesh/internal/controlplane/factory"
	"opsmesh/internal/cron"
	"opsmesh/internal/deploy"
	"opsmesh/internal/events"
	"opsmesh/internal/helm"
	"opsmesh/internal/k8s"
	"opsmesh/internal/logstore"
	"opsmesh/internal/logx"
	"opsmesh/internal/metrics"
	"opsmesh/internal/notify"
	"opsmesh/internal/orchestration"
	"opsmesh/internal/otelx"
	"opsmesh/internal/platform"
	"opsmesh/internal/proto"
	"opsmesh/internal/secrets"
	"opsmesh/internal/store"
	"opsmesh/internal/tlsutil"
)

// Server 控制面服务（HTTP + gRPC + metrics）。
type Server struct {
	cfg           *config.Config
	httpPort      int
	grpcPort      int
	metricsPort   int
	requireAuth   bool
	tlsCert       string
	tlsKey        string
	clientCA      string
	shutdownWait  time.Duration
	bus           events.Bus             // 审计/告警事件总线（默认 noop）
	metrics       *metrics.M             // 观测指标
	store         store.Store            // 持久化存储（供 CMDB 等子系统复用）
	cmdbHandler   *cmdb.Handler          // CMDB 处理器（Phase 1）
	logHandler    *logstore.Handler      // M6 日志检索处理器
	deployHandler *deploy.Handler        // M3 部署中心处理器
	orchHandler   *orchestration.Handler // M5 作业编排中心处理器
	// 告警推送去重（notifyLoop 防重复）：按告警指纹（AlertID）记录已推送集合，
	// 替代旧版全局单一时间高水位 lastAlertSent——时间水位对乱序插入/跨租户告警/
	// 多副本时钟偏差不免疫（任一租户推送后水位前移，其它租户 CreatedAt 更早的
	// 告警被永久跳过）；指纹去重对上述场景全部免疫。
	// key=AlertID，value=推送成功时间；推送成功才写入（失败不标记，下轮重试）。
	// 条目保留 24h 后由 notifyLoop 周期清理（有界防泄漏；远大于聚合窗口 5min，
	// 足够覆盖任何重试需求）。
	alertSentMu sync.Mutex
	alertSent   map[string]time.Time
	// 告警聚合器：同源告警 5 分钟聚合 + 级别抑制（critical 抑制同源 warning）。
	alertAggr *notify.AlertAggregator
	// 告警多通道（Webhook + Email）。NewServer 从 cfg 构造；notifyLoop 每次推送复用。
	alertChannels *notify.Channels
	// SSE 实时推送：订阅者集合与互斥保护。
	// 每个 SSE 连接对应一个 buffered chan，publishEvent 非阻塞广播到所有订阅者。
	// 慢消费者（缓冲满）丢弃事件，避免一个慢客户端拖垮广播（设计取舍）。
	eventMu   sync.RWMutex
	eventSubs map[chan SSEEvent]struct{}

	// 控制面联邦管理器（nil=未启用联邦）。
	// 由 NewServer 在 cfg.FederationPeers 非空时构造；启用后路由层注册 /api/v1/federation/* 端点。
	fed *FederationManager

	// 用户中心 JWT 签发密钥（HS256，来自 config.JWTSecret；空=随机生成）。
	// 用于 auth.go 中登录/注册后签发 token。空时 NewServer 随机生成 32 字节密钥
	// （重启后旧 token 失效，仅开发/单实例适用）。
	jwtSecret []byte

	// apiKeyMgr API Key 管理引擎（H5 认证接入，复用 internal/platform）。
	// 由 NewServer 用 s.store 构造；authorizeByAPIKey 经 ValidateKey（SHA-256 hash 比对 +
	// Enabled/过期校验）与 HasScope（resource:action 与 Scopes 同构直比）完成
	// "Bearer om_*"/X-API-Key 凭据的认证与授权。
	// nil=未构造（部分测试直构 Server 时可能为 nil）：此时 API Key 分支一律按无效 key 拒绝，
	// 绝不因 nil panic 产生 500 而意外绕过认证闸。
	apiKeyMgr *platform.APIKeyManager

	// apiKeyUsage API Key 使用计数（LastUsedAt 内存聚合 MVP）。
	// key=APIKey.ID，value=成功认证累计次数；由 apiKeyUsageMu 保护。
	// 设计约束（FIXPLAN §2.3.3）：仅内存累计、禁止每请求同步写 store（Memory 锁竞争/
	// MultiSchema 跨 schema 路由/SQL 写放大）；后台批量刷写 goroutine 本期不做，
	// 重启丢失计数可接受。nil map 时 recordAPIKeyUsage 静默跳过（容错未初始化的测试 Server）。
	apiKeyUsageMu sync.Mutex
	apiKeyUsage   map[string]int64

	// kubeconfig 静态加密密钥（AES-256-GCM，来自 config.EncryptionKey base64 解码）。
	// k8s_cluster.go 的 encryptKubeconfig/decryptKubeconfig 用此密钥对 kubeconfig 做加解密。
	// 空=未配置（非生产模式），加解密退化为明文透传（保持 demo 兼容）；生产模式由 config.Validate 强制非空。
	encryptionKey []byte

	// loginGuard 登录/注册防爆破 + 限流。
	// 失败计数 + 账号锁定经 SessionStore 共享（多副本 HA 下任一副本触发锁定后其他副本也拒绝）；
	// IP 令牌桶限流保留进程内（多副本各自限流，副本数 N 时实际阈值 N*burst，可接受）。
	loginGuard *loginGuard

	// 会话状态存储（JWT 黑名单/限流计数/改密令牌）。
	// 默认 InProcessSessionStore（单副本/demo）；多副本 HA 配置 --session-store=redis:// 时用 RedisSessionStore。
	// 登出时 jti 加入黑名单，userFromToken 校验时检查；多副本经 Redis 共享使登出全局生效。
	sessionStore store.SessionStore

	// DeviceFP deadline：超过此时刻签发的 refresh token 必须绑定 DeviceFP（非空）。
	// 零值=不强制（向后兼容）；非零=渐进式强制设备绑定。
	// 由 NewServer 从 cfg.DeviceFPDeadline 初始化。
	deviceFPDeadline time.Time

	// clusterMgr K8s 多集群连接管理器（Phase 3）。
	// 由 NewServer 构造；用户创建/更新集群时 AddCluster，删除时 RemoveCluster，测试连接时 TestCluster。
	clusterMgr *k8s.ClusterManager

	// OTel 链路追踪：TracerProvider 优雅关闭函数。
	// 由 NewServer 调用 otelx.Init 构造；Start 优雅退出时调用以 flush 残留 span。
	// nil=未启用 OTel（endpoint 空且 stdout=false），退出时跳过。
	otelShutdown otelx.ShutdownFunc

	// API 限流器：按 IP 令牌桶限流，超过阈值返回 429 Too Many Requests。
	// nil=禁用限流（CBRateLimitPerSec<=0，向后兼容）。
	rateLimiter *rateLimiter

	// M2 集成：告警规则引擎 + 静默器 + 聚合器 + 通知管理器。
	// alertEngine 持有 alertengine.AlertRule 集合，周期评估设备指标触发告警事件；
	// alertSilencer 按标签匹配 + 时间窗口抑制告警事件；
	// alertAggregator 按 groupBy 字段聚合告警事件（避免告警风暴）；
	// alertNotifier 多渠道推送（钉钉/企业微信/飞书/Slack/邮件/Webhook）+ 模板渲染 + 重试 + 去重。
	// 全部在 NewServer 构造；nil=未启用 M2 告警引擎（向后兼容，仅依赖 notifyLoop 推送）。
	alertEngine     *alertengine.Engine
	alertSilencer   *alertengine.Silencer
	alertAggregator *alertengine.Aggregator
	alertNotifier   *notify.Notifier

	// 告警通道密钥外置：SecretProvider 实例（env/file/vault/chain）。
	// 由 NewServer 调用 secrets.FromConfig 构造；nil=未启用密钥外置（向后兼容）。
	// 注入到 alertNotifier 与 buildChannel，使渠道构造支持 ${key} 格式密钥引用解析。
	// 构造失败时：生产模式 fail-fast，非生产模式打 Warning 继续（保持本地体验兼容）。
	secretProvider secrets.SecretProvider

	// 集成：告警抑制器（基于活跃告警状态的动态抑制）。
	// alertInhibitor 持有抑制规则与活跃告警集合，告警评估前检查 IsInhibited 跳过通知，
	// 评估后对 firing 告警调用 TrackActive，告警恢复时调用 RemoveActive。
	// 与 alertSilencer 的区别：
	//   - alertSilencer：基于时间窗口的静态抑制（运维主动配置静默规则）。
	//   - alertInhibitor：基于活跃告警状态的动态抑制（父告警存在时自动抑制子告警）。
	// nil=未启用告警抑制（向后兼容，--inhibit-rules-file 为空时），评估流程跳过抑制检查。
	alertInhibitor *alertengine.AlertInhibitor

	// 异常检测引擎：基于基线偏离的告警规则（滑动窗口 Z-Score + EWMA 突变检测）。
	// 由 NewServer 在 cfg.AnomalyDetection=true 时构造；alertEngineLoop 对设备指标调用 Evaluate，
	// 异常时产生 AnomalyAlert 并转换为 AlertEvent 进入现有告警链（静默/聚合/通知）。
	// nil=未启用异常检测（向后兼容，--anomaly-detection=false 时），评估流程跳过异常检测。
	anomalyEngine *alertengine.AnomalyEngine

	// M3 集成：Helm 应用商店（仓库管理 + Release 管理 + 预置目录）。
	// helmRepo 管理 Chart 仓库集合（add/remove/list/search），helmRelease 管理 Release 生命周期
	// （install/upgrade/rollback/uninstall/list/history）；两者通过 helm CLI 调用 helm 命令行。
	// nil=未启用 Helm 应用商店（向后兼容）；当前实现总是构造，helm CLI 不存在时 API 返回 503。
	helmRepo    *helm.RepoManager
	helmRelease *helm.ReleaseManager

	// M5 集成：批量运维/灰度发布 + 定时任务管理 + 审批引擎。
	// batches 持有批量/灰度发布的内存索引（重启后丢失，任务实例本身在 store 中持久化）。
	// scheduleMgr 维护定时任务元数据（ScheduleEntry CRUD + 暂停/恢复）。
	// approvalEngine 持有审批流定义与审批请求状态机（来自 internal/approval 包）。
	batches        *batchStore
	scheduleMgr    *cron.Manager
	approvalEngine *approval.Engine

	// M6 集成：网络拓扑缓存。
	// networkTopologyCache 持有最近一次探测的拓扑数据 + 5 分钟过期时间，
	// 由 handleNetworkTopology 在 ?refresh=true 时刷新，handleNetworkTopologyCache 读取。
	// 内存缓存（重启后丢失），不持久化到 store（拓扑数据时效性强，无需持久化）。
	networkTopologyCache *NetworkTopologyCache

	// TLS 证书热重载器（仅当 cfg.TLSWatch=true 且 TLSCert/TLSKey 非空时构造）。
	// buildGRPC 创建并赋值，server_lifecycle.go 的 Start 优雅退出时调用 Close 释放 watcher。
	// nil=未启用热重载（向后兼容，证书更新需重启服务）。
	tlsReloader *tlsutil.CertificateReloader

	// cmdbCollector CMDB 定时采集器：周期从 agent 上报的设备指标采集
	// 主机/服务元信息，更新 CMDB CI。由 NewServer 构造，Start 启动 goroutine 运行 Run(ctx)。
	// nil=未启用（向后兼容，仅手动 POST /api/v1/cmdb/collect 时返回 503）。
	cmdbCollector *CMDBCollector

	// cmdbApprovalMgr CMDB 变更审批管理器：CI 创建/修改/删除走审批流，
	// 审批通过后才执行实际变更。由 NewServer 构造，路由 /api/v1/cmdb/changes/*。
	cmdbApprovalMgr *CMDBApprovalManager

	// 多租户资源配额管理器：租户级资源配额检查 + 用量统计。
	// 由 NewServer 在 cfg.QuotaEnabled=true 时构造；nil=未启用配额检查（向后兼容）。
	// 启用后设备/任务/告警创建路径调用 CheckDevice/CheckTask/CheckAlert 校验是否超额。
	// API 路由 /api/v1/quotas[/{tenantID}] 在 server_lifecycle.go Start 中注册。
	quotaMgr *QuotaManager

	// Phase 5 API 网关运行期状态：路由规则 + 限流器 + 统计。
	// 由 NewServer 构造；nil=未启用网关（向后兼容，handler 调用前由 ensureGateway 兜底）。
	// 路由规则按 tenantID 隔离，进程级内存（重启后丢失，运行期配置）。
	gateway *gatewayState
	// gatewayOnce 保证 ensureGateway 在并发调用下只构造一次 gateway 状态。
	// 即使多个 handler goroutine 同时进入 ensureGateway，也只会赋值一次。
	gatewayOnce sync.Once

	// automationEvalInterval 自动化引擎评估周期（来自 cfg.AutomationEvalInterval）。
	// server_lifecycle.go Start 中 startAutomationEvalLoop(ctx, automationEvalInterval)
	// 周期调用 processAutomationRules 评估 enabled 规则并执行命中动作。
	automationEvalInterval time.Duration

	// backupDir 灾备备份归档目录（NewServer 用 cfg.DataDir + "/backups" 初始化）。
	// handleBackupCreate 后台 goroutine 将 JSON 快照打包为 backup-<ts>-<id>.tar.gz 写入此目录；
	// handleBackupRestore 从该目录读取归档写回 store。空值时回退 os.TempDir()/opsmesh-backups。
	backupDir string
}

// startRefreshSweep 周期清理过期刷新令牌（store 持久化后改为 no-op，
// 过期清理由 consumeRefreshToken 顺带完成；保留 sweep 机制以兼容未来 store 层扩展批量清理）。
//
// 退出机制：goroutine 通过 select 监听 ctx.Done() 与 ticker.C，ctx 取消时优雅退出并 Stop ticker，
// 避免 goroutine 泄漏。调用方（Start）在收到终止信号时取消 ctx 即可让 goroutine 退出。
func (s *Server) startRefreshSweep(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Hour
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.purgeExpiredRefreshTokens()
			}
		}
	}()
}

// shutdownOTel OTel 优雅关闭：flush 残留 span 到导出器。
// 未启用 OTel（otelShutdown 为 nil 或 no-op）时直接返回，零开销。
// 用 5s 超时避免退出窗口耗尽在 OTel flush 上（BatchSpanProcessor 批量上报）。
func (s *Server) shutdownOTel() {
	if s.otelShutdown == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.otelShutdown(ctx); err != nil {
		log.Printf("controlplane: OTel shutdown 失败: %v", err)
	}
}

// shutdownTLSReloader TLS 证书热重载器优雅关闭。
// 未启用热重载（tlsReloader 为 nil）时直接返回，零开销。
// 关闭 fsnotify watcher 与退出 watchLoop goroutine，避免资源泄漏。
func (s *Server) shutdownTLSReloader() {
	if s.tlsReloader == nil {
		return
	}
	if err := s.tlsReloader.Close(); err != nil {
		log.Printf("controlplane: TLS reloader 关闭失败: %v", err)
	}
}

func NewServer(cfg *config.Config) (*Server, error) {
	// Kafka brokers/topic 经参数传入事件总线（避免 os.Setenv 并发不安全）。
	bus := events.New(cfg.EventBus, cfg.KafkaBrokers, cfg.KafkaTopic)
	st, storeErr := factory.SelectStore(cfg, bus)
	if storeErr != nil {
		// 安全加固：静默回退改 fail-fast。
		// 生产模式（cfg.Production == true）：MySQL 初始化失败直接返回错误，避免静默回退 memory
		// 导致数据丢失/分裂（多副本 memory store 各自独立，写入互不可见）。
		if cfg.Production {
			return nil, fmt.Errorf("持久化后端初始化失败（生产模式 fail-fast）: %w", storeErr)
		}
		// 非生产模式（开发/demo/测试）：打 Warning 后回退 memory，保持本地体验兼容。
		logx.Warn(context.Background(), "持久化后端初始化失败，非生产模式回退 memory（生产模式将 fail-fast）", "err", storeErr)
		st = store.NewMemoryStore().WithSecret(cfg.ProvisionSecret).WithBus(bus).WithDemo(cfg.Demo)
	}
	s := &Server{
		cfg:           cfg,
		store:         st,
		httpPort:      cfg.HTTPPort,
		grpcPort:      cfg.GRPCPort,
		metricsPort:   cfg.MetricsPort,
		requireAuth:   cfg.RequireAuth,
		tlsCert:       cfg.TLSCert,
		tlsKey:        cfg.TLSKey,
		clientCA:      cfg.ClientCA,
		shutdownWait:  cfg.ShutdownTimeout,
		bus:           bus,
		metrics:       metrics.New(),
		cmdbHandler:   factory.NewCMDBHandler(st),
		logHandler:    factory.NewLogHandler(st, cfg),
		deployHandler: factory.NewDeployHandler(st),
		orchHandler:   factory.NewOrchestrationHandler(st),
		eventSubs:     make(map[chan SSEEvent]struct{}), // SSE 订阅者集合
		alertAggr:     notify.NewAlertAggregator(),      // 告警聚合器
		alertChannels: &notify.Channels{ // 多通道（Webhook + Email）
			NotifierType: cfg.AlertNotifierType,
			WebhookURL:   cfg.AlertWebhookURL,
			Email: &notify.EmailConfig{
				Host: cfg.AlertEmailHost,
				Port: cfg.AlertEmailPort,
				User: cfg.AlertEmailUser,
				Pass: cfg.AlertEmailPass,
				From: cfg.AlertEmailFrom,
				To:   cfg.AlertEmailTo,
			},
		},
		// M2 集成：初始化告警规则引擎 + 静默器 + 聚合器 + 通知管理器。
		// 引擎使用 NoopMetricsProvider（无指标源时）；后续可注入基于 store.DeviceMetrics 的 Provider。
		// 聚合器按 deviceID + severity 分组，每组最多 100 条。
		// 通知管理器启用 5 分钟去重 + 默认重试策略。
		alertEngine:     alertengine.NewEngine(nil, nil, nil),
		alertSilencer:   alertengine.NewSilencer(nil),
		alertAggregator: alertengine.NewAggregator([]string{"deviceID", "severity"}, 100),
		alertNotifier:   notify.NewNotifier(notify.WithDedup(5*time.Minute), notify.WithRetry(nil)),
		// H5 认证接入：API Key 管理引擎（复用 store，ValidateKey/HasScope 均为只读操作）。
		apiKeyMgr: platform.NewAPIKeyManager(st),
		// LastUsedAt 内存聚合计数（MVP：仅累计不落库，见 Server.apiKeyUsage 注释）。
		apiKeyUsage: make(map[string]int64),
		// 告警推送去重集合（按 AlertID 指纹；见 Server.alertSent 注释）。
		alertSent: make(map[string]time.Time),
		// Phase 5 API 网关运行期状态（路由规则 + 限流器 + 统计）。
		gateway: newGatewayState(),
		// 自动化引擎评估周期（startAutomationEvalLoop 消费；<=0 时 loop 内部按 30s 兜底）。
		automationEvalInterval: cfg.AutomationEvalInterval,
		// 灾备备份归档目录：data-dir/backups（与 CLI backup 子命令同源，便于运维统一备份数据目录）。
		backupDir: filepath.Join(cfg.DataDir, "backups"),
	}
	// G1 鉴权修复：给 CMDB/部署/日志/编排子包 handler 注入统一鉴权回调
	// （requireTenantContext + requireProd RBAC 权限闸），堵住全域匿名可达漏洞。
	// 回调内按请求方法映射权限点（各包 RegisterRoutes 已按 read/write 语义包装）。
	s.cmdbHandler.Authorize = s.subsystemAuthorize
	s.logHandler.Authorize = s.subsystemAuthorize
	s.deployHandler.Authorize = s.subsystemAuthorize
	s.orchHandler.Authorize = s.subsystemAuthorize
	// 告警通道密钥外置：根据 cfg.SecretProvider 构造 SecretProvider 并注入到 alertNotifier。
	// cfg.SecretProvider 为空时 FromConfig 返回 (nil, nil)，不启用密钥外置（向后兼容）。
	// 构造失败时：生产模式 fail-fast（避免运行期渠道鉴权失败），非生产模式打 Warning 继续。
	secretProvider, spErr := secrets.FromConfig(cfg)
	if spErr != nil {
		if cfg.Production {
			return nil, fmt.Errorf("密钥提供者初始化失败: %w", spErr)
		}
		logx.Warn(context.Background(), "密钥提供者初始化失败，非生产模式继续（告警通道密钥引用将无法解析）", "err", spErr)
	}
	if secretProvider != nil {
		s.secretProvider = secretProvider
		// 重新构造 alertNotifier 并注入 SecretProvider（保留原有 Dedup+Retry 选项）。
		s.alertNotifier = notify.NewNotifier(
			notify.WithDedup(5*time.Minute),
			notify.WithRetry(nil),
			notify.WithSecretProvider(secretProvider),
		)
		logx.Info(context.Background(), "告警通道密钥外置已启用", "provider", secretProvider.Name())
	}
	if cfg.Demo {
		// 演示模式：主动播种 demo 拓扑，让 6 大模块在无真实 agent 时也能完整演示。
		if ms, ok := st.(*store.MemoryStore); ok {
			ms.SeedDemoTopology()
		}
		// 预置一条示例部署（created），用户点开「部署中心」即可一键「执行」看 fan-out。
		if _, err := s.deployHandler.Store().Create(context.Background(), &deploy.DeployTask{
			Name:      "deploy-nginx-demo",
			Type:      deploy.TypeScript,
			RepoURL:   "https://git.example.com/ops/nginx.git",
			TargetIDs: "dev-demo-01, dev-demo-02",
			TenantID:  "default",
			CreatedBy: "demo",
		}); err != nil {
			logx.Warn(context.Background(), "demo 部署播种失败", err)
		}
	}
	// 控制面联邦：cfg.FederationPeers 非空时构造 FederationManager，启用联邦 API。
	// nil 时路由层跳过 /api/v1/federation/* 注册（向后兼容，不影响未启用联邦的部署）。
	// 硬化：传入共享 HMAC 密钥 + 出站 mTLS 配置（nil 表示明文联邦）。
	fedClientTLS, err := tlsutil.HTTPClientTLSConfig(cfg.FederationTLSCert, cfg.FederationTLSKey, cfg.FederationCA)
	if err != nil {
		return nil, fmt.Errorf("联邦客户端 TLS 配置失败: %w", err)
	}
	s.fed = NewFederationManager(cfg.FederationPeers, st, cfg.FederationSecret, fedClientTLS)
	// 用户中心 JWT 密钥：优先用 config.JWTSecret，空则随机生成 32 字节（重启后旧 token 失效）。
	if cfg.JWTSecret != "" {
		s.jwtSecret = []byte(cfg.JWTSecret)
	} else {
		logx.Warn(context.Background(), "警告：未配置 --jwt-secret，使用随机密钥：重启后所有会话失效；多副本部署会互踢；生产环境请配置 OPSMESH_JWT_SECRET", nil)
		s.jwtSecret = make([]byte, 32)
		if _, err := cryptoRand.Read(s.jwtSecret); err != nil {
			return nil, fmt.Errorf("JWT 密钥随机生成失败: %w", err)
		}
	}
	// kubeconfig 加密密钥：base64 解码 config.EncryptionKey 为 32 字节 AES-256 密钥。
	// 空=未配置（非生产模式，Validate 已保证生产非空），加解密退化为明文透传保持 demo 兼容。
	if cfg.EncryptionKey != "" {
		key, decErr := base64.StdEncoding.DecodeString(cfg.EncryptionKey)
		if decErr != nil {
			return nil, fmt.Errorf("--encryption-key base64 解码失败: %w", decErr)
		}
		if len(key) != 32 {
			return nil, fmt.Errorf("--encryption-key 解码后须为 32 字节（AES-256），实际 %d 字节", len(key))
		}
		s.encryptionKey = key
	} else if !cfg.Production {
		logx.Warn(context.Background(), "未配置 --encryption-key，kubeconfig 将明文存储（仅开发/demo 适用，生产必须配置）", nil)
	}
	// 会话状态存储：根据 --session-store 选择 Redis 或进程内实现。
	// 多副本 HA 须配置 redis://，否则登出/限流/改密令牌不跨副本共享。
	ss, ssErr := factory.SelectSessionStore(cfg)
	if ssErr != nil {
		// Redis 初始化失败：生产模式 fail-fast，非生产回退进程内（保持本地体验兼容）。
		if cfg.Production {
			return nil, fmt.Errorf("会话状态后端初始化失败: %w", ssErr)
		}
		logx.Warn(context.Background(), "会话状态后端初始化失败，非生产模式回退进程内", "err", ssErr)
		ss = store.NewInProcessSessionStore()
	}
	s.sessionStore = ss
	// DeviceFP deadline：从 config 初始化，consumeRefreshToken 据此强制 DeviceFP 非空。
	s.deviceFPDeadline = cfg.DeviceFPDeadline
	// 登录/注册防爆破 + 限流守卫。
	// 失败计数 + 账号锁定经 SessionStore 共享；IP 令牌桶限流保留进程内。
	s.loginGuard = newLoginGuard(ss)
	// 启动守卫回收，防止 ips map 在长运行中无界增长（内存泄漏）。
	s.loginGuard.startSweep(10 * time.Minute)
	// startRefreshSweep 移至 Start() 中调用（需要 ctx 以支持优雅退出，避免 goroutine 泄漏）。
	// Phase 3 K8s 多集群连接管理器：构造空管理器，用户创建集群时 AddCluster。
	s.clusterMgr = k8s.NewClusterManager()
	// 重启恢复连接：控制面重启后 ClusterManager 为空，按库内集群配置重建连接。
	// AddCluster 仅解析 kubeconfig 构造 Clientset，不发起网络请求，启动轻量；
	// 连通性由用户「测试连接」或资源 API 按需刷新，恢复失败仅告警不阻断启动。
	// ：store 中 kubeconfig 为加密存储，恢复连接前需解密为明文传给 AddCluster。
	for _, kc := range st.ListK8sClusters("") {
		plain, decErr := s.decryptKubeconfig(kc.Kubeconfig)
		if decErr != nil {
			logx.Warn(context.Background(), "K8s 集群重启恢复连接解密 kubeconfig 失败", decErr, "clusterID", kc.ID)
			continue
		}
		if err := s.clusterMgr.AddCluster(kc.ID, plain); err != nil {
			logx.Warn(context.Background(), "K8s 集群重启恢复连接失败", err, "clusterID", kc.ID)
		}
	}
	// 启动时将预置 OS/中间件模板幂等写入 store（按 ID 去重，已存在不覆盖）。
	// 使模板支持在线 CRUD；store 为空时 API 回退到内存常量（向后兼容）。
	s.seedPresetOSTemplates()
	s.seedPresetMiddlewareTemplates()
	// 默认 admin 随机密码：非 demo 模式下，若 admin 仍用弱口令 admin123，
	// 替换为随机口令并打印日志。demo 模式保持 admin123（本地体验兼容）。
	if !cfg.Demo {
		rotateDefaultAdminPassword(st)
	}
	// OTel 链路追踪初始化：endpoint 为空且 stdout=false 时 no-op（零开销）。
	// 服务名默认 "opsmesh-controlplane"（未配置时由 otelx 回退 "opsmesh"）。
	// 启用后控制面 HTTP + gRPC 自动埋点，trace_id 贯穿 agent→控制面→store。
	otelShutdown, otelErr := otelx.Init(otelx.Config{
		Endpoint:    cfg.OTELEndpoint,
		ServiceName: factory.FirstNonEmpty(cfg.OTELServiceName, "opsmesh-controlplane"),
		Stdout:      cfg.OTELStdout,
	})
	if otelErr != nil {
		return nil, fmt.Errorf("OTel 初始化失败: %w", otelErr)
	}
	s.otelShutdown = otelShutdown
	if cfg.OTELEndpoint != "" || cfg.OTELStdout {
		logx.Info(context.Background(), "OTel 链路追踪已启用", "endpoint", cfg.OTELEndpoint, "stdout", cfg.OTELStdout, "service", cfg.OTELServiceName)
	}
	// API 限流器：CBRateLimitPerSec>0 时启用，按 IP 令牌桶限流。
	// 超过阈值返回 429 Too Many Requests。禁用时 rateLimiter=nil，中间件透传。
	if cfg.CBRateLimitPerSec > 0 {
		s.rateLimiter = newRateLimiter(cfg.CBRateLimitPerSec, 10*time.Minute)
		logx.Info(context.Background(), "API 限流已启用", "ratePerSec", cfg.CBRateLimitPerSec)
	}
	// M3 集成：Helm 应用商店（RepoManager + ReleaseManager）。
	// kubeconfig 留空，helm CLI 使用 KUBECONFIG 环境变量或 ~/.kube/config 默认路径；
	// helm 二进制不存在时构造仍成功，API 调用时返回 503 友好错误（不阻断启动）。
	s.helmRepo = helm.NewRepoManager(nil)
	s.helmRelease = helm.NewReleaseManager("")
	// 启动时从 helm CLI 已配置的仓库同步到内存索引（helm repo list）。
	// helm 未安装/无仓库时返回错误，仅打 Warning 不阻断启动。
	if err := s.helmRepo.LoadFromHelm(); err != nil {
		logx.Warn(context.Background(), "Helm 仓库加载失败（helm 未安装或不可用）", "err", err)
	}
	// M5 集成：批量运维/灰度发布 + 定时任务管理 + 审批引擎。
	// batches 内存索引（重启后丢失，任务实例本身在 store 持久化）。
	// scheduleMgr 维护 ScheduleEntry 元数据；approvalEngine 来自 internal/approval 包。
	// 预置审批流（DefaultFlows）在引擎构造后注入，使新租户开箱可用。
	s.batches = newBatchStore()
	s.scheduleMgr = cron.NewManager()
	s.approvalEngine = approval.New()
	for _, f := range approval.DefaultFlows {
		cp := *f
		cp.TenantID = "default" // 预置流模板实例化为 default 租户
		if err := s.approvalEngine.CreateFlow(&cp); err != nil {
			logx.Warn(context.Background(), "预置审批流注入失败", "err", err, "flowID", f.ID)
		}
	}
	// M6 集成：初始化网络拓扑缓存（空缓存，首次查询时触发探测）。
	s.networkTopologyCache = &NetworkTopologyCache{}
	// CMDB 采集自动化：构造定时采集器（interval 5 分钟，跨租户采集 tenantID=""）。
	// Start 启动 goroutine 运行 Run(ctx) 周期采集；POST /api/v1/cmdb/collect 手动触发。
	// cmdbHandler.Store() 暴露 CiStore 供 collector 直接 CRUD CI（不经过 HTTP 路由层）。
	s.cmdbCollector = NewCMDBCollector(st, s.cmdbHandler.Store(), 5*time.Minute, "")
	// CMDB 变更审批：构造审批管理器，CI 创建/修改/删除走审批流。
	// 路由 /api/v1/cmdb/changes/*；审批通过后调用 cmdbHandler.Store() 执行实际 CRUD。
	s.cmdbApprovalMgr = NewCMDBApprovalManager(st, s.cmdbHandler.Store())
	// 集成：告警抑制器（--inhibit-rules-file 非空时加载规则构造 AlertInhibitor）。
	// 加载失败时 fail-fast（启动期发现问题而非运行期诡异失败）。
	// nil=未启用告警抑制（向后兼容，--inhibit-rules-file 为空时），评估流程跳过抑制检查。
	if cfg.InhibitRulesFile != "" {
		rules, loadErr := alertengine.LoadInhibitRules(cfg.InhibitRulesFile)
		if loadErr != nil {
			return nil, fmt.Errorf("加载告警抑制规则文件失败: %w", loadErr)
		}
		s.alertInhibitor = alertengine.NewAlertInhibitor(rules)
		logx.Info(context.Background(), "告警抑制已启用", "rulesFile", cfg.InhibitRulesFile, "rulesCount", len(rules))
	}
	// 异常检测引擎：--anomaly-detection=true 时构造 AnomalyEngine。
	// 引擎构造后由 alertEngineLoop 在评估周期对设备指标调用 Evaluate，
	// 异常时产生 AnomalyAlert 并转换为 AlertEvent 进入现有告警链。
	// nil=未启用异常检测（向后兼容，--anomaly-detection=false 时），评估流程跳过异常检测。
	// 默认预置两条规则：cpu_usage 与 mem_usage 的 baseline 检测（运维可后续通过 API 增删）。
	if cfg.AnomalyDetection {
		s.anomalyEngine = alertengine.NewAnomalyEngine()
		// 预置默认异常检测规则：cpu_usage 与 mem_usage 的 baseline 检测。
		// WindowSize/Threshold 来自 config，使运维可通过 flag 调参。
		s.anomalyEngine.AddRule(&alertengine.AnomalyRule{
			ID:         "anomaly-cpu-usage-default",
			MetricName: "cpu_usage",
			DeviceID:   "", // 所有设备
			Detector:   "baseline",
			WindowSize: cfg.AnomalyWindowSize,
			Threshold:  cfg.AnomalyThreshold,
			Severity:   "warning",
			TenantID:   "default",
		})
		s.anomalyEngine.AddRule(&alertengine.AnomalyRule{
			ID:         "anomaly-mem-usage-default",
			MetricName: "mem_usage",
			DeviceID:   "",
			Detector:   "baseline",
			WindowSize: cfg.AnomalyWindowSize,
			Threshold:  cfg.AnomalyThreshold,
			Severity:   "warning",
			TenantID:   "default",
		})
		logx.Info(context.Background(), "异常检测已启用",
			"windowSize", cfg.AnomalyWindowSize, "threshold", cfg.AnomalyThreshold)
	}
	// 多租户资源配额：构造 QuotaManager。
	// 始终构造（即使 cfg.QuotaEnabled=false），使 API /api/v1/quotas 可查询用量；
	// enabled 标志由 cfg.QuotaEnabled 控制，false 时 Check 方法直接放行（向后兼容）。
	// 默认配额来自 cfg.QuotaMaxDevices/QuotaMaxTasks/QuotaMaxAlerts（0=不限）。
	s.quotaMgr = NewQuotaManager(st, cfg.QuotaEnabled, &store.QuotaConfig{
		MaxDevices: cfg.QuotaMaxDevices,
		MaxTasks:   cfg.QuotaMaxTasks,
		MaxAlerts:  cfg.QuotaMaxAlerts,
	})
	if cfg.QuotaEnabled {
		logx.Info(context.Background(), "多租户资源配额已启用",
			"defaultMaxDevices", cfg.QuotaMaxDevices,
			"defaultMaxTasks", cfg.QuotaMaxTasks,
			"defaultMaxAlerts", cfg.QuotaMaxAlerts)
	}
	// 注入自动化执行器（真实执行：创建任务/发送通知/扩缩容/重启/隔离）。
	automationEngine.SetExecutor(&automationExecutor{
		store:    st,
		notifier: s.alertNotifier,
		bus:      bus,
	})
	return s, nil
}

// firstNonEmpty 返回第一个非空字符串，全空返回空串。用于服务名默认值回退。
// e 为 nil 时直接返回（与 store.Audit 一致的容错）。
func (s *Server) audit(ctx context.Context, e *proto.AuditEvent) {
	if e == nil {
		return
	}
	if e.TraceID == "" {
		e.TraceID = otelx.TraceIDFromContext(ctx)
	}
	s.store.Audit(e)
}
