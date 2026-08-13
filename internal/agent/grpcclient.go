package agent

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"opsmesh/internal/discovery"
	"opsmesh/internal/grpcx"
	"opsmesh/internal/otelx"
	"opsmesh/internal/proto"
	"opsmesh/internal/tlsutil"
)

// GRPCClient 封装到控制面 9090 的真实 gRPC 注册通道（JSON codec，U-05）。
// A3 多控制面 failover：持有候选地址列表，每次 RPC 依次尝试各控制面，
// 任一成功即返回（HA 真多副本前置：单控制面宕机不影响 agent 注册/心跳/拉任务）。
//
// B-4 连接复用：conns 按 target 地址缓存长连接，invoke 复用而非每次 Dial+Close。
// 连接仅在 Close() 或被标记 stale（Invoke 返回连接错误）时关闭并从 map 移除。
// gRPC 内置 WithConnectParams Backoff 提供指数退避重连，应用层 evictConn 兜底淘汰坏连接。
//
// M1-3 服务发现集成：可选持有 discovery.Balancer，invoke 优先通过 balancer 选择控制面实例，
// 连接失败时调用 Failover.MarkFailed 触发主→备切换。balancer 为 nil 时回退到 addrs failover（向后兼容）。
type GRPCClient struct {
	addrs    []string // 候选控制面地址（host:grpcPort），按序 failover
	creds    credentials.TransportCredentials
	grpcPort int
	// task 81 gRPC agent 身份绑定：agent 的 HMAC 签名密钥（由 Register 响应下发）。
	// 非空时，invoke 在每次请求的 gRPC metadata 中携带 agent-signature 与 agent-timestamp，
	// 控制面据此验证 agent 身份。空=未启用签名（demo 模式或控制面未下发 secret）。
	secret string
	// B-4 连接复用：按 target 地址缓存的长连接池。
	// mu 保护 conns 并发读写（invoke/heartbeat/pull/report/cancel 多 goroutine 并发调用）。
	mu    sync.Mutex
	conns map[string]*grpc.ClientConn
	// M1-3 服务发现：可选的负载均衡器，非 nil 时 invoke 优先通过 balancer 选择控制面实例。
	// balancer 为 nil 时回退到 addrs failover（向后兼容，不破坏现有单控制面行为）。
	balancer discovery.Balancer
}

// SetSecret task 81：设置 agent 的 HMAC 签名密钥（由 Register 响应下发）。
// 注册成功后由 agent.go 调用。线程安全：仅在注册成功后调用一次，后续 invoke 只读。
func (c *GRPCClient) SetSecret(secret string) {
	c.secret = secret
}

// grpcTarget 从控制面地址解析出 gRPC 拨号目标，规则：
//   - 带 scheme（http://host:port）：剥离 scheme 与 URL 中的端口，统一拼上
//     控制面实际 gRPC 端口（cfg.GRPCPort，约定 9090，便于非默认端口部署，P0-2）。
//   - 无 scheme 的 host:port：尊重显式端口（多控制面各自端口，A3 failover）。
//   - 纯 host：拼上全局 gRPC 端口（grpcPort<=0 用默认 9090）。
func grpcTarget(controlAddr string, grpcPort int) (string, error) {
	raw := controlAddr
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return "", fmt.Errorf("解析 control-addr 失败: %w", err)
		}
		// 去掉 URL 中的端口（含 IPv6 [::1]:port 形式，保留方括号），
		// 统一拼上控制面实际 gRPC 端口（cfg.GRPCPort）。
		host := u.Host
		if i := strings.LastIndex(u.Host, ":"); i != -1 {
			host = u.Host[:i]
		}
		if grpcPort <= 0 {
			grpcPort = 9090
		}
		return fmt.Sprintf("%s:%d", host, grpcPort), nil
	}
	if _, _, err := net.SplitHostPort(raw); err == nil {
		return raw, nil // 显式 host:port 原样尊重
	}
	if grpcPort <= 0 {
		grpcPort = 9090
	}
	return fmt.Sprintf("%s:%d", raw, grpcPort), nil
}

// NewGRPCClient 构造支持多地址 failover 的 gRPC 客户端。
// addrs 为候选控制面地址（host:grpcPort 或带 scheme 的 HTTP 地址），至少一个。
// tlsCert/tlsKey/tlsCA 为空时使用 insecure；非空时按 mTLS 配置拨号（P1-6）。
// B-4：连接按需建立并缓存复用（见 invoke/getConn），Close() 时统一关闭。
func NewGRPCClient(addrs []string, tlsCert, tlsKey, tlsCA string, grpcPort int) (*GRPCClient, error) {
	if len(addrs) == 0 {
		return nil, fmt.Errorf("至少需要一个控制面地址")
	}
	var creds credentials.TransportCredentials
	if tlsCert != "" || tlsCA != "" {
		c, err := tlsutil.ClientCreds(tlsCert, tlsKey, tlsCA)
		if err != nil {
			return nil, fmt.Errorf("加载客户端 TLS 凭证: %w", err)
		}
		creds = c
	} else {
		creds = insecure.NewCredentials()
	}
	return &GRPCClient{
		addrs:    addrs,
		creds:    creds,
		grpcPort: grpcPort,
		conns:    make(map[string]*grpc.ClientConn),
	}, nil
}

// SetBalancer M1-3 服务发现：设置负载均衡器。
// 设置后 invoke 优先通过 balancer 选择控制面实例，连接失败时触发主→备切换。
// 传 nil 清除 balancer，回退到 addrs failover（向后兼容）。
// 线程安全：仅在初始化阶段调用一次（agent.Run 启动前），invoke 只读 balancer 字段。
func (c *GRPCClient) SetBalancer(b discovery.Balancer) {
	c.balancer = b
}

// invoke 对每个候选地址尝试一次 RPC：短超时（5s）以便快速 failover 到下一个控制面。
// 全部失败则返回最后一个错误。
// B-4 连接复用：通过 getConn 复用已缓存的长连接，不再每次 Dial+Close。
// 连接错误时 evictConn 淘汰坏连接，下次 invoke 重新创建（应用层兜底，配合 gRPC 内置 Backoff 重连）。
//
// M1-3 服务发现：如果 balancer 非 nil，优先通过 balancer 选择控制面实例；
// 连接失败时调用 Failover.MarkFailed 触发主→备切换，最多尝试实例总数次。
// balancer 为 nil 时回退到 addrs failover（向后兼容）。
func (c *GRPCClient) invoke(ctx context.Context, method string, req, resp interface{}) error {
	// M1-3 服务发现路径：balancer 非 nil 时优先使用。
	if c.balancer != nil {
		return c.invokeWithBalancer(ctx, method, req, resp)
	}
	// 回退路径：addrs failover（向后兼容，不破坏现有单控制面行为）。
	var lastErr error
	for _, a := range c.addrs {
		target, err := grpcTarget(a, c.grpcPort)
		if err != nil {
			lastErr = err
			continue
		}
		ac, cancel := context.WithTimeout(ctx, 5*time.Second)
		conn, gerr := c.getConn(target)
		if gerr != nil {
			cancel()
			lastErr = gerr
			continue
		}
		ierr := conn.Invoke(ac, method, req, resp, grpc.ForceCodec(grpcx.JSONCodec))
		cancel()
		if ierr == nil {
			return nil
		}
		lastErr = ierr
		// 连接级错误：淘汰坏连接，下次 invoke 重新 Dial（指数退避由 gRPC Backoff 内置）。
		if isConnError(ierr) {
			c.evictConn(target)
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("无可用控制面地址")
	}
	return lastErr
}

// invokeWithBalancer M1-3 服务发现路径：通过 balancer 选择控制面实例。
//
// 逻辑：
//  1. 调用 balancer.Next() 获取当前实例（主）。
//  2. 解析实例地址为 gRPC target，建立连接并 Invoke。
//  3. 成功返回；失败时如果是连接级错误，淘汰坏连接并调用 Failover.MarkFailed 切换到备。
//  4. 最多尝试实例总数次（避免无限循环），全部失败返回最后一个错误。
//
// Failover 语义：MarkFailed 切换 current 到下一个实例；无备可用时重置到主（配合 gRPC Backoff 重连）。
// RoundRobin 语义：每次 Next 轮询到下一个实例（不显式 MarkFailed）。
func (c *GRPCClient) invokeWithBalancer(ctx context.Context, method string, req, resp interface{}) error {
	// 探测 balancer 实例总数，限定最大尝试次数（避免无限循环）。
	// Failover.Len() 返回实例数；RoundRobin 无 Len 方法时用默认 3 次（保守上限）。
	maxAttempts := 3
	if fl, ok := c.balancer.(interface{ Len() int }); ok {
		if n := fl.Len(); n > 0 {
			maxAttempts = n
		}
	}
	var lastErr error
	for i := 0; i < maxAttempts; i++ {
		svc, err := c.balancer.Next(ctx)
		if err != nil {
			lastErr = err
			break
		}
		// 构造控制面地址：Addr 已含端口时直接用，否则拼 Port。
		addr := svc.Addr
		if svc.Port > 0 && !strings.Contains(addr, ":") {
			addr = fmt.Sprintf("%s:%d", addr, svc.Port)
		}
		target, terr := grpcTarget(addr, c.grpcPort)
		if terr != nil {
			lastErr = terr
			continue
		}
		ac, cancel := context.WithTimeout(ctx, 5*time.Second)
		conn, gerr := c.getConn(target)
		if gerr != nil {
			cancel()
			lastErr = gerr
			c.markBalancerFailed()
			continue
		}
		ierr := conn.Invoke(ac, method, req, resp, grpc.ForceCodec(grpcx.JSONCodec))
		cancel()
		if ierr == nil {
			return nil
		}
		lastErr = ierr
		// 连接级错误：淘汰坏连接 + 触发 balancer 主→备切换。
		if isConnError(ierr) {
			c.evictConn(target)
			c.markBalancerFailed()
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("无可用控制面地址")
	}
	return lastErr
}

// markBalancerFailed M1-3 服务发现：通知 balancer 当前实例失败，触发主→备切换。
// 仅对 Failover 类型 balancer 生效（RoundRobin 无 MarkFailed 方法，每次 Next 自动轮询）。
func (c *GRPCClient) markBalancerFailed() {
	if mf, ok := c.balancer.(interface {
		MarkFailed() (discovery.Service, error)
	}); ok {
		_, _ = mf.MarkFailed()
	}
}

// getConn B-4 连接复用：从缓存取目标地址的长连接，不存在则 Dial 创建并缓存。
// DialContext 非阻塞（无 WithBlock），立即返回；实际连接在首次 Invoke 时惰性建立。
// 用 context.Background() 而非带超时的 ctx：Dial 不阻塞，超时无意义，连接建立由 Invoke 的 ctx 控制。
//
// M1-1 OTel：Dial 时附加 GRPCClientUnaryInterceptor，使每次 invoke 自动创建 client span
// 并注入 W3C trace context 到 metadata，控制面服务端拦截器提取后接续 trace。
func (c *GRPCClient) getConn(target string) (*grpc.ClientConn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if conn, ok := c.conns[target]; ok {
		return conn, nil
	}
	conn, err := grpc.DialContext(context.Background(), target,
		grpc.WithTransportCredentials(c.creds),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(grpcx.JSONCodec)),
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff: backoff.Config{
				BaseDelay:  500 * time.Millisecond,
				Multiplier: 1.6,
				MaxDelay:   5 * time.Second,
			},
		}),
		// M1-1 OTel gRPC 客户端拦截器：为每次 RPC 创建 client span 并注入 trace context。
		// 与 signContext（task 81 签名）兼容：两者操作不同 metadata key，互不冲突。
		grpc.WithChainUnaryInterceptor(otelx.GRPCClientUnaryInterceptor("opsmesh-agent")),
	)
	if err != nil {
		return nil, err
	}
	c.conns[target] = conn
	return conn, nil
}

// evictConn B-4 连接淘汰：关闭并从缓存移除指定地址的连接。
// 在 Invoke 返回连接级错误时调用，下次 invoke 会经 getConn 重新 Dial。
func (c *GRPCClient) evictConn(target string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if conn, ok := c.conns[target]; ok {
		conn.Close()
		delete(c.conns, target)
	}
}

// isConnError 判断错误是否为连接级错误（需淘汰重连）。
// Unavailable（连接断开/对端不可达）、Canceled（ctx 取消）、DeadlineExceeded（超时）
// 视为连接问题；非 gRPC status 错误（如本地网络栈错误）保守视为连接错误。
// 业务错误（如 InvalidArgument/NotFound/PermissionDenied）不淘汰连接。
func isConnError(err error) bool {
	if err == nil {
		return false
	}
	st, ok := status.FromError(err)
	if !ok {
		return true // 非 gRPC status 错误，保守视为连接错误
	}
	switch st.Code() {
	case codes.Unavailable, codes.Canceled, codes.DeadlineExceeded:
		return true
	}
	return false
}

// signContext task 81 gRPC agent 身份绑定：为请求 ctx 附加 HMAC 签名 metadata。
// 当 client 持有 secret 且 agentID 非空时，计算 agent-signature = HMAC-SHA256(secret, timestamp+agentID)
// 并附加 agent-signature / agent-timestamp 到 outgoing metadata。
// secret 为空（未启用签名）或 agentID 为空（无身份）时原样返回 ctx（向后兼容）。
// 控制面 verifyAgentSignature 据此验证 agent 身份，不再纯信任 agent 自报的 AgentID。
func (c *GRPCClient) signContext(ctx context.Context, agentID string) context.Context {
	if c.secret == "" || agentID == "" {
		return ctx // 未启用签名或无身份，原样返回（向后兼容 demo/未配置）
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(c.secret))
	mac.Write([]byte(ts + agentID))
	sig := hex.EncodeToString(mac.Sum(nil))
	return metadata.AppendToOutgoingContext(ctx,
		"agent-signature", sig,
		"agent-timestamp", ts,
	)
}

// Register 通过 gRPC Register 方法注册自身，返回控制面分配的 agentID 与配置。
func (c *GRPCClient) Register(ctx context.Context, info *proto.AgentInfo) (*grpcx.RegisterResp, error) {
	resp := &grpcx.RegisterResp{}
	err := c.invoke(ctx, "/opsmesh.v1.Registration/Register", info, resp)
	return resp, err
}

// Heartbeat 通过 gRPC Heartbeat 方法上报心跳。
func (c *GRPCClient) Heartbeat(ctx context.Context, req *grpcx.HeartbeatReq) error {
	resp := &grpcx.Empty{}
	ctx = c.signContext(ctx, req.AgentID) // task 81：附加 HMAC 签名
	return c.invoke(ctx, "/opsmesh.v1.Registration/Heartbeat", req, resp)
}

// PullTasks 通过 gRPC PullTasks 方法拉取本 agent 的待执行任务（内部为原子领取，P1-1）。
func (c *GRPCClient) PullTasks(ctx context.Context, agentID string) ([]proto.Task, error) {
	resp := &grpcx.PullTasksResp{}
	req := &grpcx.PullTasksReq{AgentID: agentID}
	ctx = c.signContext(ctx, agentID) // task 81：附加 HMAC 签名
	if err := c.invoke(ctx, "/opsmesh.v1.Registration/PullTasks", req, resp); err != nil {
		return nil, err
	}
	return resp.Tasks, nil
}

// ReportResult 通过 gRPC ReportResult 方法上报任务执行结果。
func (c *GRPCClient) ReportResult(ctx context.Context, res *proto.TaskResult) error {
	resp := &grpcx.Empty{}
	ctx = c.signContext(ctx, res.AgentID) // task 81：附加 HMAC 签名
	return c.invoke(ctx, "/opsmesh.v1.Registration/ReportResult", res, resp)
}

// CancelTask 通过 gRPC CancelTask 方法取消指定任务（F3）。
// B-1：CancelTask 参数为 taskID+tenantID（无 agentID），用 tenantID 作为签名身份。
// 控制面 verifyAgentSignature 会校验签名，确保取消请求来自授权 agent。
func (c *GRPCClient) CancelTask(ctx context.Context, taskID, tenantID string) error {
	resp := &grpcx.Empty{}
	req := &grpcx.CancelTaskReq{TaskID: taskID, TenantID: tenantID}
	ctx = c.signContext(ctx, tenantID) // B-1：附加 HMAC 签名（用 tenantID 作为签名身份）
	return c.invoke(ctx, "/opsmesh.v1.Registration/CancelTask", req, resp)
}

// PollCancels 通过 gRPC PollCancels 方法轮询本 agent 当前被取消的任务 ID 列表（F3 取消信号下发）。
func (c *GRPCClient) PollCancels(ctx context.Context, agentID string) ([]string, error) {
	resp := &grpcx.PollCancelsResp{}
	req := &grpcx.PollCancelsReq{AgentID: agentID}
	ctx = c.signContext(ctx, agentID) // task 81：附加 HMAC 签名
	if err := c.invoke(ctx, "/opsmesh.v1.Registration/PollCancels", req, resp); err != nil {
		return nil, err
	}
	return resp.CancelledTaskIDs, nil
}

// ReportLogs task 247：通过 gRPC ReportLogs 方法上报采集的日志批次到控制面。
// 控制面校验 HMAC 签名后按 agent 归属租户落库（行级隔离）。report.AgentID 为签名身份。
func (c *GRPCClient) ReportLogs(ctx context.Context, report *proto.LogReport) error {
	resp := &grpcx.Empty{}
	req := &grpcx.ReportLogsReq{Report: *report}
	ctx = c.signContext(ctx, report.AgentID) // task 81：附加 HMAC 签名
	return c.invoke(ctx, "/opsmesh.v1.Registration/ReportLogs", req, resp)
}

// Close B-4 连接复用：关闭所有缓存的长连接。agent shutdown 时由 agent.go 调用。
func (c *GRPCClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for target, conn := range c.conns {
		conn.Close()
		delete(c.conns, target)
	}
	return nil
}
