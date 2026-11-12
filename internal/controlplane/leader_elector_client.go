package controlplane

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"

	taskv1 "github.com/Levango7/OpsMesh/services/task-svc/api/proto/v1"
)

// LeaderElectorClient 选主客户端接口（便于测试 mock）。
type LeaderElectorClient interface {
	IsLeader(ctx context.Context) (bool, string, error)
	Close() error
}

// GRPCLeaderElectorClient gRPC 实现的选主客户端。
type GRPCLeaderElectorClient struct {
	client taskv1.LeaderElectorServiceClient
	conn   *grpc.ClientConn
}

// NewGRPCLeaderElectorClient 创建 gRPC 选主客户端。
func NewGRPCLeaderElectorClient(ctx context.Context, target string) (*GRPCLeaderElectorClient, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// grpc.WithBlock 已废弃（NewClient 不支持该 DialOption，1.x 全程不再支持）。
	//
	// 按 grpc 官方迁移路径：**NewClient（立即返回、连接惰性建立）+ Connect() 主动触发 +
	// WaitForStateChange 等到 READY** —— 保持原「阻塞到连接就绪或 ctx 超时」的**等价语义**
	// （不是简单删掉 WithBlock：那会把「启动即验证连通性」变成「首次 RPC 时才连」，
	// 选主客户端的创建语义会变）。
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	conn.Connect()
	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			break
		}
		if state == connectivity.Shutdown {
			_ = conn.Close()
			return nil, fmt.Errorf("leader elector client to %s closed while connecting", target)
		}
		if !conn.WaitForStateChange(ctx, state) {
			_ = conn.Close()
			return nil, fmt.Errorf("leader elector connect to %s: %w", target, ctx.Err())
		}
	}

	client := taskv1.NewLeaderElectorServiceClient(conn)
	return &GRPCLeaderElectorClient{
		client: client,
		conn:   conn,
	}, nil
}

// IsLeader 检查当前实例是否为 leader。
func (c *GRPCLeaderElectorClient) IsLeader(ctx context.Context) (bool, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	resp, err := c.client.IsLeader(ctx, &taskv1.IsLeaderRequest{})
	if err != nil {
		return false, "", err
	}
	return resp.IsLeader, resp.LeaderInstanceId, nil
}

// Close 关闭 gRPC 连接。
func (c *GRPCLeaderElectorClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// StubLeaderElectorClient 存根实现（用于测试/单副本）。
type StubLeaderElectorClient struct {
	isLeader bool
}

// NewStubLeaderElectorClient 创建存根客户端。
func NewStubLeaderElectorClient(isLeader bool) *StubLeaderElectorClient {
	return &StubLeaderElectorClient{isLeader: isLeader}
}

// IsLeader 存根实现。
func (s *StubLeaderElectorClient) IsLeader(ctx context.Context) (bool, string, error) {
	return s.isLeader, "", nil
}

// Close 存根实现。
func (s *StubLeaderElectorClient) Close() error {
	return nil
}
