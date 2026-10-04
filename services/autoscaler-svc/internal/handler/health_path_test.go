package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/evaluator"
	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/k8s"
	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/service"
)

// TestHealthPathContract 门禁（TD-77）：健康端点口径必须在 12 个微服务间可逐字对齐。
//
// 规范：存活 /health，就绪 /ready。历史路径 /api/v1/health 保留为别名，
// 两者必须指向同一 handler——所以这里断言的不只是「都返回 200」，
// 而是「两条路径的响应体逐字节相同」。若哪天有人把别名改成独立实现导致
// 两者行为漂移（例如别名少了鉴权或少了字段），本断言即红。
func TestHealthPathContract(t *testing.T) {
	ts, _, _ := newTestServer(t)
	c := ts.Client()

	// 规范路径必须存在。
	for _, path := range []string{"/health", "/ready"} {
		resp, err := c.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("规范路径 %s 期望 200，实得 %d", path, resp.StatusCode)
		}
		resp.Body.Close()
	}

	// 别名与规范路径必须同响应。
	pairs := [][2]string{{"/health", "/api/v1/health"}}
	for _, p := range pairs {
		canonical, alias := p[0], p[1]
		var canonicalBody, aliasBody string
		for _, spec := range []struct {
			path string
			dst  *string
		}{{canonical, &canonicalBody}, {alias, &aliasBody}} {
			resp, err := c.Get(ts.URL + spec.path)
			if err != nil {
				t.Fatalf("GET %s: %v", spec.path, err)
			}
			b, _ := io.ReadAll(resp.Body)
			*spec.dst = string(b)
			if resp.StatusCode != http.StatusOK {
				t.Errorf("%s 期望 200，实得 %d", spec.path, resp.StatusCode)
			}
			resp.Body.Close()
		}
		if string(canonicalBody) != string(aliasBody) {
			t.Errorf("别名与规范路径响应不一致：%s=%q vs %s=%q",
				canonical, canonicalBody, alias, aliasBody)
		}
	}
}

// TestHealthRejectsNonGet 保证健康端点只接受 GET——探针语义如此，
// 放行其他方法会让「GET 返回 200」这类断言失去意义。
func TestHealthRejectsNonGet(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	eng := evaluator.NewEvaluator(func() time.Time { return now })
	svc := service.NewService(eng, &mockMetricsReader{values: map[string]float64{}}, k8s.NewClient())
	mux := http.NewServeMux()
	NewHandler(svc).RegisterRoutes(mux)

	for _, path := range []string{"/health", "/ready", "/api/v1/health"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s 期望 405，实得 %d", path, rec.Code)
		}
	}
}
