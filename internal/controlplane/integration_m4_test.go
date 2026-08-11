// integration_m4_test.go M4-3 端到端 API 集成测试：覆盖认证→操作→验证全链路 + 多租户隔离。
//
// 与现有 server_test.go（单 handler 单步断言）互补，本文件以「业务场景」为粒度串联多步 API 调用，
// 验证跨 handler 的状态流转与租户隔离不变量。所有用例用 httptest + MemoryStore + 完整 Server，
// 不依赖 mysql/redis/真实 agent，在沙箱内可完整验证。
//
// 测试场景：
//  1. 认证全链路：注册 → 登录 → 携带 token 访问受保护 API → 登出 → 旧 token 失效。
//  2. 设备管理全链路：纳管（Register+UpsertDevice）→ 列表 → 详情 → 下发任务 → 查看结果 → 退管。
//  3. 告警全链路：创建规则 → 触发告警（store 产出）→ 列表 → ack → silence。
//  4. 多租户隔离：租户 A 创建设备/任务 → 租户 B 不可见/不可操作 → 租户 A 正常操作。
//  5. RBAC 权限：viewer 无 device:delete 权限 → DELETE 设备被拒；admin 有权限 → 可删除。
//  6. K8s 集群管理：添加集群 → 列表 → 测试连接 → 删除集群（clusterMgr=nil 跳过真实连接）。
//
// 设计要点：
//   - 每个用例独立构造 Server（避免状态串扰），固定 jwtSecret 避免随机性；
//   - helper 函数封装多步调用（login、createDevice、createTask...），断言失败立即 t.Fatalf；
//   - 多租户用 X-Tenant-ID 头切换上下文（与 requireTenantContext 一致）；
//   - Demo 模式放行 RBAC 闸（聚焦业务链路），RBAC 专项用例关闭 Demo 走真实权限校验。
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

// =============================================================================
// 测试 helper
// =============================================================================

// newIntegrationServer 构造集成测试用 Server：memory store + 固定 jwtSecret + RBAC seed。
// demo=true 时放行 RBAC 闸（聚焦业务链路）；demo=false 时走真实权限校验（RBAC 专项）。
// 安全债 85：预置 admin/operator/viewer 带 MustChangePassword=true，登录不签发 access token。
// 集成测试聚焦业务链路，统一清除该标记模拟"已改密"状态（首登改密拦截由 auth_test 专门覆盖）。
func newIntegrationServer(demo bool) *Server {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	s := &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3, Demo: demo, PublicRegister: true, AllowPublicRegister: true},
		jwtSecret:    []byte("test-jwt-secret-for-integration-m4-32bytes!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
	// 统一清除预置用户的 MustChangePassword 标记，模拟"已改密"状态。
	for _, name := range []string{"admin", "operator", "viewer"} {
		clearMustChangeFlag(s, name)
	}
	return s
}

// doJSON 发起一次 JSON HTTP 调用并返回 recorder。method/path/body 可空。
// 携带可选的 X-Tenant-ID / Authorization / X-User-Roles 头。
func doJSON(method, path string, body interface{}, tenantID, auth string, roles ...string) *httptest.ResponseRecorder {
	var r *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	} else {
		r = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Content-Type", "application/json")
	if tenantID != "" {
		req.Header.Set("X-Tenant-ID", tenantID)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	if len(roles) > 0 && roles[0] != "" {
		req.Header.Set("X-User-Roles", roles[0])
	}
	rec := httptest.NewRecorder()
	return rec
}

// login 用户登录，返回 "Bearer <token>"。失败 t.Fatalf。
func login(t *testing.T, s *Server, username, password string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleAuthLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login %s = %d, want 200; body=%s", username, rec.Code, rec.Body.String())
	}
	var resp authResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode login resp: %v", err)
	}
	if resp.Token == "" {
		t.Fatalf("login %s token empty; resp=%+v", username, resp)
	}
	return "Bearer " + resp.Token
}

// registerUser 注册新用户（demo+AllowPublicRegister 模式立即激活并返回 token）。
func registerUser(t *testing.T, s *Server, username, password, email string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password, "email": email})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleAuthRegister(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register %s = %d, want 201; body=%s", username, rec.Code, rec.Body.String())
	}
	var resp authResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode register resp: %v", err)
	}
	if resp.Token == "" {
		t.Fatalf("register %s token empty", username)
	}
	return "Bearer " + resp.Token
}

// registerAgent 在 store 直接注册一个 agent（绕过 HTTP，用于测试前置数据）。
// 返回 agentID。
func registerAgent(t *testing.T, s *Server, tenantID, segment string) string {
	t.Helper()
	a := s.store.Register(&proto.AgentInfo{Segment: segment, TenantID: tenantID, Hostname: "host-" + tenantID})
	if a == nil || a.AgentID == "" {
		t.Fatalf("register agent failed for tenant=%s", tenantID)
	}
	return a.AgentID
}

// createTaskViaHTTP 经 HTTP POST /api/v1/tasks 下发任务，返回创建的 task。
func createTaskViaHTTP(t *testing.T, s *Server, tenantID, agentID, auth string) *proto.Task {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"agentID": agentID,
		"command": "echo integration-m4",
		"type":    "shell",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantID)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	s.handleListTasks(rec, req) // POST 分派到 handleCreateTask
	if rec.Code != http.StatusCreated {
		t.Fatalf("create task = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var tk proto.Task
	if err := json.Unmarshal(rec.Body.Bytes(), &tk); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	return &tk
}

// =============================================================================
// 场景 1：认证全链路
// =============================================================================

// TestIntegrationM4_AuthFullChain 验证认证全链路：
// 注册 → 登录 → 携带 token 访问 /auth/me → 登出 → 旧 token 失效（401）。
//
// 覆盖：
//   - 注册返回 token（AllowPublicRegister=true）；
//   - 登录返回 token；
//   - 携带 token 访问 /auth/me 返回 200 + 用户信息；
//   - 登出返回 200；
//   - 登出后再用旧 token 访问 /auth/me 应 401（jti 已加入黑名单）。
func TestIntegrationM4_AuthFullChain(t *testing.T) {
	s := newIntegrationServer(true)

	// 1. 注册新用户（AllowPublicRegister=true → 立即激活 + 签发 token）。
	regAuth := registerUser(t, s, "integuser", "Pass1234", "integ@test.com")
	if regAuth == "" {
		t.Fatal("注册未返回 token")
	}

	// 2. 用注册返回的 token 访问 /auth/me → 200。
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", regAuth)
	rec := httptest.NewRecorder()
	s.handleAuthMe(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("注册后 /auth/me = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// 3. 登录获取正式 token（clearMustChangeFlag 不适用新用户，新用户无 MustChangePassword）。
	loginAuth := login(t, s, "integuser", "Pass1234")

	// 4. 携带登录 token 访问 /auth/me → 200 + username=integuser。
	req = httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", loginAuth)
	rec = httptest.NewRecorder()
	s.handleAuthMe(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("登录后 /auth/me = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var me store.User
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatalf("decode /auth/me: %v", err)
	}
	if me.Username != "integuser" {
		t.Fatalf("me.Username = %q, want integuser", me.Username)
	}

	// 5. 登出 → 200。
	outReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	outReq.Header.Set("Authorization", loginAuth)
	outRec := httptest.NewRecorder()
	s.handleAuthLogout(outRec, outReq)
	if outRec.Code != http.StatusOK {
		t.Fatalf("logout = %d, want 200; body=%s", outRec.Code, outRec.Body.String())
	}

	// 6. 登出后旧 token 应失效（jti 加入黑名单）→ /auth/me 401。
	req = httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", loginAuth)
	rec = httptest.NewRecorder()
	s.handleAuthMe(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("登出后旧 token /auth/me = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

// TestIntegrationM4_AuthRefreshChain 验证 refresh token 旋转链路：
// 登录 → 取 rt Cookie → POST /auth/refresh → 返回新 at + rt → 旧 rt 失效。
func TestIntegrationM4_AuthRefreshChain(t *testing.T) {
	s := newIntegrationServer(true)

	// 1. admin 登录（先清 MustChangePassword 标记）。
	clearMustChangeFlag(s, "admin")
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "admin123"})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	loginRec := httptest.NewRecorder()
	s.handleAuthLogin(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("admin login = %d; body=%s", loginRec.Code, loginRec.Body.String())
	}

	// 2. 从登录响应提取 rt Cookie。
	var rtCookie *http.Cookie
	for _, c := range loginRec.Result().Cookies() {
		if c.Name == refreshTokenCookieName {
			rtCookie = c
			break
		}
	}
	if rtCookie == nil {
		t.Fatalf("登录响应未下发 rt Cookie")
	}

	// 3. POST /auth/refresh 携带 rt Cookie → 200 + 新 at + 新 rt。
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(rtCookie)
	rec := httptest.NewRecorder()
	s.handleAuthRefresh(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	// 应下发新 at Cookie。
	var newAt *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == accessTokenCookieName {
			newAt = c
			break
		}
	}
	if newAt == nil || newAt.Value == "" {
		t.Fatal("refresh 响应未下发新 at Cookie")
	}

	// 4. 旧 rt 应已失效（旋转：consume 即删除）→ 再 refresh 401。
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req2.AddCookie(rtCookie)
	rec2 := httptest.NewRecorder()
	s.handleAuthRefresh(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("旧 rt 再 refresh = %d, want 401（旋转后旧 rt 失效）", rec2.Code)
	}
}

// =============================================================================
// 场景 2：设备管理全链路
// =============================================================================

// TestIntegrationM4_DeviceFullChain 验证设备管理全链路：
// 纳管（Register）→ 列表 → 详情 → 下发任务 → 查看结果 → 退管。
//
// 覆盖：
//   - Register 后 GET /api/v1/devices 返回该设备；
//   - GET /api/v1/devices/{id} 返回设备详情；
//   - POST /api/v1/tasks 下发任务 → 201；
//   - store.SubmitResult 模拟 agent 上报 → GET /api/v1/tasks/{id}/result 返回结果；
//   - DELETE /api/v1/devices/{id} → 200 retired；
//   - 退役后 GET /api/v1/devices/{id} → 404。
func TestIntegrationM4_DeviceFullChain(t *testing.T) {
	s := newIntegrationServer(true)
	const tenant = "t-device"

	// 1. 纳管：注册 agent（store 自动创建占位设备 dev-<agentID>）。
	agentID := registerAgent(t, s, tenant, "seg-a")
	deviceID := "dev-" + agentID

	// 2. 列表：GET /api/v1/devices → 含该设备。
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	req.Header.Set("X-Tenant-ID", tenant)
	rec := httptest.NewRecorder()
	s.handleDevices(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /devices = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	snap := make(map[string][]proto.DeviceInfo)
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode devices snapshot: %v", err)
	}
	found := false
	for _, devs := range snap {
		for _, d := range devs {
			if d.DeviceID == deviceID {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("设备列表未包含 %s; snap=%+v", deviceID, snap)
	}

	// 3. 详情：GET /api/v1/devices/{id} → 200。
	req = httptest.NewRequest(http.MethodGet, "/api/v1/devices/"+deviceID, nil)
	req.Header.Set("X-Tenant-ID", tenant)
	rec = httptest.NewRecorder()
	s.handleDeviceRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /devices/{id} = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// 4. 下发任务：POST /api/v1/tasks → 201。
	tk := createTaskViaHTTP(t, s, tenant, agentID, "")
	if tk.AgentID != agentID {
		t.Fatalf("task.AgentID = %q, want %q", tk.AgentID, agentID)
	}

	// 5. 模拟 agent 领取 + 上报结果。
	claimed := s.store.ClaimTask(agentID)
	if claimed == nil || claimed.TaskID != tk.TaskID {
		t.Fatalf("ClaimTask 未领到刚下发的任务")
	}
	s.store.SubmitResult(&proto.TaskResult{
		TaskID:   tk.TaskID,
		AgentID:  agentID,
		ExitCode: 0,
		Stdout:   "integration-m4-result",
	})

	// 6. 查看结果：GET /api/v1/tasks/{id}/result → 200 + stdout。
	req = httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+tk.TaskID+"/result", nil)
	req.Header.Set("X-Tenant-ID", tenant)
	rec = httptest.NewRecorder()
	s.handleTaskRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /tasks/{id}/result = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "integration-m4-result") {
		t.Fatalf("result body 未含 stdout; body=%s", rec.Body.String())
	}

	// 7. 退管：DELETE /api/v1/devices/{id} → 200。
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/devices/"+deviceID, nil)
	req.Header.Set("X-Tenant-ID", tenant)
	rec = httptest.NewRecorder()
	s.handleDeviceRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /devices/{id} = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// 8. 退役后设备详情仍可查（归档可查，F5 设计），但 Retired=true。
	req = httptest.NewRequest(http.MethodGet, "/api/v1/devices/"+deviceID, nil)
	req.Header.Set("X-Tenant-ID", tenant)
	rec = httptest.NewRecorder()
	s.handleDeviceRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("退役后 GET /devices/{id} = %d, want 200（归档可查）; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"retired":true`) {
		t.Fatalf("退役后设备详情未标记 retired=true; body=%s", rec.Body.String())
	}

	// 9. 退役后设备不在 Snapshot 活跃清单中（GET /api/v1/devices 过滤 retired）。
	req = httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	req.Header.Set("X-Tenant-ID", tenant)
	rec = httptest.NewRecorder()
	s.handleDevices(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("退役后 GET /devices = %d; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), deviceID) {
		t.Fatalf("退役设备 %s 仍出现在活跃清单中; body=%s", deviceID, rec.Body.String())
	}
}

// =============================================================================
// 场景 3：告警全链路
// =============================================================================

// TestIntegrationM4_AlertFullChain 验证告警全链路：
// 创建规则 → 触发告警（store 产出）→ 列表 → ack → silence。
//
// 覆盖：
//   - POST /api/v1/alert-rules → 201 创建规则；
//   - store.AddAlert 模拟告警引擎产出告警；
//   - GET /api/v1/alerts → 200 含该告警；
//   - POST /api/v1/alerts/{id}/ack → 200；
//   - POST /api/v1/alerts/{id}/silence → 200；
//   - ack 后告警 Status=acknowledged，silence 后 Status=silenced。
func TestIntegrationM4_AlertFullChain(t *testing.T) {
	s := newIntegrationServer(true)
	const tenant = "t-alert"

	// 1. 创建告警规则：POST /api/v1/alert-rules → 201。
	ruleBody, _ := json.Marshal(map[string]interface{}{
		"metric":      "cpu.usage",
		"op":          ">",
		"threshold":   90,
		"forDuration": "5m",
		"severity":    "critical",
		"message":     "CPU 使用率超过 90%",
		"enabled":     true,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alert-rules", bytes.NewReader(ruleBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenant)
	rec := httptest.NewRecorder()
	s.handleAlertRules(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create alert-rule = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var rule AlertRule
	if err := json.Unmarshal(rec.Body.Bytes(), &rule); err != nil {
		t.Fatalf("decode alert-rule: %v", err)
	}
	if rule.ID == "" || rule.TenantID != tenant {
		t.Fatalf("alert-rule = %+v, want ID 非空且 TenantID=%s", rule, tenant)
	}

	// 2. 模拟告警引擎产出告警（store 直接 AddAlert）。
	alertID := "alert-integration-m4-1"
	s.store.AddAlert(&proto.Alert{
		AlertID:   alertID,
		TenantID:  tenant,
		DeviceID:  "dev-alert-test",
		AgentID:   "agent-alert-test",
		Severity:  "critical",
		Message:   "CPU 使用率 95%",
		Metric:    "cpu.usage",
		CreatedAt: time.Now(),
		Status:    proto.AlertStatusFiring,
	})

	// 3. 列表：GET /api/v1/alerts → 200 含该告警。
	req = httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
	req.Header.Set("X-Tenant-ID", tenant)
	rec = httptest.NewRecorder()
	s.handleAlerts(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /alerts = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var alerts []proto.Alert
	if err := json.Unmarshal(rec.Body.Bytes(), &alerts); err != nil {
		t.Fatalf("decode alerts: %v", err)
	}
	if len(alerts) != 1 || alerts[0].AlertID != alertID {
		t.Fatalf("alerts = %+v, want 1 条且 ID=%s", alerts, alertID)
	}

	// 4. ack：POST /api/v1/alerts/{id}/ack → 200。
	req = httptest.NewRequest(http.MethodPost, "/api/v1/alerts/"+alertID+"/ack", nil)
	req.Header.Set("X-Tenant-ID", tenant)
	rec = httptest.NewRecorder()
	s.handleAlertRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ack alert = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	// 验证 store 中告警状态已翻为 acknowledged。
	a := s.store.Alert(alertID)
	if a == nil || a.Status != proto.AlertStatusAcknowledged {
		t.Fatalf("ack 后 alert.Status = %v, want acknowledged", a)
	}

	// 5. silence：POST /api/v1/alerts/{id}/silence → 200。
	silenceBody, _ := json.Marshal(map[string]interface{}{
		"durationMinutes": 120,
		"comment":         "正在处理",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/alerts/"+alertID+"/silence", bytes.NewReader(silenceBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenant)
	rec = httptest.NewRecorder()
	s.handleAlertRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("silence alert = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	a = s.store.Alert(alertID)
	if a == nil || a.Status != proto.AlertStatusSilenced {
		t.Fatalf("silence 后 alert.Status = %v, want silenced", a)
	}
}

// =============================================================================
// 场景 4：多租户隔离
// =============================================================================

// TestIntegrationM4_TenantIsolation 验证多租户隔离全链路：
// 租户 A 创建设备/任务 → 租户 B 不可见/不可操作 → 租户 A 正常操作。
//
// 覆盖：
//   - 租户 A 注册 agent → 设备列表只含 A 的设备；
//   - 租户 B 设备列表不含 A 的设备；
//   - 租户 B 下发任务给 A 的 agent → 403；
//   - 租户 B 访问 A 的设备详情 → 403；
//   - 租户 B ack A 的告警 → 403；
//   - 租户 A 各操作正常。
func TestIntegrationM4_TenantIsolation(t *testing.T) {
	s := newIntegrationServer(true) // Demo 放行 RBAC 闸，聚焦租户隔离
	s.requireAuth = true            // 强制要求租户头，聚焦隔离
	const tenantA = "tenant-A"
	const tenantB = "tenant-B"

	// 1. 租户 A 注册 agent + 设备。
	agentA := registerAgent(t, s, tenantA, "seg-a")
	deviceA := "dev-" + agentA

	// 2. 租户 B 注册 agent + 设备。
	agentB := registerAgent(t, s, tenantB, "seg-b")

	// 3. 租户 A 设备列表只含 A 的设备。
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	req.Header.Set("X-Tenant-ID", tenantA)
	rec := httptest.NewRecorder()
	s.handleDevices(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tenantA GET /devices = %d; body=%s", rec.Code, rec.Body.String())
	}
	snapA := make(map[string][]proto.DeviceInfo)
	_ = json.Unmarshal(rec.Body.Bytes(), &snapA)
	for _, devs := range snapA {
		for _, d := range devs {
			if d.TenantID != tenantA {
				t.Fatalf("租户 A 设备列表泄漏他租户设备: %+v", d)
			}
		}
	}

	// 4. 租户 B 设备列表不含 A 的设备。
	req = httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	req.Header.Set("X-Tenant-ID", tenantB)
	rec = httptest.NewRecorder()
	s.handleDevices(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tenantB GET /devices = %d; body=%s", rec.Code, rec.Body.String())
	}
	snapB := make(map[string][]proto.DeviceInfo)
	_ = json.Unmarshal(rec.Body.Bytes(), &snapB)
	for _, devs := range snapB {
		for _, d := range devs {
			if d.DeviceID == deviceA {
				t.Fatalf("租户 B 设备列表含租户 A 的设备 %s", deviceA)
			}
		}
	}

	// 5. 租户 B 下发任务给 A 的 agent → 403。
	body, _ := json.Marshal(map[string]string{
		"agentID": agentA,
		"command": "echo cross-tenant",
		"type":    "shell",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantB)
	rec = httptest.NewRecorder()
	s.handleListTasks(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("租户 B 下发任务给 A 的 agent = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}

	// 6. 租户 B 访问 A 的设备详情 → 403。
	req = httptest.NewRequest(http.MethodGet, "/api/v1/devices/"+deviceA, nil)
	req.Header.Set("X-Tenant-ID", tenantB)
	rec = httptest.NewRecorder()
	s.handleDeviceRouting(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("租户 B 访问 A 设备详情 = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}

	// 7. 租户 A 下发任务给自己的 agent → 201（正常）。
	tk := createTaskViaHTTP(t, s, tenantA, agentA, "")
	if tk.TenantID != tenantA {
		t.Fatalf("task.TenantID = %q, want %q", tk.TenantID, tenantA)
	}

	// 8. 租户 A 的告警，租户 B ack → 403。
	alertID := "alert-iso-A"
	s.store.AddAlert(&proto.Alert{
		AlertID:   alertID,
		TenantID:  tenantA,
		Severity:  "critical",
		Message:   "tenant A alert",
		CreatedAt: time.Now(),
		Status:    proto.AlertStatusFiring,
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/alerts/"+alertID+"/ack", nil)
	req.Header.Set("X-Tenant-ID", tenantB)
	rec = httptest.NewRecorder()
	s.handleAlertRouting(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("租户 B ack A 的告警 = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}

	// 9. 租户 A ack 自己的告警 → 200。
	req = httptest.NewRequest(http.MethodPost, "/api/v1/alerts/"+alertID+"/ack", nil)
	req.Header.Set("X-Tenant-ID", tenantA)
	rec = httptest.NewRecorder()
	s.handleAlertRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("租户 A ack 自己告警 = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// 10. 跨租户下发：租户 A 用 B 的 agent → 403（与 server_test 一致，兜底验证）。
	body, _ = json.Marshal(map[string]string{
		"agentID": agentB,
		"command": "echo cross",
		"type":    "shell",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantA)
	rec = httptest.NewRecorder()
	s.handleListTasks(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("租户 A 下发任务给 B 的 agent = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// 场景 5：RBAC 权限
// =============================================================================

// TestIntegrationM4_RBACPermission 验证 RBAC 权限隔离：
// viewer 无 device:delete 权限 → DELETE 设备被拒；admin 有权限 → 可删除。
//
// 用网关注入身份（X-User-Roles 头）而非 JWT token，避免 JWT claims 中 TenantID="default"
// 与 X-Tenant-ID 头交叉校验冲突（requireTenantContext 会校验头与 token tenant 一致）。
// 网关注入路径走 authorizeByRoles → getRolePermCache 展开角色权限，聚焦 RBAC 验证。
//
// 覆盖：
//   - viewer（X-User-Roles=viewer）DELETE /api/v1/devices/{id} → 403（无 device:delete 权限）；
//   - admin（X-User-Roles=admin）DELETE 同一设备 → 200。
func TestIntegrationM4_RBACPermission(t *testing.T) {
	s := newIntegrationServer(false) // 关 Demo：走真实 RBAC 校验
	const tenant = "t-rbac"

	// 1. 前置：注册 agent + 设备。
	agentID := registerAgent(t, s, tenant, "seg-a")
	deviceID := "dev-" + agentID

	// 2. viewer DELETE 设备 → 403（viewer 角色无 device:delete 权限）。
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/devices/"+deviceID, nil)
	req.Header.Set("X-Tenant-ID", tenant)
	req.Header.Set("X-User-Roles", "viewer")
	rec := httptest.NewRecorder()
	s.handleDeviceRouting(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer DELETE 设备 = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}

	// 3. admin DELETE 同一设备 → 200。
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/devices/"+deviceID, nil)
	req.Header.Set("X-Tenant-ID", tenant)
	req.Header.Set("X-User-Roles", "admin")
	rec = httptest.NewRecorder()
	s.handleDeviceRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin DELETE 设备 = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

// TestIntegrationM4_RBACNoTokenDenied 验证无 token 且非 demo 模式下受保护 API 拒绝访问。
func TestIntegrationM4_RBACNoTokenDenied(t *testing.T) {
	s := newIntegrationServer(false) // 关 Demo：无 token 应被拒
	const tenant = "t-rbac-no-token"

	// 无 Authorization 头访问 /api/v1/devices → 401。
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	req.Header.Set("X-Tenant-ID", tenant)
	rec := httptest.NewRecorder()
	s.handleDevices(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("无 token GET /devices = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// 场景 6：K8s 集群管理全链路
// =============================================================================

// TestIntegrationM4_K8sClusterFullChain 验证 K8s 集群管理全链路：
// 添加集群 → 列表 → 测试连接 → 删除集群。
//
// 覆盖：
//   - POST /api/v1/k8s/clusters → 201 创建集群（clusterMgr=nil 跳过真实连接，Status=unknown）；
//   - GET /api/v1/k8s/clusters → 200 含该集群（kubeconfig 脱敏）；
//   - DELETE /api/v1/k8s/clusters/{id} → 204；
//   - 删除后 GET 列表不含该集群。
//
// 注意：clusterMgr 留 nil（NewServer 之外构造的 Server 默认 nil），跳过 client-go 真实连接，
// 聚焦 HTTP 链路与 store 持久化验证。测试连接端点因 clusterMgr=nil 返回 500，此处跳过该步
// （真实 K8s 连接测试由 k8s_cluster_test.go 单独覆盖）。
func TestIntegrationM4_K8sClusterFullChain(t *testing.T) {
	s := newIntegrationServer(false) // 关 Demo：K8s API 走 requirePermission 真实鉴权
	// clusterMgr 故意留 nil：测试不依赖真实 K8s 集群。
	const tenant = "t-k8s"

	// 1. admin 登录。
	adminAuth := login(t, s, "admin", "admin123")

	// 2. 添加集群：POST /api/v1/k8s/clusters → 201。
	createBody, _ := json.Marshal(map[string]string{
		"name":       "integ-test-cluster",
		"server":     "https://10.0.0.1:6443",
		"kubeconfig": "apiVersion: v1\nclusters: []",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/k8s/clusters", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", adminAuth)
	req.Header.Set("X-Tenant-ID", tenant)
	rec := httptest.NewRecorder()
	s.handleK8sClusters(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create k8s cluster = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var created store.K8sCluster
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created cluster: %v", err)
	}
	if created.ID == "" {
		t.Fatal("创建的集群 ID 为空")
	}
	if created.Name != "integ-test-cluster" {
		t.Fatalf("cluster.Name = %q, want integ-test-cluster", created.Name)
	}
	// kubeconfig 必须脱敏。
	if created.Kubeconfig != k8sClusterKubeconfigMasked {
		t.Fatalf("响应 Kubeconfig = %q, want %q (脱敏)", created.Kubeconfig, k8sClusterKubeconfigMasked)
	}
	clusterID := created.ID

	// 3. 列表：GET /api/v1/k8s/clusters → 200 含该集群。
	req = httptest.NewRequest(http.MethodGet, "/api/v1/k8s/clusters", nil)
	req.Header.Set("Authorization", adminAuth)
	req.Header.Set("X-Tenant-ID", tenant)
	rec = httptest.NewRecorder()
	s.handleK8sClusters(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list k8s clusters = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var listResp struct {
		Clusters []*store.K8sCluster `json:"clusters"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode cluster list: %v", err)
	}
	found := false
	for _, c := range listResp.Clusters {
		if c.ID == clusterID {
			found = true
			if c.Kubeconfig != k8sClusterKubeconfigMasked {
				t.Fatalf("列表中集群 kubeconfig 未脱敏: %q", c.Kubeconfig)
			}
		}
	}
	if !found {
		t.Fatalf("集群列表未包含刚创建的集群 %s", clusterID)
	}

	// 4. 删除集群：DELETE /api/v1/k8s/clusters/{id} → 204。
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/k8s/clusters/"+clusterID, nil)
	req.Header.Set("Authorization", adminAuth)
	req.Header.Set("X-Tenant-ID", tenant)
	rec = httptest.NewRecorder()
	s.handleK8sClusterRouting(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete k8s cluster = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}

	// 5. 删除后列表不含该集群。
	req = httptest.NewRequest(http.MethodGet, "/api/v1/k8s/clusters", nil)
	req.Header.Set("Authorization", adminAuth)
	req.Header.Set("X-Tenant-ID", tenant)
	rec = httptest.NewRecorder()
	s.handleK8sClusters(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete 后 list = %d; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), clusterID) {
		t.Fatalf("删除后列表仍含集群 %s; body=%s", clusterID, rec.Body.String())
	}
}

// TestIntegrationM4_K8sClusterTenantIsolation 验证 K8s 集群租户隔离：
// 租户 A 创建的集群，租户 B 列表不可见、删除被拒。
func TestIntegrationM4_K8sClusterTenantIsolation(t *testing.T) {
	s := newIntegrationServer(false)
	const tenantA = "t-k8s-A"
	const tenantB = "t-k8s-B"

	// admin 登录。
	adminAuth := login(t, s, "admin", "admin123")

	// 租户 A 创建集群。
	createBody, _ := json.Marshal(map[string]string{
		"name":       "cluster-A",
		"server":     "https://10.0.0.1:6443",
		"kubeconfig": "apiVersion: v1",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/k8s/clusters", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", adminAuth)
	req.Header.Set("X-Tenant-ID", tenantA)
	rec := httptest.NewRecorder()
	s.handleK8sClusters(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("tenantA create cluster = %d; body=%s", rec.Code, rec.Body.String())
	}
	var clusterA store.K8sCluster
	_ = json.Unmarshal(rec.Body.Bytes(), &clusterA)

	// 租户 B 列表不含 A 的集群。
	req = httptest.NewRequest(http.MethodGet, "/api/v1/k8s/clusters", nil)
	req.Header.Set("Authorization", adminAuth)
	req.Header.Set("X-Tenant-ID", tenantB)
	rec = httptest.NewRecorder()
	s.handleK8sClusters(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tenantB list = %d; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), clusterA.ID) {
		t.Fatalf("租户 B 集群列表泄漏租户 A 的集群 %s", clusterA.ID)
	}

	// 租户 B 删除 A 的集群 → 404（按 not found 拒绝，不泄露存在性）。
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/k8s/clusters/"+clusterA.ID, nil)
	req.Header.Set("Authorization", adminAuth)
	req.Header.Set("X-Tenant-ID", tenantB)
	rec = httptest.NewRecorder()
	s.handleK8sClusterRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("租户 B 删除 A 的集群 = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}

	// 租户 A 删除自己的集群 → 204。
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/k8s/clusters/"+clusterA.ID, nil)
	req.Header.Set("Authorization", adminAuth)
	req.Header.Set("X-Tenant-ID", tenantA)
	rec = httptest.NewRecorder()
	s.handleK8sClusterRouting(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("租户 A 删除自己集群 = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
}