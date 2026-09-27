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
	disabled, switchErr := disabledProxyDomains()
	if switchErr != nil {
		// 与启动期一致：开关非法时按「全部启用」呈现，并把错误一并暴露。
		disabled = map[string]bool{}
	}

	// 按域聚合：同一域可能有多条规则（device 有 4 条）。
	type agg struct {
		prefixes []string
		envKey   string
		backend  string
		from     string
	}
	order := []string{}
	byDomain := map[string]*agg{}

	for _, r := range allProxyRules() {
		a := byDomain[r.domain]
		if a == nil {
			a = &agg{envKey: r.envKey, backend: r.defaultURL, from: "default"}
			if r.envKey != "" {
				if v := os.Getenv(r.envKey); v != "" {
					a.backend, a.from = v, "env"
				}
			}
			byDomain[r.domain] = a
			order = append(order, r.domain)
		}
		a.prefixes = append(a.prefixes, r.publicPrefix)
	}

	views := make([]proxyDomainView, 0, len(order))
	for _, d := range order {
		a := byDomain[d]
		v := proxyDomainView{
			Domain:       d,
			PublicPrefix: a.prefixes,
			EnvKey:       a.envKey,
			Backend:      a.backend,
			BackendFrom:  a.from,
		}
		switch {
		case disabled[d]:
			v.Status = "disabled"
			v.Note = "已按 " + serviceProxyEnvKey + " 停用转发，请求回落到单体本地实现"
		default:
			// 自环判定与 handleServiceProxy 用的是同一个函数，结论必然一致。
			if _, self := proxySelfLoopTarget(parseBackendURL(a.backend), s.httpPort, s.grpcPort, s.metricsPort); self {
				v.Status = "self-loop"
				v.Note = "后端指向控制面自身，该域已被 503 隔离（不转发、不返回单体数据）"
			} else if probeBackend(a.backend) {
				healthy := true
				v.Healthy, v.Status = &healthy, "ok"
			} else {
				healthy := false
				v.Healthy, v.Status = &healthy, "unreachable"
				v.Note = "后端不可达，请求会得到 503；确认服务已启动且 " + a.envKey + " 指向正确地址"
			}
			// Forwarded 的含义是「请求会被真正发往微服务」：自环被就地拦下、
			// 停用域不注册路由，两者都不算转发；unreachable 仍算（会尝试并 503）。
			v.Forwarded = v.Status == "ok" || v.Status == "unreachable"
		}
		views = append(views, v)
	}

	paginate.WriteJSON(w, http.StatusOK, map[string]any{
		"switch": map[string]any{
			"env":   serviceProxyEnvKey,
			"value": os.Getenv(serviceProxyEnvKey),
			"semantics": "未设置/on=all=全转发；off=none=全停用；或逗号分隔的域列表（可用域：" +
				joinDomains(order) + "）",
			"disabled": disabled,
		},
		"domains":   views,
		"switchErr": errText(switchErr),
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
