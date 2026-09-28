package controlplane

// service_proxy.go — 微服务聚合代理：把独立微服务域转发到对应 services/* 进程。
//
// 背景（M13 拆分后的接线层）：gpu/runbook/incident/autoscaler/portal 五域
// 已有独立微服务（services/<svc>/）+ 前端视图（web/enterprise/src/views/）+
// API 封装（src/api/*.js），但 controlplane 聚合层未注册路由，前端路由被迫
// 全量停用（router/index.js 注释块）。本文件补齐"最后一公里"（bot 域的
// bot-svc 已于 2026-09-29 删除——Web 契约由 bot_bridge.go 独任，见该文件）：
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
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sort"
	"strings"
)

// serviceProxyRule 一条微服务转发规则。
type serviceProxyRule struct {
	// domain 域标识（开关 OPSMESH_SERVICE_PROXY 用），如 gpu / portal / device。
	domain string
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
	// perm 聚合层鉴权所需权限（requirePermission）——permRules 未命中时的兜底。
	perm string
	// permRules 域内权限分级（可选）：按声明顺序 first-match，未命中回落 perm。
	// 用于同一转发前缀下不同操作对应不同权限点的场景（如 tasks 的
	// GET/POST/cancel/approve 分别是 task:read/task:write/task:cancel/task:approve）。
	permRules []proxyPermRule
}

// proxyPermRule 域内权限分级规则：按「方法 + 子路径前缀/后缀」把请求映射到权限点。
//
// 为什么需要：规则级单一 perm 在方法维度上是粗粒度的——device 域此前对
// DELETE /devices/{id}（controlplane 本地要求 device:delete）与
// POST /devices/{id}/provision（本地要求 provision:execute）都只校验
// device:read，持有只读权限的凭证可经代理前缀完成删除/纳管，构成权限放大。
// 分级后代理与本地单体对同一操作的权限要求逐一对齐（双轨期行为等价，
// 也是阶段 3 裁决时两边可比的前提）。
// 注意：pathPrefix / pathSuffix 按**上游（后端改写后）路径**书写，如
// /api/v1/schedules 而非公开前缀形态 /api/v1/task-svc/schedules——这样规则
// 表达的是"后端 API 的子路径语义"（与本地单体 handler 的路径一致，便于逐条
// 比对），且公开前缀在切流阶段变更（如剥掉 -svc 后缀直连）时分级规则不失效。
type proxyPermRule struct {
	method     string // 空=任意方法
	pathPrefix string // 空=任意路径（按上游路径）；非空要求 path == prefix 或 HasPrefix(path, prefix+"/")
	pathSuffix string // 空=不校验；非空要求 HasSuffix(path, suffix)
	perm       string // 命中后要求持有的权限点
}

// matches 判定请求（方法 + 完整路径）是否命中本条分级规则。
func (pr *proxyPermRule) matches(method, path string) bool {
	if pr.method != "" && pr.method != method {
		return false
	}
	if pr.pathPrefix != "" && path != pr.pathPrefix && !strings.HasPrefix(path, pr.pathPrefix+"/") {
		return false
	}
	if pr.pathSuffix != "" && !strings.HasSuffix(path, pr.pathSuffix) {
		return false
	}
	return true
}

// resolvePerm 解析本次请求所需的权限点：permRules 首条命中优先，否则回落 perm。
// 入参 path 为**公开请求路径**（如 /api/v1/task-svc/schedules）；匹配前先经
// rewriteProxyPath 改写为上游路径（permRules 的路径字段按上游形态书写）。
func (r *serviceProxyRule) resolvePerm(method, path string) string {
	upstream := r.rewriteProxyPath(path)
	for i := range r.permRules {
		if r.permRules[i].matches(method, upstream) {
			return r.permRules[i].perm
		}
	}
	return r.perm
}

// serviceProxyRules 五域转发映射表。端口依据各服务 pkg/config 默认值：
//
//	gpu:8090 / runbook:8082 / incident:8082 / autoscaler:8080 / portal:8080
//
// 注意 runbook 与 incident、autoscaler 与 portal 默认端口两两相同——单机同跑
// 多服务时必须用 env 覆盖（*_SVC_URL 或各服务 *_SVC_HTTP_PORT）区分。
//
// 权限分级：GET → *:read，其余方法 → *:write（permRules 首条命中，跨出即兜底）。
// 此前全体方法统一校验 *:read——持有只读凭证的调用方可经代理完成写操作
// （建 GPU 工作负载、执行 Runbook、审批门户请求等），与 device 域同一类权限
// 放大；2026-09-29 随 task 域接通一并修复。前端路由门禁本就是 *:read（页面准入），
// 动作级校验在服务端——收紧写权限不影响「只读角色进页面、点按钮被拒」的既有模型。
var serviceProxyRules = []serviceProxyRule{
	{
		domain:         "gpu",
		publicPrefix:   "/api/v1/gpu",
		upstreamPrefix: "/api/v1/gpu",
		domainPrefix:   "/api/v1/gpu",
		envKey:         "GPU_SVC_URL",
		defaultURL:     "http://127.0.0.1:8090",
		perm:           "gpu:write",
		permRules: []proxyPermRule{
			{method: http.MethodGet, perm: "gpu:read"},
		},
	},
	{
		domain:         "runbook",
		publicPrefix:   "/api/v1/runbooks",
		upstreamPrefix: "/api/v1/runbooks",
		domainPrefix:   "/api/v1/runbooks",
		envKey:         "RUNBOOK_SVC_URL",
		defaultURL:     "http://127.0.0.1:8082",
		perm:           "runbook:write",
		permRules: []proxyPermRule{
			{method: http.MethodGet, perm: "runbook:read"},
		},
	},
	{
		domain:         "incident",
		publicPrefix:   "/api/v1/incidents",
		upstreamPrefix: "/api/v1/incidents",
		domainPrefix:   "/api/v1/incidents",
		envKey:         "INCIDENT_SVC_URL",
		defaultURL:     "http://127.0.0.1:8082",
		perm:           "incident:write",
		permRules: []proxyPermRule{
			{method: http.MethodGet, perm: "incident:read"},
		},
	},
	{
		// autoscaler-svc 路径不含域前缀（/api/v1/rules 而非 /api/v1/autoscaler/rules，
		// 见 services/autoscaler-svc/internal/handler/handler.go:24-28），需路径改写。
		domain:         "autoscaler",
		publicPrefix:   "/api/v1/autoscaler",
		upstreamPrefix: "/api/v1",
		domainPrefix:   "/api/v1/autoscaler",
		envKey:         "AUTOSCALER_SVC_URL",
		defaultURL:     "http://127.0.0.1:8080",
		perm:           "autoscaler:write",
		permRules: []proxyPermRule{
			{method: http.MethodGet, perm: "autoscaler:read"},
		},
	},
	{
		// portal-svc 同理：/api/v1/requests 而非 /api/v1/portal/requests
		// （见 services/portal-svc/internal/handler/handler.go:27-34）。
		domain:         "portal",
		publicPrefix:   "/api/v1/portal",
		upstreamPrefix: "/api/v1",
		domainPrefix:   "/api/v1/portal",
		envKey:         "PORTAL_SVC_URL",
		defaultURL:     "http://127.0.0.1:8080",
		perm:           "portal:write", // 含审批动作（approve/reject 为 POST）
		permRules: []proxyPermRule{
			{method: http.MethodGet, perm: "portal:read"},
		},
	},
}

// deviceProxyExtras device 域（D1/D3 后 device-svc 已有完整 REST 网关）额外转发规则。
// 与 serviceProxyRules 分表的原因：device 域的网关直连 store 层、鉴权走
// tenant.Middleware（X-Tenant-ID 头或 JWT）——代理层完成鉴权后需显式注入
// X-Tenant-ID 头再转发（五域微服务不消费租户上下文，device 消费）。
//
// 路径设计：publicPrefix 用 /api/v1/device-svc 域前缀（剥去后拼回 /api/v1/*），
// 如 /api/v1/device-svc/devices → 后端 /api/v1/devices。不能用 /api/v1/devices
// 直转——controlplane 本地已有同名 handler（server_lifecycle.go:25-34），
// 同一 mux 重复注册会 panic；双轨期新旧路径并存（旧=controlplane 本地实现，
// 新=device-svc 网关），切流阶段再评估替换。
// bootstrap 端点（/install.sh、/api/v1/provision/register）不经代理
// （agent 自举直连 device-svc）。
var deviceProxyExtras = []serviceProxyRule{
	{
		domain:         "device",
		publicPrefix:   "/api/v1/device-svc/devices",
		upstreamPrefix: "/api/v1/devices",
		domainPrefix:   "/api/v1/device-svc/devices",
		envKey:         "DEVICE_SVC_URL",
		defaultURL:     "http://127.0.0.1:8081",
		perm:           "device:write", // 兜底：注册/更新/心跳等写操作
		permRules: []proxyPermRule{
			{method: http.MethodGet, perm: "device:read"},
			// DELETE /devices/{id} 退役：与本地 handleRetireDevice（server_devices.go:218）
			// 的 device:delete 对齐——此前统一 device:read 允许只读者经代理删除设备。
			{method: http.MethodDelete, perm: "device:delete"},
			// POST /devices/{id}/provision 纳管：与本地 handleProvision
			//（server_devices.go:248）的 provision:execute 对齐。
			{method: http.MethodPost, pathSuffix: "/provision", perm: "provision:execute"},
		},
	},
	{
		domain:         "device",
		publicPrefix:   "/api/v1/device-svc/agents",
		upstreamPrefix: "/api/v1/agents",
		domainPrefix:   "/api/v1/device-svc/agents",
		envKey:         "DEVICE_SVC_URL",
		defaultURL:     "http://127.0.0.1:8081",
		perm:           "device:write", // 兜底：agent 注册/心跳等写操作
		permRules: []proxyPermRule{
			{method: http.MethodGet, perm: "device:read"},
		},
	},
	{
		domain:         "device",
		publicPrefix:   "/api/v1/device-svc/cmdb",
		upstreamPrefix: "/api/v1/cmdb",
		domainPrefix:   "/api/v1/device-svc/cmdb",
		envKey:         "DEVICE_SVC_URL",
		defaultURL:     "http://127.0.0.1:8081",
		perm:           "cmdb:write", // 兜底：CI 创建/更新/删除
		permRules: []proxyPermRule{
			// device-svc 的 cmdb 面只有 cis CRUD + relations 查询，无
			// changes/approve 子域（gateway.go:84-86）——cmdb:approve 不经此路径。
			{method: http.MethodGet, perm: "cmdb:read"},
		},
	},
	{
		domain:         "device",
		publicPrefix:   "/api/v1/device-svc/discovery",
		upstreamPrefix: "/api/v1/discovery",
		domainPrefix:   "/api/v1/device-svc/discovery",
		envKey:         "DEVICE_SVC_URL",
		defaultURL:     "http://127.0.0.1:8081",
		perm:           "network:write", // 兜底：POST /discovery/jobs 触发扫描
		permRules: []proxyPermRule{
			// discovery 子域的本地对应实现是 POST /api/v1/network/discover
			// （network.go handleNetworkDiscover，network:write）——权限映射跟随
			// 本地语义，而不是设备域权限：能触发网段扫描的凭证应与本地一致。
			{method: http.MethodGet, perm: "network:read"},
		},
	},
}

// taskProxyExtras task 域转发规则（TD-60 阶段 2「三域接通」：task-svc REST 网关接线）。
//
// 与 device 同型：task-svc 的 HTTP 网关消费租户上下文（extractAuth 读 X-Tenant-ID
// 头，且与 token 内 tenant_id 交叉校验），代理层鉴权后须注入该头再转发。
//
// 前缀设计 /api/v1/task-svc/*：controlplane 本地已有 /api/v1/tasks、/api/v1/schedules、
// /api/v1/approval/* 等同名 handler（server_lifecycle.go:33-34、:52 与 approval 注册），
// 同 mux 重复注册会 panic；双轨期新旧路径并存（旧=controlplane 本地实现，
// 新=task-svc 网关），切流阶段再评估替换。上游路径整段剥前缀：/api/v1/task-svc/tasks
// → /api/v1/tasks（task-svc 网关的路径与单体一致）。
//
// 权限映射逐条对齐单体本地要求（server_tasks.go / server_schedules.go /
// server_approval.go），双轨期「换一条路径不改变权限边界」：
//   - tasks:        GET → task:read；POST {id}/cancel → task:cancel；
//     POST {id}/approve|/reject → task:approve；其余写 → task:write
//   - schedules:    GET → schedule:read；其余（含 pause/resume）→ schedule:write
//   - approval:     GET → approval:read；POST {id}/approve|/reject → approval:approve；
//     其余 → approval:write
//
// 注意 task:approve / schedule:* / approval:* 这组权限点在接线时并不在 rbacPermSpecs
// 目录里（单体本地同样如此）——sim 取证暴露后已随 2026-09-29 目录补齐入册（15 项
// 批量修复，老库经并集回填自愈）；approve 类不属派生动作集，仍仅 admin 恒可。
// 此处映射永远如实镜像单体本地要求，不做「代理更宽松」或「代理更严格」的偏离。
var taskProxyExtras = []serviceProxyRule{
	{
		domain:         "task",
		publicPrefix:   "/api/v1/task-svc",
		upstreamPrefix: "/api/v1",
		domainPrefix:   "/api/v1/task-svc",
		envKey:         "TASK_SVC_URL",
		// 默认端口与 task-svc pkg/config 一致（8081，与 device-svc 默认相同——
		// 单机裸进程同跑两服务时需 env 覆盖区分；容器内 compose 已显式设置
		// TASK_SVC_URL=http://task-svc:8102）。
		defaultURL: "http://127.0.0.1:8081",
		perm:       "task:write", // 兜底：创建/触发类写操作
		permRules: []proxyPermRule{
			// schedules 子域（prefix 判别须先于下面的 task 后缀规则）。
			{method: http.MethodGet, pathPrefix: "/api/v1/schedules", perm: "schedule:read"},
			{pathPrefix: "/api/v1/schedules", perm: "schedule:write"},
			// approval 子域。approve/reject 的专属权限必须在通用 write 之前命中
			// （单体 server_approval.go:343/:379 要求 approval:approve）。
			{method: http.MethodGet, pathPrefix: "/api/v1/approval", perm: "approval:read"},
			{method: http.MethodPost, pathPrefix: "/api/v1/approval", pathSuffix: "/approve", perm: "approval:approve"},
			{method: http.MethodPost, pathPrefix: "/api/v1/approval", pathSuffix: "/reject", perm: "approval:approve"},
			{pathPrefix: "/api/v1/approval", perm: "approval:write"},
			// tasks 子域的专属动作（单体：cancel → task:cancel，approve/reject → task:approve）。
			{method: http.MethodPost, pathSuffix: "/cancel", perm: "task:cancel"},
			{method: http.MethodPost, pathSuffix: "/approve", perm: "task:approve"},
			{method: http.MethodPost, pathSuffix: "/reject", perm: "task:approve"},
			// 剩余 GET 全部只读（列表/详情/result/batch 状态/canary 状态）。
			{method: http.MethodGet, perm: "task:read"},
		},
	},
}

// lookupServiceProxyRule 按请求路径匹配转发规则（最长前缀语义由注册顺序保证：
// server_lifecycle.go 按本表顺序注册，ServeMux 自身按最长模式匹配）。
// device/task 域规则（*ProxyExtras）与五域共用同一匹配语义。
func lookupServiceProxyRule(path string) *serviceProxyRule {
	for _, group := range [][]serviceProxyRule{serviceProxyRules, deviceProxyExtras, taskProxyExtras} {
		for i := range group {
			r := &group[i]
			if r.publicPrefix == "" {
				continue
			}
			if path == r.publicPrefix || strings.HasPrefix(path, r.publicPrefix+"/") {
				return r
			}
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

// ── 自环防护（2026-09-26）────────────────────────────────────────────
//
// 出厂事故：docker-compose.prod.yml 从未设置 *_SVC_URL，代理回落到硬编码的
// localhost 默认值，其中 autoscaler/portal 的默认值是 127.0.0.1:8080——
// 那正是 controlplane 自己的 HTTP 监听端口。于是
//
//	/api/v1/portal/quotas → 改写成 /api/v1/quotas → 转发回自己 → 返回单体的数据
//
// 静默、无错、无日志，比 503 难查一个数量级。gpu/runbook/incident 的默认值
// 端口上没人在听，表现为 503，反而更容易发现——**故障的可见性差异掩盖了
// 同一个配置错误的两种表现**。
//
// 下面这个检查把「代理后端指向控制面自己」变成启动期的硬失败。

// isLoopbackHost 判断主机名是否指向本机回环（127.0.0.0/8、::1、localhost）。
func isLoopbackHost(host string) bool {
	h := strings.TrimSpace(host)
	if h == "" {
		return false
	}
	if strings.EqualFold(h, "localhost") {
		return true
	}
	if ip := net.ParseIP(strings.Trim(h, "[]")); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// proxySelfLoopTarget 检出「代理后端与控制面自身监听端口重合」的配置。
//
// 只在主机为回环时判定：跨主机/跨容器的同名端口是不同监听者，不构成自环
// （compose 里各服务用服务名寻址，如 http://gpu-svc:8107，天然不在此列）。
func proxySelfLoopTarget(target *url.URL, httpPort, grpcPort, metricsPort int) (int, bool) {
	if target == nil || !isLoopbackHost(target.Hostname()) {
		return 0, false
	}
	port := target.Port()
	if port == "" {
		return 0, false // 无显式端口按 80/443 解析，无法与监听端口比对
	}
	p := 0
	for _, c := range port {
		if c < '0' || c > '9' {
			return 0, false
		}
		p = p*10 + int(c-'0')
	}
	switch p {
	case httpPort:
		return httpPort, true
	case grpcPort:
		return grpcPort, true
	case metricsPort:
		return metricsPort, true
	}
	return 0, false
}

// validateServiceProxyTargets 启动期自检：返回所有自环配置的描述（无则 nil）。
//
// 为什么只是「记录」而不是「拒绝启动」：控制面单体还承载鉴权、任务下发、
// 设备纳管等全部核心流量。为了 GPU 域的转发地址写错就把整个控制面拖停，
// 是用五个功能的故障换全站故障——可用性优先于严格性。真正的兜底在请求侧：
// handleServiceProxy 命中自环时直接 503 并说明原因，绝不把请求转发回自己。
func validateServiceProxyTargets(httpPort, grpcPort, metricsPort int) []string {
	var problems []string
	for _, group := range [][]serviceProxyRule{serviceProxyRules, deviceProxyExtras, taskProxyExtras} {
		for i := range group {
			r := &group[i]
			if r.publicPrefix == "" {
				continue
			}
			if _, self := proxySelfLoopTarget(r.upstreamBase(), httpPort, grpcPort, metricsPort); self {
				src := r.defaultURL
				if r.envKey != "" {
					if v := os.Getenv(r.envKey); v != "" {
						src = r.envKey + "=" + v
					}
				}
				problems = append(problems, fmt.Sprintf(
					"域 %q 的转发后端 %s 指向控制面自身的监听端口（HTTP=%d gRPC=%d metrics=%d）："+
						"该域已按 503 隔离。容器化部署必须设置 %s 指向服务地址",
					r.domain, src, httpPort, grpcPort, metricsPort, r.envKey))
			}
		}
	}
	return problems
}

// ── 路由开关（2026-09-26）────────────────────────────────────────────
//
// 为什么必须先有开关：没有它，任何「切流」都是改二进制 + 重启的不可逆硬改，
// 出问题只能回滚版本重发。这是 TD-60 全部方案都卡住的同一个根因。
//
// 语义（OPSMESH_SERVICE_PROXY）：
//   - 未设置 / "on" / "all"  ：全部域转发（默认，保持既有行为）
//   - "off" / "none"        ：全部域停用，回落单体本地实现
//   - "gpu,portal"          ：仅列出的域停用，其余照常转发
//
// 停用的实现方式是**不注册该前缀的代理路由**，而不是在 handler 里返回 503：
// 这样请求自然落到单体自己的同名 handler（真正的回退），若单体本就没有该域
// 实现，mux 直接 404——这比「停用后返回一句 503」更诚实，也不会掩盖
// 「这个域根本没有单体实现」这一事实。

// serviceProxyEnvKey 路由开关的环境变量名。
const serviceProxyEnvKey = "OPSMESH_SERVICE_PROXY"

// parseDisabledProxyDomains 解析路由开关，返回需要停用的域集合。
//
// 无法识别的取值按「全部启用」处理并在错误里报出：宁可不切换，也不要因为
// 一个拼错的开关值把生产流量静默切走。
func parseDisabledProxyDomains(raw string) (map[string]bool, error) {
	disabled := map[string]bool{}
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "", "on", "all":
		return disabled, nil
	case "off", "none":
		for _, r := range allProxyRules() {
			disabled[r.domain] = true
		}
		return disabled, nil
	}
	for _, part := range strings.Split(v, ",") {
		d := strings.TrimSpace(part)
		if d == "" {
			continue
		}
		if !proxyDomainExists(d) {
			return nil, fmt.Errorf("%s 含未知域 %q（可用：%s）", serviceProxyEnvKey, d, strings.Join(proxyDomainNames(), "/"))
		}
		disabled[d] = true
	}
	return disabled, nil
}

// allProxyRules 返回三张规则表的全部条目（空条目已剔除）。
func allProxyRules() []serviceProxyRule {
	out := make([]serviceProxyRule, 0, len(serviceProxyRules)+len(deviceProxyExtras)+len(taskProxyExtras))
	for _, group := range [][]serviceProxyRule{serviceProxyRules, deviceProxyExtras, taskProxyExtras} {
		for i := range group {
			if group[i].publicPrefix != "" {
				out = append(out, group[i])
			}
		}
	}
	return out
}

// proxyDomainNames 返回全部域标识（去重、升序）。
func proxyDomainNames() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, r := range allProxyRules() {
		if !seen[r.domain] {
			seen[r.domain] = true
			out = append(out, r.domain)
		}
	}
	sort.Strings(out)
	return out
}

// proxyDomainExists 判断域标识是否已知。
func proxyDomainExists(d string) bool {
	for _, r := range allProxyRules() {
		if r.domain == d {
			return true
		}
	}
	return false
}

// disabledProxyDomains 读取并解析路由开关。解析失败时退化为「全部启用」，
// 由调用方在启动日志中显式报出——开关写错不应导致生产流量被静默切走。
func disabledProxyDomains() (map[string]bool, error) {
	return parseDisabledProxyDomains(os.Getenv(serviceProxyEnvKey))
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

// handleServiceProxy 五域统一代理 handler：
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
	// 权限点按「方法 + 路径」分级解析（permRules），与单体本地对同一操作的
	// 要求逐条对齐——例如 DELETE /device-svc/devices/{id} 要求 device:delete，
	// 而不是规则级兜底的 device:read。
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requirePermission(w, r, rule.resolvePerm(r.Method, r.URL.Path)); !ok {
		return
	}
	target := rule.upstreamBase()
	if target == nil {
		writeProxyErrorJSON(w, http.StatusServiceUnavailable, "service backend address invalid: "+rule.envKey)
		return
	}
	// 自环拦截：后端指向控制面自身时**必须**停在这里。
	// 放过去的后果是请求被改写后转发回本进程，命中单体自己的同名 handler，
	// 返回另一个子系统的数据且无任何错误信号（出厂即命中：portal 域的
	// 默认值 127.0.0.1:8080 正是控制面监听端口，/api/v1/portal/quotas
	// 会被改写成 /api/v1/quots 而返回单体的配额数据）。
	if port, self := proxySelfLoopTarget(target, s.httpPort, s.grpcPort, s.metricsPort); self {
		writeProxyErrorJSON(w, http.StatusServiceUnavailable, fmt.Sprintf(
			"service backend for %s points at the control plane itself (%s:%d); "+
				"set %s to the service address",
			rule.domain, target.Hostname(), port, rule.envKey))
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
		// device/task 域网关消费租户上下文（tenant.Middleware / extractAuth 读
		// X-Tenant-ID 头，且与 token 内 tenant_id 交叉校验）：聚合层已验证的
		// 租户身份注入头后再转发，防下游兜底 default 造成跨租户数据可见
		//（五域微服务不消费租户上下文，此头对它们无影响）。
		if ruleInjectsTenantHeader(rule) {
			req.Header.Set("X-Tenant-ID", actx.TenantID)
		}
	}
	proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, err error) {
		writeProxyErrorJSON(rw, http.StatusBadGateway, "service backend error: "+err.Error())
	}
	proxy.ServeHTTP(w, r)
}

// ruleInjectsTenantHeader 判断规则对应的微服务网关是否消费租户上下文
// （device/task 域的网关读 X-Tenant-ID 头；五域微服务不消费）。
func ruleInjectsTenantHeader(r *serviceProxyRule) bool {
	for _, group := range [][]serviceProxyRule{deviceProxyExtras, taskProxyExtras} {
		for i := range group {
			if &group[i] == r {
				return true
			}
		}
	}
	return false
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
