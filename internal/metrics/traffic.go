// traffic.go — 按路径前缀聚合的 HTTP 流量视图。
//
// 为什么单独成文件：TD-60「保留但需决策」五域的最终裁决要回答的是一个朴素问题
// ——`/api/v1/gpu` 这类前缀在真实部署里到底进来多少请求。数据早就有：
// opsmesh_http_requests_total 自始按 (method, 归一化 path, status) 记账
// （见 controlplane/server_middleware.go 的 httpMetricsMiddleware）。
// 缺的只是「按域汇总」这一步，所以这里只做只读聚合，不加新计数器——
// 新增采集面要么与中间件双写（同一请求计两次），要么改中间件签名，收益为零。
package metrics

import "sort"

// TrafficBucket 一个域（一组对外路径前缀）的流量画像。
type TrafficBucket struct {
	Domain   string            `json:"domain"`
	Prefixes []string          `json:"prefixes"`
	Requests uint64            `json:"requests"`
	ByStatus map[string]uint64 `json:"byStatus"`
	ByMethod map[string]uint64 `json:"byMethod"`
}

// HTTPTrafficByPrefix 聚合自进程启动以来命中给定前缀的 HTTP 请求。
//
// domains 为「域标识 → 路径前缀集合」，由调用方从转发路由表派生（路由表是唯一来源，
// 此处不接受硬编码列表，否则新增域会静默缺席报表）。
//
// 语义：
//   - 前缀匹配要求 `path == prefix` 或 `path` 以 `prefix+"/"` 开头——
//     与 mux/分级规则同一约定，故 `/api/v1/gpu` 不会把 `/api/v1/gpu-svc/x` 算进来；
//   - 多个前缀同时命中时取**最长**命中，嵌套域（如 `/api/v1/task-svc/tasks`
//     与 `/api/v1/tasks`）之间不重复计数；
//   - 基数熔断折叠出的 `path=":other"` 序列不计入任何域（归属已不可判定），
//     由 opsmesh_http_metrics_series_dropped_total 单独观测；
//   - 零流量域**照样出现在结果里**（Requests=0）——裁决要的正是"没有流量"这条证据。
//
// m 为 nil（未挂指标注册表的 Server，如部分单测构造体）时返回 nil。
func (m *M) HTTPTrafficByPrefix(domains map[string][]string) []TrafficBucket {
	if m == nil || len(domains) == 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	buckets := make(map[string]*TrafficBucket, len(domains))
	var prefixes []string
	for domain, ps := range domains {
		buckets[domain] = &TrafficBucket{
			Domain:   domain,
			Prefixes: append([]string(nil), ps...),
			ByStatus: map[string]uint64{},
			ByMethod: map[string]uint64{},
		}
		prefixes = append(prefixes, ps...)
	}
	// 按长度降序，保证最长命中优先归属。
	sort.Slice(prefixes, func(i, j int) bool { return len(prefixes[i]) > len(prefixes[j]) })
	domainOf := map[string]string{}
	for domain, ps := range domains {
		for _, p := range ps {
			domainOf[p] = domain
		}
	}

	for key, n := range m.httpReqs {
		method, path, status := splitHTTPKey(key)
		domain, ok := matchPrefixDomain(prefixes, domainOf, path)
		if !ok {
			continue
		}
		b := buckets[domain]
		b.Requests += n
		b.ByStatus[status] += n
		b.ByMethod[method] += n
	}

	out := make([]TrafficBucket, 0, len(buckets))
	for _, domain := range sortedDomains(buckets) {
		out = append(out, *buckets[domain])
	}
	return out
}

// matchPrefixDomain 返回 path 归属的域（最长前缀命中；无命中 ok=false）。
func matchPrefixDomain(prefixes []string, domainOf map[string]string, path string) (string, bool) {
	for _, p := range prefixes {
		if path == p || (len(path) > len(p) && path[:len(p)+1] == p+"/") {
			return domainOf[p], true
		}
	}
	return "", false
}

// sortedDomains 按域名字典序输出，保证响应体稳定（测试可直接断言下标）。
func sortedDomains(buckets map[string]*TrafficBucket) []string {
	out := make([]string, 0, len(buckets))
	for d := range buckets {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}
