package controlplane

import (
	"context"
	"fmt"
	"time"

	taskv1 "github.com/Levango7/OpsMesh/services/task-svc/api/proto/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TaskServiceClient 任务服务客户端接口（便于测试 mock）。
type TaskServiceClient interface {
	CreateTask(ctx context.Context, req *taskv1.CreateTaskRequest, opts ...grpc.CallOption) (*taskv1.Task, error)
	GetTask(ctx context.Context, req *taskv1.GetTaskRequest, opts ...grpc.CallOption) (*taskv1.Task, error)
	ListTasks(ctx context.Context, req *taskv1.ListTasksRequest, opts ...grpc.CallOption) (*taskv1.ListTasksResponse, error)
	ClaimTask(ctx context.Context, req *taskv1.ClaimTaskRequest, opts ...grpc.CallOption) (*taskv1.Task, error)
	ReportResult(ctx context.Context, req *taskv1.ReportResultRequest, opts ...grpc.CallOption) (*taskv1.TaskResult, error)
	CancelTask(ctx context.Context, req *taskv1.CancelTaskRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
	ApproveTask(ctx context.Context, req *taskv1.ApproveTaskRequest, opts ...grpc.CallOption) (*taskv1.Task, error)
	RejectTask(ctx context.Context, req *taskv1.RejectTaskRequest, opts ...grpc.CallOption) (*taskv1.Task, error)
	GetTaskStatus(ctx context.Context, req *taskv1.GetTaskStatusRequest, opts ...grpc.CallOption) (*taskv1.TaskStatusResponse, error)
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

	conn, err := grpc.DialContext(ctx, target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, err
	}

	client := taskv1.NewTaskServiceClient(conn)
	return &GRPCTaskServiceClient{
		client: client,
		conn:   conn,
	}, nil
}

// CreateTask 创建任务。
func (c *GRPCTaskServiceClient) CreateTask(ctx context.Context, req *taskv1.CreateTaskRequest, opts ...grpc.CallOption) (*taskv1.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.client.CreateTask(ctx, req, opts...)
}

// GetTask 获取任务详情。
func (c *GRPCTaskServiceClient) GetTask(ctx context.Context, req *taskv1.GetTaskRequest, opts ...grpc.CallOption) (*taskv1.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return c.client.GetTask(ctx, req, opts...)
}

// ListTasks 列出任务。
func (c *GRPCTaskServiceClient) ListTasks(ctx context.Context, req *taskv1.ListTasksRequest, opts ...grpc.CallOption) (*taskv1.ListTasksResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.client.ListTasks(ctx, req, opts...)
}

// ClaimTask 原子领取任务。
func (c *GRPCTaskServiceClient) ClaimTask(ctx context.Context, req *taskv1.ClaimTaskRequest, opts ...grpc.CallOption) (*taskv1.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.client.ClaimTask(ctx, req, opts...)
}

// ReportResult 上报执行结果。
func (c *GRPCTaskServiceClient) ReportResult(ctx context.Context, req *taskv1.ReportResultRequest, opts ...grpc.CallOption) (*taskv1.TaskResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.client.ReportResult(ctx, req, opts...)
}

// CancelTask 取消任务。
func (c *GRPCTaskServiceClient) CancelTask(ctx context.Context, req *taskv1.CancelTaskRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.client.CancelTask(ctx, req, opts...)
}

// ApproveTask 审批通过任务。
func (c *GRPCTaskServiceClient) ApproveTask(ctx context.Context, req *taskv1.ApproveTaskRequest, opts ...grpc.CallOption) (*taskv1.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.client.ApproveTask(ctx, req, opts...)
}

// RejectTask 审批拒绝任务。
func (c *GRPCTaskServiceClient) RejectTask(ctx context.Context, req *taskv1.RejectTaskRequest, opts ...grpc.CallOption) (*taskv1.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.client.RejectTask(ctx, req, opts...)
}

// GetTaskStatus 获取任务状态。
func (c *GRPCTaskServiceClient) GetTaskStatus(ctx context.Context, req *taskv1.GetTaskStatusRequest, opts ...grpc.CallOption) (*taskv1.TaskStatusResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return c.client.GetTaskStatus(ctx, req, opts...)
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
func (s *StubTaskServiceClient) CreateTask(ctx context.Context, req *taskv1.CreateTaskRequest, opts ...grpc.CallOption) (*taskv1.Task, error) {
	task := req.GetTask()
	if task == nil {
		task = &taskv1.Task{}
	}
	if task.TaskId == "" {
		task.TaskId = "task-" + fmt.Sprintf("%d", time.Now().UnixNano())
	}
	if task.Status == "" {
		task.Status = "pending"
	}
	s.tasks[task.TaskId] = task
	return task, nil
}

// GetTask 存根实现。
func (s *StubTaskServiceClient) GetTask(ctx context.Context, req *taskv1.GetTaskRequest, opts ...grpc.CallOption) (*taskv1.Task, error) {
	if task, ok := s.tasks[req.TaskId]; ok {
		return task, nil
	}
	return nil, fmt.Errorf("task not found: %s", req.TaskId)
}

// ListTasks 存根实现。
func (s *StubTaskServiceClient) ListTasks(ctx context.Context, req *taskv1.ListTasksRequest, opts ...grpc.CallOption) (*taskv1.ListTasksResponse, error) {
	return nil, nil
}

// ClaimTask 存根实现。
func (s *StubTaskServiceClient) ClaimTask(ctx context.Context, req *taskv1.ClaimTaskRequest, opts ...grpc.CallOption) (*taskv1.Task, error) {
	return nil, nil
}

// ReportResult 存根实现。
func (s *StubTaskServiceClient) ReportResult(ctx context.Context, req *taskv1.ReportResultRequest, opts ...grpc.CallOption) (*taskv1.TaskResult, error) {
	return nil, nil
}

// CancelTask 存根实现。
func (s *StubTaskServiceClient) CancelTask(ctx context.Context, req *taskv1.CancelTaskRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	return nil, nil
}

// ApproveTask 存根实现。
func (s *StubTaskServiceClient) ApproveTask(ctx context.Context, req *taskv1.ApproveTaskRequest, opts ...grpc.CallOption) (*taskv1.Task, error) {
	return nil, nil
}

// RejectTask 存根实现。
func (s *StubTaskServiceClient) RejectTask(ctx context.Context, req *taskv1.RejectTaskRequest, opts ...grpc.CallOption) (*taskv1.Task, error) {
	return nil, nil
}

// GetTaskStatus 存根实现。
func (s *StubTaskServiceClient) GetTaskStatus(ctx context.Context, req *taskv1.GetTaskStatusRequest, opts ...grpc.CallOption) (*taskv1.TaskStatusResponse, error) {
	return nil, nil
}

// Close 存根实现。
func (s *StubTaskServiceClient) Close() error {
	return nil
}

// ResultServiceClient 任务结果服务客户端接口（便于测试 mock）。
type ResultServiceClient interface {
	GetTaskResult(ctx context.Context, req *taskv1.GetTaskResultRequest, opts ...grpc.CallOption) (*taskv1.TaskResult, error)
	ListTaskResults(ctx context.Context, req *taskv1.ListTaskResultsRequest, opts ...grpc.CallOption) (*taskv1.ListTaskResultsResponse, error)
	GetTaskLogs(ctx context.Context, req *taskv1.GetTaskLogsRequest, opts ...grpc.CallOption) (*taskv1.TaskLogsResponse, error)
	Close() error
}

// GRPCResultServiceClient gRPC 实现的任务结果服务客户端。
type GRPCResultServiceClient struct {
	client taskv1.ResultServiceClient
	conn   *grpc.ClientConn
}

// NewGRPCResultServiceClient 创建 gRPC 任务结果服务客户端。
func NewGRPCResultServiceClient(ctx context.Context, target string) (*GRPCResultServiceClient, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, err
	}

	client := taskv1.NewResultServiceClient(conn)
	return &GRPCResultServiceClient{
		client: client,
		conn:   conn,
	}, nil
}

// GetTaskResult 获取任务执行结果。
func (c *GRPCResultServiceClient) GetTaskResult(ctx context.Context, req *taskv1.GetTaskResultRequest, opts ...grpc.CallOption) (*taskv1.TaskResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return c.client.GetTaskResult(ctx, req, opts...)
}

// ListTaskResults 列出任务结果。
func (c *GRPCResultServiceClient) ListTaskResults(ctx context.Context, req *taskv1.ListTaskResultsRequest, opts ...grpc.CallOption) (*taskv1.ListTaskResultsResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.client.ListTaskResults(ctx, req, opts...)
}

// GetTaskLogs 获取任务日志。
func (c *GRPCResultServiceClient) GetTaskLogs(ctx context.Context, req *taskv1.GetTaskLogsRequest, opts ...grpc.CallOption) (*taskv1.TaskLogsResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.client.GetTaskLogs(ctx, req, opts...)
}

// Close 关闭 gRPC 连接。
func (c *GRPCResultServiceClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// StubResultServiceClient 存根实现（用于测试/单副本）。
type StubResultServiceClient struct{}

// NewStubResultServiceClient 创建存根客户端。
func NewStubResultServiceClient() *StubResultServiceClient {
	return &StubResultServiceClient{}
}

// GetTaskResult 存根实现。
func (s *StubResultServiceClient) GetTaskResult(ctx context.Context, req *taskv1.GetTaskResultRequest, opts ...grpc.CallOption) (*taskv1.TaskResult, error) {
	return nil, nil
}

// ListTaskResults 存根实现。
func (s *StubResultServiceClient) ListTaskResults(ctx context.Context, req *taskv1.ListTaskResultsRequest, opts ...grpc.CallOption) (*taskv1.ListTaskResultsResponse, error) {
	return nil, nil
}

// GetTaskLogs 存根实现。
func (s *StubResultServiceClient) GetTaskLogs(ctx context.Context, req *taskv1.GetTaskLogsRequest, opts ...grpc.CallOption) (*taskv1.TaskLogsResponse, error) {
	return nil, nil
}

// Close 存根实现。
func (s *StubResultServiceClient) Close() error {
	return nil
}