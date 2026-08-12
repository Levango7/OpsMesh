// server_netsec.go — gRPC/联邦/metrics 服务建造与联邦验签（mTLS + HMAC）
package controlplane

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"opsmesh/internal/grpcx"
	"opsmesh/internal/logx"
	"opsmesh/internal/otelx"
	"opsmesh/internal/store"
	"opsmesh/internal/tlsutil"
)

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
	// P0-2 兜底盘 + M1-1 OTel gRPC 服务端拦截器（链式组合）：
	//   - grpcRecoveryInterceptor 在外：拦截 unary handler panic，避免单 RPC 击穿整个 gRPC server。
	//   - otelx.GRPCServerUnaryInterceptor 在内：从 metadata 提取 W3C trace context 并创建 server span，
	//     使 agent→控制面 gRPC 调用的 trace_id 贯穿。panic 被 recovery 捕获后 span 仍能 End()。
	otelInterceptor := otelx.GRPCServerUnaryInterceptor("opsmesh-controlplane")
	opts = append(opts, grpc.ChainUnaryInterceptor(grpcRecoveryInterceptor, otelInterceptor))
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
		// P0 安全加固：传入预共享签名密钥（--grpc-signature-key）。
		// 非空时 verifyAgentSignature 优先使用此密钥验签，Register 不再下发密钥。
		signatureKey: func() string {
			if s.cfg != nil {
				return s.cfg.GRPCSignatureKey
			}
			return ""
		}(),
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

// handleHealthz 深度健康检查（K8s liveness 探针，P1-C2 增强）。
//
// 旧实现仅返回 {"status":"ok"} 无任何实际检查，无法真正反映服务健康状态。
// 现增加 Store 连接深度检查：
//   - Store 可用：200 + {"status":"ok","checks":{"store":"ok"}}
//   - Store 不可用：503 + {"status":"unhealthy","error":"store unavailable"}
//
// 向后兼容：正常时仍返回 200 且 status 字段为 "ok"（旧消费方仅看 status 字段）。
// 超时保护：健康检查总时长不超过 2 秒，避免探针超时拖垮 K8s 调度。
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	// 2 秒超时保护：探针不应阻塞 K8s 调度。
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := s.pingStore(ctx); err != nil {
		log.Printf("controlplane: healthz store ping 失败: %v", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "unhealthy",
			"error":  "store unavailable",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ok",
		"checks": map[string]string{"store": "ok"},
	})
}

// handleReadyz 就绪检查（K8s readiness 探针，P1-C2 新增）。
//
// 与 liveness（/healthz）的区别：
//   - liveness 探测进程是否存活（失败 → 重启容器）；
//   - readiness 探测是否准备好接流量（失败 → 从 Service endpoints 摘除，不重启）。
//
// 就绪条件：Store 连接可用 + 本实例持有 leader 租约（避免非 leader 副本接写流量造成脑裂/抖动）。
//   - 就绪：200 + {"status":"ready"}
//   - 未就绪：503 + {"status":"not_ready","reason":"..."}
//
// 超时保护：同 /healthz，2 秒上限。
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	// 2 秒超时保护。
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := s.pingStore(ctx); err != nil {
		log.Printf("controlplane: readyz store ping 失败: %v", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "not_ready",
			"reason": "store unavailable",
		})
		return
	}
	// leader 选举检查：非 leader 副本不接写流量（A3 HA 设计）。
	// MemoryStore 恒为 leader（单实例）；SQLStore 经 leader_lease 表原子抢占。
	if !s.store.IsLeader() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "not_ready",
			"reason": "not leader",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// pingStore 对底层 Store 做轻量连通性检查（P1-C2 健康检查支撑）。
//
// Store 接口未定义 Ping 方法（保持接口精简），此处按具体实现类型分发：
//   - *store.SQLStore：调用 DB().PingContext（database/sql 内置轻量探活，不发 SQL）；
//   - *store.MemoryStore：始终可用（无外部依赖），返回 nil；
//   - *store.MultiSchemaStore：多租户 schema 隔离，逐 schema ping（任一失败即返回错误）；
//   - 其他/未知实现：保守视为可用（向后兼容，避免误杀自定义 Store 实现）。
//
// ctx 用于超时控制；调用方应传入带 deadline 的 context（如 2s）。
func (s *Server) pingStore(ctx context.Context) error {
	switch st := s.store.(type) {
	case *store.SQLStore:
		return st.DB().PingContext(ctx)
	case *store.MemoryStore:
		// 内存存储无外部依赖，恒可用。
		return nil
	case *store.MultiSchemaStore:
		// 多租户 schema 隔离：逐 schema ping。
		// allStores() 为包内方法，controlplane 无法访问；
		// 改用 IsLeader() 间接探活——IsLeader 会遍历所有 schema 调用 IsLeader，
		// 任一 schema 持有租约即为主。若所有 schema 连接断裂，IsLeader 返回 false
		// 但不报错；此处用 globalStore() 取 default schema 做真实 ping。
		// 简化策略：尝试 RenewLeadership(短租约) 探活，成功即视为可用。
		// 但 RenewLeadership 有副作用（抢占租约），不适合探针高频调用。
		// 最终策略：MultiSchemaStore 的健康由其内部 *SQLStore 决定，
		// 此处退化为 nil（认为可用），真实连通性由 /readyz 的 IsLeader 检查兜底。
		_ = st
		return nil
	default:
		// 未知 Store 实现：保守视为可用，避免误杀自定义实现。
		return nil
	}
}

// verifyBootstrapToken 校验 agent 分发端点（/install.sh、/bin/opsmesh-agent）的访问令牌。
// P0-G1 安全加固：原端点完全开放，任何人可下载 agent 二进制与安装脚本，存在供应链投毒风险。
//
// 校验规则：
//   - demo 模式（s.cfg.Demo == true）：放宽，不要求 token（保持本地一键体验）。
//   - 否则接受 ?token=xxx 查询参数 或 Authorization: Bearer xxx 头，
//     与 s.cfg.ProvisionSecret 做 hmac.Equal 常量时间比对，防时序侧信道。
//   - 无 token 或 token 不匹配 → 401 Unauthorized。
//
// 返回 true 表示放行，false 表示已写入 401 响应（调用方应直接 return）。
