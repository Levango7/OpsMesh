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

	taskv1 "github.com/Levango7/OpsMesh/services/task-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/scheduler"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/server"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/service"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/store"
	"github.com/Levango7/OpsMesh/services/task-svc/pkg/config"
	"opsmesh/pkg/circuit"
	"opsmesh/pkg/compress"
	"opsmesh/pkg/cron"
	"opsmesh/pkg/metrics"
	"opsmesh/pkg/ratelimit"
	"opsmesh/pkg/trace"
)

// cronMatch 包装 pkg/cron.Match——main.go 不希望每次 fire 闭包都写完整包名。
func cronMatch(expr string, now time.Time) (bool, error) { return cron.Match(expr, now) }

func main() {
	cfg := config.Load()

	metrics.Init("task-svc")

	shutdown, err := trace.InitTracer("task-svc", cfg.OTelEndpoint)
	if err != nil {
		log.Fatalf("Failed to initialize tracer: %v", err)
	}
	defer shutdown(context.Background())

	// Store 初始化：StoreType=sql 且 DSN 非空时接 MySQL（自动建表）；失败或未配置回退内存。
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
			log.Printf("MySQL store 初始化失败，回退 memory: %v", err)
		} else {
			ts, ss, rs, bs = ms, ms, ms, ms
			log.Printf("MySQL store 已启用")
			defer func() { _ = ms.Close() }()
		}
	}
	svc := service.NewService(ts, ss, rs, bs)

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
		log.Fatalf("Failed to listen on gRPC port %d: %v", cfg.GRPCPort, err)
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

	var handler http.Handler = mux
	handler = metrics.HTTPMiddleware(handler)
	handler = ratelimit.Middleware()(handler)
	handler = compress.Middleware()(handler)
	handler = trace.HTTPMiddleware("opsmesh/task-svc")(handler)

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
	// renew 单进程假实现（永真），A-2 切流后接 SQL leader_lease 升级。
	sched := scheduler.New(
		schedulerRootCtx,
		// reclaim：直接读 store.AllTasks 复用 controlplane/store/memory.go:756 ReclaimStaleTasks
		// 的判定逻辑——A-1 任务必达机制逐字对齐。
		func(_ context.Context, maxAge time.Duration) int {
			if maxAge <= 0 {
				maxAge = 30 * time.Second // task-svc 默认租约（与 controlplane server_tasks.go:319 cfg.TaskLeaseSec 默认 30s 对齐）
			}
			cutoff := time.Now().Add(-maxAge)
			reclaimed := 0
			for _, t := range ts.AllTasks() {
				if t.Status != "running" || t.ClaimedAt.IsZero() || !t.ClaimedAt.Before(cutoff) {
					continue
				}
				// 防双跑：持有者心跳活跃则不回收（task-svc 无 agent 心跳索引，简化为：租约超时即回收；
				// 收敛后由 controlplane 端 agent 侧防误回收——A-2 阶段补跨服务心跳查询）。
				t.Status = "pending"
				t.ClaimedAt = time.Time{}
				t.ClaimedBy = ""
				ts.UpdateTask(t) // task-svc store 接口已有 UpdateTask
				reclaimed++
			}
			return reclaimed
		},
		// fire：直接读 store.AllTasks 复用 controlplane/store/memory.go:786 FireDueSchedules
		// 的派生逻辑——cron 匹配 + 本分钟去重。
		func(_ context.Context, now time.Time) int {
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
				// 派生 pending 实例——与 controlplane 同构（保留 Schedule 字段、ParentID 指向模板）。
				// A-1 阶段：仅更新模板的 LastFiredAt 防止重入，**不实际派生**——派生实例会改动
				// 任务必达行为，A-2 阶段切流时再做（避免本轮引入新写路径影响双轨对照）。
				t.LastFiredAt = now
				ts.UpdateTask(t)
				fired++
			}
			return fired
		},
		// renew：A-1 单进程假实现（永真），A-2 阶段接 SQLStore 真选主。
		func(_ context.Context, _ time.Duration) bool { return true },
	)
	sched.Start()

	go func() {
		log.Printf("Starting gRPC server on :%d", cfg.GRPCPort)
		if err := grpcServer.Serve(grpcLis); err != nil {
			log.Fatalf("gRPC server failed: %v", err)
		}
	}()

	go func() {
		log.Printf("Starting HTTP health server on :%d", cfg.HTTPPort)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server failed: %v", err)
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
