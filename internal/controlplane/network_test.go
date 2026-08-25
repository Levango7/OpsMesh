// network_test.go 测试 Phase 4 网络管理 HTTP handler（network.go）。
//
// 覆盖范围：
//   - handleListNetworkDevices：空列表、创建后列表
//   - handleCreateNetworkDevice：正常创建、缺必填字段、无效设备类型
//   - handleGetNetworkDevice：正常获取、不存在
//   - handleDeleteNetworkDevice：正常删除、不存在
//   - handleNetworkDeviceMetrics：正常获取指标
//   - handleNetworkDeviceConfig：正常下发配置、空配置
//   - handleNetworkDiscover：正常发现、无效 CIDR
//   - 鉴权：无 token 返回 401
//
// 测试策略（与 ticket_test.go 风格一致）：
//   - 白盒（package controlplane），直接装配 Server{store: MemoryStore, jwtSecret: 固定}；
//   - 鉴权用例通过 admin 登录获取 token（requirePermission 校验 network:read/write）；
//   - 用 httptest.NewRequest + httptest.NewRecorder 直接调用 handler，断言 status code 与响应体。
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

// newNetworkDeviceTestServer 构造网络管理 API 测试用 Server。
func newNetworkDeviceTestServer() *Server {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3},
		jwtSecret:    []byte("test-jwt-secret-for-network-test-32bytes!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
}

// =============================================================================
// handleListNetworkDevices（GET /api/v1/network/devices）
// =============================================================================

// TestHandleListNetworkDevices_Empty 验证空列表返回 200 + devices:[]。
func TestHandleListNetworkDevices_Empty(t *testing.T) {
	s := newNetworkDeviceTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/devices", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkDevices(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Devices []*store.NetworkDevice `json:"devices"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Devices) != 0 {
		t.Fatalf("devices=%d, want 0", len(resp.Devices))
	}
}

// TestHandleListNetworkDevices_AfterCreate 验证创建后列表含 1 个设备。
func TestHandleListNetworkDevices_AfterCreate(t *testing.T) {
	s := newNetworkDeviceTestServer()
	auth := loginAsAdmin(t, s)

	s.store.CreateNetworkDevice("default", &store.NetworkDevice{
		Name: "core-switch-01",
		Type: "switch",
		IP:   "10.0.0.1",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/devices", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkDevices(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Devices []*store.NetworkDevice `json:"devices"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Devices) != 1 {
		t.Fatalf("devices=%d, want 1", len(resp.Devices))
	}
	if resp.Devices[0].Name != "core-switch-01" {
		t.Fatalf("name=%s, want core-switch-01", resp.Devices[0].Name)
	}
}

// =============================================================================
// handleCreateNetworkDevice（POST /api/v1/network/devices）
// =============================================================================

// TestHandleCreateNetworkDevice_Success 验证正常创建返回 201。
func TestHandleCreateNetworkDevice_Success(t *testing.T) {
	s := newNetworkDeviceTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":"edge-router-01","type":"router","ip":"192.168.1.1","vendor":"cisco"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/devices", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkDevices(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var dev store.NetworkDevice
	if err := json.Unmarshal(w.Body.Bytes(), &dev); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dev.Name != "edge-router-01" || dev.Type != "router" {
		t.Fatalf("name=%s type=%s", dev.Name, dev.Type)
	}
	if dev.ID == "" {
		t.Fatal("ID is empty")
	}
}

// TestHandleCreateNetworkDevice_MissingName 验证缺 name 返回 400。
func TestHandleCreateNetworkDevice_MissingName(t *testing.T) {
	s := newNetworkDeviceTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"type":"router","ip":"192.168.1.1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/devices", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkDevices(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

// TestHandleCreateNetworkDevice_InvalidType 验证无效设备类型返回 400。
func TestHandleCreateNetworkDevice_InvalidType(t *testing.T) {
	s := newNetworkDeviceTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":"bad-dev","type":"unknown_type"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/devices", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkDevices(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

// =============================================================================
// handleGetNetworkDevice（GET /api/v1/network/devices/{id}）
// =============================================================================

// TestHandleGetNetworkDevice_Success 验证正常获取返回 200。
func TestHandleGetNetworkDevice_Success(t *testing.T) {
	s := newNetworkDeviceTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateNetworkDevice("default", &store.NetworkDevice{
		Name: "fw-01",
		Type: "firewall",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/devices/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkDeviceRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var dev store.NetworkDevice
	if err := json.Unmarshal(w.Body.Bytes(), &dev); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dev.Name != "fw-01" {
		t.Fatalf("name=%s, want fw-01", dev.Name)
	}
}

// TestHandleGetNetworkDevice_NotFound 验证不存在返回 404。
func TestHandleGetNetworkDevice_NotFound(t *testing.T) {
	s := newNetworkDeviceTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/devices/nonexistent", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkDeviceRouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleDeleteNetworkDevice（DELETE /api/v1/network/devices/{id}）
// =============================================================================

// TestHandleDeleteNetworkDevice_Success 验证正常删除返回 200。
func TestHandleDeleteNetworkDevice_Success(t *testing.T) {
	s := newNetworkDeviceTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateNetworkDevice("default", &store.NetworkDevice{
		Name: "to-delete",
		Type: "switch",
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/network/devices/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkDeviceRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
}

// =============================================================================
// handleNetworkDeviceMetrics（GET /api/v1/network/devices/{id}/metrics）
// =============================================================================

// TestHandleNetworkDeviceMetrics_Success 验证获取指标返回 200。
func TestHandleNetworkDeviceMetrics_Success(t *testing.T) {
	s := newNetworkDeviceTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateNetworkDevice("default", &store.NetworkDevice{
		Name: "metric-dev",
		Type: "router",
	})
	s.store.StoreNetworkMetrics(created.ID, &store.NetworkMetrics{
		CPUUsage:    45.2,
		MemoryUsage: 60.0,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/devices/"+created.ID+"/metrics", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkDeviceRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
}

// =============================================================================
// handleNetworkDeviceConfig（POST /api/v1/network/devices/{id}/config）
// =============================================================================

// TestHandleNetworkDeviceConfig_Success 验证下发配置返回 200。
func TestHandleNetworkDeviceConfig_Success(t *testing.T) {
	s := newNetworkDeviceTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateNetworkDevice("default", &store.NetworkDevice{
		Name: "cfg-dev",
		Type: "switch",
	})

	body := `{"config":"interface GigabitEthernet0/0/1\n port link-mode route"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/devices/"+created.ID+"/config", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkDeviceRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var dev store.NetworkDevice
	if err := json.Unmarshal(w.Body.Bytes(), &dev); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dev.Config == "" {
		t.Fatal("config is empty")
	}
}

// TestHandleNetworkDeviceConfig_EmptyConfig 验证空配置返回 400。
func TestHandleNetworkDeviceConfig_EmptyConfig(t *testing.T) {
	s := newNetworkDeviceTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateNetworkDevice("default", &store.NetworkDevice{
		Name: "cfg-dev2",
		Type: "switch",
	})

	body := `{"config":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/devices/"+created.ID+"/config", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkDeviceRouting(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

// =============================================================================
// handleNetworkDiscover（POST /api/v1/network/discover）
// =============================================================================

// TestHandleNetworkDiscover_Success 验证正常发现返回 200。
func TestHandleNetworkDiscover_Success(t *testing.T) {
	s := newNetworkDeviceTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"subnet":"192.168.1.0/24"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/discover", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkDiscover(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Subnet  string `json:"subnet"`
		Found   int    `json:"found"`
		Scanned int    `json:"scanned"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Found == 0 {
		t.Fatal("found=0, want >0")
	}
}

// TestHandleNetworkDiscover_InvalidCIDR 验证无效 CIDR 返回 400。
func TestHandleNetworkDiscover_InvalidCIDR(t *testing.T) {
	s := newNetworkDeviceTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"subnet":"not-a-cidr"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/discover", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkDiscover(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

// =============================================================================
// 鉴权
// =============================================================================

// TestHandleNetworkDevices_NoAuth 验证无 token 返回 401。
func TestHandleNetworkDevices_NoAuth(t *testing.T) {
	s := newNetworkDeviceTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/devices", nil)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkDevices(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", w.Code)
	}
}