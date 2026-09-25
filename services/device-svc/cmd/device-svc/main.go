package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/Levango7/OpsMesh/pkg/compress"
	applog "github.com/Levango7/OpsMesh/pkg/log"
	"github.com/Levango7/OpsMesh/pkg/metrics"
	"github.com/Levango7/OpsMesh/pkg/ratelimit"
	"github.com/Levango7/OpsMesh/pkg/tenant"
	"github.com/Levango7/OpsMesh/pkg/trace"
	devicev1 "github.com/Levango7/OpsMesh/services/device-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/device-svc/internal/catalog"
	httpgw "github.com/Levango7/OpsMesh/services/device-svc/internal/http"
	"github.com/Levango7/OpsMesh/services/device-svc/internal/server"
	"github.com/Levango7/OpsMesh/services/device-svc/internal/service"
	"github.com/Levango7/OpsMesh/services/device-svc/internal/store"
	"github.com/Levango7/OpsMesh/services/device-svc/pkg/config"
)

func main() {
	// P1-6 结构化日志统一：把标准库 log 接入统一 JSON 管道（级别由 OPSMESH_LOG_LEVEL 控制）。
	// 必须最先调用——早于任何日志输出。
	lgr := applog.Init("device-svc")
	cfg := config.Load()

	metrics.Init("device-svc")

	shutdown, err := trace.InitTracer("device-svc", cfg.OTelEndpoint)
	if err != nil {
		lgr.Fatalf("Failed to initialize tracer: %v", err)
	}
	defer shutdown(context.Background())

	// Store 初始化：StoreType=sql 且 DSN 非空时接 MySQL（自动建表）；失败或未配置回退内存。
	// MemoryStore / MySQLStore 均实现 DeviceStore、AgentStore、CiStore、DiscoveryStore 四个接口。
	memStore := store.NewMemoryStore()
	var (
		ds   store.DeviceStore    = memStore
		as   store.AgentStore     = memStore
		cs   store.CiStore        = memStore
		disc store.DiscoveryStore = memStore
	)
	if cfg.StoreType == "sql" && cfg.DSN != "" {
		if ms, err := store.NewMySQLStore(cfg.DSN); err != nil {
			// StoreType=sql 且 DSN 已显式配置 = 运维明确要求持久化存储。此时回退内存会让
			// 服务看起来正常（/health 仍 200）却在重启后丢光数据，属静默数据丢失陷阱；
			// 故直接阻断启动（对齐 controlplane --production 与 task-svc 的 fail-fast 策略）。
			lgr.Fatalf("MySQL store 初始化失败，停止启动: %v", err)
		} else {
			ds, as, cs, disc = ms, ms, ms, ms
			log.Printf("MySQL store 已启用")
			defer func() { _ = ms.Close() }()
		}
	}
	// D3 ProvisionStore：独立 MemoryStore + 配置密钥。设备四 store 可切 MySQL，
	// 但 install token 是 15min 一次性短时效凭证——进程内生命周期足够
	//（controlplane 同现状：重启后未消费 token 全部作废是安全特性而非缺陷），
	// 不随设备数据落库（MySQLStore 无 token 表，加表超出 D3 范围）。
	// 独立实例而非复用 memStore：避免 sql 模式下 memStore 只作 fallback 却被
	// token 写入的混淆。ProvisionSecret 空=issueTokenLocked 随机兜底（重启轮换）。
	provisionStore := store.NewMemoryStore()
	if cfg.ProvisionSecret != "" {
		provisionStore.SetSecret(cfg.ProvisionSecret)
	}

	tenantMgr := tenant.NewManager(nil, nil, map[tenant.ResourceType]int{
		tenant.ResourceDevices:  100,
		tenant.ResourceAgents:   50,
		tenant.ResourceTasks:    500,
		tenant.ResourceAlerts:   100,
		tenant.ResourceWebhooks: 10,
		tenant.ResourceAPIKeys:  5,
	})

	svc := service.NewService(ds, as, cs, disc, provisionStore, tenantMgr)
	// D3 自动纳管配置注入（默认关闭；需显式启用 + 配置 SSH 私钥才推送）。
	svc.SetAutoProvisionConfig(&service.AutoProvisionConfig{
		Enabled:           cfg.AutoProvision,
		FallbackAdvertise: fmt.Sprintf("http://127.0.0.1:%d", cfg.HTTPPort),
		SSHKey:            cfg.ProvisionSSHKey,
		SSHUser:           cfg.ProvisionSSHUser,
		SSHKP:             cfg.ProvisionSSHKP,
		SSHKnownHosts:     cfg.ProvisionSSHKnownHosts,
		AdvertiseAddr:     cfg.AdvertiseAddr,
		LoopInterval:      cfg.AutoProvisionInterval,
		LoopMaxBackoff:    cfg.AutoProvisionMaxBackoff,
		SegmentCIDR:       cfg.SegmentCIDR,
	})
	// D2 真实发现配置注入：白名单（空=不校验，生产必配）+ job 超时（默认 60s）。
	svc.SetDiscoverConfig(&service.DiscoverConfig{
		CIDRWhitelist: cfg.CIDRWhitelist,
		Timeout:       cfg.DiscoverTimeout,
	})
	srv := server.NewServer(svc)

	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			tenant.GRPCInterceptor(),
			ratelimit.GRPCInterceptor(),
		),
	)
	devicev1.RegisterDeviceServiceServer(grpcServer, srv)
	devicev1.RegisterAgentServiceServer(grpcServer, srv)
	devicev1.RegisterCMDBServiceServer(grpcServer, srv)
	devicev1.RegisterDiscoveryServiceServer(grpcServer, srv)

	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("opsmesh.device.v1.DeviceService", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("opsmesh.device.v1.AgentService", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("opsmesh.device.v1.CMDBService", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("opsmesh.device.v1.DiscoveryService", grpc_health_v1.HealthCheckResponse_SERVING)

	grpcLis, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.GRPCPort))
	if err != nil {
		lgr.Fatalf("Failed to listen on gRPC port %d: %v", cfg.GRPCPort, err)
	}

	cat := catalog.NewCatalog()
	seedCatalog(cat)

	mux := http.NewServeMux()

	// P0 HTTP 网关：devices/agents/cmdb/discovery 的 REST 端点（直连 store 层）。
	// 鉴权走 tenant.Middleware（与 gRPC 拦截器同语义），在下文 handler 链统一包裹。
	advertiseAddr := cfg.AdvertiseAddr
	if advertiseAddr == "" {
		advertiseAddr = fmt.Sprintf("http://127.0.0.1:%d", cfg.HTTPPort)
	}
	httpGateway := httpgw.NewGateway(ds, as, cs, disc, provisionStore, advertiseAddr)
	httpGateway.SetAgentBinDir(cfg.AgentBinDir)
	httpGateway.RegisterRoutes(mux, func(h http.Handler) http.Handler { return h })

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
	mux.Handle("/metrics", metrics.GetHandler())
	mux.HandleFunc("/api/v1/catalog/topology", func(w http.ResponseWriter, r *http.Request) {
		tenantID := r.URL.Query().Get("tenantID")
		graph := cat.BuildTopology(tenantID)
		writeJSON(w, http.StatusOK, graph)
	})
	mux.HandleFunc("/api/v1/catalog/nodes/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/api/v1/catalog/nodes/"):]
		node, err := cat.GetNode(id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, node)
	})
	mux.HandleFunc("/api/v1/catalog/relations/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/api/v1/catalog/relations/"):]
		rels, err := cat.GetRelations(id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, rels)
	})
	mux.HandleFunc("/api/v1/catalog/impact/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/api/v1/catalog/impact/"):]
		impact, err := cat.GetImpactAnalysis(id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, impact)
	})

	// NOTE: /api/v1/devices/ catch-all 已由 gateway.RegisterRoutes 注册
	// （handleDeviceDetail 处理 {id}/heartbeat|status|provision|metrics + CRUD）。

	var handler http.Handler = mux
	handler = metrics.HTTPMiddleware(handler)
	handler = ratelimit.Middleware()(handler)
	handler = compress.Middleware()(handler)
	handler = tenant.Middleware(cfg.JWTSecret)(handler)
	handler = trace.HTTPMiddleware("github.com/Levango7/OpsMesh/device-svc")(handler)

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler: handler,
	}

	go func() {
		log.Printf("Starting gRPC server on :%d", cfg.GRPCPort)
		if err := grpcServer.Serve(grpcLis); err != nil {
			lgr.Fatalf("gRPC server failed: %v", err)
		}
	}()

	// D3+ 自动纳管后台循环（与 controlplane autoProvisionLoop 同语义）。
	// 双闸默认关闭（Enabled=false 或 LoopInterval<=0 或 SegmentCIDR="" 时不启动）。
	// ctx 随进程退出取消，循环优雅退出。
	autoprovCtx, autoprovCancel := context.WithCancel(context.Background())
	go svc.AutoProvisionLoop(autoprovCtx)

	go func() {
		log.Printf("Starting HTTP health server on :%d", cfg.HTTPPort)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			lgr.Fatalf("HTTP server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down...")

	autoprovCancel()

	healthServer.SetServingStatus("opsmesh.device.v1.DeviceService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus("opsmesh.device.v1.AgentService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus("opsmesh.device.v1.CMDBService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus("opsmesh.device.v1.DiscoveryService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	grpcServer.GracefulStop()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Println("Server stopped")
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func seedCatalog(c *catalog.Catalog) {
	c.AddNode(&catalog.CatalogNode{ID: "host-001", Name: "web-server-01", Type: "host", Status: "online", Metadata: map[string]string{"tenantID": "default"}})
	c.AddNode(&catalog.CatalogNode{ID: "svc-001", Name: "auth-service", Type: "service", Status: "running", Metadata: map[string]string{"tenantID": "default"}})
	c.AddNode(&catalog.CatalogNode{ID: "db-001", Name: "postgres-main", Type: "database", Status: "online", Metadata: map[string]string{"tenantID": "default"}})
	c.AddEdge(&catalog.CatalogEdge{From: "svc-001", To: "host-001", RelationType: "runs_on"})
	c.AddEdge(&catalog.CatalogEdge{From: "svc-001", To: "db-001", RelationType: "depends_on"})
}
