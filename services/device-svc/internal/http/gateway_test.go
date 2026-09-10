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
