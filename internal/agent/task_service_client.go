package agent

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"

	taskv1 "github.com/Levango7/OpsMesh/services/task-svc/api/proto/v1"
)

// TaskServiceClient 任务服务 gRPC 客户端接口（便于测试 mock）。
type TaskServiceClient interface {
	CreateTask(ctx context.Context, req *taskv1.CreateTaskRequest) (*taskv1.Task, error)
	GetTask(ctx context.Context, req *taskv1.GetTaskRequest) (*taskv1.Task, error)
	ListTasks(ctx context.Context, req *taskv1.ListTasksRequest) (*taskv1.ListTasksResponse, error)
	ClaimTask(ctx context.Context, req *taskv1.ClaimTaskRequest) (*taskv1.Task, error)
	ReportResult(ctx context.Context, req *taskv1.ReportResultRequest) (*taskv1.TaskResult, error)
	CancelTask(ctx context.Context, req *taskv1.CancelTaskRequest) error
	GetTaskStatus(ctx context.Context, req *taskv1.GetTaskStatusRequest) (*taskv1.TaskStatusResponse, error)
	Close() error
}

// GRPCTaskServiceClient gRPC 实现的任务服务客户端。
type GRPCTaskServiceClient struct {
	client taskv1.TaskServiceClient
	conn   *grpc.ClientConn
}

// NewGRPCTaskServiceClient 创建 gRPC 任务服务客户端。
func NewGRPCTaskServiceClient(ctx context.Context, target string) (*GRPCTaskServiceClient, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

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
		if !conn.WaitForStateChange(ctx, state) {
			_ = conn.Close()
			return nil, fmt.Errorf("grpc connect to %s: %w", target, ctx.Err())
		}
	}

	client := taskv1.NewTaskServiceClient(conn)
	return &GRPCTaskServiceClient{
		client: client,
		conn:   conn,
	}, nil
}

// CreateTask 创建任务。
func (c *GRPCTaskServiceClient) CreateTask(ctx context.Context, req *taskv1.CreateTaskRequest) (*taskv1.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.client.CreateTask(ctx, req)
}

// GetTask 获取任务详情。
func (c *GRPCTaskServiceClient) GetTask(ctx context.Context, req *taskv1.GetTaskRequest) (*taskv1.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return c.client.GetTask(ctx, req)
}

// ListTasks 列出任务。
func (c *GRPCTaskServiceClient) ListTasks(ctx context.Context, req *taskv1.ListTasksRequest) (*taskv1.ListTasksResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.client.ListTasks(ctx, req)
}

// ClaimTask 原子领取任务。
func (c *GRPCTaskServiceClient) ClaimTask(ctx context.Context, req *taskv1.ClaimTaskRequest) (*taskv1.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.client.ClaimTask(ctx, req)
}

// ReportResult 上报执行结果。
func (c *GRPCTaskServiceClient) ReportResult(ctx context.Context, req *taskv1.ReportResultRequest) (*taskv1.TaskResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.client.ReportResult(ctx, req)
}

// CancelTask 取消任务。
func (c *GRPCTaskServiceClient) CancelTask(ctx context.Context, req *taskv1.CancelTaskRequest) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err := c.client.CancelTask(ctx, req)
	return err
}

// GetTaskStatus 获取任务状态。
func (c *GRPCTaskServiceClient) GetTaskStatus(ctx context.Context, req *taskv1.GetTaskStatusRequest) (*taskv1.TaskStatusResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return c.client.GetTaskStatus(ctx, req)
}

// Close 关闭 gRPC 连接。
func (c *GRPCTaskServiceClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// StubTaskServiceClient 存根实现（用于测试/单副本）。
type StubTaskServiceClient struct {
	tasks map[string]*taskv1.Task
}

// NewStubTaskServiceClient 创建存根客户端。
func NewStubTaskServiceClient() *StubTaskServiceClient {
	return &StubTaskServiceClient{
		tasks: make(map[string]*taskv1.Task),
	}
}

// AddTask 添加任务到存根（用于测试预设数据）。
func (s *StubTaskServiceClient) AddTask(task *taskv1.Task) {
	s.tasks[task.TaskId] = task
}

// CreateTask 存根实现。
func (s *StubTaskServiceClient) CreateTask(ctx context.Context, req *taskv1.CreateTaskRequest) (*taskv1.Task, error) {
	return nil, nil
}

// GetTask 存根实现。
func (s *StubTaskServiceClient) GetTask(ctx context.Context, req *taskv1.GetTaskRequest) (*taskv1.Task, error) {
	return nil, nil
}

// ListTasks 存根实现。
func (s *StubTaskServiceClient) ListTasks(ctx context.Context, req *taskv1.ListTasksRequest) (*taskv1.ListTasksResponse, error) {
	return nil, nil
}

// ClaimTask 存根实现。
func (s *StubTaskServiceClient) ClaimTask(ctx context.Context, req *taskv1.ClaimTaskRequest) (*taskv1.Task, error) {
	// 返回一个测试用的任务
	return &taskv1.Task{
		TaskId:     "test-task-id",
		AgentId:    "test-agent",
		TenantId:   "test-tenant",
		Type:       "shell",
		Command:    "echo test",
		Content:    "",
		Path:       "",
		Status:     "pending",
		ClaimedBy:  "test-agent",
		ClaimedAt:  timestamppb.New(time.Now()),
		CreatedAt:  timestamppb.New(time.Now()),
		RetryCount: 0,
		DeadLetter: false,
		ClaimEpoch: 1,
	}, nil
}

// ReportResult 存根实现。
func (s *StubTaskServiceClient) ReportResult(ctx context.Context, req *taskv1.ReportResultRequest) (*taskv1.TaskResult, error) {
	return nil, nil
}

// CancelTask 存根实现。
func (s *StubTaskServiceClient) CancelTask(ctx context.Context, req *taskv1.CancelTaskRequest) error {
	return nil
}

// GetTaskStatus 存根实现。
func (s *StubTaskServiceClient) GetTaskStatus(ctx context.Context, req *taskv1.GetTaskStatusRequest) (*taskv1.TaskStatusResponse, error) {
	return nil, nil
}

// Close 存根实现。
func (s *StubTaskServiceClient) Close() error {
	return nil
}
