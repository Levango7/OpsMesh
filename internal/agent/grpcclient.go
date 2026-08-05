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
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"opsmesh/internal/grpcx"
	"opsmesh/internal/proto"
	"opsmesh/internal/tlsutil"
)

// GRPCClient 封装到控制面 9090 的真实 gRPC 注册通道（JSON codec，U-05）。
// A3 多控制面 failover：持有候选地址列表，每次 RPC 依次尝试各控制面，
// 任一成功即返回（HA 真多副本前置：单控制面宕机不影响 agent 注册/心跳/拉任务）。
type GRPCClient struct {
	addrs   []string // 候选控制面地址（host:grpcPort），按序 failover
	creds   credentials.TransportCredentials
	grpcPort int
	// task 81 gRPC agent 身份绑定：agent 的 HMAC 签名密钥（由 Register 响应下发）。
	// 非空时，invoke 在每次请求的 gRPC metadata 中携带 agent-signature 与 agent-timestamp，
	// 控制面据此验证 agent 身份。空=未启用签名（demo 模式或控制面未下发 secret）。
	secret string
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
// 不持久持有长连接（无 WithBlock）：每次 RPC 按需拨号并快速 failover，
// 对注册/心跳/拉任务的低频调用开销可忽略，且天然适配控制面列表变化。
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
	return &GRPCClient{addrs: addrs, creds: creds, grpcPort: grpcPort}, nil
}

// invoke 对每个候选地址尝试一次 RPC：短超时（5s）以便快速 failover 到下一个控制面。
// 全部失败则返回最后一个错误。
func (c *GRPCClient) invoke(ctx context.Context, method string, req, resp interface{}) error {
	var lastErr error
	for _, a := range c.addrs {
		target, err := grpcTarget(a, c.grpcPort)
		if err != nil {
			lastErr = err
			continue
		}
		ac, cancel := context.WithTimeout(ctx, 5*time.Second)
		conn, derr := grpc.DialContext(ac, target,
			grpc.WithTransportCredentials(c.creds),
			grpc.WithDefaultCallOptions(grpc.ForceCodec(grpcx.JSONCodec)),
			grpc.WithConnectParams(grpc.ConnectParams{
				Backoff: backoff.Config{
					BaseDelay:  500 * time.Millisecond,
					Multiplier: 1.6,
					MaxDelay:   5 * time.Second,
				},
			}),
		)
		if derr != nil {
			cancel()
			lastErr = derr
			continue
		}
		ierr := conn.Invoke(ac, method, req, resp, grpc.ForceCodec(grpcx.JSONCodec))
		conn.Close()
		cancel()
		if ierr == nil {
			return nil
		}
		lastErr = ierr
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("无可用控制面地址")
	}
	return lastErr
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
func (c *GRPCClient) CancelTask(ctx context.Context, taskID, tenantID string) error {
	resp := &grpcx.Empty{}
	req := &grpcx.CancelTaskReq{TaskID: taskID, TenantID: tenantID}
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

// Close 当前客户端无长连接可关（按需拨号模型），保留以对齐旧调用点。
func (c *GRPCClient) Close() error {
	return nil
}
