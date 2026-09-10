// autoprovision_test.go — D3 自动纳管编排单测。
//
// 覆盖目标：
//   - 双闸第一闸：Enabled=false / cfg=nil 时 RunAutoProvision 拒绝
//   - CIDR 白名单校验（复用 D2 语义）
//   - 编排闭环：扫描存活 → dev-{ip} 入库 discovered → token 签发 → Summary 计数
//   - SSH 推送仅在配置 SSHKey 时发生（第二闸）
package service

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/device-svc/internal/store"
)

// startTCPEcho9100 在 127.0.0.1:9100 起监听器让 Sweep 探测到存活（同 pkg/provision 测试模式）。
func startTCPEcho9100(t *testing.T) func() {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:9100")
	if err != nil {
		t.Fatalf("listen 9100: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	return func() { ln.Close() }
}

// newAutoProvisionTestSvc 构造启用自动纳管的 Service（SSHKey 不配=仅签发 token）。
func newAutoProvisionTestSvc() (*Service, *store.MemoryStore) {
	st := store.NewMemoryStore()
	svc := NewService(st, st, st, st, st, nil)
	svc.SetAutoProvisionConfig(&AutoProvisionConfig{
		Enabled:           true,
		FallbackAdvertise: "http://127.0.0.1:8081",
	})
	return svc, st
}

// TestRunAutoProvision_DisabledByDefault 验证双闸第一闸：未启用时立即拒绝。
func TestRunAutoProvision_DisabledByDefault(t *testing.T) {
	st := store.NewMemoryStore()
	svc := NewService(st, st, st, st, st, nil)
	// 不注入 AutoProvisionConfig（默认关闭）
	_, err := svc.RunAutoProvision(context.Background(), []string{"127.0.0.1/32"}, "t1")
	if err == nil {
		t.Fatal("未启用自动纳管应拒绝")
	}
}

// TestRunAutoProvision_ExplicitFalseRejected 验证显式 Enabled=false 也拒绝。
func TestRunAutoProvision_ExplicitFalseRejected(t *testing.T) {
	st := store.NewMemoryStore()
	svc := NewService(st, st, st, st, st, nil)
	svc.SetAutoProvisionConfig(&AutoProvisionConfig{Enabled: false})
	_, err := svc.RunAutoProvision(context.Background(), []string{"127.0.0.1/32"}, "t1")
	if err == nil {
		t.Fatal("Enabled=false 应拒绝")
	}
}

// TestRunAutoProvision_CIDRWhitelist 验证白名单外 CIDR 被拒绝。
func TestRunAutoProvision_CIDRWhitelist(t *testing.T) {
	st := store.NewMemoryStore()
	svc := NewService(st, st, st, st, st, nil)
	svc.SetAutoProvisionConfig(&AutoProvisionConfig{
		Enabled:           true,
		FallbackAdvertise: "http://127.0.0.1:8081",
	})
	// 白名单只允许 192.168.1.0/24，目标 10.0.0.0/24 应被拒
	svc.SetDiscoverConfig(&DiscoverConfig{CIDRWhitelist: "192.168.1.0/24", Timeout: 60 * time.Second})
	_, err := svc.RunAutoProvision(context.Background(), []string{"10.0.0.0/24"}, "t1")
	if err == nil {
		t.Fatal("白名单外 CIDR 应被拒绝")
	}
}

// TestRunAutoProvision_FullLoop 验证编排闭环：扫描存活 → 设备入库 → token 签发 → Summary 计数。
func TestRunAutoProvision_FullLoop(t *testing.T) {
	cleanup := startTCPEcho9100(t)
	defer cleanup()

	svc, st := newAutoProvisionTestSvc()

	sum, err := svc.RunAutoProvision(context.Background(), []string{"127.0.0.1/32"}, "tenant-a")
	if err != nil {
		t.Fatalf("编排闭环不应报错: %v", err)
	}
	if sum.Scanned != 1 {
		t.Fatalf("Scanned 应为 1，got %d", sum.Scanned)
	}
	if sum.Registered != 1 {
		t.Fatalf("Registered 应为 1，got %d", sum.Registered)
	}
	if sum.Provisioned != 1 {
		t.Fatalf("Provisioned 应为 1，got %d", sum.Provisioned)
	}
	if sum.SSHPushed != 0 {
		t.Fatalf("未配置 SSHKey 时 SSHPushed 应为 0，got %d", sum.SSHPushed)
	}
	// 设备入库验证
	dev := st.Device("dev-127.0.0.1")
	if dev == nil {
		t.Fatal("dev-127.0.0.1 应已入库")
	}
	if dev.Status != "discovered" {
		t.Fatalf("设备状态应为 discovered，got %s", dev.Status)
	}
	if dev.TenantID != "tenant-a" {
		t.Fatalf("TenantID 应为 tenant-a，got %s", dev.TenantID)
	}
}

// TestRunAutoProvision_InvalidCIDR 验证无效 CIDR 记 Failures 不报错。
func TestRunAutoProvision_InvalidCIDR(t *testing.T) {
	svc, _ := newAutoProvisionTestSvc()

	sum, err := svc.RunAutoProvision(context.Background(), []string{"invalid-cidr"}, "t1")
	if err != nil {
		t.Fatalf("无效 CIDR 不应返回错误（仅记 Failures）: %v", err)
	}
	if len(sum.Failures) == 0 {
		t.Fatal("无效 CIDR 应记录到 Failures")
	}
}

// TestRunAutoProvision_InvalidAdvertiseRejected 验证 fallback advertise 格式非法时整轮拒绝（加固项）。
func TestRunAutoProvision_InvalidAdvertiseRejected(t *testing.T) {
	st := store.NewMemoryStore()
	svc := NewService(st, st, st, st, st, nil)
	svc.SetAutoProvisionConfig(&AutoProvisionConfig{
		Enabled:           true,
		FallbackAdvertise: "http://127.0.0.1:8081;rm -rf /", // 含 shell 元字符
	})
	_, err := svc.RunAutoProvision(context.Background(), []string{"127.0.0.1/32"}, "t1")
	if err == nil {
		t.Fatal("advertise 含 shell 元字符应整轮拒绝")
	}
}
