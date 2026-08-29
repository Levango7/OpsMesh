// platform_config_simulated_test.go 测试 platform_config.go 真实持久化（G2 修复）。
//
// 覆盖：
//   - handleUpdatePlatformConfig 真实落库：store.GetConfig 可读回、审计 Action 为 platform_config_update；
//   - handleGetPlatformConfig 优先读 store，未设置时回退出厂默认。
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

// newPlatformConfigTestServer 构造平台配置 API 测试用 Server。
func newPlatformConfigTestServer() *Server {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3},
		jwtSecret:    []byte("test-jwt-secret-for-platform-cfg-32!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
}

// TestHandleUpdatePlatformConfig_Persisted 验证 PUT 真实落库：
// 响应为实际配置（无 simulated 标记）、store.GetConfig 可读回、审计 Action 为 platform_config_update。
func TestHandleUpdatePlatformConfig_Persisted(t *testing.T) {
	s := newPlatformConfigTestServer()
	auth := loginAsAdmin(t, s)
	body := `{"version":"0.6.0","defaultTenant":"default","maxTenants":200,"enableMarketplace":false,"enableBilling":true}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/platform/config", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handlePlatformConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	// 响应为实际配置值（无 simulated 字段）。
	var resp struct {
		MaxTenants int  `json:"maxTenants"`
		Simulated  bool `json:"simulated"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Simulated {
		t.Fatal("simulated=true, want false (platform_config 真实落库)")
	}
	if resp.MaxTenants != 200 {
		t.Fatalf("maxTenants=%d, want 200", resp.MaxTenants)
	}
	// store 已落库（tenant=default, key=platform/config）。
	item, ok := s.store.GetConfig("default", "platform/config")
	if !ok || item == nil {
		t.Fatal("store 中应存在 platform/config 配置")
	}
	var stored PlatformConfig
	if err := json.Unmarshal([]byte(item.Value), &stored); err != nil {
		t.Fatalf("store 配置解析失败: %v", err)
	}
	if stored.MaxTenants != 200 {
		t.Fatalf("stored.maxTenants=%d, want 200", stored.MaxTenants)
	}
	// 审计 Action 为 platform_config_update（不再带 _simulated 后缀）。
	audits := s.store.Audits()
	var hit bool
	for _, ev := range audits {
		if ev.Action == "platform_config_update" {
			hit = true
			break
		}
	}
	if !hit {
		t.Fatalf("audit log missing platform_config_update; audits=%+v", audits)
	}
}

// TestHandleGetPlatformConfig_ReadsStore 验证 GET 优先读 store（PUT 后返回已持久化值）。
func TestHandleGetPlatformConfig_ReadsStore(t *testing.T) {
	s := newPlatformConfigTestServer()
	auth := loginAsAdmin(t, s)

	// 先 PUT 写入。
	body := `{"version":"0.6.0","maxTenants":300}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/platform/config", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handlePlatformConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status=%d", w.Code)
	}
	// 再 GET，应返回 store 中值而非出厂默认。
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/platform/config", nil)
	req2.Header.Set("Authorization", auth)
	w2 := httptest.NewRecorder()
	s.handlePlatformConfig(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("GET status=%d", w2.Code)
	}
	var got PlatformConfig
	if err := json.Unmarshal(w2.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.MaxTenants != 300 {
		t.Fatalf("GET maxTenants=%d, want 300（读 store 而非出厂默认 100）", got.MaxTenants)
	}
}

// TestHandleGetPlatformConfig_DefaultFallback 验证 store 未设置时回退出厂默认。
func TestHandleGetPlatformConfig_DefaultFallback(t *testing.T) {
	s := newPlatformConfigTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/platform/config", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handlePlatformConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	var got PlatformConfig
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.MaxTenants != 100 {
		t.Fatalf("GET maxTenants=%d, want 100（出厂默认）", got.MaxTenants)
	}
}
