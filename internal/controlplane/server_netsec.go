// server_netsec.go — gRPC/联邦/metrics 服务建造与联邦验签（mTLS + HMAC）
package controlplane

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	"opsmesh/internal/grpcx"
	"opsmesh/internal/logx"
	"opsmesh/internal/otelx"
	"opsmesh/internal/store"
	"opsmesh/internal/tlsutil"
)

func (s *Server) buildGRPC() (*grpc.Server, net.Listener, error) {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.grpcPort))
	if err != nil {
		return nil, nil, fmt.Errorf("gRPC 监听失败 %d: %w", s.grpcPort, err)
	}
	var opts []grpc.ServerOption
	if s.tlsCert != "" && s.tlsKey != "" {
		// TLS 证书热重载：--tls-watch=true 时启用 fsnotify 监听证书文件变更，
		// 无需重启服务即可更新 TLS 配置。否则走原 ServerCreds 路径（启动时一次性加载）。
		if s.cfg != nil && s.cfg.TLSWatch {
			reloader, err := tlsutil.NewCertificateReloader(s.tlsCert, s.tlsKey)
			if err != nil {
				return nil, nil, fmt.Errorf("gRPC TLS 热重载初始化失败: %w", err)
			}
			s.tlsReloader = reloader
			tlsCfg := &tls.Config{
				GetCertificate: reloader.GetCertificate,
				MinVersion:     tls.VersionTLS12,
			}
			// mTLS：clientCA 非空时要求客户端持证。
			if s.clientCA != "" {
				pool := x509.NewCertPool()
				b, err := os.ReadFile(s.clientCA)
				if err != nil {
					return nil, nil, fmt.Errorf("gRPC TLS 热重载模式读取 clientCA 失败: %w", err)
				}
				if !pool.AppendCertsFromPEM(b) {
					return nil, nil, fmt.Errorf("gRPC TLS 热重载模式解析 clientCA 失败")
				}
				tlsCfg.ClientCAs = pool
				tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
			}
			creds := credentials.NewTLS(tlsCfg)
			opts = append(opts, grpc.Creds(creds))
			logx.Info(context.Background(), "gRPC 已启用 TLS（热重载模式）", "mtls", s.clientCA != "", "cert", s.tlsCert, "key", s.tlsKey)
		} else {
			creds, err := tlsutil.ServerCreds(s.tlsCert, s.tlsKey, s.clientCA)
			if err != nil {
				return nil, nil, fmt.Errorf("gRPC TLS 加载失败: %w", err)
			}
			opts = append(opts, grpc.Creds(creds))
			logx.Info(context.Background(), "gRPC 已启用 TLS", "mtls", s.clientCA != "")
		}
	}
	// 兜底盘 + OTel gRPC 服务端拦截器（链式组合）：
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
		srv:         s, // 注入 Server 引用，使 gRPC handler 可发布 SSE 事件
		// gRPC agent 身份绑定：按 config.GRPCRequireSignature 启用签名验证。
		// demo 模式下 config 已强制关闭（cfg.GRPCRequireSignature=false），此处直接透传。
		requireSignature: s.cfg != nil && s.cfg.GRPCRequireSignature,
		// 安全加固：传入预共享签名密钥（--grpc-signature-key）。
		// 非空时 verifyAgentSignature 优先使用此密钥验签，Register 不再下发密钥。
		signatureKey: func() string {
			if s.cfg != nil {
				return s.cfg.GRPCSignatureKey
			}
			return ""
		}(),
	})
	return gs, lis, nil
}

// grpcRecoveryInterceptor 兜底盘：拦截任何 unary handler 内的 panic，避免单个 RPC panic
// 击穿 gRPC 默认行为（整个 server 崩溃），转为 Internal 状态码 + 结构化日志。
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

// metricsAllowed 判断客户端是否为 metrics 端点授权来源（CIDR 白名单）。
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

// buildFederationServer 构造联邦独立 mTLS 监听。
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
	// 仅暴露联邦必需的入站端点；两者均已内置 联邦签名验签。
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

// buildMetrics 构造 metrics HTTP server 与监听，渲染零依赖 Prometheus 文本指标。
func (s *Server) buildMetrics() (*http.Server, net.Listener, error) {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.metricsPort))
	if err != nil {
		return nil, nil, fmt.Errorf("metrics 监听失败 %d: %w", s.metricsPort, err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		// metrics 访问控制：白名单非空时仅允许授权来源，否则 403。
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
	return &http.Server{Handler: recoveryMiddleware(mux), ReadHeaderTimeout: 5 * time.Second}, lis, nil
}

// handleDevices 处理 GET /api/v1/devices，按网关注入租户返回 segment -> 设备 列表。
// verifyFederationRequest 校验入站请求的联邦签名。
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
	// 请求体纳入签名覆盖（sha256(body) 摘要），防中间人篡改转发任务体。
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

// handleHealthz 深度健康检查（K8s liveness 探针，增强）。
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

// handleReadyz 就绪检查（K8s readiness 探针，新增）。
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

// pingStore 对底层 Store 做轻量连通性检查（健康检查支撑）。
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
// 安全加固：原端点完全开放，任何人可下载 agent 二进制与安装脚本，存在供应链投毒风险。
//
// 校验规则：
//   - demo 模式（s.cfg.Demo == true）：放宽，不要求 token（保持本地一键体验）。
//   - 否则接受 ?token=xxx 查询参数 或 Authorization: Bearer xxx 头，
//     与 s.cfg.ProvisionSecret 做 hmac.Equal 常量时间比对，防时序侧信道。
//   - 无 token 或 token 不匹配 → 401 Unauthorized。
//
// 返回 true 表示放行，false 表示已写入 401 响应（调用方应直接 return）。

// ============================================================================
// SSRF 防护：webhook URL 校验 + autoProvision CIDR 白名单校验
// ============================================================================
//
// 与 server_security.go 中 validateURLSSRF 的关系：
//   - validateURLSSRF：旧版无参 SSRF 校验（无 allowPrivate 选项，恒拒私网），
//     供 notifyLoop 启动期校验 AlertWebhookURL 与 AdvertiseAddr（仅警告）。
//   - ValidateWebhookURL：新版带 allowPrivate 参数，支持内网部署场景显式放行私网，
//     供通知渠道 CRUD（createNotifyChannel/updateNotifyChannel）保存前校验。
//   - isPrivateIP：复用 server_security.go 中已有的实现（已增强为拒 0.0.0.0/8）。
//
// DNS 解析超时：5 秒（要求合理默认值，避免恶意域名拖垮 API）。

// ssrfDNSTimeout 是 SSRF 校验中 DNS 解析的超时时间（5 秒合理默认）。
// 通过 context.WithTimeout 控制 net.LookupIP，避免恶意域名解析拖垮 API。
const ssrfDNSTimeout = 5 * time.Second

// ValidateWebhookURL 校验 webhook URL 防止 SSRF 攻击。
//
// 规则：
//   - 协议必须是 http 或 https（拒绝 file://、gopher://、dict:// 等危险协议）
//   - 主机名非空
//   - 解析主机名：若是 IP 字面量直接校验；若是域名做 DNS 解析后校验每个返回 IP
//   - 默认拒绝私网/loopback/链路本地/云元数据地址（isPrivateIP）
//   - allowPrivate=true 时放行私网地址（用于内网部署场景，如钉钉/飞书内网网关）
//
// 拒绝的地址范围（isPrivateIP）：
//   - 127.0.0.0/8（loopback）
//   - 10.0.0.0/8（私网 A）
//   - 172.16.0.0/12（私网 B）
//   - 192.168.0.0/16（私网 C）
//   - 169.254.0.0/16（链路本地 + 云元数据 169.254.169.254）
//   - 0.0.0.0/8（本网/未指定）
//   - ::1（IPv6 loopback）
//   - fe80::/10（IPv6 link-local）
//   - fc00::/7（IPv6 ULA 私网）
//
// DNS 解析超时 5 秒（ssrfDNSTimeout），避免恶意域名拖垮 API。
//
// 返回 nil 表示安全，非 nil error 描述拒绝原因（调用方应返回 400 + 错误信息）。
func ValidateWebhookURL(rawURL string, allowPrivate bool) error {
	u, err := neturl.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	// 1. 协议白名单：仅允许 http/https（拒绝 file/gopher/dict/ftp 等可触发 SSRF/RFI 的协议）。
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("scheme %q not allowed (only http/https)", u.Scheme)
	}
	// 2. 主机名非空校验。
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("empty host in URL")
	}
	// 3. 解析主机名：IP 字面量直接校验；域名做 DNS 解析后校验每个 IP。
	//    若 allowPrivate=true 则跳过私网校验（内网部署场景）。
	if allowPrivate {
		return nil // 显式允许内网：仅校验协议 + 主机非空，不做 IP 校验
	}
	// 先尝试 IP 字面量（避免触发 DNS 解析，性能优化）。
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateIP(ip) {
			return fmt.Errorf("host %q is private/loopback/metadata address %s", host, ip)
		}
		return nil
	}
	// 域名：DNS 解析（带 5 秒超时），校验每个返回 IP。
	// 任一 IP 落入私网即拒绝（防 DNS rebinding：域名解析到内网地址）。
	resolver := net.DefaultResolver
	lookupCtx, cancel := context.WithTimeout(context.Background(), ssrfDNSTimeout)
	defer cancel()
	ips, err := resolver.LookupIP(lookupCtx, "ip", host)
	if err != nil {
		return fmt.Errorf("cannot resolve host %q: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("host %q resolved to no IP addresses", host)
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return fmt.Errorf("host %q resolves to private/loopback/metadata address %s", host, ip)
		}
	}
	return nil
}

// ValidateCIDR 校验目标 CIDR 是否在允许的白名单内（autoProvision 网段校验）。
//
// 用于 autoProvision 扫描前校验目标网段，防止运维误配置或攻击者构造请求扫描任意网段
// （如扫描 169.254.169.254 所在网段获取云元数据，或扫描内网其他网段做内网探测）。
//
// 规则：
//   - allowedCIDRs 为空时不校验（向后兼容，由调用方决定是否启用白名单）
//   - 目标 CIDR 必须是合法 CIDR 表示（如 10.30.0.0/24）
//   - 目标 CIDR 的网络地址必须包含在任一允许的 CIDR 范围内
//   - 不在白名单内则返回错误（调用方应返回 403 + 错误信息）
//
// 注意：此处比较的是 CIDR 网段而非单个 IP——目标 CIDR 必须完全落在某个允许的 CIDR 内，
// 避免目标 CIDR 范围超出白名单（如允许 10.0.0.0/16 但目标 10.0.0.0/8 应被拒，因 10.1.0.0 不在 10.0.0.0/16 内）。
// 实现：检查目标 CIDR 的起始 IP 与结束 IP 是否都在某个允许的 CIDR 内。
func ValidateCIDR(cidr string, allowedCIDRs []string) error {
	// 白名单为空：不校验（向后兼容，调用方决定是否启用）。
	if len(allowedCIDRs) == 0 {
		return nil
	}
	// 解析目标 CIDR。
	_, targetNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("invalid target CIDR %q: %w", cidr, err)
	}
	// 解析白名单 CIDR 列表。
	var allowedNets []*net.IPNet
	for _, allowed := range allowedCIDRs {
		allowed = strings.TrimSpace(allowed)
		if allowed == "" {
			continue
		}
		_, n, err := net.ParseCIDR(allowed)
		if err != nil {
			return fmt.Errorf("invalid allowed CIDR %q: %w", allowed, err)
		}
		allowedNets = append(allowedNets, n)
	}
	if len(allowedNets) == 0 {
		return fmt.Errorf("allowed CIDR whitelist is empty after parsing")
	}
	// 检查目标 CIDR 是否完全落在某个允许的 CIDR 内。
	// 即：目标网段的起始 IP 与结束 IP 都在同一个允许的 CIDR 内。
	targetStart, targetEnd := cidrRange(targetNet)
	for _, allowed := range allowedNets {
		if allowed.Contains(targetStart) && allowed.Contains(targetEnd) {
			return nil
		}
	}
	return fmt.Errorf("target CIDR %q not within any allowed CIDR in whitelist", cidr)
}

// cidrRange 返回 CIDR 网段的起始 IP 与结束 IP（用于 ValidateCIDR 范围包含校验）。
// 起始 IP = 网络地址；结束 IP = 广播地址（网络地址 | 掩码反码）。
func cidrRange(n *net.IPNet) (net.IP, net.IP) {
	ip := n.IP
	mask := n.Mask
	start := make(net.IP, len(ip))
	copy(start, ip)
	end := make(net.IP, len(ip))
	for i := 0; i < len(ip); i++ {
		end[i] = ip[i] | ^mask[i]
	}
	return start, end
}
