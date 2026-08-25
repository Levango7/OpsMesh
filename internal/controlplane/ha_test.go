// ha_test.go 测试 Phase 3 控制面 HA 管理 HTTP handler（ha.go）。
//
// 覆盖范围：
//   - handleHAStatus：获取 HA 状态
//   - handleHAInstances：列出实例
//   - handleHAFailover：手动切换 leader
//   - handleHAHealth：健康检查
//   - 鉴权：无 token 返回 401
//
// 测试策略（与 ticket_test.go 风格一致）。
package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"opsmesh/internal/config"
	"opsmesh/internal/store"
)

// newHATestServer 构造 HA API 测试用 Server。
func newHATestServer() *Server {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3, Replicas: 1},
		jwtSecret:    []byte("test-jwt-secret-for-ha-test-32bytes-pad!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
}

// TestHandleHAStatus 验证获取 HA 状态返回 200 + leader 信息。
func TestHandleHAStatus(t *testing.T) {
	s := newHATestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ha/status", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleHAStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp haStatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// MemoryStore 恒为 leader（单实例）。
	if !resp.Current.IsLeader {
		t.Fatal("current instance should be leader for MemoryStore")
	}
	if resp.Leader.Role != "leader" {
		t.Fatalf("leader role=%s, want leader", resp.Leader.Role)
	}
}

// TestHandleHAInstances 验证列出实例返回 200 + 当前实例。
func TestHandleHAInstances(t *testing.T) {
	s := newHATestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ha/instances", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleHAInstances(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Instances []haInstanceInfo `json:"instances"`
		Count     int              `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Count != 1 {
		t.Fatalf("count=%d, want 1 (MVP: self only)", resp.Count)
	}
}

// TestHandleHAFailover 验证手动切换 leader 返回 200 + accepted。
func TestHandleHAFailover(t *testing.T) {
	s := newHATestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ha/failover", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleHAFailover(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "accepted" {
		t.Fatalf("status=%s, want accepted", resp.Status)
	}
}

// TestHandleHAHealth 验证健康检查返回 200 + healthy。
func TestHandleHAHealth(t *testing.T) {
	s := newHATestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ha/health", nil)
	w := httptest.NewRecorder()
	s.handleHAHealth(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "healthy" {
		t.Fatalf("status=%s, want healthy", resp.Status)
	}
}

// TestHandleHAStatus_NoToken 验证无 token 返回 401。
func TestHandleHAStatus_NoToken(t *testing.T) {
	s := newHATestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ha/status", nil)
	w := httptest.NewRecorder()
	s.handleHAStatus(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", w.Code)
	}
}