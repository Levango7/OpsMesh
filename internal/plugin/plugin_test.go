package plugin

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

// plugin_test.go 插件框架测试：注册/钩子触发/生命周期/并发。
//
// 覆盖场景：
//  1. 插件注册成功 + Init 被调用 + GetPlugin/AllPlugins 查询。
//  2. 重复注册同名插件失败。
//  3. Init 失败回滚（不残留半注册状态）。
//  4. 反注册 + Close 被调用。
//  5. 钩子注册 + 触发 + 多 handler 顺序执行。
//  6. handler 返回 error 短路后续 handler。
//  7. nil handler 注册失败。
//  8. FireHook 无 handler 放行。
//  9. Close 幂等。
// 10. 并发：多 goroutine 同时 Register/FireHook/Unregister 不竞态。

// fakePlugin 测试用插件桩。
type fakePlugin struct {
	name     string
	version  string
	initCnt  int32
	closeCnt int32
	initErr  error // 注入 Init 错误
	closeErr error // 注入 Close 错误
	closeMu  sync.Mutex
}

func (p *fakePlugin) Name() string    { return p.name }
func (p *fakePlugin) Version() string { return p.version }
func (p *fakePlugin) Init(_ any) error {
	atomic.AddInt32(&p.initCnt, 1)
	return p.initErr
}
func (p *fakePlugin) Close() error {
	p.closeMu.Lock()
	defer p.closeMu.Unlock()
	atomic.AddInt32(&p.closeCnt, 1)
	return p.closeErr
}

func TestRegisterAndGet(t *testing.T) {
	mgr := NewManager()
	p := &fakePlugin{name: "a", version: "1.0.0"}
	if err := mgr.Register(p, nil); err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	if atomic.LoadInt32(&p.initCnt) != 1 {
		t.Errorf("Init 应被调用 1 次，实际 %d", p.initCnt)
	}
	if got := mgr.GetPlugin("a"); got != p {
		t.Errorf("GetPlugin 返回不符")
	}
	all := mgr.AllPlugins()
	if len(all) != 1 || all[0].Name() != "a" {
		t.Errorf("AllPlugins 应仅含 a，实际 %v", all)
	}
}

func TestRegisterDuplicate(t *testing.T) {
	mgr := NewManager()
	p1 := &fakePlugin{name: "dup", version: "1.0.0"}
	p2 := &fakePlugin{name: "dup", version: "2.0.0"}
	if err := mgr.Register(p1, nil); err != nil {
		t.Fatalf("首次注册失败: %v", err)
	}
	if err := mgr.Register(p2, nil); err == nil {
		t.Fatal("重复注册应失败")
	}
	if mgr.GetPlugin("dup") != p1 {
		t.Error("重复注册失败后应保留原插件")
	}
}

func TestRegisterInitErrorRollback(t *testing.T) {
	mgr := NewManager()
	p := &fakePlugin{name: "fail", version: "1.0.0", initErr: errors.New("init boom")}
	if err := mgr.Register(p, nil); err == nil {
		t.Fatal("Init 失败应返回 error")
	}
	if got := mgr.GetPlugin("fail"); got != nil {
		t.Errorf("Init 失败应回滚，GetPlugin 应返回 nil，实际 %v", got)
	}
}

func TestUnregister(t *testing.T) {
	mgr := NewManager()
	p := &fakePlugin{name: "rm", version: "1.0.0"}
	if err := mgr.Register(p, nil); err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	if err := mgr.Unregister("rm"); err != nil {
		t.Fatalf("反注册失败: %v", err)
	}
	if atomic.LoadInt32(&p.closeCnt) != 1 {
		t.Errorf("Close 应被调用 1 次，实际 %d", p.closeCnt)
	}
	if got := mgr.GetPlugin("rm"); got != nil {
		t.Error("反注册后 GetPlugin 应返回 nil")
	}
	if err := mgr.Unregister("rm"); err == nil {
		t.Error("反注册不存在的插件应返回 error")
	}
}

func TestHookFire(t *testing.T) {
	mgr := NewManager()
	var mu sync.Mutex
	order := []int{}
	h1 := func(_ Event) error { mu.Lock(); order = append(order, 1); mu.Unlock(); return nil }
	h2 := func(_ Event) error { mu.Lock(); order = append(order, 2); mu.Unlock(); return nil }
	h3 := func(_ Event) error { mu.Lock(); order = append(order, 3); mu.Unlock(); return nil }
	if err := mgr.RegisterHook("test.hook", h1); err != nil {
		t.Fatalf("注册 h1 失败: %v", err)
	}
	if err := mgr.RegisterHook("test.hook", h2); err != nil {
		t.Fatalf("注册 h2 失败: %v", err)
	}
	if err := mgr.RegisterHook("test.hook", h3); err != nil {
		t.Fatalf("注册 h3 失败: %v", err)
	}
	if err := mgr.FireHook(context.Background(), "test.hook", Event{}); err != nil {
		t.Fatalf("FireHook 失败: %v", err)
	}
	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Errorf("handler 应按注册顺序执行 [1,2,3]，实际 %v", order)
	}
}

func TestHookShortCircuit(t *testing.T) {
	mgr := NewManager()
	called := false
	h1 := func(_ Event) error { return errors.New("boom") }
	h2 := func(_ Event) error { called = true; return nil }
	_ = mgr.RegisterHook("test.short", h1)
	_ = mgr.RegisterHook("test.short", h2)
	if err := mgr.FireHook(context.Background(), "test.short", Event{}); err == nil {
		t.Fatal("h1 返回 error 时 FireHook 应返回 error")
	}
	if called {
		t.Error("h1 error 后 h2 不应被调用（短路语义）")
	}
}

func TestRegisterNilHandler(t *testing.T) {
	mgr := NewManager()
	if err := mgr.RegisterHook("test.nil", nil); err == nil {
		t.Fatal("nil handler 注册应失败")
	}
}

func TestFireHookNoHandlers(t *testing.T) {
	mgr := NewManager()
	if err := mgr.FireHook(context.Background(), "test.empty", Event{}); err != nil {
		t.Errorf("无 handler 时 FireHook 应放行（返回 nil），实际 %v", err)
	}
}

func TestCloseIdempotent(t *testing.T) {
	mgr := NewManager()
	p := &fakePlugin{name: "c", version: "1.0.0"}
	_ = mgr.Register(p, nil)
	if err := mgr.Close(); err != nil {
		t.Fatalf("首次 Close 失败: %v", err)
	}
	if err := mgr.Close(); err != nil {
		t.Fatalf("二次 Close 应幂等返回 nil，实际 %v", err)
	}
	if atomic.LoadInt32(&p.closeCnt) != 1 {
		t.Errorf("Close 应仅被调用 1 次（幂等），实际 %d", p.closeCnt)
	}
}

func TestCloseErrorBestEffort(t *testing.T) {
	mgr := NewManager()
	p1 := &fakePlugin{name: "ok", version: "1.0.0"}
	p2 := &fakePlugin{name: "bad", version: "1.0.0", closeErr: errors.New("close boom")}
	_ = mgr.Register(p1, nil)
	_ = mgr.Register(p2, nil)
	if err := mgr.Close(); err == nil {
		t.Fatal("存在 Close 失败的插件时 Manager.Close 应返回 error")
	}
	// 两个插件都应被尝试 Close（尽力关闭）。
	if atomic.LoadInt32(&p1.closeCnt) != 1 {
		t.Errorf("p1 Close 应被调用 1 次，实际 %d", p1.closeCnt)
	}
	if atomic.LoadInt32(&p2.closeCnt) != 1 {
		t.Errorf("p2 Close 应被调用 1 次（尽力关闭），实际 %d", p2.closeCnt)
	}
}

// TestConcurrent 并发测试：多 goroutine 同时 Register/FireHook/Unregister/GetPlugin。
// 用 -race 检测竞态。不严格断言最终状态（并发下计数难精确），仅确保不 panic + 不竞态。
func TestConcurrent(t *testing.T) {
	mgr := NewManager()
	const N = 50
	var wg sync.WaitGroup
	// 并发注册 N 个不同插件。
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p := &fakePlugin{name: fmt.Sprintf("p%d", i), version: "1.0.0"}
			_ = mgr.Register(p, nil)
		}(i)
	}
	// 并发注册/触发钩子。
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = mgr.RegisterHook("concurrent", func(_ Event) error { return nil })
			_ = mgr.FireHook(context.Background(), "concurrent", Event{Payload: i})
		}(i)
	}
	// 并发查询。
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = mgr.GetPlugin(fmt.Sprintf("p%d", i))
			_ = mgr.AllPlugins()
		}(i)
	}
	wg.Wait()
	// 关闭后并发反注册（应不 panic）。
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = mgr.Unregister(fmt.Sprintf("p%d", i))
		}(i)
	}
	wg.Wait()
}
