package controlplane

// list_envelope_contract_test.go — issue #96 列表信封契约门禁（tasks 端点）。
//
// 现状（earlier history + Kilo 批次42 已收敛）：dev/alerts/tenants 的 opt-in 信封
// 已有专门测试（pagination_optin_test.go / server_alerts_contract_test.go /
// acceptance_hardening_test.go）。tasks 此前无专门门禁，本文件补齐：
//   - 传 page/pageSize → 统一信封 {data,total,page,pageSize,hasMore}；
//   - 空集数据键必须是 [] 而非 null（前端 v-for/.map 依赖）。
// 不传 page 保持裸数组（向后兼容，前端已按此解析）——破坏性默认切换需 owner 裁决。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func assertEnvelopeKeys(t *testing.T, body []byte, keys ...string) map[string]json.RawMessage {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		t.Fatalf("decode envelope: %v; body=%s", err, body)
	}
	for _, k := range keys {
		if _, ok := obj[k]; !ok {
			t.Errorf("envelope missing key %q; body=%s", k, body)
		}
	}
	return obj
}

func TestListEnvelopeTasksOptIn(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks?page=1&pageSize=5", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleListTasks(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	obj := assertEnvelopeKeys(t, rec.Body.Bytes(), "data", "total", "page", "pageSize", "hasMore")
	if string(obj["page"]) != "1" || string(obj["pageSize"]) != "5" {
		t.Errorf("page/pageSize wrong: %s", rec.Body.String())
	}
}

func TestListEnvelopeTasksEmptyIsArray(t *testing.T) {
	// 空集 opt-in：data 键必须是 [] 而非 null（前端 v-for/.map 依赖）。
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks?page=1", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleListTasks(rec, req)
	obj := assertEnvelopeKeys(t, rec.Body.Bytes(), "data")
	if string(obj["data"]) == "null" {
		t.Errorf("empty data key is null, want []: %s", rec.Body.String())
	}
}
