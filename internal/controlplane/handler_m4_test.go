// handler_m4_test.go 为 任务补齐此前无单测的 HTTP handler 单元测试。
//
// 覆盖范围（10 个 handler，29 个用例）：
//   - handleAlertRules         （GET 列表 / POST 创建告警规则）
//   - handleAlertRuleRouting   （DELETE 删除告警规则）
//   - handleDevices            （GET 设备列表）
//   - handleAgents             （GET agent 列表）
//   - handleTaskResult         （GET 单条任务结果）
//   - handleDeviceMetrics      （GET 设备监控指标，最新值/历史时序）
//   - handleInstallSh          （GET /install.sh 自举脚本）
//   - handleServeAgent         （GET /bin/opsmesh-agent 二进制分发）
//   - handleScaleDeployment    （POST deployment scale，鉴权/参数错误路径）
//   - handleRollbackDeployment （POST deployment rollback，鉴权错误路径）
//
// 测试模式：直接装配 Server{store: NewMemoryStore()}，用 httptest 发请求，
// 注入 X-Tenant-ID 等网关头或 Authorization: Bearer token，断言 HTTP status code 与响应体。
// 每个 handler 至少一个 happy path 加一个错误 case（租户缺失 / 资源不存在 / 越权 / method not allowed）。
//
// 注意：globalAlertRules 为进程内全局存储，本文件用独立租户 ID "m4-alert" 避免与其他测试用例冲突。
package controlplane

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"opsmesh/internal/config"
	"opsmesh/internal/k8s"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// newM4TestServer 构造 M4 handler 测试用 Server（demo 模式、无 auth、无预置任务）。
// 与 newExtraTestServer 同语义，独立命名以区分 M4 测试上下文。
func newM4TestServer() *Server {
	st := store.NewMemoryStore()
	return &Server{
		store:       st,
		cfg:         &config.Config{TaskMaxRetries: 3, Demo: true},
		requireAuth: false,
	}
}

// newM4AuthTestServer 构造带 JWT 能力的测试 Server，用于需要 Bearer token 鉴权的 handler
// （如 K8s deployment scale/rollback 的 requirePermission 路径）。
func newM4AuthTestServer(t *testing.T) *Server {
	t.Helper()
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3, Demo: true, PublicRegister: true, AllowPublicRegister: true},
		jwtSecret:    []byte("m4-jwt-secret-for-handler-test-32bytes!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
}

// =============================================================================
// handleAlertRules（GET 列表 / POST 创建告警规则）
// =============================================================================

// TestHandlerM4AlertRules_CreateAndList 验证 POST 创建告警规则后 GET 能返回该规则。
func TestHandlerM4AlertRules_CreateAndList(t *testing.T) {
	s := newM4TestServer()

	// POST 创建一条告警规则。
	body, _ := json.Marshal(map[string]interface{}{
		"metric":      "cpu.usage",
		"op":          ">",
		"threshold":   80.0,
		"severity":    "critical",
		"message":     "CPU too high",
		"enabled":     true,
		"forDuration": "5m",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alert-rules", bytes.NewReader(body))
	req.Header.Set("X-Tenant-ID", "m4-alert")
	rec := httptest.NewRecorder()
	s.handleAlertRules(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var created AlertRule
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	if created.ID == "" || created.TenantID != "m4-alert" {
		t.Fatalf("created rule unexpected: %+v", created)
	}

	// GET 列表应包含刚创建的规则。
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/alert-rules", nil)
	req2.Header.Set("X-Tenant-ID", "m4-alert")
	rec2 := httptest.NewRecorder()
	s.handleAlertRules(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("list status=%d, want 200; body=%s", rec2.Code, rec2.Body.String())
	}
	var rules []*AlertRule
	if err := json.Unmarshal(rec2.Body.Bytes(), &rules); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	found := false
	for _, r := range rules {
		if r.ID == created.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("created rule %s not found in list", created.ID)
	}
}

// TestHandlerM4AlertRules_InvalidBody 验证 POST 缺少必填字段或非法 op 返回 400。
func TestHandlerM4AlertRules_InvalidBody(t *testing.T) {
	s := newM4TestServer()

	// 缺少 metric。
	body1, _ := json.Marshal(map[string]interface{}{"op": ">", "threshold": 80.0})
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/alert-rules", bytes.NewReader(body1))
	req1.Header.Set("X-Tenant-ID", "m4-alert")
	rec1 := httptest.NewRecorder()
	s.handleAlertRules(rec1, req1)
	if rec1.Code != http.StatusBadRequest {
		t.Fatalf("missing metric status=%d, want 400; body=%s", rec1.Code, rec1.Body.String())
	}

	// 非法 op。
	body2, _ := json.Marshal(map[string]interface{}{"metric": "cpu.usage", "op": "!=", "threshold": 80.0})
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/alert-rules", bytes.NewReader(body2))
	req2.Header.Set("X-Tenant-ID", "m4-alert")
	rec2 := httptest.NewRecorder()
	s.handleAlertRules(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("invalid op status=%d, want 400; body=%s", rec2.Code, rec2.Body.String())
	}
}

// TestHandlerM4AlertRules_MethodNotAllowed 验证非 GET/POST 方法返回 405。
func TestHandlerM4AlertRules_MethodNotAllowed(t *testing.T) {
	s := newM4TestServer()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/alert-rules", nil)
	req.Header.Set("X-Tenant-ID", "m4-alert")
	rec := httptest.NewRecorder()
	s.handleAlertRules(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

// =============================================================================
// handleAlertRuleRouting（DELETE 删除告警规则）
// =============================================================================

// TestHandlerM4AlertRuleRouting_Delete 验证 DELETE 已存在的告警规则返回 200。
func TestHandlerM4AlertRuleRouting_Delete(t *testing.T) {
	s := newM4TestServer()
	// 直接注入一条已知 ID 的规则。
	rule := &AlertRule{
		ID:       "ar-m4-test-delete",
		TenantID: "m4-alert",
		Metric:   "cpu.usage",
		Op:       ">",
		Severity: "warning",
	}
	s.saveAlertRule(rule)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/alert-rules/ar-m4-test-delete", nil)
	req.Header.Set("X-Tenant-ID", "m4-alert")
	rec := httptest.NewRecorder()
	s.handleAlertRuleRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandlerM4AlertRuleRouting_NotFound 验证 DELETE 不存在的规则返回 404。
func TestHandlerM4AlertRuleRouting_NotFound(t *testing.T) {
	s := newM4TestServer()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/alert-rules/ar-no-such", nil)
	req.Header.Set("X-Tenant-ID", "m4-alert")
	rec := httptest.NewRecorder()
	s.handleAlertRuleRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandlerM4AlertRuleRouting_MethodNotAllowed 验证非 DELETE 方法返回 405。
func TestHandlerM4AlertRuleRouting_MethodNotAllowed(t *testing.T) {
	s := newM4TestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/alert-rules/ar-m4-method", nil)
	req.Header.Set("X-Tenant-ID", "m4-alert")
	rec := httptest.NewRecorder()
	s.handleAlertRuleRouting(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

// =============================================================================
// handleDevices（GET 设备列表）
// =============================================================================

// TestHandlerM4Devices_Happy 验证 GET 返回当前租户的设备快照。
func TestHandlerM4Devices_Happy(t *testing.T) {
	s := newM4TestServer()
	s.store.UpsertDevice(&proto.DeviceInfo{
		DeviceID: "m4-dev-1", Segment: "seg-a", TenantID: "m4-dev",
		IP: "10.0.0.1", State: "online", Managed: true,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	req.Header.Set("X-Tenant-ID", "m4-dev")
	rec := httptest.NewRecorder()
	s.handleDevices(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var snap map[string][]proto.DeviceInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	total := 0
	for _, devs := range snap {
		total += len(devs)
	}
	if total == 0 {
		t.Fatalf("expected at least 1 device, got snap=%+v", snap)
	}
}

// TestHandlerM4Devices_MethodNotAllowed 验证非 GET 方法返回 405。
func TestHandlerM4Devices_MethodNotAllowed(t *testing.T) {
	s := newM4TestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices", nil)
	rec := httptest.NewRecorder()
	s.handleDevices(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

// TestHandlerM4Devices_RequireAuth 验证 requireAuth 且无身份时返回 401。
func TestHandlerM4Devices_RequireAuth(t *testing.T) {
	s := newM4TestServer()
	s.requireAuth = true
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	rec := httptest.NewRecorder()
	s.handleDevices(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", rec.Code)
	}
}

// =============================================================================
// handleAgents（GET agent 列表）
// =============================================================================

// TestHandlerM4Agents_Happy 验证 GET 返回当前租户的 agent 列表。
func TestHandlerM4Agents_Happy(t *testing.T) {
	s := newM4TestServer()
	s.store.Register(&proto.AgentInfo{
		AgentID: "m4-agent-1", Segment: "seg-a", TenantID: "m4-agent",
		Hostname: "host-1", Status: "online",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	req.Header.Set("X-Tenant-ID", "m4-agent")
	rec := httptest.NewRecorder()
	s.handleAgents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var agents []map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &agents); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(agents) == 0 {
		t.Fatal("expected at least 1 agent, got empty list")
	}
	if agents[0]["agentID"] != "m4-agent-1" {
		t.Fatalf("agentID=%q, want m4-agent-1", agents[0]["agentID"])
	}
}

// TestHandlerM4Agents_MethodNotAllowed 验证非 GET 方法返回 405。
func TestHandlerM4Agents_MethodNotAllowed(t *testing.T) {
	s := newM4TestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", nil)
	rec := httptest.NewRecorder()
	s.handleAgents(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

// TestHandlerM4Agents_RequireAuth 验证 requireAuth 且无身份时返回 401。
func TestHandlerM4Agents_RequireAuth(t *testing.T) {
	s := newM4TestServer()
	s.requireAuth = true
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	rec := httptest.NewRecorder()
	s.handleAgents(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", rec.Code)
	}
}

// =============================================================================
// handleTaskResult（GET 单条任务结果）
// =============================================================================

// TestHandlerM4TaskResult_Happy 验证 GET 已有结果的任务返回 200 + 结果体。
func TestHandlerM4TaskResult_Happy(t *testing.T) {
	s := newM4TestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "m4-task"})
	tk := s.store.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "m4-task", Type: "shell", Command: "echo hi"})
	s.store.ClaimTask(a.AgentID)
	s.store.SubmitResult(&proto.TaskResult{TaskID: tk.TaskID, AgentID: a.AgentID, ExitCode: 0, Stdout: "hi"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+tk.TaskID+"/result", nil)
	req.Header.Set("X-Tenant-ID", "m4-task")
	rec := httptest.NewRecorder()
	s.handleTaskResult(rec, req, tk.TaskID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandlerM4TaskResult_NotFound 验证 GET 不存在的结果返回 404。
func TestHandlerM4TaskResult_NotFound(t *testing.T) {
	s := newM4TestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/no-such/result", nil)
	req.Header.Set("X-Tenant-ID", "m4-task")
	rec := httptest.NewRecorder()
	s.handleTaskResult(rec, req, "no-such")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandlerM4TaskResult_TenantMismatch 验证越权访问他租户任务结果返回 403。
func TestHandlerM4TaskResult_TenantMismatch(t *testing.T) {
	s := newM4TestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t-owner"})
	tk := s.store.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t-owner", Type: "shell", Command: "echo hi"})
	s.store.ClaimTask(a.AgentID)
	s.store.SubmitResult(&proto.TaskResult{TaskID: tk.TaskID, AgentID: a.AgentID, ExitCode: 0})

	// 以 t-attacker 身份访问 t-owner 的任务结果。
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+tk.TaskID+"/result", nil)
	req.Header.Set("X-Tenant-ID", "t-attacker")
	rec := httptest.NewRecorder()
	s.handleTaskResult(rec, req, tk.TaskID)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant status=%d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// handleDeviceMetrics（GET 设备监控指标）
// =============================================================================

// TestHandlerM4DeviceMetrics_Happy 验证 GET 返回设备最新监控指标。
func TestHandlerM4DeviceMetrics_Happy(t *testing.T) {
	s := newM4TestServer()
	s.store.UpsertDevice(&proto.DeviceInfo{
		DeviceID: "m4-metrics-dev", Segment: "seg-a", TenantID: "m4-metrics",
		IP: "10.0.0.1", State: "online", Managed: true,
	})
	s.store.StoreDeviceMetrics("m4-metrics-dev", &proto.DeviceMetrics{
		DeviceID: "m4-metrics-dev",
		Hostname: "host-1",
		CPU:      proto.CPUMetrics{Cores: 4, Usage: 25.5},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices/m4-metrics-dev/metrics", nil)
	req.Header.Set("X-Tenant-ID", "m4-metrics")
	rec := httptest.NewRecorder()
	s.handleDeviceMetrics(rec, req, "m4-metrics-dev")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var m proto.DeviceMetrics
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if m.DeviceID != "m4-metrics-dev" {
		t.Fatalf("deviceID=%q, want m4-metrics-dev", m.DeviceID)
	}
}

// TestHandlerM4DeviceMetrics_DeviceNotFound 验证设备不存在时返回 404。
func TestHandlerM4DeviceMetrics_DeviceNotFound(t *testing.T) {
	s := newM4TestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices/no-such/metrics", nil)
	req.Header.Set("X-Tenant-ID", "m4-metrics")
	rec := httptest.NewRecorder()
	s.handleDeviceMetrics(rec, req, "no-such")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandlerM4DeviceMetrics_NoMetrics 验证设备存在但无指标数据时返回 404。
func TestHandlerM4DeviceMetrics_NoMetrics(t *testing.T) {
	s := newM4TestServer()
	s.store.UpsertDevice(&proto.DeviceInfo{
		DeviceID: "m4-no-metrics", Segment: "seg-a", TenantID: "m4-metrics",
		IP: "10.0.0.2", State: "online", Managed: true,
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices/m4-no-metrics/metrics", nil)
	req.Header.Set("X-Tenant-ID", "m4-metrics")
	rec := httptest.NewRecorder()
	s.handleDeviceMetrics(rec, req, "m4-no-metrics")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandlerM4DeviceMetrics_InvalidRange 验证非法 range 参数返回 400。
func TestHandlerM4DeviceMetrics_InvalidRange(t *testing.T) {
	s := newM4TestServer()
	s.store.UpsertDevice(&proto.DeviceInfo{
		DeviceID: "m4-range-dev", Segment: "seg-a", TenantID: "m4-metrics",
		IP: "10.0.0.3", State: "online", Managed: true,
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices/m4-range-dev/metrics?range=99x", nil)
	req.Header.Set("X-Tenant-ID", "m4-metrics")
	rec := httptest.NewRecorder()
	s.handleDeviceMetrics(rec, req, "m4-range-dev")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandlerM4DeviceMetrics_MethodNotAllowed 验证非 GET 方法返回 405。
func TestHandlerM4DeviceMetrics_MethodNotAllowed(t *testing.T) {
	s := newM4TestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/x/metrics", nil)
	rec := httptest.NewRecorder()
	s.handleDeviceMetrics(rec, req, "x")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

// =============================================================================
// handleInstallSh（GET /install.sh 自举脚本）
// =============================================================================

// TestHandlerM4InstallSh_Happy 验证 demo 模式下 GET /install.sh 返回 200 + shell 脚本。
func TestHandlerM4InstallSh_Happy(t *testing.T) {
	s := newM4TestServer()
	req := httptest.NewRequest(http.MethodGet, "/install.sh", nil)
	rec := httptest.NewRecorder()
	s.handleInstallSh(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/x-shellscript") {
		t.Fatalf("Content-Type=%q, want text/x-shellscript", ct)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("install script body is empty")
	}
}

// TestHandlerM4InstallSh_MethodNotAllowed 验证非 GET 方法返回 405。
func TestHandlerM4InstallSh_MethodNotAllowed(t *testing.T) {
	s := newM4TestServer()
	req := httptest.NewRequest(http.MethodPost, "/install.sh", nil)
	rec := httptest.NewRecorder()
	s.handleInstallSh(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

// TestHandlerM4InstallSh_NonDemoNoToken 验证非 demo 模式且无 token 时返回 401。
func TestHandlerM4InstallSh_NonDemoNoToken(t *testing.T) {
	s := newM4TestServer()
	s.cfg.Demo = false
	req := httptest.NewRequest(http.MethodGet, "/install.sh", nil)
	rec := httptest.NewRecorder()
	s.handleInstallSh(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// handleServeAgent（GET /bin/opsmesh-agent 二进制分发）
// =============================================================================

// TestHandlerM4ServeAgent_Happy 验证 demo 模式下 GET /bin/opsmesh-agent 返回 200 + 二进制流。
func TestHandlerM4ServeAgent_Happy(t *testing.T) {
	s := newM4TestServer()
	req := httptest.NewRequest(http.MethodGet, "/bin/opsmesh-agent", nil)
	rec := httptest.NewRecorder()
	s.handleServeAgent(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/octet-stream") {
		t.Fatalf("Content-Type=%q, want application/octet-stream", ct)
	}
}

// TestHandlerM4ServeAgent_MethodNotAllowed 验证非 GET 方法返回 405。
func TestHandlerM4ServeAgent_MethodNotAllowed(t *testing.T) {
	s := newM4TestServer()
	req := httptest.NewRequest(http.MethodPost, "/bin/opsmesh-agent", nil)
	rec := httptest.NewRecorder()
	s.handleServeAgent(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

// TestHandlerM4ServeAgent_NonDemoNoToken 验证非 demo 模式且无 token 时返回 401。
func TestHandlerM4ServeAgent_NonDemoNoToken(t *testing.T) {
	s := newM4TestServer()
	s.cfg.Demo = false
	req := httptest.NewRequest(http.MethodGet, "/bin/opsmesh-agent", nil)
	rec := httptest.NewRecorder()
	s.handleServeAgent(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// handleScaleDeployment（POST deployment scale，鉴权/参数错误路径）
// 实际 K8s API 调用需要真实集群连接，此处聚焦鉴权与参数校验错误路径。
// =============================================================================

// TestHandlerM4ScaleDeployment_NoAuth 验证无 Bearer token 时 requirePermission 返回 401。
func TestHandlerM4ScaleDeployment_NoAuth(t *testing.T) {
	s := newM4TestServer()
	client := &k8s.K8sClient{}
	body := strings.NewReader(`{"replicas":3}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/k8s/clusters/c1/deployments/ns/dep/scale", body)
	rec := httptest.NewRecorder()
	s.handleScaleDeployment(rec, req, client, "ns", "dep")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandlerM4ScaleDeployment_InvalidJSON 验证有效 token 但 body 为无效 JSON 时返回 400。
func TestHandlerM4ScaleDeployment_InvalidJSON(t *testing.T) {
	s := newM4AuthTestServer(t)
	auth := loginAsAdmin(t, s)
	client := &k8s.K8sClient{}
	req := doWithAuth(http.MethodPost, "/api/v1/k8s/clusters/c1/deployments/ns/dep/scale", auth, "not-json{")
	rec := httptest.NewRecorder()
	s.handleScaleDeployment(rec, req, client, "ns", "dep")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// handleRollbackDeployment（POST deployment rollback，鉴权错误路径）
// =============================================================================

// TestHandlerM4RollbackDeployment_NoAuth 验证无 Bearer token 时 requirePermission 返回 401。
func TestHandlerM4RollbackDeployment_NoAuth(t *testing.T) {
	s := newM4TestServer()
	client := &k8s.K8sClient{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/k8s/clusters/c1/deployments/ns/dep/rollback", nil)
	rec := httptest.NewRecorder()
	s.handleRollbackDeployment(rec, req, client, "ns", "dep")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandlerM4RollbackDeployment_NoAuthNonDemo 验证非 demo 模式下无 token 同样返回 401。
func TestHandlerM4RollbackDeployment_NoAuthNonDemo(t *testing.T) {
	s := newM4TestServer()
	s.cfg.Demo = false
	client := &k8s.K8sClient{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/k8s/clusters/c1/deployments/ns/dep/rollback", nil)
	rec := httptest.NewRecorder()
	s.handleRollbackDeployment(rec, req, client, "ns", "dep")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

// 确保 time 包被使用（部分用例可能间接依赖 time.Time 零值比较）。
var _ = time.Time{}
