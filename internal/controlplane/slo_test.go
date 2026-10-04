// slo_test.go 测试 Phase 1 SLO 管理 HTTP handler（slo.go）。
//
// 覆盖范围：
//   - handleListSLOs：空列表、创建后列表
//   - handleCreateSLO：正常创建、缺必填字段、无效 JSON
//   - handleGetSLO：正常获取、不存在
//   - handleUpdateSLO：正常更新、不存在
//   - handleDeleteSLO：正常删除、不存在
//   - handleSLOStatus：正常获取 SLI 状态、不存在
//   - handleSLOs：method not allowed 分派
//   - handleSLORouting：{id} 路由分派、空 id、status 子路径
//   - 鉴权：无 token 返回 401
//
// 测试策略（与 ticket_test.go 风格一致）：
//   - 白盒（package controlplane），直接装配 Server{store: MemoryStore, jwtSecret: 固定}；
//   - 鉴权用例通过 admin 登录获取 token（requirePermission 校验 slo:read/write/delete）；
//   - 用 httptest.NewRequest + httptest.NewRecorder 直接调用 handler，断言 status code 与响应体。
package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/store"
)

// newSLOTestServer 构造 SLO API 测试用 Server：
//   - memory store（NewMemoryStore 已 seedRBAC，预置 admin/admin123）；
//   - 固定 jwtSecret（避免随机性）。
func newSLOTestServer() *Server {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3},
		jwtSecret:    []byte("test-jwt-secret-for-slo-test-32bytes!!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
}

// =============================================================================
// handleListSLOs（GET /api/v1/slos）
// ============================================================================

// TestHandleListSLOs_Empty 验证空列表返回 200 + slos:[]。
func TestHandleListSLOs_Empty(t *testing.T) {
	s := newSLOTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/slos", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleSLOs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		SLOs []*store.SLO `json:"slos"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.SLOs) != 0 {
		t.Fatalf("slos=%d, want 0", len(resp.SLOs))
	}
}

// TestHandleListSLOs_AfterCreate 验证创建后列表含 1 个 SLO。
func TestHandleListSLOs_AfterCreate(t *testing.T) {
	s := newSLOTestServer()
	auth := loginAsAdmin(t, s)

	s.store.CreateSLO("default", &store.SLO{
		Name:        "list-test",
		ServiceName: "api-server",
		Target:      99.9,
		Window:      "30d",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/slos", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleSLOs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		SLOs []*store.SLO `json:"slos"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.SLOs) != 1 {
		t.Fatalf("slos=%d, want 1", len(resp.SLOs))
	}
	if resp.SLOs[0].Name != "list-test" {
		t.Fatalf("Name=%q, want list-test", resp.SLOs[0].Name)
	}
}

// TestHandleListSLOs_NoAuth 验证无 Authorization 头返回 401。
func TestHandleListSLOs_NoAuth(t *testing.T) {
	s := newSLOTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/slos", nil)
	w := httptest.NewRecorder()
	s.handleSLOs(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", w.Code)
	}
}

// =============================================================================
// handleCreateSLO（POST /api/v1/slos）
// ============================================================================

// TestHandleCreateSLO 验证正常创建返回 201 + SLO（含 ID）。
func TestHandleCreateSLO(t *testing.T) {
	s := newSLOTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":"test-slo","description":"test desc","serviceName":"api","target":99.9,"window":"30d","slis":[{"name":"cpu-low","metric":"cpu_usage","target":0.8,"operator":"<"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/slos", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleSLOs(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", w.Code, w.Body.String())
	}
	var slo store.SLO
	if err := json.Unmarshal(w.Body.Bytes(), &slo); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if slo.ID == "" {
		t.Fatal("ID is empty, want server-assigned")
	}
	if slo.Name != "test-slo" {
		t.Fatalf("Name=%q, want test-slo", slo.Name)
	}
	if slo.Target != 99.9 {
		t.Fatalf("Target=%v, want 99.9", slo.Target)
	}
	if len(slo.SLIs) != 1 {
		t.Fatalf("SLIs=%d, want 1", len(slo.SLIs))
	}
	if slo.SLIs[0].Name != "cpu-low" {
		t.Fatalf("SLIs[0].Name=%q, want cpu-low", slo.SLIs[0].Name)
	}
	// 确认 SLO 已持久化到 store
	got, ok := s.store.GetSLO("default", slo.ID)
	if !ok || got == nil {
		t.Fatal("GetSLO returned nil after create")
	}
}

// TestHandleCreateSLO_UnsupportedSLIMetric 验证引用无真实数据来源的指标返回 400 而不是静默接受。
//
// 这条断言存在的原因：SLI 的 metric 只能是 store 有取值路径的那几个（见 store/slo_eval.go）。
// 修前 `metric:"up"` 会被 201 接受，然后状态永远 nodata（SQL 后端）或恒 "met"/99.5
// （修前的内存后端）——即"配置成功、结果造假"。写入时就拒，比交付一份看不出破绽的
// 空报告诚实（2026-10-04）。
func TestHandleCreateSLO_UnsupportedSLIMetric(t *testing.T) {
	s := newSLOTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":"bad-slo","target":99.9,"window":"30d","slis":[{"name":"availability","metric":"up","target":99.9,"operator":">="}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/slos", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleSLOs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "unsupported SLI metric") {
		t.Fatalf("body=%s, want 包含 unsupported SLI metric 文案", w.Body.String())
	}
	// 必须"拒了就什么都没写"：不能出现 400 与半条持久化并存。
	for _, slo := range s.store.ListSLOs("default") {
		if slo.Name == "bad-slo" {
			t.Fatal("400 之后 bad-slo 仍被写入 store")
		}
	}
}

// TestHandleCreateSLO_MissingName 验证缺 name 返回 400。
func TestHandleCreateSLO_MissingName(t *testing.T) {
	s := newSLOTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"description":"no name"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/slos", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleSLOs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// =============================================================================
// handleGetSLO（GET /api/v1/slos/{id}）
// ============================================================================

// TestHandleGetSLO 验证正常获取 SLO 详情。
func TestHandleGetSLO(t *testing.T) {
	s := newSLOTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateSLO("default", &store.SLO{Name: "get-test", Target: 99.5})
	if created == nil {
		t.Fatal("CreateSLO returned nil")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/slos/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleSLORouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var slo store.SLO
	if err := json.Unmarshal(w.Body.Bytes(), &slo); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if slo.ID != created.ID {
		t.Fatalf("ID=%q, want %q", slo.ID, created.ID)
	}
	if slo.Name != "get-test" {
		t.Fatalf("Name=%q, want get-test", slo.Name)
	}
}

// TestHandleGetSLO_NotFound 验证获取不存在的 SLO 返回 404。
func TestHandleGetSLO_NotFound(t *testing.T) {
	s := newSLOTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/slos/nonexistent", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleSLORouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleUpdateSLO（PUT /api/v1/slos/{id}）
// ============================================================================

// TestHandleUpdateSLO 验证正常更新 SLO。
func TestHandleUpdateSLO(t *testing.T) {
	s := newSLOTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateSLO("default", &store.SLO{Name: "update-test", Target: 99.0})
	if created == nil {
		t.Fatal("CreateSLO returned nil")
	}

	body := `{"name":"updated-name","target":99.99,"window":"7d"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/slos/"+created.ID, strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleSLORouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var slo store.SLO
	if err := json.Unmarshal(w.Body.Bytes(), &slo); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if slo.Name != "updated-name" {
		t.Fatalf("Name=%q, want updated-name", slo.Name)
	}
	if slo.Target != 99.99 {
		t.Fatalf("Target=%v, want 99.99", slo.Target)
	}
	if slo.Window != "7d" {
		t.Fatalf("Window=%q, want 7d", slo.Window)
	}
}

// =============================================================================
// handleDeleteSLO（DELETE /api/v1/slos/{id}）
// ============================================================================

// TestHandleDeleteSLO 验证正常删除 SLO 返回 204。
func TestHandleDeleteSLO(t *testing.T) {
	s := newSLOTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateSLO("default", &store.SLO{Name: "delete-test"})
	if created == nil {
		t.Fatal("CreateSLO returned nil")
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/slos/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleSLORouting(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want 204; body=%s", w.Code, w.Body.String())
	}
	// 确认已删除
	if _, ok := s.store.GetSLO("default", created.ID); ok {
		t.Fatal("SLO still exists after delete")
	}
}

// TestHandleDeleteSLO_NotFound 验证删除不存在的 SLO 返回 404。
func TestHandleDeleteSLO_NotFound(t *testing.T) {
	s := newSLOTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/slos/nonexistent", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleSLORouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleSLOStatus（GET /api/v1/slos/{id}/status）
// ============================================================================

// TestHandleSLOStatus 验证 SLI 状态来自真实样本聚合，且无样本时诚实报 nodata。
//
// 修前这条测试断言的是 CurrentValue==99.5 / Status=="met"（注释写着"MVP 模拟值"），
// 等于把编造的达标报告钉成了期望值——/slos/{id}/status 是对外 SLA 口径，恒 met 会让
// 客户据此做复盘（2026-10-04 反转该断言）。
func TestHandleSLOStatus(t *testing.T) {
	s := newSLOTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateSLO("default", &store.SLO{
		Name:   "status-test",
		Target: 99.9,
		SLIs: []store.SLI{
			{Name: "cpu-low", Metric: "cpu_usage", Target: 0.5, Operator: "<"},
			{Name: "availability", Metric: "up", Target: 99.9, Operator: ">="},
		},
	})
	if created == nil {
		t.Fatal("CreateSLO returned nil")
	}

	call := func() []*store.SLIStatus {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/slos/"+created.ID+"/status", nil)
		req.Header.Set("Authorization", auth)
		w := httptest.NewRecorder()
		s.handleSLORouting(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
		}
		var resp struct {
			Statuses []*store.SLIStatus `json:"statuses"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(resp.Statuses) != 2 {
			t.Fatalf("statuses=%d, want 2", len(resp.Statuses))
		}
		return resp.Statuses
	}

	// 无样本：两条都必须是 nodata，CurrentValue=-1（-1 是"没有观测"的哨兵，不是测到的值）。
	st := call()
	for _, x := range st {
		if x.Status != "nodata" || x.CurrentValue != -1 {
			t.Fatalf("无样本时状态 = %+v，期望 nodata/-1", x)
		}
	}

	// 放入真实样本：cpu_usage 均值 0.9，目标 "< 0.5" ⇒ breached（证明判定跟着观测值走）。
	s.store.StoreNetworkMetrics("dev-slo", &store.NetworkMetrics{
		TenantID: "default", CPUUsage: 0.9, Timestamp: time.Now(),
	})
	st = call()
	if st[0].Status != "breached" || st[0].CurrentValue < 0.89 || st[0].CurrentValue > 0.91 {
		t.Fatalf("有样本时 cpu-low = %+v，期望 breached 且 CurrentValue≈0.9", st[0])
	}
	// 第二条没有取值路径（up），放了样本也必须仍是 nodata——不能被别的指标"顺带"算出来。
	if st[1].Status != "nodata" {
		t.Fatalf("无取值路径的指标 = %+v，期望仍是 nodata", st[1])
	}
	if st[0].LastEvaluated.IsZero() {
		t.Fatal("LastEvaluated 为空")
	}
}

// TestHandleSLOStatus_NotFound 验证获取不存在 SLO 的状态返回 404。
func TestHandleSLOStatus_NotFound(t *testing.T) {
	s := newSLOTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/slos/nonexistent/status", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleSLORouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleSLORouting 路由分派
// ============================================================================

// TestHandleSLORouting_EmptyID 验证空 id 返回 400。
func TestHandleSLORouting_EmptyID(t *testing.T) {
	s := newSLOTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/slos/", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleSLORouting(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

// TestHandleSLORouting_UnknownSubPath 验证未知子路径返回 404。
func TestHandleSLORouting_UnknownSubPath(t *testing.T) {
	s := newSLOTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/slos/some-id/unknown", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleSLORouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// TestHandleSLOs_MethodNotAllowed 验证不支持的方法返回 405。
func TestHandleSLOs_MethodNotAllowed(t *testing.T) {
	s := newSLOTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/slos", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleSLOs(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", w.Code)
	}
}
