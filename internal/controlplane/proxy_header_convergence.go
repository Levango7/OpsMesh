// proxy_header_convergence.go — 代理响应头收敛（演练报告 F5）。
//
// 背景（docs/td60-batch0-drill-2026-10-12.md F5，2026-10-12 批次 0 实测）：
// 切流后同一请求里 Content-Security-Policy / Referrer-Policy / X-Frame-Options /
// X-Content-Type-Options 各出现**两份**（对侧还额外补了 CORS 与 HSTS）。
//
// 成因是结构性的、不是某一侧的 bug：聚合层在外层已权威设置这些头
// （securityHeadersMiddleware / corsMiddleware），而上游（auth-svc 经
// pkg/security.SecurityHeaders 等）各自也设一遍同名头；httputil.ReverseProxy
// 拷贝上游响应头用的是 **Add** ⇒ 同名头两份、且取值冲突：
//   - 两份 CSP：浏览器对多份 CSP 取**交集**强制执行 ⇒ 外层 nonce 与上游
//     "default-src 'self'" 互不满足，内联 script（nonce 型）两条规则下都放不过；
//   - 两份 Referrer-Policy 取值冲突（no-referrer vs strict-origin-when-cross-origin），
//     浏览器与缓存对多值头处理并不一致；
//   - 两份不同取值的 Access-Control-Allow-Origin：浏览器直接判该响应
//     "contains multiple Access-Control-Allow-Origin values" ⇒ 跨源请求失败。
//
// 修法（收敛为单一来源=外层）：转发前剥掉上游的这些头，让外层值单份生效。
// 只收"外层一定会自己设"的头；上游独有的头（如 X-XSS-Protection）不动——
// 不构成重复，剥它反而丢信息。
package controlplane

import (
	"net/http"
	"net/http/httputil"
)

// proxyConvergedResponseHeaders 外层已权威设置、须从上游响应中剥除的头。
// securityHeadersMiddleware 必设：X-Content-Type-Options / X-Frame-Options /
// Referrer-Policy / Permissions-Policy / Content-Security-Policy，TLS 时另设
// Strict-Transport-Security；corsMiddleware 在 Origin 过白名单时设 CORS 五件套。
// （HSTS 也在列：外层无 TLS 时它一个都不该出现，上游补的同样只该由外层决定。）
var proxyConvergedResponseHeaders = []string{
	"Content-Security-Policy",
	"Referrer-Policy",
	"X-Frame-Options",
	"X-Content-Type-Options",
	"Permissions-Policy",
	"Strict-Transport-Security",
	"Access-Control-Allow-Origin",
	"Access-Control-Allow-Methods",
	"Access-Control-Allow-Headers",
	"Access-Control-Allow-Credentials",
	"Access-Control-Max-Age",
}

// installProxyHeaderConvergence 给 ReverseProxy 装响应头收敛钩子。
//
// 两条转发路径共用（同一形状的问题只保留一份实现，不手抄第二份）：
//   - handleServiceProxy：聚合层 /api/v1/<domain>-svc/* 前缀代理；
//   - forwardAuthToSvc：auth 切流把本地 /api/v1/auth/* 请求整体转给 auth-svc。
//
// 不覆盖 /gw/ 数据面（extension.RouteRule 指向运维自配后端，与本问题无关）；
// 若将来那边也发现重复，挂同一个函数即可。
func installProxyHeaderConvergence(proxy *httputil.ReverseProxy) {
	proxy.ModifyResponse = func(resp *http.Response) error {
		for _, h := range proxyConvergedResponseHeaders {
			resp.Header.Del(h)
		}
		return nil
	}
}
