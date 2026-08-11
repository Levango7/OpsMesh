// grpcclient_discovery_test.go M1-3 服务发现集成测试。
package agent

import (
	"testing"

	"opsmesh/internal/discovery"
)

// TestGRPCClient_SetBalancer 验证 SetBalancer 能正确注入 balancer。
func TestGRPCClient_SetBalancer(t *testing.T) {
	cli, err := NewGRPCClient([]string{"127.0.0.1:9090"}, "", "", "", 9090)
	if err != nil {
		t.Fatal(err)
	}
	// 初始 balancer 为 nil（回退到 addrs failover）
	if cli.balancer != nil {
		t.Fatal("初始 balancer 应为 nil")
	}

	// 注入 Failover balancer
	svcs := []discovery.Service{
		{ID: "cp1", Name: "opsmesh-controlplane", Addr: "cp1", Port: 9090, Healthy: true},
		{ID: "cp2", Name: "opsmesh-controlplane", Addr: "cp2", Port: 9090, Healthy: true},
	}
	balancer := discovery.NewFailover(svcs)
	cli.SetBalancer(balancer)
	if cli.balancer == nil {
		t.Fatal("SetBalancer 后 balancer 不应为 nil")
	}

	// 清除 balancer
	cli.SetBalancer(nil)
	if cli.balancer != nil {
		t.Fatal("SetBalancer(nil) 后 balancer 应为 nil")
	}
}

// TestGRPCClient_MarkBalancerFailed 验证 markBalancerFailed 对 Failover 类型 balancer 生效。
func TestGRPCClient_MarkBalancerFailed(t *testing.T) {
	cli, err := NewGRPCClient([]string{"127.0.0.1:9090"}, "", "", "", 9090)
	if err != nil {
		t.Fatal(err)
	}
	svcs := []discovery.Service{
		{ID: "cp1", Addr: "cp1", Port: 9090, Healthy: true},
		{ID: "cp2", Addr: "cp2", Port: 9090, Healthy: true},
	}
	fo := discovery.NewFailover(svcs)
	cli.SetBalancer(fo)

	// 初始 current=0 (cp1)
	if fo.Current() != 0 {
		t.Fatalf("初始 Current 应为 0，得到 %d", fo.Current())
	}

	// markBalancerFailed 应触发主→备切换
	cli.markBalancerFailed()
	if fo.Current() != 1 {
		t.Fatalf("markBalancerFailed 后 Current 应为 1，得到 %d", fo.Current())
	}
}

// TestGRPCClient_MarkBalancerFailed_RoundRobin 验证 markBalancerFailed 对 RoundRobin 无效
// （RoundRobin 无 MarkFailed 方法，每次 Next 自动轮询）。
func TestGRPCClient_MarkBalancerFailed_RoundRobin(t *testing.T) {
	cli, err := NewGRPCClient([]string{"127.0.0.1:9090"}, "", "", "", 9090)
	if err != nil {
		t.Fatal(err)
	}
	svcs := []discovery.Service{
		{ID: "cp1", Addr: "cp1", Port: 9090, Healthy: true},
		{ID: "cp2", Addr: "cp2", Port: 9090, Healthy: true},
	}
	rr := discovery.NewRoundRobin(svcs)
	cli.SetBalancer(rr)

	// markBalancerFailed 不应 panic（RoundRobin 无 MarkFailed，类型断言失败，静默跳过）
	cli.markBalancerFailed() // 不应 panic
}
