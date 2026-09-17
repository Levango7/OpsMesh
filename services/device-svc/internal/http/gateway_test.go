// gateway_test.go — P0 HTTP 网关单测（httptest 全端点覆盖）。
//
// 覆盖目标：注册路由后每个 REST 端点的方法分发、状态码、响应形状。
// 用 MemoryStore 种子数据验证 CRUD 往返；鉴权中间件传直通 stub（本测只验网关层）。
package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/device-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/device-svc/internal/store"
)

// newTestGateway 构造带种子数据的 gateway + mux。
func newTestGateway(t *testing.T) *http.ServeMux {
	t.Helper()
	ms := store.NewMemoryStore()
	// 种子：1 设备 + 1 agent + 1 CI。
	ms.RegisterDevice(&models.Device{ID: "dev-1", TenantID: "default", Name: "web-1", IP: "10.0.0.1", Status: "online"})
	ms.RegisterAgent(&models.Agent{ID: "ag-1", TenantID: "default", Hostname: "host-1", Status: "online"})
	ms.CreateCI(&models.CI{ID: "ci-1", TenantID: "default", CiType: "host", Name: "web-1", Status: "active"})

	g := NewGateway(ms, ms, ms, ms, ms, "https://opsmesh.example.com:8443")
	mux := http.NewServeMux()
	g.RegisterRoutes(mux, func(h http.Handler) http.Handler { return h })
	return mux
}

// doReq 发请求并返回 recorder。
func doReq(t *testing.T, mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// ============ 设备 ============

func TestDevices_ListAndCreate(t *testing.T) {
	mux := newTestGateway(t)

	// 列表：种子设备可见。
	rec := doReq(t, mux, http.MethodGet, "/api/v1/devices", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list devices: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var list struct {
		Devices []*models.Device `json:"devices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("list devices 响应解析失败: %v", err)
	}
	if len(list.Devices) != 1 || list.Devices[0].ID != "dev-1" {
		t.Fatalf("期望 1 台种子设备 dev-1，实际 %+v", list.Devices)
	}

	// 创建：201 + 回显。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/devices", `{"tenantID":"default","name":"new-dev","ip":"10.0.0.9"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create device: got %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var created models.Device
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("create device 响应解析失败: %v", err)
	}
	if created.Name != "new-dev" {
		t.Fatalf("创建回显 name=%q, want new-dev", created.Name)
	}
}

func TestDevices_DetailHeartbeatStatus(t *testing.T) {
	mux := newTestGateway(t)

	// 详情。
	rec := doReq(t, mux, http.MethodGet, "/api/v1/devices/dev-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get device: got %d, body=%s", rec.Code, rec.Body.String())
	}
	// 404。
	rec = doReq(t, mux, http.MethodGet, "/api/v1/devices/no-such", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get missing device: got %d, want 404", rec.Code)
	}
	// 心跳。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/devices/dev-1/heartbeat", `{"status":"online"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("heartbeat: got %d, body=%s", rec.Code, rec.Body.String())
	}
	// 状态。
	rec = doReq(t, mux, http.MethodGet, "/api/v1/devices/dev-1/status", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, body=%s", rec.Code, rec.Body.String())
	}
	// 更新。
	rec = doReq(t, mux, http.MethodPut, "/api/v1/devices/dev-1", `{"name":"renamed"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: got %d, body=%s", rec.Code, rec.Body.String())
	}
	// 删除。
	rec = doReq(t, mux, http.MethodDelete, "/api/v1/devices/dev-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: got %d, body=%s", rec.Code, rec.Body.String())
	}
	// 删除后 404。
	rec = doReq(t, mux, http.MethodGet, "/api/v1/devices/dev-1", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete: got %d, want 404", rec.Code)
	}
}

// ============ Agent ============

func TestAgents_ListAndDetail(t *testing.T) {
	mux := newTestGateway(t)

	rec := doReq(t, mux, http.MethodGet, "/api/v1/agents", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list agents: got %d", rec.Code)
	}
	var list struct {
		Agents []*models.Agent `json:"agents"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("list agents 解析失败: %v", err)
	}
	if len(list.Agents) != 1 || list.Agents[0].ID != "ag-1" {
		t.Fatalf("期望种子 agent ag-1，实际 %+v", list.Agents)
	}

	rec = doReq(t, mux, http.MethodGet, "/api/v1/agents/ag-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get agent: got %d", rec.Code)
	}
	rec = doReq(t, mux, http.MethodPost, "/api/v1/agents/ag-1/heartbeat", `{"status":"online","load":3}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("agent heartbeat: got %d, body=%s", rec.Code, rec.Body.String())
	}
	rec = doReq(t, mux, http.MethodGet, "/api/v1/agents/no-such", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get missing agent: got %d, want 404", rec.Code)
	}
}

// ============ CMDB ============

func TestCMDB_CICRUDAndRelations(t *testing.T) {
	mux := newTestGateway(t)

	// 列表。
	rec := doReq(t, mux, http.MethodGet, "/api/v1/cmdb/cis", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list cis: got %d", rec.Code)
	}
	// 详情。
	rec = doReq(t, mux, http.MethodGet, "/api/v1/cmdb/cis/ci-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get ci: got %d", rec.Code)
	}
	// 更新。
	rec = doReq(t, mux, http.MethodPut, "/api/v1/cmdb/cis/ci-1", `{"name":"renamed-ci"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update ci: got %d, body=%s", rec.Code, rec.Body.String())
	}
	// 创建关系后查关系。
	rec = doReq(t, mux, http.MethodGet, "/api/v1/cmdb/relations/ci-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("relations: got %d, body=%s", rec.Code, rec.Body.String())
	}
	// 删除。
	rec = doReq(t, mux, http.MethodDelete, "/api/v1/cmdb/cis/ci-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete ci: got %d", rec.Code)
	}
	rec = doReq(t, mux, http.MethodGet, "/api/v1/cmdb/cis/ci-1", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete: got %d, want 404", rec.Code)
	}
}

// ============ 发现 ============

func TestDiscovery_CreateAndStatus(t *testing.T) {
	mux := newTestGateway(t)

	// 创建任务：201。
	rec := doReq(t, mux, http.MethodPost, "/api/v1/discovery/jobs", `{"tenantID":"default","cidr":"192.168.1.0/24"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create job: got %d, body=%s", rec.Code, rec.Body.String())
	}
	var job models.DiscoveryJob
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatalf("create job 解析失败: %v", err)
	}
	if job.ID == "" || job.CIDR != "192.168.1.0/24" {
		t.Fatalf("job 回显异常: %+v", job)
	}

	// 查状态。
	rec = doReq(t, mux, http.MethodGet, "/api/v1/discovery/jobs/"+job.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("job status: got %d", rec.Code)
	}

	// 缺 cidr：400。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/discovery/jobs", `{"tenantID":"default"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("create job without cidr: got %d, want 400", rec.Code)
	}

	// 已发现设备列表。
	rec = doReq(t, mux, http.MethodGet, "/api/v1/discovery/devices", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("discovered devices: got %d", rec.Code)
	}
}

// ============ 自动纳管（D3） ============

// TestProvisionAuto_BadRequests 验证 /api/v1/provision/auto 的方法与参数校验。
func TestProvisionAuto_BadRequests(t *testing.T) {
	mux := newTestGateway(t)

	// GET 不允许：405。
	rec := doReq(t, mux, http.MethodGet, "/api/v1/provision/auto", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET provision/auto: got %d, want 405", rec.Code)
	}
	// 无效 JSON：400。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/provision/auto", `{invalid`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid JSON: got %d, want 400", rec.Code)
	}
	// 空 cidrs：400。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/provision/auto", `{"tenantID":"t1"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no cidrs: got %d, want 400", rec.Code)
	}
}

// TestProvisionAuto_RunFullLoop 验证编排触发返回 200 + Summary 形状。
// 用 RFC 5737 TEST-NET 网段避免触碰真实网络；无存活主机时 Scanned=0。
func TestProvisionAuto_RunFullLoop(t *testing.T) {
	mux := newTestGateway(t)

	rec := doReq(t, mux, http.MethodPost, "/api/v1/provision/auto", `{"cidrs":["192.0.2.0/30"],"tenantID":"t1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("provision auto: got %d, body=%s", rec.Code, rec.Body.String())
	}
	var sum struct {
		Scanned     int      `json:"scanned"`
		Registered  int      `json:"registered"`
		Provisioned int      `json:"provisioned"`
		SSHPushed   int      `json:"sshPushed"`
		Failures    []string `json:"failures"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &sum); err != nil {
		t.Fatalf("summary 解析失败: %v (body=%s)", err, rec.Body.String())
	}
	// TEST-NET 网段无存活主机：全零计数（SSH 未配置，无副作用）。
	if sum.Scanned != 0 || sum.SSHPushed != 0 {
		t.Fatalf("TEST-NET 应无存活主机，Scanned=%d SSHPushed=%d", sum.Scanned, sum.SSHPushed)
	}
}

// ============ bootstrap 端点（D3-d） ============

// TestInstallSh_ServesScript 验证 GET /install.sh 下发自举脚本（含 advertise 地址）。
func TestInstallSh_ServesScript(t *testing.T) {
	mux := newTestGateway(t)

	// 方法校验：405。
	rec := doReq(t, mux, http.MethodPost, "/install.sh", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /install.sh: got %d, want 405", rec.Code)
	}
	// GET：200 + 脚本内容。
	rec = doReq(t, mux, http.MethodGet, "/install.sh", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /install.sh: got %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "opsmesh-agent") {
		t.Fatalf("脚本应包含 opsmesh-agent，got: %.200s", body)
	}
	if !strings.Contains(body, "https://opsmesh.example.com:8443") {
		t.Fatalf("脚本应内嵌 advertise 地址（newTestGateway 注入），got: %.200s", body)
	}
}

// TestProvisionRegister_TokenLifecycle 验证 token 消费注册端点全生命周期：
// 无 token 400 → 无效 token 401 → 有效 token 200 翻转设备 → 二次消费 401 → 设备不存在 404。
func TestProvisionRegister_TokenLifecycle(t *testing.T) {
	ms := store.NewMemoryStore()
	// 种子：候选设备 dev-1（discovered 状态，模拟 AutoProvision 登记产物）。
	ms.RegisterDevice(&models.Device{ID: "dev-1", TenantID: "default", IP: "10.0.0.1", Status: "discovered"})
	g := NewGateway(ms, ms, ms, ms, ms, "https://opsmesh.example.com:8443")
	mux := http.NewServeMux()
	g.RegisterRoutes(mux, func(h http.Handler) http.Handler { return h })

	// 方法校验：405。
	rec := doReq(t, mux, http.MethodGet, "/api/v1/provision/register", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET register: got %d, want 405", rec.Code)
	}
	// 缺 token：400。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/provision/register", `{"agentID":"ag-1"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing token: got %d, want 400", rec.Code)
	}
	// 无效 token：401。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/provision/register", `{"token":"garbage.token"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid token: got %d, want 401", rec.Code)
	}

	// 签发有效 token 并消费注册。
	tok, err := ms.IssueToken("dev-1", "default", 15*time.Minute)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	rec = doReq(t, mux, http.MethodPost, "/api/v1/provision/register", `{"token":"`+tok+`","agentID":"ag-1","hostname":"web-1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid token register: got %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Status   string `json:"status"`
		DeviceID string `json:"deviceID"`
		TenantID string `json:"tenantID"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("register 响应解析失败: %v", err)
	}
	if resp.Status != "registered" || resp.DeviceID != "dev-1" {
		t.Fatalf("register 回显异常: %+v", resp)
	}
	// 设备翻转验证：discovered → online + AgentID 回填。
	dev := ms.Device("dev-1")
	if dev == nil {
		t.Fatal("dev-1 应存在")
	}
	if dev.Status != "online" {
		t.Fatalf("设备应翻转为 online，got %s", dev.Status)
	}
	if dev.AgentID != "ag-1" {
		t.Fatalf("AgentID 应回填 ag-1，got %s", dev.AgentID)
	}
	if dev.Name != "web-1" {
		t.Fatalf("hostname 应回填为 Name=web-1，got %s", dev.Name)
	}

	// 一次性语义：同一 token 二次消费 401。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/provision/register", `{"token":"`+tok+`"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("re-consume token: got %d, want 401（一次性）", rec.Code)
	}

	// 设备不存在：为不存在的设备签发 token，注册 404。
	tok2, err := ms.IssueToken("dev-404", "default", 15*time.Minute)
	if err != nil {
		t.Fatalf("IssueToken dev-404: %v", err)
	}
	rec = doReq(t, mux, http.MethodPost, "/api/v1/provision/register", `{"token":"`+tok2+`"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("device not found: got %d, want 404", rec.Code)
	}
}

// ============ P0 端点补齐：provision / metrics / 聚合响应 ============

// TestDeviceProvision_ManualTrigger 验证 POST /api/v1/devices/{id}/provision 手动纳管：
// 设备不存在 404 → 成功 200 返回 token + bootstrap → 租户不匹配 403。
func TestDeviceProvision_ManualTrigger(t *testing.T) {
	mux := newTestGateway(t)

	// 设备不存在：404。
	rec := doReq(t, mux, http.MethodPost, "/api/v1/devices/no-such/provision", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("provision missing device: got %d, want 404", rec.Code)
	}

	// 成功纳管：200 + token + bootstrap。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/devices/dev-1/provision", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("provision dev-1: got %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Status       string `json:"status"`
		DeviceID     string `json:"deviceID"`
		InstallToken string `json:"installToken"`
		Bootstrap    string `json:"bootstrap"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("provision 响应解析失败: %v", err)
	}
	if resp.Status != "provisioning" {
		t.Fatalf("status=%q, want provisioning", resp.Status)
	}
	if resp.DeviceID != "dev-1" {
		t.Fatalf("deviceID=%q, want dev-1", resp.DeviceID)
	}
	if resp.InstallToken == "" {
		t.Fatal("installToken 不应为空")
	}
	if !strings.Contains(resp.Bootstrap, "install.sh") || !strings.Contains(resp.Bootstrap, resp.InstallToken) {
		t.Fatalf("bootstrap 应包含 install.sh 和 token，got: %s", resp.Bootstrap)
	}
	if !strings.Contains(resp.Bootstrap, "https://opsmesh.example.com:8443") {
		t.Fatalf("bootstrap 应使用 advertise 地址，got: %s", resp.Bootstrap)
	}

	// 租户不匹配：403。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/devices/dev-1/provision?tenantID=other-tenant", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("provision tenant mismatch: got %d, want 403", rec.Code)
	}

	// GET 不允许：走 handleDeviceDetail 的子路径匹配，provision 只接受 POST。
	// GET /api/v1/devices/dev-1/provision 不匹配任何子路径分支，落到 rest 含 / 的 404。
	rec = doReq(t, mux, http.MethodGet, "/api/v1/devices/dev-1/provision", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET provision: got %d, want 404", rec.Code)
	}
}

// TestDeviceMetrics_NoStoreDegraded 验证未注入 MetricsStore 时降级返回空数组不报错。
func TestDeviceMetrics_NoStoreDegraded(t *testing.T) {
	mux := newTestGateway(t)

	// 设备不存在：404。
	rec := doReq(t, mux, http.MethodGet, "/api/v1/devices/no-such/metrics", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("metrics missing device: got %d, want 404", rec.Code)
	}

	// 无 range：降级返回空 metrics。
	rec = doReq(t, mux, http.MethodGet, "/api/v1/devices/dev-1/metrics", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics no store: got %d, body=%s", rec.Code, rec.Body.String())
	}
	var latest struct {
		DeviceID string `json:"deviceID"`
		Metrics  any    `json:"metrics"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &latest); err != nil {
		t.Fatalf("metrics 响应解析失败: %v", err)
	}
	if latest.DeviceID != "dev-1" || latest.Metrics != nil {
		t.Fatalf("降级应返回 deviceID + nil metrics，got %+v", latest)
	}

	// 带 range：降级返回空 samples 数组。
	rec = doReq(t, mux, http.MethodGet, "/api/v1/devices/dev-1/metrics?range=2h", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics range no store: got %d, body=%s", rec.Code, rec.Body.String())
	}
	var series struct {
		DeviceID string `json:"deviceID"`
		Range    string `json:"range"`
		Samples  []any  `json:"samples"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &series); err != nil {
		t.Fatalf("metrics series 响应解析失败: %v", err)
	}
	if series.DeviceID != "dev-1" || series.Range != "2h" || len(series.Samples) != 0 {
		t.Fatalf("降级应返回空 samples，got %+v", series)
	}
}

// fakeMetricsStore 是测试用的 MetricsStore 桩。
type fakeMetricsStore struct {
	latest    any
	history   []any
	callDev   string
	callSince time.Time
}

func (f *fakeMetricsStore) DeviceMetrics(deviceID string) any {
	f.callDev = deviceID
	return f.latest
}

func (f *fakeMetricsStore) DeviceMetricsHistory(deviceID string, since time.Time) []any {
	f.callDev = deviceID
	f.callSince = since
	return f.history
}

// TestDeviceMetrics_WithStore 验证注入 MetricsStore 后返回真实数据。
func TestDeviceMetrics_WithStore(t *testing.T) {
	ms := store.NewMemoryStore()
	ms.RegisterDevice(&models.Device{ID: "dev-1", TenantID: "default", IP: "10.0.0.1", Status: "online"})
	g := NewGateway(ms, ms, ms, ms, ms, "https://opsmesh.example.com:8443")

	// 注入 fakeMetricsStore：最新值模式。
	fakeLatest := map[string]any{"deviceID": "dev-1", "cpu": 42.5, "memory": 60.0}
	g.SetMetricsStore(&fakeMetricsStore{latest: fakeLatest})

	mux := http.NewServeMux()
	g.RegisterRoutes(mux, func(h http.Handler) http.Handler { return h })

	// 无 range：返回最新值。
	rec := doReq(t, mux, http.MethodGet, "/api/v1/devices/dev-1/metrics", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics with store: got %d, body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("metrics 响应解析失败: %v", err)
	}
	if got["deviceID"] != "dev-1" || got["cpu"] != 42.5 {
		t.Fatalf("应返回 fake 最新指标，got %+v", got)
	}

	// 带 range：返回历史时序。
	fakeHistory := []any{
		map[string]any{"deviceID": "dev-1", "cpu": 10.0},
		map[string]any{"deviceID": "dev-1", "cpu": 20.0},
	}
	g.SetMetricsStore(&fakeMetricsStore{history: fakeHistory})
	// 重新注册 mux（SetMetricsStore 后需要重新注册以反映新状态——实际上 Gateway 持有引用，
	// 无需重新注册；但此处为清晰起见重新构建）。
	mux2 := http.NewServeMux()
	g.RegisterRoutes(mux2, func(h http.Handler) http.Handler { return h })

	rec = doReq(t, mux2, http.MethodGet, "/api/v1/devices/dev-1/metrics?range=2h", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics range with store: got %d, body=%s", rec.Code, rec.Body.String())
	}
	var series struct {
		DeviceID string `json:"deviceID"`
		Range    string `json:"range"`
		Samples  []any  `json:"samples"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &series); err != nil {
		t.Fatalf("series 响应解析失败: %v", err)
	}
	if series.Range != "2h" || len(series.Samples) != 2 {
		t.Fatalf("应返回 2 条历史样本，got %+v", series)
	}

	// 非法 range：400。
	rec = doReq(t, mux2, http.MethodGet, "/api/v1/devices/dev-1/metrics?range=99d", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid range: got %d, want 400", rec.Code)
	}
}

// fakeTaskResultFetcher 是测试用的 TaskResultFetcher 桩。
type fakeTaskResultFetcher struct {
	tasks   []any
	results []any
}

func (f *fakeTaskResultFetcher) TasksByAgent(agentID, tenantID string) []any {
	return f.tasks
}

func (f *fakeTaskResultFetcher) ResultsByAgent(agentID string) []any {
	return f.results
}

// TestDeviceDetail_AggregatedResponse 验证 GET /api/v1/devices/{id} 聚合响应：
// 返回 {device, tasks, results} 而非仅 device；未注入 fetcher 时降级为空数组。
func TestDeviceDetail_AggregatedResponse(t *testing.T) {
	ms := store.NewMemoryStore()
	// 种子设备带 AgentID（聚合响应按 AgentID 关联 tasks/results）。
	ms.RegisterDevice(&models.Device{
		ID: "dev-1", TenantID: "default", Name: "web-1", IP: "10.0.0.1",
		Status: "online", AgentID: "ag-1",
	})
	g := NewGateway(ms, ms, ms, ms, ms, "https://opsmesh.example.com:8443")

	// 未注入 TaskResultFetcher：降级返回空 tasks/results。
	mux := http.NewServeMux()
	g.RegisterRoutes(mux, func(h http.Handler) http.Handler { return h })

	rec := doReq(t, mux, http.MethodGet, "/api/v1/devices/dev-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get device: got %d, body=%s", rec.Code, rec.Body.String())
	}
	var dd struct {
		Device  *models.Device `json:"device"`
		Tasks   []any          `json:"tasks"`
		Results []any          `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &dd); err != nil {
		t.Fatalf("聚合响应解析失败: %v", err)
	}
	if dd.Device == nil || dd.Device.ID != "dev-1" {
		t.Fatalf("device 字段应非空且 ID=dev-1，got %+v", dd.Device)
	}
	if dd.Tasks == nil || len(dd.Tasks) != 0 {
		t.Fatalf("降级 tasks 应为空数组，got %+v", dd.Tasks)
	}
	if dd.Results == nil || len(dd.Results) != 0 {
		t.Fatalf("降级 results 应为空数组，got %+v", dd.Results)
	}

	// 注入 TaskResultFetcher：返回真实 tasks/results。
	g.SetTaskResultFetcher(&fakeTaskResultFetcher{
		tasks:   []any{map[string]any{"taskID": "task-1", "agentID": "ag-1"}},
		results: []any{map[string]any{"taskID": "task-1", "exitCode": 0}},
	})
	mux2 := http.NewServeMux()
	g.RegisterRoutes(mux2, func(h http.Handler) http.Handler { return h })

	rec = doReq(t, mux2, http.MethodGet, "/api/v1/devices/dev-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get device with fetcher: got %d, body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &dd); err != nil {
		t.Fatalf("聚合响应解析失败: %v", err)
	}
	if len(dd.Tasks) != 1 {
		t.Fatalf("应返回 1 个 task，got %d", len(dd.Tasks))
	}
	if len(dd.Results) != 1 {
		t.Fatalf("应返回 1 个 result，got %d", len(dd.Results))
	}

	// 设备不存在：404。
	rec = doReq(t, mux2, http.MethodGet, "/api/v1/devices/no-such", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get missing device: got %d, want 404", rec.Code)
	}
}

// TestDeviceDetail_Aggregated_NoAgentID 验证设备无 AgentID 时聚合响应 tasks/results 为空。
func TestDeviceDetail_Aggregated_NoAgentID(t *testing.T) {
	mux := newTestGateway(t)
	// 种子设备 dev-1 无 AgentID（newTestGateway 默认种子）。

	rec := doReq(t, mux, http.MethodGet, "/api/v1/devices/dev-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get device: got %d, body=%s", rec.Code, rec.Body.String())
	}
	var dd struct {
		Device  *models.Device `json:"device"`
		Tasks   []any          `json:"tasks"`
		Results []any          `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &dd); err != nil {
		t.Fatalf("聚合响应解析失败: %v", err)
	}
	if dd.Device == nil || dd.Device.ID != "dev-1" {
		t.Fatalf("device 字段应非空且 ID=dev-1，got %+v", dd.Device)
	}
	// 无 AgentID：tasks/results 应为空数组（fetchTasksAndResults 在 agentID 为空时返回空）。
	if len(dd.Tasks) != 0 || len(dd.Results) != 0 {
		t.Fatalf("无 AgentID 时 tasks/results 应为空，got tasks=%d results=%d", len(dd.Tasks), len(dd.Results))
	}
}
