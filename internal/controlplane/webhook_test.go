// webhook_test.go 测试 Phase 5 Webhook 管理 HTTP handler（webhook.go）。
//
// 覆盖范围：
//   - handleListWebhooks：空列表、创建后列表
//   - handleCreateWebhook：正常创建、缺必填字段、无效 JSON
//   - handleCreateWebhook SSRF 校验（M1）：file://、loopback、云元数据 URL 拒绝（400 不入库）、
//     公网 IP 正例创建成功、WebhookAllowPrivate 开关与 notify-channels 同源联动
//   - handleGetWebhook：正常获取、不存在
//   - handleUpdateWebhook：正常更新、不存在、SSRF 恶意 URL 拒绝（防 PUT 绕过）
//   - handleDeleteWebhook：正常删除、不存在
//   - handleWebhookTest：正常测试投递、不存在
//   - handleWebhookDeliveries：投递记录列表
//   - handleWebhooks：method not allowed 分派
//   - handleWebhookRouting：{id} 路由分派、空 id、未知子路径
//   - 鉴权：无 token 返回 401
//
// 测试策略（与 ticket_test.go 风格一致）：
//   - 白盒（package controlplane），直接装配 Server{store: MemoryStore, jwtSecret: 固定}；
//   - 鉴权用例通过 admin 登录获取 token（requirePermission 校验 webhook:read/write）；
//   - 用 httptest.NewRequest + httptest.NewRecorder 直接调用 handler，断言 status code 与响应体；
//   - 走 create/update handler 的 URL 一律用 RFC5737 公网 IP 字面量（203.0.113.1），
//     避免 SSRF 校验触发 DNS 解析导致离线环境不确定（同 server_netsec_test.go 实践）。
package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opsmesh/internal/config"
	"opsmesh/internal/store"
)

// newWebhookTestServer 构造 Webhook API 测试用 Server。
func newWebhookTestServer() *Server {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3},
		jwtSecret:    []byte("test-jwt-secret-for-webhook-test-32bytes!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
}

// =============================================================================
// handleListWebhooks（GET /api/v1/webhooks）
// ============================================================================

// TestHandleListWebhooks_Empty 验证空列表返回 200 + webhooks:[]。
func TestHandleListWebhooks_Empty(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleWebhooks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Webhooks []*store.Webhook `json:"webhooks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Webhooks) != 0 {
		t.Fatalf("webhooks=%d, want 0", len(resp.Webhooks))
	}
}

// TestHandleListWebhooks_AfterCreate 验证创建后列表含 1 个 Webhook。
func TestHandleListWebhooks_AfterCreate(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	s.store.CreateWebhook("default", &store.Webhook{
		Name: "list-test",
		URL:  "http://example.com/hook",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleWebhooks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Webhooks []*store.Webhook `json:"webhooks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Webhooks) != 1 {
		t.Fatalf("webhooks=%d, want 1", len(resp.Webhooks))
	}
	if resp.Webhooks[0].Name != "list-test" {
		t.Fatalf("Name=%q, want list-test", resp.Webhooks[0].Name)
	}
}

// TestHandleListWebhooks_NoAuth 验证无 Authorization 头返回 401。
func TestHandleListWebhooks_NoAuth(t *testing.T) {
	s := newWebhookTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks", nil)
	w := httptest.NewRecorder()
	s.handleWebhooks(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", w.Code)
	}
}

// =============================================================================
// handleCreateWebhook（POST /api/v1/webhooks）
// ============================================================================

// TestHandleCreateWebhook 验证正常创建返回 201 + Webhook（含 ID）。
func TestHandleCreateWebhook(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	// URL 用 RFC5737 文档段公网 IP 字面量（203.0.113.1）：
	// 创建路径已启用 ValidateWebhookURL SSRF 校验，IP 字面量不触发 DNS 解析，
	// 测试在离线环境保持确定性（与 server_netsec_test.go 同一实践）。
	body := `{"name":"test-webhook","url":"http://203.0.113.1/hook","events":["alert.created"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleWebhooks(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", w.Code, w.Body.String())
	}
	var wh store.Webhook
	if err := json.Unmarshal(w.Body.Bytes(), &wh); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if wh.ID == "" {
		t.Fatal("ID is empty, want server-assigned")
	}
	if wh.Name != "test-webhook" {
		t.Fatalf("Name=%q, want test-webhook", wh.Name)
	}
	// 确认已持久化到 store
	got, ok := s.store.GetWebhook("default", wh.ID)
	if !ok || got == nil {
		t.Fatal("GetWebhook returned nil after create")
	}
}

// TestHandleCreateWebhook_MissingName 验证缺 name 返回 400。
func TestHandleCreateWebhook_MissingName(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"url":"http://203.0.113.1/hook"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleWebhooks(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleCreateWebhook_MissingURL 验证缺 url 返回 400。
func TestHandleCreateWebhook_MissingURL(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":"test-webhook"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleWebhooks(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleCreateWebhook_InvalidJSON 验证无效 JSON 返回 400。
func TestHandleCreateWebhook_InvalidJSON(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":invalid`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleWebhooks(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// =============================================================================
// handleCreateWebhook SSRF 校验（FIXPLAN-phase1-6.md M1 修复防回归）
// ============================================================================

// TestHandleCreateWebhook_SSRFRejected 表驱动验证恶意 URL 创建被拒（返回 400 且不入库）：
//   - file:///etc/passwd：非 http(s) 协议被拒（协议白名单）；
//   - http://127.0.0.1:8080：loopback 地址被拒；
//   - http://169.254.169.254/latest/meta-data：云元数据（链路本地）地址被拒。
//
// 与 notify-channels 共用 ValidateWebhookURL + WebhookAllowPrivate=false 默认语义。
func TestHandleCreateWebhook_SSRFRejected(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"file 协议被拒", "file:///etc/passwd"},
		{"loopback 被拒", "http://127.0.0.1:8080/hook"},
		{"云元数据被拒", "http://169.254.169.254/latest/meta-data"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newWebhookTestServer()
			auth := loginAsAdmin(t, s)

			body := `{"name":"evil-webhook","url":"` + c.url + `"}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", strings.NewReader(body))
			req.Header.Set("Authorization", auth)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			s.handleWebhooks(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("url=%q: status=%d, want 400; body=%s", c.url, w.Code, w.Body.String())
			}
			var resp struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode error body: %v", err)
			}
			if !strings.Contains(resp.Error, "invalid webhook url") {
				t.Fatalf("error=%q, want prefix \"invalid webhook url\"", resp.Error)
			}
			// 确认恶意 URL 未入库。
			if hooks := s.store.ListWebhooks("default"); len(hooks) != 0 {
				t.Fatalf("webhooks persisted=%d, want 0 (rejected URL must not be stored)", len(hooks))
			}
		})
	}
}

// TestHandleCreateWebhook_ValidPublicIP 正例：合法公网 URL 创建成功返回 201 并入库。
// 用 RFC5737 文档段公网 IP 字面量（203.0.113.1），不触发 DNS 解析，离线环境确定性通过
// （与 server_netsec_test.go TestValidateWebhookURL_Happy 同一实践）。
func TestHandleCreateWebhook_ValidPublicIP(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":"public-hook","url":"http://203.0.113.1/hook"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleWebhooks(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", w.Code, w.Body.String())
	}
	var wh store.Webhook
	if err := json.Unmarshal(w.Body.Bytes(), &wh); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if wh.ID == "" {
		t.Fatal("ID is empty, want server-assigned")
	}
	got, ok := s.store.GetWebhook("default", wh.ID)
	if !ok || got == nil || got.URL != "http://203.0.113.1/hook" {
		t.Fatal("valid public webhook not persisted correctly after create")
	}
}

// TestHandleCreateWebhook_AllowPrivateToggle 验证 allowPrivate 开关与 notify-channels 同源联动：
//   - cfg.WebhookAllowPrivate=true 时私网 URL 放行（内网部署场景，同 createNotifyChannel）；
//   - cfg.WebhookAllowPrivate=false 时同一 URL 被拒——证明两端由同一配置字段控制，
//     不存在双标。
func TestHandleCreateWebhook_AllowPrivateToggle(t *testing.T) {
	privateURL := `{"name":"intranet-hook","url":"http://192.168.1.10/hook"}`

	// allowPrivate=true：放行。
	sOn := newWebhookTestServer()
	sOn.cfg.WebhookAllowPrivate = true
	authOn := loginAsAdmin(t, sOn)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", strings.NewReader(privateURL))
	req.Header.Set("Authorization", authOn)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	sOn.handleWebhooks(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("allowPrivate=true: status=%d, want 201; body=%s", w.Code, w.Body.String())
	}

	// allowPrivate=false：拒绝。
	sOff := newWebhookTestServer()
	authOff := loginAsAdmin(t, sOff)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", strings.NewReader(privateURL))
	req.Header.Set("Authorization", authOff)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	sOff.handleWebhooks(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("allowPrivate=false: status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// =============================================================================
// handleGetWebhook（GET /api/v1/webhooks/{id}）
// ============================================================================

// TestHandleGetWebhook 验证正常获取 Webhook 详情。
func TestHandleGetWebhook(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateWebhook("default", &store.Webhook{Name: "get-test", URL: "http://example.com/hook"})
	if created == nil {
		t.Fatal("CreateWebhook returned nil")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleWebhookRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var wh store.Webhook
	if err := json.Unmarshal(w.Body.Bytes(), &wh); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if wh.ID != created.ID {
		t.Fatalf("ID=%q, want %q", wh.ID, created.ID)
	}
}

// TestHandleGetWebhook_NotFound 验证获取不存在的 Webhook 返回 404。
func TestHandleGetWebhook_NotFound(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/nonexistent", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleWebhookRouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleUpdateWebhook（PUT /api/v1/webhooks/{id}）
// ============================================================================

// TestHandleUpdateWebhook 验证正常更新 Webhook。
func TestHandleUpdateWebhook(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateWebhook("default", &store.Webhook{Name: "update-test", URL: "http://example.com/hook"})
	if created == nil {
		t.Fatal("CreateWebhook returned nil")
	}

	body := `{"name":"updated-name","url":"http://203.0.113.1/hook2"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/webhooks/"+created.ID, strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleWebhookRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var wh store.Webhook
	if err := json.Unmarshal(w.Body.Bytes(), &wh); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if wh.Name != "updated-name" {
		t.Fatalf("Name=%q, want updated-name", wh.Name)
	}
	if wh.URL != "http://203.0.113.1/hook2" {
		t.Fatalf("URL=%q, want http://203.0.113.1/hook2", wh.URL)
	}
}

// TestHandleUpdateWebhook_NotFound 验证更新不存在的 Webhook 返回 404。
func TestHandleUpdateWebhook_NotFound(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":"updated-name","url":"http://203.0.113.1/hook"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/webhooks/nonexistent", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleWebhookRouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// TestHandleUpdateWebhook_SSRFRejected 验证更新路径同样执行 SSRF 校验（防 PUT 绕过）：
// 先以合法公网 URL 创建，再 PUT file:/// 恶意 URL → 400，
// 且原 Webhook 记录保持不变（恶意 URL 未落库）。
func TestHandleUpdateWebhook_SSRFRejected(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateWebhook("default", &store.Webhook{Name: "update-ssrf", URL: "http://203.0.113.1/hook"})
	if created == nil {
		t.Fatal("CreateWebhook returned nil")
	}

	for _, evil := range []string{"file:///etc/passwd", "http://127.0.0.1:8080/hook", "http://169.254.169.254/latest/meta-data"} {
		body := `{"name":"update-ssrf","url":"` + evil + `"}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/webhooks/"+created.ID, strings.NewReader(body))
		req.Header.Set("Authorization", auth)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.handleWebhookRouting(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("evil=%q: status=%d, want 400; body=%s", evil, w.Code, w.Body.String())
		}
	}
	// 原记录未被篡改：URL 保持创建时的合法值。
	got, ok := s.store.GetWebhook("default", created.ID)
	if !ok || got == nil {
		t.Fatal("GetWebhook returned nil after rejected update")
	}
	if got.URL != "http://203.0.113.1/hook" {
		t.Fatalf("URL=%q, want original http://203.0.113.1/hook (rejected update must not persist)", got.URL)
	}
}

// =============================================================================
// handleDeleteWebhook（DELETE /api/v1/webhooks/{id}）
// ============================================================================

// TestHandleDeleteWebhook 验证正常删除 Webhook。
func TestHandleDeleteWebhook(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateWebhook("default", &store.Webhook{Name: "delete-test", URL: "http://example.com/hook"})
	if created == nil {
		t.Fatal("CreateWebhook returned nil")
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/webhooks/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleWebhookRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	// 确认已删除
	if _, ok := s.store.GetWebhook("default", created.ID); ok {
		t.Fatal("GetWebhook returned ok after delete")
	}
}

// TestHandleDeleteWebhook_NotFound 验证删除不存在的 Webhook 返回 404。
func TestHandleDeleteWebhook_NotFound(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/webhooks/nonexistent", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleWebhookRouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleWebhookTest（POST /api/v1/webhooks/{id}/test）
// ============================================================================

// TestHandleWebhookTest 验证 webhook 测试投递（SSRF 校验：私网地址返回 400）。
func TestHandleWebhookTest(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	// 使用私网地址（SSRF 校验应拒绝）。
	created := s.store.CreateWebhook("default", &store.Webhook{Name: "test-hook", URL: "http://127.0.0.1:9999/hook"})
	if created == nil {
		t.Fatal("CreateWebhook returned nil")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/"+created.ID+"/test", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleWebhookRouting(w, req)

	// SSRF 校验应拒绝私网地址，返回 400。
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400 (SSRF blocked); body=%s", w.Code, w.Body.String())
	}
}

// TestHandleWebhookTest_NotFound 验证测试不存在的 Webhook 返回 404。
func TestHandleWebhookTest_NotFound(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/nonexistent/test", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleWebhookRouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleWebhookDeliveries（GET /api/v1/webhooks/{id}/deliveries）
// ============================================================================

// TestHandleWebhookDeliveries 验证投递记录列表。
func TestHandleWebhookDeliveries(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateWebhook("default", &store.Webhook{Name: "del-test", URL: "http://example.com/hook"})
	if created == nil {
		t.Fatal("CreateWebhook returned nil")
	}
	// 先记录一条投递
	// M3：直接经 Store 接口调用，消除对 *MemoryStore 的类型断言。
	s.store.RecordWebhookDelivery("default", created.ID, "test.event", "{}", 200, "ok", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/"+created.ID+"/deliveries", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleWebhookRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Deliveries []*store.WebhookDelivery `json:"deliveries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Deliveries) != 1 {
		t.Fatalf("deliveries=%d, want 1", len(resp.Deliveries))
	}
}

// TestHandleWebhookDeliveries_Empty 验证空投递记录返回 200 + deliveries:[]。
func TestHandleWebhookDeliveries_Empty(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateWebhook("default", &store.Webhook{Name: "del-empty", URL: "http://example.com/hook"})
	if created == nil {
		t.Fatal("CreateWebhook returned nil")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/"+created.ID+"/deliveries", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleWebhookRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Deliveries []*store.WebhookDelivery `json:"deliveries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Deliveries) != 0 {
		t.Fatalf("deliveries=%d, want 0", len(resp.Deliveries))
	}
}

// =============================================================================
// handleWebhookRouting 路由分派
// ============================================================================

// TestHandleWebhookRouting_EmptyID 验证空 id 返回 400。
func TestHandleWebhookRouting_EmptyID(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleWebhookRouting(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

// TestHandleWebhookRouting_UnknownSubPath 验证未知子路径返回 404。
func TestHandleWebhookRouting_UnknownSubPath(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/some-id/unknown", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleWebhookRouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// TestHandleWebhooks_MethodNotAllowed 验证不支持的方法返回 405。
func TestHandleWebhooks_MethodNotAllowed(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/webhooks", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleWebhooks(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", w.Code)
	}
}
