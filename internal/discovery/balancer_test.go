package discovery

import (
	"context"
	"testing"
)

// makeServices 构造测试用服务实例列表。
func makeServices(n int) []Service {
	out := make([]Service, n)
	for i := 0; i < n; i++ {
		out[i] = Service{
			ID:      string(rune('a' + i)),
			Name:    "svc",
			Addr:    "host" + string(rune('a'+i)),
			Port:    9090,
			Healthy: true,
		}
	}
	return out
}

// TestRoundRobinBasic 验证 RoundRobin 轮询语义。
func TestRoundRobinBasic(t *testing.T) {
	ctx := context.Background()
	rr := NewRoundRobin(makeServices(3))

	// 连续调用 Next 应轮询 a, b, c, a, b, c, ...
	expected := []string{"a", "b", "c", "a", "b", "c"}
	for i, want := range expected {
		svc, err := rr.Next(ctx)
		if err != nil {
			t.Fatalf("Next[%d] 错误: %v", i, err)
		}
		if svc.ID != want {
			t.Fatalf("Next[%d] = %q, want %q", i, svc.ID, want)
		}
	}
}

// TestRoundRobinEmpty 验证空实例列表返回 ErrNoInstances。
func TestRoundRobinEmpty(t *testing.T) {
	ctx := context.Background()
	rr := NewRoundRobin(nil)
	_, err := rr.Next(ctx)
	if err != ErrNoInstances {
		t.Fatalf("空列表应返回 ErrNoInstances，得到: %v", err)
	}
}

// TestRoundRobinUpdate 验证 Update 更新实例列表。
func TestRoundRobinUpdate(t *testing.T) {
	ctx := context.Background()
	rr := NewRoundRobin(makeServices(2))

	// 初始 a, b
	svc, _ := rr.Next(ctx)
	if svc.ID != "a" {
		t.Fatalf("初始 Next 应为 a，得到 %q", svc.ID)
	}

	// Update 后重置 cursor，从 a 开始
	rr.Update(makeServices(3))
	svc, _ = rr.Next(ctx)
	if svc.ID != "a" {
		t.Fatalf("Update 后 Next 应为 a，得到 %q", svc.ID)
	}
}

// TestRoundRobinUnhealthy 验证不健康实例被过滤。
func TestRoundRobinUnhealthy(t *testing.T) {
	ctx := context.Background()
	svcs := []Service{
		{ID: "a", Healthy: true},
		{ID: "b", Healthy: false},
		{ID: "c", Healthy: true},
	}
	rr := NewRoundRobin(svcs)
	// 应只轮询 a, c
	for i, want := range []string{"a", "c", "a", "c"} {
		svc, err := rr.Next(ctx)
		if err != nil {
			t.Fatalf("Next[%d] 错误: %v", i, err)
		}
		if svc.ID != want {
			t.Fatalf("Next[%d] = %q, want %q", i, svc.ID, want)
		}
	}
}

// TestFailoverBasic 验证 Failover 主备语义：始终返回主，直到 MarkFailed。
func TestFailoverBasic(t *testing.T) {
	ctx := context.Background()
	fo := NewFailover(makeServices(3))

	// 始终返回主 a
	for i := 0; i < 5; i++ {
		svc, err := fo.Next(ctx)
		if err != nil {
			t.Fatalf("Next[%d] 错误: %v", i, err)
		}
		if svc.ID != "a" {
			t.Fatalf("Failover 应始终返回主 a，得到 %q", svc.ID)
		}
	}

	// MarkFailed 切换到备 b
	next, err := fo.MarkFailed()
	if err != nil {
		t.Fatal(err)
	}
	if next.ID != "b" {
		t.Fatalf("MarkFailed 应切换到 b，得到 %q", next.ID)
	}
	// 现在 Next 返回 b
	svc, _ := fo.Next(ctx)
	if svc.ID != "b" {
		t.Fatalf("MarkFailed 后 Next 应为 b，得到 %q", svc.ID)
	}
}

// TestFailoverWrapAround 验证无备可用时回绕到主。
func TestFailoverWrapAround(t *testing.T) {
	ctx := context.Background()
	fo := NewFailover(makeServices(2))

	// 当前 a，MarkFailed → b
	_, _ = fo.MarkFailed()
	svc, _ := fo.Next(ctx)
	if svc.ID != "b" {
		t.Fatalf("应为 b，得到 %q", svc.ID)
	}

	// 当前 b，MarkFailed → 回绕到 a
	next, _ := fo.MarkFailed()
	if next.ID != "a" {
		t.Fatalf("无备应回绕到 a，得到 %q", next.ID)
	}
}

// TestFailoverSingleInstance 验证单实例退化为始终返回该实例（向后兼容）。
func TestFailoverSingleInstance(t *testing.T) {
	ctx := context.Background()
	fo := NewFailover(makeServices(1))

	for i := 0; i < 5; i++ {
		svc, err := fo.Next(ctx)
		if err != nil {
			t.Fatalf("Next[%d] 错误: %v", i, err)
		}
		if svc.ID != "a" {
			t.Fatalf("单实例应始终返回 a，得到 %q", svc.ID)
		}
	}

	// MarkFailed 后仍回绕到 a
	next, _ := fo.MarkFailed()
	if next.ID != "a" {
		t.Fatalf("单实例 MarkFailed 应回绕到 a，得到 %q", next.ID)
	}
}

// TestFailoverEmpty 验证空实例列表返回 ErrNoInstances。
func TestFailoverEmpty(t *testing.T) {
	ctx := context.Background()
	fo := NewFailover(nil)
	_, err := fo.Next(ctx)
	if err != ErrNoInstances {
		t.Fatalf("空列表应返回 ErrNoInstances，得到: %v", err)
	}
	_, err = fo.MarkFailed()
	if err != ErrNoInstances {
		t.Fatalf("空列表 MarkFailed 应返回 ErrNoInstances，得到: %v", err)
	}
}

// TestFailoverUpdate 验证 Update 重置 current 到 0。
func TestFailoverUpdate(t *testing.T) {
	ctx := context.Background()
	fo := NewFailover(makeServices(3))

	// MarkFailed 两次 → current=2 (c)
	_, _ = fo.MarkFailed()
	_, _ = fo.MarkFailed()
	svc, _ := fo.Next(ctx)
	if svc.ID != "c" {
		t.Fatalf("应为 c，得到 %q", svc.ID)
	}

	// Update 后重置到 a
	fo.Update(makeServices(3))
	svc, _ = fo.Next(ctx)
	if svc.ID != "a" {
		t.Fatalf("Update 后应为 a，得到 %q", svc.ID)
	}
}

// TestNewBalancer 验证 NewBalancer 工厂函数。
func TestNewBalancer(t *testing.T) {
	svcs := makeServices(2)

	// round-robin 系列
	for _, name := range []string{"round-robin", "roundrobin", "rr"} {
		b := NewBalancer(name, svcs)
		if _, ok := b.(*RoundRobin); !ok {
			t.Fatalf("NewBalancer(%q) 应返回 *RoundRobin", name)
		}
	}

	// failover 系列
	for _, name := range []string{"failover", "fo", ""} {
		b := NewBalancer(name, svcs)
		if _, ok := b.(*Failover); !ok {
			t.Fatalf("NewBalancer(%q) 应返回 *Failover", name)
		}
	}

	// 未知策略默认 Failover
	b := NewBalancer("unknown", svcs)
	if _, ok := b.(*Failover); !ok {
		t.Fatal("未知策略应默认返回 *Failover")
	}
}

// TestUpdatableBalancer 验证 UpdatableBalancer 接口。
func TestUpdatableBalancer(t *testing.T) {
	svcs := makeServices(2)
	b := NewBalancer("round-robin", svcs)

	ub, ok := b.(UpdatableBalancer)
	if !ok {
		t.Fatal("RoundRobin 应实现 UpdatableBalancer")
	}
	// Update 不应 panic
	ub.Update(makeServices(3))
}

// TestFailoverLenCurrent 验证 Len/Current 调试方法。
func TestFailoverLenCurrent(t *testing.T) {
	fo := NewFailover(makeServices(3))
	if fo.Len() != 3 {
		t.Fatalf("Len 应为 3，得到 %d", fo.Len())
	}
	if fo.Current() != 0 {
		t.Fatalf("Current 应为 0，得到 %d", fo.Current())
	}
	_, _ = fo.MarkFailed()
	if fo.Current() != 1 {
		t.Fatalf("MarkFailed 后 Current 应为 1，得到 %d", fo.Current())
	}
}
