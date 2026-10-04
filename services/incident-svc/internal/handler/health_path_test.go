package handler

import (
	"io"
	"net/http"
	"testing"
)

// TestHealthPathContract 门禁（TD-77）：健康端点口径必须在 12 个微服务间可逐字对齐。
//
// 本服务的 /ready 由 cmd 层注册（不在 RegisterRoutes 内），所以这里只用
// RegisterRoutes 能覆盖的路径断言存活与别名；就绪路径由 compose/helm 的
// 探针配置侧保证（统一后 12 个服务的 readiness 都指向必然存在的路径）。
//
// 规范：存活 /health。历史路径 /api/v1/health 保留为别名，两者必须指向同一
// handler——这里断言的不只是「都返回 200」，而是「响应体逐字节相同」。
// 若有人把别名改成独立实现导致行为漂移，本断言即红。
func TestHealthPathContract(t *testing.T) {
	ts := newTestServer(t)
	t.Cleanup(ts.Close)
	c := ts.Client()

	// 规范路径必须存在。
	resp, err := c.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	canonicalBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("规范路径 /health 期望 200，实得 %d", resp.StatusCode)
	}

	// 别名与规范路径必须同响应。
	resp, err = c.Get(ts.URL + "/api/v1/health")
	if err != nil {
		t.Fatalf("GET /api/v1/health: %v", err)
	}
	aliasBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("别名 /api/v1/health 期望 200，实得 %d", resp.StatusCode)
	}

	if string(canonicalBody) != string(aliasBody) {
		t.Errorf("别名与规范路径响应不一致：/health=%q vs /api/v1/health=%q",
			canonicalBody, aliasBody)
	}
}

// TestHealthRejectsNonGet 保证健康端点只接受 GET——探针语义如此，
// 放行其他方法会让「GET 返回 200」这类断言失去意义。
func TestHealthRejectsNonGet(t *testing.T) {
	ts := newTestServer(t)
	t.Cleanup(ts.Close)

	for _, path := range []string{"/health", "/api/v1/health"} {
		req, err := http.NewRequest(http.MethodPost, ts.URL+path, nil)
		if err != nil {
			t.Fatalf("new request %s: %v", path, err)
		}
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("POST %s 期望 405，实得 %d", path, resp.StatusCode)
		}
		resp.Body.Close()
	}
}
