// auth_guard.go 客户端身份提取（clientIP / deviceFingerprint）+ 防爆破器的父包门面。
//
// TD-87 批 2：防爆破/限流实现（原 loginGuard）已整体迁入 internal/controlplane/loginguard/；
// 本文件保留两件：
//   - `clientIP`/`deviceFingerprint`：通用 HTTP 身份提取，被 auth_login/server_netsec/
//     server_bootstrap/server_middleware 等多处（23/7 处）复用，不属于防爆破器；
//   - 门面：类型别名 `loginGuard` + `newLoginGuard` 薄包装 —— 43 处 Server 构造点
//     （测试 `loginGuard: newLoginGuard(ss)`）与生产调用点因此零改动。
package controlplane

import (
	"net"
	"net/http"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/loginguard"
	"github.com/Levango7/OpsMesh/internal/store"
)

// loginGuard 防爆破/限流器（loginguard.Guard 的别名——同一类型）。
type loginGuard = loginguard.Guard

// newLoginGuard 构造防爆破器（loginguard.New 的薄包装）。
func newLoginGuard(ss store.SessionStore) *loginGuard {
	return loginguard.New(ss)
}

// clientIP 提取客户端真实 IP。
// trustProxy=false（默认，安全）：仅用 RemoteAddr，防止客户端伪造 X-Forwarded-For 绕过登录限流/审计；
// trustProxy=true（确有可信反代/LB 前置并注入真实 IP 时）：信任 XFF 首段。
func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if idx := strings.Index(xff, ","); idx > 0 {
				return strings.TrimSpace(xff[:idx])
			}
			return strings.TrimSpace(xff)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// parseTrustedProxies 解析 CIDR 列表；任一条目非法即返回错误（配置错误显式失败）。
func parseTrustedProxies(cidrs []string) ([]*net.IPNet, error) {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, ipnet, err := net.ParseCIDR(strings.TrimSpace(c))
		if err != nil {
			return nil, err
		}
		out = append(out, ipnet)
	}
	return out, nil
}

// peerIP 取 RemoteAddr 的 IP 部分（无端口时原样返回）。
func peerIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func ipInNets(ip string, nets []*net.IPNet) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(parsed) {
			return true
		}
	}
	return false
}

// clientIPTrusted 依据可信 CIDR 解析客户端 IP（ADR-2026-11-15 §ADR-1）。
//
// 规则：仅当直连对端（RemoteAddr）落在可信 CIDR 内，才解析 X-Forwarded-For；
// 从右向左跳过可信跳，返回第一个非可信地址（最右侧可信代理写入的客户端地址）。
// 不在可信 CIDR 内时一律用 RemoteAddr，避免任意客户端伪造 XFF。
func clientIPTrusted(r *http.Request, trustProxy bool, trusted []*net.IPNet) string {
	peer := peerIP(r)
	if !trustProxy || len(trusted) == 0 || !ipInNets(peer, trusted) {
		return peer
	}
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return peer
	}
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip := strings.TrimSpace(parts[i])
		if ip == "" {
			continue
		}
		if !ipInNets(ip, trusted) {
			return ip
		}
	}
	// 全部跳都是可信代理：退化为最左段（原始客户端）。
	if len(parts) > 0 {
		return strings.TrimSpace(parts[0])
	}
	return peer
}

// clientIP 是 Server 级入口：依据可信反代 CIDR 解析客户端 IP（ADR-2026-11-15 §ADR-1）。
// 所有 handler/中间件应使用本方法，而不是直接调用包级 clientIP。
func (s *Server) clientIP(r *http.Request) string {
	if s == nil || s.cfg == nil {
		return clientIP(r, false)
	}
	return clientIPTrusted(r, s.cfg.TrustProxy, s.trustedProxyNets)
}

// deviceFingerprint 从请求头 X-Device-FP 提取设备指纹（设备绑定）。
// 前端可传 User-Agent 摘要或随机 UUID（同设备稳定即可）。空串表示不校验设备
// （向后兼容：旧客户端不传头时 DeviceFP 为空，签发时存空，验证时不校验）。
func deviceFingerprint(r *http.Request) string {
	if r == nil {
		return ""
	}
	return strings.TrimSpace(r.Header.Get("X-Device-FP"))
}
