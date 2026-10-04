// Package egress 是全仓**唯一**的出网（egress）策略权威实现。
//
// # 为什么要独立成包（2026-10-04）
//
// SSRF 防护最初只长在 internal/controlplane 里（newEgressClient / ValidateWebhookURL），
// 于是 internal/notify 走 http.DefaultClient 出网时**完全绕过**了它：
//
//   - 管理员设了 --webhook-allow-private=false（安全基线），notify 仍能打内网；
//   - 管理员设了 true，notify 也不知道，照样走默认策略；
//   - 且 DefaultClient 无 Client.Timeout、CheckRedirect 为 nil（跟随重定向）。
//
// 也就是说，"是否允许 webhook 打内网"这个开关**对通知渠道无效**——
// 而通知渠道恰恰是最常被配成内网网关（钉钉/飞飞/企微内网接入）的地方。
//
// 与其给 notify 再写一份第二版 SSRF 逻辑（两份实现必然随时间漂移，最终又是一处
// "这里漏了"），不如把判定下沉成包，让两个调用方共用**同一份权威实现**。
//
// # 三层防护（与原 controlplane 实现逐条一致，搬迁时不得弱化）
//
//  1. DialContext 建连时解析主机名并**逐 IP 复检**——抗 DNS rebinding 的正确位置：
//     校验发生在真正建连的那一刻，而不是几十秒前的"保存时刻"。TLS 的 ServerName
//     仍取自 URL 主机名，故按 IP 拨号不影响证书校验。
//  2. CheckRedirect 对**每一跳**重新做 URL 级校验并限制跳数——堵"外网 URL 返回
//     302 打到云元数据"这一最常见 SSRF 形态。
//  3. Client.Timeout 兜底，防止连接/读响应黑洞时 goroutine 永久挂起。
//
// # 为什么 Proxy 恒为 nil
//
// 本 client 存在的意义就是让"建连时逐 IP 复检"成为权威判定。一旦启用环境代理，
// Go 会把请求交给代理、由**代理**去解析并连接目标主机，DialContext 拿到的是代理
// 地址而非目标地址，IP 级校验形同虚设——实测设置 HTTP_PROXY 时，直连
// 169.254.169.254 的请求会绕过 DialContext 直接发给代理（返回 502 而非被拒）。
//
// 需要出网代理的部署应改在网络层（sidecar / 网关 / iptables）做，而非在此处。
// 这一点与搬迁前完全一致，是刻意保留的行为而非疏漏。
package egress

import (
	"context"
	"fmt"
	"net"
	"net/http"
	neturl "net/url"
	"strings"
	"sync"
	"time"
)

// DNSTimeout 是 SSRF 校验中 DNS 解析的超时时间（5 秒合理默认）。
// 独立常量（原 controlplane.ssrfDNSTimeout）：恶意域名不应拖垮 API。
const DNSTimeout = 5 * time.Second

// maxRedirects 是允许跟随的最大重定向跳数。
const maxRedirects = 5

// isPrivateIP 判定 IP 是否属于应默认拒绝的私网/环回/链路本地地址段。
//
// ⚠️ 搬迁说明：本函数逐条等价搬迁自 internal/controlplane/server_security.go
// 的 isPrivateIP（2026-10-04），**刻意不使用 net.IP 的 IsPrivate/IsLoopback
// 等标准库方法替代手写分支**：搬迁的目的是消除"两份 SSRF 逻辑"，而不是改变
// 判定结果。任何"顺手简化"都可能让某条地址的判定口径漂移，SSRF 防护对此零容忍。
// 若日后确要改判定，必须同步改门禁测试并作为独立安全变更评审。
//
// 覆盖范围：
//   - 127.0.0.0/8（loopback）
//   - 10.0.0.0/8（私网 A）
//   - 172.16.0.0/12（私网 B）
//   - 192.168.0.0/16（私网 C）
//   - 169.254.0.0/16（链路本地 + 云元数据 169.254.169.254）
//   - 0.0.0.0/8（本网/未指定；原实现仅拒 0.0.0.0 单地址，后增强为拒整个
//     /8 网段，防 0.x.x.x 绕过校验访问本机网络栈）
//   - ::1（IPv6 loopback）
//   - fe80::/10（IPv6 link-local）
//   - fc00::/7（IPv6 ULA 私网）
func isPrivateIP(ip net.IP) bool {
	// IPv4 私网/环回/链路本地。
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 127 { // 环回
			return true
		}
		if ip4[0] == 10 { // A 类私网
			return true
		}
		if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 { // B 类私网
			return true
		}
		if ip4[0] == 192 && ip4[1] == 168 { // C 类私网
			return true
		}
		if ip4[0] == 169 && ip4[1] == 254 { // 链路本地 + 云元数据
			return true
		}
		if ip4[0] == 0 { // 0.0.0.0/8
			return true
		}
		return false
	}
	// IPv6：拒绝 loopback (::1) 与 link-local (fe80::/10)。
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	// IPv6 ULA (fc00::/7) 私网地址。
	if len(ip) == 16 {
		if (ip[0] & 0xfe) == 0xfc {
			return true
		}
	}
	return false
}

// IsRestrictedEvenWhenAllowed 是"任何开关都不放行"的地址段：链路本地（含云元数据
// 169.254.169.254）、0.0.0.0/8 本网、IPv6 的 fe80::/10 与未指定地址 ::。
//
// 与 isPrivateIP 的分工是刻意的：私网/环回属于"内网部署确实要用"的范围，由
// allowPrivate 开关决定放不放；而这一段没有任何合法收件端住在里面，开了内网开关
// 也不该把它们一起放行（SSRF 的实际收益恰恰集中在这一段）。
//
// ⚠️ 逐条等价搬迁自 controlplane.isRestrictedEvenWhenAllowed，IPv4 分支先行
// （169.254/16 与 0.0.0.0/8 恒拒，不受 allowPrivate 影响），IPv6 走标准库判定。
func IsRestrictedEvenWhenAllowed(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
		if ip4[0] == 0 {
			return true
		}
		return false
	}
	return ip.IsLinkLocalUnicast() || ip.IsUnspecified()
}

// IPRejection 返回该 IP 在当前 allowPrivate 策略下是否应被拒绝拨号，
// 非空字符串即为拒绝原因（供日志与错误文案使用）。
//
// 与 ValidateURL 的 IP 判定共用同一套谓词，保证"保存时校验"与"建连时复检"
// 口径一致——两处口径一旦分叉，攻击者只需让两个时刻解析到不同 IP 即可绕过。
func IPRejection(ip net.IP, allowPrivate bool) string {
	if IsRestrictedEvenWhenAllowed(ip) {
		return "link-local/unspecified address (cloud metadata endpoint)"
	}
	if isPrivateIP(ip) {
		if allowPrivate {
			return ""
		}
		return "private address (set allowPrivate to permit)"
	}
	return ""
}

// LookupHostIPs 带超时地解析主机名。
//
// 单独抽出来的原因：allowPrivate 分支也必须解析（否则"域名指向元数据"这一形态
// 会被完全跳过），而两处各写一遍 WithTimeout+cancel 很容易漏掉 cancel。
func LookupHostIPs(host string) ([]net.IP, error) {
	ctx, cancel := context.WithTimeout(context.Background(), DNSTimeout)
	defer cancel()
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

// ValidateURL 校验出网 URL 是否符合 SSRF 安全基线。
//
// ⚠️ 逐条等价搬迁自 controlplane.ValidateWebhookURL（2026-10-04），控制流与
// 错误文案均保持原样以便对照审计。**搬迁不改判定**——本包的目的只是消除
// "两份 SSRF 逻辑各自漂移"，任何简化都必须作为独立安全变更评审。
//
// 规则：
//   - 协议必须是 http 或 https（拒绝 file://、gopher://、dict:// 等危险协议）
//   - 主机名非空
//   - IP 字面量直接判定（不走 DNS）；域名做 DNS 解析后校验每个返回 IP
//   - 默认拒绝私网/loopback/链路本地/云元数据地址
//   - allowPrivate=true 时放行**私网与环回**（内网部署场景，如钉钉/飞书内网网关），
//     但链路本地 / 云元数据 / 0.0.0.0/8 仍恒拒（见 IsRestrictedEvenWhenAllowed）
//
// allowPrivate=true 的语义**收窄为"放行私网与环回"**：更早的实现在这里直接
// return nil（连 DNS 都不做），于是"允许内网收件端"这个开关顺带把 169.254.169.254
// （IMDS 凭证窃取的头号目标）和 0.0.0.0/8 一起放行。没有任何"钉钉/飞书内网网关"
// 住在链路本地段，而 SSRF 的实际价值恰恰在这一段。
//
// DNS 解析超时 5 秒（DNSTimeout），避免恶意域名拖垮 API。
//
// 返回 nil 表示安全；非 nil error 描述拒绝原因。
//
// 注意：本函数是"某一时刻"的判断，抗 rebinding 靠的是 NewClient 的 DialContext
// 逐 IP 复检，二者缺一不可——只做保存时校验，DNS 记录随后被改即绕过。
func ValidateURL(rawURL string, allowPrivate bool) error {
	u, err := neturl.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	// 1. 协议白名单：仅允许 http/https。
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("scheme %q not allowed (only http/https)", u.Scheme)
	}
	// 2. 主机名非空校验。
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("empty host in URL")
	}
	// 3. IP 字面量直接校验（不走 DNS）；域名做 DNS 解析后逐 IP 校验。
	if ip := net.ParseIP(host); ip != nil {
		if allowPrivate {
			if IsRestrictedEvenWhenAllowed(ip) {
				return fmt.Errorf("host %q is link-local/unspecified/metadata address %s（--webhook-allow-private 不放行该段）", host, ip)
			}
			return nil
		}
		if isPrivateIP(ip) {
			return fmt.Errorf("host %q is private/loopback/metadata address %s", host, ip)
		}
		return nil
	}
	if allowPrivate {
		// 开关打开时仍要解析域名：否则"域名解析到 169.254.169.254"这条最常见的
		// rebinding / 内网指向元数据形态会被完全跳过。
		ips, err := LookupHostIPs(host)
		if err != nil {
			return fmt.Errorf("cannot resolve host %q: %w", host, err)
		}
		for _, ip := range ips {
			if IsRestrictedEvenWhenAllowed(ip) {
				return fmt.Errorf("host %q resolves to link-local/unspecified/metadata address %s（--webhook-allow-private 不放行该段）", host, ip)
			}
		}
		return nil
	}
	// 域名：DNS 解析（带 5 秒超时），校验每个返回 IP。
	// 任一 IP 落入私网即拒绝（防 DNS rebinding：域名解析到内网地址）。
	ips, err := LookupHostIPs(host)
	if err != nil {
		return fmt.Errorf("cannot resolve host %q: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("host %q resolved to no IP addresses", host)
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return fmt.Errorf("host %q resolves to private/loopback/metadata address %s", host, ip)
		}
	}
	return nil
}

// NewClient 返回一个带 SSRF 三层防护的出网 http.Client。
//
// allowPrivate 语义与 ValidateURL 一致：true 放行私网/环回（内网收件端场景），
// 但链路本地/云元数据/0.0.0.0/8/IPv6 fe80::/10 恒拒。
func NewClient(timeout time.Duration, allowPrivate bool) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		// Proxy 恒为 nil，理由见包注释（环境代理会让 IP 级校验失效）。
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("invalid dial address %q: %w", addr, err)
			}
			ips, err := LookupHostIPs(host)
			if err != nil {
				return nil, fmt.Errorf("resolve %q: %w", host, err)
			}
			var lastErr error
			for _, ip := range ips {
				if reason := IPRejection(ip, allowPrivate); reason != "" {
					lastErr = fmt.Errorf("dial %s blocked by SSRF guard: %s", ip, reason)
					continue
				}
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			if lastErr == nil {
				lastErr = fmt.Errorf("host %q resolved to no usable address", host)
			}
			return nil, lastErr
		},
	}
	c := &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("too many redirects (>%d)", maxRedirects)
			}
			// 每一跳重新做 URL 级校验：这是"外网 URL 返回 302 打到云元数据"
			// 最常见形态的唯一阻断点。
			if err := ValidateURL(req.URL.String(), allowPrivate); err != nil {
				return fmt.Errorf("redirect target rejected by SSRF guard: %w", err)
			}
			return nil
		},
	}
	// 登记该 client 的 allowPrivate 口径，供 AllowsPrivate 反查。
	// 目的：调用方（internal/notify）需要在做前置 URL 校验时用**同一口径**，
	// 否则会出现"client 放行、前置校验拒绝"或反之的自相矛盾。
	// 用 client 指针做 key 是安全的：本包的 client 生命周期与进程同长或由
	// 调用方持有，不会在登记后被丢弃指针复用（见 AllowsPrivate 的兜底语义）。
	policies.Store(c, allowPrivate)
	return c
}

// policies 记录每个由本包构造的 client 的 allowPrivate 口径。
var policies sync.Map // map[*http.Client]bool

// AllowsPrivate 报告该 client 由 NewClient 构造时使用的 allowPrivate 口径。
//
// 对非本包构造的 client（如测试替身、http.DefaultClient）返回 false——
// 即按"恒拒私网"这一安全基线处理。
func AllowsPrivate(c *http.Client) bool {
	if c == nil {
		return false
	}
	if v, ok := policies.Load(c); ok {
		// 断言失败也落回 false（安全基线）：登记表里若混进非 bool 值，
		// 宁可拒绝私网也不放行。
		b, isBool := v.(bool)
		return isBool && b
	}
	return false
}
