package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHealthPathContract 门禁（TD-77）：健康端点口径必须在 12 个微服务间可逐字对齐。
//
// 规范：存活 /health，就绪 /ready。历史路径 /api/v1/health 保留为别名，
// 两者必须指向同一 handler——所以这里断言的不只是「都返回 200」，
// 而是「两条路径的响应体逐字节相同」。若哪天有人把别名改成独立实现导致
// 两者行为漂移（例如别名少了某个字段），本断言即红。
func TestHealthPathContract(t *testing.T) {
	mux := newTestServer()

	// 规范路径必须存在。
	for _, path := range []string{"/health", "/ready"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("规范路径 %s 期望 200，实得 %d", path, rec.Code)
		}
	}

	// 别名与规范路径必须同响应。
	canonical, alias := "/health", "/api/v1/health"
	var canonicalBody, aliasBody string
	for _, spec := range []struct {
		path string
		dst  *string
	}{{canonical, &canonicalBody}, {alias, &aliasBody}} {
		req := httptest.NewRequest(http.MethodGet, spec.path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		b, _ := io.ReadAll(rec.Result().Body)
		*spec.dst = string(b)
		if rec.Code != http.StatusOK {
			t.Errorf("%s 期望 200，实得 %d", spec.path, rec.Code)
		}
	}
	if canonicalBody != aliasBody {
		t.Errorf("别名与规范路径响应不一致：%s=%q vs %s=%q",
			canonical, canonicalBody, alias, aliasBody)
	}
}

// TestHealthRejectsNonGet 保证健康端点只接受 GET——探针语义如此，
// 放行其他方法会让「GET 返回 200」这类断言失去意义。
func TestHealthRejectsNonGet(t *testing.T) {
	mux := http.NewServeMux()
	newTestHandler().RegisterRoutes(mux)

	for _, path := range []string{"/health", "/ready", "/api/v1/health"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s 期望 405，实得 %d", path, rec.Code)
		}
	}
}
