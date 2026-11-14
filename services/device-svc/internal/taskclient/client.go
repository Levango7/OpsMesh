// Package taskclient 提供 device-svc → task-svc 的任务下发 gRPC 客户端。
//
// TD-60 device 域下沉：netsec 端点的 shell 脚本需要落到 agent 上执行，
// 而任务域已归 task-svc 管辖——本包把 HTTP 层的 TaskDispatcher 接口
// 桥接到 task-svc TaskService.CreateTask。
package taskclient

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"

	taskv1 "github.com/Levango7/OpsMesh/services/task-svc/api/proto/v1"
)

// Client 是 task-svc TaskService 的最小客户端（仅 CreateTask）。
type Client struct {
	client taskv1.TaskServiceClient
	conn   *grpc.ClientConn
}

// New 建立到 task-svc gRPC 端点的连接。
// target 形如 127.0.0.1:9090；5s 内连不通即返回错误（调用方决定降级）。
func New(ctx context.Context, target string) (*Client, error) {
	dctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// 与 internal/agent 同款迁移：grpc.DialContext + grpc.WithBlock 已废弃（SA1019）。
	// NewClient + Connect + WaitForStateChange 等 READY，等价于「拨号即阻塞到就绪或 ctx 超时」。
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
			return nil, fmt.Errorf("grpc client to %s closed while connecting", target)
		}
		if !conn.WaitForStateChange(dctx, state) {
			_ = conn.Close()
			return nil, fmt.Errorf("grpc connect to %s: %w", target, dctx.Err())
		}
	}
	return &Client{client: taskv1.NewTaskServiceClient(conn), conn: conn}, nil
}

// DispatchTask 下发 shell 脚本任务到指定设备（agent_id=deviceID）。
// 实现 http.TaskDispatcher 接口。
func (c *Client) DispatchTask(ctx context.Context, deviceID, script, taskType string) (string, error) {
	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := c.client.CreateTask(dctx, &taskv1.CreateTaskRequest{
		Task: &taskv1.Task{
			AgentId: deviceID,
			Type:    "shell",
			Command: script,
		},
	})
	if err != nil {
		return "", err
	}
	return resp.GetTaskId(), nil
}

// Close 关闭 gRPC 连接。
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
