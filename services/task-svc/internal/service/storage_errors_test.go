package service

import (
	"context"
	"errors"
	"testing"

	taskv1 "github.com/Levango7/OpsMesh/services/task-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/store"
)

// 模拟 INSERT 失败，确保错误不会被服务层转换为成功。
type failingTaskStore struct {
	*store.MemoryStore
}

func (s *failingTaskStore) CreateTask(_ *models.Task) (*models.Task, error) {
	return nil, errors.New("insert failed")
}

func (s *failingTaskStore) ListTasks(_, _, _ string, _ int) ([]*models.Task, error) {
	return nil, errors.New("query failed")
}

func TestListTasksStorageFailure(t *testing.T) {
	st := &failingTaskStore{MemoryStore: store.NewMemoryStore()}
	svc := NewService(st, st, st, st)
	got, err := svc.ListTasks(context.Background(), &taskv1.ListTasksRequest{TenantId: "tenant-1"})
	if err == nil || got != nil {
		t.Fatalf("查询失败应返回错误，而不是空列表成功：response=%v err=%v", got, err)
	}
}

func TestCreateTaskStorageFailureDoesNotEmitSuccess(t *testing.T) {
	st := &failingTaskStore{MemoryStore: store.NewMemoryStore()}
	svc := NewService(st, st, st, st)
	bus, sink, bridge := &captureBus{}, &captureAuditSink{}, &captureSSEBridge{}
	svc.SetEventBus(bus)
	svc.SetAuditSink(sink)
	svc.SetSSEBridge(bridge)
	got, err := svc.CreateTask(context.Background(), &taskv1.CreateTaskRequest{
		Task: &taskv1.Task{AgentId: "agent-1", TenantId: "tenant-1", Command: "echo test"},
	})
	if err == nil || got != nil {
		t.Errorf("存储失败应返回错误和空任务，实际 task=%v err=%v", got, err)
	}
	if len(bus.Actions()) != 0 || len(sink.Actions()) != 0 || bridge.Count() != 0 {
		t.Error("存储失败不应发成功事件、审计或 SSE")
	}
}
