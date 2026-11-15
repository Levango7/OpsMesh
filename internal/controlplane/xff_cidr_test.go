package controlplane

import (
	"net"
	"net/http/httptest"
	"testing"
)

// ADR-2026-11-15 §ADR-1：可信反代 CIDR 的 clientIP 解析守卫。

func mustNets(t *testing.T, cidrs ...string) []*net.IPNet {
	t.Helper()
	nets, err := parseTrustedProxies(cidrs)
	if err != nil {
		t.Fatalf("parseTrustedProxies: %v", err)
	}
	return nets
}

func TestClientIPTrusted_UntrustedPeerIgnoresXFF(t *testing.T) {
	trusted := mustNets(t, "10.0.0.0/8")
	req := httptest.NewRequest("GET", "/api/v1/tasks", nil)
	req.RemoteAddr = "203.0.113.9:5555" // 直连对端不在可信段
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := clientIPTrusted(req, true, trusted); got != "203.0.113.9" {
		t.Fatalf("非可信对端的 XFF 必须被忽略，得到 %q", got)
	}
}

func TestClientIPTrusted_TrustedPeerPicksRightmostUntrusted(t *testing.T) {
	trusted := mustNets(t, "10.0.0.0/8", "192.168.0.0/16")
	req := httptest.NewRequest("GET", "/api/v1/tasks", nil)
	req.RemoteAddr = "10.1.2.3:4444" // 可信 LB
	// 右起：10.1.2.3(可信) → 192.168.1.1(可信) → 203.0.113.7(客户端)
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 192.168.1.1, 10.1.2.3")
	if got := clientIPTrusted(req, true, trusted); got != "203.0.113.7" {
		t.Fatalf("应取最右侧非可信地址，得到 %q", got)
	}
}

func TestClientIPTrusted_AllTrustedFallsBackToLeftmost(t *testing.T) {
	trusted := mustNets(t, "10.0.0.0/8")
	req := httptest.NewRequest("GET", "/api/v1/tasks", nil)
	req.RemoteAddr = "10.9.9.9:1234"
	req.Header.Set("X-Forwarded-For", "10.1.1.1, 10.2.2.2")
	if got := clientIPTrusted(req, true, trusted); got != "10.1.1.1" {
		t.Fatalf("全部跳可信时应回退最左段，得到 %q", got)
	}
}

func TestClientIPTrusted_NoTrustProxyUsesRemoteAddr(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/tasks", nil)
	req.RemoteAddr = "198.51.100.5:9999"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := clientIPTrusted(req, false, mustNets(t, "10.0.0.0/8")); got != "198.51.100.5" {
		t.Fatalf("trustProxy=false 必须只用 RemoteAddr，得到 %q", got)
	}
}

func TestParseTrustedProxies_InvalidCIDRRejected(t *testing.T) {
	if _, err := parseTrustedProxies([]string{"10.0.0.0/8", "not-a-cidr"}); err == nil {
		t.Fatal("非法 CIDR 必须返回错误（启动校验依赖）")
	}
}
