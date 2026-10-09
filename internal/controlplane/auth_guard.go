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

// deviceFingerprint 从请求头 X-Device-FP 提取设备指纹（设备绑定）。
// 前端可传 User-Agent 摘要或随机 UUID（同设备稳定即可）。空串表示不校验设备
// （向后兼容：旧客户端不传头时 DeviceFP 为空，签发时存空，验证时不校验）。
func deviceFingerprint(r *http.Request) string {
	if r == nil {
		return ""
	}
	return strings.TrimSpace(r.Header.Get("X-Device-FP"))
}
