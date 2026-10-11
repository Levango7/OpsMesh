// gateway_metrics.go API 网关路由级 Prometheus 埋点（TD-66 P3）。
//
// 为什么不用通用的 RecordHTTP：那套把路径归一化（数字段折叠、基数上限 2000），
// 网关的 `/gw/<path>` 天然带大量可变前缀，归一化会把不同路由折叠成同一个键——
// 路由级区分度全无。网关的**键是路由 ID**（管理员配的有界集合，天然有基数上限），
// 所以单独一套：counter（按 status 标签）+ gauge（最近一次延迟）。
//
// 时序形状：
//
//	opsmesh_gateway_requests_total{route,tenant,status}  counter（单调累加）
//	opsmesh_gateway_latency_ms{route,tenant}              gauge（最近一次）
package metrics

import (
	"sort"
	"strconv"
)

// RecordGatewayRequest 记录一次网关请求（counter + 最近延迟 gauge）。
// status 用 HTTP 状态码文本；latencyMs 为毫秒。
func (m *M) RecordGatewayRequest(route, tenant string, status int, latencyMs float64) {
	key := route + "|" + tenant + "|" + itoa(int64(status))
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.gatewayReqs == nil {
		m.gatewayReqs = make(map[string]int64)
	}
	if m.gatewayLat == nil {
		m.gatewayLat = make(map[string]float64)
	}
	m.gatewayReqs[key]++
	m.gatewayLat[route+"|"+tenant] = latencyMs
}

// appendGatewayMetrics 渲染网关时序（由 Render 调用；key 排序保证输出稳定）。
// appendGatewayMetrics 渲染网关时序。**调用方须已持锁**（与包内其余 append 系列
// 同一约定：Render 全程持锁，append 内再 Lock 即自锁死）。
func (m *M) appendGatewayMetrics(b []byte) []byte {
	keys := make([]string, 0, len(m.gatewayReqs))
	for k := range m.gatewayReqs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		route, tenant, status := splitGatewayReqKey(k)
		line := `opsmesh_gateway_requests_total{route="` + route +
			`",tenant="` + tenant + `",status="` + status + `"} ` + itoa(m.gatewayReqs[k]) + "\n"
		b = append(b, line...)
	}
	latKeys := make([]string, 0, len(m.gatewayLat))
	for k := range m.gatewayLat {
		latKeys = append(latKeys, k)
	}
	sort.Strings(latKeys)
	for _, k := range latKeys {
		route, tenant := splitGatewayLatKey(k)
		line := `opsmesh_gateway_latency_ms{route="` + route +
			`",tenant="` + tenant + `"} ` + fmtFloat(m.gatewayLat[k]) + "\n"
		b = append(b, line...)
	}
	return b
}

// splitGatewayReqKey 拆 route|tenant|status（渲染期用；tenant/route/status 均不含 |）。
func splitGatewayReqKey(k string) (route, tenant, status string) {
	parts := splitPipe(k, 3)
	if len(parts) < 3 {
		return ":unknown", ":unknown", "0"
	}
	return parts[0], parts[1], parts[2]
}

// splitGatewayLatKey 拆 route|tenant。
func splitGatewayLatKey(k string) (route, tenant string) {
	parts := splitPipe(k, 2)
	if len(parts) < 2 {
		return ":unknown", ":unknown"
	}
	return parts[0], parts[1]
}

// splitPipe 按分隔符切分（固定段数，超出段并入末段——路由 ID 本身不含 |）。
func splitPipe(s string, n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n-1; i++ {
		idx := indexPipe(s)
		if idx < 0 {
			return append(out, s)
		}
		out = append(out, s[:idx])
		s = s[idx+1:]
	}
	return append(out, s)
}

func indexPipe(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '|' {
			return i
		}
	}
	return -1
}

// fmtFloat 毫秒值的紧凑格式（strconv 最短表示，与包内其他渲染一致）。
func fmtFloat(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
