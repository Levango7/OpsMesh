// acceptance_idempotency_test.go — 批次30-32 验收硬化：重复提交 / 幂等语义。
//
// 覆盖：
//
//	C9  device-op 同一签名 payload 第二次 → 403 replay；
//	C10 中间件同一 (tenant, template, version, agent) 重复部署：两次均 201，
//	    instanceID 相同，实例清单按复合键去重（仅 1 行），但每次部署仍各自创建任务
//	    （代码现状：部署不做任务级幂等——已在测试中钉住）；
//	C11 未知实例卸载 → 404；重复卸载 → 仍 201（实例进入 uninstalling 后仍允许再次下发），
//	    实例清单始终 1 行，不出现重复/负数；
//	C12 非 pending 用户重复 approve/reject → 409；重复 DELETE → 第二次 404；
//	C13 重复 suspend/activate → 200 且状态幂等。
package controlplane

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

// ---------------------------------------------------------------------------
// C9 device-op 签名重放
// ---------------------------------------------------------------------------

// TestFederationDeviceOp_ReplayRejected_Metrics 验证同一份 metrics 设备操作签名
// 第二次提交被判重放（403）。与既有 cmdb-discover 重放用例互补。
func TestFederationDeviceOp_ReplayRejected_Metrics(t *testing.T) {
	const secret = "fed-device-op-replay-metrics"
	s := newFederationDeviceTestServer(t, secret, true)
	deviceID := seedDeviceMetrics(t, s, "t1")

	payload, _ := json.Marshal(map[string]string{"deviceID": deviceID})
	body, _ := json.Marshal(map[string]interface{}{"op": "metrics", "payload": payload})
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	header := federationTestHeaders(t, secret, http.MethodPost, federationDeviceOpPath, ts, "device-op-replay-"+strconv.FormatInt(time.Now().UnixNano(), 10), "t1", "peer-user", "role-operator", body)

	do := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, federationDeviceOpPath, bytes.NewReader(body))
		req.Header = header.Clone()
		rec := httptest.NewRecorder()
		s.handleFederationForwardDeviceOp(rec, req)
		return rec
	}

	if rec := do(); rec.Code != http.StatusOK {
		t.Fatalf("first signed device-op = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if rec := do(); rec.Code != http.StatusForbidden {
		t.Fatalf("replayed device-op = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// C10 中间件重复部署
// ---------------------------------------------------------------------------

func deployAcceptanceMiddleware(t *testing.T, s *Server, agentID string) (int, map[string]interface{}) {
	t.Helper()
	body := fmt.Sprintf(`{"agentID":%q,"params":{"password":"pw-accept"}}`, agentID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-templates/mysql/deploy", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	var resp map[string]interface{}
	if rec.Code == http.StatusCreated {
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode deploy resp: %v; body=%s", err, rec.Body.String())
		}
	}
	return rec.Code, resp
}

// TestMiddlewareDeploy_RepeatedSameTuple 锁定重复部署语义：
//   - 两次部署都 201（部署请求不幂等，每次都会下发新任务）；
//   - 响应 instanceID 相同（复合键由 template/agent/deployType/version 决定）；
//   - 实例清单按复合键去重，只有 1 行；审计中有 2 条 deploy_middleware。
func TestMiddlewareDeploy_RepeatedSameTuple(t *testing.T) {
	s := newMWTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})

	code1, resp1 := deployAcceptanceMiddleware(t, s, a.AgentID)
	if code1 != http.StatusCreated {
		t.Fatalf("first deploy = %d, want 201; resp=%v", code1, resp1)
	}
	code2, resp2 := deployAcceptanceMiddleware(t, s, a.AgentID)
	if code2 != http.StatusCreated {
		t.Fatalf("second deploy = %d, want 201; resp=%v", code2, resp2)
	}
	key1, _ := resp1["instanceID"].(string)
	key2, _ := resp2["instanceID"].(string)
	if key1 == "" || key1 != key2 {
		t.Fatalf("instanceID diverged or empty: first=%q second=%q", key1, key2)
	}

	instances := s.deriveMiddlewareInstances("default", "", "", "", false)
	if len(instances) != 1 {
		t.Fatalf("inventory rows = %d, want 1 (dedup by instance key); instances=%+v", len(instances), instances)
	}
	if instances[0].ID != key1 {
		t.Fatalf("inventory id = %q, want %q", instances[0].ID, key1)
	}

	deployAudits := 0
	for _, ev := range s.store.Audits() {
		if ev.Action == "deploy_middleware" {
			deployAudits++
		}
	}
	if deployAudits != 2 {
		t.Fatalf("deploy_middleware audits = %d, want 2 (task-level deploy is not idempotent)", deployAudits)
	}
}

// ---------------------------------------------------------------------------
// C11 未知 / 重复卸载
// ---------------------------------------------------------------------------

// TestMiddlewareUninstall_UnknownAndRepeated 锁定卸载语义：
//   - 存在部署清单后，未知实例键 → 404，且不写卸载审计；
//   - 已部署实例首次卸载 → 201；
//   - 同一实例重复卸载 → 仍 201（清单存在 uninstalling 记录时允许再次下发）；
//   - 清单始终 1 行（includeRemoved=1），默认视图 0 行，不重复计数。
func TestMiddlewareUninstall_UnknownAndRepeated(t *testing.T) {
	s := newMWTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})

	code, resp := deployAcceptanceMiddleware(t, s, a.AgentID)
	if code != http.StatusCreated {
		t.Fatalf("deploy = %d, want 201; resp=%v", code, resp)
	}
	key, _ := resp["instanceID"].(string)
	if key == "" {
		t.Fatal("deploy response missing instanceID")
	}
	uninstallBody := fmt.Sprintf(`{"agentID":%q,"templateID":"mysql","deployType":"docker","params":{"password":"pw-accept"}}`, a.AgentID)

	uninstall := func(id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-instances/"+id+"/uninstall", strings.NewReader(uninstallBody))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.handleMiddlewareInstanceRouting(rec, req)
		return rec
	}

	// 未知实例键：存在部署清单时必须 404。
	if rec := uninstall("unknown@agent:docker:0.0"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown instance uninstall = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	uninstallAudits := func() int {
		n := 0
		for _, ev := range s.store.Audits() {
			if ev.Action == "uninstall_middleware" {
				n++
			}
		}
		return n
	}
	if n := uninstallAudits(); n != 0 {
		t.Fatalf("uninstall audits after unknown instance = %d, want 0", n)
	}

	// 首次卸载。
	if rec := uninstall(key); rec.Code != http.StatusCreated {
		t.Fatalf("first uninstall = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	// 重复卸载：同一实例仍存在（uninstalling），语义固定为再次 201。
	if rec := uninstall(key); rec.Code != http.StatusCreated {
		t.Fatalf("repeated uninstall = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}

	// includeRemoved=1：唯一实例、状态 uninstalling、无重复行。
	req := httptest.NewRequest(http.MethodGet, "/api/v1/middleware-instances?includeRemoved=1", nil)
	rec := httptest.NewRecorder()
	s.handleMiddlewareInstances(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list includeRemoved = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var withRemoved []middlewareInstance
	if err := json.Unmarshal(rec.Body.Bytes(), &withRemoved); err != nil {
		t.Fatalf("decode includeRemoved: %v", err)
	}
	if len(withRemoved) != 1 {
		t.Fatalf("includeRemoved rows = %d, want 1; rows=%+v", len(withRemoved), withRemoved)
	}
	if withRemoved[0].ID != key || withRemoved[0].Status != "uninstalling" {
		t.Fatalf("instance = %+v, want id=%s status=uninstalling", withRemoved[0], key)
	}

	// 默认视图：不显示已卸载实例（0 行，不会出现负数/重复计数）。
	req = httptest.NewRequest(http.MethodGet, "/api/v1/middleware-instances", nil)
	rec = httptest.NewRecorder()
	s.handleMiddlewareInstances(rec, req)
	var visible []middlewareInstance
	if err := json.Unmarshal(rec.Body.Bytes(), &visible); err != nil {
		t.Fatalf("decode visible: %v", err)
	}
	if len(visible) != 0 {
		t.Fatalf("default inventory rows = %d, want 0", len(visible))
	}
	if n := uninstallAudits(); n != 2 {
		t.Fatalf("uninstall audits = %d, want 2", n)
	}
}

// ---------------------------------------------------------------------------
// C12 用户审批 / 拒绝 / 删除幂等
// ---------------------------------------------------------------------------

func createAcceptancePendingUser(t *testing.T, s *Server, id string) {
	t.Helper()
	u := &store.User{
		ID:           id,
		Username:     id,
		TenantID:     store.DefaultTenantID,
		PasswordHash: "unused",
		Status:       "pending",
		RoleIDs:      []string{"role-viewer"},
	}
	if s.store.CreateUser(u) == nil {
		t.Fatalf("CreateUser(%s) failed", id)
	}
}

// TestUserLifecycle_RepeatedActionsIdempotency 验证重复审批/拒绝/删除的状态码契约：
//   - 非 pending 用户 approve/reject → 409（一致，不 500）；
//   - 重复 DELETE → 第一次 204，第二次 404。
func TestUserLifecycle_RepeatedActionsIdempotency(t *testing.T) {
	s := newAuthTestServer(t)
	auth := loginAsAdmin(t, s)

	call := func(method, path string) *httptest.ResponseRecorder {
		req := doWithAuth(method, path, auth, nil)
		rec := httptest.NewRecorder()
		s.handleUserRouting(rec, req)
		return rec
	}

	// approve 幂等性。
	createAcceptancePendingUser(t, s, "acc-pending-approve")
	if rec := call(http.MethodPost, "/api/v1/users/acc-pending-approve/approve"); rec.Code != http.StatusOK {
		t.Fatalf("first approve = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if rec := call(http.MethodPost, "/api/v1/users/acc-pending-approve/approve"); rec.Code != http.StatusConflict {
		t.Fatalf("repeated approve = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	if rec := call(http.MethodPost, "/api/v1/users/acc-pending-approve/reject"); rec.Code != http.StatusConflict {
		t.Fatalf("reject after approve = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}

	// reject 幂等性。
	createAcceptancePendingUser(t, s, "acc-pending-reject")
	if rec := call(http.MethodPost, "/api/v1/users/acc-pending-reject/reject"); rec.Code != http.StatusOK {
		t.Fatalf("first reject = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if rec := call(http.MethodPost, "/api/v1/users/acc-pending-reject/reject"); rec.Code != http.StatusConflict {
		t.Fatalf("repeated reject = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}

	// DELETE 幂等性：首次 204，第二次 404。
	createAcceptancePendingUser(t, s, "acc-delete-me")
	if rec := call(http.MethodDelete, "/api/v1/users/acc-delete-me"); rec.Code != http.StatusNoContent {
		t.Fatalf("first delete = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	if rec := call(http.MethodDelete, "/api/v1/users/acc-delete-me"); rec.Code != http.StatusNotFound {
		t.Fatalf("repeated delete = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	if s.store.GetUser("acc-delete-me") != nil {
		t.Fatal("deleted user still present in store")
	}
}

// ---------------------------------------------------------------------------
// C13 租户 suspend / activate 幂等
// ---------------------------------------------------------------------------

// TestTenantLifecycle_RepeatedSuspendActivate 验证重复 suspend/activate：
// 每次均 200、状态幂等、不产生 5xx。
func TestTenantLifecycle_RepeatedSuspendActivate(t *testing.T) {
	s := newAuthTestServer(t)
	auth := loginAsAdmin(t, s)
	const tenantID = "acc-tenant-lifecycle"
	if s.store.CreateTenant(&store.Tenant{ID: tenantID, Name: tenantID}) == nil {
		t.Fatal("CreateTenant failed")
	}

	call := func(action string) *httptest.ResponseRecorder {
		req := doWithAuth(http.MethodPost, "/api/v1/tenants/"+tenantID+"/"+action, auth, nil)
		rec := httptest.NewRecorder()
		s.handleTenantRouting(rec, req)
		return rec
	}
	assertStatus := func(action, want string) {
		t.Helper()
		got, ok := s.store.GetTenant(tenantID)
		if !ok || got == nil {
			t.Fatalf("tenant %s missing after %s", tenantID, action)
		}
		if string(got.Status) != want {
			t.Fatalf("%s status = %q, want %q", action, got.Status, want)
		}
	}

	for i := 0; i < 2; i++ {
		if rec := call("suspend"); rec.Code != http.StatusOK {
			t.Fatalf("suspend #%d = %d, want 200; body=%s", i+1, rec.Code, rec.Body.String())
		}
		assertStatus("suspend", string(store.TenantStatusSuspended))
	}
	for i := 0; i < 2; i++ {
		if rec := call("activate"); rec.Code != http.StatusOK {
			t.Fatalf("activate #%d = %d, want 200; body=%s", i+1, rec.Code, rec.Body.String())
		}
		assertStatus("activate", string(store.TenantStatusActive))
	}

	// 四次操作各留一条审计（无错误风暴；重复操作不改变状态机）。
	seen := map[string]int{}
	for _, ev := range s.store.Audits() {
		if ev.Target == tenantID {
			seen[ev.Action]++
		}
	}
	if seen["tenant_suspend"] != 2 || seen["tenant_activate"] != 2 {
		t.Fatalf("tenant lifecycle audits = %+v, want suspend=2 activate=2", seen)
	}
}
