// security_headers.go — HTTP 安全响应头中间件（可配置）。
//
// 应用位置：server_lifecycle.go 组装的中间件链第 6 层（限流之后、CSRF/默认拒绝
// 之前），因此 401/403/404 等由内层产生的响应同样携带安全头；企业版 SPA 静态
// 资源经同一 mux 出口，亦被覆盖。
//
// 配置来源（临时接线，待 internal/config 新增字段后迁移）：
//   - OPSMESH_SECURITY_HEADERS：总开关，缺省 true；
//   - OPSMESH_CSP_POLICY：CSP 策略，缺省为 defaultSecurityCSPPolicy，显式置空关闭 CSP；
//   - OPSMESH_HSTS：HSTS 强制开关，缺省 false（TLS 请求始终注入）。
//
// CSP 与内联资源审计（2026-11-16）：
//   - web/enterprise/index.html 与 web/enterprise/dist/index.html 均无 inline
//     <script>，入口为 <script type="module" src=...>；
//   - Vue 模板的 @click 由编译器转为 addEventListener，不产生 inline onclick；
//   - style-src 保留 'unsafe-inline'：Vue :style 运行时注入 + 个人版引导页 inline
//     <style> 块；
//   - script-src 额外保留每请求 nonce：现有代理响应头收敛守卫
//     （proxy_header_convergence_test.go）要求外层 CSP 携带 nonce，且 nonce 是
//     纯增量收紧，不会放宽 'self'。若以后前端引入 inline script，需改为
//     nonce/hash 白名单而不是 'unsafe-inline'。
package controlplane

import (
	cryptoRand "crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
)

const (
	// defaultSecurityCSPPolicy 是 CSP 缺省策略；空字符串表示关闭 CSP。
	defaultSecurityCSPPolicy = "default-src 'self'; base-uri 'self'; frame-ancestors 'none'; " +
		"form-action 'self'; object-src 'none'; img-src 'self' data:; " +
		"style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'"

	// defaultSecurityPermissionsPolicy 禁用敏感设备权限。
	defaultSecurityPermissionsPolicy = "geolocation=(), microphone=(), camera=()"

	// securityHSTSValue 仅在 TLS 请求或显式启用时注入。
	securityHSTSValue = "max-age=31536000; includeSubDomains"
)

// securityHeadersConfig 是安全头中间件的运行时配置。
// 零值由 defaultSecurityHeadersConfig 提供；Server.securityHeaders 为 nil 时
// 同样回落到缺省值（兼容直接 &Server{} 构造的既有测试）。
type securityHeadersConfig struct {
	// Disabled 为 true 时整个中间件直通，不写任何安全头。
	Disabled bool
	// CSPPolicy 为空时关闭 Content-Security-Policy，其余头不受影响。
	CSPPolicy string
	// HSTSEnabled 为 true 时对明文 HTTP 也注入 HSTS（显式 opt-in）。
	HSTSEnabled bool
}

// defaultSecurityHeadersConfig 返回生产缺省配置：总开关开启、缺省 CSP、HSTS 仅随 TLS。
func defaultSecurityHeadersConfig() securityHeadersConfig {
	return securityHeadersConfig{
		Disabled:  false,
		CSPPolicy: defaultSecurityCSPPolicy,
	}
}

// securityHeadersMiddleware 为所有响应注入安全头。
//
// HSTS 注入条件：请求经 TLS（r.TLS != nil）、配置显式开启（OPSMESH_HSTS），
// 或服务端已配置 TLS 证书（s.tlsCert != "" 即显式声明 HTTPS 部署）。
func (s *Server) securityHeadersMiddleware(h http.Handler) http.Handler {
	cfg := defaultSecurityHeadersConfig()
	if s.securityHeaders != nil {
		cfg = *s.securityHeaders
	}
	if cfg.Disabled {
		return h
	}
	hstsForced := cfg.HSTSEnabled || s.tlsCert != ""
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", defaultSecurityPermissionsPolicy)
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		if cfg.CSPPolicy != "" {
			w.Header().Set("Content-Security-Policy", buildSecurityCSP(cfg.CSPPolicy))
		}
		if r.TLS != nil || hstsForced {
			w.Header().Set("Strict-Transport-Security", securityHSTSValue)
		}
		h.ServeHTTP(w, r)
	})
}

// buildSecurityCSP 在策略的 script-src 上追加每请求 nonce。
//
// nonce 仅作为防御纵深：当前前后端均无 inline script，'self' 已足以放行入口
// module。随机源失败时回退固定 nonce（只影响强度，不阻断请求）。
func buildSecurityCSP(policy string) string {
	nonceBytes := make([]byte, 16)
	if _, err := cryptoRand.Read(nonceBytes); err != nil {
		nonceBytes = []byte("fallback-nonce-v1")
	}
	nonce := hex.EncodeToString(nonceBytes)
	nonceDirective := " 'nonce-" + nonce + "'"
	// 追加在 'self' 之后，保证 "script-src 'self'" 前缀语义不变。
	if strings.Contains(policy, "script-src 'self'") {
		return strings.Replace(policy, "script-src 'self'", "script-src 'self'"+nonceDirective, 1)
	}
	if strings.Contains(policy, "script-src") {
		return strings.Replace(policy, "script-src", "script-src"+nonceDirective, 1)
	}
	return policy + "; script-src" + nonceDirective
}
