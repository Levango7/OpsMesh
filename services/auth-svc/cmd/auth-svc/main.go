package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	applog "github.com/Levango7/OpsMesh/pkg/log"
	"github.com/Levango7/OpsMesh/pkg/security"
	"github.com/Levango7/OpsMesh/pkg/tenant"
	"github.com/Levango7/OpsMesh/pkg/trace"
	authv1 "github.com/Levango7/OpsMesh/services/auth-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/auth"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/cache"
	httpgw "github.com/Levango7/OpsMesh/services/auth-svc/internal/http"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/server"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/service"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/store"
	"github.com/Levango7/OpsMesh/services/auth-svc/pkg/config"
)

func main() {
	// P1-6 结构化日志统一：把标准库 log 接入统一 JSON 管道（级别由 OPSMESH_LOG_LEVEL 控制）。
	// 必须最先调用——早于任何日志输出。
	lgr := applog.Init("auth-svc")
	cfg := config.Load()

	shutdown, err := trace.InitTracer("auth-svc", cfg.OTelEndpoint)
	if err != nil {
		lgr.Fatalf("Failed to initialize tracer: %v", err)
	}
	defer shutdown(context.Background())

	// Store 初始化：StoreType=sql 且 DSN 非空时接 MySQL（自动建表）；失败或未配置回退内存。
	var st store.Store = store.NewMemoryStore()
	if cfg.StoreType == "sql" && cfg.DSN != "" {
		if ms, err := store.NewMySQLStore(cfg.DSN); err != nil {
			// StoreType=sql 且 DSN 已显式配置 = 运维明确要求持久化存储。此时回退内存会让
			// 服务看起来正常（/health 仍 200）却在重启后丢光数据，属静默数据丢失陷阱；
			// 故直接阻断启动（对齐 controlplane --production 与 task-svc 的 fail-fast 策略）。
			lgr.Fatalf("MySQL store 初始化失败，停止启动: %v", err)
		} else {
			st = ms
			log.Printf("MySQL store 已启用")
			defer func() { _ = st.Close() }()
		}
	}
	eng := auth.NewEngine(cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)

	svc := service.NewService(eng, st)

	// A2 admin 弱口令轮换（与 controlplane enforceInitialCredentials 同语义）：
	// seed 的 admin/admin123 若仍是弱口令，启动时替换掉，MustChangePassword=true 保持
	// ——即使管理员不改密，公开的已知弱口令也无法登录。
	// 替换后的口令须交得到运维手里，否则管理员被锁死：优先 AUTH_SVC_ADMIN_PASSWORD，
	// 其次 AUTH_SVC_ADMIN_PASSWORD_FILE，都没有才回退"打印一次到日志"（会进日志采集，仅兜底）。
	if err := rotateDefaultAdminPassword(st, cfg); err != nil {
		lgr.Fatalf("[auth-svc] 初始 admin 口令引导失败: %v", err)
	}

	srv := server.NewServer(svc)

	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			tenant.GRPCInterceptor(),
			trace.GRPCServerInterceptor(),
		),
	)
	authv1.RegisterAuthServiceServer(grpcServer, srv)
	authv1.RegisterUserServiceServer(grpcServer, srv)
	authv1.RegisterRoleServiceServer(grpcServer, srv)
	authv1.RegisterPermissionServiceServer(grpcServer, srv)

	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("opsmesh.auth.v1.AuthService", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("opsmesh.auth.v1.UserService", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("opsmesh.auth.v1.RoleService", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("opsmesh.auth.v1.PermissionService", grpc_health_v1.HealthCheckResponse_SERVING)

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

	// A1 HTTP 网关（方案 B 用户中心后端）：AUTH_SVC_HTTP_ENABLED 默认 false——
	// 关闭时 auth-svc 仅 gRPC+health，与 controlplane 并存期不产生双轨 Cookie 冲突（R1）。
	if cfg.HTTPEnabled {
		// TD-60 安全能力初始化：Redis 缓存 + SessionStore。
		var redisCache *cache.Cache
		var sessionStore *cache.SessionStore
		if cfg.RedisAddr != "" {
			redisCache = cache.NewWithAddr("auth:", cfg.RedisAddr)
			defer redisCache.Close()
			if redisCache.Enabled() {
				log.Printf("Redis cache enabled at %s — guard/deviceFP backed by Redis", cfg.RedisAddr)
			}
		}
		if cfg.SessionStoreEnabled && cfg.RedisAddr != "" {
			sessionStore = cache.NewSessionStore(cfg.RedisAddr, cfg.SessionTTL)
			if sessionStore.Enabled() {
				log.Printf("Redis SessionStore enabled (TTL=%v) — stateful session management active", cfg.SessionTTL)
			} else {
				log.Printf("Redis SessionStore disabled (Redis unreachable) — degraded to stateless JWT mode")
			}
		}

		gw := httpgw.NewGatewayWithConfig(svc, cfg.CookieSecure, &httpgw.GatewayConfig{
			Cache:           redisCache,
			DeviceFPEnabled: cfg.DeviceFPEnabled,
			Sessions:        sessionStore,
		})
		gw.RegisterRoutes(mux)
		log.Printf("HTTP gateway enabled (AUTH_SVC_HTTP_ENABLED=true) — cookie_secure=%v deviceFP=%v",
			cfg.CookieSecure, cfg.DeviceFPEnabled)
	} else {
		log.Printf("HTTP gateway disabled (default) — auth-svc serves gRPC only; controlplane remains the sole login entry")
	}

	corsConfig := security.CORSConfig{
		AllowedOrigins: []string{"https://opsmesh.io", "https://app.opsmesh.io"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE"},
		AllowedHeaders: []string{"Content-Type", "Authorization", "X-User-ID"},
	}

	var handler http.Handler = mux
	handler = security.SecurityHeadersMiddleware()(handler)
	handler = security.ConnectionLimit(100)(handler)
	handler = security.RequestSizeLimit(1 << 20)(handler)
	handler = security.IPRateLimit(60, time.Minute)(handler)
	handler = security.UserRateLimit(120, time.Minute)(handler)
	handler = corsConfig.Middleware()(handler)
	handler = tenant.Middleware(cfg.JWTSecret)(handler)
	handler = trace.HTTPMiddleware("github.com/Levango7/OpsMesh/auth-svc")(handler)

	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		log.Printf("Starting gRPC server on :%d", cfg.GRPCPort)
		if err := grpcServer.Serve(grpcLis); err != nil {
			lgr.Fatalf("gRPC server failed: %v", err)
		}
	}()

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

	healthServer.SetServingStatus("opsmesh.auth.v1.AuthService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus("opsmesh.auth.v1.UserService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus("opsmesh.auth.v1.RoleService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus("opsmesh.auth.v1.PermissionService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	grpcServer.GracefulStop()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Println("Server stopped")
}

// rotateDefaultAdminPassword admin 弱口令启动加固（A2，与 controlplane
// controlplane.enforceInitialCredentials 同语义）：
//   - 仅当 admin 当前密码仍是 seed 弱口令 "admin123"（bcrypt 比对命中）时才替换，
//     幂等——管理员已改密则不覆盖；MemoryStore 每次启动新实例会重置（预期），
//     MySQLStore 持久化后重启不重复重置；
//   - 口令来源：cfg.AdminPassword 优先（须满足强口令策略，否则返回错误中止启动，
//     避免部署方以为已生效实则仍在用弱口令）；留空则生成 16 字节 hex 随机口令
//     （crypto/rand），经 cfg.AdminPasswordFile 落盘（0600），无文件通道时打印一次到日志；
//   - MustChangePassword=true 保持：首登强制改密语义与 controlplane 安全债一致。
func rotateDefaultAdminPassword(st store.Store, cfg *config.Config) error {
	u := st.GetUserByUsername("admin")
	if u == nil {
		return nil
	}
	if !auth.VerifyPassword(u.PasswordHash, "admin123") {
		return nil // 管理员已改密：不覆盖。
	}
	newPass := cfg.AdminPassword
	if newPass != "" {
		if msg := httpgw.ValidateStrongPassword(newPass); msg != "" {
			return fmt.Errorf("AUTH_SVC_ADMIN_PASSWORD 不满足强口令要求: %s", msg)
		}
	} else {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return fmt.Errorf("生成随机 admin 口令失败（crypto/rand 不可用）: %w", err)
		}
		newPass = hex.EncodeToString(b)
		if cfg.AdminPasswordFile != "" {
			if err := writeAdminPasswordFile(cfg.AdminPasswordFile, newPass); err != nil {
				return err
			}
			log.Printf("[auth-svc] 初始 admin 口令已写入 %s（权限 0600，内含明文，请读取后妥善保管并删除该文件）", cfg.AdminPasswordFile)
		} else {
			log.Printf("============================================================")
			log.Printf("[auth-svc] 安全提示：默认 admin 弱口令(admin123)已替换为随机口令。")
			log.Printf("[auth-svc]   一次性随机密码（请立即复制并登录后修改）: %s", newPass)
			log.Printf("[auth-svc]   注意：该口令已进入容器/服务日志；生产请改用 AUTH_SVC_ADMIN_PASSWORD")
			log.Printf("[auth-svc]   （Secret 注入）或 AUTH_SVC_ADMIN_PASSWORD_FILE 交付。")
			log.Printf("[auth-svc]   MustChangePassword=true 保持：首登仍强制改密。")
			log.Printf("============================================================")
		}
	}
	hash, err := auth.HashPassword(newPass)
	if err != nil {
		return fmt.Errorf("哈希 admin 口令失败: %w", err)
	}
	if err := st.ChangePassword(u.ID, hash); err != nil {
		return fmt.Errorf("admin 口令落库失败: %w", err)
	}
	// ChangePassword 语义会清 MustChangePassword=false（正常用户改密完成）——
	// 轮换不是用户主动改密，标记须置回 true（首登强制改密保持，与 controlplane
	// enforceInitialCredentials 的"改密后恢复标记"同语义）。
	if err := st.SetMustChangePassword(u.ID, true); err != nil {
		log.Printf("[auth-svc] admin 轮换置回 MustChangePassword 失败: %v", err)
	}
	if cfg.AdminPassword != "" {
		log.Printf("[auth-svc] 安全提示：admin 口令已按 AUTH_SVC_ADMIN_PASSWORD 设置（首登仍须改密）。")
	}
	return nil
}

// writeAdminPasswordFile 把口令写入 path（权限 0600，先截断再写）。
// 目录不存在时报错而非静默创建——配置写错的路径应该被发现，而不是把口令写到意料之外的位置。
func writeAdminPasswordFile(path, password string) error {
	dir := filepath.Dir(path)
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("AUTH_SVC_ADMIN_PASSWORD_FILE 目录不可用 %s: %w", dir, err)
	}
	if err := os.WriteFile(path, []byte(password+"\n"), 0o600); err != nil {
		return fmt.Errorf("写入 AUTH_SVC_ADMIN_PASSWORD_FILE %s 失败: %w", path, err)
	}
	return nil
}
