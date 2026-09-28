package handler

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/portal-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/portal-svc/internal/service"
	"github.com/Levango7/OpsMesh/services/portal-svc/internal/store"
)

// newTestServer 组装带全部路由的测试服务器，并暴露 service 供数据准备。
func newTestServer(t *testing.T) (*httptest.Server, *service.Service) {
	t.Helper()
	svc := service.NewService(store.NewMemoryStore())
	h := NewHandler(svc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, svc
}

// doJSON 发请求并解析 JSON 响应体（空体返回 nil map）。
func doJSON(t *testing.T, srv *httptest.Server, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("序列化请求体失败: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, srv.URL+path, reader)
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// requestsOf 取契约包装形态 {requests:[…]} 中的数组。
func requestsOf(t *testing.T, payload map[string]any) []map[string]any {
	t.Helper()
	raw, ok := payload["requests"]
	if !ok {
		t.Fatalf("响应缺 requests 包装: %v", payload)
	}
	arr, ok := raw.([]any)
	if !ok {
		t.Fatalf("requests 不是数组: %T", raw)
	}
	out := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("requests 元素不是对象: %T", item)
		}
		out = append(out, m)
	}
	return out
}

// TestCreateRequestFrontendContract 前端契约形态提交：{type,resource,params,reason}
// 映射到模型（title=resource、description 含 reason 与 params、resource_type=type），
// params 的 JSON 数值参与成本估算，响应为 camelCase 视图。
func TestCreateRequestFrontendContract(t *testing.T) {
	srv, _ := newTestServer(t)
	code, payload := doJSON(t, srv, http.MethodPost, "/api/v1/requests", map[string]any{
		"type":     "gpu",
		"resource": "my-app-prod",
		"params":   `{"cpu":4,"memory_gb":8,"storage_gb":100}`,
		"reason":   "需要训练环境",
	})
	if code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%v", code, payload)
	}
	if payload["type"] != "gpu" || payload["resource"] != "my-app-prod" || payload["status"] != "draft" {
		t.Fatalf("契约字段映射错误: %v", payload)
	}
	if payload["requester"] != "unknown" {
		t.Fatalf("requester=%v, want unknown（无网关注入头时的留痕兜底）", payload["requester"])
	}
	wantCost := 4*0.05 + 8*0.01 + 100*0.001
	if got, ok := payload["cost"].(float64); !ok || math.Abs(got-wantCost) > 1e-9 {
		t.Fatalf("cost=%v, want %v（params JSON 数值应参与估算）", payload["cost"], wantCost)
	}
	if _, ok := payload["createdAt"]; !ok {
		t.Fatalf("响应缺 createdAt: %v", payload)
	}
}

// TestCreateRequestLegacySnakeCase 既有 snake_case 形态仍可用（双解析不回归）。
func TestCreateRequestLegacySnakeCase(t *testing.T) {
	srv, _ := newTestServer(t)
	code, payload := doJSON(t, srv, http.MethodPost, "/api/v1/requests", map[string]any{
		"tenant_id":     "t-1",
		"requester":     "alice",
		"title":         "legacy vm",
		"description":   "legacy desc",
		"resource_type": "vm",
		"cpu":           2,
		"memory_gb":     4,
		"storage_gb":    0,
	})
	if code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%v", code, payload)
	}
	if payload["resource"] != "legacy vm" || payload["requester"] != "alice" || payload["type"] != "vm" {
		t.Fatalf("legacy 字段映射错误: %v", payload)
	}
}

// TestCreateRequestMissingResource 缺 resource/title 仍按校验拒绝（不能建出空标题请求）。
func TestCreateRequestMissingResource(t *testing.T) {
	srv, _ := newTestServer(t)
	code, _ := doJSON(t, srv, http.MethodPost, "/api/v1/requests", map[string]any{"type": "vm"})
	if code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", code)
	}
}

// TestListRequestsContract 列表契约：{requests:[…]} + camelCase 字段。
func TestListRequestsContract(t *testing.T) {
	srv, svc := newTestServer(t)
	if _, err := svc.CreateRequest("default", "alice", "res-a", "why", "gpu", 1, 2, 0); err != nil {
		t.Fatalf("准备数据失败: %v", err)
	}
	code, payload := doJSON(t, srv, http.MethodGet, "/api/v1/requests", nil)
	if code != http.StatusOK {
		t.Fatalf("status=%d, want 200", code)
	}
	rows := requestsOf(t, payload)
	if len(rows) != 1 {
		t.Fatalf("rows=%d, want 1", len(rows))
	}
	row := rows[0]
	for _, key := range []string{"id", "type", "resource", "status", "requester", "cost", "createdAt"} {
		if _, ok := row[key]; !ok {
			t.Fatalf("契约缺字段 %s: %v", key, row)
		}
	}
	if row["resource"] != "res-a" || row["requester"] != "alice" {
		t.Fatalf("字段映射错误: %v", row)
	}
}

// TestApprovalQueueAndDecisions 审批队列只出 pending；approve 空体、reject 带 reason
// 都能推进状态并回 {status}；非 pending 再批报 400，未知 ID 报 404。
func TestApprovalQueueAndDecisions(t *testing.T) {
	srv, svc := newTestServer(t)
	draft, err := svc.CreateRequest("default", "alice", "res-b", "why", "vm", 1, 1, 0)
	if err != nil {
		t.Fatalf("准备数据失败: %v", err)
	}
	if _, err := svc.SubmitRequest(draft.ID); err != nil {
		t.Fatalf("提交请求失败: %v", err)
	}

	code, payload := doJSON(t, srv, http.MethodGet, "/api/v1/approvals", nil)
	if code != http.StatusOK {
		t.Fatalf("status=%d, want 200", code)
	}
	rows := requestsOf(t, payload)
	if len(rows) != 1 || rows[0]["id"] != draft.ID || rows[0]["status"] != "pending" {
		t.Fatalf("待审批队列错误: %v", rows)
	}

	// approve：空请求体（前端 approveRequest 不带 body）。
	code, payload = doJSON(t, srv, http.MethodPost, "/api/v1/approvals/"+draft.ID+"/approve", nil)
	if code != http.StatusOK || payload["status"] != "approved" {
		t.Fatalf("approve status=%d body=%v, want 200 approved", code, payload)
	}

	// 队列应已清空。
	_, payload = doJSON(t, srv, http.MethodGet, "/api/v1/approvals", nil)
	if rows := requestsOf(t, payload); len(rows) != 0 {
		t.Fatalf("审批后队列应为空: %v", rows)
	}

	// 已 approved 的再批 → 400（非法迁移）。
	code, _ = doJSON(t, srv, http.MethodPost, "/api/v1/approvals/"+draft.ID+"/approve", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("重复 approve status=%d, want 400", code)
	}

	// reject 带 reason → note 落库。
	draft2, err := svc.CreateRequest("default", "bob", "res-c", "why", "vm", 1, 1, 0)
	if err != nil {
		t.Fatalf("准备数据失败: %v", err)
	}
	if _, err := svc.SubmitRequest(draft2.ID); err != nil {
		t.Fatalf("提交请求失败: %v", err)
	}
	code, payload = doJSON(t, srv, http.MethodPost, "/api/v1/approvals/"+draft2.ID+"/reject",
		map[string]any{"reason": "budget exceeded"})
	if code != http.StatusOK || payload["status"] != "rejected" {
		t.Fatalf("reject status=%d body=%v, want 200 rejected", code, payload)
	}
	got, err := svc.GetRequest(draft2.ID)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if got.ApprovalNote != "budget exceeded" {
		t.Fatalf("approval_note=%q, want budget exceeded", got.ApprovalNote)
	}

	// 未知 ID → 404。
	code, _ = doJSON(t, srv, http.MethodPost, "/api/v1/approvals/no-such-id/reject", map[string]any{"reason": "x"})
	if code != http.StatusNotFound {
		t.Fatalf("未知 ID status=%d, want 404", code)
	}
}

// TestApprovalDecisionRecordsInjectedUser 网关注入 X-User-Id 时审批人落库。
func TestApprovalDecisionRecordsInjectedUser(t *testing.T) {
	srv, svc := newTestServer(t)
	draft, err := svc.CreateRequest("default", "alice", "res-d", "why", "vm", 1, 1, 0)
	if err != nil {
		t.Fatalf("准备数据失败: %v", err)
	}
	if _, err := svc.SubmitRequest(draft.ID); err != nil {
		t.Fatalf("提交请求失败: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/approvals/"+draft.ID+"/approve", nil)
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	req.Header.Set("X-User-Id", "carol")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}
	got, err := svc.GetRequest(draft.ID)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if got.Approver != "carol" {
		t.Fatalf("approver=%q, want carol", got.Approver)
	}
}

// TestCostOverviewContract 成本总览契约：{total,trend,byCategory}，
// 数值来自已批准/已交付请求，趋势窗口 14 天。
func TestCostOverviewContract(t *testing.T) {
	srv, svc := newTestServer(t)
	draft, err := svc.CreateRequest("default", "alice", "res-e", "why", "gpu", 4, 8, 0)
	if err != nil {
		t.Fatalf("准备数据失败: %v", err)
	}
	if _, err := svc.SubmitRequest(draft.ID); err != nil {
		t.Fatalf("提交请求失败: %v", err)
	}
	if _, err := svc.ApproveRequest(draft.ID, "carol", ""); err != nil {
		t.Fatalf("审批失败: %v", err)
	}

	code, payload := doJSON(t, srv, http.MethodGet, "/api/v1/cost", nil)
	if code != http.StatusOK {
		t.Fatalf("status=%d, want 200", code)
	}
	want := 4*0.05 + 8*0.01
	if got, ok := payload["total"].(float64); !ok || math.Abs(got-want) > 1e-9 {
		t.Fatalf("total=%v, want %v", payload["total"], want)
	}
	trend, ok := payload["trend"].([]any)
	if !ok || len(trend) != costTrendDays {
		t.Fatalf("trend 长度错误: %v", payload["trend"])
	}
	last, ok := trend[len(trend)-1].(map[string]any)
	if !ok || math.Abs(last["amount"].(float64)-want) > 1e-9 {
		t.Fatalf("当日趋势点错误: %v", trend[len(trend)-1])
	}
	byCategory, ok := payload["byCategory"].([]any)
	if !ok || len(byCategory) != 1 {
		t.Fatalf("byCategory 错误: %v", payload["byCategory"])
	}
	cat, _ := byCategory[0].(map[string]any)
	if cat["category"] != "gpu" {
		t.Fatalf("category=%v, want gpu", cat["category"])
	}
}

// TestBuildCostOverview 固定时钟单测：只统计 approved/fulfilled、
// 窗口外不计入趋势但计入 total、分类金额降序。
func TestBuildCostOverview(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	reqs := []*models.ResourceRequest{
		{ID: "r1", ResourceType: "gpu", Status: models.StatusApproved, CostEstimate: 10, UpdatedAt: now},
		{ID: "r2", ResourceType: "vm", Status: models.StatusFulfilled, CostEstimate: 5, UpdatedAt: now.AddDate(0, 0, -3)},
		{ID: "r3", ResourceType: "gpu", Status: models.StatusPending, CostEstimate: 99, UpdatedAt: now},
		{ID: "r4", ResourceType: "gpu", Status: models.StatusRejected, CostEstimate: 77, UpdatedAt: now},
		{ID: "r5", ResourceType: "storage", Status: models.StatusApproved, CostEstimate: 1, UpdatedAt: now.AddDate(0, 0, -20)},
	}
	view := buildCostOverview(reqs, now, 14)

	if math.Abs(view.Total-16) > 1e-9 {
		t.Fatalf("total=%v, want 16", view.Total)
	}
	if len(view.Trend) != 14 {
		t.Fatalf("trend len=%d, want 14", len(view.Trend))
	}
	if view.Trend[13].Date != "2026-09-29" || math.Abs(view.Trend[13].Amount-10) > 1e-9 {
		t.Fatalf("当日点错误: %+v", view.Trend[13])
	}
	if math.Abs(view.Trend[10].Amount-5) > 1e-9 {
		t.Fatalf("3 天前点错误: %+v", view.Trend[10])
	}
	for _, p := range view.Trend {
		if p.Date == "2026-09-09" {
			t.Fatalf("窗口外日期不应出现在趋势中")
		}
	}
	if len(view.ByCategory) != 3 ||
		view.ByCategory[0].Category != "gpu" || view.ByCategory[1].Category != "vm" || view.ByCategory[2].Category != "storage" {
		t.Fatalf("byCategory 排序错误: %+v", view.ByCategory)
	}
}

// TestCostSubroutesNotShadowed 新增的 /api/v1/cost 精确路由不得遮蔽既有子路由。
func TestCostSubroutesNotShadowed(t *testing.T) {
	srv, _ := newTestServer(t)
	code, payload := doJSON(t, srv, http.MethodGet, "/api/v1/cost/utilization?tenant_id=t-1", nil)
	if code != http.StatusOK {
		t.Fatalf("cost/utilization status=%d, body=%v", code, payload)
	}
	if _, ok := payload["cpu_usage"]; !ok {
		t.Fatalf("cost/utilization 响应异常: %v", payload)
	}
}
