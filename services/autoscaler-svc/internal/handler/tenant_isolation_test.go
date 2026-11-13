package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// doAs 发一个带指定租户头的请求（空租户 = 不发头，用于验证 401）。
func doAs(ts *httptest.Server, tenant, method, path, body string) *httptest.ResponseRecorder {
	// 注意：必须用 http.NewRequest（client 语义）；httptest.NewRequest 会设置 RequestURI，
	// client.Do 会直接拒绝（"Request.RequestURI can't be set in client requests"）。
	req, err := http.NewRequest(method, ts.URL+path, bytes.NewBufferString(body))
	if err != nil {
		panic(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if tenant != "" {
		req.Header.Set("X-Tenant-ID", tenant)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	rec := httptest.NewRecorder()
	rec.Code = resp.StatusCode
	_, _ = rec.Body.ReadFrom(resp.Body)
	return rec
}

// TestTenantHeaderRequired 无租户头 ⇒ 401（D4：不兜底 default）。
func TestTenantHeaderRequired(t *testing.T) {
	ts, _, _ := newTestServer(t)
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/rules", ""},
		{http.MethodPost, "/api/v1/evaluate", `{}`},
		{http.MethodGet, "/api/v1/decisions", ""},
		{http.MethodGet, "/api/v1/cooldowns", ""},
	} {
		rec := doAs(ts, "", tc.method, tc.path, tc.body)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s 无租户头应 401，实得 %d", tc.method, tc.path, rec.Code)
		}
	}
}

// TestCrossTenantIsolationRules：A 建的规则，B 读不到、列不到、删不掉。
func TestCrossTenantIsolationRules(t *testing.T) {
	ts, _, _ := newTestServer(t)

	body := `{"id":"r-iso","name":"iso","deployment":"web-app","namespace":"default","metric":"cpu",` +
		`"scaleUpThreshold":80,"scaleDownThreshold":20,"minReplicas":1,"maxReplicas":10,"enabled":true}`
	rec := doAs(ts, "A", http.MethodPost, "/api/v1/rules", body)
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("A 建规则失败: %d %s", rec.Code, rec.Body.String())
	}

	// B 列列表 ⇒ 0 条
	rec = doAs(ts, "B", http.MethodGet, "/api/v1/rules", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("B 列表应 200，实得 %d", rec.Code)
	}
	var list []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("列表不是合法 JSON 数组: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("B 的列表应 0 条，实得 %d（跨租户可见）", len(list))
	}

	// B 删 A 的规则 ⇒ 404
	rec = doAs(ts, "B", http.MethodDelete, "/api/v1/rules/r-iso", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("B 删 A 的规则应 404，实得 %d", rec.Code)
	}

	// A 自己列表 ⇒ 1 条
	rec = doAs(ts, "A", http.MethodGet, "/api/v1/rules", "")
	var listA []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &listA); err != nil {
		t.Fatalf("A 列表解析失败: %v", err)
	}
	if len(listA) != 1 {
		t.Fatalf("A 的列表应 1 条，实得 %d", len(listA))
	}
}
