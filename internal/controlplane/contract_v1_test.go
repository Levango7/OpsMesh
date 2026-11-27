package controlplane

// contract_v1_test.go — v1 默认响应形态契约门禁（docs/api-versioning-policy.md §4）。
//
// 这些测试直接调用真实 handler，钉住两类客户端可见形态：
//   - 无 page/pageSize：v1 历史默认形态（顶层键集合冻结）；
//   - 显式 page/pageSize：opt-in 形态（统一信封或 users/tenants 的历史例外）。
//
// 顶层键集合的增/删/重命名在 v1 内都属于 breaking（§2.3），
// 资源对象内部新增字段不受此门禁限制（§2.1）。断言只看键集合与类型，不看 JSON 顺序。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

// ---------------------------------------------------------------------------
// 断言辅助
// ---------------------------------------------------------------------------

// contractServe 按既有测试风格直接调用 handler：租户经 X-Tenant-ID 注入，
// 需要 RBAC 的端点携带 loginAsAdmin 返回的 Authorization。
func contractServe(h http.HandlerFunc, target, tenant, auth string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if tenant != "" {
		req.Header.Set("X-Tenant-ID", tenant)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func contractMustOK(t *testing.T, rec *httptest.ResponseRecorder) []byte {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	return rec.Body.Bytes()
}

func contractObject(t *testing.T, body []byte) map[string]json.RawMessage {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		t.Fatalf("decode JSON object: %v; body=%s", err, body)
	}
	if obj == nil {
		t.Fatalf("body is not a JSON object: %s", body)
	}
	return obj
}

func contractArray(t *testing.T, body []byte) []json.RawMessage {
	t.Helper()
	var arr []json.RawMessage
	if err := json.Unmarshal(body, &arr); err != nil {
		t.Fatalf("decode JSON array: %v; body=%s", err, body)
	}
	if arr == nil {
		t.Fatalf("body is not a JSON array: %s", body)
	}
	return arr
}

func contractAssertJSONArray(t *testing.T, label string, raw json.RawMessage) {
	t.Helper()
	if string(raw) == "null" {
		t.Errorf("%s must encode as [], not null", label)
		return
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		t.Errorf("%s is not a JSON array: %v; raw=%s", label, err, raw)
	}
}

// contractAssertExactKeys 断言顶层键集合完全相等：缺失键与多余键都判红。
func contractAssertExactKeys(t *testing.T, label string, obj map[string]json.RawMessage, want ...string) {
	t.Helper()
	got := make([]string, 0, len(obj))
	for k := range obj {
		got = append(got, k)
	}
	sort.Strings(got)
	wantSorted := append([]string(nil), want...)
	sort.Strings(wantSorted)
	if !reflect.DeepEqual(got, wantSorted) {
		t.Errorf("%s top-level keys = %v, want %v (v1 key set is frozen)", label, got, wantSorted)
	}
}

func contractAssertIntField(t *testing.T, label string, obj map[string]json.RawMessage, key string) {
	t.Helper()
	raw, ok := obj[key]
	if !ok {
		return
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		t.Errorf("%s.%s must be an int, got %s", label, key, raw)
	}
}

// contractAssertEnvelope 钉住统一信封的精确五键：缺 total/page/pageSize/hasMore 或
// 多出任何键都判红；data 必须是数组且空集编码为 []。
func contractAssertEnvelope(t *testing.T, label string, body []byte) map[string]json.RawMessage {
	t.Helper()
	obj := contractObject(t, body)
	contractAssertExactKeys(t, label+" envelope", obj, "data", "total", "page", "pageSize", "hasMore")
	if raw, ok := obj["data"]; ok {
		contractAssertJSONArray(t, label+".data", raw)
	}
	contractAssertIntField(t, label, obj, "total")
	contractAssertIntField(t, label, obj, "page")
	contractAssertIntField(t, label, obj, "pageSize")
	if raw, ok := obj["hasMore"]; ok {
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			t.Errorf("%s.hasMore must be a bool, got %s", label, raw)
		}
	}
	return obj
}

// contractAssertLegacyContainer 钉住 users/tenants 的默认形态：顶层只有旧容器键。
func contractAssertLegacyContainer(t *testing.T, label string, body []byte, container string) {
	t.Helper()
	obj := contractObject(t, body)
	contractAssertExactKeys(t, label+" legacy", obj, container)
	if raw, ok := obj[container]; ok {
		contractAssertJSONArray(t, label+"."+container, raw)
	}
}

// contractAssertPaginatedContainer 钉住 users/tenants 的分页历史例外：
// 旧容器键 + total/page/pageSize/hasMore，且绝不出现 data（docs/api-pagination.md:77-87）。
func contractAssertPaginatedContainer(t *testing.T, label string, body []byte, container string) {
	t.Helper()
	obj := contractObject(t, body)
	contractAssertExactKeys(t, label+" opt-in", obj, container, "total", "page", "pageSize", "hasMore")
	if _, ok := obj["data"]; ok {
		t.Errorf("%s opt-in must not contain data; documented exception keeps the %q container", label, container)
	}
	if raw, ok := obj[container]; ok {
		contractAssertJSONArray(t, label+"."+container, raw)
	}
	contractAssertIntField(t, label, obj, "total")
	contractAssertIntField(t, label, obj, "page")
	contractAssertIntField(t, label, obj, "pageSize")
	if raw, ok := obj["hasMore"]; ok {
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			t.Errorf("%s.hasMore must be a bool, got %s", label, raw)
		}
	}
}

// ---------------------------------------------------------------------------
// 测试数据
// ---------------------------------------------------------------------------

func seedContractDevices(s *Server) {
	for _, d := range []struct{ id, segment, tenant string }{
		{"contract-dev-a", "seg-a", "t1"},
		{"contract-dev-b", "seg-b", "t1"},
		{"contract-dev-t2", "seg-a", "t2"},
	} {
		s.store.UpsertDevice(&proto.DeviceInfo{
			DeviceID: d.id, Segment: d.segment, TenantID: d.tenant, State: "online", Managed: true,
		})
	}
}

func seedContractAgents(s *Server) {
	for _, a := range []struct{ host, segment, tenant string }{
		{"contract-host-a", "seg-a", "t1"},
		{"contract-host-b", "seg-b", "t1"},
		{"contract-host-t2", "seg-a", "t2"},
	} {
		s.store.Register(&proto.AgentInfo{Hostname: a.host, Segment: a.segment, TenantID: a.tenant})
	}
}

func seedContractTasks(s *Server) {
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	if a == nil {
		return
	}
	s.store.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "echo contract"})
}

func seedContractAudits(s *Server) {
	s.store.Audit(&proto.AuditEvent{
		TenantID:  "t1",
		Action:    "contract_probe",
		Target:    "contract-audit-1",
		Detail:    "seed",
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
}

func seedContractTenants(s *Server) {
	for _, id := range []string{"contract-tenant-a", "contract-tenant-b"} {
		s.store.CreateTenant(&store.Tenant{ID: id, Name: id})
	}
}

// ---------------------------------------------------------------------------
// 默认形态（无 page/pageSize）
// ---------------------------------------------------------------------------

func TestContractV1DefaultShapes(t *testing.T) {
	cases := []struct {
		name   string
		call   func(t *testing.T) *httptest.ResponseRecorder
		assert func(t *testing.T, body []byte)
	}{
		{
			// 文档 §4 #2：tasks 默认是裸数组；pageSize 单独存在不触发信封。
			name: "tasks_bare_array",
			call: func(t *testing.T) *httptest.ResponseRecorder {
				s := newTestServer()
				seedContractTasks(s)
				return contractServe(s.handleListTasks, "/api/v1/tasks", "t1", "")
			},
			assert: func(t *testing.T, body []byte) {
				arr := contractArray(t, body)
				if len(arr) == 0 {
					t.Fatalf("tasks default array is empty; fixture missing: %s", body)
				}
				for _, raw := range arr {
					elem := contractObject(t, raw)
					contractAssertExactKeys(t, "tasks element", elem,
						"taskID", "agentID", "tenantID", "type", "command", "content", "path", "status", "claimedBy", "claimedAt", "claimEpoch", "createdAt", "retryCount", "maxRetries", "deadLetter", "schedule", "parentID", "lastFiredAt", "dependsOn")
				}
			},
		},
		{
			// 文档 §4 #1：devices 默认按 segment 分组，不是信封。
			name: "devices_segment_map",
			call: func(t *testing.T) *httptest.ResponseRecorder {
				s := newTestServer()
				seedContractDevices(s)
				return contractServe(s.handleDevices, "/api/v1/devices", "t1", "")
			},
			assert: func(t *testing.T, body []byte) {
				obj := contractObject(t, body)
				contractAssertExactKeys(t, "devices legacy", obj, "seg-a", "seg-b")
				for segment, raw := range obj {
					contractAssertJSONArray(t, "devices."+segment, raw)
				}
			},
		},
		{
			// 文档 §4 #9：agents 默认是裸数组，元素六键固定。
			name: "agents_bare_array_element_keys",
			call: func(t *testing.T) *httptest.ResponseRecorder {
				s := newTestServer()
				seedContractAgents(s)
				return contractServe(s.handleAgents, "/api/v1/agents", "t1", "")
			},
			assert: func(t *testing.T, body []byte) {
				arr := contractArray(t, body)
				if len(arr) == 0 {
					t.Fatalf("agents default array is empty; fixture missing: %s", body)
				}
				for _, raw := range arr {
					elem := contractObject(t, raw)
					contractAssertExactKeys(t, "agents element", elem,
						"agentID", "hostname", "segment", "status", "lastSeen", "live")
				}
			},
		},
		{
			// 文档 §4 #4：users 默认只有 {users:[...]}，不得出现 total/page 等控制键。
			name: "users_container_only",
			call: func(t *testing.T) *httptest.ResponseRecorder {
				s := newAuthTestServer(t)
				auth := loginAsAdmin(t, s)
				return contractServe(s.handleUsers, "/api/v1/users", "", auth)
			},
			assert: func(t *testing.T, body []byte) {
				contractAssertLegacyContainer(t, "users", body, "users")
			},
		},
		{
			// 文档 §4 #5：tenants 默认只有 {tenants:[...]}。
			name: "tenants_container_only",
			call: func(t *testing.T) *httptest.ResponseRecorder {
				s := newAuthTestServer(t)
				auth := loginAsAdmin(t, s)
				seedContractTenants(s)
				return contractServe(s.handleTenants, "/api/v1/tenants", "", auth)
			},
			assert: func(t *testing.T, body []byte) {
				contractAssertLegacyContainer(t, "tenants", body, "tenants")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.assert(t, contractMustOK(t, tc.call(t)))
		})
	}
}

// ---------------------------------------------------------------------------
// opt-in 形态（page/pageSize）
// ---------------------------------------------------------------------------

func TestContractV1OptInShapes(t *testing.T) {
	cases := []struct {
		name   string
		call   func(t *testing.T) *httptest.ResponseRecorder
		assert func(t *testing.T, body []byte)
	}{
		{
			name: "tasks_page_envelope",
			call: func(t *testing.T) *httptest.ResponseRecorder {
				s := newTestServer()
				seedContractTasks(s)
				return contractServe(s.handleListTasks, "/api/v1/tasks?page=1&pageSize=5", "t1", "")
			},
			assert: func(t *testing.T, body []byte) {
				contractAssertEnvelope(t, "tasks", body)
			},
		},
		{
			name: "devices_page_envelope",
			call: func(t *testing.T) *httptest.ResponseRecorder {
				s := newTestServer()
				seedContractDevices(s)
				return contractServe(s.handleDevices, "/api/v1/devices?page=1&pageSize=5", "t1", "")
			},
			assert: func(t *testing.T, body []byte) {
				contractAssertEnvelope(t, "devices", body)
			},
		},
		{
			name: "audits_page_envelope",
			call: func(t *testing.T) *httptest.ResponseRecorder {
				s := newTestServer()
				seedContractAudits(s)
				return contractServe(s.handleAudits, "/api/v1/audits?page=1&pageSize=5", "t1", "")
			},
			assert: func(t *testing.T, body []byte) {
				contractAssertEnvelope(t, "audits", body)
			},
		},
		{
			// 历史例外：users 分页仍是 {users,...}，没有 data。
			name: "users_page_container_and_metadata",
			call: func(t *testing.T) *httptest.ResponseRecorder {
				s := newAuthTestServer(t)
				auth := loginAsAdmin(t, s)
				return contractServe(s.handleUsers, "/api/v1/users?page=1&pageSize=2", "", auth)
			},
			assert: func(t *testing.T, body []byte) {
				contractAssertPaginatedContainer(t, "users", body, "users")
			},
		},
		{
			// 历史例外：tenants 分页仍是 {tenants,...}，没有 data。
			name: "tenants_page_container_and_metadata",
			call: func(t *testing.T) *httptest.ResponseRecorder {
				s := newAuthTestServer(t)
				auth := loginAsAdmin(t, s)
				seedContractTenants(s)
				return contractServe(s.handleTenants, "/api/v1/tenants?page=1&pageSize=2", "", auth)
			},
			assert: func(t *testing.T, body []byte) {
				contractAssertPaginatedContainer(t, "tenants", body, "tenants")
			},
		},
		{
			// 触发条件冻结（§2.3）：tasks 只认 page，pageSize 单独存在保持裸数组。
			name: "tasks_page_size_only_stays_array",
			call: func(t *testing.T) *httptest.ResponseRecorder {
				s := newTestServer()
				seedContractTasks(s)
				return contractServe(s.handleListTasks, "/api/v1/tasks?pageSize=5", "t1", "")
			},
			assert: func(t *testing.T, body []byte) {
				if arr := contractArray(t, body); len(arr) == 0 {
					t.Fatalf("tasks pageSize-only array is empty; fixture missing: %s", body)
				}
			},
		},
		{
			name: "devices_page_size_only_envelope",
			call: func(t *testing.T) *httptest.ResponseRecorder {
				s := newTestServer()
				seedContractDevices(s)
				return contractServe(s.handleDevices, "/api/v1/devices?pageSize=5", "t1", "")
			},
			assert: func(t *testing.T, body []byte) {
				contractAssertEnvelope(t, "devices", body)
			},
		},
		{
			name: "audits_page_size_only_envelope",
			call: func(t *testing.T) *httptest.ResponseRecorder {
				s := newTestServer()
				seedContractAudits(s)
				return contractServe(s.handleAudits, "/api/v1/audits?pageSize=5", "t1", "")
			},
			assert: func(t *testing.T, body []byte) {
				contractAssertEnvelope(t, "audits", body)
			},
		},
		{
			name: "users_page_size_only_stays_container",
			call: func(t *testing.T) *httptest.ResponseRecorder {
				s := newAuthTestServer(t)
				auth := loginAsAdmin(t, s)
				return contractServe(s.handleUsers, "/api/v1/users?pageSize=2", "", auth)
			},
			assert: func(t *testing.T, body []byte) {
				contractAssertLegacyContainer(t, "users", body, "users")
			},
		},
		{
			name: "tenants_page_size_only_stays_container",
			call: func(t *testing.T) *httptest.ResponseRecorder {
				s := newAuthTestServer(t)
				auth := loginAsAdmin(t, s)
				seedContractTenants(s)
				return contractServe(s.handleTenants, "/api/v1/tenants?pageSize=2", "", auth)
			},
			assert: func(t *testing.T, body []byte) {
				contractAssertLegacyContainer(t, "tenants", body, "tenants")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.assert(t, contractMustOK(t, tc.call(t)))
		})
	}
}

// ---------------------------------------------------------------------------
// 非法分页参数（非数字 page/pageSize → 400）
// ---------------------------------------------------------------------------

// TestContractV1TasksInvalidPagination 钉住 tasks 列表端点对非法分页参数的契约：
// page/pageSize 存在但不是合法整数时必须回 400 {"error":"invalid page/pageSize"}，
// 而不是静默回退默认值或返回 200 信封（B1）。
func TestContractV1TasksInvalidPagination(t *testing.T) {
	cases := []struct {
		name   string
		target string
	}{
		{name: "non_numeric_page", target: "/api/v1/tasks?page=abc"},
		{name: "non_numeric_page_size", target: "/api/v1/tasks?pageSize=xyz"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer()
			seedContractTasks(s)
			rec := contractServe(s.handleListTasks, tc.target, "t1", "")
			assertInvalidPagination(t, rec)
		})
	}
}
