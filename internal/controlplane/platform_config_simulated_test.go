// platform_config_simulated_test.go 测试 platform_config.go 假审计修正。
//
// 覆盖：
//   - handleUpdatePlatformConfig 审计 Action 为 platform_config_update_simulated；
//   - 响应体含 simulated:true。
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

// TestHandleUpdatePlatformConfig_SimulatedActionAndFlag 验证 PUT 审计 Action 为
// platform_config_update_simulated 且响应含 simulated:true（假审计修正）。
func TestHandleUpdatePlatformConfig_SimulatedActionAndFlag(t *testing.T) {
	s := newPlatformConfigTestServer()
	auth := loginAsAdmin(t, s)
	body := `{"version":"0.6.0","defaultTenant":"default","maxTenants":100}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/platform/config", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handlePlatformConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	// 断言响应含 simulated:true。
	var resp struct {
		Simulated bool `json:"simulated"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Simulated {
		t.Fatal("simulated=false, want true (platform_config 假审计标记)")
	}
	// 断言审计 Action 为 platform_config_update_simulated。
	audits := s.store.Audits()
	var hit bool
	for _, ev := range audits {
		if ev.Action == "platform_config_update_simulated" {
			hit = true
			break
		}
	}
	if !hit {
		t.Fatalf("audit log missing platform_config_update_simulated; audits=%+v", audits)
	}
}