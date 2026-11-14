package controlplane

import (
	"context"
	"time"

	"google.golang.org/grpc"

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

	// WithBlock 已废弃 ⇒ 改用 dialGRPCBlocking（NewClient + Connect + 等到 READY，语义等价）。
	conn, err := dialGRPCBlocking(ctx, target)
	if err != nil {
		return nil, err
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
