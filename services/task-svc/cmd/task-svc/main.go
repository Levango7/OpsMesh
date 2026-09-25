package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/Levango7/OpsMesh/pkg/circuit"
	"github.com/Levango7/OpsMesh/pkg/compress"
	"github.com/Levango7/OpsMesh/pkg/cron"
	applog "github.com/Levango7/OpsMesh/pkg/log"
	"github.com/Levango7/OpsMesh/pkg/metrics"
	"github.com/Levango7/OpsMesh/pkg/ratelimit"
	"github.com/Levango7/OpsMesh/pkg/tenant"
	"github.com/Levango7/OpsMesh/pkg/trace"
	taskv1 "github.com/Levango7/OpsMesh/services/task-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/approval"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/events"
	httpgw "github.com/Levango7/OpsMesh/services/task-svc/internal/http"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/leader"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/scheduler"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/server"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/service"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/store"
	"github.com/Levango7/OpsMesh/services/task-svc/pkg/config"
)

// cronMatch 包装 pkg/cron.Match——main.go 不希望每次 fire 闭包都写完整包名。
func cronMatch(expr string, now time.Time) (bool, error) { return cron.Match(expr, now) }

func main() {
	// P1-6 结构化日志统一：把标准库 log 接入统一 JSON 管道（级别由 OPSMESH_LOG_LEVEL 控制）。
	// 必须最先调用——早于任何日志输出。
	lgr := applog.Init("task-svc")
	cfg := config.Load()

	metrics.Init("task-svc")

	shutdown, err := trace.InitTracer("task-svc", cfg.OTelEndpoint)
	if err != nil {
		lgr.Fatalf("Failed to initialize tracer: %v", err)
	}
	defer shutdown(context.Background())

	// Store 初始化：SQL 模式自动迁移 tasks 表；初始化失败必须阻断启动，不能静默回退内存。
	// MemoryStore / MySQLStore 均实现 TaskStore、ScheduleStore、ResultStore、BatchStore 四个接口。
	memStore := store.NewMemoryStore()
	var (
		ts store.TaskStore     = memStore
		ss store.ScheduleStore = memStore
		rs store.ResultStore   = memStore
		bs store.BatchStore    = memStore
	)
	if cfg.StoreType == "sql" && cfg.DSN != "" {
		if ms, err := store.NewMySQLStore(cfg.DSN); err != nil {
			lgr.Fatalf("MySQL store 初始化失败，停止启动: %v", err)
		} else {
			ts, ss, rs, bs = ms, ms, ms, ms
			log.Printf("MySQL store 已启用")
			defer func() { _ = ms.Close() }()
		}
	}
	svc := service.NewService(ts, ss, rs, bs)

	// M5 增强：初始化 canary store（内存索引，不持久化）
	svc.SetCanaryStore(store.NewMemoryStore())

	// A-2 阶段：事件总线/审计/SSE 桥接注入（接口注入模式，生产环境由 main 注入真实实现）。
	// 默认 LogBus/LogAuditSink/StubSSEBridge（开发/单机可见）；生产环境可通过环境变量切换。
	svc.SetEventBus(events.LogBus{})
	svc.SetAuditSink(events.LogAuditSink{})
	svc.SetSSEBridge(events.StubSSEBridge{})

	cb := circuit.New("task-execution", 5, 30*time.Second)
	svc.SetCircuitBreaker(cb)

	srv := server.NewServer(svc)

	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			ratelimit.GRPCInterceptor(),
		),
	)
	taskv1.RegisterTaskServiceServer(grpcServer, srv)
	taskv1.RegisterScheduleServiceServer(grpcServer, srv)
	taskv1.RegisterResultServiceServer(grpcServer, srv)
	taskv1.RegisterBatchServiceServer(grpcServer, srv)

	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("opsmesh.task.v1.TaskService", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("opsmesh.task.v1.ScheduleService", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("opsmesh.task.v1.ResultService", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("opsmesh.task.v1.BatchService", grpc_health_v1.HealthCheckResponse_SERVING)

	grpcLis, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.GRPCPort))
	if err != nil {
		lgr.Fatalf("Failed to listen on gRPC port %d: %v", cfg.GRPCPort, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
	mux.Handle("/metrics", metrics.GetHandler())

	// P0 HTTP 业务网关：将 gRPC service 方法包装为 REST 端点，路径对齐 controlplane。
	// /api/v1/tasks, /api/v1/tasks/{id}/cancel|result|approve|reject, /api/v1/schedules[/{id}]。
	// 鉴权/租户走 tenant.Middleware（在下文 handler 链统一包裹，与 gRPC 拦截器同语义）。
	// 默认启用（cfg.HTTPGatewayEnabled）；测试/CI 可经 TASK_SVC_HTTP_GATEWAY_ENABLED=false 关闭。
	if cfg.HTTPGatewayEnabled {
		httpGateway := httpgw.NewGateway(svc, cfg.JWTSecret)
		// M5 增强：初始化审批引擎
		approvalEngine := approval.New()
		httpGateway.SetApprovalEngine(approvalEngine)
		httpGateway.RegisterRoutes(mux)
		log.Printf("HTTP 业务网关已启用（/api/v1/tasks, /api/v1/schedules）")
	} else {
		log.Printf("HTTP 业务网关已关闭（TASK_SVC_HTTP_GATEWAY_ENABLED=false）")
	}

	var handler http.Handler = mux
	handler = metrics.HTTPMiddleware(handler)
	handler = ratelimit.Middleware()(handler)
	handler = compress.Middleware()(handler)
	// 租户中间件：从 X-Tenant-ID 头或 JWT 提取 tenantID 注入 context（与 gRPC 拦截器同语义）。
	// HTTP 网关 handler 经此中间件后可从 context 提取租户（extractAuth 优先读 context）。
	handler = tenant.Middleware(cfg.JWTSecret)(handler)
	handler = trace.HTTPMiddleware("github.com/Levango7/OpsMesh/task-svc")(handler)

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler: handler,
	}

	// schedulerRootCtx 是 scheduler 三循环的共享生命周期上下文：信号触发 cancel
	// 时 3 循环 select 全部退出（与 controlplane 4 循环的 ctx 取消退出行为一致）。
	schedulerRootCtx, cancelScheduler := context.WithCancel(context.Background())
	defer cancelScheduler()

	// A-1 阶段：fire/reclaim 闭包直接读 store 全部任务、按 controlplane 同样规则
	// 派生 pending 实例或回收超期 running——不污染 TaskStore 公开方法集。
	// 派生规则：ParentID=="" + Schedule!="" + cron.Match 命中 + 本分钟未触发过。
	// 回收规则：status=running + ClaimAt 早于 maxAge + 持有者无活跃心跳。
	// A-2 阶段：renew 通过 LeaderElector 接口注入（stub 永真为默认，
	// 生产环境可注入 K8sLeaseElector 实现多副本选主）。
	elector := leader.NewStub() // A-2 默认：stub 永真（单进程/CI）；生产注入 K8sLeaseElector
	defer elector.Close()
	// ShadowMode=true 时，task-svc 不执行 fire/reclaim（真正只读），只有 controlplane 调度。
	// 这防止共库双调度器并发写 tasks 表。
	var reclaimFn scheduler.ReclaimFunc
	var fireFn scheduler.FireFunc
	if !cfg.ShadowMode {
		reclaimFn = func(_ context.Context, maxAge time.Duration) int {
			if maxAge <= 0 {
				maxAge = 30 * time.Second
			}
			cutoff := time.Now().Add(-maxAge)
			reclaimed := 0
			for _, t := range ts.AllTasks() {
				if t.Status != "running" || t.ClaimedAt.IsZero() || !t.ClaimedAt.Before(cutoff) {
					continue
				}
				t.Status = "pending"
				t.ClaimedAt = time.Time{}
				t.ClaimedBy = ""
				if ts.UpdateTask(t) {
					reclaimed++
				} else {
					log.Printf("[reclaim] UpdateTask failed for task %s", t.TaskID)
				}
			}
			return reclaimed
		}
		fireFn = func(_ context.Context, now time.Time) int {
			fired := 0
			minuteStart := now.Truncate(time.Minute)
			for _, t := range ts.AllTasks() {
				if t.ParentID != "" || t.Schedule == "" {
					continue
				}
				ok, err := cronMatch(t.Schedule, now)
				if err != nil || !ok {
					continue
				}
				if !t.LastFiredAt.IsZero() && !t.LastFiredAt.Before(minuteStart) {
					continue
				}
				t.LastFiredAt = now
				if ts.UpdateTask(t) {
					fired++
				} else {
					log.Printf("[fire] UpdateTask failed for task %s", t.TaskID)
				}
			}
			return fired
		}
	}
	sched := scheduler.New(
		schedulerRootCtx,
		reclaimFn,
		fireFn,
		func(ctx context.Context, ttl time.Duration) bool {
			return elector.Renew(ctx, ttl)
		},
	)
	sched.Start()

	// A-2 影子模式（TASK_SVC_SHADOW_MODE=true 开启）：在常规 scheduler 之外额外启动
	// 只读影子循环，评估 task 派生/回收期望并与现状对比；不写任何 store 状态。
	// 生命周期与 schedulerRootCtx 绑定，cancel 时自动退出。
	if cfg.ShadowMode {
		shadow := scheduler.NewShadowLoop(ts)
		shadow.Start(schedulerRootCtx)
	}

	go func() {
		log.Printf("Starting gRPC server on :%d", cfg.GRPCPort)
		if err := grpcServer.Serve(grpcLis); err != nil {
			lgr.Fatalf("gRPC server failed: %v", err)
		}
	}()

	go func() {
		log.Printf("Starting HTTP server on :%d (health/ready/metrics + business gateway)", cfg.HTTPPort)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			lgr.Fatalf("HTTP server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down...")

	healthServer.SetServingStatus("opsmesh.task.v1.TaskService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus("opsmesh.task.v1.ScheduleService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus("opsmesh.task.v1.ResultService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus("opsmesh.task.v1.BatchService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	grpcServer.GracefulStop()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Println("Server stopped")
}
