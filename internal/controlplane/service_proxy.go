// service_proxy.go 微服务聚合代理的 handler 与响应助手（TD-87 批 2：规则引擎已迁至
// internal/controlplane/svcproxy，本文件保留 Server 端接线）。
package controlplane

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/svcproxy"
)

// handleServiceProxy 五域统一代理 handler：
// 鉴权（requirePermission）→ 匹配规则 → ReverseProxy 转发（路径已改写）。
//
// 错误语义：
//   - 404：路径不匹配任何域（正常由 mux 注册边界保证，防御性兜底）；
//   - 503：后端地址解析失败或连接被拒（服务未启动/端口不对）；
//   - 401/403：由 requirePermission/requireTenantContext 写出。
func (s *Server) handleServiceProxy(w http.ResponseWriter, r *http.Request) {
	rule := svcproxy.Lookup(r.URL.Path)
	if rule != nil && !rule.IsActive() {
		// 双轨机制（TD-60 A-2 auth-svc）：AUTH_SVC_PROXY_ENABLED 默认 false →
		// auth-svc 代理规则不活跃，控制面本地 /api/v1/auth/* 直接处理；
		// 设 true 时代理接线生效（/api/v1/auth-svc 前缀→auth-svc /api/v1/auth）。
		rule = nil
	}
	if rule == nil {
		writeProxyErrorJSON(w, http.StatusNotFound, "no service proxy route matches "+r.URL.Path)
		return
	}
	// 聚合层鉴权：与站内其他 API 同一守卫（第七轮越权修复后 requireAuth 语义：
	// 无凭证的裸租户头在此被拒，绝不透传到无鉴权的微服务）。
	// 权限点按「方法 + 路径」分级解析（permRules），与单体本地对同一操作的
	// 要求逐条对齐——例如 DELETE /device-svc/devices/{id} 要求 device:delete，
	// 而不是规则级兜底的 device:read。
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requirePermission(w, r, rule.ResolvePerm(r.Method, r.URL.Path)); !ok {
		return
	}
	target := rule.UpstreamBase()
	if target == nil {
		writeProxyErrorJSON(w, http.StatusServiceUnavailable, "service backend address invalid: "+rule.EnvKey)
		return
	}
	// 自环拦截：后端指向控制面自身时**必须**停在这里。
	// 放过去的后果是请求被改写后转发回本进程，命中单体自己的同名 handler，
	// 返回另一个子系统的数据且无任何错误信号（出厂即命中：portal 域的
	// 默认值 127.0.0.1:8080 正是控制面监听端口，/api/v1/portal/quotas
	// 会被改写成 /api/v1/quots 而返回单体的配额数据）。
	if port, self := svcproxy.SelfLoopTarget(target, s.httpPort, s.grpcPort, s.metricsPort); self {
		writeProxyErrorJSON(w, http.StatusServiceUnavailable, fmt.Sprintf(
			"service backend for %s points at the control plane itself (%s:%d); "+
				"set %s to the service address",
			rule.Domain, target.Hostname(), port, rule.EnvKey))
		return
	}
	// 连接预检：后端不可达直接 503（带服务名），比 ReverseProxy 空响应体
	// 的 502 更可诊断——前端 toast 能提示"服务未启动"而非空洞错误。
	if conn, err := net.Dial("tcp", target.Host); err != nil {
		writeProxyErrorJSON(w, http.StatusServiceUnavailable, "service unreachable: "+rule.PublicPrefix+" backend "+target.Host)
		return
	} else {
		_ = conn.Close()
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(req *http.Request) {
		director(req)
		req.URL.Path = rule.RewriteProxyPath(r.URL.Path)
		req.URL.RawPath = ""
		// 下游微服务不消费会话 Cookie；鉴权已在聚合层完成，剥除防止
		// 会话凭证意外落地到内部服务的访问日志。
		req.Header.Del("Cookie")
		// 身份头统一治理：客户端自带的租户/用户/角色头一律剥离，再以聚合层
		// 已校验的 actx 重注入（requireTenantContext 已完成令牌交叉校验；
		// 头注入模式下即为网关已认证值）。下游消费面：device/task 域网关读
		// X-Tenant-ID 做租户过滤，gpu-svc / portal-svc 读 X-Tenant-ID 与
		// X-User-Id 做租户归置与审计留痕——不注入则下游兜底 default，多租户
		// 下数据落错租户桶；不剥离则客户端可直通伪造值（X-User-Roles 下游
		// 无消费方，仅剥离防伪造留存）。
		req.Header.Del("X-Tenant-ID")
		req.Header.Del("X-User-Id")
		req.Header.Del("X-User-Roles")
		if actx.TenantID != "" {
			req.Header.Set("X-Tenant-ID", actx.TenantID)
		}
		if actx.UserID != "" {
			req.Header.Set("X-User-Id", actx.UserID)
		}
	}
	proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, err error) {
		// 原始 err 只进日志：httputil 的代理错误含**上游服务的 host:port 与失败
		// 原因**（连接被拒 / TLS 握手失败 / 超时），回吐给客户端等于把内部
		// 服务拓扑与网络策略一并暴露。502 对客户端的含义是"后端不可用"，
		// 排查所需信息服务端日志里都有。
		log.Printf("[controlplane] service proxy backend error: %v", err)
		writeProxyErrorJSON(rw, http.StatusBadGateway, "service backend unavailable")
	}
	proxy.ServeHTTP(w, r)
}

// writeProxyErrorJSON 代理层错误响应（与站内 {"error": msg} 约定一致）。
// paginate.WriteJSON 不可直接用的原因：避免与本文件引入 paginate 依赖形成
// 循环——此处手写最小实现（Content-Type + 状态码 + 单字段 JSON）。
func writeProxyErrorJSON(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":` + jsonString(msg) + `}`))
}

// jsonString 极简 JSON 字符串转义（错误消息只含 ASCII 与常见中文场景，
// 覆盖引号/反斜杠/控制字符；完整转义由前端 JSON.parse 容错）。
func jsonString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, c := range s {
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}
