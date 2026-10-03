package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandleAlerts_EmptyListIsJSONArray 契约守护：空告警列表必须序列化为 [] 而非
// null。转正运行时巡检（2026-10-03）在真实栈上实测到本端点返回 null（incident-svc
// 同款缺陷），前端列表 v-for/.map() 会直接炸——此处固化契约防回潮。
func TestHandleAlerts_EmptyListIsJSONArray(t *testing.T) {
	s := newAlertsTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-User-Id", "u1")
	rec := httptest.NewRecorder()
	s.handleAlerts(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200：%s", rec.Code, rec.Body.String())
	}
	body := strings.TrimSpace(rec.Body.String())
	if body == "null" {
		t.Fatalf("空告警列表序列化成了 null，前端列表会崩")
	}
	if body != "[]" {
		t.Fatalf("空告警列表响应 = %s，期望 []", body)
	}
	var arr []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &arr); err != nil {
		t.Fatalf("响应不是合法 JSON 数组: %v", err)
	}
}
