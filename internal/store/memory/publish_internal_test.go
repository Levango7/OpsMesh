// publish_internal_test.go MemoryStore.publish 边界（自 internal/store/store_extra_test.go 迁入，TD-61）。
//
// 迁入理由：publish 是 MemoryStore 的私有方法（nil bus / bus 报错 / 正常发布三条路径），
// 拆包后父包不可见；failingBus/errPublishFail/busFunc 三个替身随用例一起迁移。
package memory

import (
	"context"
	"sync"
	"testing"

	"github.com/Levango7/OpsMesh/internal/events"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// errPublishFail 用于测试 publish 失败路径的自定义错误。
type errPublishFail struct{}

func (errPublishFail) Error() string { return "publish failed" }

// failingBus 测试用 Bus，返回错误以触发 publish 失败分支。
type failingBus struct{ called bool }

func (b *failingBus) Publish(ctx context.Context, e events.Event) error {
	b.called = true
	return errPublishFail{}
}

// TestMemoryStore_Publish_NilBus 验证 nil bus 时 publish 不 panic。
func TestMemoryStore_Publish_NilBus(t *testing.T) {
	m := NewMemoryStore()
	// bus 为 nil，publish 不应 panic
	m.publish(events.Event{Action: "test"})
}

// TestMemoryStore_Publish_BusError 验证 bus 返回错误时 publish 不 panic（仅日志）。
func TestMemoryStore_Publish_BusError(t *testing.T) {
	m := NewMemoryStore()
	bus := &failingBus{}
	m.WithBus(bus)
	// 触发一次 publish（Register 内部会 publish）
	m.Register(&proto.AgentInfo{Segment: "s1", TenantID: "t1"})
	if !bus.called {
		t.Fatal("failingBus.Publish 应被调用")
	}
}

// TestMemoryStore_Publish_Success 验证 bus 正常发布。
func TestMemoryStore_Publish_Success(t *testing.T) {
	m := NewMemoryStore()
	var mu sync.Mutex
	got := []events.Event{}
	m.WithBus(busFunc(func(ctx context.Context, e events.Event) error {
		mu.Lock()
		got = append(got, e)
		mu.Unlock()
		return nil
	}))
	m.Register(&proto.AgentInfo{Segment: "s1", TenantID: "t1"})
	mu.Lock()
	defer mu.Unlock()
	if len(got) == 0 {
		t.Fatal("应至少发布一个事件")
	}
	if got[0].Action != "register" {
		t.Fatalf("首个事件 action = %q, want register", got[0].Action)
	}
}

// busFunc 把函数转为 Bus 接口（测试用）。
type busFunc func(ctx context.Context, e events.Event) error

func (f busFunc) Publish(ctx context.Context, e events.Event) error { return f(ctx, e) }
