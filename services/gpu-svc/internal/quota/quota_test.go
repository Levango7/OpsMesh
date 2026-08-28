package quota

import (
	"testing"

	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/models"
)

func TestSetQuota(t *testing.T) {
	mgr := NewManager(nil)
	q := &models.GPUQuota{
		TenantID:     "tenant-1",
		MaxGPUs:      8,
		MaxVRAMMB:    655360,
		MaxWorkloads: 10,
		Priority:     5,
	}

	if err := mgr.SetQuota(q); err != nil {
		t.Fatalf("SetQuota failed: %v", err)
	}
	if q.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestSetQuotaInvalid(t *testing.T) {
	mgr := NewManager(nil)
	err := mgr.SetQuota(nil)
	if err != ErrInvalidQuota {
		t.Fatalf("expected ErrInvalidQuota, got: %v", err)
	}

	err = mgr.SetQuota(&models.GPUQuota{TenantID: ""})
	if err != ErrInvalidQuota {
		t.Fatalf("expected ErrInvalidQuota for empty tenant, got: %v", err)
	}

	err = mgr.SetQuota(&models.GPUQuota{TenantID: "t1", MaxGPUs: -1})
	if err != ErrInvalidQuota {
		t.Fatalf("expected ErrInvalidQuota for negative max, got: %v", err)
	}
}

func TestGetQuota(t *testing.T) {
	mgr := NewManager(nil)
	q := &models.GPUQuota{TenantID: "tenant-1", MaxGPUs: 4}
	mgr.SetQuota(q)

	got, err := mgr.GetQuota("tenant-1")
	if err != nil {
		t.Fatalf("GetQuota failed: %v", err)
	}
	if got.MaxGPUs != 4 {
		t.Errorf("expected max 4 GPUs, got %d", got.MaxGPUs)
	}
}

func TestGetQuotaNotFound(t *testing.T) {
	mgr := NewManager(nil)
	_, err := mgr.GetQuota("nonexistent")
	if err != ErrQuotaNotFound {
		t.Fatalf("expected ErrQuotaNotFound, got: %v", err)
	}
}

func TestListQuotas(t *testing.T) {
	mgr := NewManager(nil)
	mgr.SetQuota(&models.GPUQuota{TenantID: "t1", MaxGPUs: 4})
	mgr.SetQuota(&models.GPUQuota{TenantID: "t2", MaxGPUs: 8})

	quotas := mgr.ListQuotas()
	if len(quotas) != 2 {
		t.Errorf("expected 2 quotas, got %d", len(quotas))
	}
}

func TestDeleteQuota(t *testing.T) {
	mgr := NewManager(nil)
	mgr.SetQuota(&models.GPUQuota{TenantID: "t1", MaxGPUs: 4})

	if err := mgr.DeleteQuota("t1"); err != nil {
		t.Fatalf("DeleteQuota failed: %v", err)
	}

	_, err := mgr.GetQuota("t1")
	if err != ErrQuotaNotFound {
		t.Fatalf("expected ErrQuotaNotFound after delete, got: %v", err)
	}
}

func TestDeleteQuotaNotFound(t *testing.T) {
	mgr := NewManager(nil)
	err := mgr.DeleteQuota("nonexistent")
	if err != ErrQuotaNotFound {
		t.Fatalf("expected ErrQuotaNotFound, got: %v", err)
	}
}

func TestCheckAllocation(t *testing.T) {
	mgr := NewManager(nil)
	mgr.SetQuota(&models.GPUQuota{
		TenantID:     "t1",
		MaxGPUs:      8,
		MaxVRAMMB:    655360,
		MaxWorkloads: 5,
		UsedGPUs:     4,
		UsedVRAMMB:   327680,
		UsedWorkloads: 2,
	})

	// Should pass: 4+2 <= 8
	err := mgr.CheckAllocation("t1", 2, 163840, 1)
	if err != nil {
		t.Fatalf("CheckAllocation should pass: %v", err)
	}

	// Should fail: 4+5 > 8
	err = mgr.CheckAllocation("t1", 5, 163840, 1)
	if err != ErrQuotaExceeded {
		t.Fatalf("expected ErrQuotaExceeded, got: %v", err)
	}
}

func TestCheckAllocationNoQuota(t *testing.T) {
	mgr := NewManager(nil)
	// No quota set for tenant - should pass
	err := mgr.CheckAllocation("no-quota-tenant", 100, 1000000, 50)
	if err != nil {
		t.Fatalf("expected no error for tenant without quota, got: %v", err)
	}
}

func TestRecordAllocation(t *testing.T) {
	mgr := NewManager(nil)
	mgr.SetQuota(&models.GPUQuota{
		TenantID: "t1", MaxGPUs: 8, MaxVRAMMB: 655360, MaxWorkloads: 5,
	})

	if err := mgr.RecordAllocation("t1", 2, 163840, 1); err != nil {
		t.Fatalf("RecordAllocation failed: %v", err)
	}

	usage, _ := mgr.GetUsage("t1")
	if usage.UsedGPUs != 2 {
		t.Errorf("expected 2 used GPUs, got %d", usage.UsedGPUs)
	}
	if usage.UsedVRAMMB != 163840 {
		t.Errorf("expected 163840 used VRAM, got %d", usage.UsedVRAMMB)
	}
}

func TestReleaseAllocation(t *testing.T) {
	mgr := NewManager(nil)
	mgr.SetQuota(&models.GPUQuota{
		TenantID: "t1", MaxGPUs: 8, MaxVRAMMB: 655360, MaxWorkloads: 5,
		UsedGPUs: 4, UsedVRAMMB: 327680, UsedWorkloads: 2,
	})

	if err := mgr.ReleaseAllocation("t1", 2, 163840, 1); err != nil {
		t.Fatalf("ReleaseAllocation failed: %v", err)
	}

	usage, _ := mgr.GetUsage("t1")
	if usage.UsedGPUs != 2 {
		t.Errorf("expected 2 used GPUs after release, got %d", usage.UsedGPUs)
	}
}

func TestReleaseAllocationClampsToZero(t *testing.T) {
	mgr := NewManager(nil)
	mgr.SetQuota(&models.GPUQuota{
		TenantID: "t1", MaxGPUs: 8, MaxVRAMMB: 655360, MaxWorkloads: 5,
		UsedGPUs: 1,
	})

	mgr.ReleaseAllocation("t1", 10, 1000, 5)
	usage, _ := mgr.GetUsage("t1")
	if usage.UsedGPUs != 0 {
		t.Errorf("expected 0 used GPUs (clamped), got %d", usage.UsedGPUs)
	}
}

func TestGetUsage(t *testing.T) {
	mgr := NewManager(nil)
	mgr.SetQuota(&models.GPUQuota{
		TenantID: "t1", MaxGPUs: 8, MaxVRAMMB: 655360, MaxWorkloads: 5,
		UsedGPUs: 3, UsedVRAMMB: 200000, UsedWorkloads: 2,
	})

	usage, err := mgr.GetUsage("t1")
	if err != nil {
		t.Fatalf("GetUsage failed: %v", err)
	}
	if usage.MaxGPUs != 8 {
		t.Errorf("expected max 8, got %d", usage.MaxGPUs)
	}
	if usage.UsedGPUs != 3 {
		t.Errorf("expected 3 used, got %d", usage.UsedGPUs)
	}
}

func TestGetUsageNotFound(t *testing.T) {
	mgr := NewManager(nil)
	_, err := mgr.GetUsage("nonexistent")
	if err != ErrQuotaNotFound {
		t.Fatalf("expected ErrQuotaNotFound, got: %v", err)
	}
}

func TestUpdateQuota(t *testing.T) {
	mgr := NewManager(nil)
	mgr.SetQuota(&models.GPUQuota{TenantID: "t1", MaxGPUs: 4})

	updated := &models.GPUQuota{TenantID: "t1", MaxGPUs: 16}
	if err := mgr.SetQuota(updated); err != nil {
		t.Fatalf("SetQuota update failed: %v", err)
	}

	got, _ := mgr.GetQuota("t1")
	if got.MaxGPUs != 16 {
		t.Errorf("expected updated max 16, got %d", got.MaxGPUs)
	}
}
