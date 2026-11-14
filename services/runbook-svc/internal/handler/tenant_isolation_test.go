package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// doAs 发一个带指定租户头的请求（空租户 = 不发头，用于验证 401）。
func doAs(mux *http.ServeMux, tenant, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if tenant != "" {
		req.Header.Set("X-Tenant-ID", tenant)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestTenantHeaderRequired 无租户头 ⇒ 401（D4：不兜底 default）。
func TestTenantHeaderRequired(t *testing.T) {
	mux := newTestServer()
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/runbooks", ""},
		{http.MethodGet, "/api/v1/runbooks/rb-x", ""},
		{http.MethodPost, "/api/v1/runbooks", `{"name":"x","content":"echo x"}`},
	} {
		rec := doAs(mux, "", tc.method, tc.path, tc.body)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s 无租户头应 401，实得 %d", tc.method, tc.path, rec.Code)
		}
	}
}

// TestCrossTenantIsolation：A 建的手册，B 读不到、列不到。
func TestCrossTenantIsolation(t *testing.T) {
	mux := newTestServer()

	rec := doAs(mux, "A", http.MethodPost, "/api/v1/runbooks", `{"name":"A 的手册","content":"echo A"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("A 建手册失败: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("解析创建响应失败: %v", err)
	}

	// B 读详情 ⇒ 404
	if rec := doAs(mux, "B", http.MethodGet, "/api/v1/runbooks/"+created.ID, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("B 读 A 的手册应 404，实得 %d", rec.Code)
	}
	// B 列表 ⇒ 0 条（且仍为 JSON 数组）
	rec = doAs(mux, "B", http.MethodGet, "/api/v1/runbooks", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("B 列表应 200，实得 %d", rec.Code)
	}
	var list []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("列表不是合法 JSON 数组: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("B 的列表应 0 条，实得 %d（跨租户可见）", len(list))
	}
	// A 自己列表 ⇒ 1 条
	rec = doAs(mux, "A", http.MethodGet, "/api/v1/runbooks", "")
	var listA []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&listA); err != nil {
		t.Fatalf("A 列表解析失败: %v", err)
	}
	if len(listA) != 1 {
		t.Fatalf("A 的列表应 1 条，实得 %d", len(listA))
	}
}

// TestWebhookRequiresTenantInPayload：webhook 是外部触发，租户必须由调用方在**体里**声明；
// 缺失即 400（不靠网关头兜底——外部系统不走租户网关那套）。
func TestWebhookRequiresTenantInPayload(t *testing.T) {
	mux := newTestServer()
	rec := doAs(mux, "", http.MethodPost, "/api/v1/trigger/webhook", `{"event_type":"alert","source":"prometheus"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("webhook 缺 tenant_id 应 400，实得 %d: %s", rec.Code, rec.Body.String())
	}
}
