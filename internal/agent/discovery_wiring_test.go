// discovery_wiring_test.go 聚焦测试服务发现后端选择（newDiscoveryForConfig /
// setupDiscoveryBalancer）与上报退出码收敛（clampExitCode）。
package agent

import (
	"context"
	"math"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/discovery"
)

// testDiscoveryService 与 setupDiscoveryBalancer 内部使用的服务名保持一致。
const testDiscoveryService = "opsmesh-controlplane"

// TestNewDiscoveryForConfig_Memory 验证 memory（含缺省空值）后端由 addrs 播种，
// 且返回的具体类型是 InMemoryDiscovery。
func TestNewDiscoveryForConfig_Memory(t *testing.T) {
	ctx := context.Background()
	for _, backend := range []string{"", "memory", " MEMORY "} {
		disc, err := newDiscoveryForConfig(
			&config.Config{DiscoveryBackend: backend},
			testDiscoveryService,
			"cp1:9090, cp2:9091",
		)
		if err != nil {
			t.Fatalf("backend=%q: newDiscoveryForConfig 返回错误: %v", backend, err)
		}
		im, ok := disc.(*discovery.InMemoryDiscovery)
		if !ok {
			t.Fatalf("backend=%q: 期望 *InMemoryDiscovery，得到 %T", backend, disc)
		}
		list, err := im.List(ctx, testDiscoveryService)
		if err != nil {
			t.Fatalf("backend=%q: List 返回错误: %v", backend, err)
		}
		if len(list) != 2 {
			t.Fatalf("backend=%q: List 长度 = %d，期望 2", backend, len(list))
		}
		want := []struct {
			id   string
			addr string
			port int
		}{
			{"cp1:9090", "cp1", 9090},
			{"cp2:9091", "cp2", 9091},
		}
		for i, w := range want {
			got := list[i]
			if got.ID != w.id || got.Addr != w.addr || got.Port != w.port {
				t.Fatalf("backend=%q: list[%d] = %+v，期望 id=%s addr=%s port=%d",
					backend, i, got, w.id, w.addr, w.port)
			}
			if got.Name != testDiscoveryService || !got.Healthy {
				t.Fatalf("backend=%q: list[%d] 服务名/健康状态不符: %+v", backend, i, got)
			}
		}
	}
}

// TestNewDiscoveryForConfig_Static 验证 static 后端保持既有语义（类型与实例列表）。
func TestNewDiscoveryForConfig_Static(t *testing.T) {
	ctx := context.Background()
	disc, err := newDiscoveryForConfig(
		&config.Config{DiscoveryBackend: "static"},
		testDiscoveryService,
		"cp1:9090,cp2:9091",
	)
	if err != nil {
		t.Fatalf("newDiscoveryForConfig 返回错误: %v", err)
	}
	sd, ok := disc.(*discovery.StaticDiscovery)
	if !ok {
		t.Fatalf("期望 *StaticDiscovery，得到 %T", disc)
	}
	list, err := sd.List(ctx, testDiscoveryService)
	if err != nil {
		t.Fatalf("List 返回错误: %v", err)
	}
	if len(list) != 2 || list[0].ID != "cp1:9090" || list[1].ID != "cp2:9091" {
		t.Fatalf("static 实例列表不符: %+v", list)
	}
}

// TestNewDiscoveryForConfig_File 验证 file 后端可经返回的 discovery 注册并被 List 读到，
// 且数据确实落到 backing file（重新打开同一路径仍可见）。
func TestNewDiscoveryForConfig_File(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "discovery.json")
	disc, err := newDiscoveryForConfig(
		&config.Config{DiscoveryBackend: "file", DiscoveryFile: path},
		testDiscoveryService,
		"",
	)
	if err != nil {
		t.Fatalf("newDiscoveryForConfig 返回错误: %v", err)
	}
	fd, ok := disc.(*discovery.FileDiscovery)
	if !ok {
		t.Fatalf("期望 *FileDiscovery，得到 %T", disc)
	}
	// 初始文件缺失：List 应为空且不报错。
	list, err := fd.List(ctx, testDiscoveryService)
	if err != nil || len(list) != 0 {
		t.Fatalf("空文件 List = %+v, %v；期望空列表 + nil", list, err)
	}
	// 经返回的 discovery 注册，随后应能列出。
	if err := fd.Register(ctx, discovery.Service{
		ID: "cp9:9099", Name: testDiscoveryService, Addr: "cp9", Port: 9099, Healthy: true,
	}); err != nil {
		t.Fatalf("Register 返回错误: %v", err)
	}
	list, err = fd.List(ctx, testDiscoveryService)
	if err != nil {
		t.Fatalf("注册后 List 返回错误: %v", err)
	}
	if len(list) != 1 || list[0].Addr != "cp9" || list[0].Port != 9099 {
		t.Fatalf("注册后 List 不符: %+v", list)
	}
	// 重新打开同一文件，验证已持久化。
	reopened, err := discovery.NewFileDiscovery(path)
	if err != nil {
		t.Fatalf("NewFileDiscovery 返回错误: %v", err)
	}
	list, err = reopened.List(ctx, testDiscoveryService)
	if err != nil || len(list) != 1 || list[0].ID != "cp9:9099" {
		t.Fatalf("重新打开后 List = %+v, %v；期望 cp9:9099", list, err)
	}
}

// TestNewDiscoveryForConfig_Fallback 验证无法构造后端时返回 (nil, error)（调用方据此回退），
// 且不 panic（含日志路径）。
func TestNewDiscoveryForConfig_Fallback(t *testing.T) {
	cases := []struct {
		name string
		cfg  *config.Config
	}{
		{"unknown-backend", &config.Config{DiscoveryBackend: "bogus"}},
		{"file-without-path", &config.Config{DiscoveryBackend: "file"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			disc, err := newDiscoveryForConfig(tc.cfg, testDiscoveryService, "cp1:9090")
			if err == nil {
				t.Fatal("期望返回错误以便回退 addrs failover")
			}
			if disc != nil {
				t.Fatalf("回退时 discovery 应为 nil，得到 %T", disc)
			}
		})
	}
}

// TestSetupDiscoveryBalancer_MemoryBackend 验证 memory 后端把 addrs 注入 balancer。
func TestSetupDiscoveryBalancer_MemoryBackend(t *testing.T) {
	cli, err := newTestGRPCClient()
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{
		cfg:  &config.Config{DiscoveryBackend: "memory", LBStrategy: "failover"},
		grpc: cli,
	}
	a.setupDiscoveryBalancer([]string{"cp1:9090", "cp2:9091"})
	if cli.balancer == nil {
		t.Fatal("memory 后端应注入 balancer")
	}
	fo, ok := cli.balancer.(*discovery.Failover)
	if !ok {
		t.Fatalf("LBStrategy=failover 应得到 *Failover，得到 %T", cli.balancer)
	}
	if fo.Len() != 2 {
		t.Fatalf("balancer 实例数 = %d，期望 2", fo.Len())
	}
	svc, err := fo.Next(context.Background())
	if err != nil {
		t.Fatalf("Next 返回错误: %v", err)
	}
	if svc.Addr != "cp1" || svc.Port != 9090 {
		t.Fatalf("Next = %+v，期望主实例 cp1:9090", svc)
	}
}

// TestSetupDiscoveryBalancer_StaticBackend 验证 static 后端保持原有行为（注入 balancer）。
func TestSetupDiscoveryBalancer_StaticBackend(t *testing.T) {
	cli, err := newTestGRPCClient()
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{
		cfg:  &config.Config{DiscoveryBackend: "static", LBStrategy: "round-robin"},
		grpc: cli,
	}
	a.setupDiscoveryBalancer([]string{"cp1:9090", "cp2:9091"})
	if cli.balancer == nil {
		t.Fatal("static 后端应注入 balancer")
	}
	rr, ok := cli.balancer.(*discovery.RoundRobin)
	if !ok {
		t.Fatalf("LBStrategy=round-robin 应得到 *RoundRobin，得到 %T", cli.balancer)
	}
	// 轮询两次应覆盖两个播种实例（顺序按 ID 稳定排序）。
	seen := make(map[string]bool, 2)
	for i := 0; i < 2; i++ {
		svc, err := rr.Next(context.Background())
		if err != nil {
			t.Fatalf("Next 返回错误: %v", err)
		}
		seen[svc.ID] = true
	}
	if !seen["cp1:9090"] || !seen["cp2:9091"] {
		t.Fatalf("轮询实例 = %v，期望覆盖 cp1:9090 与 cp2:9091", seen)
	}
}

// TestSetupDiscoveryBalancer_FileBackend 验证 file 后端从 backing file 读取实例并注入 balancer。
func TestSetupDiscoveryBalancer_FileBackend(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "discovery.json")
	seed, err := discovery.NewFileDiscovery(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Register(ctx, discovery.Service{
		ID: "cp-file:9090", Name: testDiscoveryService, Addr: "cp-file", Port: 9090, Healthy: true,
	}); err != nil {
		t.Fatal(err)
	}

	cli, err := newTestGRPCClient()
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{
		cfg:  &config.Config{DiscoveryBackend: "file", DiscoveryFile: path, LBStrategy: "failover"},
		grpc: cli,
	}
	a.setupDiscoveryBalancer([]string{"ignored:9090"})
	if cli.balancer == nil {
		t.Fatal("file 后端应注入 balancer")
	}
	fo, ok := cli.balancer.(*discovery.Failover)
	if !ok {
		t.Fatalf("期望 *Failover，得到 %T", cli.balancer)
	}
	if fo.Len() != 1 {
		t.Fatalf("balancer 实例数 = %d，期望 1", fo.Len())
	}
	svc, err := fo.Next(ctx)
	if err != nil {
		t.Fatalf("Next 返回错误: %v", err)
	}
	if svc.Addr != "cp-file" || svc.Port != 9090 {
		t.Fatalf("Next = %+v，期望 cp-file:9090", svc)
	}
}

// TestSetupDiscoveryBalancer_FallbackNoPanic 验证未知后端 / 非法 file 路径时回退：
// 不 panic、不注入 balancer（invoke 回退 addrs failover）。
func TestSetupDiscoveryBalancer_FallbackNoPanic(t *testing.T) {
	cases := []struct {
		name string
		cfg  *config.Config
	}{
		{"unknown-backend", &config.Config{DiscoveryBackend: "bogus", LBStrategy: "failover"}},
		{"file-without-path", &config.Config{DiscoveryBackend: "file", LBStrategy: "failover"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cli, err := newTestGRPCClient()
			if err != nil {
				t.Fatal(err)
			}
			a := &Agent{cfg: tc.cfg, grpc: cli}
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("setupDiscoveryBalancer panic: %v", r)
				}
			}()
			a.setupDiscoveryBalancer([]string{"127.0.0.1:9090"})
			if cli.balancer != nil {
				t.Fatalf("回退时应保持 balancer 为 nil，得到 %T", cli.balancer)
			}
		})
	}
}

// TestClampExitCode 验证退出码收敛到 int32 值域（含 Windows DWORD 2^32+5）。
func TestClampExitCode(t *testing.T) {
	cases := []struct {
		name string
		in   int64
		want int32
	}{
		{"zero", 0, 0},
		{"negative-one", -1, -1},
		{"min-int32", math.MinInt32, math.MinInt32},
		{"min-int32-minus-one", int64(math.MinInt32) - 1, math.MinInt32},
		{"max-int32", math.MaxInt32, math.MaxInt32},
		{"max-int32-plus-one", int64(math.MaxInt32) + 1, math.MaxInt32},
		{"windows-dword-2pow32-plus-5", 1<<32 + 5, math.MaxInt32},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if strconv.IntSize == 32 && (tc.in > math.MaxInt32 || tc.in < math.MinInt32) {
				t.Skip("32 位 int 无法表示越界输入")
			}
			if got := clampExitCode(int(tc.in)); got != tc.want {
				t.Fatalf("clampExitCode(%d) = %d，期望 %d", tc.in, got, tc.want)
			}
		})
	}
}
