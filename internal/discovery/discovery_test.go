package discovery

import (
	"context"
	"testing"
	"time"
)

// TestNoopDiscovery 验证 NoopDiscovery 的 no-op 语义。
func TestNoopDiscovery(t *testing.T) {
	n := NewNoopDiscovery()
	ctx := context.Background()

	// Register 返回 ErrNotImplemented
	if err := n.Register(ctx, Service{ID: "x"}); err != ErrNotImplemented {
		t.Fatalf("Register 应返回 ErrNotImplemented，得到: %v", err)
	}
	// Deregister 返回 ErrNotImplemented
	if err := n.Deregister(ctx, "x"); err != ErrNotImplemented {
		t.Fatalf("Deregister 应返回 ErrNotImplemented，得到: %v", err)
	}
	// List 返回空切片 + nil
	list, err := n.List(ctx, "any")
	if err != nil {
		t.Fatalf("List 不应返回错误: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("List 应返回空切片，得到长度 %d", len(list))
	}
	// Watch 返回立即关闭的 channel
	ch, err := n.Watch(ctx, "any")
	if err != nil {
		t.Fatalf("Watch 不应返回错误: %v", err)
	}
	// 读取应立即得到零值（channel 关闭）
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("Watch channel 应已关闭")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Watch channel 应立即关闭")
	}
}

// TestStaticDiscoveryRegisterDeregister 验证 StaticDiscovery 的注册/注销/列表。
func TestStaticDiscoveryRegisterDeregister(t *testing.T) {
	d := NewStaticDiscovery()
	ctx := context.Background()

	// 初始为空
	list, err := d.List(ctx, "svc")
	if err != nil || len(list) != 0 {
		t.Fatalf("初始 List 应为空，得到: %v, %v", list, err)
	}

	// 注册两个实例
	if err := d.Register(ctx, Service{ID: "a", Name: "svc", Addr: "host1", Port: 9090, Healthy: true}); err != nil {
		t.Fatal(err)
	}
	if err := d.Register(ctx, Service{ID: "b", Name: "svc", Addr: "host2", Port: 9090, Healthy: true}); err != nil {
		t.Fatal(err)
	}

	// List 返回两个实例，按 ID 升序
	list, err = d.List(ctx, "svc")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("List 应返回 2 个实例，得到 %d", len(list))
	}
	if list[0].ID != "a" || list[1].ID != "b" {
		t.Fatalf("List 顺序错误: %v", list)
	}

	// 注销 a
	if err := d.Deregister(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	list, _ = d.List(ctx, "svc")
	if len(list) != 1 || list[0].ID != "b" {
		t.Fatalf("注销后 List 应只剩 b，得到: %v", list)
	}

	// 注销不存在的 ID 不报错（幂等）
	if err := d.Deregister(ctx, "nonexistent"); err != nil {
		t.Fatalf("注销不存在的 ID 应幂等无错: %v", err)
	}
}

// TestStaticDiscoveryUnhealthy 验证不健康实例被过滤。
func TestStaticDiscoveryUnhealthy(t *testing.T) {
	d := NewStaticDiscovery()
	ctx := context.Background()
	_ = d.Register(ctx, Service{ID: "a", Name: "svc", Addr: "h1", Healthy: true})
	_ = d.Register(ctx, Service{ID: "b", Name: "svc", Addr: "h2", Healthy: false})

	list, _ := d.List(ctx, "svc")
	if len(list) != 1 || list[0].ID != "a" {
		t.Fatalf("应过滤不健康实例，得到: %v", list)
	}
}

// TestStaticDiscoveryFromAddrs 验证从地址列表构造 StaticDiscovery。
func TestStaticDiscoveryFromAddrs(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		addrs string
		want  int
	}{
		{"cp1:9090,cp2:9090,cp3:9090", 3},
		{"cp1:9090,,cp2:9090", 2},       // 空项被忽略
		{" cp1:9090 , cp2:9090 ", 2},    // 空格被 trim
		{"http://cp1:8080,cp2:9090", 2}, // 带 scheme
		{"", 0},                         // 空字符串
		{",,,", 0},                      // 全空项
	}
	for _, c := range cases {
		d := NewStaticDiscoveryFromAddrs("opsmesh-controlplane", c.addrs)
		list, _ := d.List(ctx, "opsmesh-controlplane")
		if len(list) != c.want {
			t.Fatalf("NewStaticDiscoveryFromAddrs(%q) 应返回 %d 个实例，得到 %d: %v", c.addrs, c.want, len(list), list)
		}
	}
}

// TestStaticDiscoveryFromAddrsParseAddr 验证地址解析。
func TestStaticDiscoveryFromAddrsParseAddr(t *testing.T) {
	ctx := context.Background()
	d := NewStaticDiscoveryFromAddrs("svc", "cp1:9090,http://cp2:8080,plainhost")
	list, _ := d.List(ctx, "svc")
	if len(list) != 3 {
		t.Fatalf("应返回 3 个实例，得到 %d", len(list))
	}
	// 验证解析结果（按 ID 升序：cp1:9090, http://cp2:8080, plainhost）
	if list[0].Addr != "cp1" || list[0].Port != 9090 {
		t.Fatalf("cp1:9090 解析错误: %+v", list[0])
	}
	if list[1].Addr != "cp2" || list[1].Port != 8080 {
		t.Fatalf("http://cp2:8080 解析错误: %+v", list[1])
	}
	if list[2].Addr != "plainhost" || list[2].Port != 0 {
		t.Fatalf("plainhost 解析错误: %+v", list[2])
	}
}

// TestStaticDiscoveryWatch 验证 Watch 立即推送当前列表。
func TestStaticDiscoveryWatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d := NewStaticDiscoveryFromAddrs("svc", "cp1:9090,cp2:9090")
	ch, err := d.Watch(ctx, "svc")
	if err != nil {
		t.Fatal(err)
	}
	// 应立即收到初始列表
	select {
	case list := <-ch:
		if len(list) != 2 {
			t.Fatalf("Watch 应立即推送 2 个实例，得到 %d", len(list))
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Watch 应立即推送初始列表")
	}
}

// TestParseAddrPort 验证地址解析函数。
func TestParseAddrPort(t *testing.T) {
	cases := []struct {
		in       string
		wantAddr string
		wantPort int
	}{
		{"cp1:9090", "cp1", 9090},
		{"http://cp1:8080", "cp1", 8080},
		{"https://cp1:443", "cp1", 443},
		{"plainhost", "plainhost", 0},
		{"[::1]:9090", "[::1]", 9090},
		{"http://[::1]:8080", "[::1]", 8080},
	}
	for _, c := range cases {
		addr, port := parseAddrPort(c.in)
		if addr != c.wantAddr || port != c.wantPort {
			t.Fatalf("parseAddrPort(%q) = (%q, %d), want (%q, %d)", c.in, addr, port, c.wantAddr, c.wantPort)
		}
	}
}
