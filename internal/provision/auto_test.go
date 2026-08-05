package provision

import (
	"context"
	"strings"
	"testing"

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

// TestAutoProvision_ProductionRequiresHTTPS 验证 task 93：
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
