package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func newTestFileDiscovery(t *testing.T) (*FileDiscovery, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "discovery.json")
	d, err := NewFileDiscovery(path)
	if err != nil {
		t.Fatalf("NewFileDiscovery: %v", err)
	}
	return d, path
}

func awaitSnapshot(t *testing.T, ch <-chan []Service) []Service {
	t.Helper()
	select {
	case snap, ok := <-ch:
		if !ok {
			t.Fatal("watch channel 意外关闭")
		}
		return snap
	case <-time.After(6 * time.Second):
		t.Fatal("等待 watch 快照超时")
	}
	return nil
}

func TestFileDiscovery_RegisterListDeregister(t *testing.T) {
	d, path := newTestFileDiscovery(t)
	ctx := context.Background()

	svc := Service{
		Name:     "opsmesh-controlplane",
		Addr:     "10.0.0.1",
		Port:     9090,
		Healthy:  true,
		Metadata: map[string]string{"az": "a"},
	}
	if err := d.Register(ctx, svc); err != nil {
		t.Fatalf("Register: %v", err)
	}

	list, err := d.List(ctx, svc.Name)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List 应返回 1 个实例，得到 %d", len(list))
	}
	if list[0].Addr != svc.Addr || list[0].Port != svc.Port || !list[0].Healthy {
		t.Fatalf("List 实例字段不符: %+v", list[0])
	}
	if list[0].Metadata["az"] != "a" {
		t.Fatalf("Metadata 未持久化: %+v", list[0].Metadata)
	}

	// 重新打开同一路径应读到已落盘实例（验证 JSON 持久化往返）。
	d2, err := NewFileDiscovery(path)
	if err != nil {
		t.Fatalf("NewFileDiscovery reopen: %v", err)
	}
	list2, err := d2.List(ctx, svc.Name)
	if err != nil || len(list2) != 1 {
		t.Fatalf("重开后 List 应返回 1 个实例，得到 %d, err=%v", len(list2), err)
	}

	// 文件权限必须为 0600（Windows 的 Chmod 仅控制只读位，跳过严格断言）。
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("文件权限应为 0600，得到 %04o", perm)
		}
	}

	if err := d.Deregister(ctx, svc.Name, svc.Addr, svc.Port); err != nil {
		t.Fatalf("Deregister: %v", err)
	}
	list, err = d.List(ctx, svc.Name)
	if err != nil {
		t.Fatalf("Deregister 后 List: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("Deregister 后应为空，得到 %d", len(list))
	}

	if err := d.Deregister(ctx, svc.Name, svc.Addr, svc.Port); !errors.Is(err, ErrNotRegistered) {
		t.Fatalf("重复 Deregister 应返回 ErrNotRegistered，得到 %v", err)
	}
}

func TestFileDiscovery_MarkHealthyFiltersList(t *testing.T) {
	d, _ := newTestFileDiscovery(t)
	ctx := context.Background()
	const name = "svc"

	for _, addr := range []string{"10.0.0.1", "10.0.0.2"} {
		if err := d.Register(ctx, Service{Name: name, Addr: addr, Port: 9090, Healthy: true}); err != nil {
			t.Fatalf("Register %s: %v", addr, err)
		}
	}

	if err := d.MarkHealthy(ctx, name, "10.0.0.1", 9090, false); err != nil {
		t.Fatalf("MarkHealthy(false): %v", err)
	}
	list, err := d.List(ctx, name)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].Addr != "10.0.0.2" {
		t.Fatalf("不健康实例应被过滤，得到 %+v", list)
	}

	if err := d.MarkHealthy(ctx, name, "10.0.0.1", 9090, true); err != nil {
		t.Fatalf("MarkHealthy(true): %v", err)
	}
	list, err = d.List(ctx, name)
	if err != nil || len(list) != 2 {
		t.Fatalf("恢复健康后应有 2 个实例，得到 %d, err=%v", len(list), err)
	}

	if err := d.MarkHealthy(ctx, name, "10.9.9.9", 9090, true); !errors.Is(err, ErrNotRegistered) {
		t.Fatalf("未知实例 MarkHealthy 应返回 ErrNotRegistered，得到 %v", err)
	}
}

func TestFileDiscovery_WatchEmitsOnFileChange(t *testing.T) {
	d, path := newTestFileDiscovery(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const name = "svc"

	ch, err := d.Watch(ctx, name)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if snap := awaitSnapshot(t, ch); len(snap) != 0 {
		t.Fatalf("初始快照应为空，得到 %+v", snap)
	}

	// 经 Register 变更（内部原子写）应触发一次全量快照。
	if err := d.Register(ctx, Service{Name: name, Addr: "10.0.0.1", Port: 9090, Healthy: true}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if snap := awaitSnapshot(t, ch); len(snap) != 1 || snap[0].Addr != "10.0.0.1" {
		t.Fatalf("Register 后快照不符: %+v", snap)
	}

	// 外部直接改写 backing file（模拟其他进程/运维编辑）也应被轮询捕获。
	external := fileDocument{Services: []fileService{
		{Name: name, Address: "10.0.0.1", Port: 9090, Healthy: true},
		{Name: name, Address: "10.0.0.2", Port: 9090, Healthy: true},
	}}
	raw, err := json.Marshal(external)
	if err != nil {
		t.Fatalf("marshal external doc: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("外部写文件: %v", err)
	}
	snap := awaitSnapshot(t, ch)
	if len(snap) != 2 {
		t.Fatalf("外部变更后应有 2 个实例，得到 %+v", snap)
	}
}

func TestFileDiscovery_WatchClosesOnContextCancel(t *testing.T) {
	d, _ := newTestFileDiscovery(t)
	ctx, cancel := context.WithCancel(context.Background())
	const name = "svc"

	ch, err := d.Watch(ctx, name)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	_ = awaitSnapshot(t, ch)

	cancel()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("ctx 取消后 watch channel 未关闭")
		}
	}
}

func TestFileDiscovery_CorruptFileTolerated(t *testing.T) {
	d, path := newTestFileDiscovery(t)
	ctx := context.Background()

	// 文件不存在：List 返回空切片 + nil。
	list, err := d.List(ctx, "svc")
	if err != nil {
		t.Fatalf("缺失文件 List 应返回 nil 错误，得到 %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("缺失文件 List 应为空，得到 %+v", list)
	}

	// 文件损坏：List 同样返回空切片 + nil。
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("写入损坏文件: %v", err)
	}
	list, err = d.List(ctx, "svc")
	if err != nil {
		t.Fatalf("损坏文件 List 应返回 nil 错误，得到 %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("损坏文件 List 应为空，得到 %+v", list)
	}

	// Register 应重建合法文件。
	if err := d.Register(ctx, Service{Name: "svc", Addr: "10.0.0.1", Port: 9090, Healthy: true}); err != nil {
		t.Fatalf("损坏文件 Register 应重建: %v", err)
	}
	list, err = d.List(ctx, "svc")
	if err != nil || len(list) != 1 {
		t.Fatalf("重建后应有 1 个实例，得到 %d, err=%v", len(list), err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取重建文件: %v", err)
	}
	var doc fileDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("重建文件应为合法 JSON: %v", err)
	}
	if len(doc.Services) != 1 {
		t.Fatalf("重建文件应有 1 条记录，得到 %d", len(doc.Services))
	}
}
