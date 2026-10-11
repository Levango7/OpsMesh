package metrics

import (
	"strings"
	"testing"
	"time"
)

// TestGatewayMetricsRendered /metrics 输出包含 gateway_requests_total 与
// gateway_latency_ms，且 route/tenant/status 标签正确（TD-66 P3 验收标准第三条）。
func TestGatewayMetricsRendered(t *testing.T) {
	m := New()
	m.RecordGatewayRequest("r-1", "default", 200, 12.5)
	m.RecordGatewayRequest("r-1", "default", 500, 3.0)
	m.RecordGatewayRequest("r-1", "default", 200, 9.0)

	out := m.Render()
	for _, want := range []string{
		`opsmesh_gateway_requests_total{route="r-1",tenant="default",status="200"} 2`,
		`opsmesh_gateway_requests_total{route="r-1",tenant="default",status="500"} 1`,
		`opsmesh_gateway_latency_ms{route="r-1",tenant="default"} 9`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("渲染缺少 %q\nrender 输出片段：\n%s", want, firstMatch(out, "opsmesh_gateway"))
		}
	}
}

// TestGatewayMetricsZeroSeriesAfterInit 空注册表不应输出网关时序（absent 与 0 的
// PromQL 语义由 handler 层统一，不由这里伪造 0 值序列）。
func TestGatewayMetricsZeroSeriesAfterInit(t *testing.T) {
	m := New()
	if out := m.Render(); strings.Contains(out, "opsmesh_gateway_") {
		t.Errorf("空表不应渲染网关时序")
	}
}

// TestGatewayMetricsRenderNotDeadlocked Render 全程持锁——append 系列不得再 Lock
// （首版即在 appendGatewayMetrics 内 m.mu.Lock()，测试 1m40s 超时死锁）。
// 用带超时的方式验证：能返回即不死锁。
func TestGatewayMetricsRenderNotDeadlocked(t *testing.T) {
	done := make(chan string, 1)
	go func() {
		m := New()
		m.RecordGatewayRequest("r-x", "t-x", 200, 1)
		done <- m.Render()
	}()
	select {
	case out := <-done:
		if !strings.Contains(out, "opsmesh_gateway_requests_total") {
			t.Error("渲染结果应含网关时序")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Render 5s 未返回——疑似 append 内重复加锁（自锁）")
	}
}

// TestGatewayMetricsTenantAndRouteSeparation 不同租户/路由的时序互不覆盖。
func TestGatewayMetricsTenantAndRouteSeparation(t *testing.T) {
	m := New()
	m.RecordGatewayRequest("r-a", "t1", 200, 1)
	m.RecordGatewayRequest("r-a", "t2", 200, 1)
	m.RecordGatewayRequest("r-b", "t1", 200, 1)
	out := m.Render()
	if strings.Count(out, "opsmesh_gateway_requests_total{") != 3 {
		t.Errorf("应有 3 条独立时序: %s", firstMatch(out, "opsmesh_gateway_requests_total"))
	}
}

// firstMatch 提取渲染输出中含 need 的行（断言失败时给上下文）。
func firstMatch(out, need string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, need) {
			return line
		}
	}
	return "(无)"
}
