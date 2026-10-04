// egress_test.go SSRF 判定搬迁的等价性门禁（2026-10-04）。
//
// 背景：SSRF 防护从 internal/controlplane 下沉到 internal/egress 以便 notify
// 复用。搬迁安全代码的最大风险是"顺手简化"导致某条地址判定口径漂移——
// 而 SSRF 防护对此零容忍：一条判定的放宽就是一条可直接利用的绕过。
//
// 本测试的作用是**钉住判定表**：把搬迁前的行为逐条固化成用例，任何后续改动
// （含"看起来等价"的简化）只要改变其中一条即判红。
package egress

import (
	"net"
	"net/http"
	"testing"
	"time"
)

// defaultTestTimeout 是 NewClient 接线测试用的超时值（非零即可，语义在断言里）。
const defaultTestTimeout = 7 * time.Second

// TestPrivateIPTable 钉住 isPrivateIP 的完整判定表。
// 覆盖搬迁自 controlplane/server_security.go 的全部分支。
func TestPrivateIPTable(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
		why  string
	}{
		// IPv4 环回
		{"127.0.0.1", true, "环回"},
		{"127.255.255.254", true, "环回整段"},
		// IPv4 私网 A/B/C
		{"10.0.0.1", true, "A 类私网"},
		{"10.255.255.255", true, "A 类私网边界"},
		{"172.16.0.1", true, "B 类下界"},
		{"172.31.255.254", true, "B 类上界"},
		{"172.15.0.1", false, "B 类下界之外（易错边界）"},
		{"172.32.0.1", false, "B 类上界之外（易错边界）"},
		{"192.168.0.1", true, "C 类私网"},
		{"192.168.255.254", true, "C 类私网边界"},
		// 链路本地 + 云元数据
		{"169.254.169.254", true, "云元数据头号目标"},
		{"169.254.0.1", true, "链路本地整段"},
		{"169.253.0.1", false, "链路本地之外（易错边界）"},
		// 0.0.0.0/8 —— 本实现的增强点：原仅拒 0.0.0.0 单地址
		{"0.0.0.0", true, "未指定地址"},
		{"0.1.2.3", true, "0.0.0.0/8 整段（防 0.x.x.x 绕过访问本机网络栈）"},
		// 公网样例
		{"8.8.8.8", false, "公网"},
		{"1.1.1.1", false, "公网"},
		{"100.64.0.1", false, "CGNAT 段：原实现不判私网，保持一致"},
		// IPv6
		{"::1", true, "IPv6 环回"},
		{"fe80::1", true, "IPv6 link-local"},
		{"fc00::1", true, "IPv6 ULA 下界"},
		{"fdff::1", true, "IPv6 ULA 上界"},
		{"2001:4860:4860::8888", false, "公网 IPv6"},
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if ip == nil {
			t.Fatalf("测试用例 IP 非法：%s", c.ip)
		}
		if got := isPrivateIP(ip); got != c.want {
			t.Errorf("isPrivateIP(%s) = %v，期望 %v（%s）", c.ip, got, c.want, c.why)
		}
	}
}

// TestIsRestrictedEvenWhenAllowedTable 钉住"任何开关都不放行"段的判定表。
//
// 关键契约：allowPrivate=true 也**不放行** 169.254/16 与 0.0.0.0/8。
// 若这条被放宽，--webhook-allow-private=true 就等于给云元数据端点开了后门。
func TestIsRestrictedEvenWhenAllowedTable(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
		why  string
	}{
		{"169.254.169.254", true, "云元数据：恒拒，allowPrivate 也不例外"},
		{"169.254.0.1", true, "链路本地整段恒拒"},
		{"0.0.0.0", true, "未指定地址恒拒"},
		{"0.9.9.9", true, "0.0.0.0/8 恒拒"},
		{"10.0.0.1", false, "私网不属本段：由 allowPrivate 决定"},
		{"172.16.0.1", false, "私网不属本段"},
		{"192.168.1.1", false, "私网不属本段"},
		{"127.0.0.1", false, "环回不属本段：由 allowPrivate 决定"},
		{"8.8.8.8", false, "公网"},
		{"::1", false, "IPv6 环回不属本段"},
		{"fe80::1", true, "IPv6 link-local 恒拒"},
		{"2001:4860:4860::8888", false, "公网 IPv6"},
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if ip == nil {
			t.Fatalf("测试用例 IP 非法：%s", c.ip)
		}
		if got := IsRestrictedEvenWhenAllowed(ip); got != c.want {
			t.Errorf("IsRestrictedEvenWhenAllowed(%s) = %v，期望 %v（%s）", c.ip, got, c.want, c.why)
		}
	}
}

// TestIPRejectionTwoModes 钉住 IPRejection 在两种 allowPrivate 下的行为。
func TestIPRejectionTwoModes(t *testing.T) {
	cases := []struct {
		ip           string
		wantPrivate  bool // allowPrivate=false 时是否拒绝
		wantRestrict bool // allowPrivate=true 时是否仍拒绝
	}{
		{"8.8.8.8", false, false},
		{"10.1.2.3", true, false}, // 内网部署场景：开关打开后放行
		{"192.168.1.1", true, false},
		{"127.0.0.1", true, false},
		{"169.254.169.254", true, true}, // 恒拒
		{"0.0.0.0", true, true},         // 恒拒
		{"fe80::1", true, true},         // 恒拒
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if ip == nil {
			t.Fatalf("测试用例 IP 非法：%s", c.ip)
		}
		if got := IPRejection(ip, false) != ""; got != c.wantPrivate {
			t.Errorf("IPRejection(%s, allowPrivate=false) 拒绝=%v，期望 %v", c.ip, got, c.wantPrivate)
		}
		if got := IPRejection(ip, true) != ""; got != c.wantRestrict {
			t.Errorf("IPRejection(%s, allowPrivate=true) 拒绝=%v，期望 %v", c.ip, got, c.wantRestrict)
		}
	}
}

// TestValidateURLLiteralHost 用 IP 字面量（不走 DNS，离线可跑）钉住协议白名单、
// 空主机、以及 allowPrivate 对元数据段的收窄语义。
func TestValidateURLLiteralHost(t *testing.T) {
	cases := []struct {
		name         string
		url          string
		allowPrivate bool
		wantErr      bool
	}{
		{"公网字面量放行", "http://8.8.8.8/hook", false, false},
		{"公网字面量放行-https", "https://1.1.1.1/hook", false, false},
		{"私网默认拒", "http://10.0.0.1/hook", false, true},
		{"私网开关打开放行", "http://10.0.0.1/hook", true, false},
		{"云元数据默认拒", "http://169.254.169.254/latest/meta-data/", false, true},
		{"云元数据开关打开仍拒", "http://169.254.169.254/latest/meta-data/", true, true},
		{"0.0.0.0 开关打开仍拒", "http://0.0.0.0/hook", true, true},
		{"环回默认拒", "http://127.0.0.1:8080/hook", false, true},
		{"环回开关打开放行", "http://127.0.0.1:8080/hook", true, false},
		{"file 协议拒", "file:///etc/passwd", false, true},
		{"gopher 协议拒", "gopher://8.8.8.8:70/x", false, true},
		{"dict 协议拒", "dict://8.8.8.8:11211/x", false, true},
		{"ftp 协议拒", "ftp://8.8.8.8/x", false, true},
		{"空主机拒", "http:///hook", false, true},
		{"IPv6 公网放行", "http://[2001:4860:4860::8888]/hook", false, false},
		{"IPv6 环回默认拒", "http://[::1]/hook", false, true},
		{"IPv6 环回开关放行", "http://[::1]/hook", true, false},
		{"IPv6 link-local 开关打开仍拒", "http://[fe80::1]/hook", true, true},
	}
	for _, c := range cases {
		err := ValidateURL(c.url, c.allowPrivate)
		if (err != nil) != c.wantErr {
			t.Errorf("%s：ValidateURL(%q, %v) err=%v，期望有错=%v", c.name, c.url, c.allowPrivate, err, c.wantErr)
		}
	}
}

// TestNewClientWiring 钉住 NewClient 的关键防护配置。
//
// 这些是"看着对但实际没生效"的高发点：Proxy 用了环境代理、CheckRedirect 为 nil
// （即默认跟随重定向）、Timeout 为 0（即无超时）。任一回归都会让三层防护失效，
// 而功能测试未必能察觉——所以显式断言字段值。
func TestNewClientWiring(t *testing.T) {
	c := NewClient(defaultTestTimeout, false)
	if c.Timeout != defaultTestTimeout {
		t.Errorf("Client.Timeout = %v，期望 %v（0 表示无超时，goroutine 可能永久挂起）", c.Timeout, defaultTestTimeout)
	}
	if c.CheckRedirect == nil {
		t.Error("Client.CheckRedirect 为 nil：默认跟随最多 10 跳重定向，每跳都需复检 SSRF")
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport 类型为 %T，期望 *http.Transport", c.Transport)
	}
	if tr.Proxy != nil {
		t.Error("Transport.Proxy 非 nil：启用环境代理后 DialContext 拿到的是代理地址，IP 级校验形同虚设")
	}
	if tr.DialContext == nil {
		t.Error("Transport.DialContext 为 nil：抗 DNS rebinding 的建连时逐 IP 复检失效")
	}
	if tr.TLSHandshakeTimeout == 0 {
		t.Error("Transport.TLSHandshakeTimeout 为 0")
	}
}
