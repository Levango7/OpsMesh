// traffic_test.go — 按域流量聚合的取数语义（TD-60 逐域裁决的证据出口）。
//
// 这些断言锁定的是裁决赖以成立的四条口径：前缀边界不吃相邻域、嵌套前缀不重复计数、
// 零流量域必须显式出现、基数折叠流量不得被错归到某个域。
package metrics

import "testing"

func trafficDomains() map[string][]string {
	return map[string][]string{
		"gpu":    {"/api/v1/gpu"},
		"portal": {"/api/v1/portal"},
		"device": {"/api/v1/device-svc/devices", "/api/v1/device-svc/agents"},
	}
}

func findBucket(t *testing.T, buckets []TrafficBucket, domain string) TrafficBucket {
	t.Helper()
	for _, b := range buckets {
		if b.Domain == domain {
			return b
		}
	}
	t.Fatalf("结果缺少域 %q（零流量域也必须出现）: %+v", domain, buckets)
	return TrafficBucket{}
}

func TestHTTPTrafficByPrefixCountsDomain(t *testing.T) {
	m := New()
	m.RecordHTTP("GET", "/api/v1/gpu/nodes", "200", 0.01)
	m.RecordHTTP("GET", "/api/v1/gpu/nodes", "200", 0.02)
	m.RecordHTTP("POST", "/api/v1/gpu/workloads", "201", 0.03)
	// 非代理域流量不应被算进任何桶。
	m.RecordHTTP("GET", "/api/v1/devices", "200", 0.01)

	buckets := m.HTTPTrafficByPrefix(trafficDomains())
	gpu := findBucket(t, buckets, "gpu")
	if gpu.Requests != 3 {
		t.Errorf("gpu Requests = %d, want 3", gpu.Requests)
	}
	if gpu.ByStatus["200"] != 2 || gpu.ByStatus["201"] != 1 {
		t.Errorf("gpu ByStatus = %v, want {200:2, 201:1}", gpu.ByStatus)
	}
	if gpu.ByMethod["GET"] != 2 || gpu.ByMethod["POST"] != 1 {
		t.Errorf("gpu ByMethod = %v, want {GET:2, POST:1}", gpu.ByMethod)
	}
}

// TestHTTPTrafficZeroDomainPresent 裁决口径的核心：requests=0 是证据，不是缺数据。
func TestHTTPTrafficZeroDomainPresent(t *testing.T) {
	m := New()
	m.RecordHTTP("GET", "/api/v1/gpu/nodes", "200", 0.01)

	buckets := m.HTTPTrafficByPrefix(trafficDomains())
	if len(buckets) != 3 {
		t.Fatalf("桶数 = %d, want 3（含无流量的 portal/device）: %+v", len(buckets), buckets)
	}
	portal := findBucket(t, buckets, "portal")
	if portal.Requests != 0 || len(portal.ByStatus) != 0 {
		t.Errorf("portal 应为显式零流量: %+v", portal)
	}
	// 输出按域名升序，脚本可直接按下标取。
	if buckets[0].Domain != "device" || buckets[1].Domain != "gpu" || buckets[2].Domain != "portal" {
		t.Errorf("域顺序未按字典序: %v", []string{buckets[0].Domain, buckets[1].Domain, buckets[2].Domain})
	}
}

// TestHTTPTrafficPrefixBoundary 前缀必须是完整路径段命中：
// /api/v1/gpu 不得把 /api/v1/gpu-svc/... 算进来（否则相邻域之间会互相吞并）。
func TestHTTPTrafficPrefixBoundary(t *testing.T) {
	m := New()
	m.RecordHTTP("GET", "/api/v1/gpu/nodes", "200", 0.01)
	m.RecordHTTP("GET", "/api/v1/gpu", "200", 0.01)
	m.RecordHTTP("GET", "/api/v1/gpu-svc/nodes", "200", 0.01)
	m.RecordHTTP("GET", "/api/v1/gpumall/nodes", "200", 0.01)

	gpu := findBucket(t, m.HTTPTrafficByPrefix(trafficDomains()), "gpu")
	if gpu.Requests != 2 {
		t.Errorf("gpu Requests = %d, want 2（仅 /api/v1/gpu 与 /api/v1/gpu/... ；相邻前缀域不应计入）", gpu.Requests)
	}
}

// TestHTTPTrafficMultiplePrefixesPerDomain device 域有 4 条规则（此处取 2 条），
// 同域多前缀应合并成一个桶。
func TestHTTPTrafficMultiplePrefixesPerDomain(t *testing.T) {
	m := New()
	m.RecordHTTP("GET", "/api/v1/device-svc/devices", "200", 0.01)
	m.RecordHTTP("POST", "/api/v1/device-svc/agents/register", "201", 0.01)

	dev := findBucket(t, m.HTTPTrafficByPrefix(trafficDomains()), "device")
	if dev.Requests != 2 {
		t.Errorf("device Requests = %d, want 2（两条前缀合并）", dev.Requests)
	}
	if dev.ByMethod["GET"] != 1 || dev.ByMethod["POST"] != 1 {
		t.Errorf("device ByMethod = %v", dev.ByMethod)
	}
}

// TestHTTPTrafficLongestPrefixWins 嵌套前缀之间不重复计数：
// /api/v1/gpu/nodes 归更具体的 gpuNodes，其余 gpu 子路径归 gpu。
func TestHTTPTrafficLongestPrefixWins(t *testing.T) {
	m := New()
	m.RecordHTTP("GET", "/api/v1/gpu/nodes", "200", 0.01)
	m.RecordHTTP("GET", "/api/v1/gpu/clusters", "200", 0.01)

	domains := map[string][]string{
		"gpu":      {"/api/v1/gpu"},
		"gpuNodes": {"/api/v1/gpu/nodes"},
	}
	buckets := m.HTTPTrafficByPrefix(domains)
	if g := findBucket(t, buckets, "gpu").Requests; g != 1 {
		t.Errorf("gpu Requests = %d, want 1（更具体的 nodes 路径不应重复计入宽前缀）", g)
	}
	if n := findBucket(t, buckets, "gpuNodes").Requests; n != 1 {
		t.Errorf("gpuNodes Requests = %d, want 1", n)
	}
}

// TestHTTPTrafficIgnoresFoldedPath 基数熔断折叠出的 path=":other" 归属已不可判定，
// 不应被算进任何域（该部分由 opsmesh_http_metrics_series_dropped_total 单独观测）。
func TestHTTPTrafficIgnoresFoldedPath(t *testing.T) {
	m := New()
	m.IncHTTPRequest("GET", otherPathLabel, "200")
	m.IncHTTPRequest("GET", "/api/v1/gpu/nodes", "200")

	gpu := findBucket(t, m.HTTPTrafficByPrefix(trafficDomains()), "gpu")
	if gpu.Requests != 1 {
		t.Errorf("gpu Requests = %d, want 1（:other 折叠流量不计入）", gpu.Requests)
	}
}

func TestHTTPTrafficNilReceiver(t *testing.T) {
	var m *M
	if got := m.HTTPTrafficByPrefix(trafficDomains()); got != nil {
		t.Errorf("nil 注册表应返回 nil, got %v", got)
	}
	if got := New().HTTPTrafficByPrefix(map[string][]string{}); got != nil {
		t.Errorf("空前缀表应返回 nil, got %v", got)
	}
}
