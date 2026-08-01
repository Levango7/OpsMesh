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
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// 本文件为 M2 handler 测试补全：覆盖 server.go 中此前无单测的 HTTP handler。
//
// 覆盖范围：
//   - handleCancelTask      （F3 取消任务）
//   - handleRetireDevice    （F5 退役设备）
//   - handleAckAlert        （M7 告警确认）
//   - handleSilenceAlert    （M7 告警静默）
//   - handleHealthz         （P2-12 健康检查）
//   - handleAutoProvision   （B1 自动纳管触发）
//   - handleProvision       （B1 纳管签发，错误 case；happy path 见 server_test.go）
//   - handleMe              （当前租户解析）
//   - handleBatchCreateTasks（P0-3 批量下发，错误 case；happy path 见 endpoint_test.go）
//   - handleAudits          （P0-4 审计检索，补充 case；happy path 见 endpoint_test.go）
//
// 测试模式：直接装配 Server{reg: NewRegistryWithStore(MemoryStore)}，用 httptest 发请求，
// 注入 X-Tenant-ID 等网关头，断言 HTTP status code 与响应体。每个 handler 至少一个 happy path
// 加一个错误 case（租户缺失 / 资源不存在 / 越权 / method not allowed）。

// newExtraTestServer 构造一个最小测试控制面（无 demo 预置、无 auth、无总线），便于精确断言。
// 与 endpoint_test.go 的 newTestServer（开 demo）区分开，避免预置告警/任务干扰断言。
func newExtraTestServer() *Server {
	st := store.NewMemoryStore()
	return &Server{
		store:       st,
		cfg:         &config.Config{TaskMaxRetries: 3},
		requireAuth: false,
	}
}

// =============================================================================
// handleCancelTask（F3 取消任务）
// =============================================================================

// TestHandleCancelTask_Happy 验证取消 pending 任务成功，状态翻 cancelled。
func TestHandleCancelTask_Happy(t *testing.T) {
	s := newExtraTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	tk := s.store.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "echo hi"})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+tk.TaskID+"/cancel", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleCancelTask(rec, req, tk.TaskID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "cancelled" {
		t.Fatalf("status=%q, want cancelled", resp["status"])
	}
	// 验证任务状态已变 cancelled
	got := s.store.AllTasks("t1")
	if len(got) == 0 || got[0].Status != "cancelled" {
		t.Fatalf("task status not cancelled: %+v", got)
	}
}

// TestHandleCancelTask_NotFound 验证取消不存在的任务返回 404。
func TestHandleCancelTask_NotFound(t *testing.T) {
	s := newExtraTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/nope/cancel", nil)
	rec := httptest.NewRecorder()
	s.handleCancelTask(rec, req, "nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

// TestHandleCancelTask_NotCancellable 验证已 done 的任务不可取消，返回 404。
func TestHandleCancelTask_NotCancellable(t *testing.T) {
	s := newExtraTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	tk := s.store.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "echo hi"})
	// 上报成功结果，任务变 done
	s.store.SubmitResult(&proto.TaskResult{TaskID: tk.TaskID, AgentID: a.AgentID, ExitCode: 0})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+tk.TaskID+"/cancel", nil)
	rec := httptest.NewRecorder()
	s.handleCancelTask(rec, req, tk.TaskID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("done task cancel=%d, want 404", rec.Code)
	}
}

// TestHandleCancelTask_RequireAuth 验证 requireAuth 且无网关注入身份时返回 401。
func TestHandleCancelTask_RequireAuth(t *testing.T) {
	s := newExtraTestServer()
	s.requireAuth = true
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/x/cancel", nil)
	rec := httptest.NewRecorder()
	s.handleCancelTask(rec, req, "x")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", rec.Code)
	}
}

// TestHandleTaskRouting_CancelDispatch 验证 handleTaskRouting 能正确分派到 cancel 子路径。
func TestHandleTaskRouting_CancelDispatch(t *testing.T) {
	s := newExtraTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	tk := s.store.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "echo hi"})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+tk.TaskID+"/cancel", nil)
	rec := httptest.NewRecorder()
	s.handleTaskRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("routing cancel=%d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleTaskRouting_UnknownSubPath 验证未知子路径返回 404。
func TestHandleTaskRouting_UnknownSubPath(t *testing.T) {
	s := newExtraTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/foo/bar", nil)
	rec := httptest.NewRecorder()
	s.handleTaskRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown subpath=%d, want 404", rec.Code)
	}
}

// =============================================================================
// handleRetireDevice（F5 退役设备）
// =============================================================================

// TestHandleRetireDevice_Happy 验证退役已纳管设备成功，状态变 retired。
func TestHandleRetireDevice_Happy(t *testing.T) {
	s := newExtraTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	devID := "dev-" + a.AgentID

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/devices/"+devID, nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleRetireDevice(rec, req, devID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "retired" {
		t.Fatalf("status=%q, want retired", resp["status"])
	}
	dev := s.store.Device(devID)
	if dev == nil || !dev.Retired {
		t.Fatalf("device not retired: %+v", dev)
	}
}

// TestHandleRetireDevice_NotFound 验证退役不存在的设备返回 404。
func TestHandleRetireDevice_NotFound(t *testing.T) {
	s := newExtraTestServer()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/devices/nope", nil)
	rec := httptest.NewRecorder()
	s.handleRetireDevice(rec, req, "nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

// TestHandleRetireDevice_TenantMismatch 验证跨租户退役被拒（404）。
func TestHandleRetireDevice_TenantMismatch(t *testing.T) {
	s := newExtraTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	devID := "dev-" + a.AgentID

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/devices/"+devID, nil)
	req.Header.Set("X-Tenant-ID", "t2") // 冒充他租户
	rec := httptest.NewRecorder()
	s.handleRetireDevice(rec, req, devID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant retire=%d, want 404", rec.Code)
	}
}

// TestHandleRetireDevice_RequireAuth 验证 requireAuth 且无身份时返回 401。
func TestHandleRetireDevice_RequireAuth(t *testing.T) {
	s := newExtraTestServer()
	s.requireAuth = true
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/devices/x", nil)
	rec := httptest.NewRecorder()
	s.handleRetireDevice(rec, req, "x")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", rec.Code)
	}
}

// TestHandleDeviceRouting_RetireDispatch 验证 handleDeviceRouting 能正确分派 DELETE 到退役。
func TestHandleDeviceRouting_RetireDispatch(t *testing.T) {
	s := newExtraTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	devID := "dev-" + a.AgentID

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/devices/"+devID, nil)
	rec := httptest.NewRecorder()
	s.handleDeviceRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("routing retire=%d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// handleAckAlert（M7 告警确认）
// =============================================================================

// TestHandleAckAlert_Happy 验证确认告警成功，状态变 acknowledged 并记录确认人。
func TestHandleAckAlert_Happy(t *testing.T) {
	s := newExtraTestServer()
	s.store.AddAlert(&proto.Alert{AlertID: "al-1", TenantID: "t1", Severity: "critical", Status: proto.AlertStatusFiring})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/al-1/ack", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-User-Id", "u1")
	rec := httptest.NewRecorder()
	s.handleAckAlert(rec, req, "al-1")

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "acknowledged" {
		t.Fatalf("status=%q, want acknowledged", resp["status"])
	}
	al := s.store.Alert("al-1")
	if al == nil || al.Status != proto.AlertStatusAcknowledged || al.AcknowledgedBy != "u1" {
		t.Fatalf("alert not acked: %+v", al)
	}
}

// TestHandleAckAlert_NotFound 验证确认不存在的告警返回 404。
func TestHandleAckAlert_NotFound(t *testing.T) {
	s := newExtraTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/nope/ack", nil)
	rec := httptest.NewRecorder()
	s.handleAckAlert(rec, req, "nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

// TestHandleAckAlert_TenantMismatch 验证越权确认他租户告警返回 403。
func TestHandleAckAlert_TenantMismatch(t *testing.T) {
	s := newExtraTestServer()
	s.store.AddAlert(&proto.Alert{AlertID: "al-1", TenantID: "t1", Severity: "critical", Status: proto.AlertStatusFiring})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/al-1/ack", nil)
	req.Header.Set("X-Tenant-ID", "t2") // 越权
	rec := httptest.NewRecorder()
	s.handleAckAlert(rec, req, "al-1")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant ack=%d, want 403", rec.Code)
	}
}

// TestHandleAckAlert_RequireAuth 验证 requireAuth 且无身份时返回 401。
func TestHandleAckAlert_RequireAuth(t *testing.T) {
	s := newExtraTestServer()
	s.requireAuth = true
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/x/ack", nil)
	rec := httptest.NewRecorder()
	s.handleAckAlert(rec, req, "x")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", rec.Code)
	}
}

// =============================================================================
// handleSilenceAlert（M7 告警静默）
// =============================================================================

// TestHandleSilenceAlert_Happy 验证带 duration + comment 静默告警成功。
func TestHandleSilenceAlert_Happy(t *testing.T) {
	s := newExtraTestServer()
	s.store.AddAlert(&proto.Alert{AlertID: "al-2", TenantID: "t1", Severity: "warning", Status: proto.AlertStatusFiring})

	body := strings.NewReader(`{"durationMinutes":60,"comment":"investigating"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/al-2/silence", body)
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-User-Id", "u1")
	rec := httptest.NewRecorder()
	s.handleSilenceAlert(rec, req, "al-2")

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "silenced" {
		t.Fatalf("status=%q, want silenced", resp["status"])
	}
	al := s.store.Alert("al-2")
	if al == nil || al.Status != proto.AlertStatusSilenced || al.Comment != "investigating" {
		t.Fatalf("alert not silenced: %+v", al)
	}
}

// TestHandleSilenceAlert_DefaultDuration 验证空 body 时使用默认静默参数仍成功。
func TestHandleSilenceAlert_DefaultDuration(t *testing.T) {
	s := newExtraTestServer()
	s.store.AddAlert(&proto.Alert{AlertID: "al-3", TenantID: "t1", Severity: "warning", Status: proto.AlertStatusFiring})

	// 空 body：durationMinutes=0 -> until=now（静默截止为当前时刻），仍应 200
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/al-3/silence", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleSilenceAlert(rec, req, "al-3")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleSilenceAlert_NotFound 验证静默不存在的告警返回 404。
func TestHandleSilenceAlert_NotFound(t *testing.T) {
	s := newExtraTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/nope/silence", nil)
	rec := httptest.NewRecorder()
	s.handleSilenceAlert(rec, req, "nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

// TestHandleSilenceAlert_TenantMismatch 验证越权静默他租户告警返回 403。
func TestHandleSilenceAlert_TenantMismatch(t *testing.T) {
	s := newExtraTestServer()
	s.store.AddAlert(&proto.Alert{AlertID: "al-4", TenantID: "t1", Severity: "warning", Status: proto.AlertStatusFiring})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/al-4/silence", nil)
	req.Header.Set("X-Tenant-ID", "t2")
	rec := httptest.NewRecorder()
	s.handleSilenceAlert(rec, req, "al-4")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant silence=%d, want 403", rec.Code)
	}
}

// TestHandleAlertRouting_AckAndSilence 验证 handleAlertRouting 能正确分派 ack/silence 子路径。
func TestHandleAlertRouting_AckAndSilence(t *testing.T) {
	s := newExtraTestServer()
	s.store.AddAlert(&proto.Alert{AlertID: "al-5", TenantID: "t1", Severity: "critical", Status: proto.AlertStatusFiring})

	// ack via routing
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/al-5/ack", nil)
	rec := httptest.NewRecorder()
	s.handleAlertRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("routing ack=%d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// silence via routing
	s.store.AddAlert(&proto.Alert{AlertID: "al-6", TenantID: "t1", Severity: "warning", Status: proto.AlertStatusFiring})
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/al-6/silence", nil)
	rec2 := httptest.NewRecorder()
	s.handleAlertRouting(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("routing silence=%d, want 200; body=%s", rec2.Code, rec2.Body.String())
	}
}

// =============================================================================
// handleHealthz（P2-12 健康检查）
// =============================================================================

// TestHandleHealthz_Happy 验证 GET /healthz 返回 200 + {"status":"ok"}。
func TestHandleHealthz_Happy(t *testing.T) {
	s := newExtraTestServer()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	s.handleHealthz(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "ok" {
		t.Fatalf("status=%q, want ok", resp["status"])
	}
}

// TestHandleHealthz_MethodNotAllowed 验证非 GET 方法返回 405。
func TestHandleHealthz_MethodNotAllowed(t *testing.T) {
	s := newExtraTestServer()
	req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	rec := httptest.NewRecorder()
	s.handleHealthz(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

// =============================================================================
// handleAutoProvision（B1 自动纳管触发）
// =============================================================================
//
// 注意：handleAutoProvision 内部调用 provision.AutoProvision -> discover.Sweep 做真实 TCP 存活扫描。
// 为避免测试依赖真实网络环境且不引入长 timeout，happy path 使用 127.0.0.1/32（本机回环）：
//   - 若本机未监听 22/9100：TCP 连接立即被 RST（connection refused），alive=false，sum.Scanned=0；
//   - 若本机监听了：alive=true，会 UpsertDevice + Provision，sum.Scanned=1。
// 两种情况均返回 200，断言仅校验状态码与 sum 解码成功，不绑定具体数值。

// TestHandleAutoProvision_Happy 验证带 cidrs 的请求返回 200 + Summary。
func TestHandleAutoProvision_Happy(t *testing.T) {
	s := newExtraTestServer()
	body := strings.NewReader(`{"cidrs":["127.0.0.1/32"],"tenantID":"t1"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/provision/auto", body)
	rec := httptest.NewRecorder()
	s.handleAutoProvision(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var sum struct {
		Scanned     int      `json:"scanned"`
		Registered  int      `json:"registered"`
		Provisioned int      `json:"provisioned"`
		SSHPushed   int      `json:"sshPushed"`
		Failures    []string `json:"failures"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &sum); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if sum.Scanned < 0 {
		t.Fatalf("scanned=%d, want >=0", sum.Scanned)
	}
}

// TestHandleAutoProvision_NoCIDRs 验证无 cidrs 且 cfg.SegmentCIDR 为空时返回 400。
func TestHandleAutoProvision_NoCIDRs(t *testing.T) {
	s := newExtraTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/provision/auto", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	s.handleAutoProvision(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleAutoProvision_FallbackSegmentCIDR 验证 body 无 cidrs 时回退 cfg.SegmentCIDR。
func TestHandleAutoProvision_FallbackSegmentCIDR(t *testing.T) {
	s := newExtraTestServer()
	s.cfg.SegmentCIDR = "127.0.0.1/32"
	req := httptest.NewRequest(http.MethodPost, "/api/v1/provision/auto", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	s.handleAutoProvision(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleAutoProvision_MethodNotAllowed 验证非 POST 方法返回 405。
func TestHandleAutoProvision_MethodNotAllowed(t *testing.T) {
	s := newExtraTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/provision/auto", nil)
	rec := httptest.NewRecorder()
	s.handleAutoProvision(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

// TestHandleAutoProvision_RequireAuth 验证 requireAuth 且无身份时返回 401。
func TestHandleAutoProvision_RequireAuth(t *testing.T) {
	s := newExtraTestServer()
	s.requireAuth = true
	req := httptest.NewRequest(http.MethodPost, "/api/v1/provision/auto", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	s.handleAutoProvision(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", rec.Code)
	}
}

// =============================================================================
// handleProvision 错误 case（B1 纳管签发；happy path 见 server_test.go TestHandleProvision_ReturnsToken）
// =============================================================================

// TestHandleProvision_NotFound 验证为不存在的设备签发返回 404。
func TestHandleProvision_NotFound(t *testing.T) {
	s := newExtraTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/nope/provision", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	s.handleProvision(rec, req, "nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleProvision_TenantMismatch 验证越权为他租户设备签发返回 403。
func TestHandleProvision_TenantMismatch(t *testing.T) {
	s := newExtraTestServer()
	s.store.UpsertDevice(&proto.DeviceInfo{
		DeviceID: "dev-x", Segment: "seg-a", TenantID: "t1",
		IP: "10.0.0.1", State: "discovered", Managed: false,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/dev-x/provision", strings.NewReader(`{}`))
	req.Header.Set("X-Tenant-ID", "t2") // 越权
	rec := httptest.NewRecorder()
	s.handleProvision(rec, req, "dev-x")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant provision=%d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleProvision_RequireAuth 验证 requireAuth 且无身份时返回 401。
func TestHandleProvision_RequireAuth(t *testing.T) {
	s := newExtraTestServer()
	s.requireAuth = true
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/x/provision", nil)
	rec := httptest.NewRecorder()
	s.handleProvision(rec, req, "x")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", rec.Code)
	}
}

// =============================================================================
// handleMe（当前租户解析）
// =============================================================================

// TestHandleMe_Happy 验证返回网关注入的身份上下文。
func TestHandleMe_Happy(t *testing.T) {
	s := newExtraTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-User-Id", "u1")
	req.Header.Set("X-User-Roles", "admin,ops")
	rec := httptest.NewRecorder()
	s.handleMe(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["tenantID"] != "t1" || resp["userID"] != "u1" {
		t.Fatalf("me=%+v, want t1/u1", resp)
	}
	if resp["mode"] != "gateway-injected" {
		t.Fatalf("mode=%v, want gateway-injected", resp["mode"])
	}
}

// TestHandleMe_RequireAuth 验证 requireAuth 且无身份时返回 401。
func TestHandleMe_RequireAuth(t *testing.T) {
	s := newExtraTestServer()
	s.requireAuth = true
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	rec := httptest.NewRecorder()
	s.handleMe(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", rec.Code)
	}
}

// TestHandleMe_MethodNotAllowed 验证非 GET 方法返回 405。
func TestHandleMe_MethodNotAllowed(t *testing.T) {
	s := newExtraTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/me", nil)
	rec := httptest.NewRecorder()
	s.handleMe(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

// =============================================================================
// handleBatchCreateTasks 错误 case（P0-3 批量下发；happy path 见 endpoint_test.go）
// =============================================================================

// TestHandleBatchCreateTasks_EmptyTargets 验证空 targets 返回 400。
func TestHandleBatchCreateTasks_EmptyTargets(t *testing.T) {
	s := newExtraTestServer()
	body, _ := json.Marshal(map[string]interface{}{
		"targets": []string{},
		"command": "echo x",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/batch", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleBatchCreateTasks(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleBatchCreateTasks_MissingCommand 验证缺 command 返回 400。
func TestHandleBatchCreateTasks_MissingCommand(t *testing.T) {
	s := newExtraTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	body, _ := json.Marshal(map[string]interface{}{
		"targets": []string{a.AgentID},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/batch", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleBatchCreateTasks(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleBatchCreateTasks_PartialFailure 验证部分目标不存在时返回 201 + errors 列表。
func TestHandleBatchCreateTasks_PartialFailure(t *testing.T) {
	s := newExtraTestServer()
	a1 := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	// "ghost-agent" 未注册 -> 进 errors
	body, _ := json.Marshal(map[string]interface{}{
		"targets": []string{a1.AgentID, "ghost-agent"},
		"command": "echo batch",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/batch", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleBatchCreateTasks(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Count   int      `json:"count"`
		Created []string `json:"created"`
		Errors  []struct {
			Target string `json:"target"`
			Error  string `json:"error"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Count != 1 || len(resp.Created) != 1 {
		t.Fatalf("count=%d created=%d, want 1/1", resp.Count, len(resp.Created))
	}
	if len(resp.Errors) != 1 || resp.Errors[0].Target != "ghost-agent" {
		t.Fatalf("errors=%+v, want 1 ghost-agent", resp.Errors)
	}
}

// TestHandleBatchCreateTasks_RequireAuth 验证 requireAuth 且无身份时返回 401。
func TestHandleBatchCreateTasks_RequireAuth(t *testing.T) {
	s := newExtraTestServer()
	s.requireAuth = true
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/batch", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	s.handleBatchCreateTasks(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", rec.Code)
	}
}

// TestHandleBatchCreateTasks_MethodNotAllowed 验证非 POST 方法返回 405。
func TestHandleBatchCreateTasks_MethodNotAllowed(t *testing.T) {
	s := newExtraTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/batch", nil)
	rec := httptest.NewRecorder()
	s.handleBatchCreateTasks(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

// =============================================================================
// handleAudits 补充 case（P0-4 审计检索；happy path 见 endpoint_test.go）
// =============================================================================

// TestHandleAudits_RequireAuth 验证 requireAuth 且无身份时返回 401。
func TestHandleAudits_RequireAuth(t *testing.T) {
	s := newExtraTestServer()
	s.requireAuth = true
	req := httptest.NewRequest(http.MethodGet, "/api/v1/audits", nil)
	rec := httptest.NewRecorder()
	s.handleAudits(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", rec.Code)
	}
}

// TestHandleAudits_TenantIsolation 验证 requireAuth 时强制按网关注入租户过滤，
// 客户端伪造的 ?tenant= 参数被忽略，不会越权看到他租户审计。
func TestHandleAudits_TenantIsolation(t *testing.T) {
	s := newExtraTestServer()
	s.requireAuth = true
	s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	s.store.Register(&proto.AgentInfo{Segment: "seg-b", TenantID: "t2"})
	// 手动写一条 t2 的审计
	s.store.Audit(&proto.AuditEvent{TenantID: "t2", Action: "create_task", Target: "t2-task"})

	// 以 t1 身份查询，?tenant=t2 试图越权 -> handler 强制取 actx.TenantID=t1
	req := httptest.NewRequest(http.MethodGet, "/api/v1/audits?tenant=t2", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleAudits(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var evs []*proto.AuditEvent
	if err := json.Unmarshal(rec.Body.Bytes(), &evs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, e := range evs {
		if e.TenantID != "t1" {
			t.Fatalf("tenant isolation broken: got event tenant=%q, want t1 only", e.TenantID)
		}
	}
}

// TestHandleAudits_TimeWindow 验证 from 时间窗过滤：from 设为未来应返回空。
// 注意：时间用 UTC（RFC3339 以 Z 结尾）格式化，避免本地时区的 '+' 在 URL query 中被解析为空格。
func TestHandleAudits_TimeWindow(t *testing.T) {
	s := newExtraTestServer()
	s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})

	future := time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/audits?from="+future, nil)
	rec := httptest.NewRecorder()
	s.handleAudits(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var evs []*proto.AuditEvent
	if err := json.Unmarshal(rec.Body.Bytes(), &evs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(evs) != 0 {
		t.Fatalf("future from should yield 0, got %d", len(evs))
	}
}

// TestHandleAudits_Limit 验证 limit 参数限制返回条数。
func TestHandleAudits_Limit(t *testing.T) {
	s := newExtraTestServer()
	for i := 0; i < 5; i++ {
		s.store.Audit(&proto.AuditEvent{TenantID: "t1", Action: "create_task", Target: "x"})
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/audits?limit=2", nil)
	rec := httptest.NewRecorder()
	s.handleAudits(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var evs []*proto.AuditEvent
	if err := json.Unmarshal(rec.Body.Bytes(), &evs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(evs) > 2 {
		t.Fatalf("limit=2 should yield <=2, got %d", len(evs))
	}
}

// TestHandleAudits_MethodNotAllowed 验证非 GET 方法返回 405。
func TestHandleAudits_MethodNotAllowed(t *testing.T) {
	s := newExtraTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/audits", nil)
	rec := httptest.NewRecorder()
	s.handleAudits(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}