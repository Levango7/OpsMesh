package agent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/proto"

	taskv1 "github.com/Levango7/OpsMesh/services/task-svc/api/proto/v1"
)

// countingStubTaskClient 在存根客户端上统计 ClaimTask 调用次数（验证空闲时不空转）。
type countingStubTaskClient struct {
	*StubTaskServiceClient
	claims atomic.Int64
}

func (c *countingStubTaskClient) ClaimTask(ctx context.Context, req *taskv1.ClaimTaskRequest) (*taskv1.Task, error) {
	c.claims.Add(1)
	return c.StubTaskServiceClient.ClaimTask(ctx, req)
}

// TestStubTaskServiceClient_ClaimTaskIdleReturnsEmpty：
// 无预置任务时必须返回 (nil, nil) 表示“无工作”，不得返回合成任务。
func TestStubTaskServiceClient_ClaimTaskIdleReturnsEmpty(t *testing.T) {
	s := NewStubTaskServiceClient()
	task, err := s.ClaimTask(context.Background(), &taskv1.ClaimTaskRequest{AgentId: "a1"})
	if err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}
	if task != nil {
		t.Fatalf("空存根应返回 nil 任务，得到 %q", task.GetTaskId())
	}

	s.AddTask(&taskv1.Task{TaskId: "t-1", Command: "echo hi"})
	task, err = s.ClaimTask(context.Background(), &taskv1.ClaimTaskRequest{AgentId: "a1"})
	if err != nil || task.GetTaskId() != "t-1" {
		t.Fatalf("预置任务应被领取一次：task=%v err=%v", task, err)
	}
	task, err = s.ClaimTask(context.Background(), &taskv1.ClaimTaskRequest{AgentId: "a1"})
	if err != nil || task != nil {
		t.Fatalf("任务领取后应回到空闲：task=%v err=%v", task, err)
	}
}

// TestAgentClaimTask_EmptyResponseIsIdle：空响应/空 taskID 必须映射为 (nil, nil)，
// 不能把零值任务投给 worker。
func TestAgentClaimTask_EmptyResponseIsIdle(t *testing.T) {
	a := &Agent{agentID: "a1", taskServiceClient: NewStubTaskServiceClient()}
	task, err := a.claimTask(context.Background())
	if err != nil || task != nil {
		t.Fatalf("空存根 claimTask 应返回 (nil,nil)，得到 task=%v err=%v", task, err)
	}

	a.taskServiceClient = &emptyIDTaskClient{StubTaskServiceClient: NewStubTaskServiceClient()}
	task, err = a.claimTask(context.Background())
	if err != nil || task != nil {
		t.Fatalf("空 taskID 应视为空闲，得到 task=%v err=%v", task, err)
	}
}

// emptyIDTaskClient 模拟 task-svc 返回非 nil 但无 taskID 的响应。
type emptyIDTaskClient struct {
	*StubTaskServiceClient
}

func (emptyIDTaskClient) ClaimTask(context.Context, *taskv1.ClaimTaskRequest) (*taskv1.Task, error) {
	return &taskv1.Task{}, nil
}

// TestDispatchLoop_StubIdleNoSpin：存根客户端下 dispatchLoop 只按 ticker 周期领取，
// 不执行任何任务，且短窗口内 ClaimTask 次数有界（回归 D1 的无限空转）。
func TestDispatchLoop_StubIdleNoSpin(t *testing.T) {
	client := &countingStubTaskClient{StubTaskServiceClient: NewStubTaskServiceClient()}
	a := &Agent{
		agentID:           "a1",
		cfg:               &config.Config{TaskDispatchInterval: 50 * time.Millisecond},
		taskCh:            make(chan proto.Task, 1),
		taskServiceClient: client,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		a.dispatchLoop(ctx)
		close(done)
	}()

	time.Sleep(600 * time.Millisecond)
	cancel()
	<-done

	if got := len(a.taskCh); got != 0 {
		t.Fatalf("空闲存根不应入队任何任务，队列长度 = %d", got)
	}
	claims := client.claims.Load()
	if claims == 0 {
		t.Fatal("dispatchLoop 未执行任何 ClaimTask，ticker 可能未生效")
	}
	// 600ms / 50ms ≈ 12 次；给 CI 抖动留足余量，但必须远低于空转量级。
	if claims > 40 {
		t.Fatalf("ClaimTask 调用次数异常（疑似空转）：%d 次 / 600ms", claims)
	}
}
