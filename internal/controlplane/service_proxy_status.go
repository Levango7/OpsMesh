// service_proxy_status.go — 微服务转发拓扑的只读视图。
//
// 为什么需要：TD-60 切流的第一个问题永远是「这个域现在到底走哪条路」。
// 出厂事故（2026-09-26）之所以能长期存在，正是因为转发拓扑没有任何可见出口：
// 出问题只能靠逐个点前端页面去撞，看日志也看不出请求被转给了谁。
//
// 本端点把「域 → 当前后端 → 启用状态 → 自检结论」一次性摊开，
// 使「GPU 域的请求现在到底由谁在处理」成为一条可查询的事实，
// 而不是需要推理的谜题。
package controlplane

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/controlplane/svcproxy"
	"github.com/Levango7/OpsMesh/internal/metrics"
)

// proxyDomainView 单个域的转发状态。
type proxyDomainView struct {
	Domain       string   `json:"domain"`            // 域标识（开关用的名字）
	PublicPrefix []string `json:"publicPrefixes"`    // 对外路径前缀
	EnvKey       string   `json:"envKey,omitempty"`  // 覆盖后端用的环境变量
	Backend      string   `json:"backend"`           // 实际生效的后端地址
	BackendFrom  string   `json:"backendFrom"`       // "env" 或 "default"
	Forwarded    bool     `json:"forwarded"`         // 当前是否真的转发到微服务
	Healthy      *bool    `json:"healthy,omitempty"` // 后端连通性（仅已启用的域探测）
	Status       string   `json:"status"`            // ok / self-loop / unreachable / disabled
	Note         string   `json:"note,omitempty"`    // 处置建议
}

// handleServiceRouting 处理 GET /api/v1/admin/service-routing。
//
// 权限：与 /api/v1/admin/* 其余端点同档（RBAC 派生规则下仅 admin）。
func (s *Server) handleServiceRouting(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := s.requireProd(w, r, levelPermission); !ok {
		return
	}
	disabled, switchErr := svcproxy.DisabledDomains()
	if switchErr != nil {
		// 与启动期一致：开关非法时按「全部启用」呈现，并把错误一并暴露。
		disabled = map[string]bool{}
	}

	groups := groupProxyDomains()
	order := make([]string, 0, len(groups))
	views := make([]proxyDomainView, 0, len(groups))
	for _, g := range groups {
		order = append(order, g.domain)
		backend, from := g.first.DefaultURL, "default"
		if g.first.EnvKey != "" {
			if v := os.Getenv(g.first.EnvKey); v != "" {
				backend, from = v, "env"
			}
		}
		v := proxyDomainView{
			Domain:       g.domain,
			PublicPrefix: g.prefixes,
			EnvKey:       g.first.EnvKey,
			Backend:      backend,
			BackendFrom:  from,
		}
		switch {
		case disabled[g.domain]:
			v.Status = "disabled"
			v.Note = "已按 " + svcproxy.EnvKey + " 停用转发，请求回落到单体本地实现"
		default:
			// 自环判定与 handleServiceProxy 用的是同一个函数，结论必然一致。
			if _, self := svcproxy.SelfLoopTarget(parseBackendURL(backend), s.httpPort, s.grpcPort, s.metricsPort); self {
				v.Status = "self-loop"
				v.Note = "后端指向控制面自身，该域已被 503 隔离（不转发、不返回单体数据）"
			} else if probeBackend(backend) {
				healthy := true
				v.Healthy, v.Status = &healthy, "ok"
			} else {
				healthy := false
				v.Healthy, v.Status = &healthy, "unreachable"
				v.Note = "后端不可达，请求会得到 503；确认服务已启动且 " + g.first.EnvKey + " 指向正确地址"
			}
			// Forwarded 的含义是「请求会被真正发往微服务」：自环被就地拦下、
			// 停用域不注册路由，两者都不算转发；unreachable 仍算（会尝试并 503）。
			v.Forwarded = v.Status == "ok" || v.Status == "unreachable"
		}
		views = append(views, v)
	}

	paginate.WriteJSON(w, http.StatusOK, map[string]any{
		"switch": map[string]any{
			"env":   svcproxy.EnvKey,
			"value": os.Getenv(svcproxy.EnvKey),
			"semantics": "未设置/on=all=全转发；off=none=全停用；或逗号分隔的域列表（可用域：" +
				joinDomains(order) + "）",
			"disabled": disabled,
		},
		"domains":   views,
		"switchErr": errText(switchErr),
	})
}

// proxyDomainGroup 转发路由表按域聚合后的一行。
// 同一域可能有多条规则（device 4 条、task 数条），后端地址取首条规则的
// envKey/defaultURL——同域各规则本就指向同一服务，首条即代表。
type proxyDomainGroup struct {
	domain   string
	prefixes []string
	first    svcproxy.Rule
}

// groupProxyDomains 按域聚合 svcproxy.AllRules()，保持域首次出现的顺序。
// service-routing 与 service-traffic 两个只读端点共用：「域→对外前缀」的派生
// 逻辑只此一份，新增域不会在某个端点里静默缺席。
func groupProxyDomains() []proxyDomainGroup {
	order := []string{}
	byDomain := map[string]*proxyDomainGroup{}
	for _, r := range svcproxy.AllRules() {
		g := byDomain[r.Domain]
		if g == nil {
			g = &proxyDomainGroup{domain: r.Domain, first: r}
			byDomain[r.Domain] = g
			order = append(order, r.Domain)
		}
		g.prefixes = append(g.prefixes, r.PublicPrefix)
	}
	out := make([]proxyDomainGroup, 0, len(order))
	for _, d := range order {
		out = append(out, *byDomain[d])
	}
	return out
}

// handleAdminServiceTraffic 处理 GET /api/v1/admin/service-traffic：逐域真实流量计数。
//
// 为什么需要：TD-60 §5.3 把五个「保留但需决策」域（gpu/portal/incident/runbook/
// autoscaler）的最终裁决定为「待真实流量」，但此前没有任何出口能回答这个问题——
// 只能翻访问日志或指望 Prometheus 已抓取，于是裁决一直悬着（§5.5 那五个被删的服务
// 之所以判得动，正因为有「从未进部署清单」这类静态事实；有对等实现的域必须看流量）。
// 数据其实一直在记（opsmesh_http_requests_total 按归一化路径记账），缺的只是按域
// 汇总这一步，所以这里只做只读聚合，不新增计数器。
//
// 权限：与 /api/v1/admin/* 其余只读端点同档（diagnostics:execute，operator 亦可）。
func (s *Server) handleAdminServiceTraffic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := s.requireProd(w, r, levelPermission); !ok {
		return
	}
	groups := groupProxyDomains()
	domains := make(map[string][]string, len(groups))
	for _, g := range groups {
		domains[g.domain] = g.prefixes
	}
	buckets := s.metrics.HTTPTrafficByPrefix(domains)
	if buckets == nil {
		// 序列化为 [] 而非 null：前端/脚本可直接遍历，无需判空。
		buckets = []metrics.TrafficBucket{}
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]any{
		"domains": buckets,
		"window":  "自控制面进程启动以来（进程内计数，重启归零）",
		"crossRestart": "跨重启/多副本取数走 PromQL：sum(increase(opsmesh_http_requests_total" +
			"{path=~\"/api/v1/<域前缀>(/.*)?\"}[7d]))",
		"note": "requests=0 是裁决证据而非缺数据——该域前缀自启动以来无任何请求。",
	})
}

// joinDomains 拼接域标识供错误提示使用。
func joinDomains(domains []string) string {
	out := ""
	for i, d := range domains {
		if i > 0 {
			out += "/"
		}
		out += d
	}
	return out
}

// parseBackendURL 解析后端地址字符串（非法返回 nil）。
func parseBackendURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil
	}
	return u
}

// probeBackend 探测后端 TCP 可达性。
//
// 只做「能不能连上」，不做任何请求：管理端点不该因为某个后端慢而把自己拖挂。
func probeBackend(raw string) bool {
	u := parseBackendURL(raw)
	if u == nil {
		return false
	}
	host := u.Host
	if u.Port() == "" {
		if u.Scheme == "https" {
			host = net.JoinHostPort(u.Hostname(), "443")
		} else {
			host = net.JoinHostPort(u.Hostname(), "80")
		}
	}
	conn, err := net.DialTimeout("tcp", host, 2*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// errText 把 error 转成字符串，nil 得空串。
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
