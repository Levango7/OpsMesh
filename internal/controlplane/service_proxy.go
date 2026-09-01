package controlplane

// service_proxy.go — 微服务聚合代理：把 6 个独立微服务域转发到对应 services/* 进程。
//
// 背景（M13 拆分后的接线层）：gpu/bot/runbook/incident/autoscaler/portal 六域
// 已有独立微服务（services/<svc>/）+ 前端视图（web/enterprise/src/views/）+
// API 封装（src/api/*.js），但 controlplane 聚合层未注册路由，前端路由被迫
// 全量停用（router/index.js 注释块）。本文件补齐"最后一公里"：
//
//   - 前端 request.js baseURL=/api/v1，调 /api/v1/{domain}/*；
//   - 各微服务监听 /api/v1/{svc 路径}/*（部分服务路径不含自身域前缀，
//     如 autoscaler-svc 是 /api/v1/rules——由 prefixStrip 做路径改写）；
//   - 鉴权：微服务本身不做租户鉴权（内部服务定位），聚合层统一
//     requirePermission + requireTenantContext 双守卫——与第七轮越权修复
//     （http_infra.go requireTenantContext）同一信任边界，绝不裸转发。
//
// 设计（与 gateway.go 的 ReverseProxy 模式一致，差异点）：
//   - 静态映射表（serviceProxyRules），非运行期 CRUD 数据面——路由是
//     产品结构而非运行期配置，重启不丢；
//   - 后端地址 env 可覆盖（GPU_SVC_URL 等），默认 localhost:<默认端口>；
//   - 后端不可达 → 503（含服务名提示），不吞错；
//   - 只透传方法与 body，剥离 Cookie（下游不消费会话；鉴权已完成）。

import (
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
)

// serviceProxyRule 一条微服务转发规则。
type serviceProxyRule struct {
	// publicPrefix 聚合层对外路径前缀（mux 注册用），如 /api/v1/gpu。
	publicPrefix string
	// upstreamPrefix 后端服务真实路径前缀。publicPrefix 剥去 domainPrefix
	// 后的剩余部分拼到 upstreamPrefix 之后（路径改写规则见 rewriteProxyPath）。
	upstreamPrefix string
	// domainPrefix publicPrefix 中需剥掉的域前缀（剩余子路径转发时保留）。
	// 例：publicPrefix=/api/v1/autoscaler，domainPrefix=/api/v1/autoscaler，
	// 请求 /api/v1/autoscaler/rules/123 → 后端 /api/v1/rules/123。
	domainPrefix string
	// envKey 覆盖后端地址的环境变量名（空=不可覆盖，恒用 defaultURL）。
	envKey string
	// defaultURL 默认后端地址（本机默认端口；与各 svc pkg/config 默认值一致）。
	defaultURL string
	// perm 聚合层鉴权所需权限（requirePermission）。
	perm string
}

// serviceProxyRules 六域转发映射表。端口依据各服务 pkg/config 默认值：
//   gpu:8090 / runbook:8082 / incident:8082 / autoscaler:8080 / portal:8080 / bot:8080
// 注意 runbook 与 incident、autoscaler 与 portal 默认端口两两相同——单机同跑
// 多服务时必须用 env 覆盖（*_SVC_URL 或各服务 *_SVC_HTTP_PORT）区分。
var serviceProxyRules = []serviceProxyRule{
	{
		publicPrefix:  "/api/v1/gpu",
		upstreamPrefix: "/api/v1/gpu",
		domainPrefix:  "/api/v1/gpu",
		envKey:        "GPU_SVC_URL",
		defaultURL:    "http://127.0.0.1:8090",
		perm:          "gpu:read",
	},
	{
		publicPrefix:  "/api/v1/runbooks",
		upstreamPrefix: "/api/v1/runbooks",
		domainPrefix:  "/api/v1/runbooks",
		envKey:        "RUNBOOK_SVC_URL",
		defaultURL:    "http://127.0.0.1:8082",
		perm:          "runbook:read",
	},
	{
		publicPrefix:  "/api/v1/incidents",
		upstreamPrefix: "/api/v1/incidents",
		domainPrefix:  "/api/v1/incidents",
		envKey:        "INCIDENT_SVC_URL",
		defaultURL:    "http://127.0.0.1:8082",
		perm:          "incident:read",
	},
	{
		// autoscaler-svc 路径不含域前缀（/api/v1/rules 而非 /api/v1/autoscaler/rules，
	// 见 services/autoscaler-svc/internal/handler/handler.go:24-28），需路径改写。
		publicPrefix:  "/api/v1/autoscaler",
		upstreamPrefix: "/api/v1",
		domainPrefix:  "/api/v1/autoscaler",
		envKey:        "AUTOSCALER_SVC_URL",
		defaultURL:    "http://127.0.0.1:8080",
		perm:          "autoscaler:read",
	},
	{
		// portal-svc 同理：/api/v1/requests 而非 /api/v1/portal/requests
		// （见 services/portal-svc/internal/handler/handler.go:27-34）。
		publicPrefix:  "/api/v1/portal",
		upstreamPrefix: "/api/v1",
		domainPrefix:  "/api/v1/portal",
		envKey:        "PORTAL_SVC_URL",
		defaultURL:    "http://127.0.0.1:8080",
		perm:          "portal:read",
	},
	{
		// bot-svc 暴露的是 ChatOps 平台回调（/webhook/{wecom,feishu,slack,dingtalk}）
		// 与前端契约（/bot/command、/bot/history…）不同构——本条目只透传
		// /api/v1/bot/platforms 等只读探活类端点不可行，因此 bot 域在聚合层
		// 由 bot_bridge.go 提供与前端契约一致的 handler（本表不注册 bot）。
		// 保留此注释作为六域清单的完整性说明。
	},
}

// lookupServiceProxyRule 按请求路径匹配转发规则（最长前缀语义由注册顺序保证：
// server_lifecycle.go 按本表顺序注册，ServeMux 自身按最长模式匹配）。
func lookupServiceProxyRule(path string) *serviceProxyRule {
	for i := range serviceProxyRules {
		r := &serviceProxyRules[i]
		if r.publicPrefix == "" {
			continue
		}
		if path == r.publicPrefix || strings.HasPrefix(path, r.publicPrefix+"/") {
			return r
		}
	}
	return nil
}

// upstreamBase 解析后端地址：envKey 覆盖优先，缺省 defaultURL。
func (r *serviceProxyRule) upstreamBase() *url.URL {
	raw := r.defaultURL
	if r.envKey != "" {
		if v := os.Getenv(r.envKey); v != "" {
			raw = v
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		// 静态默认值不可达才会走到这里；按不可达处理。
		return nil
	}
	return u
}

// rewriteProxyPath 把聚合层路径改写为后端真实路径：
// 剥 domainPrefix，剩余子路径拼到 upstreamPrefix 之后。
// 例：/api/v1/autoscaler/rules/123 → /api/v1/rules/123。
func (r *serviceProxyRule) rewriteProxyPath(path string) string {
	rest := strings.TrimPrefix(path, r.domainPrefix)
	if rest == "" || rest == "/" {
		return r.upstreamPrefix
	}
	// rest 形如 /rules/123（TrimPrefix 保留斜杠开头）。
	return r.upstreamPrefix + rest
}

// handleServiceProxy 六域统一代理 handler：
// 鉴权（requirePermission）→ 匹配规则 → ReverseProxy 转发（路径已改写）。
//
// 错误语义：
//   - 404：路径不匹配任何域（正常由 mux 注册边界保证，防御性兜底）；
//   - 503：后端地址解析失败或连接被拒（服务未启动/端口不对）；
//   - 401/403：由 requirePermission/requireTenantContext 写出。
func (s *Server) handleServiceProxy(w http.ResponseWriter, r *http.Request) {
	rule := lookupServiceProxyRule(r.URL.Path)
	if rule == nil {
		writeProxyErrorJSON(w, http.StatusNotFound, "no service proxy route matches "+r.URL.Path)
		return
	}
	// 聚合层鉴权：与站内其他 API 同一守卫（第七轮越权修复后 requireAuth 语义：
	// 无凭证的裸租户头在此被拒，绝不透传到无鉴权的微服务）。
	if _, ok := s.requirePermission(w, r, rule.perm); !ok {
		return
	}
	if _, ok := s.requireTenantContext(w, r); !ok {
		return
	}
	target := rule.upstreamBase()
	if target == nil {
		writeProxyErrorJSON(w, http.StatusServiceUnavailable, "service backend address invalid: "+rule.envKey)
		return
	}
	// 连接预检：后端不可达直接 503（带服务名），比 ReverseProxy 空响应体
	// 的 502 更可诊断——前端 toast 能提示"服务未启动"而非空洞错误。
	if conn, err := net.Dial("tcp", target.Host); err != nil {
		writeProxyErrorJSON(w, http.StatusServiceUnavailable, "service unreachable: "+rule.publicPrefix+" backend "+target.Host)
		return
	} else {
		_ = conn.Close()
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(req *http.Request) {
		director(req)
		req.URL.Path = rule.rewriteProxyPath(r.URL.Path)
		req.URL.RawPath = ""
		// 下游微服务不消费会话 Cookie；鉴权已在聚合层完成，剥除防止
		// 会话凭证意外落地到内部服务的访问日志。
		req.Header.Del("Cookie")
	}
	proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, err error) {
		writeProxyErrorJSON(rw, http.StatusBadGateway, "service backend error: "+err.Error())
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
