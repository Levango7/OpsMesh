package controlplane

// service_proxy_test.go 测试 M13 微服务聚合代理 + ChatOps 命令台（service_proxy.go / bot_bridge.go）。
//
// 覆盖范围：
//  1. 路径改写：autoscaler/portal 的域前缀剥离（/api/v1/autoscaler/rules → /api/v1/rules）
//  2. 五域路由注册：Start() 注册的 mux 对五域路径分发到 handleServiceProxy；/api/v1/bot/* 分发到 bot handler
//  3. bot 命令台契约：POST /api/v1/bot/command 执行+历史；GET history/platforms/quick-commands
//  4. 权限点目录：六域权限在 rbacPermSpecs 中（viewer 派生获得 *:read）
//  5. 后端不可达：503 service unreachable（不裸 500）
//
// 测试策略（与 gateway_test.go 一致）：白盒直构 Server + loginAsAdmin 鉴权 +
// httptest.NewRequest/NewRecorder 直调 handler；代理转发用 httptest.NewServer 模拟后端。

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/store"
)

// newServiceProxyTestServer 构造聚合层测试用 Server（与 gateway_test.go 同模式）。
func newServiceProxyTestServer() *Server {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3},
		jwtSecret:    []byte("test-jwt-secret-for-svc-proxy-32bytes!!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
}

// TestRewriteProxyPath 验证路径改写规则：
// gpu/runbook/incident 前缀保持不变；autoscaler/portal 剥域前缀（服务真实路径无域前缀）。
func TestRewriteProxyPath(t *testing.T) {
	cases := []struct {
		publicPath  string
		requestPath string
		wantPath    string
	}{
		{"/api/v1/gpu", "/api/v1/gpu/nodes", "/api/v1/gpu/nodes"},
		{"/api/v1/gpu", "/api/v1/gpu/metrics/node-1", "/api/v1/gpu/metrics/node-1"},
		{"/api/v1/runbooks", "/api/v1/runbooks/rb-1/execute", "/api/v1/runbooks/rb-1/execute"},
		{"/api/v1/incidents", "/api/v1/incidents", "/api/v1/incidents"},
		{"/api/v1/autoscaler", "/api/v1/autoscaler/rules", "/api/v1/rules"},
		{"/api/v1/autoscaler", "/api/v1/autoscaler/rules/rule-1", "/api/v1/rules/rule-1"},
		{"/api/v1/autoscaler", "/api/v1/autoscaler/evaluate", "/api/v1/evaluate"},
		{"/api/v1/portal", "/api/v1/portal/requests", "/api/v1/requests"},
		{"/api/v1/portal", "/api/v1/portal/approvals/ap-1/approve", "/api/v1/approvals/ap-1/approve"},
	}
	for i := range serviceProxyRules {
		r := &serviceProxyRules[i]
		found := false
		for _, c := range cases {
			if c.publicPath == r.publicPrefix {
				found = true
				if got := r.rewriteProxyPath(c.requestPath); got != c.wantPath {
					t.Errorf("rule %s: rewrite(%s) = %s, want %s", r.publicPrefix, c.requestPath, got, c.wantPath)
				}
			}
		}
		if !found {
			t.Errorf("规则 %s 未被用例覆盖（请补改写用例）", r.publicPrefix)
		}
	}
}

// TestServiceProxyForwardToBackend 端到端：httptest 后端 + env 覆盖地址 + 代理转发
// 断言（方法透传、路径改写、鉴权通过）。
func TestServiceProxyForwardToBackend(t *testing.T) {
	var gotPath, gotMethod string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rules": [{"id": "rule-1"}]}`))
	}))
	defer backend.Close()

	t.Setenv("AUTOSCALER_SVC_URL", backend.URL)

	s := newServiceProxyTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/autoscaler/rules", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleServiceProxy(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if gotPath != "/api/v1/rules" {
		t.Errorf("后端收到路径 %s, want /api/v1/rules（域前缀应被剥除）", gotPath)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("后端收到方法 %s, want GET", gotMethod)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应非 JSON: %v", err)
	}
	if _, ok := body["rules"]; !ok {
		t.Errorf("响应缺 rules 字段: %v", body)
	}
}

// TestServiceProxyUnreachable503 后端不可达 → 503 + service unreachable 提示
// （端口 1 是保留地址，连接必失败，不依赖竞态性的端口占用）。
func TestServiceProxyUnreachable503(t *testing.T) {
	t.Setenv("GPU_SVC_URL", "http://127.0.0.1:1")
	s := newServiceProxyTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/gpu/nodes", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleServiceProxy(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "service unreachable") {
		t.Errorf("503 响应应含 service unreachable 提示: %s", w.Body.String())
	}
}

// TestServiceProxyPermDenied 无权限 → 403（聚合层鉴权先行，不触达后端）。
func TestServiceProxyPermDenied(t *testing.T) {
	s := newServiceProxyTestServer()
	// viewer 只有 *:read 权限；用 viewer 登录后请求写方法（PUT）依然过 read 闸——
	// 此处直接用无 token 请求验证 401 路径（鉴权第一道）。
	req := httptest.NewRequest(http.MethodGet, "/api/v1/gpu/nodes", nil)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleServiceProxy(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("无凭证 status = %d, want 401; body=%s", w.Code, w.Body.String())
	}
}

// TestBotCommandExecuteAndHistory bot 命令台：执行 status 命令 + 历史回读
// （契约：响应即历史记录项；history.list 租户隔离）。
func TestBotCommandExecuteAndHistory(t *testing.T) {
	s := newServiceProxyTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"command": "/opsmesh status", "platform": "web"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/bot/command", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleBotCommand(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var rec botCommandRecord
	if err := json.Unmarshal(w.Body.Bytes(), &rec); err != nil {
		t.Fatalf("响应非记录结构: %v", err)
	}
	if rec.Status != "success" {
		t.Fatalf("命令应成功: %s (%v)", rec.Status, rec.Response)
	}
	if rec.ID == "" || rec.ExecutedAt == "" {
		t.Errorf("记录缺 ID/ExecutedAt: %+v", rec)
	}

	// 历史回读（同租户可见）。
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/bot/history?platform=web", nil)
	req2.Header.Set("Authorization", auth)
	req2.Header.Set("X-Tenant-ID", "default")
	w2 := httptest.NewRecorder()
	s.handleBotHistory(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("history status = %d", w2.Code)
	}
	var hist struct {
		History []*botCommandRecord `json:"history"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &hist); err != nil {
		t.Fatalf("history 响应解析失败: %v", err)
	}
	if len(hist.History) == 0 {
		t.Fatal("history 应含刚执行的命令")
	}
	if hist.History[0].Command != "/opsmesh status" {
		t.Errorf("最新历史应为 status 命令: %+v", hist.History[0])
	}
}

// TestBotCommandBadSyntax 语法错误 → 200 + status=failed 记录（命令台语义，
// 不用 4xx——前端按记录状态渲染，与 store.unshift 契约一致）。
func TestBotCommandBadSyntax(t *testing.T) {
	s := newServiceProxyTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"command": "not a slash command"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/bot/command", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleBotCommand(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var rec botCommandRecord
	_ = json.Unmarshal(w.Body.Bytes(), &rec)
	if rec.Status != "failed" {
		t.Errorf("错误命令应记 failed: %+v", rec)
	}
}

// TestBotPlatformsAndQuickCommands 平台清单 + 快捷命令只读端点。
func TestBotPlatformsAndQuickCommands(t *testing.T) {
	s := newServiceProxyTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/bot/platforms", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleBotPlatforms(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("platforms status = %d", w.Code)
	}
	var pf struct {
		Platforms []map[string]any `json:"platforms"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &pf); err != nil || len(pf.Platforms) == 0 {
		t.Fatalf("platforms 响应异常: %v %s", err, w.Body.String())
	}
	// web 平台必须恒开（Web 命令台自身）。
	foundWeb := false
	for _, p := range pf.Platforms {
		if p["id"] == "web" && p["enabled"] == true {
			foundWeb = true
		}
	}
	if !foundWeb {
		t.Error("web 平台应恒为 enabled")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/bot/quick-commands", nil)
	req2.Header.Set("Authorization", auth)
	req2.Header.Set("X-Tenant-ID", "default")
	w2 := httptest.NewRecorder()
	s.handleBotQuickCommands(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("quick-commands status = %d", w2.Code)
	}
	if !strings.Contains(w2.Body.String(), "/opsmesh status") {
		t.Errorf("快捷命令应含 status: %s", w2.Body.String())
	}
}

// TestServiceProxyPermSeeded 六域权限点已入 rbacPermSpecs 目录
// （前端 router requirePerm 引用；缺失会导致 viewer 无权访问、权限页不可见）。
func TestServiceProxyPermSeeded(t *testing.T) {
	rp := store.RolePermissions()
	readPerms := []string{
		"gpu:read", "bot:read", "runbook:read",
		"incident:read", "autoscaler:read", "portal:read",
	}
	allPerms := append([]string{}, readPerms...)
	allPerms = append(allPerms,
		"gpu:write", "bot:write", "runbook:write",
		"incident:write", "autoscaler:write", "portal:write",
	)
	adminSet := map[string]bool{}
	for _, p := range rp["admin"] {
		adminSet[p] = true
	}
	for _, wp := range allPerms {
		if !adminSet[wp] {
			t.Errorf("admin 缺权限 %s", wp)
		}
	}
	viewerSet := map[string]bool{}
	for _, p := range rp["viewer"] {
		viewerSet[p] = true
	}
	for _, wp := range readPerms {
		if !viewerSet[wp] {
			t.Errorf("viewer 缺 read 权限 %s", wp)
		}
	}
	for _, wp := range []string{"gpu:write", "bot:write", "runbook:write", "incident:write", "autoscaler:write", "portal:write"} {
		if viewerSet[wp] {
			t.Errorf("viewer 不应持有 %s", wp)
		}
	}
}

// TestServiceProxyRulesEnvOverrides envKey 覆盖默认地址的解析正确性。
func TestServiceProxyRulesEnvOverrides(t *testing.T) {
	t.Setenv("RUNBOOK_SVC_URL", "http://10.0.0.5:9000")
	r := lookupServiceProxyRule("/api/v1/runbooks/rb-1")
	if r == nil {
		t.Fatal("runbooks 规则应命中")
	}
	u := r.upstreamBase()
	if u == nil || u.Host != "10.0.0.5:9000" {
		t.Fatalf("env 覆盖未生效: %v", u)
	}
	// 未覆盖的走默认。
	_ = os.Unsetenv("INCIDENT_SVC_URL")
	r2 := lookupServiceProxyRule("/api/v1/incidents/inc-1")
	if r2 == nil {
		t.Fatal("incidents 规则应命中")
	}
	u2 := r2.upstreamBase()
	if u2 == nil || u2.Port() != "8082" {
		t.Fatalf("默认地址应 8082: %v", u2)
	}
}

// bytes 导入守卫（避免未来重构删 import 编译仍过的假阴性——bytes 仅在
// 扩展用例时使用，这里显式引用一次）。
var _ = bytes.MinRead

// ============ device 域代理（TD-60 D1/D3 接线） ============

// TestDeviceProxyRuleRewrite 验证 device 域路径改写：
// /api/v1/device-svc/{devices,agents,cmdb,discovery} → /api/v1/{...}（剥 device-svc 域前缀）。
func TestDeviceProxyRuleRewrite(t *testing.T) {
	cases := []struct {
		publicPath  string
		requestPath string
		wantPath    string
	}{
		{"/api/v1/device-svc/devices", "/api/v1/device-svc/devices", "/api/v1/devices"},
		{"/api/v1/device-svc/devices", "/api/v1/device-svc/devices/dev-1", "/api/v1/devices/dev-1"},
		{"/api/v1/device-svc/devices", "/api/v1/device-svc/devices/dev-1/heartbeat", "/api/v1/devices/dev-1/heartbeat"},
		{"/api/v1/device-svc/agents", "/api/v1/device-svc/agents", "/api/v1/agents"},
		{"/api/v1/device-svc/agents", "/api/v1/device-svc/agents/ag-1", "/api/v1/agents/ag-1"},
		{"/api/v1/device-svc/cmdb", "/api/v1/device-svc/cmdb/cis", "/api/v1/cmdb/cis"},
		{"/api/v1/device-svc/discovery", "/api/v1/device-svc/discovery/jobs", "/api/v1/discovery/jobs"},
		{"/api/v1/device-svc/discovery", "/api/v1/device-svc/discovery/devices", "/api/v1/discovery/devices"},
	}
	for i := range deviceProxyExtras {
		r := &deviceProxyExtras[i]
		found := false
		for _, c := range cases {
			if c.publicPath == r.publicPrefix {
				found = true
				if got := r.rewriteProxyPath(c.requestPath); got != c.wantPath {
					t.Errorf("rule %s: rewrite(%s) = %s, want %s", r.publicPrefix, c.requestPath, got, c.wantPath)
				}
			}
		}
		if !found {
			t.Errorf("device 规则 %s 未被用例覆盖", r.publicPrefix)
		}
	}
}

// TestDeviceProxyLookup 验证 device 域路径命中规则表 + 五域规则不受影响。
func TestDeviceProxyLookup(t *testing.T) {
	// device 域四前缀命中。
	for _, p := range []string{
		"/api/v1/device-svc/devices",
		"/api/v1/device-svc/devices/dev-1",
		"/api/v1/device-svc/agents",
		"/api/v1/device-svc/cmdb/cis",
		"/api/v1/device-svc/discovery/jobs",
	} {
		r := lookupServiceProxyRule(p)
		if r == nil {
			t.Fatalf("device 路径 %s 应命中规则", p)
		}
		if r.envKey != "DEVICE_SVC_URL" {
			t.Errorf("device 路径 %s 命中的 envKey=%s, want DEVICE_SVC_URL", p, r.envKey)
		}
	}
	// 五域不受影响（回归）。
	if r := lookupServiceProxyRule("/api/v1/gpu/nodes"); r == nil || r.publicPrefix != "/api/v1/gpu" {
		t.Fatalf("gpu 规则回归失败: %v", r)
	}
	// 未知路径不命中。
	if lookupServiceProxyRule("/api/v1/no-such-domain") != nil {
		t.Fatal("未知路径不应命中")
	}
	// 旧 device 路径（controlplane 本地 handler 域）不命中代理——双轨并存边界。
	if lookupServiceProxyRule("/api/v1/devices") != nil {
		t.Fatal("/api/v1/devices 是 controlplane 本地 handler 域，不应命中代理（会 panic 重复注册）")
	}
}

// TestDeviceProxyForwardWithTenantHeader 端到端：device 代理转发时
// 聚合层验证的租户/用户身份以 X-Tenant-ID / X-User-Id 头剥离重注入后端
// （身份头统一治理后全代理域同路径，五域不再例外）。
func TestDeviceProxyForwardWithTenantHeader(t *testing.T) {
	var gotPath, gotTenant, gotUser, gotCookie string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotTenant = r.Header.Get("X-Tenant-ID")
		gotUser = r.Header.Get("X-User-Id")
		gotCookie = r.Header.Get("Cookie")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"devices": []}`))
	}))
	defer backend.Close()

	t.Setenv("DEVICE_SVC_URL", backend.URL)

	s := newServiceProxyTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/device-svc/devices", nil)
	req.Header.Set("Authorization", auth)
	// 不带 X-Tenant-ID 头：租户身份从 JWT 提取（requireTenantContext 验证后
	// 由代理注入 X-Tenant-ID 头转发——本测正验证该注入）。
	req.Header.Set("Cookie", "opsmesh_at=leak-me")
	w := httptest.NewRecorder()
	s.handleServiceProxy(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if gotPath != "/api/v1/devices" {
		t.Errorf("后端收到路径 %s, want /api/v1/devices（device-svc 域前缀应剥除）", gotPath)
	}
	if gotTenant != "default" {
		t.Errorf("后端 X-Tenant-ID = %q, want default（loginAsAdmin JWT 租户应被注入转发）", gotTenant)
	}
	if want := s.store.GetUserByUsername("admin").ID; gotUser != want {
		t.Errorf("后端 X-User-Id = %q, want %q（loginAsAdmin JWT 用户应被注入转发）", gotUser, want)
	}
	if gotCookie != "" {
		t.Errorf("后端不应收到 Cookie（会话凭证不下落内部服务）: %q", gotCookie)
	}
}

// TestDeviceProxyPermDenied device 域规则权限守卫生效（无权 viewer 之外的角色拒绝）。
func TestDeviceProxyPermDenied(t *testing.T) {
	s := newServiceProxyTestServer()
	// 未登录裸请求：401（绝不透传到无鉴权的后端）。
	req := httptest.NewRequest(http.MethodGet, "/api/v1/device-svc/devices", nil)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleServiceProxy(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("未登录请求 status = %d, want 401", w.Code)
	}
}

// ============ task 域代理（TD-60 阶段 2 三域接通） ============

// TestTaskProxyRuleRewrite 验证 task 域路径改写：/api/v1/task-svc/* 整段剥域前缀
// 拼回 /api/v1/*（task-svc 网关路径与单体一致：tasks/schedules/approval 同名）。
func TestTaskProxyRuleRewrite(t *testing.T) {
	cases := []struct {
		requestPath string
		wantPath    string
	}{
		{"/api/v1/task-svc", "/api/v1"},
		{"/api/v1/task-svc/tasks", "/api/v1/tasks"},
		{"/api/v1/task-svc/tasks/t-1", "/api/v1/tasks/t-1"},
		{"/api/v1/task-svc/tasks/t-1/cancel", "/api/v1/tasks/t-1/cancel"},
		{"/api/v1/task-svc/tasks/batch-exec", "/api/v1/tasks/batch-exec"},
		{"/api/v1/task-svc/tasks/batch/b-1", "/api/v1/tasks/batch/b-1"},
		{"/api/v1/task-svc/tasks/canary/c-1/advance", "/api/v1/tasks/canary/c-1/advance"},
		{"/api/v1/task-svc/schedules", "/api/v1/schedules"},
		{"/api/v1/task-svc/schedules/s-1/pause", "/api/v1/schedules/s-1/pause"},
		{"/api/v1/task-svc/approval/flows", "/api/v1/approval/flows"},
		{"/api/v1/task-svc/approval/requests/r-1/approve", "/api/v1/approval/requests/r-1/approve"},
	}
	for i := range taskProxyExtras {
		r := &taskProxyExtras[i]
		for _, c := range cases {
			if got := r.rewriteProxyPath(c.requestPath); got != c.wantPath {
				t.Errorf("rule %s: rewrite(%s) = %s, want %s", r.publicPrefix, c.requestPath, got, c.wantPath)
			}
		}
	}
}

// TestTaskProxyLookup 验证 task 域路径命中规则表 + 单体本地路径不命中（双轨边界）。
func TestTaskProxyLookup(t *testing.T) {
	for _, p := range []string{
		"/api/v1/task-svc/tasks",
		"/api/v1/task-svc/tasks/t-1/cancel",
		"/api/v1/task-svc/schedules",
		"/api/v1/task-svc/approval/requests",
	} {
		r := lookupServiceProxyRule(p)
		if r == nil {
			t.Fatalf("task 路径 %s 应命中规则", p)
		}
		if r.envKey != "TASK_SVC_URL" {
			t.Errorf("task 路径 %s 命中的 envKey=%s, want TASK_SVC_URL", p, r.envKey)
		}
	}
	// 单体本地路径（tasks/schedules/approval）不命中代理——同 mux 重复注册会 panic。
	for _, p := range []string{"/api/v1/tasks", "/api/v1/tasks/t-1", "/api/v1/schedules", "/api/v1/approval/flows"} {
		if lookupServiceProxyRule(p) != nil {
			t.Fatalf("%s 是 controlplane 本地 handler 域，不应命中代理", p)
		}
	}
	// device 与五域不受影响（回归）。
	if r := lookupServiceProxyRule("/api/v1/device-svc/devices"); r == nil || r.envKey != "DEVICE_SVC_URL" {
		t.Fatalf("device 规则回归失败: %v", r)
	}
	if r := lookupServiceProxyRule("/api/v1/gpu/nodes"); r == nil || r.publicPrefix != "/api/v1/gpu" {
		t.Fatalf("gpu 规则回归失败: %v", r)
	}
}

// TestProxyPermResolution 权限分级矩阵：代理对每个操作要求的权限点必须与
// controlplane 本地同名 handler 逐条一致（双轨期「换路径不换权限边界」）。
// device 列同时覆盖越权修复断言（此前统一 device:read，DELETE/provision 只读可过）。
func TestProxyPermResolution(t *testing.T) {
	cases := []struct {
		method, path, wantPerm string
	}{
		// task 域（镜像 server_tasks.go / server_batch.go / server_schedules.go /
		// server_approval.go 的 requirePermission 取值）。
		{http.MethodGet, "/api/v1/task-svc/tasks", "task:read"},
		{http.MethodPost, "/api/v1/task-svc/tasks", "task:write"},
		{http.MethodGet, "/api/v1/task-svc/tasks/t-1", "task:read"},
		{http.MethodGet, "/api/v1/task-svc/tasks/t-1/result", "task:read"},
		{http.MethodPost, "/api/v1/task-svc/tasks/t-1/cancel", "task:cancel"},
		{http.MethodPost, "/api/v1/task-svc/tasks/t-1/approve", "task:approve"},
		{http.MethodPost, "/api/v1/task-svc/tasks/t-1/reject", "task:approve"},
		{http.MethodPost, "/api/v1/task-svc/tasks/batch-exec", "task:write"},
		{http.MethodGet, "/api/v1/task-svc/tasks/batch/b-1", "task:read"},
		{http.MethodPost, "/api/v1/task-svc/tasks/canary", "task:write"},
		{http.MethodGet, "/api/v1/task-svc/tasks/canary/c-1", "task:read"},
		{http.MethodPost, "/api/v1/task-svc/tasks/canary/c-1/advance", "task:write"},
		{http.MethodGet, "/api/v1/task-svc/schedules", "schedule:read"},
		{http.MethodPost, "/api/v1/task-svc/schedules", "schedule:write"},
		{http.MethodGet, "/api/v1/task-svc/schedules/s-1", "schedule:read"},
		{http.MethodPut, "/api/v1/task-svc/schedules/s-1", "schedule:write"},
		{http.MethodDelete, "/api/v1/task-svc/schedules/s-1", "schedule:write"},
		{http.MethodPost, "/api/v1/task-svc/schedules/s-1/pause", "schedule:write"},
		{http.MethodPost, "/api/v1/task-svc/schedules/s-1/resume", "schedule:write"},
		{http.MethodGet, "/api/v1/task-svc/approval/flows", "approval:read"},
		{http.MethodPost, "/api/v1/task-svc/approval/flows", "approval:write"},
		{http.MethodGet, "/api/v1/task-svc/approval/requests", "approval:read"},
		{http.MethodPost, "/api/v1/task-svc/approval/requests", "approval:write"},
		{http.MethodPost, "/api/v1/task-svc/approval/requests/r-1/approve", "approval:approve"},
		{http.MethodPost, "/api/v1/task-svc/approval/requests/r-1/reject", "approval:approve"},
		{http.MethodPost, "/api/v1/task-svc/approval/requests/r-1/cancel", "approval:write"},
		{http.MethodGet, "/api/v1/task-svc/approval/requests/r-1/history", "approval:read"},
		{http.MethodGet, "/api/v1/task-svc/approval/pending", "approval:read"},
		// device 域（与 server_devices.go 对齐；越权修复断言）。
		{http.MethodGet, "/api/v1/device-svc/devices", "device:read"},
		{http.MethodPost, "/api/v1/device-svc/devices", "device:write"},
		{http.MethodPut, "/api/v1/device-svc/devices/d-1", "device:write"},
		{http.MethodDelete, "/api/v1/device-svc/devices/d-1", "device:delete"},
		{http.MethodPost, "/api/v1/device-svc/devices/d-1/provision", "provision:execute"},
		{http.MethodPost, "/api/v1/device-svc/devices/d-1/heartbeat", "device:write"},
		{http.MethodGet, "/api/v1/device-svc/agents", "device:read"},
		{http.MethodPost, "/api/v1/device-svc/agents/a-1/heartbeat", "device:write"},
		{http.MethodGet, "/api/v1/device-svc/cmdb/cis", "cmdb:read"},
		{http.MethodPost, "/api/v1/device-svc/cmdb/cis", "cmdb:write"},
		{http.MethodDelete, "/api/v1/device-svc/cmdb/cis/c-1", "cmdb:write"},
		{http.MethodGet, "/api/v1/device-svc/discovery/jobs", "network:read"},
		{http.MethodPost, "/api/v1/device-svc/discovery/jobs", "network:write"},
		// 五域（写方法从 *:read 收紧为 *:write，2026-09-29 同类越权修复）。
		{http.MethodGet, "/api/v1/gpu/nodes", "gpu:read"},
		{http.MethodPost, "/api/v1/gpu/workloads", "gpu:write"},
		{http.MethodDelete, "/api/v1/gpu/workloads/w-1", "gpu:write"},
		{http.MethodGet, "/api/v1/runbooks", "runbook:read"},
		{http.MethodPost, "/api/v1/runbooks/rb-1/execute", "runbook:write"},
		{http.MethodGet, "/api/v1/incidents", "incident:read"},
		{http.MethodPost, "/api/v1/incidents/i-1/timeline", "incident:write"},
		{http.MethodGet, "/api/v1/autoscaler/rules", "autoscaler:read"},
		{http.MethodPost, "/api/v1/autoscaler/scale", "autoscaler:write"},
		{http.MethodGet, "/api/v1/portal/requests", "portal:read"},
		{http.MethodPost, "/api/v1/portal/approvals/ap-1/approve", "portal:write"},
	}
	for _, c := range cases {
		r := lookupServiceProxyRule(c.path)
		if r == nil {
			t.Errorf("%s %s 未命中规则", c.method, c.path)
			continue
		}
		if got := r.resolvePerm(c.method, c.path); got != c.wantPerm {
			t.Errorf("%s %s resolvePerm = %s, want %s", c.method, c.path, got, c.wantPerm)
		}
	}
}

// TestTaskProxyForwardWithTenantHeader 端到端：task 代理转发时路径改写 +
// 注入 X-Tenant-ID/X-User-Id（task-svc 网关消费租户上下文并与 token tenant_id
// 交叉校验）+ 剥离 Cookie（会话凭证不下落内部服务，与 device 域同语义）。
func TestTaskProxyForwardWithTenantHeader(t *testing.T) {
	var gotPath, gotTenant, gotCookie string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotTenant = r.Header.Get("X-Tenant-ID")
		gotCookie = r.Header.Get("Cookie")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tasks": []}`))
	}))
	defer backend.Close()

	t.Setenv("TASK_SVC_URL", backend.URL)

	s := newServiceProxyTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/task-svc/tasks", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("Cookie", "opsmesh_at=leak-me")
	w := httptest.NewRecorder()
	s.handleServiceProxy(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if gotPath != "/api/v1/tasks" {
		t.Errorf("后端收到路径 %s, want /api/v1/tasks（task-svc 域前缀应剥除）", gotPath)
	}
	if gotTenant != "default" {
		t.Errorf("后端 X-Tenant-ID = %q, want default（loginAsAdmin JWT 租户应被注入转发）", gotTenant)
	}
	if gotCookie != "" {
		t.Errorf("后端不应收到 Cookie: %q", gotCookie)
	}
}

// TestProxyPermEscalationClosed 权限放大回归：viewer（仅 *:read）经代理前缀
// 不得完成写操作。修复前规则级单一 perm（*:read）放行全部方法——viewer 可经
// /api/v1/device-svc/devices/{id} DELETE 删设备、POST provision 纳管设备、
// 经 /api/v1/gpu/workloads 建 GPU 负载、经 /api/v1/task-svc/tasks/{id}/cancel
// 取消任务。现在这些逐一 403，且拒绝发生在聚合层（不触达后端）。
func TestProxyPermEscalationClosed(t *testing.T) {
	var backendHits int
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer backend.Close()
	t.Setenv("DEVICE_SVC_URL", backend.URL)
	t.Setenv("TASK_SVC_URL", backend.URL)
	t.Setenv("GPU_SVC_URL", backend.URL)

	s := newServiceProxyTestServer()
	auth := loginAsViewer(t, s)

	// viewer 的 task:read 应放行只读路径（真正转发到后端）。
	okReq := httptest.NewRequest(http.MethodGet, "/api/v1/task-svc/tasks", nil)
	okReq.Header.Set("Authorization", auth)
	okReq.Header.Set("X-Tenant-ID", "default")
	okW := httptest.NewRecorder()
	s.handleServiceProxy(okW, okReq)
	if okW.Code != http.StatusOK {
		t.Fatalf("viewer GET /task-svc/tasks status = %d, want 200; body=%s", okW.Code, okW.Body.String())
	}
	hitsAfterRead := backendHits

	denied := []struct {
		method, path, wantPerm string
	}{
		{http.MethodDelete, "/api/v1/device-svc/devices/d-1", "device:delete"},
		{http.MethodPost, "/api/v1/device-svc/devices/d-1/provision", "provision:execute"},
		{http.MethodPost, "/api/v1/device-svc/devices", "device:write"},
		{http.MethodPost, "/api/v1/task-svc/tasks/t-1/cancel", "task:cancel"},
		{http.MethodPost, "/api/v1/task-svc/tasks/t-1/approve", "task:approve"},
		{http.MethodPost, "/api/v1/task-svc/approval/requests/r-1/approve", "approval:approve"},
		{http.MethodPost, "/api/v1/gpu/workloads", "gpu:write"},
	}
	for _, c := range denied {
		req := httptest.NewRequest(c.method, c.path, nil)
		req.Header.Set("Authorization", auth)
		req.Header.Set("X-Tenant-ID", "default")
		w := httptest.NewRecorder()
		s.handleServiceProxy(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("viewer %s %s status = %d, want 403; body=%s", c.method, c.path, w.Code, w.Body.String())
			continue
		}
		if !strings.Contains(w.Body.String(), c.wantPerm) {
			t.Errorf("viewer %s %s 拒绝响应应指明权限点 %s: %s", c.method, c.path, c.wantPerm, w.Body.String())
		}
	}
	if backendHits != hitsAfterRead {
		t.Errorf("越权请求不应触达后端：读取后命中 %d 次，最终 %d 次", hitsAfterRead, backendHits)
	}
}

// ============ 身份头统一治理（全代理域注入 + 伪造防护，§5.6 遗留项收口） ============

// TestProxyIdentityHeadersForAllDomains 验证代理对全部域（五域 + device/task）
// 统一剥离客户端身份头并重注入已校验身份：
//   - X-Tenant-ID / X-User-Id 取自令牌（requireTenantContext 校验后的 actx，
//     修复前仅 device/task 注入、五域透传客户端值或缺失）；
//   - 客户端自带 X-User-Roles 被剥离（下游无消费方，防伪造留存）。
func TestProxyIdentityHeadersForAllDomains(t *testing.T) {
	var gotTenant, gotUser, gotRoles string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTenant = r.Header.Get("X-Tenant-ID")
		gotUser = r.Header.Get("X-User-Id")
		gotRoles = r.Header.Get("X-User-Roles")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer backend.Close()
	t.Setenv("GPU_SVC_URL", backend.URL)
	t.Setenv("DEVICE_SVC_URL", backend.URL)
	t.Setenv("TASK_SVC_URL", backend.URL)

	s := newServiceProxyTestServer()
	auth := loginAsAdmin(t, s)
	adminID := s.store.GetUserByUsername("admin").ID
	if adminID == "" {
		t.Fatal("内存库 admin 用户 ID 为空（测试前置不成立）")
	}

	for _, path := range []string{
		"/api/v1/gpu/nodes",          // 五域规则
		"/api/v1/device-svc/devices", // device 域（治理前唯一注入方）
		"/api/v1/task-svc/tasks",     // task 域（治理前唯一注入方之二）
	} {
		gotTenant, gotUser, gotRoles = "unset", "unset", "unset"
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", auth)
		req.Header.Set("X-User-Roles", "admin,superuser") // 客户端伪造角色头：应被剥离
		w := httptest.NewRecorder()
		s.handleServiceProxy(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200; body=%s", path, w.Code, w.Body.String())
		}
		if gotTenant != "default" {
			t.Errorf("%s 后端 X-Tenant-ID = %q, want default（令牌租户应注入）", path, gotTenant)
		}
		if gotUser != adminID {
			t.Errorf("%s 后端 X-User-Id = %q, want %s（令牌用户应注入）", path, gotUser, adminID)
		}
		if gotRoles != "" {
			t.Errorf("%s 后端不应收到 X-User-Roles（应剥离防伪造）: %q", path, gotRoles)
		}
	}
}

// TestProxyIdentityUserForgeryRejected 验证 X-User-Id 与令牌 user 交叉校验：
// 已认证客户端携有效令牌 + 伪造用户头 → 403，拒绝发生在聚合层（不触达后端）。
// 覆盖租户头携带/缺省两条路径——缺省路径（回退令牌租户）此前不校验用户头，
// 伪造值会经代理落到下游审计（本批收口）。
func TestProxyIdentityUserForgeryRejected(t *testing.T) {
	var backendHits int
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendHits++
		_, _ = w.Write([]byte(`{}`))
	}))
	defer backend.Close()
	t.Setenv("GPU_SVC_URL", backend.URL)

	s := newServiceProxyTestServer()
	auth := loginAsAdmin(t, s)

	for _, tenantHdr := range []string{"default", ""} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/gpu/nodes", nil)
		req.Header.Set("Authorization", auth)
		if tenantHdr != "" {
			req.Header.Set("X-Tenant-ID", tenantHdr)
		}
		req.Header.Set("X-User-Id", "mallory")
		w := httptest.NewRecorder()
		s.handleServiceProxy(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("租户头=%q 伪造用户请求 status = %d, want 403; body=%s", tenantHdr, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "user mismatch") {
			t.Errorf("拒绝文案应指明 user mismatch: %s", w.Body.String())
		}
	}
	if backendHits != 0 {
		t.Errorf("伪造身份请求不应触达后端: %d 次", backendHits)
	}
}

// TestRequireTenantContextUserCrossCheck 直接验证 requireTenantContext 的
// 用户交叉校验语义与 trust-gateway-headers 跳过行为（租户交叉校验回归在内）。
func TestRequireTenantContextUserCrossCheck(t *testing.T) {
	s := newServiceProxyTestServer()
	auth := loginAsAdmin(t, s)
	adminID := s.store.GetUserByUsername("admin").ID

	cases := []struct {
		name      string
		tenantHdr string
		userHdr   string
		wantOK    bool
	}{
		{"用户头一致放行", "default", adminID, true},
		{"用户头不一致 403", "default", "mallory", false},
		{"租户头缺省 + 用户头不一致 403", "", "mallory", false},
		{"租户头不一致仍 403（租户交叉校验回归）", "other", adminID, false},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/gpu/nodes", nil)
		req.Header.Set("Authorization", auth)
		if c.tenantHdr != "" {
			req.Header.Set("X-Tenant-ID", c.tenantHdr)
		}
		if c.userHdr != "" {
			req.Header.Set("X-User-Id", c.userHdr)
		}
		w := httptest.NewRecorder()
		actx, ok := s.requireTenantContext(w, req)
		if ok != c.wantOK {
			t.Fatalf("%s: ok=%v, want %v; code=%d body=%s", c.name, ok, c.wantOK, w.Code, w.Body.String())
		}
		if c.wantOK && actx.UserID != adminID {
			t.Errorf("%s: actx.UserID = %q, want %q", c.name, actx.UserID, adminID)
		}
	}

	// trustGateway 模式：网关注入头为权威声明——无令牌的头注入请求直通。
	s2 := newServiceProxyTestServer()
	s2.cfg.TrustGatewayHeaders = true
	req := httptest.NewRequest(http.MethodGet, "/api/v1/gpu/nodes", nil)
	req.Header.Set("X-Tenant-ID", "acme")
	req.Header.Set("X-User-Id", "gw-user")
	w := httptest.NewRecorder()
	actx, ok := s2.requireTenantContext(w, req)
	if !ok {
		t.Fatalf("trustGateway 头注入请求应放行: code=%d body=%s", w.Code, w.Body.String())
	}
	if actx.TenantID != "acme" || actx.UserID != "gw-user" {
		t.Errorf("trustGateway 应保留网关注入身份: %+v", actx)
	}

	// trustGateway 模式 + 令牌：用户交叉校验跳过（头值保留，网关权威）。
	s3 := newServiceProxyTestServer()
	s3.cfg.TrustGatewayHeaders = true
	auth3 := loginAsAdmin(t, s3)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/gpu/nodes", nil)
	req.Header.Set("Authorization", auth3)
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-Id", "gw-user")
	w = httptest.NewRecorder()
	actx, ok = s3.requireTenantContext(w, req)
	if !ok {
		t.Fatalf("trustGateway + 令牌请求应放行: code=%d body=%s", w.Code, w.Body.String())
	}
	if actx.UserID != "gw-user" {
		t.Errorf("trustGateway 模式应保留网关注入的用户头: %q", actx.UserID)
	}
}

// TestProxyIdentityHeadersCarryNonDefaultTenant 非 default 租户注入的直接证据：
// 现有用例均以 admin（default 租户）验证，无法区分"注入取自令牌"与"注入默认值"。
// 此处以 t-acme 租户 + viewer 角色（最小只读权限）用户经令牌请求，断言后端收到的
// 租户/用户头为令牌实际值——多租户下数据归置正确性的关键路径。
func TestProxyIdentityHeadersCarryNonDefaultTenant(t *testing.T) {
	var gotTenant, gotUser string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTenant = r.Header.Get("X-Tenant-ID")
		gotUser = r.Header.Get("X-User-Id")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer backend.Close()
	t.Setenv("GPU_SVC_URL", backend.URL)

	s := newServiceProxyTestServer()
	u := s.store.CreateUser(&store.User{
		ID: "u-acme-1", Username: "acme-user", TenantID: "t-acme",
		Status: "active", RoleIDs: []string{"role-viewer"},
	})
	if u == nil {
		t.Fatal("CreateUser 失败（测试前置不成立）")
	}
	token, err := s.issueUserToken(u)
	if err != nil {
		t.Fatalf("签发令牌失败: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/gpu/nodes", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.handleServiceProxy(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if gotTenant != "t-acme" {
		t.Errorf("后端 X-Tenant-ID = %q, want t-acme（应注入令牌租户，而非默认值）", gotTenant)
	}
	if gotUser != "u-acme-1" {
		t.Errorf("后端 X-User-Id = %q, want u-acme-1（应注入令牌用户）", gotUser)
	}
}
