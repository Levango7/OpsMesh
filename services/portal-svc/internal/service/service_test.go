package service

import (
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/portal-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/portal-svc/internal/store"
)

func fixedTime() time.Time {
	return time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)
}

func setupTestService() (*Service, *store.MemoryStore) {
	s := store.NewMemoryStore()
	svc := NewServiceWithClock(s, fixedTime)
	return svc, s
}

func TestCreateRequest(t *testing.T) {
	svc, _ := setupTestService()

	req, err := svc.CreateRequest("tenant-1", "user-1", "Need new VM", "For production workload", "vm", 4, 16, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.ID == "" {
		t.Error("expected non-empty ID")
	}
	if req.Status != models.StatusDraft {
		t.Errorf("expected status %s, got %s", models.StatusDraft, req.Status)
	}
	if req.TenantID != "tenant-1" {
		t.Errorf("expected tenant-1, got %s", req.TenantID)
	}
	if req.CostEstimate <= 0 {
		t.Errorf("expected positive cost estimate, got %f", req.CostEstimate)
	}
}

func TestCreateRequestInvalidInput(t *testing.T) {
	svc, _ := setupTestService()

	_, err := svc.CreateRequest("", "user-1", "title", "", "vm", 0, 0, 0)
	if err == nil {
		t.Error("expected error for empty tenantID")
	}

	_, err = svc.CreateRequest("t1", "", "title", "", "vm", 0, 0, 0)
	if err == nil {
		t.Error("expected error for empty requester")
	}

	_, err = svc.CreateRequest("t1", "u1", "", "", "vm", 0, 0, 0)
	if err == nil {
		t.Error("expected error for empty title")
	}
}

func TestGetRequest(t *testing.T) {
	svc, _ := setupTestService()

	req, _ := svc.CreateRequest("t1", "u1", "title", "desc", "vm", 2, 8, 50)
	got, err := svc.GetRequest(req.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != req.ID {
		t.Errorf("expected ID %s, got %s", req.ID, got.ID)
	}
}

func TestGetRequestNotFound(t *testing.T) {
	svc, _ := setupTestService()

	_, err := svc.GetRequest("nonexistent")
	if err != ErrRequestNotFound {
		t.Errorf("expected ErrRequestNotFound, got %v", err)
	}
}

func TestListRequests(t *testing.T) {
	svc, _ := setupTestService()

	svc.CreateRequest("t1", "u1", "r1", "", "vm", 1, 4, 50)
	svc.CreateRequest("t1", "u2", "r2", "", "vm", 2, 8, 100)
	svc.CreateRequest("t2", "u3", "r3", "", "vm", 4, 16, 200)

	all := svc.ListRequests("", "")
	if len(all) != 3 {
		t.Errorf("expected 3 requests, got %d", len(all))
	}

	t1Requests := svc.ListRequests("t1", "")
	if len(t1Requests) != 2 {
		t.Errorf("expected 2 requests for t1, got %d", len(t1Requests))
	}
}

func TestUpdateRequest(t *testing.T) {
	svc, _ := setupTestService()

	req, _ := svc.CreateRequest("t1", "u1", "title", "desc", "vm", 2, 8, 50)
	updated, err := svc.UpdateRequest(req.ID, "new title", "new desc", 4, 16, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Title != "new title" {
		t.Errorf("expected 'new title', got '%s'", updated.Title)
	}
	if updated.CPU != 4 {
		t.Errorf("expected CPU 4, got %d", updated.CPU)
	}
}

func TestUpdateRequestNotFound(t *testing.T) {
	svc, _ := setupTestService()

	_, err := svc.UpdateRequest("nonexistent", "title", "", 0, 0, 0)
	if err != ErrRequestNotFound {
		t.Errorf("expected ErrRequestNotFound, got %v", err)
	}
}

func TestSubmitRequest(t *testing.T) {
	svc, _ := setupTestService()

	req, _ := svc.CreateRequest("t1", "u1", "title", "", "vm", 2, 8, 50)
	submitted, err := svc.SubmitRequest(req.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if submitted.Status != models.StatusPending {
		t.Errorf("expected status %s, got %s", models.StatusPending, submitted.Status)
	}
}

func TestSubmitNonDraftRequest(t *testing.T) {
	svc, _ := setupTestService()

	req, _ := svc.CreateRequest("t1", "u1", "title", "", "vm", 2, 8, 50)
	svc.SubmitRequest(req.ID)

	_, err := svc.SubmitRequest(req.ID)
	if err == nil {
		t.Error("expected error when submitting non-draft request")
	}
}

func TestApproveRequest(t *testing.T) {
	svc, _ := setupTestService()

	req, _ := svc.CreateRequest("t1", "u1", "title", "", "vm", 2, 8, 50)
	svc.SubmitRequest(req.ID)

	approved, err := svc.ApproveRequest(req.ID, "admin", "Looks good")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approved.Status != models.StatusApproved {
		t.Errorf("expected status %s, got %s", models.StatusApproved, approved.Status)
	}
	if approved.Approver != "admin" {
		t.Errorf("expected approver 'admin', got '%s'", approved.Approver)
	}
}

func TestRejectRequest(t *testing.T) {
	svc, _ := setupTestService()

	req, _ := svc.CreateRequest("t1", "u1", "title", "", "vm", 2, 8, 50)
	svc.SubmitRequest(req.ID)

	rejected, err := svc.RejectRequest(req.ID, "admin", "Too expensive")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rejected.Status != models.StatusRejected {
		t.Errorf("expected status %s, got %s", models.StatusRejected, rejected.Status)
	}
}

func TestFulfillRequest(t *testing.T) {
	svc, _ := setupTestService()

	req, _ := svc.CreateRequest("t1", "u1", "title", "", "vm", 2, 8, 50)
	svc.SubmitRequest(req.ID)
	svc.ApproveRequest(req.ID, "admin", "")

	fulfilled, err := svc.FulfillRequest(req.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fulfilled.Status != models.StatusFulfilled {
		t.Errorf("expected status %s, got %s", models.StatusFulfilled, fulfilled.Status)
	}
}

func TestCancelRequest(t *testing.T) {
	svc, _ := setupTestService()

	req, _ := svc.CreateRequest("t1", "u1", "title", "", "vm", 2, 8, 50)
	cancelled, err := svc.CancelRequest(req.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cancelled.Status != models.StatusCancelled {
		t.Errorf("expected status %s, got %s", models.StatusCancelled, cancelled.Status)
	}
}

func TestRequestLifecycle(t *testing.T) {
	svc, _ := setupTestService()

	req, _ := svc.CreateRequest("t1", "u1", "title", "", "vm", 2, 8, 50)
	if req.Status != models.StatusDraft {
		t.Fatal("expected draft status")
	}

	req, _ = svc.SubmitRequest(req.ID)
	if req.Status != models.StatusPending {
		t.Fatal("expected pending status")
	}

	req, _ = svc.ApproveRequest(req.ID, "admin", "")
	if req.Status != models.StatusApproved {
		t.Fatal("expected approved status")
	}

	req, _ = svc.FulfillRequest(req.ID)
	if req.Status != models.StatusFulfilled {
		t.Fatal("expected fulfilled status")
	}
}

func TestQuotaManagement(t *testing.T) {
	svc, _ := setupTestService()

	_, err := svc.UpdateQuota("t1", 100, 256, 1000, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	q, err := svc.GetQuota("t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q.MaxCPU != 100 {
		t.Errorf("expected MaxCPU 100, got %d", q.MaxCPU)
	}
	if q.MaxMemoryGB != 256 {
		t.Errorf("expected MaxMemoryGB 256, got %d", q.MaxMemoryGB)
	}
}

func TestQuotaNotFound(t *testing.T) {
	svc, _ := setupTestService()

	_, err := svc.GetQuota("nonexistent")
	if err != ErrQuotaNotFound {
		t.Errorf("expected ErrQuotaNotFound, got %v", err)
	}
}

func TestQuotaUsage(t *testing.T) {
	svc, _ := setupTestService()

	svc.UpdateQuota("t1", 100, 256, 1000, 50)
	svc.CreateRequest("t1", "u1", "r1", "", "vm", 4, 16, 100)

	usage, err := svc.GetQuotaUsage("t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if usage.Quota.MaxCPU != 100 {
		t.Errorf("expected MaxCPU 100, got %d", usage.Quota.MaxCPU)
	}
}

func TestQuotaExceeded(t *testing.T) {
	svc, _ := setupTestService()

	svc.UpdateQuota("t1", 4, 16, 100, 10)
	req, _ := svc.CreateRequest("t1", "u1", "r1", "", "vm", 4, 16, 100)
	svc.SubmitRequest(req.ID)
	svc.ApproveRequest(req.ID, "admin", "")

	// This request should exceed quota
	req2, _ := svc.CreateRequest("t1", "u2", "r2", "", "vm", 8, 32, 200)
	_, err := svc.SubmitRequest(req2.ID)
	if err == nil {
		t.Error("expected quota exceeded error")
	}
}

func TestBudgetManagement(t *testing.T) {
	svc, _ := setupTestService()

	b, err := svc.SetBudget("t1", 5000.0, 0.8)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.MonthlyLimit != 5000.0 {
		t.Errorf("expected limit 5000, got %f", b.MonthlyLimit)
	}

	got, err := svc.GetBudget("t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.MonthlyLimit != 5000.0 {
		t.Errorf("expected limit 5000, got %f", got.MonthlyLimit)
	}
}

func TestBudgetNotFound(t *testing.T) {
	svc, _ := setupTestService()

	_, err := svc.GetBudget("nonexistent")
	if err != ErrBudgetNotFound {
		t.Errorf("expected ErrBudgetNotFound, got %v", err)
	}
}

func TestBudgetInvalidInput(t *testing.T) {
	svc, _ := setupTestService()

	_, err := svc.SetBudget("t1", 0, 0.8)
	if err == nil {
		t.Error("expected error for zero monthly limit")
	}

	_, err = svc.SetBudget("", 1000, 0.8)
	if err == nil {
		t.Error("expected error for empty tenantID")
	}
}

func TestCostRecommendations(t *testing.T) {
	svc, _ := setupTestService()

	recs := svc.GetRecommendations("t1")
	if len(recs) == 0 {
		t.Error("expected recommendations")
	}

	// Verify sorted by savings (highest first)
	for i := 1; i < len(recs); i++ {
		if recs[i].Savings > recs[i-1].Savings {
			t.Error("recommendations not sorted by savings descending")
		}
	}
}

func TestSavingsAnalysis(t *testing.T) {
	svc, _ := setupTestService()

	svc.SetBudget("t1", 5000, 0.8)
	analysis, err := svc.GetSavingsAnalysis("t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if analysis.PotentialSavings <= 0 {
		t.Error("expected positive potential savings")
	}
	if analysis.TenantID != "t1" {
		t.Errorf("expected tenant t1, got %s", analysis.TenantID)
	}
}

func TestUtilization(t *testing.T) {
	svc, _ := setupTestService()

	u, err := svc.GetUtilization("t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.TenantID != "t1" {
		t.Errorf("expected tenant t1, got %s", u.TenantID)
	}
}

func TestDashboardStats(t *testing.T) {
	svc, _ := setupTestService()

	svc.CreateRequest("t1", "u1", "r1", "", "vm", 2, 8, 50)
	req2, _ := svc.CreateRequest("t1", "u1", "r2", "", "vm", 4, 16, 100)
	svc.SubmitRequest(req2.ID)
	svc.ApproveRequest(req2.ID, "admin", "")

	stats := svc.GetDashboardStats("t1")
	if stats.TotalRequests != 2 {
		t.Errorf("expected 2 total requests, got %d", stats.TotalRequests)
	}
	if stats.PendingRequests != 0 {
		t.Errorf("expected 0 pending, got %d", stats.PendingRequests)
	}
	if stats.ApprovedRequests != 1 {
		t.Errorf("expected 1 approved, got %d", stats.ApprovedRequests)
	}
}

func TestRecentActivity(t *testing.T) {
	svc, _ := setupTestService()

	svc.CreateRequest("t1", "u1", "r1", "", "vm", 2, 8, 50)
	activity := svc.GetRecentActivity("t1", 10)
	if len(activity) == 0 {
		t.Error("expected activity events")
	}
}

func TestEstimateCost(t *testing.T) {
	cost := estimateCost(4, 16, 100)
	expected := 4*0.05 + 16*0.01 + 100*0.001
	if abs(cost-expected) > 1e-9 {
		t.Errorf("expected cost %f, got %f", expected, cost)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func TestListQuotas(t *testing.T) {
	svc, _ := setupTestService()

	svc.UpdateQuota("t1", 100, 256, 1000, 50)
	svc.UpdateQuota("t2", 200, 512, 2000, 100)

	quotas := svc.ListQuotas()
	if len(quotas) != 2 {
		t.Errorf("expected 2 quotas, got %d", len(quotas))
	}
}

func TestInvalidTransitionOnApproved(t *testing.T) {
	svc, _ := setupTestService()

	req, _ := svc.CreateRequest("t1", "u1", "title", "", "vm", 2, 8, 50)
	svc.SubmitRequest(req.ID)
	svc.ApproveRequest(req.ID, "admin", "")

	_, err := svc.CancelRequest(req.ID)
	if err == nil {
		t.Error("expected error when canceling approved request")
	}
}
