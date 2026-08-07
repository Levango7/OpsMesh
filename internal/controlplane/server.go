// Package controlplane 实现控制面：HTTP(B/S) 仪表盘 + gRPC 注册通道 + metrics。
// U-05: 通过 --mode=controlplane 启动。
//   - gRPC 9090：承载 agent 注册/心跳/拉任务/上报结果（真实 gRPC，JSON codec，见 grpc.go）。
//   - HTTP 8080：B/S 仪表盘 + GET /api/v1/devices（人工查看）+ POST /api/v1/tasks（内部下发入口）。
//   - metrics 9091：极简文本指标（P2-1 观测）。
//
// U-04: 持久化后端由 --store 选择（默认 memory，可选 mysql）。
package controlplane

import (
	"bytes"
	"context"
	"crypto/hmac"
	cryptoRand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"

	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"opsmesh/internal/version"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"opsmesh/internal/authctx"
	"opsmesh/internal/cmdb"
	"opsmesh/internal/config"
	"opsmesh/internal/deploy"
	"opsmesh/internal/events"
	"opsmesh/internal/grpcx"
	"opsmesh/internal/k8s"
	"opsmesh/internal/logstore"
	"opsmesh/internal/logx"
	"opsmesh/internal/metrics"
	"opsmesh/internal/notify"
	"opsmesh/internal/orchestration"
	"opsmesh/internal/proto"
	"opsmesh/internal/provision"
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
	bus           events.Bus             // 审计/告警事件总线（P1-5，默认 noop）
	metrics       *metrics.M             // 观测指标（P2-1）
	store         store.Store            // 持久化存储（供 CMDB 等子系统复用）
	cmdbHandler   *cmdb.Handler          // CMDB 处理器（Phase 1）
	logHandler    *logstore.Handler      // M6 日志检索处理器
	deployHandler *deploy.Handler        // M3 部署中心处理器
	orchHandler   *orchestration.Handler // M5 作业编排中心处理器
	lastAlertSent time.Time              // M7 告警 Webhook：上次已推送的告警时间戳（notifyLoop 防重复）
	// B7 告警聚合器：同源告警 5 分钟聚合 + 级别抑制（critical 抑制同源 warning）。
	alertAggr *notify.AlertAggregator
	// B7 告警多通道（Webhook + Email）。NewServer 从 cfg 构造；notifyLoop 每次推送复用。
	alertChannels *notify.Channels
	// M3-2B SSE 实时推送：订阅者集合与互斥保护。
	// 每个 SSE 连接对应一个 buffered chan，publishEvent 非阻塞广播到所有订阅者。
	// 慢消费者（缓冲满）丢弃事件，避免一个慢客户端拖垮广播（M3-2B 设计取舍）。
	eventMu   sync.RWMutex
	eventSubs map[chan SSEEvent]struct{}

	// M4-4D 控制面联邦管理器（nil=未启用联邦）。
	// 由 NewServer 在 cfg.FederationPeers 非空时构造；启用后路由层注册 /api/v1/federation/* 端点。
	fed *FederationManager

	// 用户中心 JWT 签发密钥（HS256，来自 config.JWTSecret；空=随机生成）。
	// 用于 auth.go 中登录/注册后签发 token。空时 NewServer 随机生成 32 字节密钥
	// （重启后旧 token 失效，仅开发/单实例适用）。
	jwtSecret []byte

	// loginGuard 登录/注册防爆破 + 限流（P1-4，进程内）。多副本 HA 下建议后续换 Redis 共享。
	loginGuard *loginGuard

	// clusterMgr K8s 多集群连接管理器（Phase 3）。
	// 由 NewServer 构造；用户创建/更新集群时 AddCluster，删除时 RemoveCluster，测试连接时 TestCluster。
	clusterMgr *k8s.ClusterManager
}

// NewServer 构造控制面服务。按 cfg.Store 选择持久化后端（默认 memory），并初始化事件总线与指标。
// startRefreshSweep 周期清理过期刷新令牌（task 112：store 持久化后改为 no-op，
// 过期清理由 consumeRefreshToken 顺带完成；保留 sweep 机制以兼容未来 store 层扩展批量清理）。
func (s *Server) startRefreshSweep(interval time.Duration) {
	if interval <= 0 {
		interval = time.Hour
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for range t.C {
			s.purgeExpiredRefreshTokens()
		}
	}()
}

func NewServer(cfg *config.Config) *Server {
	// Kafka brokers/topic 经参数传入事件总线（避免 os.Setenv 并发不安全，P1-5）。
	bus := events.New(cfg.EventBus, cfg.KafkaBrokers, cfg.KafkaTopic)
	st := selectStore(cfg, bus)
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
		cmdbHandler:   newCMDBHandler(st),
		logHandler:    newLogHandler(st, cfg),
		deployHandler: newDeployHandler(st),
		orchHandler:   newOrchestrationHandler(st),
		eventSubs:     make(map[chan SSEEvent]struct{}), // M3-2B SSE 订阅者集合
		alertAggr:     notify.NewAlertAggregator(),      // B7 告警聚合器
		alertChannels: &notify.Channels{ // B7 多通道（Webhook + Email）
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
	}
	if cfg.Demo {
		// 演示模式（P0-5）：主动播种 demo 拓扑，让 6 大模块在无真实 agent 时也能完整演示。
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
	// M4-4D 控制面联邦：cfg.FederationPeers 非空时构造 FederationManager，启用联邦 API。
	// nil 时路由层跳过 /api/v1/federation/* 注册（向后兼容，不影响未启用联邦的部署）。
	// P1-6 硬化：传入共享 HMAC 密钥 + 出站 mTLS 配置（nil 表示明文联邦）。
	fedClientTLS, err := tlsutil.HTTPClientTLSConfig(cfg.FederationTLSCert, cfg.FederationTLSKey, cfg.FederationCA)
	if err != nil {
		log.Fatalf("[controlplane] 联邦客户端 TLS 配置失败: %v", err)
	}
	s.fed = NewFederationManager(cfg.FederationPeers, st, cfg.FederationSecret, fedClientTLS)
	// 用户中心 JWT 密钥：优先用 config.JWTSecret，空则随机生成 32 字节（重启后旧 token 失效）。
	if cfg.JWTSecret != "" {
		s.jwtSecret = []byte(cfg.JWTSecret)
	} else {
		s.jwtSecret = make([]byte, 32)
		if _, err := cryptoRand.Read(s.jwtSecret); err != nil {
			log.Fatalf("[controlplane] JWT 密钥随机生成失败: %v", err)
		}
	}
	// P1-4 登录/注册防爆破 + 限流守卫（进程内；多副本建议后续换 Redis）。
	s.loginGuard = newLoginGuard()
	// P2 启动守卫回收，防止 ips/fails map 在长运行中无界增长（内存泄漏）。
	s.loginGuard.startSweep(10 * time.Minute)
	s.startRefreshSweep(time.Hour) // 周期清理过期刷新令牌，防内存增长
	// Phase 3 K8s 多集群连接管理器：构造空管理器，用户创建集群时 AddCluster。
	s.clusterMgr = k8s.NewClusterManager()
	// task 92 重启恢复连接：控制面重启后 ClusterManager 为空，按库内集群配置重建连接。
	// AddCluster 仅解析 kubeconfig 构造 Clientset，不发起网络请求，启动轻量；
	// 连通性由用户「测试连接」或资源 API 按需刷新，恢复失败仅告警不阻断启动。
	for _, kc := range st.ListK8sClusters("") {
		if err := s.clusterMgr.AddCluster(kc.ID, kc.Kubeconfig); err != nil {
			logx.Warn(context.Background(), "K8s 集群重启恢复连接失败", err, "clusterID", kc.ID)
		}
	}
	// task 104：启动时将预置 OS/中间件模板幂等写入 store（按 ID 去重，已存在不覆盖）。
	// 使模板支持在线 CRUD；store 为空时 API 回退到内存常量（向后兼容）。
	s.seedPresetOSTemplates()
	s.seedPresetMiddlewareTemplates()
	return s
}

// newDeployHandler 构造 M3 部署处理器：按 store 类型选 SQL/Memory 后端，
// 并用 store 适配 deploy.Dispatcher（防腐接口，避免 deploy 反向依赖 controlplane）。
func newDeployHandler(st store.Store) *deploy.Handler {
	var ds deploy.DeployStore
	if ss, ok := st.(*store.SQLStore); ok {
		s, err := deploy.NewSQL(ss.DB())
		if err != nil {
			logx.Error(context.Background(), "MySQL 部署后端初始化失败，回退 memory", err)
			ds = deploy.NewMemory()
		} else {
			logx.Info(context.Background(), "M3 部署后端=mysql", "reason", "U-04 数据本地化")
			ds = s
		}
	} else {
		ds = deploy.NewMemory()
	}
	return deploy.NewHandler(ds, &storeDispatcher{store: st})
}

// newOrchestrationHandler 构造 M5 作业编排处理器：按 store 类型选 SQL/Memory 后端，
// 并以 store（具备 CreateTask + TasksByParent）直接适配 orchestration.TaskEngine（防腐）。
func newOrchestrationHandler(st store.Store) *orchestration.Handler {
	var ws orchestration.WorkflowStore
	if ss, ok := st.(*store.SQLStore); ok {
		s, err := orchestration.NewSQL(ss.DB())
		if err != nil {
			logx.Error(context.Background(), "MySQL 工作流后端初始化失败，回退 memory", err)
			ws = orchestration.NewMemory()
		} else {
			logx.Info(context.Background(), "M5 工作流后端=mysql", "reason", "U-04 数据本地化")
			ws = s
		}
	} else {
		ws = orchestration.NewMemory()
	}
	return orchestration.NewHandler(ws, st)
}

// storeDispatcher 以 store.Store 适配 deploy.Dispatcher（M3 -> M4 任务引擎派发）。
// M2-1B：原 registryDispatcher 持有 *Registry 薄间接层，现直连 store.Store 小接口。
type storeDispatcher struct {
	store store.Store
}

func (d *storeDispatcher) CreateTask(t *proto.Task) *proto.Task {
	return d.store.CreateTask(t)
}

func (d *storeDispatcher) Device(id string) *proto.DeviceInfo {
	return d.store.Device(id)
}

func (d *storeDispatcher) TaskStates(ids []string, tenantID string) map[string]string {
	out := make(map[string]string)
	if len(ids) == 0 {
		return out
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	for _, t := range d.store.AllTasks(tenantID) {
		if set[t.TaskID] {
			out[t.TaskID] = t.Status
		}
	}
	return out
}

// selectStore 按配置选择后端：--store=mysql 且 DSN 非空时启用 SQLStore，否则 MemoryStore。
// 同时注入事件总线（P1-5），使 store 层状态变更可经 Kafka 等真实消费。
// M4-4C：--multi-schema=true 时使用 MultiSchemaStore（每租户独立 schema），而非单个 SQLStore。
func selectStore(cfg *config.Config, bus events.Bus) store.Store {
	if cfg.Store == "mysql" && cfg.MySQLDSN != "" {
		if cfg.MultiSchema {
			ms, err := store.NewMultiSchemaStore(cfg.MySQLDSN, cfg.RedisAddr, store.DefaultSchemaNamer(cfg.SchemaPrefix))
			if err != nil {
				logx.Error(context.Background(), "Multi-schema store 初始化失败，回退 memory", err)
				return store.NewMemoryStore().WithSecret(cfg.ProvisionSecret).WithBus(bus).WithDemo(cfg.Demo)
			}
			logx.Info(context.Background(), "持久化后端=mysql(multi-schema)", "reason", "M4-4C 多租户 schema 隔离")
			return ms.WithBus(bus).WithSecret(cfg.ProvisionSecret).WithDemo(cfg.Demo)
		}
		ss, err := store.NewSQLStore(cfg.MySQLDSN, cfg.RedisAddr)
		if err != nil {
			logx.Error(context.Background(), "MySQL store 初始化失败，回退 memory", err)
			return store.NewMemoryStore().WithSecret(cfg.ProvisionSecret).WithBus(bus).WithDemo(cfg.Demo)
		}
		logx.Info(context.Background(), "持久化后端=mysql", "reason", "U-04 数据本地化")
		return ss.WithBus(bus).WithSecret(cfg.ProvisionSecret).WithDemo(cfg.Demo)
	}
	logx.Info(context.Background(), "持久化后端=memory", "reason", "默认，无外部依赖")
	return store.NewMemoryStore().WithSecret(cfg.ProvisionSecret).WithBus(bus).WithDemo(cfg.Demo)
}

// newCMDBHandler 按 store 类型创建 CMDB 处理器：MySQL 时使用 SQLCiStore，否则 MemoryCiStore。
func newCMDBHandler(st store.Store) *cmdb.Handler {
	if ss, ok := st.(*store.SQLStore); ok {
		return cmdb.NewHandler(cmdb.NewSQLCiStore(ss.DB()))
	}
	return cmdb.NewHandler(cmdb.NewMemoryCiStore())
}

// newLogHandler 按 cfg.LogStore 选择 M6 日志检索后端（B1 修复 8：Loki/ES 接入）：
//   - memory（默认）：环形缓冲，无外部依赖
//   - sql：MySQL 后端（与控制面共享连接池）
//   - loki：Grafana Loki 后端（仅查询，Append 为 noop，日志由 promtail 直接推送）
//   - es：Elasticsearch 后端（仅查询，Append 为 noop，日志由 filebeat 直接推送）
//
// loki/es 初始化失败时回退 memory（不阻断启动）。
func newLogHandler(st store.Store, cfg *config.Config) *logstore.Handler {
	// B1 修复 8：优先按 cfg.LogStore 选择后端（loki/es 分支）。
	switch cfg.LogStore {
	case "loki":
		if cfg.LokiEndpoint == "" {
			logx.Error(context.Background(), "LogStore=loki 但 LokiEndpoint 为空，回退 memory", nil)
			return logstore.NewHandler(logstore.NewMemory(0))
		}
		logx.Info(context.Background(), "M6 日志后端=loki", "endpoint", cfg.LokiEndpoint)
		return logstore.NewHandler(logstore.NewLokiStore(cfg.LokiEndpoint))
	case "es":
		if cfg.ESEndpoint == "" {
			logx.Error(context.Background(), "LogStore=es 但 ESEndpoint 为空，回退 memory", nil)
			return logstore.NewHandler(logstore.NewMemory(0))
		}
		idx := cfg.ESIndex
		if idx == "" {
			idx = "opsmesh-logs"
		}
		logx.Info(context.Background(), "M6 日志后端=es", "endpoint", cfg.ESEndpoint, "index", idx)
		return logstore.NewHandler(logstore.NewESStore(cfg.ESEndpoint, idx))
	}
	// 默认：按 store 类型选择 memory/sql。
	if ss, ok := st.(*store.SQLStore); ok {
		ls, err := logstore.NewSQL(ss.DB())
		if err != nil {
			logx.Error(context.Background(), "MySQL 日志后端初始化失败，回退 memory", err)
			return logstore.NewHandler(logstore.NewMemory(0))
		}
		logx.Info(context.Background(), "M6 日志后端=mysql", "reason", "U-04 数据本地化")
		return logstore.NewHandler(ls)
	}
	logx.Info(context.Background(), "M6 日志后端=memory", "reason", "默认，无外部依赖")
	return logstore.NewHandler(logstore.NewMemory(0))
}

// securityHeadersMiddleware 为 HTTP 响应注入安全头（H5 安全头中间件）。
// 应用于整个主 mux（仪表盘 + /api/v1/* + 静态资源）；/metrics 在独立 server（buildMetrics）不受影响。
//
// B1 修复 5+6：安全头补全 + CSP nonce 收紧
//   - HSTS：仅 HTTPS 部署（s.tlsCert != ""）时注入 Strict-Transport-Security
//   - Permissions-Policy：禁用 camera/microphone/geolocation
//   - CSP nonce：每请求生成随机 nonce 并注入 CSP（为后续前端改造做准备）；
//     由于前端有 141+ 个 inline onclick 事件处理器，暂保留 'unsafe-inline' 向后兼容。
//     后续收紧计划：前端改造为 addEventListener + nonce-based inline script/style 后，
//     移除 'unsafe-inline'，仅保留 'self' + 'nonce-{nonce}'。
//
// /healthz 也被包裹但 CSP 对其无副作用（返回 text/plain，无脚本/HTML 解析）。
func (s *Server) securityHeadersMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		// B1 修复 5：Permissions-Policy 禁用敏感设备权限。
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		// B1 修复 5：HSTS 仅 HTTPS 部署时注入（tlsCert 非空表示启用了 TLS）。
		if s.tlsCert != "" {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		// B1 修复 6：CSP nonce-based 收紧。
		// 每请求生成 16 字节随机 nonce（hex 编码 32 字符），注入 CSP 头。
		// 前端改造完成后移除 'unsafe-inline'，仅保留 'self' + 'nonce-{nonce}'。
		nonceBytes := make([]byte, 16)
		if _, err := cryptoRand.Read(nonceBytes); err != nil {
			// 随机数生成失败（极罕见）：回退到固定 nonce（仅影响 CSP 强度，不阻断请求）。
			nonceBytes = []byte("fallback-nonce-v1")
		}
		nonce := hex.EncodeToString(nonceBytes)
		w.Header().Set("Content-Security-Policy",
			fmt.Sprintf("default-src 'self'; script-src 'self' 'unsafe-inline' 'nonce-%s'; style-src 'self' 'unsafe-inline' 'nonce-%s'; img-src 'self' data:; connect-src 'self'", nonce, nonce))
		h.ServeHTTP(w, r)
	})
}

// recoveryMiddleware 兜底盘：捕获任何 handler 内的 panic，避免单请求崩溃拖垮整个 HTTP 服务
// （P0-2 致命短板——internal/ 生产代码零 recover，某 handler 未预期 panic 会击穿 net/http 默认
// recover 仅打印日志但仍返回 200 空响应，掩盖故障且无 trace）。此处返回 500 + 结构化错误 + traceID，
// 并交由 logx 落结构化日志。
func recoveryMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				ctx := logx.WithTrace(r.Context(), "http-recover")
				logx.Error(ctx, "HTTP handler panic recovered",
					fmt.Errorf("%v", rec), "method", r.Method, "path", r.URL.Path)
				// net/http 在 WriteHeader 已调用后无法覆写状态码；此时仅记录，避免二次 panic。
				if w.Header().Get("Content-Type") == "" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					if err := json.NewEncoder(w).Encode(map[string]string{
						"error":   "internal server error",
						"traceId": logx.Trace(ctx),
					}); err != nil {
						log.Printf("controlplane: panic recover 写错误响应失败: %v", err)
					}
				}
			}
		}()
		h.ServeHTTP(w, r)
	})
}

// maxBodyBytes 限制请求体大小（P1-3 防 DoS：拒绝超大 body 直接 413，避免 JSON 解析拖垮内存）。
const maxBodyBytes = 1 << 20 // 1 MiB

// decodeJSONBody 在 MaxBytesReader 约束下解析 JSON 请求体（P1-3 请求体大小限制）。
// 替换所有裸 json.NewDecoder(r.Body).Decode 调用，统一防超大请求体。
// 注意：仅做大小限制，不启用 DisallowUnknownFields，避免破坏前端多传字段的既有兼容行为。
func decodeJSONBody(w http.ResponseWriter, r *http.Request, v interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	return json.NewDecoder(r.Body).Decode(v)
}

// requireTenantContext 提取并校验网关注入的租户身份上下文（H6 认证防御）。
//
// 行为矩阵（B1 修复 1+2：增加 Bearer token 回退与交叉校验）：
//   - 头非空（X-Tenant-ID 已注入）：
//   - token 也携带 tenant_id 且一致 → 返回 actx, true
//   - token 也携带 tenant_id 但不一致 → 403 Forbidden（防绕过网关伪造租户头）
//   - token 无 tenant_id → 返回 actx, true（仅头注入，向后兼容）
//   - 头为空且 token 携带 tenant_id → 回退到 token 中的 tenant_id，返回 actx, true
//   - 头为空且 token 无 tenant_id 且 requireAuth=true → 401 Unauthorized
//   - 头为空且 token 无 tenant_id 且 requireAuth=false 且 demo=true → 自动填充 default/demo
//   - 头为空且 token 无 tenant_id 且 requireAuth=false 且 demo=false → 400 Bad Request
//
// 安全语义：Bearer token 中的 tenant_id 与 X-Tenant-ID 头交叉校验，防绕过网关伪造租户头；
// 头空时回退到 token，支持无网关直连场景（用户中心登录后直接访问 API）。
// 调用方应在 ok=false 时直接 return（响应已写入）。
func (s *Server) requireTenantContext(w http.ResponseWriter, r *http.Request) (authctx.Context, bool) {
	actx := authctx.FromHTTPHeader(r.Header)
	// B1 修复 1+2：从 Bearer token/Cookie 提取 tenant_id 作为回退/交叉校验。
	tokenTenant, tokenUser := s.tenantFromBearer(r)
	if actx.TenantID != "" {
		// 头非空：若 token 也携带 tenant_id，校验两者一致，防绕过网关伪造租户头。
		if tokenTenant != "" && tokenTenant != actx.TenantID {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch between X-Tenant-ID header and JWT claims"})
			return actx, false
		}
		return actx, true
	}
	// 头空：回退到 token 中的 tenant_id（支持无网关直连场景）。
	if tokenTenant != "" {
		actx.TenantID = tokenTenant
		if actx.UserID == "" && tokenUser != "" {
			actx.UserID = tokenUser
		}
		return actx, true
	}
	if s.requireAuth {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing identity context (gateway auth required)"})
		return actx, false
	}
	if s.cfg != nil && s.cfg.Demo {
		// demo 模式放宽：未携带身份头时填充默认租户/用户，便于本地一键体验。
		actx.TenantID = "default"
		if actx.UserID == "" {
			actx.UserID = "demo"
		}
		return actx, true
	}
	// 非生产非 demo 模式：拒绝空租户头，防越权伪造。
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing X-Tenant-ID header (tenant context required)"})
	return actx, false
}

// tenantFromBearer 从 Authorization: Bearer <token> 或 HttpOnly Cookie 中提取 tenant_id/user_id。
// 用于 requireTenantContext 的 token 回退与交叉校验（B1 修复 1+2）。
// token 缺失/无效时返回空串（不阻断，由调用方决定后续行为）。
func (s *Server) tenantFromBearer(r *http.Request) (tenantID, userID string) {
	tokenStr, err := extractBearer(r)
	if err != nil {
		// 回退 HttpOnly Cookie（与 userFromToken 一致，task 94 双 Cookie 方案）。
		if ck, ckErr := r.Cookie(accessTokenCookieName); ckErr == nil && strings.TrimSpace(ck.Value) != "" {
			tokenStr = ck.Value
		} else {
			return "", ""
		}
	}
	claims, err := authctx.ParseHSJWT(tokenStr, s.jwtSecret)
	if err != nil {
		return "", ""
	}
	return claims.TenantID, claims.UserID
}

// Start 启动 HTTP(B/S)、gRPC(9090)、metrics(9091) 三个监听，并在收到 SIGTERM/SIGINT 时优雅退出。
func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleDashboard)
	mux.HandleFunc("/assets/", s.handleAsset) // 前端静态资源（E2 独立化：web/assets/*）
	mux.HandleFunc("/api/v1/devices", s.handleDevices)
	mux.HandleFunc("/api/v1/agents", s.handleAgents)
	mux.HandleFunc("/api/v1/me", s.handleMe)
	mux.HandleFunc("/api/v1/tasks", s.handleListTasks)
	mux.HandleFunc("/api/v1/tasks/", s.handleTaskRouting)           // 子路径：{id}/cancel、{id}/result
	mux.HandleFunc("/healthz", s.handleHealthz)                     // K8s 探针（P2-12）
	mux.HandleFunc("/api/v1/audits", s.handleAudits)                // GET 审计检索（P0-4）
	mux.HandleFunc("/api/v1/tasks/batch", s.handleBatchCreateTasks) // POST 批量下发（P0-3）
	mux.HandleFunc("/api/v1/devices/", s.handleDeviceRouting)       // 子路径：{id} DELETE 退役、{id}/provision
	mux.HandleFunc("/api/v1/alerts", s.handleAlerts)                // GET 活跃告警（M7）
	mux.HandleFunc("/api/v1/alerts/", s.handleAlertRouting)         // 子路径：{id}/ack、{id}/silence
	mux.HandleFunc("/api/v1/events/stream", s.handleEventsStream)   // M3-2B SSE 实时推送（替代 5s 轮询）
	// B1 自动纳管：install.sh 分发脚本 + agent 二进制分发 + 自动纳管触发
	mux.HandleFunc("/install.sh", s.handleInstallSh)
	mux.HandleFunc("/bin/opsmesh-agent", s.handleServeAgent)
	mux.HandleFunc("/api/v1/provision/auto", s.handleAutoProvision)
	// CMDB（Phase 1）：按持久化后端选择 SQL 或 Memory 实现。
	s.cmdbHandler.RegisterRoutes(mux)
	// M6 日志检索：GET/POST /api/v1/logs（租户隔离由 authctx 注入）。
	s.logHandler.RegisterRoutes(mux)
	// M3 部署中心：POST/GET /api/v1/deploys（租户隔离由 authctx 注入）。
	// B1 修复 3：用 paginateJSONHandler 包装 GET 列表做分页（向后兼容）。
	deployMux := http.NewServeMux()
	s.deployHandler.RegisterRoutes(deployMux)
	mux.Handle("/api/v1/deploys", paginateJSONHandler(deployMux))
	mux.Handle("/api/v1/deploys/", deployMux)
	// M5 作业编排中心：POST/GET /api/v1/workflows（租户隔离由 authctx 注入）。
	// B1 修复 3：同上分页包装。
	orchMux := http.NewServeMux()
	s.orchHandler.RegisterRoutes(orchMux)
	mux.Handle("/api/v1/workflows", paginateJSONHandler(orchMux))
	mux.Handle("/api/v1/workflows/", orchMux)
	// M4-4D 控制面联邦：仅当配置了 --federation-peers 时注册联邦 API。
	// 未启用时这些端点返回 404（mux 未注册），保证向后兼容。
	if s.fed != nil {
		mux.HandleFunc("/api/v1/federation/peers", s.handleFederationPeers)
		mux.HandleFunc("/api/v1/federation/forward/task", s.handleFederationForwardTask)
		mux.HandleFunc("/api/v1/federation/devices", s.handleFederationDevices)
	}
	// 用户中心（RBAC + JWT）：注册/登录/查询当前用户 + 用户/角色/权限 CRUD。
	// 与网关注入身份模式并存：用户中心用于 B/S 仪表盘登录，网关注入用于 agent gRPC 通道。
	mux.HandleFunc("/api/v1/auth/register", s.handleAuthRegister)
	mux.HandleFunc("/api/v1/auth/login", s.handleAuthLogin)
	mux.HandleFunc("/api/v1/auth/me", s.handleAuthMe)
	mux.HandleFunc("/api/v1/auth/logout", s.handleAuthLogout)                  // task 94：登出清 HttpOnly Cookie
	mux.HandleFunc("/api/v1/auth/refresh", s.handleAuthRefresh)                // 双 Cookie：rt 静默换新 at+rt（旋转）
	mux.HandleFunc("/api/v1/auth/change-password", s.handleAuthChangePassword) // 安全债 85：预置弱口令强制改密
	mux.HandleFunc("/api/v1/users", s.handleUsers)
	mux.HandleFunc("/api/v1/users/", s.handleUserRouting)
	mux.HandleFunc("/api/v1/roles", s.handleRoles)
	mux.HandleFunc("/api/v1/roles/", s.handleRoleRouting)
	mux.HandleFunc("/api/v1/permissions", s.handlePermissions)
	// OS 基础环境优化：预置模板列表 + 详情 + 在指定 agent 上执行。
	mux.HandleFunc("/api/v1/os-templates", s.handleListOSTemplates)
	mux.HandleFunc("/api/v1/os-templates/", s.handleOSTemplateRouting) // 子路径：{id} 和 {id}/execute
	// 中间件部署：预置模板列表 + 详情 + 在指定 agent 上部署 + 已部署实例查询 + 卸载。
	mux.HandleFunc("/api/v1/middleware-templates", s.handleMiddlewareTemplates)
	mux.HandleFunc("/api/v1/middleware-templates/", s.handleMiddlewareTemplateDetail) // 子路径：{id} 和 {id}/deploy
	mux.HandleFunc("/api/v1/middleware-instances", s.handleMiddlewareInstances)
	mux.HandleFunc("/api/v1/middleware-instances/", s.handleMiddlewareInstanceRouting) // 子路径：{id}/uninstall
	// Phase 3 K8s 集群管理：GET/POST 集群列表 + DELETE 单集群 + POST 测试连接。
	mux.HandleFunc("/api/v1/k8s/clusters", s.handleK8sClusters)
	mux.HandleFunc("/api/v1/k8s/clusters/", s.handleK8sClusterRouting) // 子路径：{id} 和 {id}/test
	// B1 修复 9：告警规则 CRUD API。
	mux.HandleFunc("/api/v1/alert-rules", s.handleAlertRules)
	mux.HandleFunc("/api/v1/alert-rules/", s.handleAlertRuleRouting) // 子路径：{id} DELETE 删除

	// B1 修复 4：用 jsonErrorMux 包装 mux，将 404 统一为 JSON 格式。
	httpSrv := &http.Server{
		Addr:              fmt.Sprintf(":%d", s.httpPort),
		Handler:           recoveryMiddleware(s.securityHeadersMiddleware(&jsonErrorMux{inner: mux})), // P0-2 兜底盘 + H5 安全头 + B1 404 JSON
		ReadHeaderTimeout: 10 * time.Second,
	}

	grpcSrv, grpcLis := s.buildGRPC()
	metricsSrv, metricsLis := s.buildMetrics()
	// P1-6 联邦独立 mTLS 监听（端口 >0 且已启用联邦时生效；否则返回 nil）。
	fedSrv, fedLis, fedErr := s.buildFederationServer()
	if fedErr != nil {
		log.Fatalf("[controlplane] 联邦 mTLS 监听构建失败: %v", fedErr)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx = logx.WithTrace(ctx, "controlplane")
	go s.leaderLoop(ctx)           // A3 选主：周期续租，仅 leader 执行周期协调任务
	go s.reclaimLoop(ctx)          // P0-1 任务租约回收：周期复位失联 agent 的 running 任务（仅 leader）
	go s.scheduleLoop(ctx)         // F4 定时/周期调度：周期派生到点模板任务的 pending 实例（仅 leader）
	go s.archiveLoop(ctx)          // F5 ��线超龄自动归档（仅 leader）
	go s.notifyLoop(ctx)           // M7 告警 Webhook 推送：周期检查新 critical 告警并推送到 webhook URL
	go s.autoProvisionLoop(ctx)    // B1 自动纳管：--discover + --auto-provision 时周期扫描网段并推送 agent
	go s.deployReconcileLoop(ctx)  // M3 部署对账：周期把 running 部署按底层任务结果翻终态（仅 leader）
	go s.workflowScheduleLoop(ctx) // M5 作业编排：周期按 cron 触发 active 工作流并 reconcile 运行态（仅 leader）

	errCh := make(chan error, 3)
	go func() {
		if err := grpcSrv.Serve(grpcLis); err != nil {
			errCh <- fmt.Errorf("grpc: %w", err)
		}
	}()
	go func() {
		if err := metricsSrv.Serve(metricsLis); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("metrics: %w", err)
		}
	}()
	go func() {
		logx.Info(ctx, "HTTP(B/S) 监听", "port", s.httpPort)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("http: %w", err)
		}
	}()
	if fedSrv != nil {
		go func() {
			logx.Info(ctx, "联邦 mTLS 监听", "port", s.cfg.FederationPort)
			// TLSConfig 已设置 RequireAndVerifyClientCert，ServeTLS 启用 mTLS。
			if err := fedSrv.ServeTLS(fedLis, "", ""); err != nil && err != http.ErrServerClosed {
				errCh <- fmt.Errorf("federation: %w", err)
			}
		}()
	}

	if s.cfg.FederationPort > 0 && s.fed != nil {
		logx.Info(ctx, "控制面已启动", "http", s.httpPort, "grpc", s.grpcPort, "metrics", s.metricsPort, "federation_mtls", s.cfg.FederationPort)
	} else {
		logx.Info(ctx, "控制面已启动", "http", s.httpPort, "grpc", s.grpcPort, "metrics", s.metricsPort)
	}
	select {
	case <-ctx.Done():
		logx.Info(ctx, "收到终止信号，优雅退出", "window", s.shutdownWait.String())
		grpcSrv.GracefulStop()
		shutCtx, cancel := context.WithTimeout(context.Background(), s.shutdownWait)
		defer cancel()
		if err := httpSrv.Shutdown(shutCtx); err != nil {
			log.Printf("controlplane: HTTP 服务优雅退出失败: %v", err)
		}
		if err := metricsSrv.Shutdown(shutCtx); err != nil {
			log.Printf("controlplane: metrics 服务优雅退出失败: %v", err)
		}
		if fedSrv != nil {
			if err := fedSrv.Shutdown(shutCtx); err != nil {
				log.Printf("controlplane: federation 服务优雅退出失败: %v", err)
			}
		}
		return nil
	case err := <-errCh:
		return err
	}
}

// buildGRPC 构造 gRPC server 与监听（支持可选 TLS/mTLS）。
func (s *Server) buildGRPC() (*grpc.Server, net.Listener) {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.grpcPort))
	if err != nil {
		log.Fatalf("[controlplane] gRPC 监听失败 %d: %v", s.grpcPort, err)
	}
	var opts []grpc.ServerOption
	if s.tlsCert != "" && s.tlsKey != "" {
		creds, err := tlsutil.ServerCreds(s.tlsCert, s.tlsKey, s.clientCA)
		if err != nil {
			log.Fatalf("[controlplane] gRPC TLS 加载失败: %v", err)
		}
		opts = append(opts, grpc.Creds(creds))
		logx.Info(context.Background(), "gRPC 已启用 TLS", "mtls", s.clientCA != "")
	}
	// P0-2 兜底盘：拦截 unary handler panic，避免单 RPC 击穿整个 gRPC server。
	opts = append(opts, grpc.UnaryInterceptor(grpcRecoveryInterceptor))
	gs := grpc.NewServer(opts...)
	gs.RegisterService(&grpcx.Registration_ServiceDesc, &grpcServerImpl{
		store:       s.store,
		requireAuth: s.requireAuth,
		cfg:         s.cfg,
		bus:         s.bus,
		metrics:     s.metrics,
		cmdb:        s.cmdbHandler,
		logs:        s.logHandler,
		srv:         s, // M3-2B：注入 Server 引用，使 gRPC handler 可发布 SSE 事件
		// task 81 gRPC agent 身份绑定：按 config.GRPCRequireSignature 启用签名验证。
		// demo 模式下 config 已强制关闭（cfg.GRPCRequireSignature=false），此处直接透传。
		requireSignature: s.cfg != nil && s.cfg.GRPCRequireSignature,
	})
	return gs, lis
}

// grpcRecoveryInterceptor 兜底盘：拦截任何 unary handler 内的 panic，避免单个 RPC panic
// 击穿 gRPC 默认行为（整个 server 崩溃），转为 Internal 状态码 + 结构化日志（P0-2）。
func grpcRecoveryInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			tctx := logx.WithTrace(ctx, "grpc-recover")
			logx.Error(tctx, "gRPC handler panic recovered",
				fmt.Errorf("%v", rec), "method", info.FullMethod)
			err = status.Errorf(codes.Internal, "internal server error")
		}
	}()
	return handler(ctx, req)
}

// metricsAllowed 判断客户端是否为 metrics 端点授权来源（P1-5 CIDR 白名单）。
// 白名单为空（默认）= 允许全部（向后兼容 MVP）；非空时仅允许命中白名单的 IP。
func (s *Server) metricsAllowed(remoteAddr string) bool {
	if strings.TrimSpace(s.cfg.MetricsAllowCIDR) == "" {
		return true // 未配置白名单：向后兼容开放
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	clientIP := net.ParseIP(host)
	if clientIP == nil {
		return false
	}
	for _, item := range strings.Split(s.cfg.MetricsAllowCIDR, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		_, netCIDR, err := net.ParseCIDR(item)
		if err != nil {
			continue
		}
		if netCIDR.Contains(clientIP) {
			return true
		}
	}
	return false
}

// buildFederationServer 构造联邦独立 mTLS 监听（P1-6）。
// 仅暴露联邦入站端点（任务创建 / 设备视图），强制对端持证（RequireAndVerifyClientCert）。
// 端口 ≤0 或未启用联邦时返回 (nil, nil, nil)（不启用独立监听，复用主 HTTP）。
func (s *Server) buildFederationServer() (*http.Server, net.Listener, error) {
	if s.cfg.FederationPort <= 0 || s.fed == nil {
		return nil, nil, nil
	}
	tlsCfg, err := tlsutil.HTTPServerTLSConfig(s.cfg.FederationTLSCert, s.cfg.FederationTLSKey, s.cfg.FederationCA)
	if err != nil {
		return nil, nil, fmt.Errorf("federation server TLS: %w", err)
	}
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.cfg.FederationPort))
	if err != nil {
		return nil, nil, fmt.Errorf("federation listen: %w", err)
	}
	mux := http.NewServeMux()
	// 仅暴露联邦必需的入站端点；两者均已内置 P1-6 联邦签名验签。
	mux.HandleFunc("/api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			s.handleCreateTask(w, r)
			return
		}
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
	})
	mux.HandleFunc("/api/v1/devices", s.handleDevices)
	srv := &http.Server{
		Handler:           recoveryMiddleware(s.securityHeadersMiddleware(&jsonErrorMux{inner: mux})),
		TLSConfig:         tlsCfg,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return srv, lis, nil
}

// buildMetrics 构造 metrics HTTP server 与监听，渲染零依赖 Prometheus 文本指标（P2-1）。
func (s *Server) buildMetrics() (*http.Server, net.Listener) {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.metricsPort))
	if err != nil {
		log.Fatalf("[controlplane] metrics 监听失败 %d: %v", s.metricsPort, err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		// P1-5 metrics 访问控制：白名单非空时仅允许授权来源，否则 403。
		if !s.metricsAllowed(r.RemoteAddr) {
			ctx := logx.WithTrace(r.Context(), "metrics")
			logx.Warn(ctx, "metrics 访问被拒（不在 CIDR 白名单）", "remote", r.RemoteAddr)
			jsonError(w, http.StatusForbidden, "metrics access denied")
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		s.metrics.SetAgents(len(s.store.Agents("")))
		fmt.Fprint(w, s.metrics.Render())
	})
	return &http.Server{Handler: recoveryMiddleware(mux), ReadHeaderTimeout: 5 * time.Second}, lis
}

// handleDevices 处理 GET /api/v1/devices，按网关注入租户返回 segment -> 设备 列表。
// verifyFederationRequest 校验入站请求的联邦签名（P1-6 / task 83）。
// 仅当请求携带 X-Federation-Forwarded 标记（由本集群转发管理器设置）时才验签；
// 未携带则视为普通网关注入请求，返回 nil（不改变既有网关鉴权路径）。
// 签名覆盖 method + path + 时间戳 + 身份头 + sha256(body)，防任务体被中间人篡改；
// 验签读取 body 后以 NopCloser 复原，下游 handler（decodeJSONBody）可继续读取。
// 验签失败（密钥缺失/签名不符/时间戳超窗/body 超限）返回 error，调用方应拒绝（401）。
func (s *Server) verifyFederationRequest(r *http.Request) error {
	if r.Header.Get("X-Federation-Forwarded") != "1" {
		return nil // 非联邦转发请求，跳过（走网关注入逻辑）
	}
	if s.cfg.FederationSecret == "" {
		return fmt.Errorf("federation request received but --federation-secret not configured")
	}
	tsStr := r.Header.Get("X-Federation-Ts")
	sig := r.Header.Get("X-Federation-Sig")
	if tsStr == "" || sig == "" {
		return fmt.Errorf("missing federation signature headers")
	}
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid federation timestamp")
	}
	skew := time.Now().Unix() - ts
	if skew > federationSigMaxSkew || skew < -federationSigMaxSkew {
		return fmt.Errorf("federation timestamp skew out of window")
	}
	tenant := r.Header.Get("X-Tenant-ID")
	user := r.Header.Get("X-User-Id")
	roles := r.Header.Get("X-User-Roles")
	// task 83：请求体纳入签名覆盖（sha256(body) 摘要），防中间人篡改转发任务体。
	// 读取 body 参与验签后以 NopCloser 复原，保证下游 decodeJSONBody 仍能读取；
	// 限读 maxBodyBytes+1 防超大请求体内存攻击（超限即拒绝，不复原）。
	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil {
		return fmt.Errorf("read federation request body: %w", err)
	}
	if int64(len(bodyBytes)) > maxBodyBytes {
		return fmt.Errorf("federation request body exceeds %d bytes", maxBodyBytes)
	}
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	bodyDigest := sha256.Sum256(bodyBytes)
	mac := hmac.New(sha256.New, []byte(s.cfg.FederationSecret))
	mac.Write([]byte(strings.Join([]string{r.Method, r.URL.Path, tsStr, tenant, user, roles, hex.EncodeToString(bodyDigest[:])}, "|")))
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return fmt.Errorf("federation signature mismatch")
	}
	return nil
}


func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		log.Printf("controlplane: handleHealthz 写响应失败: %v", err)
	}
}

// handleInstallSh 处理 GET /install.sh：下发 agent 自举安装脚本（B1 bootstrap）。
// 脚本由 provision.InstallScript 按 --advertise-addr 动态生成（内嵌下载地址），
// 配合 bootstrap 命令 `curl -sSL <addr>/install.sh | sh -s -- --token=<tok>` 完成 agent 安装与注册。
func (s *Server) handleInstallSh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	// P1-5 访问审计：install.sh 是 bootstrap 端点，保持开放但审计访问来源供溯源。
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: clientIP(r, s.cfg.TrustProxy), Action: "bootstrap_install_sh", Target: "/install.sh",
		Detail: "remote=" + r.RemoteAddr,
	})
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(provision.InstallScript(s.cfg.AdvertiseAddr, version.Version))); err != nil {
		log.Printf("controlplane: handleInstallSh 写安装脚本失败: %v", err)
	}
}

// handleServeAgent 处理 GET /bin/opsmesh-agent：分发 agent 二进制本体（双模式同体），
// 供 install.sh 脚本下载安装。
func (s *Server) handleServeAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	// P1-5 访问审计：agent 二进制分发端点，保持开放但审计下载来源供溯源。
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: clientIP(r, s.cfg.TrustProxy), Action: "bootstrap_serve_agent", Target: "/bin/opsmesh-agent",
		Detail: "remote=" + r.RemoteAddr,
	})
	path, err := os.Executable()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "cannot resolve agent binary")
		return
	}
	f, err := os.Open(path)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "cannot open agent binary")
		return
	}
	defer f.Close()
	info, _ := f.Stat()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=opsmesh-agent")
	if info != nil {
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	}
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, f); err != nil {
		log.Printf("controlplane: handleServeAgent 写 agent 二进制失败: %v", err)
	}
}

// handleAutoProvision 处理 POST /api/v1/provision/auto：手动触发 B1 自动纳管编排。
// body: {"cidrs":["10.30.0.0/24"], "tenantID":"t1"}；cidrs 缺省时回退 --segment-cidr。
func (s *Server) handleAutoProvision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "provision:execute"); !ok {
		return
	}
	var body struct {
		CIDRs    []string `json:"cidrs"`
		TenantID string   `json:"tenantID"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil && r.ContentLength != 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid JSON: %v", err)})
		return
	}
	cidrs := body.CIDRs
	if len(cidrs) == 0 && s.cfg.SegmentCIDR != "" {
		cidrs = []string{s.cfg.SegmentCIDR}
	}
	// H6 认证防御：强制使用头中的租户 ID，忽略 body 中的 tenantID，防 body 覆盖头租户越权。
	tenant := actx.TenantID
	if len(cidrs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no cidrs provided (body.cidrs or --segment-cidr)"})
		return
	}
	// B1 修复 7：SSRF 校验 advertise URL（仅警告不阻止，控制面常部署内网）。
	// advertise URL 是控制面自身地址（运维配置），非用户可控，SSRF 校验仅做安全审计告警。
	if s.cfg.AdvertiseAddr != "" {
		if err := validateURLSSRF(s.cfg.AdvertiseAddr); err != nil {
			logx.Warn(r.Context(), "AdvertiseAddr SSRF 校验失败（仅警告，控制面常部署内网）", "url", s.cfg.AdvertiseAddr, "err", err)
		}
	}
	sum, err := provision.AutoProvision(r.Context(), provision.Deps{
		UpsertDevice: s.store.UpsertDevice,
		Provision:    s.store.Provision,
	}, s.cfg, cidrs, tenant)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: tenant, UserID: actx.UserID, Action: "auto_provision", Target: strings.Join(cidrs, ","),
		Detail: fmt.Sprintf("scanned=%d registered=%d provisioned=%d sshPushed=%d", sum.Scanned, sum.Registered, sum.Provisioned, sum.SSHPushed),
	})
	writeJSON(w, http.StatusOK, sum)
}

// autoProvisionLoop 后台周期执行 B1 自动纳管：仅当 --discover && --auto-provision 开启时，
// 每隔 discoverInterval 对 --segment-cidr 做存活扫描→登记候选设备→（配置 SSH key 时）推送 agent。
// 仅 leader 执行（避免多副本重复推送）。
func (s *Server) autoProvisionLoop(ctx context.Context) {
	if !s.cfg.Discover || !s.cfg.AutoProvision {
		return
	}
	const interval = 5 * time.Minute
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if s.store.IsLeader() && s.cfg.SegmentCIDR != "" {
			if _, err := provision.AutoProvision(ctx, provision.Deps{
				UpsertDevice: s.store.UpsertDevice,
				Provision:    s.store.Provision,
			}, s.cfg, []string{s.cfg.SegmentCIDR}, ""); err != nil {
				log.Printf("controlplane: autoProvisionLoop 自动纳管失败: %v", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}


// writeJSON 统一写出 JSON 响应。
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ============================================================================
// B1 修复 4：404/405 统一 JSON 错误格式
// ============================================================================

// jsonError 替换 http.Error，返回 application/json 格式的错误响应。
// 用于所有原 http.Error 调用点，统一错误格式为 {"error": "message"}。
func jsonError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// jsonErrorMux 包装 http.ServeMux，将 404 响应统一为 JSON 格式。
// 当 mux 无匹配路由时（pattern == ""），返回 JSON 404 而非默认的 text/plain。
// 405 由各 handler 内部用 jsonError 处理（显式方法检查）。
type jsonErrorMux struct {
	inner *http.ServeMux
}

func (m *jsonErrorMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h, pattern := m.inner.Handler(r)
	if pattern == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found", "path": r.URL.Path})
		return
	}
	h.ServeHTTP(w, r)
}

// ============================================================================
// B1 修复 3：列表 API 分页辅助函数
// ============================================================================

// paginateResult 分页响应结构。当客户端传 page 参数时，列表 API 返回此结构；
// 不传 page 时返回原数组（向后兼容）。
type paginateResult struct {
	Data     interface{} `json:"data"`
	Total    int         `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"pageSize"`
	HasMore  bool        `json:"hasMore"`
}

// parsePagination 从 query 参数解析 page/pageSize。
// page 从 1 开始，pageSize 默认 20、上限 200。
// page == 0 表示不分页（返回全量，向后兼容）。
func parsePagination(q url.Values) (page, pageSize int) {
	pageStr := q.Get("page")
	if pageStr == "" {
		return 0, 0 // 不分页
	}
	page, _ = strconv.Atoi(pageStr)
	if page < 1 {
		page = 1
	}
	pageSize = 20
	if v := q.Get("pageSize"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			pageSize = n
		}
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return page, pageSize
}

// responseCapture 捕获 http.Handler 的响应（状态码 + body），用于分页包装。
// 仅用于 GET 列表请求的分页捕获，非分页请求直接透传。
type responseCapture struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (c *responseCapture) WriteHeader(code int) {
	c.status = code
}

func (c *responseCapture) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	return c.body.Write(b)
}

// paginateJSONHandler 包装一个返回 JSON 数组的 handler，对 GET 请求支持 page/pageSize 分页。
// 不传 page 时直接透传（向后兼容）；传 page 时捕获原 handler 响应，解析 JSON 数组并分页。
// 用于 deploys/workflows 等外部 handler 注册的列表 API。
func paginateJSONHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Query().Get("page") == "" {
			h.ServeHTTP(w, r)
			return
		}
		// 捕获原 handler 响应
		rc := &responseCapture{ResponseWriter: w}
		h.ServeHTTP(rc, r)
		if rc.status != http.StatusOK {
			// 非 200 直接转发原响应
			for k, v := range rc.Header() {
				w.Header()[k] = v
			}
			if rc.status == 0 {
				rc.status = http.StatusOK
			}
			w.WriteHeader(rc.status)
			w.Write(rc.body.Bytes())
			return
		}
		// 解析 JSON 数组并分页
		page, pageSize := parsePagination(r.URL.Query())
		var arr []json.RawMessage
		if err := json.Unmarshal(rc.body.Bytes(), &arr); err != nil {
			// 非 JSON 数组（可能是对象），直接转发原响应
			for k, v := range rc.Header() {
				w.Header()[k] = v
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(rc.status)
			w.Write(rc.body.Bytes())
			return
		}
		total := len(arr)
		start := (page - 1) * pageSize
		if start >= total {
			start = total
		}
		end := start + pageSize
		if end > total {
			end = total
		}
		writeJSON(w, http.StatusOK, paginateResult{
			Data:     arr[start:end],
			Total:    total,
			Page:     page,
			PageSize: pageSize,
			HasMore:  end < total,
		})
	})
}

// ============================================================================
// B1 修复 7：SSRF 校验
// ============================================================================

// validateURLSSRF 校验 URL 是否安全（防 SSRF）。
// 拒绝条件：
//   - 协议非 http/https
//   - 主机解析为私网地址（10.x/172.16-31.x/192.168.x/127.x/169.254.x）
//   - IPv6 link-local（fe80::/10）
//   - 元数据地址（169.254.169.254）
//   - 主机无法解析
func validateURLSSRF(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	// 协议白名单：仅允许 http/https。
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("scheme %q not allowed (only http/https)", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("empty host in URL")
	}
	// 解析主机名中的 IP 地址（如果是域名则解析 DNS）。
	ips, err := net.LookupIP(host)
	if err != nil {
		// DNS 解析失败：可能是 IP 字面量，尝试直接解析。
		ip := net.ParseIP(host)
		if ip == nil {
			return fmt.Errorf("cannot resolve host %q: %w", host, err)
		}
		ips = []net.IP{ip}
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return fmt.Errorf("host %q resolves to private/loopback/link-local address %s", host, ip)
		}
	}
	return nil
}

// isPrivateIP 判断 IP 是否为私网/环回/链路本地/元数据地址。
func isPrivateIP(ip net.IP) bool {
	// IPv4 私网/环回/链路本地。
	if ip4 := ip.To4(); ip4 != nil {
		// 127.x.x.x（环回）
		if ip4[0] == 127 {
			return true
		}
		// 10.x.x.x（A 类私网）
		if ip4[0] == 10 {
			return true
		}
		// 172.16-31.x.x（B 类私网）
		if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
			return true
		}
		// 192.168.x.x（C 类私网）
		if ip4[0] == 192 && ip4[1] == 168 {
			return true
		}
		// 169.254.x.x（链路本地 + 云元数据）
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
		// 0.0.0.0（未指定）
		if ip4[0] == 0 && ip4[1] == 0 && ip4[2] == 0 && ip4[3] == 0 {
			return true
		}
		return false
	}
	// IPv6：拒绝 loopback (::1) 和 link-local (fe80::/10)。
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	// IPv6 ULA (fc00::/7) 私网地址。
	if len(ip) == 16 {
		if (ip[0] & 0xfe) == 0xfc {
			return true
		}
	}
	return false
}

