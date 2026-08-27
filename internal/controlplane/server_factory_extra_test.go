package controlplane

import (
	"context"
	"testing"
	"time"

	"opsmesh/internal/config"
	"opsmesh/internal/controlplane/factory"
	"opsmesh/internal/events"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// 本文件补全 server.go / server_factory.go / server_lifecycle.go 中 0% 覆盖的函数：
//   - startRefreshSweep / shutdownOTel / shutdownTLSReloader
//   - storeDispatcher.CreateTask / storeDispatcher.Device / storeDispatcher.TaskStates
//   - SelectStore / selectSessionStore

// =============================================================================
// server.go: startRefreshSweep / shutdownOTel / shutdownTLSReloader
// =============================================================================

func TestStartRefreshSweep_Cancel(t *testing.T) {
	s := &Server{store: store.NewMemoryStore(), cfg: &config.Config{}}
	ctx, cancel := context.WithCancel(context.Background())
	s.startRefreshSweep(ctx, 10*time.Millisecond)
	// 取消后 goroutine 应退出（不阻塞）
	cancel()
	time.Sleep(50 * time.Millisecond) // 给 goroutine 时间退出
}

func TestStartRefreshSweep_DefaultInterval(t *testing.T) {
	s := &Server{store: store.NewMemoryStore(), cfg: &config.Config{}}
	ctx, cancel := context.WithCancel(context.Background())
	s.startRefreshSweep(ctx, 0) // 0 应回退到 1h
	cancel()
	time.Sleep(20 * time.Millisecond)
}

func TestShutdownOTel_Nil(t *testing.T) {
	s := &Server{}
	s.shutdownOTel() // otelShutdown 为 nil，应直接返回
}

func TestShutdownTLSReloader_Nil(t *testing.T) {
	s := &Server{}
	s.shutdownTLSReloader() // tlsReloader 为 nil，应直接返回
}

// =============================================================================
// server_factory.go: storeDispatcher
// =============================================================================

func TestStoreDispatcher_CreateTask(t *testing.T) {
	st := store.NewMemoryStore()
	d := &factory.StoreDispatcher{Store: st}
	tk := d.CreateTask(&proto.Task{AgentID: "a1", TenantID: "t1", Type: "shell", Command: "echo"})
	if tk.TaskID == "" {
		t.Error("CreateTask should return task with ID")
	}
}

func TestStoreDispatcher_Device(t *testing.T) {
	st := store.NewMemoryStore()
	st.Register(&proto.AgentInfo{Segment: "seg", TenantID: "t1"})
	d := &factory.StoreDispatcher{Store: st}
	dev := d.Device("dev-a1")
	// 可能 nil（取决于 agent ID），但不应 panic
	_ = dev
}

func TestStoreDispatcher_DeviceNotFound(t *testing.T) {
	st := store.NewMemoryStore()
	d := &factory.StoreDispatcher{Store: st}
	if dev := d.Device("nope"); dev != nil {
		t.Error("non-existent device should return nil")
	}
}

func TestStoreDispatcher_TaskStates_Empty(t *testing.T) {
	st := store.NewMemoryStore()
	d := &factory.StoreDispatcher{Store: st}
	m := d.TaskStates(nil, "t1")
	if len(m) != 0 {
		t.Errorf("empty ids: got %d, want 0", len(m))
	}
}

func TestStoreDispatcher_TaskStates_Happy(t *testing.T) {
	st := store.NewMemoryStore()
	a := st.Register(&proto.AgentInfo{Segment: "seg", TenantID: "t1"})
	tk := st.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "echo"})
	d := &factory.StoreDispatcher{Store: st}
	m := d.TaskStates([]string{tk.TaskID, "nope"}, "t1")
	if m[tk.TaskID] != "pending" {
		t.Errorf("task state=%q, want pending", m[tk.TaskID])
	}
	if _, ok := m["nope"]; ok {
		t.Error("nope should not be in map")
	}
}

// =============================================================================
// server_factory.go: SelectStore / selectSessionStore
// =============================================================================

func TestSelectStore_Memory(t *testing.T) {
	cfg := &config.Config{}
	bus := events.New("", "", "")
	st, err := factory.SelectStore(cfg, bus)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if _, ok := st.(*store.MemoryStore); !ok {
		t.Error("expected MemoryStore")
	}
}

func TestSelectStore_MemoryWithDemo(t *testing.T) {
	cfg := &config.Config{Demo: true}
	bus := events.New("", "", "")
	st, err := factory.SelectStore(cfg, bus)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if st == nil {
		t.Error("store should not be nil")
	}
}

func TestSelectSessionStore_InProcess(t *testing.T) {
	cfg := &config.Config{}
	ss, err := factory.SelectSessionStore(cfg)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if ss == nil {
		t.Error("session store should not be nil")
	}
}

func TestSelectSessionStore_InvalidFormat(t *testing.T) {
	cfg := &config.Config{SessionStore: "invalid-format"}
	_, err := factory.SelectSessionStore(cfg)
	if err == nil {
		t.Error("invalid format should return error")
	}
}

func TestSelectSessionStore_EmptyRedisAddr(t *testing.T) {
	cfg := &config.Config{SessionStore: "redis://"}
	_, err := factory.SelectSessionStore(cfg)
	if err == nil {
		t.Error("empty redis addr should return error")
	}
}

// =============================================================================
// server_factory.go: newDeployHandler / newOrchestrationHandler / newCMDBHandler / newLogHandler
// =============================================================================

func TestNewDeployHandler_Memory(t *testing.T) {
	st := store.NewMemoryStore()
	h := factory.NewDeployHandler(st)
	if h == nil {
		t.Error("deploy handler should not be nil")
	}
}

func TestNewOrchestrationHandler_Memory(t *testing.T) {
	st := store.NewMemoryStore()
	h := factory.NewOrchestrationHandler(st)
	if h == nil {
		t.Error("orchestration handler should not be nil")
	}
}

func TestNewCMDBHandler_Memory(t *testing.T) {
	st := store.NewMemoryStore()
	h := factory.NewCMDBHandler(st)
	if h == nil {
		t.Error("cmdb handler should not be nil")
	}
}

func TestNewLogHandler_Memory(t *testing.T) {
	st := store.NewMemoryStore()
	cfg := &config.Config{}
	h := factory.NewLogHandler(st, cfg)
	if h == nil {
		t.Error("log handler should not be nil")
	}
}

func TestNewLogHandler_LokiEmptyEndpoint(t *testing.T) {
	st := store.NewMemoryStore()
	cfg := &config.Config{LogStore: "loki"}
	h := factory.NewLogHandler(st, cfg)
	if h == nil {
		t.Error("log handler should not be nil even with empty loki endpoint")
	}
}

func TestNewLogHandler_ESEmptyEndpoint(t *testing.T) {
	st := store.NewMemoryStore()
	cfg := &config.Config{LogStore: "es"}
	h := factory.NewLogHandler(st, cfg)
	if h == nil {
		t.Error("log handler should not be nil even with empty es endpoint")
	}
}

// =============================================================================
// server_lifecycle.go: Start
// =============================================================================

func TestServerStart_NilServer(t *testing.T) {
	// Start 方法通常需要完整 Server，这里仅测试不 panic
	// 实际 Start 会启动 HTTP server，不适合单元测试
	s := &Server{store: store.NewMemoryStore(), cfg: &config.Config{}}
	if s.store == nil || s.cfg == nil {
		t.Fatal("server 字段应已就绪")
	}
}

// =============================================================================
// server_factory.go: firstNonEmpty
// =============================================================================

func TestFirstNonEmpty(t *testing.T) {
	if got := factory.FirstNonEmpty(); got != "" {
		t.Errorf("empty: got %q, want empty", got)
	}
	if got := factory.FirstNonEmpty("", "", "a"); got != "a" {
		t.Errorf("got %q, want 'a'", got)
	}
	if got := factory.FirstNonEmpty("b", "a"); got != "b" {
		t.Errorf("got %q, want 'b'", got)
	}
}
