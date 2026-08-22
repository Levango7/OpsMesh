package provision

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"opsmesh/internal/config"
	"opsmesh/internal/proto"
)

// noopDeps 返回注入哑依赖（UpsertDevice/Provision 均空实现）。
func noopDeps() Deps {
	return Deps{
		UpsertDevice: func(d *proto.DeviceInfo) {},
		Provision:    func(deviceID, host, tenantID string) (string, string, error) { return "tok", "", nil },
	}
}

// TestAutoProvision_ProductionRequiresHTTPS 验证 ：
// 生产模式下 advertise 非 HTTPS 时整轮纳管被拒绝（防 agent 二进制明文下载被篡改）。
func TestAutoProvision_ProductionRequiresHTTPS(t *testing.T) {
	cfg := &config.Config{Production: true, AdvertiseAddr: "http://10.30.0.1:8080", HTTPPort: 8080}
	_, err := AutoProvision(context.Background(), noopDeps(), cfg, []string{"192.0.2.0/30"}, "t1")
	if err == nil {
		t.Fatal("生产模式 + http advertise 应被拒绝")
	}
	if !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("错误信息应提及 HTTPS 要求: %v", err)
	}
}

// TestAutoProvision_ProductionEmptyAdvertiseRejected 验证 advertise 缺省回退本机回环（http）时同样被生产模式拒绝。
func TestAutoProvision_ProductionEmptyAdvertiseRejected(t *testing.T) {
	cfg := &config.Config{Production: true, HTTPPort: 8080}
	_, err := AutoProvision(context.Background(), noopDeps(), cfg, []string{"192.0.2.0/30"}, "t1")
	if err == nil {
		t.Fatal("生产模式 + 缺省 advertise（回退 http 回环）应被拒绝")
	}
}

// TestAutoProvision_ProductionHTTPSAllowed 验证生产模式 + HTTPS advertise 不被守卫拒绝
// （扫描本身用 RFC 5737 TEST-NET 网段，不会命中真实主机；扫描失败仅记 Failures 不报错）。
func TestAutoProvision_ProductionHTTPSAllowed(t *testing.T) {
	cfg := &config.Config{Production: true, AdvertiseAddr: "https://opsmesh.example.com:8443"}
	sum, err := AutoProvision(context.Background(), noopDeps(), cfg, []string{"192.0.2.0/30"}, "t1")
	if err != nil {
		t.Fatalf("HTTPS advertise 不应被守卫拒绝: %v", err)
	}
	if sum == nil {
		t.Fatal("summary 不应为 nil")
	}
}

// ============================================================
// AutoProvision 输入校验测试
// ============================================================

// TestAutoProvision_NoCIDR 验证无待扫描网段时返回错误。
func TestAutoProvision_NoCIDR(t *testing.T) {
	cfg := &config.Config{AdvertiseAddr: "https://opsmesh.example.com:8443"}
	_, err := AutoProvision(context.Background(), noopDeps(), cfg, nil, "t1")
	if err == nil {
		t.Fatal("无 CIDR 应返回错误")
	}
	if !strings.Contains(err.Error(), "无待扫描网段") {
		t.Fatalf("错误信息应提及无待扫描网段: %v", err)
	}
}

// TestAutoProvision_EmptyCIDRList 验证空 CIDR 列表返回错误。
func TestAutoProvision_EmptyCIDRList(t *testing.T) {
	cfg := &config.Config{AdvertiseAddr: "https://opsmesh.example.com:8443"}
	_, err := AutoProvision(context.Background(), noopDeps(), cfg, []string{}, "t1")
	if err == nil {
		t.Fatal("空 CIDR 列表应返回错误")
	}
}

// TestAutoProvision_NilUpsertDevice 验证 UpsertDevice 未注入时返回错误。
func TestAutoProvision_NilUpsertDevice(t *testing.T) {
	cfg := &config.Config{AdvertiseAddr: "https://opsmesh.example.com:8443"}
	deps := Deps{
		UpsertDevice: nil,
		Provision:    func(deviceID, host, tenantID string) (string, string, error) { return "tok", "", nil },
	}
	_, err := AutoProvision(context.Background(), deps, cfg, []string{"192.0.2.0/30"}, "t1")
	if err == nil {
		t.Fatal("UpsertDevice 未注入应返回错误")
	}
	if !strings.Contains(err.Error(), "依赖未注入") {
		t.Fatalf("错误信息应提及依赖未注入: %v", err)
	}
}

// TestAutoProvision_NilProvision 验证 Provision 未注入时返回错误。
func TestAutoProvision_NilProvision(t *testing.T) {
	cfg := &config.Config{AdvertiseAddr: "https://opsmesh.example.com:8443"}
	deps := Deps{
		UpsertDevice: func(d *proto.DeviceInfo) {},
		Provision:    nil,
	}
	_, err := AutoProvision(context.Background(), deps, cfg, []string{"192.0.2.0/30"}, "t1")
	if err == nil {
		t.Fatal("Provision 未注入应返回错误")
	}
	if !strings.Contains(err.Error(), "依赖未注入") {
		t.Fatalf("错误信息应提及依赖未注入: %v", err)
	}
}

// ============================================================
// AutoProvision 非 HTTPS 警告测试
// ============================================================

// TestAutoProvision_NonHTTPSWarning 验证非生产模式下非 HTTPS advertise 打印警告但不报错。
func TestAutoProvision_NonHTTPSWarning(t *testing.T) {
	cfg := &config.Config{AdvertiseAddr: "http://10.30.0.1:8080"}
	sum, err := AutoProvision(context.Background(), noopDeps(), cfg, []string{"192.0.2.0/30"}, "t1")
	if err != nil {
		t.Fatalf("非生产模式 + http advertise 不应报错: %v", err)
	}
	if sum == nil {
		t.Fatal("summary 不应为 nil")
	}
}

// TestAutoProvision_EmptyAdvertiseNonProduction 验证非生产模式下 advertise 缺省回退本机回环不报错。
func TestAutoProvision_EmptyAdvertiseNonProduction(t *testing.T) {
	cfg := &config.Config{HTTPPort: 8080}
	sum, err := AutoProvision(context.Background(), noopDeps(), cfg, []string{"192.0.2.0/30"}, "t1")
	if err != nil {
		t.Fatalf("非生产模式 + 缺省 advertise 不应报错: %v", err)
	}
	if sum == nil {
		t.Fatal("summary 不应为 nil")
	}
}

// ============================================================
// AutoProvision 扫描失败测试
// ============================================================

// TestAutoProvision_InvalidCIDR 验证无效 CIDR 扫描失败记录到 Failures 但不返回错误。
func TestAutoProvision_InvalidCIDR(t *testing.T) {
	cfg := &config.Config{AdvertiseAddr: "https://opsmesh.example.com:8443"}
	sum, err := AutoProvision(context.Background(), noopDeps(), cfg, []string{"invalid-cidr"}, "t1")
	if err != nil {
		t.Fatalf("扫描失败不应返回错误（仅记 Failures）: %v", err)
	}
	if sum == nil {
		t.Fatal("summary 不应为 nil")
	}
	if len(sum.Failures) == 0 {
		t.Fatal("无效 CIDR 应记录到 Failures")
	}
	if !strings.Contains(sum.Failures[0], "scan") {
		t.Fatalf("Failures 应包含 scan 错误: %s", sum.Failures[0])
	}
}

// TestAutoProvision_MixedCIDRs 验证混合有效/无效 CIDR：无效的记 Failures，有效的继续处理。
func TestAutoProvision_MixedCIDRs(t *testing.T) {
	cfg := &config.Config{AdvertiseAddr: "https://opsmesh.example.com:8443"}
	sum, err := AutoProvision(context.Background(), noopDeps(), cfg, []string{"invalid-cidr", "192.0.2.0/30"}, "t1")
	if err != nil {
		t.Fatalf("混合 CIDR 不应返回错误: %v", err)
	}
	if sum == nil {
		t.Fatal("summary 不应为 nil")
	}
	// 至少有一条失败记录（来自 invalid-cidr）
	foundScanFailure := false
	for _, f := range sum.Failures {
		if strings.Contains(f, "scan") {
			foundScanFailure = true
			break
		}
	}
	if !foundScanFailure {
		t.Fatal("应记录 invalid-cidr 的扫描失败")
	}
}

// ============================================================
// AutoProvision 设备登记与 Provision 流程测试
// 使用 127.0.0.1 + TCP 监听器让 Sweep 探测到存活主机
// ============================================================

// startTCPEcho 在 127.0.0.1:port 启动 TCP 监听器，返回清理函数。
// Sweep 探测端口连通即视为存活，不需要真正的 SSH 服务。
func startTCPEcho(t *testing.T, port int) func() {
	t.Helper()
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("listen tcp %d: %v", port, err)
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

// TestAutoProvision_DeviceRegistration 验证存活主机被登记为候选设备（UpsertDevice 被调用）。
func TestAutoProvision_DeviceRegistration(t *testing.T) {
	// 在 127.0.0.1:9100 启动 TCP 监听器，让 Sweep 探测到存活
	cleanup := startTCPEcho(t, 9100)
	defer cleanup()

	cfg := &config.Config{AdvertiseAddr: "https://opsmesh.example.com:8443"}

	var mu sync.Mutex
	var upserted []*proto.DeviceInfo
	deps := Deps{
		UpsertDevice: func(d *proto.DeviceInfo) {
			mu.Lock()
			defer mu.Unlock()
			upserted = append(upserted, d)
		},
		Provision: func(deviceID, host, tenantID string) (string, string, error) {
			return "tok-" + deviceID, "", nil
		},
	}

	// 127.0.0.1/32 只扫描 127.0.0.1
	sum, err := AutoProvision(context.Background(), deps, cfg, []string{"127.0.0.1/32"}, "tenant-a")
	if err != nil {
		t.Fatalf("设备登记不应报错: %v", err)
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

	mu.Lock()
	defer mu.Unlock()
	if len(upserted) != 1 {
		t.Fatalf("UpsertDevice 应被调用 1 次，got %d", len(upserted))
	}
	dev := upserted[0]
	if dev.DeviceID != "dev-127.0.0.1" {
		t.Fatalf("DeviceID 应为 dev-127.0.0.1，got %s", dev.DeviceID)
	}
	if dev.State != "discovered" {
		t.Fatalf("State 应为 discovered，got %s", dev.State)
	}
	if dev.Managed {
		t.Fatal("Managed 应为 false")
	}
	if dev.TenantID != "tenant-a" {
		t.Fatalf("TenantID 应为 tenant-a，got %s", dev.TenantID)
	}
	if dev.IP != "127.0.0.1" {
		t.Fatalf("IP 应为 127.0.0.1，got %s", dev.IP)
	}
}

// TestAutoProvision_ProvisionFailure 验证 Provision 失败记录到 Failures 但不影响设备登记。
func TestAutoProvision_ProvisionFailure(t *testing.T) {
	cleanup := startTCPEcho(t, 9100)
	defer cleanup()

	cfg := &config.Config{AdvertiseAddr: "https://opsmesh.example.com:8443"}

	deps := Deps{
		UpsertDevice: func(d *proto.DeviceInfo) {},
		Provision: func(deviceID, host, tenantID string) (string, string, error) {
			return "", "", fmt.Errorf("token signing failed")
		},
	}

	sum, err := AutoProvision(context.Background(), deps, cfg, []string{"127.0.0.1/32"}, "t1")
	if err != nil {
		t.Fatalf("Provision 失败不应返回错误（仅记 Failures）: %v", err)
	}
	if sum.Registered != 1 {
		t.Fatalf("设备应已登记（Registered=1），got %d", sum.Registered)
	}
	if sum.Provisioned != 0 {
		t.Fatalf("Provision 失败时 Provisioned 应为 0，got %d", sum.Provisioned)
	}
	if len(sum.Failures) == 0 {
		t.Fatal("Provision 失败应记录到 Failures")
	}
	if !strings.Contains(sum.Failures[0], "provision") {
		t.Fatalf("Failures 应包含 provision 错误: %s", sum.Failures[0])
	}
}

// TestAutoProvision_NoSSHKey_OnlyToken 验证未配置 SSH 私钥时仅签发 token，不触发 SSH 推送。
func TestAutoProvision_NoSSHKey_OnlyToken(t *testing.T) {
	cleanup := startTCPEcho(t, 9100)
	defer cleanup()

	// 不配置 ProvisionSSHKey
	cfg := &config.Config{AdvertiseAddr: "https://opsmesh.example.com:8443"}

	sum, err := AutoProvision(context.Background(), noopDeps(), cfg, []string{"127.0.0.1/32"}, "t1")
	if err != nil {
		t.Fatalf("未配置 SSH 私钥不应报错: %v", err)
	}
	if sum.Provisioned != 1 {
		t.Fatalf("应签发 token（Provisioned=1），got %d", sum.Provisioned)
	}
	if sum.SSHPushed != 0 {
		t.Fatalf("未配置 SSH 私钥时 SSHPushed 应为 0，got %d", sum.SSHPushed)
	}
}

// TestAutoProvision_ProductionNoKnownHostsRejected 验证生产模式 + 已配置 SSH 私钥但未配置 known_hosts 时
// 跳过 SSH 推送并记录失败（M12 MITM 防护）。
func TestAutoProvision_ProductionNoKnownHostsRejected(t *testing.T) {
	cleanup := startTCPEcho(t, 9100)
	defer cleanup()

	// 生产模式 + HTTPS advertise + SSH 私钥但无 known_hosts
	cfg := &config.Config{
		Production:       true,
		AdvertiseAddr:    "https://opsmesh.example.com:8443",
		ProvisionSSHKey:  "/tmp/fake-key", // 不需要真实文件，因为会在 known_hosts 检查时跳过
		ProvisionSSHUser: "root",
	}

	sum, err := AutoProvision(context.Background(), noopDeps(), cfg, []string{"127.0.0.1/32"}, "t1")
	if err != nil {
		t.Fatalf("不应返回错误（失败仅记 Failures）: %v", err)
	}
	if sum.Provisioned != 1 {
		t.Fatalf("应已签发 token（Provisioned=1），got %d", sum.Provisioned)
	}
	if sum.SSHPushed != 0 {
		t.Fatalf("生产模式无 known_hosts 时 SSHPushed 应为 0，got %d", sum.SSHPushed)
	}
	// 应记录失败
	foundMITMFailure := false
	for _, f := range sum.Failures {
		if strings.Contains(f, "InsecureIgnoreHostKey") || strings.Contains(f, "known_hosts") {
			foundMITMFailure = true
			break
		}
	}
	if !foundMITMFailure {
		t.Fatalf("应记录生产模式拒绝 InsecureIgnoreHostKey 的失败，Failures: %v", sum.Failures)
	}
}

// TestAutoProvision_SSHPushFailureRecorded 验证非生产模式下 SSH 推送失败被记录到 Failures。
// 配置 SSH 私钥但指向不存在的文件，PushAndExec 会失败并记录。
func TestAutoProvision_SSHPushFailureRecorded(t *testing.T) {
	cleanup := startTCPEcho(t, 9100)
	defer cleanup()

	cfg := &config.Config{
		AdvertiseAddr:    "https://opsmesh.example.com:8443",
		ProvisionSSHKey:  "/nonexistent/id_rsa", // 私钥文件不存在，PushAndExec 会失败
		ProvisionSSHUser: "root",
	}

	sum, err := AutoProvision(context.Background(), noopDeps(), cfg, []string{"127.0.0.1/32"}, "t1")
	if err != nil {
		t.Fatalf("SSH 推送失败不应返回错误（仅记 Failures）: %v", err)
	}
	if sum.Provisioned != 1 {
		t.Fatalf("应已签发 token（Provisioned=1），got %d", sum.Provisioned)
	}
	// SSH 推送是异步的，等待失败记录出现
	// 由于 SSH 推送在 goroutine 中执行，需要等待
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(sum.Failures) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(sum.Failures) == 0 {
		t.Fatal("SSH 推送失败应记录到 Failures")
	}
	foundSSHFailure := false
	for _, f := range sum.Failures {
		if strings.Contains(f, "ssh") {
			foundSSHFailure = true
			break
		}
	}
	if !foundSSHFailure {
		t.Fatalf("Failures 应包含 ssh 推送失败: %v", sum.Failures)
	}
}

// TestAutoProvision_EmptyTenantID 验证 tenantID 为空时视为单租户（开发模式）。
func TestAutoProvision_EmptyTenantID(t *testing.T) {
	cleanup := startTCPEcho(t, 9100)
	defer cleanup()

	cfg := &config.Config{AdvertiseAddr: "https://opsmesh.example.com:8443"}

	var mu sync.Mutex
	var upserted []*proto.DeviceInfo
	deps := Deps{
		UpsertDevice: func(d *proto.DeviceInfo) {
			mu.Lock()
			defer mu.Unlock()
			upserted = append(upserted, d)
		},
		Provision: func(deviceID, host, tenantID string) (string, string, error) {
			return "tok", "", nil
		},
	}

	sum, err := AutoProvision(context.Background(), deps, cfg, []string{"127.0.0.1/32"}, "")
	if err != nil {
		t.Fatalf("空 tenantID 不应报错: %v", err)
	}
	if sum.Registered != 1 {
		t.Fatalf("Registered 应为 1，got %d", sum.Registered)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(upserted) != 1 {
		t.Fatalf("UpsertDevice 应被调用 1 次，got %d", len(upserted))
	}
	if upserted[0].TenantID != "" {
		t.Fatalf("TenantID 应为空，got %s", upserted[0].TenantID)
	}
}

// TestAutoProvision_MultipleAliveHosts 验证多个存活主机都被登记。
// 在 127.0.0.1:9100 和 127.0.0.1:22 启动监听器，使用 127.0.0.0/30 扫描 127.0.0.1 和 127.0.0.2。
func TestAutoProvision_MultipleAliveHosts(t *testing.T) {
	cleanup1 := startTCPEcho(t, 9100)
	defer cleanup1()

	cfg := &config.Config{AdvertiseAddr: "https://opsmesh.example.com:8443"}

	var mu sync.Mutex
	upsertedCount := 0
	deps := Deps{
		UpsertDevice: func(d *proto.DeviceInfo) {
			mu.Lock()
			defer mu.Unlock()
			upsertedCount++
		},
		Provision: func(deviceID, host, tenantID string) (string, string, error) {
			return "tok", "", nil
		},
	}

	// 127.0.0.0/30 扫描 127.0.0.1 和 127.0.0.2（排除网络 127.0.0.0 和广播 127.0.0.3）
	// 只有 127.0.0.1 有监听器，127.0.0.2 不可达
	sum, err := AutoProvision(context.Background(), deps, cfg, []string{"127.0.0.0/30"}, "t1")
	if err != nil {
		t.Fatalf("多主机扫描不应报错: %v", err)
	}
	if sum.Scanned != 1 {
		t.Fatalf("Scanned 应为 1（仅 127.0.0.1 存活），got %d", sum.Scanned)
	}
	mu.Lock()
	defer mu.Unlock()
	if upsertedCount != 1 {
		t.Fatalf("UpsertDevice 应被调用 1 次，got %d", upsertedCount)
	}
}
