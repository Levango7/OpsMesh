package k8slifecycle

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/authctx"
)

// 本包多数执行路径都要 SSH 到真实节点；这里只覆盖不需要任何外部连接的契约面：
// 路由注册、方法/入参校验、只读状态查询、纯辅助函数，以及两条纯状态机
// （Rollback / CertRotation 的 execute* 不触达 SSH）。

func newTestHandler() *K8sLifecycleHandler {
	return NewK8sLifecycleHandler()
}

func doRequest(t *testing.T, h http.HandlerFunc, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是 JSON: %v (body=%s)", err, rec.Body.String())
	}
	return out
}

func TestParseK8sVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Client Version: v1.30.0\nServer Version: v1.29.4", "1.30.0"},
		{"v1.28", "1.28"},
		{"kubectl v1.31.2-eks-123", "1.31.2"},
		{"no version here", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := parseK8sVersion(c.in); got != c.want {
			t.Fatalf("parseK8sVersion(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNodePort(t *testing.T) {
	if got := nodePort(NodeSpec{Host: "h"}); got != 22 {
		t.Fatalf("默认端口 = %d, want 22", got)
	}
	if got := nodePort(NodeSpec{Host: "h", Port: 2222}); got != 2222 {
		t.Fatalf("自定义端口 = %d, want 2222", got)
	}
}

func TestHandlerMethodMatrix(t *testing.T) {
	h := newTestHandler()

	postOnly := []struct {
		name string
		fn   http.HandlerFunc
	}{
		{"upgrade", h.HandleUpgrade},
		{"upgrade/plan", h.HandleUpgradePlan},
		{"rollback", h.HandleRollback},
		{"cert-rotate", h.HandleCertRotation},
		{"node-scale", h.HandleNodeScale},
		{"etcd-backup", h.HandleEtcdBackup},
		{"etcd-restore", h.HandleEtcdRestore},
	}
	for _, c := range postOnly {
		rec := doRequest(t, c.fn, http.MethodGet, "/x", "")
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s: GET 返回 %d, want 405", c.name, rec.Code)
		}
		if body := decodeJSON(t, rec); body["error"] != "method not allowed" {
			t.Fatalf("%s: 405 错误体 = %v", c.name, body)
		}
	}

	for _, c := range []struct {
		name string
		fn   http.HandlerFunc
	}{
		{"upgrade/status", h.HandleUpgradeStatus},
		{"etcd-backups", h.HandleEtcdListBackups},
	} {
		rec := doRequest(t, c.fn, http.MethodPost, "/x", "{}")
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s: POST 返回 %d, want 405", c.name, rec.Code)
		}
	}
}

func TestHandlerRejectsInvalidJSON(t *testing.T) {
	h := newTestHandler()

	for _, c := range []struct {
		name string
		fn   http.HandlerFunc
	}{
		{"upgrade", h.HandleUpgrade},
		{"upgrade/plan", h.HandleUpgradePlan},
		{"rollback", h.HandleRollback},
		{"cert-rotate", h.HandleCertRotation},
		{"node-scale", h.HandleNodeScale},
		{"etcd-backup", h.HandleEtcdBackup},
		{"etcd-restore", h.HandleEtcdRestore},
	} {
		rec := doRequest(t, c.fn, http.MethodPost, "/x", "{not-json")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: 非法 JSON 返回 %d, want 400", c.name, rec.Code)
		}
		body := decodeJSON(t, rec)
		msg, _ := body["error"].(string)
		if !strings.HasPrefix(msg, "invalid JSON") {
			t.Fatalf("%s: 400 错误体 = %v", c.name, body)
		}
	}
}

func TestHandlerValidationBeforeAnyDial(t *testing.T) {
	h := newTestHandler()
	h.upgradeMgr = NewUpgradeManager()
	h.nodeScaler = NewNodeScaler()

	// 缺 clusterId/targetVersion：必须在启动执行 goroutine 之前就拒绝。
	rec := doRequest(t, h.HandleUpgrade, http.MethodPost, "/x", `{"clusterId":"c1"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("HandleUpgrade(缺 targetVersion) = %d, want 400", rec.Code)
	}
	if body := decodeJSON(t, rec); body["error"] != "clusterId and targetVersion are required" {
		t.Fatalf("错误体 = %v", body)
	}

	rec = doRequest(t, h.HandleNodeScale, http.MethodPost, "/x", `{"clusterId":"c1","action":"grow","nodes":["n1"]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("HandleNodeScale(非法 action) = %d, want 400", rec.Code)
	}
	if body := decodeJSON(t, rec); body["error"] != "action must be add or remove" {
		t.Fatalf("错误体 = %v", body)
	}
}

func TestUpgradeStatusLifecycle(t *testing.T) {
	h := newTestHandler()

	rec := doRequest(t, h.HandleUpgradeStatus, http.MethodGet, "/x?upgradeID=missing", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("未知 upgradeID = %d, want 404", rec.Code)
	}
	decodeJSON(t, rec)

	// 直接驱动状态机，避免任何 SSH；GetStatus 必须返回副本（缓存不得被调用方改坏）。
	// updateStatus 只更新已存在的作业，故先种一条再推进。
	h.upgradeMgr.mu.Lock()
	h.upgradeMgr.upgrades["c1-upgrade-x"] = &UpgradeStatus{Phase: "Planning", NodeStatus: map[string]string{}}
	h.upgradeMgr.mu.Unlock()
	h.upgradeMgr.updateStatus("c1-upgrade-x", "Upgrading", 42, "升级控制平面", "")
	st, ok := h.upgradeMgr.GetStatus("c1-upgrade-x")
	if !ok {
		t.Fatal("GetStatus 未命中刚写入的作业")
	}
	st.Phase = "Tampered"
	if again, _ := h.upgradeMgr.GetStatus("c1-upgrade-x"); again.Phase != "Upgrading" {
		t.Fatalf("GetStatus 未返回副本：内部状态被改成 %q", again.Phase)
	}

	rec = doRequest(t, h.HandleUpgradeStatus, http.MethodGet, "/x?upgradeID=c1-upgrade-x", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("已知 upgradeID = %d, want 200", rec.Code)
	}
	body := decodeJSON(t, rec)
	if body["phase"] != "Upgrading" || body["progress"] != float64(42) || body["currentStep"] != "升级控制平面" {
		t.Fatalf("状态体 = %v", body)
	}
}

func TestRollbackAndCertRotationAccepted(t *testing.T) {
	h := newTestHandler()

	// 这两条 execute* 只推进内存状态（不触达 SSH），因此可以走成功路径。
	rec := doRequest(t, h.HandleRollback, http.MethodPost, "/x",
		`{"clusterId":"c1","targetVersion":"1.28.0","reason":"regression"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("HandleRollback = %d, want 202（body=%s）", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec)
	if body["phase"] != "RollingBack" || body["toVersion"] != "1.28.0" {
		t.Fatalf("回滚响应 = %v", body)
	}

	rec = doRequest(t, h.HandleCertRotation, http.MethodPost, "/x", `{"clusterId":"c1"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("HandleCertRotation = %d, want 202（body=%s）", rec.Code, rec.Body.String())
	}
	body = decodeJSON(t, rec)
	if body["phase"] != "Rotating" {
		t.Fatalf("证书轮换响应 = %v", body)
	}
}

func TestRouteRegistration(t *testing.T) {
	mux := http.NewServeMux()
	RegisterK8sLifecycleRoutes(mux, NewK8sLifecycleHandler())

	// 路由模式带方法前缀：GET 打到 POST 专属路径必须 405（而非 404）。
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/k8s/lifecycle/upgrade", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /upgrade = %d, want 405", rec.Code)
	}

	// 未知作业走真实路由：404 + JSON 错误体。
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/k8s/lifecycle/upgrade/status?upgradeID=nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /upgrade/status(未知) = %d, want 404", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(rec.Body.Bytes()), &body); err != nil {
		t.Fatalf("404 响应不是 JSON: %v", err)
	}
	if body["error"] != "upgrade not found" {
		t.Fatalf("404 错误体 = %v", body)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/k8s/lifecycle/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("未注册路径 = %d, want 404", rec.Code)
	}
}

// 鉴权接线：GET 走 k8s:read，其余走 k8s:write；通过后回填租户头。
func TestAuthorizeMethodMappingAndTenantBackfill(t *testing.T) {
	h := newTestHandler()
	var gotPerm, gotTenant string
	var ran bool
	h.Authorize = func(w http.ResponseWriter, r *http.Request, perm string) (authctx.Context, bool) {
		gotPerm = perm
		return authctx.Context{TenantID: "t1"}, true
	}
	fn := h.authorize(func(w http.ResponseWriter, r *http.Request) {
		ran = true
		gotTenant = r.Header.Get("X-Tenant-ID")
	}, "k8s:read", "k8s:write")

	doRequest(t, fn, http.MethodGet, "/x", "")
	if gotPerm != "k8s:read" {
		t.Fatalf("GET 权限 = %q, want k8s:read", gotPerm)
	}
	if !ran || gotTenant != "t1" {
		t.Fatalf("GET 未执行或租户未回填: ran=%v tenant=%q", ran, gotTenant)
	}

	gotPerm, ran, gotTenant = "", false, ""
	doRequest(t, fn, http.MethodPost, "/x", "{}")
	if gotPerm != "k8s:write" {
		t.Fatalf("POST 权限 = %q, want k8s:write", gotPerm)
	}
	if !ran {
		t.Fatal("POST 鉴权通过后未执行 handler")
	}

	// 已有租户头不被覆盖。
	gotTenant = ""
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Tenant-ID", "t2")
	fn(httptest.NewRecorder(), req)
	if gotTenant != "t2" {
		t.Fatalf("已有租户头被覆盖为 %q", gotTenant)
	}
}

// 9 条路由必须全部经过鉴权：拒绝时 401 且回调拿到的方法映射权限正确。
func TestLifecycleRoutesRequireAuthorization(t *testing.T) {
	h := newTestHandler()
	var perms []string
	h.Authorize = func(w http.ResponseWriter, r *http.Request, perm string) (authctx.Context, bool) {
		perms = append(perms, perm)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return authctx.Context{}, false
	}
	mux := http.NewServeMux()
	RegisterK8sLifecycleRoutes(mux, h)

	cases := []struct {
		method, path, wantPerm string
	}{
		{http.MethodPost, "/api/v1/k8s/lifecycle/upgrade", "k8s:write"},
		{http.MethodPost, "/api/v1/k8s/lifecycle/upgrade/plan", "k8s:write"},
		{http.MethodGet, "/api/v1/k8s/lifecycle/upgrade/status?upgradeID=x", "k8s:read"},
		{http.MethodPost, "/api/v1/k8s/lifecycle/rollback", "k8s:write"},
		{http.MethodPost, "/api/v1/k8s/lifecycle/cert-rotate", "k8s:write"},
		{http.MethodPost, "/api/v1/k8s/lifecycle/node-scale", "k8s:write"},
		{http.MethodPost, "/api/v1/k8s/lifecycle/etcd-backup", "k8s:write"},
		{http.MethodPost, "/api/v1/k8s/lifecycle/etcd-restore", "k8s:write"},
		{http.MethodGet, "/api/v1/k8s/lifecycle/etcd-backups?cpNode=n1", "k8s:read"},
	}
	for _, c := range cases {
		perms = nil
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(c.method, c.path, strings.NewReader(`{"clusterId":"c1","targetVersion":"1.30.0"}`))
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s = %d, want 401（鉴权未生效）", c.method, c.path, rec.Code)
		}
		if len(perms) != 1 || perms[0] != c.wantPerm {
			t.Fatalf("%s %s 权限 = %v, want [%s]", c.method, c.path, perms, c.wantPerm)
		}
	}
}

// 未注入 Authorize 时保持向后兼容（独立使用/单测）：路由照常执行。
func TestRoutesPassThroughWithoutAuthorize(t *testing.T) {
	h := newTestHandler()
	mux := http.NewServeMux()
	RegisterK8sLifecycleRoutes(mux, h)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/k8s/lifecycle/upgrade",
		strings.NewReader(`{"clusterId":"c1","targetVersion":"1.30.0"}`)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("未注入 Authorize 时 upgrade = %d, want 202", rec.Code)
	}
}
