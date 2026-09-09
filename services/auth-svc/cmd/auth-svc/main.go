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
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	authv1 "github.com/Levango7/OpsMesh/services/auth-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/auth"
	httpgw "github.com/Levango7/OpsMesh/services/auth-svc/internal/http"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/server"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/service"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/store"
	"github.com/Levango7/OpsMesh/services/auth-svc/pkg/config"
	"opsmesh/pkg/security"
	"opsmesh/pkg/tenant"
	"opsmesh/pkg/trace"
)

func main() {
	cfg := config.Load()

	shutdown, err := trace.InitTracer("auth-svc", cfg.OTelEndpoint)
	if err != nil {
		log.Fatalf("Failed to initialize tracer: %v", err)
	}
	defer shutdown(context.Background())

	// Store 初始化：StoreType=sql 且 DSN 非空时接 MySQL（自动建表）；失败或未配置回退内存。
	var st store.Store = store.NewMemoryStore()
	if cfg.StoreType == "sql" && cfg.DSN != "" {
		if ms, err := store.NewMySQLStore(cfg.DSN); err != nil {
			log.Printf("MySQL store 初始化失败，回退 memory: %v", err)
		} else {
			st = ms
			log.Printf("MySQL store 已启用")
			defer func() { _ = st.Close() }()
		}
	}
	eng := auth.NewEngine(cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)

	svc := service.NewService(eng, st)

	// A2 admin 弱口令轮换（与 controlplane rotateDefaultAdminPassword 同语义）：
	// seed 的 admin/admin123 若仍是弱口令，启动时换为随机口令（仅打印一次日志），
	// MustChangePassword=true 保持——即使管理员不改密，已知弱口令也无法登录。
	rotateDefaultAdminPassword(st)

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

	// A1 HTTP 网关（方案 B 用户中心后端）：AUTH_SVC_HTTP_ENABLED 默认 false——
	// 关闭时 auth-svc 仅 gRPC+health，与 controlplane 并存期不产生双轨 Cookie 冲突（R1）。
	if cfg.HTTPEnabled {
		gw := httpgw.NewGateway(svc, cfg.CookieSecure)
		gw.RegisterRoutes(mux)
		log.Printf("HTTP gateway enabled (AUTH_SVC_HTTP_ENABLED=true) — cookie_secure=%v", cfg.CookieSecure)
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
	handler = trace.HTTPMiddleware("opsmesh/auth-svc")(handler)

	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

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

// rotateDefaultAdminPassword admin 弱口令启动轮换（A2，与 controlplane
// auth.go:336 rotateDefaultAdminPassword 同语义）：
//   - 仅当 admin 当前密码仍是 seed 弱口令 "admin123"（bcrypt 比对命中）时才重置，
//     幂等——管理员已改密则不覆盖；MemoryStore 每次启动新实例会重置（预期），
//     MySQLStore 持久化后重启不重复重置；
//   - 随机口令 16 字节 hex（crypto/rand），仅打印一次日志（须妥善保管）；
//   - MustChangePassword=true 保持：首登强制改密语义与 controlplane 安全债一致。
func rotateDefaultAdminPassword(st store.Store) {
	u := st.GetUserByUsername("admin")
	if u == nil {
		return
	}
	if !auth.VerifyPassword(u.PasswordHash, "admin123") {
		return // 管理员已改密：不覆盖。
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		log.Printf("[auth-svc] admin 弱口令轮换失败（crypto/rand 不可用，保持原口令+强制改密兜底）: %v", err)
		return
	}
	newPass := hex.EncodeToString(b)
	hash, err := auth.HashPassword(newPass)
	if err != nil {
		log.Printf("[auth-svc] admin 弱口令轮换哈希失败: %v", err)
		return
	}
	if err := st.ChangePassword(u.ID, hash); err != nil {
		log.Printf("[auth-svc] admin 弱口令轮换落库失败: %v", err)
		return
	}
	// ChangePassword 语义会清 MustChangePassword=false（正常用户改密完成）——
	// 轮换不是用户主动改密，标记须置回 true（首登强制改密保持，与 controlplane
	// rotateDefaultAdminPassword 的"改密后恢复标记"同语义）。
	if err := st.SetMustChangePassword(u.ID, true); err != nil {
		log.Printf("[auth-svc] admin 轮换置回 MustChangePassword 失败: %v", err)
	}
	log.Printf("============================================================")
	log.Printf("[auth-svc] 安全提示：默认 admin 弱口令(admin123)已替换为随机口令。")
	log.Printf("[auth-svc]   一次性随机密码（请立即复制并登录后修改）: %s", newPass)
	log.Printf("[auth-svc]   MustChangePassword=true 保持：首登仍强制改密。")
	log.Printf("============================================================")
}
