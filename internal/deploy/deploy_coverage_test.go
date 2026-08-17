// deploy_coverage_test.go 补充 internal/deploy 包未覆盖函数的测试，目标覆盖率 70%+。
// 重点覆盖：Handler getter/setter、HTTP 路由、AdvanceCanary/PromoteMember、
// AutoAdvanceManager.ServeHTTP/recordError/isTerminalStatus 错误路径、
// Federation 各方法错误路径、Model 边界、Store.Delete、SQL 纯函数。
package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"opsmesh/internal/proto"
)

// =============================================================================
// Handler getter/setter（SetFederationStore/Federation/SetAutoAdvance/Store）
// =============================================================================

func TestHandler_GettersSetters(t *testing.T) {
	st := NewMemory()
	disp := newFakeDisp()
	h := NewHandler(st, disp)

	// Store getter。
	if h.Store() != st {
		t.Fatal("Store() should return underlying store")
	}

	// Federation getter（NewHandler 默认启用内存联邦协调器）。
	if h.Federation() == nil {
		t.Fatal("Federation() should not be nil by default")
	}

	// SetFederationStore 替换为新的内存后端。
	newFedStore := NewMemoryFederationStore()
	h.SetFederationStore(newFedStore)
	if h.Federation().Store() != newFedStore {
		t.Fatal("SetFederationStore should replace federation store")
	}

	// SetFederationStore(nil) 应被忽略（保留原协调器）。
	origFed := h.Federation()
	h.SetFederationStore(nil)
	if h.Federation() != origFed {
		t.Fatal("SetFederationStore(nil) should be ignored")
	}

	// SetAutoAdvance 注入管理器。
	mgr := NewAutoAdvanceManager(DefaultAutoAdvanceConfig(), st, newFakeTaskResults(), nil, nil, nil)
	h.SetAutoAdvance(mgr)
	if h.autoAdvance != mgr {
		t.Fatal("SetAutoAdvance should set autoAdvance field")
	}
}

// =============================================================================
// handleDeploys HTTP 路由（POST/GET/default）
// =============================================================================

func TestHandleDeploys_POST_GET(t *testing.T) {
	st := NewMemory()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	h := NewHandler(st, disp)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// POST 创建。
	body := `{"name":"svc","type":"script","target_ids":"dev-1","repo_url":"https://x.git"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/deploys", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-User-ID", "u1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST should return 201, got %d body=%s", w.Code, w.Body.String())
	}
	var created DeployTask
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	if created.TenantID != "t1" || created.CreatedBy != "u1" || created.Status != StatusCreated {
		t.Fatalf("unexpected created: %+v", created)
	}

	// GET 列表。
	req = httptest.NewRequest(http.MethodGet, "/api/v1/deploys", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET should return 200, got %d", w.Code)
	}
	var list []DeployTask
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("unexpected list: %+v", list)
	}

	// GET with status filter（应返回空列表，因 created 状态匹配）。
	req = httptest.NewRequest(http.MethodGet, "/api/v1/deploys?status=created", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET with status filter should return 200, got %d", w.Code)
	}
}

func TestHandleDeploys_BadJSON(t *testing.T) {
	st := NewMemory()
	disp := newFakeDisp()
	h := NewHandler(st, disp)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/deploys", strings.NewReader("{bad json"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON should return 400, got %d", w.Code)
	}
}

func TestHandleDeploys_InvalidDeploy(t *testing.T) {
	st := NewMemory()
	disp := newFakeDisp()
	h := NewHandler(st, disp)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// 缺 name 字段，应触发 store.Create 校验失败。
	body := `{"name":"","type":"script","target_ids":"dev-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/deploys", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid deploy should return 400, got %d", w.Code)
	}
}

func TestHandleDeploys_MethodNotAllowed(t *testing.T) {
	st := NewMemory()
	disp := newFakeDisp()
	h := NewHandler(st, disp)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/deploys", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT should return 405, got %d", w.Code)
	}
}

func TestHandleDeploys_DefaultTenant(t *testing.T) {
	st := NewMemory()
	disp := newFakeDisp()
	h := NewHandler(st, disp)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// 无 X-Tenant-ID 头：应回退 default。
	body := `{"name":"svc","type":"script","target_ids":"dev-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/deploys", strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST with default tenant should return 201, got %d", w.Code)
	}
	var created DeployTask
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if created.TenantID != "default" || created.CreatedBy != "local" {
		t.Fatalf("expected default tenant/local user, got tenant=%s user=%s", created.TenantID, created.CreatedBy)
	}
}

// =============================================================================
// handleDeployByID HTTP 路由（GET/execute/rollback/promote/auto-advance）
// =============================================================================

func TestHandleDeployByID_GET(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	h := NewHandler(st, disp)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	dt, _ := st.Create(ctx, newDeploy("svc", TypeScript, "dev-1", "t1"))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/deploys/"+itoa(dt.ID), nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET should return 200, got %d", w.Code)
	}
}

func TestHandleDeployByID_NotFound(t *testing.T) {
	st := NewMemory()
	disp := newFakeDisp()
	h := NewHandler(st, disp)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/deploys/9999", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GET non-existent should return 404, got %d", w.Code)
	}
}

func TestHandleDeployByID_InvalidID(t *testing.T) {
	st := NewMemory()
	disp := newFakeDisp()
	h := NewHandler(st, disp)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/deploys/abc", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid id should return 400, got %d", w.Code)
	}
}

func TestHandleDeployByID_Execute(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	h := NewHandler(st, disp)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	dt, _ := st.Create(ctx, newDeploy("svc", TypeScript, "dev-1", "t1"))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/deploys/"+itoa(dt.ID)+"/execute", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("execute should return 200, got %d body=%s", w.Code, w.Body.String())
	}
	got, _ := st.Get(ctx, dt.ID, "t1")
	if got.Status != StatusRunning {
		t.Fatalf("expected running, got %s", got.Status)
	}
}

func TestHandleDeployByID_ExecuteMethodNotAllowed(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	disp := newFakeDisp()
	h := NewHandler(st, disp)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	dt, _ := st.Create(ctx, newDeploy("svc", TypeScript, "dev-1", "t1"))

	// GET /execute 应 405。
	req := httptest.NewRequest(http.MethodGet, "/api/v1/deploys/"+itoa(dt.ID)+"/execute", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /execute should return 405, got %d", w.Code)
	}
}

func TestHandleDeployByID_Rollback(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	h := NewHandler(st, disp)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	dt, _ := st.Create(ctx, newDeploy("svc", TypeScript, "dev-1", "t1"))
	_ = h.Execute(ctx, dt.ID, "t1")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/deploys/"+itoa(dt.ID)+"/rollback", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("rollback should return 200, got %d", w.Code)
	}
}

func TestHandleDeployByID_Promote(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	h := NewHandler(st, disp)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	dt, _ := st.Create(ctx, &DeployTask{
		Name: "svc", Type: TypeScript, TargetIDs: "dev-1", TenantID: "t1", CreatedBy: "tester",
		Strategy: StrategyBlueGreen,
	})
	_ = h.Execute(ctx, dt.ID, "t1")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/deploys/"+itoa(dt.ID)+"/promote", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("promote should return 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHandleDeployByID_AutoAdvanceNotEnabled(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	h := NewHandler(st, disp) // 未注入 autoAdvance
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	dt, _ := st.Create(ctx, newDeploy("svc", TypeScript, "dev-1", "t1"))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/deploys/"+itoa(dt.ID)+"/auto-advance", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("auto-advance without manager should return 501, got %d", w.Code)
	}
}

func TestHandleDeployByID_UnknownSubPath(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	disp := newFakeDisp()
	h := NewHandler(st, disp)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	dt, _ := st.Create(ctx, newDeploy("svc", TypeScript, "dev-1", "t1"))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/deploys/"+itoa(dt.ID)+"/bogus", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown sub-path should return 404, got %d", w.Code)
	}
}

func TestHandleDeployByID_DefaultMethodNotAllowed(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	disp := newFakeDisp()
	h := NewHandler(st, disp)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	dt, _ := st.Create(ctx, newDeploy("svc", TypeScript, "dev-1", "t1"))

	// PUT /{id} 默认分支应 405。
	req := httptest.NewRequest(http.MethodPut, "/api/v1/deploys/"+itoa(dt.ID), nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT /{id} should return 405, got %d", w.Code)
	}
}

// =============================================================================
// handleFederation HTTP 路由补充覆盖
// =============================================================================

func TestHandleFederation_Deploys_GET_List(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	disp := newFakeDisp()
	h := NewHandler(st, disp)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// 创建一个联邦计划。
	plan := newFederationPlan("fed1", "t1")
	_, err := h.Federation().Store().Create(ctx, plan)
	if err != nil {
		t.Fatalf("create federation: %v", err)
	}

	// GET 列表。
	req := httptest.NewRequest(http.MethodGet, "/api/v1/deploys/federation", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET federation list should return 200, got %d", w.Code)
	}
}

func TestHandleFederation_Deploys_MethodNotAllowed(t *testing.T) {
	st := NewMemory()
	disp := newFakeDisp()
	h := NewHandler(st, disp)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/deploys/federation", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE federation should return 405, got %d", w.Code)
	}
}

func TestHandleFederation_Deploys_BadJSON(t *testing.T) {
	st := NewMemory()
	disp := newFakeDisp()
	h := NewHandler(st, disp)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/deploys/federation", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON should return 400, got %d", w.Code)
	}
}

func TestHandleFederation_Deploys_InvalidPlan(t *testing.T) {
	st := NewMemory()
	disp := newFakeDisp()
	h := NewHandler(st, disp)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// 缺 name 字段。
	body := `{"name":"","mode":"sequential","members":[{"cluster_id":"a","target_ids":"d1","order":0,"weight":100}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/deploys/federation", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid plan should return 400, got %d", w.Code)
	}
}

func TestHandleFederation_DeployByID_GET(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	disp := newFakeDisp()
	h := NewHandler(st, disp)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	plan := newFederationPlan("fed1", "t1")
	created, err := h.Federation().Store().Create(ctx, plan)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/deploys/federation/"+itoa(created.ID), nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET should return 200, got %d", w.Code)
	}
}

func TestHandleFederation_DeployByID_NotFound(t *testing.T) {
	st := NewMemory()
	disp := newFakeDisp()
	h := NewHandler(st, disp)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/deploys/federation/9999", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GET non-existent should return 404, got %d", w.Code)
	}
}

func TestHandleFederation_DeployByID_InvalidID(t *testing.T) {
	st := NewMemory()
	disp := newFakeDisp()
	h := NewHandler(st, disp)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/deploys/federation/abc", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid id should return 400, got %d", w.Code)
	}
}

func TestHandleFederation_DeployByID_Status(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	disp := newFakeDisp()
	h := NewHandler(st, disp)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	plan := newFederationPlan("fed1", "t1")
	created, err := h.Federation().Store().Create(ctx, plan)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/deploys/federation/"+itoa(created.ID)+"/status", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET status should return 200, got %d", w.Code)
	}
}

func TestHandleFederation_DeployByID_StatusNotFound(t *testing.T) {
	st := NewMemory()
	disp := newFakeDisp()
	h := NewHandler(st, disp)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/deploys/federation/9999/status", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GET status non-existent should return 404, got %d", w.Code)
	}
}

func TestHandleFederation_DeployByID_UnknownAction(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	disp := newFakeDisp()
	h := NewHandler(st, disp)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	plan := newFederationPlan("fed1", "t1")
	created, _ := h.Federation().Store().Create(ctx, plan)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/deploys/federation/"+itoa(created.ID)+"/bogus", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown action should return 404, got %d", w.Code)
	}
}

func TestHandleFederation_DeployByID_DefaultMethodNotAllowed(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	disp := newFakeDisp()
	h := NewHandler(st, disp)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	plan := newFederationPlan("fed1", "t1")
	created, _ := h.Federation().Store().Create(ctx, plan)

	// PUT /{id} 默认分支应 405。
	req := httptest.NewRequest(http.MethodPut, "/api/v1/deploys/federation/"+itoa(created.ID), nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT /{id} should return 405, got %d", w.Code)
	}
}

func TestHandleFederation_DeployByID_StatusMethodNotAllowed(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	disp := newFakeDisp()
	h := NewHandler(st, disp)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	plan := newFederationPlan("fed1", "t1")
	created, _ := h.Federation().Store().Create(ctx, plan)

	// POST /status 应 405。
	req := httptest.NewRequest(http.MethodPost, "/api/v1/deploys/federation/"+itoa(created.ID)+"/status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /status should return 405, got %d", w.Code)
	}
}

// =============================================================================
// AdvanceCanary
// =============================================================================

func TestAdvanceCanary_Success(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	disp.devices["dev-2"] = &proto.DeviceInfo{DeviceID: "dev-2", AgentID: "a2"}
	disp.devices["dev-3"] = &proto.DeviceInfo{DeviceID: "dev-3", AgentID: "a3"}
	disp.devices["dev-4"] = &proto.DeviceInfo{DeviceID: "dev-4", AgentID: "a4"}
	disp.devices["dev-5"] = &proto.DeviceInfo{DeviceID: "dev-5", AgentID: "a5"}
	h := NewHandler(st, disp)

	dt, _ := st.Create(ctx, &DeployTask{
		Name: "svc", Type: TypeScript, TargetIDs: "dev-1,dev-2,dev-3,dev-4,dev-5", TenantID: "t1", CreatedBy: "t",
		Strategy: StrategyCanary, CanaryWeight: 20,
	})
	_ = h.Execute(ctx, dt.ID, "t1")

	// 推进到 60%。
	if err := h.AdvanceCanary(ctx, dt.ID, "t1", 60); err != nil {
		t.Fatalf("advance canary: %v", err)
	}
	got, _ := st.Get(ctx, dt.ID, "t1")
	if got.CanaryWeight != 60 || got.Status != StatusCanary {
		t.Fatalf("expected weight=60 status=canary, got weight=%d status=%s", got.CanaryWeight, got.Status)
	}
}

func TestAdvanceCanary_NotCanaryStrategy(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	h := NewHandler(st, disp)

	dt, _ := st.Create(ctx, newDeploy("svc", TypeScript, "dev-1", "t1"))
	_ = h.Execute(ctx, dt.ID, "t1")
	if err := h.AdvanceCanary(ctx, dt.ID, "t1", 50); err == nil {
		t.Fatal("advance on rolling should fail")
	}
}

func TestAdvanceCanary_WrongStatus(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	disp := newFakeDisp()
	h := NewHandler(st, disp)

	// created 状态不可推进。
	dt, _ := st.Create(ctx, &DeployTask{
		Name: "svc", Type: TypeScript, TargetIDs: "dev-1", TenantID: "t1", CreatedBy: "t",
		Strategy: StrategyCanary, CanaryWeight: 20,
	})
	if err := h.AdvanceCanary(ctx, dt.ID, "t1", 50); err == nil {
		t.Fatal("advance from created should fail")
	}
}

func TestAdvanceCanary_InvalidWeight(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	h := NewHandler(st, disp)

	dt, _ := st.Create(ctx, &DeployTask{
		Name: "svc", Type: TypeScript, TargetIDs: "dev-1", TenantID: "t1", CreatedBy: "t",
		Strategy: StrategyCanary, CanaryWeight: 20,
	})
	_ = h.Execute(ctx, dt.ID, "t1")

	// weight <= 0。
	if err := h.AdvanceCanary(ctx, dt.ID, "t1", 0); err == nil {
		t.Fatal("weight=0 should fail")
	}
	// weight > 100。
	if err := h.AdvanceCanary(ctx, dt.ID, "t1", 101); err == nil {
		t.Fatal("weight=101 should fail")
	}
	// weight <= current。
	if err := h.AdvanceCanary(ctx, dt.ID, "t1", 10); err == nil {
		t.Fatal("weight<=current should fail")
	}
}

func TestAdvanceCanary_NotFound(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	disp := newFakeDisp()
	h := NewHandler(st, disp)

	if err := h.AdvanceCanary(ctx, 9999, "t1", 50); err == nil {
		t.Fatal("advance non-existent should fail")
	}
}

// =============================================================================
// PromoteMember / RollbackMember / MemberStatus
// =============================================================================

func TestHandler_PromoteMember(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	h := NewHandler(st, disp)

	dt, _ := st.Create(ctx, &DeployTask{
		Name: "svc", Type: TypeScript, TargetIDs: "dev-1", TenantID: "t1", CreatedBy: "t",
		Strategy: StrategyBlueGreen,
	})
	_ = h.Execute(ctx, dt.ID, "t1")
	if err := h.PromoteMember(ctx, dt.ID, "t1"); err != nil {
		t.Fatalf("promote member: %v", err)
	}
}

func TestHandler_MemberStatus(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	h := NewHandler(st, disp)

	dt, _ := st.Create(ctx, newDeploy("svc", TypeScript, "dev-1", "t1"))
	_ = h.Execute(ctx, dt.ID, "t1")
	st2, err := h.MemberStatus(ctx, dt.ID, "t1")
	if err != nil {
		t.Fatalf("member status: %v", err)
	}
	if st2 != StatusRunning {
		t.Fatalf("expected running, got %s", st2)
	}
	// 不存在。
	if _, err := h.MemberStatus(ctx, 9999, "t1"); err == nil {
		t.Fatal("member status non-existent should fail")
	}
}

func TestHandler_RollbackMember(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	h := NewHandler(st, disp)

	dt, _ := st.Create(ctx, newDeploy("svc", TypeScript, "dev-1", "t1"))
	_ = h.Execute(ctx, dt.ID, "t1")
	if err := h.RollbackMember(ctx, dt.ID, "t1"); err != nil {
		t.Fatalf("rollback member: %v", err)
	}
}

// =============================================================================
// CreateAndExecute 错误路径
// =============================================================================

func TestHandler_CreateAndExecute_NilTemplate(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	disp := newFakeDisp()
	h := NewHandler(st, disp)

	if _, err := h.CreateAndExecute(ctx, nil, "dev-1", "t1"); err == nil {
		t.Fatal("nil template should fail")
	}
}

func TestHandler_CreateAndExecute_InvalidTemplate(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	h := NewHandler(st, disp)

	// 模板缺 name（无效）。
	tpl := &DeployTask{Type: TypeScript}
	if _, err := h.CreateAndExecute(ctx, tpl, "dev-1", "t1"); err == nil {
		t.Fatal("invalid template should fail")
	}
}

// =============================================================================
// AutoAdvanceManager.ServeHTTP
// =============================================================================

func TestAutoAdvanceManager_ServeHTTP_Status(t *testing.T) {
	ctx := context.Background()
	deploys := NewMemory()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	disp.devices["dev-2"] = &proto.DeviceInfo{DeviceID: "dev-2", AgentID: "a2"}
	disp.devices["dev-3"] = &proto.DeviceInfo{DeviceID: "dev-3", AgentID: "a3"}
	disp.devices["dev-4"] = &proto.DeviceInfo{DeviceID: "dev-4", AgentID: "a4"}
	disp.devices["dev-5"] = &proto.DeviceInfo{DeviceID: "dev-5", AgentID: "a5"}
	tr := newFakeTaskResults()
	deployID := setupCanaryDeploy(t, deploys, disp, tr, 20)

	m, _, _, _ := newAutoAdvanceManagerForTest(t, deploys, tr)

	// GET status（未启动）。
	req := httptest.NewRequest(http.MethodGet, "/auto-advance/status", nil)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, req, deployID, "t1", "auto-advance/status")
	if w.Code != http.StatusOK {
		t.Fatalf("GET status should return 200, got %d", w.Code)
	}

	// 启动监控（异步）。
	monCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go m.Monitor(monCtx, deployID)
	// 等待注册。
	waitForMonitor := func() bool {
		for i := 0; i < 50; i++ {
			if m.Status(deployID).Running {
				return true
			}
			sleepMs(10)
		}
		return false
	}
	if !waitForMonitor() {
		t.Fatal("monitor did not start")
	}

	// GET status（已启动）。
	req = httptest.NewRequest(http.MethodGet, "/auto-advance/status", nil)
	w = httptest.NewRecorder()
	m.ServeHTTP(w, req, deployID, "t1", "auto-advance/status")
	if w.Code != http.StatusOK {
		t.Fatalf("GET status (running) should return 200, got %d", w.Code)
	}

	// DELETE 停止。
	req = httptest.NewRequest(http.MethodDelete, "/auto-advance", nil)
	w = httptest.NewRecorder()
	m.ServeHTTP(w, req, deployID, "t1", "auto-advance")
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE should return 200, got %d", w.Code)
	}

	// 再次 DELETE（未运行）应 404。
	req = httptest.NewRequest(http.MethodDelete, "/auto-advance", nil)
	w = httptest.NewRecorder()
	m.ServeHTTP(w, req, deployID, "t1", "auto-advance")
	if w.Code != http.StatusNotFound {
		t.Fatalf("DELETE not running should return 404, got %d", w.Code)
	}
}

func TestAutoAdvanceManager_ServeHTTP_Start(t *testing.T) {
	deploys := NewMemory()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	disp.devices["dev-2"] = &proto.DeviceInfo{DeviceID: "dev-2", AgentID: "a2"}
	disp.devices["dev-3"] = &proto.DeviceInfo{DeviceID: "dev-3", AgentID: "a3"}
	disp.devices["dev-4"] = &proto.DeviceInfo{DeviceID: "dev-4", AgentID: "a4"}
	disp.devices["dev-5"] = &proto.DeviceInfo{DeviceID: "dev-5", AgentID: "a5"}
	tr := newFakeTaskResults()
	deployID := setupCanaryDeploy(t, deploys, disp, tr, 20)

	m, _, _, _ := newAutoAdvanceManagerForTest(t, deploys, tr)

	// POST 启动。
	req := httptest.NewRequest(http.MethodPost, "/auto-advance", nil)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, req, deployID, "t1", "auto-advance")
	if w.Code != http.StatusAccepted {
		t.Fatalf("POST start should return 202, got %d", w.Code)
	}
	// 停止以避免泄漏。
	sleepMs(150)
	m.Stop(deployID)
}

func TestAutoAdvanceManager_ServeHTTP_MethodNotAllowed(t *testing.T) {
	deploys := NewMemory()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	disp.devices["dev-2"] = &proto.DeviceInfo{DeviceID: "dev-2", AgentID: "a2"}
	disp.devices["dev-3"] = &proto.DeviceInfo{DeviceID: "dev-3", AgentID: "a3"}
	disp.devices["dev-4"] = &proto.DeviceInfo{DeviceID: "dev-4", AgentID: "a4"}
	disp.devices["dev-5"] = &proto.DeviceInfo{DeviceID: "dev-5", AgentID: "a5"}
	tr := newFakeTaskResults()
	deployID := setupCanaryDeploy(t, deploys, disp, tr, 20)

	m, _, _, _ := newAutoAdvanceManagerForTest(t, deploys, tr)

	// PUT /auto-advance 应 405。
	req := httptest.NewRequest(http.MethodPut, "/auto-advance", nil)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, req, deployID, "t1", "auto-advance")
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT should return 405, got %d", w.Code)
	}

	// POST /auto-advance/status 应 405。
	req = httptest.NewRequest(http.MethodPost, "/auto-advance/status", nil)
	w = httptest.NewRecorder()
	m.ServeHTTP(w, req, deployID, "t1", "auto-advance/status")
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /status should return 405, got %d", w.Code)
	}
}

func TestAutoAdvanceManager_ServeHTTP_UnknownSub(t *testing.T) {
	deploys := NewMemory()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	disp.devices["dev-2"] = &proto.DeviceInfo{DeviceID: "dev-2", AgentID: "a2"}
	disp.devices["dev-3"] = &proto.DeviceInfo{DeviceID: "dev-3", AgentID: "a3"}
	disp.devices["dev-4"] = &proto.DeviceInfo{DeviceID: "dev-4", AgentID: "a4"}
	disp.devices["dev-5"] = &proto.DeviceInfo{DeviceID: "dev-5", AgentID: "a5"}
	tr := newFakeTaskResults()
	deployID := setupCanaryDeploy(t, deploys, disp, tr, 20)

	m, _, _, _ := newAutoAdvanceManagerForTest(t, deploys, tr)

	req := httptest.NewRequest(http.MethodGet, "/bogus", nil)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, req, deployID, "t1", "bogus")
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown sub should return 404, got %d", w.Code)
	}
}

// =============================================================================
// isTerminalStatus / recordError / recordAction
// =============================================================================

func TestIsTerminalStatus(t *testing.T) {
	// 终态。
	for _, s := range []string{StatusSuccess, StatusFailed, StatusRolledBack} {
		if !isTerminalStatus(s) {
			t.Fatalf("status %s should be terminal", s)
		}
	}
	// 非终态。
	for _, s := range []string{StatusCreated, StatusRunning, StatusCanary, StatusPromoting, StatusGated, ""} {
		if isTerminalStatus(s) {
			t.Fatalf("status %s should not be terminal", s)
		}
	}
}

func TestAutoAdvanceManager_recordError(t *testing.T) {
	deploys := NewMemory()
	m, _, _, _ := newAutoAdvanceManagerForTest(t, deploys, newFakeTaskResults())
	st := &monitorState{}
	m.recordError(st, errors.New("boom"))
	if st.lastAction != "error" || st.lastError != "boom" {
		t.Fatalf("recordError should set action/error, got action=%s err=%s", st.lastAction, st.lastError)
	}
	if st.lastCheck.IsZero() {
		t.Fatal("lastCheck should be set")
	}
}

func TestAutoAdvanceManager_recordAction(t *testing.T) {
	deploys := NewMemory()
	m, _, _, _ := newAutoAdvanceManagerForTest(t, deploys, newFakeTaskResults())
	st := &monitorState{}
	gate := &GateResult{Passed: true}
	m.recordAction(st, "advance", gate)
	if st.lastAction != "advance" || st.lastGate != gate || st.lastError != "" {
		t.Fatalf("recordAction should set action/gate/clear error, got action=%s", st.lastAction)
	}
}

// =============================================================================
// AutoAdvanceManager.checkAndAdvance 错误路径
// =============================================================================

func TestAutoAdvanceManager_CheckAndAdvance_EvaluateGateError(t *testing.T) {
	deploys := NewMemory()
	m, _, _, _ := newAutoAdvanceManagerForTest(t, deploys, newFakeTaskResults())
	st := &monitorState{}
	// 不存在的部署 ID，evaluateGate 会返回错误。
	if err := m.checkAndAdvance(context.Background(), 9999, st); err == nil {
		t.Fatal("checkAndAdvance on non-existent should fail")
	}
	if st.lastAction != "error" {
		t.Fatalf("should record error, got %s", st.lastAction)
	}
}

func TestAutoAdvanceManager_CheckAndAdvance_RollbackNil(t *testing.T) {
	ctx := context.Background()
	deploys := NewMemory()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	disp.devices["dev-2"] = &proto.DeviceInfo{DeviceID: "dev-2", AgentID: "a2"}
	disp.devices["dev-3"] = &proto.DeviceInfo{DeviceID: "dev-3", AgentID: "a3"}
	disp.devices["dev-4"] = &proto.DeviceInfo{DeviceID: "dev-4", AgentID: "a4"}
	disp.devices["dev-5"] = &proto.DeviceInfo{DeviceID: "dev-5", AgentID: "a5"}
	tr := newFakeTaskResults()
	deployID := setupCanaryDeploy(t, deploys, disp, tr, 40)

	// 构造一个 rollback=nil 的 manager（仅评估不动作）。
	cfg := AutoAdvanceConfig{
		Enabled:              true,
		CheckInterval:        10 * time.Millisecond,
		FailureRateThreshold: 0.05,
		LatencyThreshold:     500 * time.Millisecond,
		MinSampleSize:        2,
		AdvanceRatio:         0.2,
		MaxRatio:             1.0,
	}
	m := NewAutoAdvanceManager(cfg, deploys, tr, nil, nil, nil)

	// 全部失败 -> gate 不通过 -> rollback_skipped（rollback=nil）。
	got, _ := deploys.Get(ctx, deployID, "t1")
	for _, tid := range SplitIDs(got.TaskIDs) {
		tr.setResult(tid, 1, 100)
	}
	st := &monitorState{}
	if err := m.checkAndAdvance(ctx, deployID, st); err != nil {
		t.Fatalf("checkAndAdvance: %v", err)
	}
	if st.lastAction != "rollback_skipped" {
		t.Fatalf("expected rollback_skipped, got %s", st.lastAction)
	}
}

func TestAutoAdvanceManager_CheckAndAdvance_PromoteNil(t *testing.T) {
	ctx := context.Background()
	deploys := NewMemory()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	disp.devices["dev-2"] = &proto.DeviceInfo{DeviceID: "dev-2", AgentID: "a2"}
	disp.devices["dev-3"] = &proto.DeviceInfo{DeviceID: "dev-3", AgentID: "a3"}
	disp.devices["dev-4"] = &proto.DeviceInfo{DeviceID: "dev-4", AgentID: "a4"}
	disp.devices["dev-5"] = &proto.DeviceInfo{DeviceID: "dev-5", AgentID: "a5"}
	tr := newFakeTaskResults()
	deployID := setupCanaryDeploy(t, deploys, disp, tr, 90)

	cfg := AutoAdvanceConfig{
		Enabled:              true,
		CheckInterval:        10 * time.Millisecond,
		FailureRateThreshold: 0.05,
		LatencyThreshold:     500 * time.Millisecond,
		MinSampleSize:        2,
		AdvanceRatio:         0.2,
		MaxRatio:             1.0,
	}
	m := NewAutoAdvanceManager(cfg, deploys, tr, nil, nil, nil)

	// 全部成功，90 + 20 >= 100 -> promote_skipped（promote=nil）。
	got, _ := deploys.Get(ctx, deployID, "t1")
	for _, tid := range SplitIDs(got.TaskIDs) {
		tr.setResult(tid, 0, 100)
	}
	st := &monitorState{}
	if err := m.checkAndAdvance(ctx, deployID, st); err != nil {
		t.Fatalf("checkAndAdvance: %v", err)
	}
	if st.lastAction != "promote_skipped" {
		t.Fatalf("expected promote_skipped, got %s", st.lastAction)
	}
}

func TestAutoAdvanceManager_CheckAndAdvance_AdvanceNil(t *testing.T) {
	ctx := context.Background()
	deploys := NewMemory()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	disp.devices["dev-2"] = &proto.DeviceInfo{DeviceID: "dev-2", AgentID: "a2"}
	disp.devices["dev-3"] = &proto.DeviceInfo{DeviceID: "dev-3", AgentID: "a3"}
	disp.devices["dev-4"] = &proto.DeviceInfo{DeviceID: "dev-4", AgentID: "a4"}
	disp.devices["dev-5"] = &proto.DeviceInfo{DeviceID: "dev-5", AgentID: "a5"}
	tr := newFakeTaskResults()
	deployID := setupCanaryDeploy(t, deploys, disp, tr, 40)

	cfg := AutoAdvanceConfig{
		Enabled:              true,
		CheckInterval:        10 * time.Millisecond,
		FailureRateThreshold: 0.05,
		LatencyThreshold:     500 * time.Millisecond,
		MinSampleSize:        2,
		AdvanceRatio:         0.2,
		MaxRatio:             1.0,
	}
	m := NewAutoAdvanceManager(cfg, deploys, tr, nil, nil, nil)

	// 全部成功，40 + 20 = 60 < 100 -> advance_skipped（advance=nil）。
	got, _ := deploys.Get(ctx, deployID, "t1")
	for _, tid := range SplitIDs(got.TaskIDs) {
		tr.setResult(tid, 0, 100)
	}
	st := &monitorState{}
	if err := m.checkAndAdvance(ctx, deployID, st); err != nil {
		t.Fatalf("checkAndAdvance: %v", err)
	}
	if st.lastAction != "advance_skipped" {
		t.Fatalf("expected advance_skipped, got %s", st.lastAction)
	}
}

func TestAutoAdvanceManager_Monitor_NotFound(t *testing.T) {
	deploys := NewMemory()
	m, _, _, _ := newAutoAdvanceManagerForTest(t, deploys, newFakeTaskResults())
	if err := m.Monitor(context.Background(), 9999); err == nil {
		t.Fatal("Monitor non-existent should fail")
	}
}

func TestAutoAdvanceManager_Monitor_NotCanaryStrategy(t *testing.T) {
	ctx := context.Background()
	deploys := NewMemory()
	dt, _ := deploys.Create(ctx, &DeployTask{
		Name: "svc", Type: TypeScript, TargetIDs: "dev-1", TenantID: "t1", CreatedBy: "t",
		Strategy: StrategyRolling,
	})
	// 直接置 running 状态（绕过 Execute 以避免 dispatcher）。
	got, _ := deploys.Get(ctx, dt.ID, "t1")
	got.Status = StatusRunning
	_ = deploys.Update(ctx, got)

	m, _, _, _ := newAutoAdvanceManagerForTest(t, deploys, newFakeTaskResults())
	if err := m.Monitor(context.Background(), dt.ID); err == nil {
		t.Fatal("Monitor on non-canary strategy should fail")
	}
}

func TestAutoAdvanceManager_Monitor_NotCanaryStatus(t *testing.T) {
	ctx := context.Background()
	deploys := NewMemory()
	// 创建 canary 部署但状态为 created。
	dt, _ := deploys.Create(ctx, &DeployTask{
		Name: "svc", Type: TypeScript, TargetIDs: "dev-1", TenantID: "t1", CreatedBy: "t",
		Strategy: StrategyCanary, CanaryWeight: 20,
	})
	m, _, _, _ := newAutoAdvanceManagerForTest(t, deploys, newFakeTaskResults())
	if err := m.Monitor(context.Background(), dt.ID); err == nil {
		t.Fatal("Monitor on non-canary status should fail")
	}
}

func TestAutoAdvanceManager_Status_NotRunning(t *testing.T) {
	deploys := NewMemory()
	m, _, _, _ := newAutoAdvanceManagerForTest(t, deploys, newFakeTaskResults())
	st := m.Status(9999)
	if st.Running {
		t.Fatal("Status on non-monitored should return Running=false")
	}
}

// =============================================================================
// Federation: setMemberError / Start / Promote / Reconcile / Rollback 错误路径
// =============================================================================

func TestFederation_Start_NotPending(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryFederationStore()
	exec := newFakeExec()
	c := NewFederationCoordinator(store, exec)

	plan := newFederationPlan("fed1", "t1")
	created, _ := store.Create(ctx, plan)
	// 改状态为非 pending。
	created.Status = FedStatusRunning
	_ = store.Update(ctx, created)

	if err := c.Start(ctx, created.ID, "t1"); err == nil {
		t.Fatal("Start on non-pending should fail")
	}
}

func TestFederation_Start_NoMembers(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryFederationStore()
	exec := newFakeExec()
	c := NewFederationCoordinator(store, exec)

	// 创建一个有成员但 firstBatch 为空的情况：构造所有成员 Order<0 不可能，
	// 改为直接构造一个无效计划：手动置 Status=pending 且 Members 为空。
	// 但 Valid() 要求 Members 非空，故通过 Create 后清空 Members 模拟。
	plan := newFederationPlan("fed1", "t1")
	created, _ := store.Create(ctx, plan)
	created.Members = nil
	created.Status = FedStatusPending
	_ = store.Update(ctx, created)

	if err := c.Start(ctx, created.ID, "t1"); err != nil {
		t.Fatalf("Start with no members should not error: %v", err)
	}
	// 应进入 fed_failed。
	got, _ := store.Get(ctx, created.ID, "t1")
	if got.Status != FedStatusFailed {
		t.Fatalf("expected fed_failed, got %s", got.Status)
	}
}

func TestFederation_Start_ExecutorNil(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryFederationStore()
	c := NewFederationCoordinator(store, nil) // exec=nil

	plan := newFederationPlan("fed1", "t1")
	created, _ := store.Create(ctx, plan)

	if err := c.Start(ctx, created.ID, "t1"); err == nil {
		t.Fatal("Start with nil executor should fail")
	}
}

func TestFederation_Start_CreateError(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryFederationStore()
	exec := newFakeExec()
	exec.createErr = errors.New("dispatch failed")
	c := NewFederationCoordinator(store, exec)

	plan := newFederationPlan("fed1", "t1")
	created, _ := store.Create(ctx, plan)

	if err := c.Start(ctx, created.ID, "t1"); err != nil {
		t.Fatalf("Start should tolerate member dispatch error: %v", err)
	}
	// 应记录成员错误（setMemberError 路径覆盖）。
	got, _ := store.Get(ctx, created.ID, "t1")
	if got.Members[0].Status != StatusFailed {
		t.Fatalf("expected member failed, got %s", got.Members[0].Status)
	}
}

func TestFederation_Promote_NotCanaryGated(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryFederationStore()
	exec := newFakeExec()
	c := NewFederationCoordinator(store, exec)

	plan := newFederationPlan("fed1", "t1")
	created, _ := store.Create(ctx, plan)
	// 状态为 pending，不可 promote。
	if err := c.Promote(ctx, created.ID, "t1"); err == nil {
		t.Fatal("Promote on pending should fail")
	}
}

func TestFederation_Promote_ExecutorNil(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryFederationStore()
	c := NewFederationCoordinator(store, nil)

	plan := newFederationPlan("fed1", "t1")
	created, _ := store.Create(ctx, plan)
	created.Status = FedStatusCanary
	_ = store.Update(ctx, created)

	if err := c.Promote(ctx, created.ID, "t1"); err == nil {
		t.Fatal("Promote with nil executor should fail")
	}
}

func TestFederation_Promote_GateNotPassed(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryFederationStore()
	exec := newFakeExec()
	c := NewFederationCoordinator(store, exec)

	plan := newFederationPlan("fed1", "t1")
	created, _ := store.Create(ctx, plan)
	// 模拟首批已派发但成员仍在 running（门禁不通过）。
	created.Status = FedStatusCanary
	created.Members[0].DeployID = 1
	created.Members[0].Status = StatusRunning
	_ = store.Update(ctx, created)

	if err := c.Promote(ctx, created.ID, "t1"); err == nil {
		t.Fatal("Promote with gate not passed should fail")
	}
}

func TestFederation_Promote_Parallel(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryFederationStore()
	exec := newFakeExec()
	c := NewFederationCoordinator(store, exec)

	// parallel 模式：promote 即全量晋级。
	plan := &FederationDeploy{
		Name:     "fed1",
		TenantID: "t1",
		Mode:     FedModeParallel,
		Template: DeployTask{Name: "svc", Type: TypeScript},
		Members: []FederationMember{
			{ClusterID: "a", Name: "A", TargetIDs: "d1", Order: 0, Weight: 100},
			{ClusterID: "b", Name: "B", TargetIDs: "d2", Order: 0, Weight: 100},
		},
	}
	created, _ := store.Create(ctx, plan)
	_ = c.Start(ctx, created.ID, "t1")
	// 模拟成员子部署进入 success（门禁通过）。
	got, _ := store.Get(ctx, created.ID, "t1")
	for i := range got.Members {
		got.Members[i].Status = StatusSuccess
		exec.setStatus(got.Members[i].DeployID, StatusSuccess)
	}
	_ = store.Update(ctx, got)
	// promote 前置回 canary 以便 Promote 接受（parallel 走 PromoteMember 把 success 再晋级）。
	// 但 Promote 要求状态为 canary/gated；Start 后 recomputeStatus 会因成员 success 而置 fed_success。
	// 改为成员 canary 状态，让门禁通过需成员终态——矛盾。改用 gated：成员内部门禁通过可晋级。
	got2, _ := store.Get(ctx, created.ID, "t1")
	got2.Status = FedStatusCanary
	for i := range got2.Members {
		got2.Members[i].Status = StatusGated
		exec.setStatus(got2.Members[i].DeployID, StatusGated)
	}
	_ = store.Update(ctx, got2)

	if err := c.Promote(ctx, created.ID, "t1"); err != nil {
		t.Fatalf("Promote parallel: %v", err)
	}
}

func TestFederation_Promote_SequentialNoNextBatch(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryFederationStore()
	exec := newFakeExec()
	c := NewFederationCoordinator(store, exec)

	// 单成员 sequential：promote 时无后续批次，直接 fed_success。
	plan := &FederationDeploy{
		Name:     "fed1",
		TenantID: "t1",
		Mode:     FedModeSequential,
		Template: DeployTask{Name: "svc", Type: TypeScript},
		Members: []FederationMember{
			{ClusterID: "a", Name: "A", TargetIDs: "d1", Order: 0, Weight: 100},
		},
	}
	created, _ := store.Create(ctx, plan)
	_ = c.Start(ctx, created.ID, "t1")
	// 模拟成员子部署 success。
	got, _ := store.Get(ctx, created.ID, "t1")
	got.Members[0].Status = StatusSuccess
	exec.setStatus(got.Members[0].DeployID, StatusSuccess)
	got.Status = FedStatusCanary
	_ = store.Update(ctx, got)

	if err := c.Promote(ctx, created.ID, "t1"); err != nil {
		t.Fatalf("Promote sequential no next: %v", err)
	}
	final, _ := store.Get(ctx, created.ID, "t1")
	if final.Status != FedStatusSuccess {
		t.Fatalf("expected fed_success, got %s", final.Status)
	}
}

func TestFederation_Reconcile_TerminalStatus(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryFederationStore()
	exec := newFakeExec()
	c := NewFederationCoordinator(store, exec)

	plan := newFederationPlan("fed1", "t1")
	created, _ := store.Create(ctx, plan)
	// 终态不应被对账。
	created.Status = FedStatusSuccess
	_ = store.Update(ctx, created)

	if err := c.Reconcile(ctx, created.ID, "t1"); err != nil {
		t.Fatalf("Reconcile on terminal should be no-op: %v", err)
	}
}

func TestFederation_Rollback_NotAllowed(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryFederationStore()
	exec := newFakeExec()
	c := NewFederationCoordinator(store, exec)

	plan := newFederationPlan("fed1", "t1")
	created, _ := store.Create(ctx, plan)
	// pending 状态不可回滚。
	if err := c.Rollback(ctx, created.ID, "t1"); err == nil {
		t.Fatal("Rollback on pending should fail")
	}
}

func TestFederation_Rollback_Success(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryFederationStore()
	exec := newFakeExec()
	c := NewFederationCoordinator(store, exec)

	plan := newFederationPlan("fed1", "t1")
	created, _ := store.Create(ctx, plan)
	_ = c.Start(ctx, created.ID, "t1")
	// 进入 canary 后可回滚。
	if err := c.Rollback(ctx, created.ID, "t1"); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	got, _ := store.Get(ctx, created.ID, "t1")
	if got.Status != FedStatusRolledBack {
		t.Fatalf("expected fed_rolledback, got %s", got.Status)
	}
}

func TestFederation_Status_NotFound(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryFederationStore()
	exec := newFakeExec()
	c := NewFederationCoordinator(store, exec)

	if _, err := c.Status(ctx, 9999, "t1"); err == nil {
		t.Fatal("Status non-existent should fail")
	}
}

func TestFederation_Update_NotFound(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryFederationStore()
	f := &FederationDeploy{ID: 9999, TenantID: "t1"}
	if err := store.Update(ctx, f); err != ErrFedNotFound {
		t.Fatalf("expected ErrFedNotFound, got %v", err)
	}
}

func TestFederation_Update_TenantMismatch(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryFederationStore()
	plan := newFederationPlan("fed1", "t1")
	created, _ := store.Create(ctx, plan)
	// 用 Get 取独立副本，越权改租户后 Update。
	cp, _ := store.Get(ctx, created.ID, "")
	cp.TenantID = "t2"
	if err := store.Update(ctx, cp); err != ErrFedTenantMismatch {
		t.Fatalf("expected ErrFedTenantMismatch, got %v", err)
	}
}

func TestFederation_Delete_NotFound(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryFederationStore()
	if err := store.Delete(ctx, 9999, "t1"); err != ErrFedNotFound {
		t.Fatalf("expected ErrFedNotFound, got %v", err)
	}
}

func TestFederation_Delete_TenantMismatch(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryFederationStore()
	plan := newFederationPlan("fed1", "t1")
	created, _ := store.Create(ctx, plan)
	if err := store.Delete(ctx, created.ID, "t2"); err != ErrFedTenantMismatch {
		t.Fatalf("expected ErrFedTenantMismatch, got %v", err)
	}
}

func TestFederation_Delete_Success(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryFederationStore()
	plan := newFederationPlan("fed1", "t1")
	created, _ := store.Create(ctx, plan)
	if err := store.Delete(ctx, created.ID, "t1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.Get(ctx, created.ID, "t1"); err != ErrFedNotFound {
		t.Fatalf("expected ErrFedNotFound after delete, got %v", err)
	}
}

func TestFederation_Get_TenantMismatch(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryFederationStore()
	plan := newFederationPlan("fed1", "t1")
	created, _ := store.Create(ctx, plan)
	if _, err := store.Get(ctx, created.ID, "t2"); err != ErrFedTenantMismatch {
		t.Fatalf("expected ErrFedTenantMismatch, got %v", err)
	}
}

func TestFederation_Create_NilAndEmptyTenant(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryFederationStore()
	if _, err := store.Create(ctx, nil); err == nil {
		t.Fatal("nil create should fail")
	}
	if _, err := store.Create(ctx, &FederationDeploy{Name: "x"}); err == nil {
		t.Fatal("empty tenant create should fail")
	}
}

func TestFederation_Update_Nil(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryFederationStore()
	if err := store.Update(ctx, nil); err == nil {
		t.Fatal("nil update should fail")
	}
}

// =============================================================================
// FederationDeploy.Valid 边界
// =============================================================================

func TestFederationDeploy_Valid_Boundaries(t *testing.T) {
	// 缺 name。
	f1 := &FederationDeploy{Mode: FedModeSequential, Members: []FederationMember{{ClusterID: "a", TargetIDs: "d1", Order: 0, Weight: 100}}}
	if err := f1.Valid(); err == nil {
		t.Fatal("missing name should fail")
	}
	// 非法 mode。
	f2 := &FederationDeploy{Name: "x", Mode: "bogus", Members: []FederationMember{{ClusterID: "a", TargetIDs: "d1", Order: 0, Weight: 100}}}
	if err := f2.Valid(); err == nil {
		t.Fatal("invalid mode should fail")
	}
	// 空成员。
	f3 := &FederationDeploy{Name: "x", Mode: FedModeSequential, Members: nil}
	if err := f3.Valid(); err == nil {
		t.Fatal("empty members should fail")
	}
	// 成员缺 cluster_id。
	f4 := &FederationDeploy{Name: "x", Mode: FedModeSequential, Members: []FederationMember{{TargetIDs: "d1", Order: 0, Weight: 100}}}
	if err := f4.Valid(); err == nil {
		t.Fatal("missing cluster_id should fail")
	}
	// 重复 cluster_id。
	f5 := &FederationDeploy{Name: "x", Mode: FedModeSequential, Members: []FederationMember{
		{ClusterID: "a", TargetIDs: "d1", Order: 0, Weight: 100},
		{ClusterID: "a", TargetIDs: "d2", Order: 1, Weight: 100},
	}}
	if err := f5.Valid(); err == nil {
		t.Fatal("duplicate cluster_id should fail")
	}
	// 成员缺 target_ids。
	f6 := &FederationDeploy{Name: "x", Mode: FedModeSequential, Members: []FederationMember{{ClusterID: "a", Order: 0, Weight: 100}}}
	if err := f6.Valid(); err == nil {
		t.Fatal("missing target_ids should fail")
	}
	// 成员 Order 越界。
	f7 := &FederationDeploy{Name: "x", Mode: FedModeSequential, Members: []FederationMember{{ClusterID: "a", TargetIDs: "d1", Order: -1, Weight: 100}}}
	if err := f7.Valid(); err == nil {
		t.Fatal("order < 0 should fail")
	}
	// 成员 Weight 越界。
	f8 := &FederationDeploy{Name: "x", Mode: FedModeSequential, Members: []FederationMember{{ClusterID: "a", TargetIDs: "d1", Order: 0, Weight: 101}}}
	if err := f8.Valid(); err == nil {
		t.Fatal("weight > 100 should fail")
	}
	// 模板无效。
	f9 := &FederationDeploy{
		Name: "x", Mode: FedModeSequential,
		Template: DeployTask{Name: "", Type: TypeScript}, // 缺 name
		Members:  []FederationMember{{ClusterID: "a", TargetIDs: "d1", Order: 0, Weight: 100}},
	}
	if err := f9.Valid(); err == nil {
		t.Fatal("invalid template should fail")
	}
	// Gate 越界。
	f10 := &FederationDeploy{
		Name: "x", Mode: FedModeSequential,
		Template: DeployTask{Name: "svc", Type: TypeScript},
		Members:  []FederationMember{{ClusterID: "a", TargetIDs: "d1", Order: 0, Weight: 100}},
		Gate:     &GateConfig{SuccessRate: 150},
	}
	if err := f10.Valid(); err == nil {
		t.Fatal("gate success_rate > 100 should fail")
	}
	f11 := &FederationDeploy{
		Name: "x", Mode: FedModeSequential,
		Template: DeployTask{Name: "svc", Type: TypeScript},
		Members:  []FederationMember{{ClusterID: "a", TargetIDs: "d1", Order: 0, Weight: 100}},
		Gate:     &GateConfig{MaxFailRate: -1},
	}
	if err := f11.Valid(); err == nil {
		t.Fatal("gate max_fail_rate < 0 should fail")
	}
	// 合法。
	f12 := newFederationPlan("ok", "t1")
	if err := f12.Valid(); err != nil {
		t.Fatalf("valid plan should pass: %v", err)
	}
}

// =============================================================================
// FederationDeploy.EffectiveMode / ResolvedFedGate
// =============================================================================

func TestFederationDeploy_EffectiveMode(t *testing.T) {
	f := &FederationDeploy{}
	if f.EffectiveMode() != FedModeSequential {
		t.Fatal("empty mode should default to sequential")
	}
	f.Mode = FedModeParallel
	if f.EffectiveMode() != FedModeParallel {
		t.Fatal("parallel mode should be preserved")
	}
}

func TestFederationDeploy_ResolvedFedGate(t *testing.T) {
	// nil gate -> 默认。
	f := &FederationDeploy{}
	g := f.ResolvedFedGate()
	if g.SuccessRate != defaultGateSuccessRate || g.MaxFailRate != defaultGateMaxFailRate {
		t.Fatalf("nil gate should return defaults, got %+v", g)
	}
	// 自定义 gate。
	f.Gate = &GateConfig{SuccessRate: 80, MaxFailRate: 10}
	g = f.ResolvedFedGate()
	if g.SuccessRate != 80 || g.MaxFailRate != 10 {
		t.Fatalf("custom gate should be preserved, got %+v", g)
	}
	// 空 gate（全零）-> 回退默认 SuccessRate。
	f.Gate = &GateConfig{}
	g = f.ResolvedFedGate()
	if g.SuccessRate != defaultGateSuccessRate {
		t.Fatalf("empty gate should fallback to default, got %+v", g)
	}
}

// =============================================================================
// MemoryDeployStore.Delete / Update 边界
// =============================================================================

func TestMemoryDeployStore_Delete_NotFound(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	if err := st.Delete(ctx, 9999, "t1"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMemoryDeployStore_Delete_TenantMismatch(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	dt, _ := st.Create(ctx, newDeploy("svc", TypeScript, "dev-1", "t1"))
	if err := st.Delete(ctx, dt.ID, "t2"); err != ErrTenantMismatch {
		t.Fatalf("expected ErrTenantMismatch, got %v", err)
	}
}

func TestMemoryDeployStore_Delete_Success(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	dt, _ := st.Create(ctx, newDeploy("svc", TypeScript, "dev-1", "t1"))
	if err := st.Delete(ctx, dt.ID, "t1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := st.Get(ctx, dt.ID, "t1"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestMemoryDeployStore_Update_NotFound(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	dt := &DeployTask{ID: 9999, Name: "x", Type: TypeScript, TargetIDs: "d1", TenantID: "t1"}
	if err := st.Update(ctx, dt); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMemoryDeployStore_Update_TenantMismatch(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	dt, _ := st.Create(ctx, newDeploy("svc", TypeScript, "dev-1", "t1"))
	// 用 Get 取独立副本，越权改租户后 Update。
	cp, _ := st.Get(ctx, dt.ID, "")
	cp.TenantID = "t2"
	if err := st.Update(ctx, cp); err != ErrTenantMismatch {
		t.Fatalf("expected ErrTenantMismatch, got %v", err)
	}
}

func TestMemoryDeployStore_Create_NilAndEmptyTenant(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	if _, err := st.Create(ctx, nil); err == nil {
		t.Fatal("nil create should fail")
	}
	if _, err := st.Create(ctx, &DeployTask{Name: "x"}); err == nil {
		t.Fatal("empty tenant create should fail")
	}
}

func TestMemoryDeployStore_Update_Nil(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	if err := st.Update(ctx, nil); err == nil {
		t.Fatal("nil update should fail")
	}
}

// =============================================================================
// SQL 纯函数（无需数据库）：nullStr / marshalGate / unmarshalGate / isDupColumnErr / containsAny / indexOf
// =============================================================================

func TestNullStr(t *testing.T) {
	if nullStr("") != nil {
		t.Fatal("empty string should return nil")
	}
	if nullStr("x") != "x" {
		t.Fatal("non-empty should return itself")
	}
}

func TestMarshalGate(t *testing.T) {
	// nil gate -> nil。
	v, err := marshalGate(nil)
	if err != nil || v != nil {
		t.Fatalf("nil gate should return (nil, nil), got (%v, %v)", v, err)
	}
	// 非 nil gate -> JSON 字符串。
	g := &GateConfig{SuccessRate: 80, MaxFailRate: 10}
	v, err = marshalGate(g)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("expected string, got %T", v)
	}
	if !strings.Contains(s, "80") || !strings.Contains(s, "10") {
		t.Fatalf("JSON should contain 80 and 10, got %s", s)
	}
}

func TestUnmarshalGate(t *testing.T) {
	// 空 raw -> nil。
	if unmarshalGate(nil) != nil {
		t.Fatal("nil raw should return nil")
	}
	if unmarshalGate([]byte{}) != nil {
		t.Fatal("empty raw should return nil")
	}
	// 合法 JSON -> GateConfig。
	g := unmarshalGate([]byte(`{"success_rate":80,"max_fail_rate":10}`))
	if g == nil || g.SuccessRate != 80 || g.MaxFailRate != 10 {
		t.Fatalf("unmarshal failed, got %+v", g)
	}
	// 非法 JSON -> nil。
	if unmarshalGate([]byte(`{bad`)) != nil {
		t.Fatal("bad JSON should return nil")
	}
}

func TestIsDupColumnErr(t *testing.T) {
	// nil 错误。
	if isDupColumnErr(nil) {
		t.Fatal("nil error should return false")
	}
	// MySQL 1060 错误。
	if !isDupColumnErr(errors.New("Error 1060: Duplicate column name 'foo'")) {
		t.Fatal("1060 error should be detected")
	}
	if !isDupColumnErr(errors.New("Duplicate column 'bar'")) {
		t.Fatal("Duplicate column error should be detected")
	}
	// 其他错误。
	if isDupColumnErr(errors.New("syntax error")) {
		t.Fatal("non-dup error should return false")
	}
}

func TestContainsAny(t *testing.T) {
	if !containsAny("hello world", "world") {
		t.Fatal("should contain 'world'")
	}
	if !containsAny("abc", "a", "b", "z") {
		t.Fatal("should contain 'a' or 'b'")
	}
	if containsAny("abc", "xyz") {
		t.Fatal("should not contain 'xyz'")
	}
	// 空 sub 应被忽略。
	if containsAny("abc", "") {
		t.Fatal("empty sub should be ignored")
	}
}

func TestIndexOf(t *testing.T) {
	if indexOf("hello", "ll") != 2 {
		t.Fatal("indexOf 'll' in 'hello' should be 2")
	}
	if indexOf("hello", "world") != -1 {
		t.Fatal("indexOf 'world' in 'hello' should be -1")
	}
	if indexOf("", "x") != -1 {
		t.Fatal("indexOf in empty string should be -1")
	}
	if indexOf("abc", "") != 0 {
		t.Fatal("indexOf empty sub should be 0")
	}
}

// =============================================================================
// 辅助函数
// =============================================================================

// sleepMs 简化 time.Sleep 包装。
func sleepMs(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }

// itoa 简化的 int64 -> string 转换（避免引入 strconv 仅此处使用）。
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
