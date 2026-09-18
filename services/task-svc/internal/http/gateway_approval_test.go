// gateway_approval_test.go — 审批流管理 HTTP handler 单测。
//
// 覆盖 13 个端点：
//   - GET    /api/v1/approval/flows             列表审批流
//   - POST   /api/v1/approval/flows             创建审批流
//   - GET    /api/v1/approval/flows/{id}        查询审批流
//   - PUT    /api/v1/approval/flows/{id}        更新审批流
//   - DELETE /api/v1/approval/flows/{id}        删除审批流
//   - GET    /api/v1/approval/requests          列表审批请求
//   - POST   /api/v1/approval/requests          提交审批请求
//   - GET    /api/v1/approval/requests/{id}     审批请求详情
//   - POST   /api/v1/approval/requests/{id}/approve 审批通过
//   - POST   /api/v1/approval/requests/{id}/reject  审批拒绝
//   - POST   /api/v1/approval/requests/{id}/cancel  取消审批
//   - GET    /api/v1/approval/requests/{id}/history 审批历史
//   - GET    /api/v1/approval/pending           待我审批列表
//
// 辅助函数 newTestGateway / doReq 定义在 gateway_batch_test.go（同包共享）。
//
// 注意：ApprovalFlow / ApprovalRequest 无 JSON tag，序列化用 PascalCase 字段名。
// jwtSecret="" 时 actx.UserID 为空串，测试中 flow.Approvers 含 "" 以匹配 approver。
package http

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Levango7/OpsMesh/services/task-svc/internal/approval"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/service"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/store"
)

// createTestFlow 通过 HTTP API 创建审批流（anyof 模式，Approvers=[""] 匹配空 UserID）。
// 返回后 flowID 对应的 flow 可用于提交审批请求。
func createTestFlow(t *testing.T, mux *http.ServeMux, flowID string) {
	t.Helper()
	body := `{"ID":"` + flowID + `","Name":"Test Flow","TenantID":"default","TriggerType":"shell","Enabled":true,"Steps":[{"ID":"s1","Name":"Manager","Order":1,"Mode":"anyof","Approvers":[""]}]}`
	rec := doReq(t, mux, http.MethodPost, "/api/v1/approval/flows", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create flow %q: got %d, want 201; body=%s", flowID, rec.Code, rec.Body.String())
	}
}

// submitTestRequest 通过 HTTP API 提交审批请求，返回 request ID。
func submitTestRequest(t *testing.T, mux *http.ServeMux, reqID, flowID string) {
	t.Helper()
	body := `{"ID":"` + reqID + `","FlowID":"` + flowID + `","TriggerType":"shell","Operator":"user-1","Target":"task-1","Detail":"run cmd","Risk":"high"}`
	rec := doReq(t, mux, http.MethodPost, "/api/v1/approval/requests", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit request %q: got %d, want 201; body=%s", reqID, rec.Code, rec.Body.String())
	}
}

// ============ 审批流 CRUD ============

// TestApprovalFlows_CRUD 验证审批流完整 CRUD 生命周期。
func TestApprovalFlows_CRUD(t *testing.T) {
	mux := newTestGateway(t)

	// 创建 → 201。
	rec := doReq(t, mux, http.MethodPost, "/api/v1/approval/flows",
		`{"ID":"flow-1","Name":"Shell Approval","TenantID":"default","TriggerType":"shell","Enabled":true,"Steps":[{"ID":"s1","Name":"Manager","Order":1,"Mode":"anyof","Approvers":[""]}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create flow: got %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var flow approval.ApprovalFlow
	if err := json.Unmarshal(rec.Body.Bytes(), &flow); err != nil {
		t.Fatalf("create flow 解析失败: %v", err)
	}
	if flow.ID != "flow-1" || flow.Name != "Shell Approval" {
		t.Fatalf("flow 回显异常: %+v", flow)
	}

	// 列表 → 200。
	rec = doReq(t, mux, http.MethodGet, "/api/v1/approval/flows", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list flows: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var listResp struct {
		Flows []approval.ApprovalFlow `json:"flows"`
		Total int                     `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("list flows 解析失败: %v", err)
	}
	if listResp.Total < 1 {
		t.Fatalf("期望至少 1 个 flow，实际 total=%d", listResp.Total)
	}

	// 查询 → 200。
	rec = doReq(t, mux, http.MethodGet, "/api/v1/approval/flows/flow-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get flow: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// 更新 → 200。
	rec = doReq(t, mux, http.MethodPut, "/api/v1/approval/flows/flow-1",
		`{"ID":"flow-1","Name":"Updated Flow","TenantID":"default","TriggerType":"shell","Enabled":true,"Steps":[{"ID":"s1","Name":"Manager","Order":1,"Mode":"anyof","Approvers":[""]}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update flow: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// 删除 → 200。
	rec = doReq(t, mux, http.MethodDelete, "/api/v1/approval/flows/flow-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete flow: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var delResp struct {
		Status string `json:"status"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &delResp); err != nil {
		t.Fatalf("delete flow 解析失败: %v", err)
	}
	if delResp.Status != "deleted" || delResp.ID != "flow-1" {
		t.Fatalf("delete 回显异常: %+v", delResp)
	}

	// 删除后查询 → 404。
	rec = doReq(t, mux, http.MethodGet, "/api/v1/approval/flows/flow-1", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete: got %d, want 404", rec.Code)
	}
}

// TestApprovalFlows_BadRequests 验证审批流的错误请求。
func TestApprovalFlows_BadRequests(t *testing.T) {
	mux := newTestGateway(t)

	// 无效 JSON → 400。
	rec := doReq(t, mux, http.MethodPost, "/api/v1/approval/flows", `{invalid`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid JSON: got %d, want 400", rec.Code)
	}

	// 查询不存在 → 404。
	rec = doReq(t, mux, http.MethodGet, "/api/v1/approval/flows/no-such-flow", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing flow: got %d, want 404", rec.Code)
	}

	// 方法错误：PATCH → 405。
	rec = doReq(t, mux, http.MethodPatch, "/api/v1/approval/flows", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PATCH flows: got %d, want 405", rec.Code)
	}

	// 空 flow id → 400。
	rec = doReq(t, mux, http.MethodGet, "/api/v1/approval/flows/", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty flow id: got %d, want 400", rec.Code)
	}
}

// ============ 审批请求生命周期 ============

// TestApprovalRequests_Lifecycle 验证审批请求完整生命周期：提交→查询→approve→history。
func TestApprovalRequests_Lifecycle(t *testing.T) {
	mux := newTestGateway(t)

	// 前置：创建 flow。
	createTestFlow(t, mux, "flow-lc")

	// 提交请求 → 201。
	rec := doReq(t, mux, http.MethodPost, "/api/v1/approval/requests",
		`{"ID":"apr-lc","FlowID":"flow-lc","TriggerType":"shell","Operator":"user-1","Target":"task-1","Detail":"run cmd","Risk":"high"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit request: got %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var req approval.ApprovalRequest
	if err := json.Unmarshal(rec.Body.Bytes(), &req); err != nil {
		t.Fatalf("submit request 解析失败: %v", err)
	}
	if req.ID != "apr-lc" || req.Status != approval.StatusPending {
		t.Fatalf("request 回显异常: ID=%s Status=%s", req.ID, req.Status)
	}

	// 查询请求 → 200。
	rec = doReq(t, mux, http.MethodGet, "/api/v1/approval/requests/apr-lc", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get request: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// approve → 200（Approvers=[""] 匹配空 UserID）。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/approval/requests/apr-lc/approve", `{"comment":"ok"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var approveResp struct {
		Status string `json:"status"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &approveResp); err != nil {
		t.Fatalf("approve 解析失败: %v", err)
	}
	if approveResp.Status != "approved" || approveResp.ID != "apr-lc" {
		t.Fatalf("approve 回显异常: %+v", approveResp)
	}

	// 查询历史 → 200。
	rec = doReq(t, mux, http.MethodGet, "/api/v1/approval/requests/apr-lc/history", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("history: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// 再 approve → 400（已 approved，not pending）。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/approval/requests/apr-lc/approve", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("approve again: got %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

// TestApprovalRequests_RejectCancel 验证 reject 和 cancel 端点。
func TestApprovalRequests_RejectCancel(t *testing.T) {
	mux := newTestGateway(t)
	createTestFlow(t, mux, "flow-rc")

	// 提交请求 → reject。
	submitTestRequest(t, mux, "apr-reject", "flow-rc")
	rec := doReq(t, mux, http.MethodPost, "/api/v1/approval/requests/apr-reject/reject", `{"comment":"no"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("reject: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var rejectResp struct {
		Status string `json:"status"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &rejectResp); err != nil {
		t.Fatalf("reject 解析失败: %v", err)
	}
	if rejectResp.Status != "rejected" {
		t.Fatalf("reject status=%q, want rejected", rejectResp.Status)
	}

	// 提交另一个请求 → cancel。
	submitTestRequest(t, mux, "apr-cancel", "flow-rc")
	rec = doReq(t, mux, http.MethodPost, "/api/v1/approval/requests/apr-cancel/cancel", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var cancelResp struct {
		Status string `json:"status"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &cancelResp); err != nil {
		t.Fatalf("cancel 解析失败: %v", err)
	}
	if cancelResp.Status != "cancelled" {
		t.Fatalf("cancel status=%q, want cancelled", cancelResp.Status)
	}
}

// TestApprovalRequests_List 验证审批请求列表端点。
func TestApprovalRequests_List(t *testing.T) {
	mux := newTestGateway(t)
	createTestFlow(t, mux, "flow-list")
	submitTestRequest(t, mux, "apr-list-1", "flow-list")

	// 列表 → 200。
	rec := doReq(t, mux, http.MethodGet, "/api/v1/approval/requests", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list requests: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Requests []approval.ApprovalRequest `json:"requests"`
		Total    int                        `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("list requests 解析失败: %v", err)
	}
	if resp.Total < 1 {
		t.Fatalf("期望至少 1 个请求，实际 total=%d", resp.Total)
	}

	// 按状态过滤：pending。
	rec = doReq(t, mux, http.MethodGet, "/api/v1/approval/requests?status=pending", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list pending requests: got %d, want 200", rec.Code)
	}
}

// ============ 待我审批 ============

// TestApproval_Pending 验证待我审批列表端点。
func TestApproval_Pending(t *testing.T) {
	mux := newTestGateway(t)
	createTestFlow(t, mux, "flow-pending")
	submitTestRequest(t, mux, "apr-pending", "flow-pending")

	// GET pending → 200。
	rec := doReq(t, mux, http.MethodGet, "/api/v1/approval/pending", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("pending: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Pending []approval.ApprovalRequest `json:"pending"`
		Total   int                        `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("pending 解析失败: %v", err)
	}
	// Approvers=[""] 匹配空 UserID，应能查到待审批请求。
	if resp.Total < 1 {
		t.Fatalf("期望至少 1 个待审批请求，实际 total=%d", resp.Total)
	}

	// 方法错误：POST → 405。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/approval/pending", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST pending: got %d, want 405", rec.Code)
	}
}

// ============ 引擎未配置 ============

// TestApproval_EngineNotConfigured 验证未注入审批引擎时返回 503。
func TestApproval_EngineNotConfigured(t *testing.T) {
	// 构造不注入 approvalEngine 的 Gateway。
	st := store.NewMemoryStore()
	svc := service.NewService(st, st, st, st)
	g := NewGateway(svc, "")
	// 不调用 SetApprovalEngine → approvalEngine 为 nil
	mux := http.NewServeMux()
	g.RegisterRoutes(mux)

	// GET flows → 503。
	rec := doReq(t, mux, http.MethodGet, "/api/v1/approval/flows", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("flows without engine: got %d, want 503", rec.Code)
	}

	// POST requests → 503。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/approval/requests", `{"ID":"x","FlowID":"f","TriggerType":"shell","Operator":"u"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("requests without engine: got %d, want 503", rec.Code)
	}

	// GET pending → 503。
	rec = doReq(t, mux, http.MethodGet, "/api/v1/approval/pending", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("pending without engine: got %d, want 503", rec.Code)
	}
}

// ============ 审批请求错误请求 ============

// TestApprovalRequests_BadRequests 验证审批请求的错误请求。
func TestApprovalRequests_BadRequests(t *testing.T) {
	mux := newTestGateway(t)

	// 无效 JSON 提交请求 → 400。
	rec := doReq(t, mux, http.MethodPost, "/api/v1/approval/requests", `{invalid`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid JSON: got %d, want 400", rec.Code)
	}

	// 查询不存在的请求 → 404。
	rec = doReq(t, mux, http.MethodGet, "/api/v1/approval/requests/no-such-req", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing request: got %d, want 404", rec.Code)
	}

	// approve 不存在的请求 → 400（engine.Approve 返回 ErrRequestNotFound → handler 400）。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/approval/requests/no-such-req/approve", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("approve missing request: got %d, want 400", rec.Code)
	}

	// 查询不存在的 history → 404。
	rec = doReq(t, mux, http.MethodGet, "/api/v1/approval/requests/no-such-req/history", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("history missing request: got %d, want 404", rec.Code)
	}

	// 方法错误：PUT /api/v1/approval/requests → 405。
	rec = doReq(t, mux, http.MethodPut, "/api/v1/approval/requests", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT requests: got %d, want 405", rec.Code)
	}

	// 未知子路径 → 404。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/approval/requests/some-id/unknown", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown sub-path: got %d, want 404", rec.Code)
	}
}
