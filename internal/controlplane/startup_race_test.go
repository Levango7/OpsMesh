package controlplane

// startup_race_test.go — issue #121：启动期 gRPC 客户端顺序竞态守卫。
//
// 修法：task-svc 三个客户端在 NewServer 阶段预装存根（非 nil），真实连接延后到
// Start() 的 gRPC server 就绪后由 initTaskClients() 完成。本测试钉住两个不变式：
//   1. initTaskClients 在未配置 task-svc 地址时快速返回、保持存根（不阻塞、不 panic）；
//   2. NewServer 返回的 Server 三个客户端均非 nil（延迟初始化不会留下 nil 解引用）。

import (
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/config"
)

func TestInitTaskClientsEmptyAddrKeepsStub(t *testing.T) {
	s := &Server{
		cfg:                 &config.Config{TaskSvcGRPCAddr: ""},
		electorClient:       NewStubLeaderElectorClient(true),
		taskServiceClient:   NewStubTaskServiceClient(),
		resultServiceClient: NewStubResultServiceClient(),
	}
	done := make(chan struct{})
	go func() {
		s.initTaskClients()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("initTaskClients with empty addr must return promptly")
	}
	if _, ok := s.taskServiceClient.(*StubTaskServiceClient); !ok {
		t.Errorf("empty addr should keep stub, got %T", s.taskServiceClient)
	}
	if s.electorClient == nil || s.resultServiceClient == nil {
		t.Error("clients must stay non-nil")
	}
}

func TestNewServerClientsNonNil(t *testing.T) {
	cfg := &config.Config{
		HTTPPort: 0, GRPCPort: 0, MetricsPort: 0,
		TaskSvcGRPCAddr: "localhost:1", // 故意指向不可达地址：NewServer 不应因此阻塞或留 nil
		Store:           "memory",
	}
	start := time.Now()
	s, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	// 放宽阈值：CI 的 -race + coverage + 全包并发下 NewServer 可达 10s+；
	// 核心不变式是"不永久阻塞在 task-svc 拨号"，60s 足以抓死锁又不误报。
	if elapsed := time.Since(start); elapsed > 60*time.Second {
		t.Errorf("NewServer must not block on task-svc dial (took %v)", elapsed)
	}
	if s.electorClient == nil || s.taskServiceClient == nil || s.resultServiceClient == nil {
		t.Fatal("NewServer must pre-install non-nil stub clients")
	}
	if _, ok := s.taskServiceClient.(*StubTaskServiceClient); !ok {
		t.Errorf("NewServer should pre-install stub, got %T", s.taskServiceClient)
	}
}
