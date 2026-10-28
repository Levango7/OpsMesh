package models

import (
	"encoding/json"
	"slices"
	"testing"
	"time"
)

func TestRequestStatusLiteralValues(t *testing.T) {
	statuses := map[string]RequestStatus{
		"draft":     StatusDraft,
		"pending":   StatusPending,
		"approved":  StatusApproved,
		"rejected":  StatusRejected,
		"fulfilled": StatusFulfilled,
		"cancelled": StatusCancelled,
	}
	for want, got := range statuses {
		if string(got) != want {
			t.Fatalf("RequestStatus 字面值 = %q, want %q（API/持久化契约）", got, want)
		}
	}
}

func TestRequestStatusIsTerminal(t *testing.T) {
	cases := []struct {
		status RequestStatus
		want   bool
	}{
		{StatusDraft, false},
		{StatusPending, false},
		{StatusApproved, true},
		{StatusRejected, true},
		{StatusFulfilled, true},
		{StatusCancelled, true},
		{RequestStatus(""), false},
		{RequestStatus("unknown"), false},
	}
	for _, tc := range cases {
		if got := tc.status.IsTerminal(); got != tc.want {
			t.Fatalf("IsTerminal(%q) = %v, want %v", tc.status, got, tc.want)
		}
	}
}

func decodeObject(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %T: %v", v, err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal %T: %v (json=%s)", v, err, raw)
	}
	return out
}

func requireKeys(t *testing.T, obj map[string]any, keys ...string) {
	t.Helper()
	got := make([]string, 0, len(obj))
	for k := range obj {
		got = append(got, k)
	}
	slices.Sort(got)
	for _, k := range keys {
		if !slices.Contains(got, k) {
			t.Fatalf("缺少键 %q（实得 %v）", k, got)
		}
	}
}

// 注意：ResourceRequest.StorageGB / QuotaUsage.UsedStorageGB 的 JSON tag 目前是
// storage_gd / used_storage_gd（与 handler 的 storage_gb、DB 列 storage_gb 不一致，
// 疑似笔误）。当前模型不直接序列化给前端（handler 有自有 DTO），故本测试不钉这两个键，
// 避免把笔误固化成契约；已在板面登记待 owner 处置。
func TestResourceRequestJSONContract(t *testing.T) {
	now := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	req := ResourceRequest{
		ID:           "req-1",
		TenantID:     "t1",
		Requester:    "alice",
		Title:        "web",
		Description:  "2C4G",
		ResourceType: "vm",
		CPU:          2,
		MemoryGB:     4,
		StorageGB:    100,
		CostEstimate: 12.5,
		Status:       StatusPending,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	obj := decodeObject(t, req)
	requireKeys(t, obj, "id", "tenant_id", "requester", "title", "description",
		"resource_type", "cpu", "memory_gb", "cost_estimate", "status", "created_at", "updated_at")
	if _, ok := obj["approver"]; ok {
		t.Fatal("空 approver 应省略")
	}
	if _, ok := obj["approval_note"]; ok {
		t.Fatal("空 approval_note 应省略")
	}

	req.Approver = "bob"
	req.ApprovalNote = "ok"
	obj = decodeObject(t, req)
	if obj["approver"] != "bob" || obj["approval_note"] != "ok" {
		t.Fatalf("审批字段未序列化: %v", obj)
	}

	// CPU/Memory/Storage 都是整型数值，不能被序列化成字符串。
	if obj["cpu"] != float64(2) || obj["memory_gb"] != float64(4) {
		t.Fatalf("数值字段类型漂移: %v", obj)
	}
}

func TestQuotaUsageJSONContract(t *testing.T) {
	usage := QuotaUsage{
		TenantID:      "t1",
		UsedCPU:       4,
		UsedMemoryGB:  8,
		UsedStorageGB: 160,
		UsedRequests:  3,
	}
	obj := decodeObject(t, usage)
	requireKeys(t, obj, "tenant_id", "used_cpu", "used_memory_gb", "used_requests", "quota")
	if obj["quota"] != nil {
		t.Fatalf("nil quota 应序列化为 null, got %v", obj["quota"])
	}

	usage.Quota = &Quota{TenantID: "t1", MaxCPU: 8, MaxMemoryGB: 16, MaxStorageGB: 320, MaxRequests: 10}
	obj = decodeObject(t, usage)
	quota, ok := obj["quota"].(map[string]any)
	if !ok {
		t.Fatalf("quota 不是对象: %#v", obj["quota"])
	}
	requireKeys(t, quota, "tenant_id", "max_cpu", "max_memory_gb", "max_storage_gb", "max_requests")
	if quota["max_cpu"] != float64(8) {
		t.Fatalf("max_cpu = %v, want 8", quota["max_cpu"])
	}
}

func TestDashboardStatsJSONContract(t *testing.T) {
	obj := decodeObject(t, DashboardStats{TotalRequests: 5, PendingRequests: 2, ApprovedRequests: 1})
	requireKeys(t, obj, "total_requests", "pending_requests", "approved_requests",
		"rejected_requests", "total_savings", "monthly_spend", "active_quotas")
	if obj["total_requests"] != float64(5) {
		t.Fatalf("total_requests = %v, want 5", obj["total_requests"])
	}
}
