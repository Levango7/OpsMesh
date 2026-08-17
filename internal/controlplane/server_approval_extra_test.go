package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opsmesh/internal/approval"
	"opsmesh/internal/config"
	"opsmesh/internal/store"
)

// 本文件补全 server_approval.go 中 0% 覆盖的审批 handler：
//   - approvalCreateFlow / approvalGetFlow / approvalUpdateFlow / approvalDeleteFlow
//   - handleApprovalFlowRouting
//   - handleApprovalRequests / approvalListRequests / approvalSubmitRequest
//   - handleApprovalRequestRouting
//   - approvalGetRequest / approvalApproveRequest / approvalRejectRequest
//   - approvalCancelRequest / approvalGetHistory
//
// 测试模式：构造 Server{approvalEngine: approval.New(), Demo: true}，用 httptest 发请求，
// demo 模式下 requireTenantContext 自动填充 default 租户、requireProd 自动放行。

// newApprovalTestServer 构造带审批引擎的测试控制面（demo 模式放宽认证）。
func newApprovalTestServer() *Server {
	return &Server{
		store:          store.NewMemoryStore(),
		cfg:            &config.Config{Demo: true},
		requireAuth:    false,
		approvalEngine: approval.New(),
	}
}

// sampleFlowBody 构造合法审批流 JSON。
const sampleFlowBody = `{
  "ID": "flow-test",
  "Name": "test-flow",
  "TenantID": "default",
  "TriggerType": "shell",
  "Enabled": true,
  "Steps": [
    {"ID": "s1", "Name": "step1", "Order": 1, "Mode": "sequential", "Approvers": ["alice"]}
  ]
}`

// =============================================================================
// approvalCreateFlow / handleApprovalFlows
// =============================================================================

func TestApprovalCreateFlow_Happy(t *testing.T) {
	s := newApprovalTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/approval/flows", strings.NewReader(sampleFlowBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-ID", "u1")
	rec := httptest.NewRecorder()
	s.handleApprovalFlows(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var f approval.ApprovalFlow
	if err := json.Unmarshal(rec.Body.Bytes(), &f); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if f.ID != "flow-test" {
		t.Errorf("ID=%q, want flow-test", f.ID)
	}
}

func TestApprovalCreateFlow_BadJSON(t *testing.T) {
	s := newApprovalTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/approval/flows", strings.NewReader("not json"))
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleApprovalFlows(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestApprovalCreateFlow_InvalidFlow(t *testing.T) {
	s := newApprovalTestServer()
	// 缺 Name 字段，Validate 应失败
	body := `{"ID":"f2","TenantID":"default","TriggerType":"shell","Steps":[{"ID":"s1","Order":1,"Mode":"sequential","Approvers":["a"]}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/approval/flows", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleApprovalFlows(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestApprovalListFlows_Happy(t *testing.T) {
	s := newApprovalTestServer()
	// 先创建一个流
	req := httptest.NewRequest(http.MethodPost, "/api/v1/approval/flows", strings.NewReader(sampleFlowBody))
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleApprovalFlows(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup create failed: %d body=%s", rec.Code, rec.Body.String())
	}
	// 列表
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/approval/flows", nil)
	req2.Header.Set("X-Tenant-ID", "default")
	rec2 := httptest.NewRecorder()
	s.handleApprovalFlows(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec2.Code, rec2.Body.String())
	}
	var resp struct {
		Flows []*approval.ApprovalFlow `json:"flows"`
		Total int                      `json:"total"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total == 0 {
		t.Error("expect flows, got 0")
	}
}

func TestHandleApprovalFlows_MethodNotAllowed(t *testing.T) {
	s := newApprovalTestServer()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/approval/flows", nil)
	rec := httptest.NewRecorder()
	s.handleApprovalFlows(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

// =============================================================================
// handleApprovalFlowRouting / approvalGetFlow / approvalUpdateFlow / approvalDeleteFlow
// =============================================================================

func TestApprovalFlowRouting_GetUpdateDelete(t *testing.T) {
	s := newApprovalTestServer()
	// 创建
	req := httptest.NewRequest(http.MethodPost, "/api/v1/approval/flows", strings.NewReader(sampleFlowBody))
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleApprovalFlows(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup: %d body=%s", rec.Code, rec.Body.String())
	}

	// GET via routing
	gReq := httptest.NewRequest(http.MethodGet, "/api/v1/approval/flows/flow-test", nil)
	gReq.Header.Set("X-Tenant-ID", "default")
	gRec := httptest.NewRecorder()
	s.handleApprovalFlowRouting(gRec, gReq)
	if gRec.Code != http.StatusOK {
		t.Fatalf("get: %d body=%s", gRec.Code, gRec.Body.String())
	}

	// PUT via routing
	updBody := `{"ID":"flow-test","Name":"updated","TenantID":"default","TriggerType":"shell","Enabled":true,"Steps":[{"ID":"s1","Name":"step1","Order":1,"Mode":"sequential","Approvers":["bob"]}]}`
	pReq := httptest.NewRequest(http.MethodPut, "/api/v1/approval/flows/flow-test", strings.NewReader(updBody))
	pReq.Header.Set("X-Tenant-ID", "default")
	pRec := httptest.NewRecorder()
	s.handleApprovalFlowRouting(pRec, pReq)
	if pRec.Code != http.StatusOK {
		t.Fatalf("update: %d body=%s", pRec.Code, pRec.Body.String())
	}

	// DELETE via routing
	dReq := httptest.NewRequest(http.MethodDelete, "/api/v1/approval/flows/flow-test", nil)
	dReq.Header.Set("X-Tenant-ID", "default")
	dRec := httptest.NewRecorder()
	s.handleApprovalFlowRouting(dRec, dReq)
	if dRec.Code != http.StatusOK {
		t.Fatalf("delete: %d body=%s", dRec.Code, dRec.Body.String())
	}
}

func TestApprovalFlowRouting_EmptyID(t *testing.T) {
	s := newApprovalTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/approval/flows/", nil)
	rec := httptest.NewRecorder()
	s.handleApprovalFlowRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestApprovalFlowRouting_MethodNotAllowed(t *testing.T) {
	s := newApprovalTestServer()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/approval/flows/x", nil)
	rec := httptest.NewRecorder()
	s.handleApprovalFlowRouting(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

func TestApprovalGetFlow_NotFound(t *testing.T) {
	s := newApprovalTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/approval/flows/nope", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleApprovalFlowRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

func TestApprovalGetFlow_TenantMismatch(t *testing.T) {
	s := newApprovalTestServer()
	// 创建 default 租户的流
	req := httptest.NewRequest(http.MethodPost, "/api/v1/approval/flows", strings.NewReader(sampleFlowBody))
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleApprovalFlows(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup: %d", rec.Code)
	}
	// 用其他租户查询
	gReq := httptest.NewRequest(http.MethodGet, "/api/v1/approval/flows/flow-test", nil)
	gReq.Header.Set("X-Tenant-ID", "other")
	gRec := httptest.NewRecorder()
	s.handleApprovalFlowRouting(gRec, gReq)
	if gRec.Code != http.StatusForbidden {
		t.Fatalf("tenant mismatch: %d, want 403", gRec.Code)
	}
}

func TestApprovalUpdateFlow_BadJSON(t *testing.T) {
	s := newApprovalTestServer()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/approval/flows/x", strings.NewReader("not json"))
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleApprovalFlowRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestApprovalDeleteFlow_NotFound(t *testing.T) {
	s := newApprovalTestServer()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/approval/flows/nope", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleApprovalFlowRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

// =============================================================================
// handleApprovalRequests / approvalListRequests / approvalSubmitRequest
// =============================================================================

// submitReqBody 构造合法审批请求 JSON（依赖 flow-test）。
const submitReqBody = `{
  "ID": "apr-1",
  "FlowID": "flow-test",
  "TenantID": "default",
  "TriggerType": "shell",
  "Operator": "u1",
  "Target": "restart svc",
  "Risk": "medium",
  "Status": "pending"
}`

func TestApprovalSubmitRequest_Happy(t *testing.T) {
	s := newApprovalTestServer()
	// 先创建流
	fReq := httptest.NewRequest(http.MethodPost, "/api/v1/approval/flows", strings.NewReader(sampleFlowBody))
	fReq.Header.Set("X-Tenant-ID", "default")
	fRec := httptest.NewRecorder()
	s.handleApprovalFlows(fRec, fReq)
	if fRec.Code != http.StatusCreated {
		t.Fatalf("setup flow: %d body=%s", fRec.Code, fRec.Body.String())
	}
	// 提交请求
	req := httptest.NewRequest(http.MethodPost, "/api/v1/approval/requests", strings.NewReader(submitReqBody))
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-ID", "u1")
	rec := httptest.NewRecorder()
	s.handleApprovalRequests(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit: %d body=%s", rec.Code, rec.Body.String())
	}
	var r approval.ApprovalRequest
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if r.ID != "apr-1" {
		t.Errorf("ID=%q, want apr-1", r.ID)
	}
}

func TestApprovalSubmitRequest_BadJSON(t *testing.T) {
	s := newApprovalTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/approval/requests", strings.NewReader("not json"))
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleApprovalRequests(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestApprovalListRequests_Happy(t *testing.T) {
	s := newApprovalTestServer()
	// 先创建流并提交请求
	fReq := httptest.NewRequest(http.MethodPost, "/api/v1/approval/flows", strings.NewReader(sampleFlowBody))
	fReq.Header.Set("X-Tenant-ID", "default")
	s.handleApprovalFlows(httptest.NewRecorder(), fReq)

	sReq := httptest.NewRequest(http.MethodPost, "/api/v1/approval/requests", strings.NewReader(submitReqBody))
	sReq.Header.Set("X-Tenant-ID", "default")
	sReq.Header.Set("X-User-ID", "u1")
	s.handleApprovalRequests(httptest.NewRecorder(), sReq)

	// 列表
	req := httptest.NewRequest(http.MethodGet, "/api/v1/approval/requests?status=pending", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleApprovalRequests(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Requests []*approval.ApprovalRequest `json:"requests"`
		Total    int                         `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total == 0 {
		t.Error("expect requests, got 0")
	}
}

func TestHandleApprovalRequests_MethodNotAllowed(t *testing.T) {
	s := newApprovalTestServer()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/approval/requests", nil)
	rec := httptest.NewRecorder()
	s.handleApprovalRequests(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

// =============================================================================
// handleApprovalRequestRouting / approvalGetRequest / approve / reject / cancel / history
// =============================================================================

// setupApprovalRequest 创建流并提交请求，返回 server。
func setupApprovalRequest(t *testing.T) *Server {
	t.Helper()
	s := newApprovalTestServer()
	fReq := httptest.NewRequest(http.MethodPost, "/api/v1/approval/flows", strings.NewReader(sampleFlowBody))
	fReq.Header.Set("X-Tenant-ID", "default")
	fRec := httptest.NewRecorder()
	s.handleApprovalFlows(fRec, fReq)
	if fRec.Code != http.StatusCreated {
		t.Fatalf("setup flow: %d body=%s", fRec.Code, fRec.Body.String())
	}
	sReq := httptest.NewRequest(http.MethodPost, "/api/v1/approval/requests", strings.NewReader(submitReqBody))
	sReq.Header.Set("X-Tenant-ID", "default")
	sReq.Header.Set("X-User-ID", "u1")
	sRec := httptest.NewRecorder()
	s.handleApprovalRequests(sRec, sReq)
	if sRec.Code != http.StatusCreated {
		t.Fatalf("setup request: %d body=%s", sRec.Code, sRec.Body.String())
	}
	return s
}

func TestApprovalGetRequest_Happy(t *testing.T) {
	s := setupApprovalRequest(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/approval/requests/apr-1", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleApprovalRequestRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestApprovalGetRequest_NotFound(t *testing.T) {
	s := newApprovalTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/approval/requests/nope", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleApprovalRequestRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

func TestApprovalGetRequest_TenantMismatch(t *testing.T) {
	s := setupApprovalRequest(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/approval/requests/apr-1", nil)
	req.Header.Set("X-Tenant-ID", "other")
	rec := httptest.NewRecorder()
	s.handleApprovalRequestRouting(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("tenant mismatch: %d, want 403", rec.Code)
	}
}

func TestApprovalApproveRequest_Happy(t *testing.T) {
	s := setupApprovalRequest(t)
	body := `{"comment":"lgtm"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/approval/requests/apr-1/approve", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-ID", "alice")
	rec := httptest.NewRecorder()
	s.handleApprovalRequestRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve: %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestApprovalApproveRequest_NotFound(t *testing.T) {
	s := newApprovalTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/approval/requests/nope/approve", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleApprovalRequestRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestApprovalRejectRequest_Happy(t *testing.T) {
	s := setupApprovalRequest(t)
	body := `{"comment":"nope"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/approval/requests/apr-1/reject", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-ID", "alice")
	rec := httptest.NewRecorder()
	s.handleApprovalRequestRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("reject: %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestApprovalRejectRequest_NotFound(t *testing.T) {
	s := newApprovalTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/approval/requests/nope/reject", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleApprovalRequestRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestApprovalCancelRequest_Happy(t *testing.T) {
	s := setupApprovalRequest(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/approval/requests/apr-1/cancel", nil)
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-ID", "u1")
	rec := httptest.NewRecorder()
	s.handleApprovalRequestRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel: %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestApprovalCancelRequest_NotFound(t *testing.T) {
	s := newApprovalTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/approval/requests/nope/cancel", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleApprovalRequestRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestApprovalGetHistory_Happy(t *testing.T) {
	s := setupApprovalRequest(t)
	// 先 approve 一下产生历史
	aReq := httptest.NewRequest(http.MethodPost, "/api/v1/approval/requests/apr-1/approve", strings.NewReader(`{"comment":"ok"}`))
	aReq.Header.Set("X-Tenant-ID", "default")
	aReq.Header.Set("X-User-ID", "alice")
	s.handleApprovalRequestRouting(httptest.NewRecorder(), aReq)

	// 查历史
	req := httptest.NewRequest(http.MethodGet, "/api/v1/approval/requests/apr-1/history", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleApprovalRequestRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("history: %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestApprovalGetHistory_NotFound(t *testing.T) {
	s := newApprovalTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/approval/requests/nope/history", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleApprovalRequestRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

func TestApprovalRequestRouting_EmptyID(t *testing.T) {
	s := newApprovalTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/approval/requests/", nil)
	rec := httptest.NewRecorder()
	s.handleApprovalRequestRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestApprovalRequestRouting_UnknownSubPath(t *testing.T) {
	s := newApprovalTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/approval/requests/apr-1/unknown", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleApprovalRequestRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

func TestApprovalRequestRouting_MethodNotAllowed(t *testing.T) {
	s := newApprovalTestServer()
	// GET on /requests/{id}/approve should be 405
	req := httptest.NewRequest(http.MethodGet, "/api/v1/approval/requests/apr-1/approve", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleApprovalRequestRouting(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

func TestApprovalRequestRouting_GetMethodNotAllowed(t *testing.T) {
	s := newApprovalTestServer()
	// POST on /requests/{id} (no sub) should be 405
	req := httptest.NewRequest(http.MethodPost, "/api/v1/approval/requests/apr-1", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleApprovalRequestRouting(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

// =============================================================================
// handleApprovalPending
// =============================================================================

func TestApprovalPending_Happy(t *testing.T) {
	s := setupApprovalRequest(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/approval/pending", nil)
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-ID", "alice")
	rec := httptest.NewRecorder()
	s.handleApprovalPending(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestApprovalPending_MethodNotAllowed(t *testing.T) {
	s := newApprovalTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/approval/pending", nil)
	rec := httptest.NewRecorder()
	s.handleApprovalPending(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}
