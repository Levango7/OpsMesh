// inmemory_test.go InMemoryDiscovery 契约测试（含 NewInMemoryDiscoveryFromAddrs 播种）。
package discovery

import (
	"context"
	"testing"
	"time"
)

// waitForSnapshot 在超时内从 ch 读取一个长度符合 wantLen 的快照。
func waitForSnapshot(t *testing.T, ch <-chan []Service, wantLen int, timeout time.Duration) []Service {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case snap, ok := <-ch:
			if !ok {
				t.Fatalf("watch channel 提前关闭（等待长度 %d 的快照）", wantLen)
			}
			if len(snap) == wantLen {
				return snap
			}
		case <-deadline:
			t.Fatalf("等待长度 %d 的快照超时", wantLen)
		}
	}
}

// TestNewInMemoryDiscoveryFromAddrs 验证地址解析、排序、健康标记与深拷贝。
func TestNewInMemoryDiscoveryFromAddrs(t *testing.T) {
	ctx := context.Background()
	const svcName = "opsmesh-controlplane"
	d := NewInMemoryDiscoveryFromAddrs(svcName, " cp1:9090 ,cp2:9091, http://cp3:9092 ,")

	list, err := d.List(ctx, svcName)
	if err != nil {
		t.Fatalf("List 返回错误: %v", err)
	}
	want := []struct {
		id   string
		addr string
		port int
	}{
		{"cp1:9090", "cp1", 9090},
		{"cp2:9091", "cp2", 9091},
		{"http://cp3:9092", "cp3", 9092},
	}
	if len(list) != len(want) {
		t.Fatalf("List 长度 = %d，期望 %d：%+v", len(list), len(want), list)
	}
	for i, w := range want {
		got := list[i]
		if got.ID != w.id || got.Addr != w.addr || got.Port != w.port {
			t.Fatalf("list[%d] = %+v，期望 id=%s addr=%s port=%d", i, got, w.id, w.addr, w.port)
		}
		if got.Name != svcName {
			t.Fatalf("list[%d].Name = %q，期望 %q", i, got.Name, svcName)
		}
		if !got.Healthy {
			t.Fatalf("list[%d] 应初始为 healthy=true：%+v", i, got)
		}
	}

	// 其他服务名应为空。
	other, err := d.List(ctx, "other-service")
	if err != nil || len(other) != 0 {
		t.Fatalf("未知服务 List = %+v, %v；期望空列表 + nil", other, err)
	}

	// List 返回深拷贝：修改结果不应影响内部状态。
	list[0].Name = "mutated"
	again, err := d.List(ctx, svcName)
	if err != nil {
		t.Fatalf("二次 List 返回错误: %v", err)
	}
	if again[0].Name != svcName {
		t.Fatalf("List 结果被外部修改污染内部状态：Name=%q", again[0].Name)
	}
}

// TestNewInMemoryDiscoveryFromAddrs_Empty 验证空/空白地址返回空发现实例。
func TestNewInMemoryDiscoveryFromAddrs_Empty(t *testing.T) {
	ctx := context.Background()
	for _, addrs := range []string{"", "   ", " , "} {
		d := NewInMemoryDiscoveryFromAddrs("svc", addrs)
		list, err := d.List(ctx, "svc")
		if err != nil {
			t.Fatalf("addrs=%q: List 返回错误: %v", addrs, err)
		}
		if list == nil || len(list) != 0 {
			t.Fatalf("addrs=%q: List = %+v，期望非 nil 空切片", addrs, list)
		}
	}
}

// TestNewInMemoryDiscoveryFromAddrs_RegisterDeregister 验证播种实例上的注册/注销/健康标记契约。
func TestNewInMemoryDiscoveryFromAddrs_RegisterDeregister(t *testing.T) {
	ctx := context.Background()
	const svcName = "svc"
	d := NewInMemoryDiscoveryFromAddrs(svcName, "cp1:9090")

	// 幂等注册：按 (Name, Addr, Port) upsert，健康标记可更新。
	if err := d.Register(ctx, Service{ID: "cp1:9090", Name: svcName, Addr: "cp1", Port: 9090, Healthy: false}); err != nil {
		t.Fatalf("重复 Register 返回错误: %v", err)
	}
	list, err := d.List(ctx, svcName)
	if err != nil {
		t.Fatalf("List 返回错误: %v", err)
	}
	// upsert 更新为不健康后，List 只返回健康实例，故应为空。
	if len(list) != 0 {
		t.Fatalf("upsert Healthy=false 后 List = %+v，期望空列表", list)
	}

	// 注册新实例后可见。
	if err := d.Register(ctx, Service{ID: "cp2:9091", Name: svcName, Addr: "cp2", Port: 9091, Healthy: true}); err != nil {
		t.Fatalf("Register 返回错误: %v", err)
	}
	list, err = d.List(ctx, svcName)
	if err != nil || len(list) != 1 || list[0].ID != "cp2:9091" {
		t.Fatalf("健康实例列表 = %+v, %v；期望仅 cp2:9091", list, err)
	}

	// 注销不存在实例返回 ErrNotRegistered。
	if err := d.Deregister(ctx, svcName, "nope", 1); err != ErrNotRegistered {
		t.Fatalf("Deregister 不存在实例 = %v，期望 ErrNotRegistered", err)
	}
	// 注销存在实例后从 List 消失；重复注销返回 ErrNotRegistered。
	if err := d.Deregister(ctx, svcName, "cp2", 9091); err != nil {
		t.Fatalf("Deregister 返回错误: %v", err)
	}
	if err := d.Deregister(ctx, svcName, "cp2", 9091); err != ErrNotRegistered {
		t.Fatalf("重复 Deregister = %v，期望 ErrNotRegistered", err)
	}
	list, err = d.List(ctx, svcName)
	if err != nil || len(list) != 0 {
		t.Fatalf("全部注销后 List = %+v, %v；期望空列表 + nil", list, err)
	}
}

// TestInMemoryDiscoveryFromAddrs_Watch 验证订阅快照、注册推送与 ctx 取消关闭 channel。
func TestInMemoryDiscoveryFromAddrs_Watch(t *testing.T) {
	const svcName = "svc"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d := NewInMemoryDiscoveryFromAddrs(svcName, "cp1:9090")
	ch, err := d.Watch(ctx, svcName)
	if err != nil {
		t.Fatalf("Watch 返回错误: %v", err)
	}

	// 订阅时立即推送当前快照。
	first := waitForSnapshot(t, ch, 1, time.Second)
	if first[0].ID != "cp1:9090" {
		t.Fatalf("初始快照 = %+v，期望 cp1:9090", first)
	}

	// 注册新实例后推送全量快照。
	if err := d.Register(context.Background(), Service{
		ID: "cp2:9091", Name: svcName, Addr: "cp2", Port: 9091, Healthy: true,
	}); err != nil {
		t.Fatalf("Register 返回错误: %v", err)
	}
	second := waitForSnapshot(t, ch, 2, time.Second)
	if second[0].ID != "cp1:9090" || second[1].ID != "cp2:9091" {
		t.Fatalf("注册后快照 = %+v，期望 cp1:9090 + cp2:9091", second)
	}

	// MarkHealthy=false 后推送的快照应只剩健康实例。
	if err := d.MarkHealthy(context.Background(), svcName, "cp2", 9091, false); err != nil {
		t.Fatalf("MarkHealthy 返回错误: %v", err)
	}
	third := waitForSnapshot(t, ch, 1, time.Second)
	if third[0].ID != "cp1:9090" {
		t.Fatalf("健康变更后快照 = %+v，期望仅 cp1:9090", third)
	}

	// ctx 取消后 channel 应在 1s 内关闭。
	cancel()
	timeout := time.After(time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("ctx 取消后 Watch channel 未关闭")
		}
	}
}
