package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestTenantHeaderRequired —— D4（issue #27 裁决②）的入口钉子：
// 租户上下文缺失一律 401，**不兜底 default**。无主请求不许落到任何租户数据上。
func TestTenantHeaderRequired(t *testing.T) {
	ts := newTestServer(t)
	paths := []string{
		"/api/v1/incidents",
		"/api/v1/incidents/inc-1",
		"/api/v1/incidents/inc-1/timeline",
		"/api/v1/incidents/metrics",
	}
	for _, path := range paths {
		req, err := http.NewRequest(http.MethodGet, ts.URL+path, nil)
		if err != nil {
			t.Fatalf("new request %s: %v", path, err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do %s: %v", path, err)
		}
		code := resp.StatusCode
		resp.Body.Close()
		if code != http.StatusUnauthorized {
			t.Fatalf("%s 无租户头应 401，实得 %d", path, code)
		}
	}
}

// TestCrossTenantIsolation —— A（t1）建的单，B 用自己的租户头读不到、列不到。
// 这条与存储层钉子（models.TestMemoryStoreTenantIsolation）互为上下游：
// 前者证明"入口按租户取数"，后者证明"数据面按租户隔离"。
func TestCrossTenantIsolation(t *testing.T) {
	ts := newTestServer(t)
	id := createTestIncident(t, ts, map[string]interface{}{"title": "A 的单"})

	// B 读详情 ⇒ 404（不区分"没有"与"不是你的"）
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/incidents/"+id, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("X-Tenant-ID", "B")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do detail: %v", err)
	}
	code := resp.StatusCode
	resp.Body.Close()
	if code != http.StatusNotFound {
		t.Fatalf("B 读 A 的单应 404，实得 %d", code)
	}

	// B 列列表 ⇒ 不含该单（且契约仍是 JSON 数组）
	req2, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/incidents", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req2.Header.Set("X-Tenant-ID", "B")
	rec := httptest.NewRecorder()
	// 直接走 mux 以便拿到 recorder（newTestServer 返回的是真实 server，这里复用它的语义即可）
	_ = rec
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("do list: %v", err)
	}
	defer resp2.Body.Close()
	var arr []map[string]any
	if err := json.NewDecoder(resp2.Body).Decode(&arr); err != nil {
		t.Fatalf("列表不是合法 JSON 数组: %v", err)
	}
	if len(arr) != 0 {
		t.Fatalf("B 的列表应 0 条，实得 %d（跨租户可见）", len(arr))
	}
}
