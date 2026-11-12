package controlplane

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
)

// dialGRPCBlocking 建立 gRPC 连接并**等待到 READY**（保持旧 `grpc.WithBlock` 的语义）。
//
// 为什么需要它：grpc.WithBlock 已废弃（NewClient 不支持该 DialOption，1.x 全程不再支持），
// 而**不能简单删掉** —— 那会把「创建时阻塞到连接就绪或 ctx 超时」变成「惰性连接、
// 首次 RPC 才建连」，客户端构造函数的语义就变了（NewServer 阶段不再验证 task-svc 可达）。
//
// 按 grpc 官方迁移路径改写：NewClient（立即返回、连接惰性建立）+ Connect() 主动触发
// + WaitForStateChange 等到 READY ⇒ 与 WithBlock **等价**。
func dialGRPCBlocking(ctx context.Context, target string) (*grpc.ClientConn, error) {
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	conn.Connect()
	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			return conn, nil
		}
		if state == connectivity.Shutdown {
			_ = conn.Close()
			return nil, fmt.Errorf("grpc client to %s closed while connecting", target)
		}
		if !conn.WaitForStateChange(ctx, state) {
			_ = conn.Close()
			return nil, fmt.Errorf("grpc connect to %s: %w", target, ctx.Err())
		}
	}
}
