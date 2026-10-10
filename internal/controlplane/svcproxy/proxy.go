// proxy.go 微服务聚合代理的规则引擎（TD-87 批 2：自父包 service_proxy.go 迁入）。
//
// 组成：规则类型（Rule/PermRule）+ 三张规则表（Rules/DeviceExtras/TaskExtras）+
// 匹配/权限解析/路径改写/自环检测/启动校验等纯逻辑。
// 父包保留 handler（handleServiceProxy）、错误响应与 JSON 转义助手，以及全部 Server 级测试。
//
// 注：Rule/PermRule 的字段为导出形态（Domain/PublicPrefix/EnvKey/Method/PermRules…），
// 因为父包的域分组（groupProxyDomains）与多份测试直接读这些字段断言路由表——
// 这是「数据即接口」的一类：表本身就是被断言的对象。
package svcproxy

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
)

// Rule 一条微服务转发规则。
type Rule struct {
	// domain 域标识（开关 OPSMESH_SERVICE_PROXY 用），如 gpu / portal / device。
	Domain string
	// publicPrefix 聚合层对外路径前缀（mux 注册用），如 /api/v1/gpu。
	PublicPrefix string
	// upstreamPrefix 后端服务真实路径前缀。publicPrefix 剥去 domainPrefix
	// 后的剩余部分拼到 upstreamPrefix 之后（路径改写规则见 rewriteProxyPath）。
	UpstreamPrefix string
	// domainPrefix publicPrefix 中需剥掉的域前缀（剩余子路径转发时保留）。
	// 例：publicPrefix=/api/v1/autoscaler，domainPrefix=/api/v1/autoscaler，
	// 请求 /api/v1/autoscaler/rules/123 → 后端 /api/v1/rules/123。
	DomainPrefix string
	// envKey 覆盖后端地址的环境变量名（空=不可覆盖，恒用 defaultURL）。
	EnvKey string
	// defaultURL 默认后端地址（本机默认端口；与各 svc pkg/config 默认值一致）。
	DefaultURL string
	// perm 聚合层鉴权所需权限（requirePermission）——permRules 未命中时的兜底。
	Perm string
	// permRules 域内权限分级（可选）：按声明顺序 first-match，未命中回落 perm。
	// 用于同一转发前缀下不同操作对应不同权限点的场景（如 tasks 的
	// GET/POST/cancel/approve 分别是 task:read/task:write/task:cancel/task:approve）。
	PermRules []PermRule
	// forwardCookie 是否把客户端会话 Cookie 透传给上游（默认 false=剥除）。
	// **只有 auth 域（凭证签发方）可为 true**，静态守卫
	// TestCredentialForwardingRestrictedToAuthDomain 会拦其他域：
	//   - 常规域：下游微服务不消费会话 Cookie，身份走聚合层注入头（X-Tenant-ID /
	//     X-User-Id），剥除防凭证意外落地到内部服务的访问日志；
	//   - auth 域：auth-svc 网关是**自验 token 模型**（bearerOrCookie → ValidateToken），
	//     不读注入身份头；剥 Cookie ⇒ 每个到达它的请求都无凭无据恒 401
	//     （2026-10-10 双轨冒烟实测，td60-auth-data-plane-proposal.md §9.2）。
	// 方向裁决见该提案 §9.3 方向 A（仅对 auth 域放行凭证，不引入新的信任面）。
	ForwardCookie bool
}

// PermRule 域内权限分级规则：按「方法 + 子路径前缀/后缀」把请求映射到权限点。
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
type PermRule struct {
	Method     string // 空=任意方法
	PathPrefix string // 空=任意路径（按上游路径）；非空要求 path == prefix 或 HasPrefix(path, prefix+"/")
	PathSuffix string // 空=不校验；非空要求 HasSuffix(path, suffix)
	Perm       string // 命中后要求持有的权限点
}

// matches 判定请求（方法 + 完整路径）是否命中本条分级规则。
func (pr *PermRule) Matches(method, path string) bool {
	if pr.Method != "" && pr.Method != method {
		return false
	}
	if pr.PathPrefix != "" && path != pr.PathPrefix && !strings.HasPrefix(path, pr.PathPrefix+"/") {
		return false
	}
	if pr.PathSuffix != "" && !strings.HasSuffix(path, pr.PathSuffix) {
		return false
	}
	return true
}

// isActive 双轨机制（TD-60 A-2 auth-svc）：环境开关控制代理接线。
// AUTH_SVC_PROXY_ENABLED 默认 false → auth-svc 代理规则不活跃（控制面本地处理 /api/v1/auth/*）；
// 设 true → auth-svc 代理规则活跃（/api/v1/auth-svc 前缀→/api/v1/auth）；
// AUTH_SVC_PROXY_RATIO（默认 50）控制 50/50 对比比例（机制准备，运行时报告留下一轮）。
func (r *Rule) IsActive() bool {
	if r.Domain == "auth" {
		return os.Getenv("AUTH_SVC_PROXY_ENABLED") == "true"
	}
	return true
}

// resolvePerm 解析本次请求所需的权限点：permRules 首条命中优先，否则回落 perm。
// 入参 path 为**公开请求路径**（如 /api/v1/task-svc/schedules）；匹配前先经
// rewriteProxyPath 改写为上游路径（permRules 的路径字段按上游形态书写）。
func (r *Rule) ResolvePerm(method, path string) string {
	upstream := r.RewriteProxyPath(path)
	for i := range r.PermRules {
		if r.PermRules[i].Matches(method, upstream) {
			return r.PermRules[i].Perm
		}
	}
	return r.Perm
}

// PermAuthenticated 规则权限哨兵：**仅要求已认证，不校验权限点**。
//
// 用于控制面本地本就不查权限的自服务端点（auth 域 me/refresh/logout/change-password ——
// 本地 handler 只做 token 校验，见 auth_login.go 的 handleAuthMe/handleAuthRefresh）。
//
// 为什么必须有这个形态（2026-10-10 端到端冒烟实测的阻断级缺陷）：代理层给这些端点写的
// 权限点若在控制面权限目录（internal/store/model/perm.go 的 rbacPermSpecs）里不存在，
// requirePermission 的判据「用户权限集 ∋ required」恒为假 ⇒ **含 admin 在内的任何身份都过不了**，
// 双轨开关打开即认证面恒 403。此前的 auth:read/auth:write 正是这种「目录外权限点」。
//
// 以 @ 开头：与真实权限点（`domain:action`）不可能撞名，便于门禁静态识别
// （见 perm_catalog_test.go：三张规则表的每个 Perm 必须 ∈ 目录 ∪ 本哨兵）。
const PermAuthenticated = "@authenticated"

// Rules 五域转发映射表。端口依据各服务 pkg/config 默认值：
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
var Rules = []Rule{
	{
		Domain:         "gpu",
		PublicPrefix:   "/api/v1/gpu",
		UpstreamPrefix: "/api/v1/gpu",
		DomainPrefix:   "/api/v1/gpu",
		EnvKey:         "GPU_SVC_URL",
		DefaultURL:     "http://127.0.0.1:8090",
		Perm:           "gpu:write",
		PermRules: []PermRule{
			{Method: http.MethodGet, Perm: "gpu:read"},
		},
	},
	{
		Domain:         "runbook",
		PublicPrefix:   "/api/v1/runbooks",
		UpstreamPrefix: "/api/v1/runbooks",
		DomainPrefix:   "/api/v1/runbooks",
		EnvKey:         "RUNBOOK_SVC_URL",
		DefaultURL:     "http://127.0.0.1:8082",
		Perm:           "runbook:write",
		PermRules: []PermRule{
			{Method: http.MethodGet, Perm: "runbook:read"},
		},
	},
	{
		Domain:         "incident",
		PublicPrefix:   "/api/v1/incidents",
		UpstreamPrefix: "/api/v1/incidents",
		DomainPrefix:   "/api/v1/incidents",
		EnvKey:         "INCIDENT_SVC_URL",
		DefaultURL:     "http://127.0.0.1:8082",
		Perm:           "incident:write",
		PermRules: []PermRule{
			{Method: http.MethodGet, Perm: "incident:read"},
		},
	},
	{
		// autoscaler-svc 路径不含域前缀（/api/v1/rules 而非 /api/v1/autoscaler/rules，
		// 见 services/autoscaler-svc/internal/handler/handler.go:24-28），需路径改写。
		Domain:         "autoscaler",
		PublicPrefix:   "/api/v1/autoscaler",
		UpstreamPrefix: "/api/v1",
		DomainPrefix:   "/api/v1/autoscaler",
		EnvKey:         "AUTOSCALER_SVC_URL",
		DefaultURL:     "http://127.0.0.1:8080",
		Perm:           "autoscaler:write",
		PermRules: []PermRule{
			{Method: http.MethodGet, Perm: "autoscaler:read"},
		},
	},
	{
		// portal-svc 同理：/api/v1/requests 而非 /api/v1/portal/requests
		// （见 services/portal-svc/internal/handler/handler.go:27-34）。
		Domain:         "portal",
		PublicPrefix:   "/api/v1/portal",
		UpstreamPrefix: "/api/v1",
		DomainPrefix:   "/api/v1/portal",
		EnvKey:         "PORTAL_SVC_URL",
		DefaultURL:     "http://127.0.0.1:8080",
		Perm:           "portal:write", // 含审批动作（approve/reject 为 POST）
		PermRules: []PermRule{
			{Method: http.MethodGet, Perm: "portal:read"},
		},
	},
	{
		// auth-svc（TD-60 A-2：auth-svc 双轨对比/切流）。controlplane 本地已有
		// /api/v1/auth/* 实现（login/register/me/logout/refresh/change-password），
		// auth-svc 网关同路径提供 /api/v1/auth/*（login/register/me/logout/
		// refresh/change-password + users/roles/permissions 管理）。
		// 双轨期用 /api/v1/auth-svc/* 前缀转发到 auth-svc 的 /api/v1/auth/*，
		// 与 controlplane 本地 /api/v1/auth/* 并存；切流阶段再评估替换。
		Domain:         "auth",
		PublicPrefix:   "/api/v1/auth-svc",
		UpstreamPrefix: "/api/v1/auth",
		DomainPrefix:   "/api/v1/auth-svc",
		EnvKey:         "AUTH_SVC_URL",
		DefaultURL:     "http://127.0.0.1:8081",
		Perm:           PermAuthenticated, // 兜底：auth 域自服务端点只认证不查权限（与本地 handler 语义对齐）
		ForwardCookie:  true,              // 唯一允许值：auth-svc 自验 token（bearerOrCookie），剥 Cookie 即恒 401（§9.2/§9.3 方向 A）
		PermRules: []PermRule{
			{Method: http.MethodGet, PathPrefix: "/api/v1/auth/me", Perm: PermAuthenticated},
			{Method: http.MethodPost, PathPrefix: "/api/v1/auth/login", Perm: PermAuthenticated},
			{Method: http.MethodPost, PathPrefix: "/api/v1/auth/register", Perm: PermAuthenticated},
			{Method: http.MethodPost, PathPrefix: "/api/v1/auth/logout", Perm: PermAuthenticated},
			{Method: http.MethodPost, PathPrefix: "/api/v1/auth/refresh", Perm: PermAuthenticated},
			{Method: http.MethodPost, PathPrefix: "/api/v1/auth/change-password", Perm: PermAuthenticated},
		},
	},
}

// DeviceExtras device 域（D1/D3 后 device-svc 已有完整 REST 网关）额外转发规则。
// 与 Rules 分表的原因：device 域的网关直连 store 层、鉴权走
// tenant.Middleware（X-Tenant-ID 头或 JWT）——代理层完成鉴权后统一剥离客户端身份头
// 并以已校验身份重注入再转发（见 handleServiceProxy 的 director）。
//
// 路径设计：publicPrefix 用 /api/v1/device-svc 域前缀（剥去后拼回 /api/v1/*），
// 如 /api/v1/device-svc/devices → 后端 /api/v1/devices。不能用 /api/v1/devices
// 直转——controlplane 本地已有同名 handler（server_lifecycle.go:25-34），
// 同一 mux 重复注册会 panic；双轨期新旧路径并存（旧=controlplane 本地实现，
// 新=device-svc 网关），切流阶段再评估替换。
// bootstrap 端点（/install.sh、/api/v1/provision/register）不经代理
// （agent 自举直连 device-svc）。
var DeviceExtras = []Rule{
	{
		Domain:         "device",
		PublicPrefix:   "/api/v1/device-svc/devices",
		UpstreamPrefix: "/api/v1/devices",
		DomainPrefix:   "/api/v1/device-svc/devices",
		EnvKey:         "DEVICE_SVC_URL",
		DefaultURL:     "http://127.0.0.1:8081",
		Perm:           "device:write", // 兜底：注册/更新/心跳等写操作
		PermRules: []PermRule{
			{Method: http.MethodGet, Perm: "device:read"},
			// DELETE /devices/{id} 退役：与本地 handleRetireDevice（server_devices.go:218）
			// 的 device:delete 对齐——此前统一 device:read 允许只读者经代理删除设备。
			{Method: http.MethodDelete, Perm: "device:delete"},
			// POST /devices/{id}/provision 纳管：与本地 handleProvision
			//（server_devices.go:248）的 provision:execute 对齐。
			{Method: http.MethodPost, PathSuffix: "/provision", Perm: "provision:execute"},
		},
	},
	{
		Domain:         "device",
		PublicPrefix:   "/api/v1/device-svc/agents",
		UpstreamPrefix: "/api/v1/agents",
		DomainPrefix:   "/api/v1/device-svc/agents",
		EnvKey:         "DEVICE_SVC_URL",
		DefaultURL:     "http://127.0.0.1:8081",
		Perm:           "device:write", // 兜底：agent 注册/心跳等写操作
		PermRules: []PermRule{
			{Method: http.MethodGet, Perm: "device:read"},
		},
	},
	{
		Domain:         "device",
		PublicPrefix:   "/api/v1/device-svc/cmdb",
		UpstreamPrefix: "/api/v1/cmdb",
		DomainPrefix:   "/api/v1/device-svc/cmdb",
		EnvKey:         "DEVICE_SVC_URL",
		DefaultURL:     "http://127.0.0.1:8081",
		Perm:           "cmdb:write", // 兜底：CI 创建/更新/删除
		PermRules: []PermRule{
			// device-svc 的 cmdb 面只有 cis CRUD + relations 查询，无
			// changes/approve 子域（gateway.go:84-86）——cmdb:approve 不经此路径。
			{Method: http.MethodGet, Perm: "cmdb:read"},
		},
	},
	{
		Domain:         "device",
		PublicPrefix:   "/api/v1/device-svc/discovery",
		UpstreamPrefix: "/api/v1/discovery",
		DomainPrefix:   "/api/v1/device-svc/discovery",
		EnvKey:         "DEVICE_SVC_URL",
		DefaultURL:     "http://127.0.0.1:8081",
		Perm:           "network:write", // 兜底：POST /discovery/jobs 触发扫描
		PermRules: []PermRule{
			// discovery 子域的本地对应实现是 POST /api/v1/network/discover
			// （network.go handleNetworkDiscover，network:write）——权限映射跟随
			// 本地语义，而不是设备域权限：能触发网段扫描的凭证应与本地一致。
			{Method: http.MethodGet, Perm: "network:read"},
		},
	},
}

// TaskExtras task 域转发规则（TD-60 阶段 2「三域接通」：task-svc REST 网关接线）。
//
// 与 device 同型：task-svc 的 HTTP 网关消费租户上下文（extractAuth 读 X-Tenant-ID
// 头，且与 token 内 tenant_id 交叉校验），代理层鉴权后统一剥离重注入身份头再转发。
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
var TaskExtras = []Rule{
	{
		Domain:         "task",
		PublicPrefix:   "/api/v1/task-svc",
		UpstreamPrefix: "/api/v1",
		DomainPrefix:   "/api/v1/task-svc",
		EnvKey:         "TASK_SVC_URL",
		// 默认端口与 task-svc pkg/config 一致（8081，与 device-svc 默认相同——
		// 单机裸进程同跑两服务时需 env 覆盖区分；容器内 compose 已显式设置
		// TASK_SVC_URL=http://task-svc:8102）。
		DefaultURL: "http://127.0.0.1:8081",
		Perm:       "task:write", // 兜底：创建/触发类写操作
		PermRules: []PermRule{
			// schedules 子域（prefix 判别须先于下面的 task 后缀规则）。
			{Method: http.MethodGet, PathPrefix: "/api/v1/schedules", Perm: "schedule:read"},
			{PathPrefix: "/api/v1/schedules", Perm: "schedule:write"},
			// approval 子域。approve/reject 的专属权限必须在通用 write 之前命中
			// （单体 server_approval.go:343/:379 要求 approval:approve）。
			{Method: http.MethodGet, PathPrefix: "/api/v1/approval", Perm: "approval:read"},
			{Method: http.MethodPost, PathPrefix: "/api/v1/approval", PathSuffix: "/approve", Perm: "approval:approve"},
			{Method: http.MethodPost, PathPrefix: "/api/v1/approval", PathSuffix: "/reject", Perm: "approval:approve"},
			{PathPrefix: "/api/v1/approval", Perm: "approval:write"},
			// tasks 子域的专属动作（单体：cancel → task:cancel，approve/reject → task:approve）。
			{Method: http.MethodPost, PathSuffix: "/cancel", Perm: "task:cancel"},
			{Method: http.MethodPost, PathSuffix: "/approve", Perm: "task:approve"},
			{Method: http.MethodPost, PathSuffix: "/reject", Perm: "task:approve"},
			// 剩余 GET 全部只读（列表/详情/result/batch 状态/canary 状态）。
			{Method: http.MethodGet, Perm: "task:read"},
		},
	},
}

// Lookup 按请求路径匹配转发规则（最长前缀语义由注册顺序保证：
// server_lifecycle.go 按本表顺序注册，ServeMux 自身按最长模式匹配）。
// device/task 域规则（*ProxyExtras）与五域共用同一匹配语义。
func Lookup(path string) *Rule {
	for _, group := range [][]Rule{Rules, DeviceExtras, TaskExtras} {
		for i := range group {
			r := &group[i]
			if r.PublicPrefix == "" {
				continue
			}
			if path == r.PublicPrefix || strings.HasPrefix(path, r.PublicPrefix+"/") {
				return r
			}
		}
	}
	return nil
}

// upstreamBase 解析后端地址：envKey 覆盖优先，缺省 defaultURL。
func (r *Rule) UpstreamBase() *url.URL {
	raw := r.DefaultURL
	if r.EnvKey != "" {
		if v := os.Getenv(r.EnvKey); v != "" {
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

// IsLoopbackHost 判断主机名是否指向本机回环（127.0.0.0/8、::1、localhost）。
func IsLoopbackHost(host string) bool {
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

// SelfLoopTarget 检出「代理后端与控制面自身监听端口重合」的配置。
//
// 只在主机为回环时判定：跨主机/跨容器的同名端口是不同监听者，不构成自环
// （compose 里各服务用服务名寻址，如 http://gpu-svc:8107，天然不在此列）。
func SelfLoopTarget(target *url.URL, httpPort, grpcPort, metricsPort int) (int, bool) {
	if target == nil || !IsLoopbackHost(target.Hostname()) {
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

// ValidateTargets 启动期自检：返回所有自环配置的描述（无则 nil）。
//
// 为什么只是「记录」而不是「拒绝启动」：控制面单体还承载鉴权、任务下发、
// 设备纳管等全部核心流量。为了 GPU 域的转发地址写错就把整个控制面拖停，
// 是用五个功能的故障换全站故障——可用性优先于严格性。真正的兜底在请求侧：
// handleServiceProxy 命中自环时直接 503 并说明原因，绝不把请求转发回自己。
func ValidateTargets(httpPort, grpcPort, metricsPort int) []string {
	var problems []string
	for _, group := range [][]Rule{Rules, DeviceExtras, TaskExtras} {
		for i := range group {
			r := &group[i]
			if r.PublicPrefix == "" {
				continue
			}
			if _, self := SelfLoopTarget(r.UpstreamBase(), httpPort, grpcPort, metricsPort); self {
				src := r.DefaultURL
				if r.EnvKey != "" {
					if v := os.Getenv(r.EnvKey); v != "" {
						src = r.EnvKey + "=" + v
					}
				}
				problems = append(problems, fmt.Sprintf(
					"域 %q 的转发后端 %s 指向控制面自身的监听端口（HTTP=%d gRPC=%d metrics=%d）："+
						"该域已按 503 隔离。容器化部署必须设置 %s 指向服务地址",
					r.Domain, src, httpPort, grpcPort, metricsPort, r.EnvKey))
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

// EnvKey 路由开关的环境变量名。
const EnvKey = "OPSMESH_SERVICE_PROXY"

// ParseDisabledProxyDomains 解析路由开关，返回需要停用的域集合。
//
// 无法识别的取值按「全部启用」处理并在错误里报出：宁可不切换，也不要因为
// 一个拼错的开关值把生产流量静默切走。
func ParseDisabledProxyDomains(raw string) (map[string]bool, error) {
	disabled := map[string]bool{}
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "", "on", "all":
		return disabled, nil
	case "off", "none":
		for _, r := range AllRules() {
			disabled[r.Domain] = true
		}
		return disabled, nil
	}
	for _, part := range strings.Split(v, ",") {
		d := strings.TrimSpace(part)
		if d == "" {
			continue
		}
		if !DomainExists(d) {
			return nil, fmt.Errorf("%s 含未知域 %q（可用：%s）", EnvKey, d, strings.Join(DomainNames(), "/"))
		}
		disabled[d] = true
	}
	return disabled, nil
}

// AllRules 返回三张规则表的全部条目（空条目已剔除）。
func AllRules() []Rule {
	out := make([]Rule, 0, len(Rules)+len(DeviceExtras)+len(TaskExtras))
	for _, group := range [][]Rule{Rules, DeviceExtras, TaskExtras} {
		for i := range group {
			if group[i].PublicPrefix != "" {
				out = append(out, group[i])
			}
		}
	}
	return out
}

// DomainNames 返回全部域标识（去重、升序）。
func DomainNames() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, r := range AllRules() {
		if !seen[r.Domain] {
			seen[r.Domain] = true
			out = append(out, r.Domain)
		}
	}
	sort.Strings(out)
	return out
}

// DomainExists 判断域标识是否已知。
func DomainExists(d string) bool {
	for _, r := range AllRules() {
		if r.Domain == d {
			return true
		}
	}
	return false
}

// DisabledDomains 读取并解析路由开关。解析失败时退化为「全部启用」，
// 由调用方在启动日志中显式报出——开关写错不应导致生产流量被静默切走。
func DisabledDomains() (map[string]bool, error) {
	return ParseDisabledProxyDomains(os.Getenv(EnvKey))
}

// rewriteProxyPath 把聚合层路径改写为后端真实路径：
// 剥 domainPrefix，剩余子路径拼到 upstreamPrefix 之后。
// 例：/api/v1/autoscaler/rules/123 → /api/v1/rules/123。
func (r *Rule) RewriteProxyPath(path string) string {
	rest := strings.TrimPrefix(path, r.DomainPrefix)
	if rest == "" || rest == "/" {
		return r.UpstreamPrefix
	}
	// rest 形如 /rules/123（TrimPrefix 保留斜杠开头）。
	return r.UpstreamPrefix + rest
}
