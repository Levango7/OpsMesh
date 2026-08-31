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

// TestHandleCreateTask_TenantIsolation 验证 下发入口的租户归属校验与审计产出。
func TestHandleCreateTask_TenantIsolation(t *testing.T) {
	st := store.NewMemoryStore()
	// 注册两个不同租户的 agent
	a1 := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	a2 := st.Register(&proto.AgentInfo{Segment: "seg-b", TenantID: "t2"})
	s := &Server{store: st, requireAuth: false, cfg: &config.Config{TaskMaxRetries: 3, Demo: true}} // Demo 放行 RBAC，聚焦租户隔离验证

	post := func(tenant, agentID, cmd string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{
			"agentID":  agentID,
			"command":  cmd,
			"tenantID": tenant,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", bytes.NewReader(body))
		req.Header.Set("X-Tenant-ID", tenant)
		req.Header.Set("X-User-Id", "u1")
		rec := httptest.NewRecorder()
		s.handleCreateTask(rec, req)
		return rec
	}

	// 同租户下发：成功 201
	rec := post("t1", a1.AgentID, "echo ok")
	if rec.Code != http.StatusCreated {
		t.Fatalf("same-tenant create = %d, want 201", rec.Code)
	}

	// 跨租户下发（用 t2 的 agent 但带 t1 头）：应被拒 403
	rec = post("t1", a2.AgentID, "echo nope")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant create = %d, want 403", rec.Code)
	}

	// 缺 agentID：400
	rec = post("t1", "", "echo x")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty agentID = %d, want 400", rec.Code)
	}

	// 审计事件应已产出（register x2 + create_task x1）
	if len(st.Audits()) < 3 {
		t.Fatalf("audit events = %d, want >=3", len(st.Audits()))
	}
}

// TestHandleDashboard_ServesEmbedded 验证 前端独立化：HTML 从 Go 字符串抽离为
// embed.FS 静态资源（web/index.html + web/assets/app.css|main.js 等模块）。
// GET / 返回 200 + text/html 且引用拆分后的资源；/assets/app.css 与 /assets/main.js
// 各按扩展名设 Content-Type，且 main.js 作为 ES module 入口含 import 语句。
func TestHandleDashboard_ServesEmbedded(t *testing.T) {
	st := store.NewMemoryStore()
	s := &Server{store: st, requireAuth: false, cfg: &config.Config{TaskMaxRetries: 3, Demo: true}}

	// --- GET / ：HTML 外壳 ---
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	s.handleDashboard(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("content-type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	for _, marker := range []string{"OpsMesh", "活跃告警（M7）"} {
		if !strings.Contains(body, marker) {
			t.Fatalf("dashboard HTML missing marker %q", marker)
		}
	}
	if !strings.Contains(body, `<link rel="stylesheet" href="/assets/app.css">`) {
		t.Fatalf("dashboard HTML missing /assets/app.css link")
	}
	if !strings.Contains(body, `<script type="module" src="/assets/main.js"></script>`) {
		t.Fatalf("dashboard HTML missing /assets/main.js script tag")
	}

	// --- GET /assets/app.css ：样式表 ---
	creq := httptest.NewRequest(http.MethodGet, "/assets/app.css", nil)
	crec := httptest.NewRecorder()
	s.handleAsset(crec, creq)
	if crec.Code != http.StatusOK {
		t.Fatalf("app.css = %d, want 200", crec.Code)
	}
	if ct := crec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Fatalf("app.css content-type = %q, want text/css", ct)
	}

	// --- GET /assets/main.js ：JS bundle 含原内联时的 marker ---
	jreq := httptest.NewRequest(http.MethodGet, "/assets/main.js", nil)
	jrec := httptest.NewRecorder()
	s.handleAsset(jrec, jreq)
	if jrec.Code != http.StatusOK {
		t.Fatalf("main.js = %d, want 200", jrec.Code)
	}
	if ct := jrec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/javascript") {
		t.Fatalf("main.js content-type = %q, want application/javascript", ct)
	}
	js := jrec.Body.String()
	if !strings.Contains(js, "import") {
		t.Fatalf("main.js missing import statement")
	}

	// --- 路径穿越防护：../ 必须 404（不回退宿主文件系统）---
	breq := httptest.NewRequest(http.MethodGet, "/assets/../controlplane/server.go", nil)
	brec := httptest.NewRecorder()
	s.handleAsset(brec, breq)
	if brec.Code != http.StatusNotFound {
		t.Fatalf("path traversal = %d, want 404", brec.Code)
	}
}

// TestHandleAlerts_DeadLetter M7 告警闭环：失败任务达到重试上限进入死信，
// 内核产出 critical 告警，且 GET /api/v1/alerts 可查询、前端告警面板有数据源。
func TestHandleAlerts_DeadLetter(t *testing.T) {
	st := store.NewMemoryStore()
	s := &Server{store: st, requireAuth: false, cfg: &config.Config{TaskMaxRetries: 0, Demo: true}}

	// 注册 agent + 创建一条 MaxRetries=0 的任务（一次失败即死信）
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	tk := st.CreateTask(&proto.Task{
		AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "false",
		MaxRetries: 0, Status: "running",
	})

	// 上报失败结果（exitCode=1，已达上限 -> 死信 + critical 告警）
	st.SubmitResult(&proto.TaskResult{TaskID: tk.TaskID, AgentID: a.AgentID, ExitCode: 1})

	alerts := st.Alerts("")
	if len(alerts) != 1 {
		t.Fatalf("alerts = %d, want 1 (dead-letter critical)", len(alerts))
	}
	if alerts[0].Severity != "critical" || alerts[0].AgentID != a.AgentID {
		t.Fatalf("alert = %+v, want severity=critical & agent match", alerts[0])
	}

	// HTTP 端点可查询
	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleAlerts(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/alerts = %d, want 200", rec.Code)
	}
	var got []proto.Alert
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode alerts: %v", err)
	}
	if len(got) != 1 || got[0].Severity != "critical" {
		t.Fatalf("HTTP alerts = %+v, want 1 critical", got)
	}
}

// TestHandleDashboard_RequireAuth 验证 requireAuth=true 且无网关注入身份时返回 401。
func TestHandleDashboard_RequireAuth(t *testing.T) {
	st := store.NewMemoryStore()
	s := &Server{store: st, requireAuth: true, cfg: &config.Config{TaskMaxRetries: 3}}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	s.handleDashboard(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("dashboard auth = %d, want 401", rec.Code)
	}
}

// TestHandleProvision_ReturnsToken ：handleProvision 返回 installToken 与 bootstrap 命令。
func TestHandleProvision_ReturnsToken(t *testing.T) {
	st := store.NewMemoryStore().WithSecret("opsmesh-test-secret")
	s := &Server{store: st, requireAuth: false, cfg: &config.Config{TaskMaxRetries: 3, Demo: true}}

	// 先创建一个候选手动注册的设备
	st.Register(&proto.AgentInfo{AgentID: "a1", Hostname: "h1", Segment: "seg-a", Addr: "10.0.0.6", TenantID: "t1"})
	// 通过 UpsertDevice 创建一个 discovered 候选（Managed=false）以测试 provision 返回 token
	st.UpsertDevice(&proto.DeviceInfo{
		DeviceID: "dev-provision-test", Segment: "seg-a", TenantID: "t1",
		IP: "10.0.0.7", State: "discovered", Managed: false,
	})

	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/dev-provision-test/provision", body)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	// 因为 handleProvision 通过路由调用，直接调 handleDeviceRouting
	s.handleDeviceRouting(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("handleProvision = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if resp["status"] != "provisioning" {
		t.Fatalf("status = %q, want provisioning", resp["status"])
	}
	if resp["installToken"] == "" {
		t.Fatal("installToken empty")
	}
	if resp["bootstrap"] == "" {
		t.Fatal("bootstrap empty")
	}
	if !strings.Contains(resp["bootstrap"], "--token=") {
		t.Fatalf("bootstrap missing --token=: %s", resp["bootstrap"])
	}
	// 验证设备状态变为 provisioning
	dev := st.Device("dev-provision-test")
	if dev == nil || dev.State != "provisioning" {
		t.Fatalf("device state = %q, want provisioning", dev.State)
	}
}

// TestHandleDeviceMetrics_GET 测试 GET /api/v1/devices/{id}/metrics 端点。
// 覆盖：正常返回、设备不存在 404、无指标 404、租户隔离。
func TestHandleDeviceMetrics_GET(t *testing.T) {
	st := store.NewMemoryStore()
	// 注册一台 agent，控制面创建占位设备 dev-<agentID>
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1", Hostname: "web-01", OS: "linux", Arch: "amd64"})
	deviceID := "dev-" + a.AgentID
	// 注入监控指标
	st.StoreDeviceMetrics(deviceID, &proto.DeviceMetrics{
		DeviceID: deviceID,
		CPU:      proto.CPUMetrics{Cores: 4, Usage: 50.0},
		Memory:   proto.MemMetrics{Total: 8192, Used: 4096},
	})

	s := &Server{store: st, requireAuth: false, cfg: &config.Config{Demo: true}}

	// 正常返回
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices/"+deviceID+"/metrics", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleDeviceRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET metrics = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got proto.DeviceMetrics
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if got.CPU.Cores != 4 || got.CPU.Usage != 50.0 {
		t.Fatalf("CPU = %+v, want cores=4 usage=50", got.CPU)
	}

	// 设备不存在 -> 404
	req = httptest.NewRequest(http.MethodGet, "/api/v1/devices/dev-nonexistent/metrics", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec = httptest.NewRecorder()
	s.handleDeviceRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("nonexistent device = %d, want 404", rec.Code)
	}

	// 设备存在但无指标 -> 404
	st.UpsertDevice(&proto.DeviceInfo{DeviceID: "dev-no-metrics", Segment: "seg-a", TenantID: "t1", State: "online"})
	req = httptest.NewRequest(http.MethodGet, "/api/v1/devices/dev-no-metrics/metrics", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec = httptest.NewRecorder()
	s.handleDeviceRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("device without metrics = %d, want 404", rec.Code)
	}
}

// TestHandleDeviceMetrics_TenantIsolation 租户隔离：他租户设备指标不可访问。
// TrustGatewayHeaders=true：本用例只带 X-Tenant-ID 头无凭证（网关注入场景，IAM 路径 B）——
// requireAuth 下裸头默认 401（越权修复），显式信任网关才直通（隔离语义不变）。
func TestHandleDeviceMetrics_TenantIsolation(t *testing.T) {
	st := store.NewMemoryStore()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	deviceID := "dev-" + a.AgentID
	st.StoreDeviceMetrics(deviceID, &proto.DeviceMetrics{DeviceID: deviceID, Hostname: "h1"})

	s := &Server{store: st, requireAuth: true, cfg: &config.Config{Demo: true, TrustGatewayHeaders: true}} // Demo 放行 RBAC；信任网关注入头，聚焦租户隔离验证

	// t2 用户访问 t1 设备 -> 403
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices/"+deviceID+"/metrics", nil)
	req.Header.Set("X-Tenant-ID", "t2")
	rec := httptest.NewRecorder()
	s.handleDeviceRouting(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant access = %d, want 403", rec.Code)
	}

	// t1 用户访问自己设备 -> 200
	req = httptest.NewRequest(http.MethodGet, "/api/v1/devices/"+deviceID+"/metrics", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec = httptest.NewRecorder()
	s.handleDeviceRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("same-tenant access = %d, want 200", rec.Code)
	}
}

// TestHandleDeviceMetrics_RangeQuery 测试 GET /api/v1/devices/{id}/metrics?range=2h 返回历史时序。
// 覆盖：合法 range 返回 MetricsSeries、非法 range 400、无历史 404、不带 range 保持现有行为。
func TestHandleDeviceMetrics_RangeQuery(t *testing.T) {
	st := store.NewMemoryStore()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1", Hostname: "web-01"})
	deviceID := "dev-" + a.AgentID
	// 注入 3 条历史指标（最近 10 分钟内，确保 range=15m 能查到）。
	base := time.Now().Add(-10 * time.Minute) // 最早一条在 10 分钟前
	for i := 0; i < 3; i++ {
		st.StoreDeviceMetrics(deviceID, &proto.DeviceMetrics{
			DeviceID:    deviceID,
			CPU:         proto.CPUMetrics{Cores: 4, Usage: float64(10 + i*5)},
			CollectedAt: base.Add(time.Duration(i) * 5 * time.Minute),
		})
	}

	s := &Server{store: st, requireAuth: false, cfg: &config.Config{Demo: true}}

	// 1) ?range=2h 返回 MetricsSeries，含 3 条样本。
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices/"+deviceID+"/metrics?range=2h", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleDeviceRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("range=2h = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var series proto.MetricsSeries
	if err := json.Unmarshal(rec.Body.Bytes(), &series); err != nil {
		t.Fatalf("json decode MetricsSeries: %v", err)
	}
	if series.DeviceID != deviceID {
		t.Fatalf("series.DeviceID = %q, want %q", series.DeviceID, deviceID)
	}
	if series.Range != "2h" {
		t.Fatalf("series.Range = %q, want 2h", series.Range)
	}
	if len(series.Samples) != 3 {
		t.Fatalf("series.Samples = %d 条, want 3", len(series.Samples))
	}
	// 样本应按时间升序。
	for i := 1; i < len(series.Samples); i++ {
		if series.Samples[i].CollectedAt.Before(series.Samples[i-1].CollectedAt) {
			t.Fatalf("samples[%d] 早于 samples[%d]，应升序", i, i-1)
		}
	}

	// 2) ?range=15m 返回部分历史（只有最近 1-2 条在 15 分钟内）。
	req = httptest.NewRequest(http.MethodGet, "/api/v1/devices/"+deviceID+"/metrics?range=15m", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec = httptest.NewRecorder()
	s.handleDeviceRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("range=15m = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	series = proto.MetricsSeries{}
	if err := json.Unmarshal(rec.Body.Bytes(), &series); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if len(series.Samples) == 0 {
		t.Fatal("range=15m 返回 0 条，应至少有 1 条最近指标")
	}

	// 3) 非法 range -> 400。
	req = httptest.NewRequest(http.MethodGet, "/api/v1/devices/"+deviceID+"/metrics?range=999h", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec = httptest.NewRecorder()
	s.handleDeviceRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 range = %d, want 400", rec.Code)
	}

	// 4) 不带 range 保持现有行为，返回最新值（DeviceMetrics 而非 MetricsSeries）。
	req = httptest.NewRequest(http.MethodGet, "/api/v1/devices/"+deviceID+"/metrics", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec = httptest.NewRecorder()
	s.handleDeviceRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("无 range = %d, want 200", rec.Code)
	}
	var dm proto.DeviceMetrics
	if err := json.Unmarshal(rec.Body.Bytes(), &dm); err != nil {
		t.Fatalf("json decode DeviceMetrics: %v", err)
	}
	if dm.DeviceID != deviceID {
		t.Fatalf("DeviceMetrics.DeviceID = %q, want %q", dm.DeviceID, deviceID)
	}
	// 最新一条 Usage 应为 20（10+2*5）。
	if dm.CPU.Usage != 20 {
		t.Fatalf("最新 CPU.Usage = %v, want 20", dm.CPU.Usage)
	}
}

// TestParseMetricsRange 验证 range 参数解析。
func TestParseMetricsRange(t *testing.T) {
	cases := []struct {
		input string
		ok    bool
	}{
		{"15m", true}, {"1h", true}, {"2h", true}, {"6h", true}, {"24h", true},
		{"2H", true},  // 大小写不敏感
		{"", false},   // 空
		{"3h", false}, // 不支持
		{"abc", false},
	}
	for _, c := range cases {
		_, ok := parseMetricsRange(c.input)
		if ok != c.ok {
			t.Errorf("parseMetricsRange(%q) ok = %v, want %v", c.input, ok, c.ok)
		}
	}
	// 合法 range 返回的 since 应在过去。
	since, ok := parseMetricsRange("2h")
	if !ok {
		t.Fatal("parseMetricsRange(2h) ok=false, want true")
	}
	if !since.Before(time.Now()) {
		t.Fatal("since 应在过去")
	}
}
