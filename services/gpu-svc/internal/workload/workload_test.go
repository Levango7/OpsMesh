package workload

import (
	"testing"

	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/models"
)

func sampleWorkload() *models.Workload {
	return &models.Workload{
		Name:     "train-llama",
		TenantID: "tenant-1",
		Type:     models.WorkloadTypeTraining,
		GPURequest: models.GPURequest{
			Count:     4,
			MinVRAMMB: 40960,
		},
		Priority: 10,
	}
}

func TestSubmit(t *testing.T) {
	mgr := NewManager(nil)
	wl := sampleWorkload()

	if err := mgr.Submit(wl); err != nil {
		t.Fatalf("Submit failed: %v", err)
	}
	if wl.ID == "" {
		t.Error("expected workload ID to be set")
	}
	if wl.Status != models.WorkloadStatusPending {
		t.Errorf("expected status pending, got %s", wl.Status)
	}
	if wl.Replicas != 1 {
		t.Errorf("expected default replicas 1, got %d", wl.Replicas)
	}
}

func TestSubmitInvalid(t *testing.T) {
	mgr := NewManager(nil)
	err := mgr.Submit(nil)
	if err != ErrWorkloadInvalid {
		t.Fatalf("expected ErrWorkloadInvalid, got: %v", err)
	}

	err = mgr.Submit(&models.Workload{Name: "", TenantID: "t1"})
	if err != ErrWorkloadInvalid {
		t.Fatalf("expected ErrWorkloadInvalid for empty name, got: %v", err)
	}

	err = mgr.Submit(&models.Workload{Name: "test", TenantID: ""})
	if err != ErrWorkloadInvalid {
		t.Fatalf("expected ErrWorkloadInvalid for empty tenant, got: %v", err)
	}
}

func TestGet(t *testing.T) {
	mgr := NewManager(nil)
	wl := sampleWorkload()
	mgr.Submit(wl)

	got, err := mgr.Get(wl.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Name != "train-llama" {
		t.Errorf("expected name train-llama, got %s", got.Name)
	}
}

func TestGetNotFound(t *testing.T) {
	mgr := NewManager(nil)
	_, err := mgr.Get("nonexistent")
	if err != ErrWorkloadNotFound {
		t.Fatalf("expected ErrWorkloadNotFound, got: %v", err)
	}
}

func TestList(t *testing.T) {
	mgr := NewManager(nil)
	for i := 0; i < 3; i++ {
		wl := sampleWorkload()
		wl.Name = "wl-" + string(rune('A'+i))
		mgr.Submit(wl)
	}

	workloads := mgr.List("")
	if len(workloads) != 3 {
		t.Errorf("expected 3 workloads, got %d", len(workloads))
	}
}

func TestListByStatus(t *testing.T) {
	mgr := NewManager(nil)
	wl1 := sampleWorkload()
	wl1.Name = "wl-1"
	mgr.Submit(wl1)
	wl2 := sampleWorkload()
	wl2.Name = "wl-2"
	mgr.Submit(wl2)
	mgr.MarkFailed(wl2.ID, "oom")

	pending := mgr.List(models.WorkloadStatusPending)
	if len(pending) != 1 {
		t.Errorf("expected 1 pending, got %d", len(pending))
	}
	failed := mgr.List(models.WorkloadStatusFailed)
	if len(failed) != 1 {
		t.Errorf("expected 1 failed, got %d", len(failed))
	}
}

func TestCancel(t *testing.T) {
	mgr := NewManager(nil)
	wl := sampleWorkload()
	mgr.Submit(wl)

	if err := mgr.Cancel(wl.ID); err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}

	got, _ := mgr.Get(wl.ID)
	if got.Status != models.WorkloadStatusCancelled {
		t.Errorf("expected cancelled status, got %s", got.Status)
	}
	if got.FinishedAt == nil {
		t.Error("expected FinishedAt to be set after cancel")
	}
}

func TestCancelNotFound(t *testing.T) {
	mgr := NewManager(nil)
	err := mgr.Cancel("nonexistent")
	if err != ErrWorkloadNotFound {
		t.Fatalf("expected ErrWorkloadNotFound, got: %v", err)
	}
}

func TestCancelInvalidTransition(t *testing.T) {
	mgr := NewManager(nil)
	wl := sampleWorkload()
	mgr.Submit(wl)
	mgr.MarkCompleted(wl.ID)

	err := mgr.Cancel(wl.ID)
	if err != ErrInvalidStateTransition {
		t.Fatalf("expected ErrInvalidStateTransition, got: %v", err)
	}
}

func TestScale(t *testing.T) {
	mgr := NewManager(nil)
	wl := sampleWorkload()
	mgr.Submit(wl)

	if err := mgr.Scale(wl.ID, 5); err != nil {
		t.Fatalf("Scale failed: %v", err)
	}

	got, _ := mgr.Get(wl.ID)
	if got.Replicas != 5 {
		t.Errorf("expected 5 replicas, got %d", got.Replicas)
	}
}

func TestScaleNegative(t *testing.T) {
	mgr := NewManager(nil)
	wl := sampleWorkload()
	mgr.Submit(wl)

	err := mgr.Scale(wl.ID, -1)
	if err != ErrWorkloadInvalid {
		t.Fatalf("expected ErrWorkloadInvalid, got: %v", err)
	}
}

func TestScaleNotFound(t *testing.T) {
	mgr := NewManager(nil)
	err := mgr.Scale("nonexistent", 2)
	if err != ErrWorkloadNotFound {
		t.Fatalf("expected ErrWorkloadNotFound, got: %v", err)
	}
}

func TestAssignNode(t *testing.T) {
	mgr := NewManager(nil)
	wl := sampleWorkload()
	mgr.Submit(wl)

	if err := mgr.AssignNode(wl.ID, []string{"node-1"}); err != nil {
		t.Fatalf("AssignNode failed: %v", err)
	}

	got, _ := mgr.Get(wl.ID)
	if got.Status != models.WorkloadStatusRunning {
		t.Errorf("expected running status, got %s", got.Status)
	}
	if len(got.NodeIDs) != 1 {
		t.Errorf("expected 1 node, got %d", len(got.NodeIDs))
	}
	if got.StartedAt == nil {
		t.Error("expected StartedAt to be set")
	}
}

func TestMarkCompleted(t *testing.T) {
	mgr := NewManager(nil)
	wl := sampleWorkload()
	mgr.Submit(wl)
	mgr.AssignNode(wl.ID, []string{"node-1"})

	if err := mgr.MarkCompleted(wl.ID); err != nil {
		t.Fatalf("MarkCompleted failed: %v", err)
	}

	got, _ := mgr.Get(wl.ID)
	if got.Status != models.WorkloadStatusCompleted {
		t.Errorf("expected completed status, got %s", got.Status)
	}
}

func TestMarkFailed(t *testing.T) {
	mgr := NewManager(nil)
	wl := sampleWorkload()
	mgr.Submit(wl)
	mgr.AssignNode(wl.ID, []string{"node-1"})

	if err := mgr.MarkFailed(wl.ID, "out of memory"); err != nil {
		t.Fatalf("MarkFailed failed: %v", err)
	}

	got, _ := mgr.Get(wl.ID)
	if got.Status != models.WorkloadStatusFailed {
		t.Errorf("expected failed status, got %s", got.Status)
	}
	if got.ErrorMsg != "out of memory" {
		t.Errorf("expected error msg 'out of memory', got %s", got.ErrorMsg)
	}
}

func TestInvalidStateTransition(t *testing.T) {
	mgr := NewManager(nil)
	wl := sampleWorkload()
	mgr.Submit(wl)
	mgr.MarkCompleted(wl.ID)

	err := mgr.AssignNode(wl.ID, []string{"node-1"})
	if err != ErrInvalidStateTransition {
		t.Fatalf("expected ErrInvalidStateTransition, got: %v", err)
	}
}

func TestGetRunningWorkloads(t *testing.T) {
	mgr := NewManager(nil)
	wl1 := sampleWorkload()
	wl1.Name = "wl-running"
	mgr.Submit(wl1)
	mgr.AssignNode(wl1.ID, []string{"node-1"})

	wl2 := sampleWorkload()
	wl2.Name = "wl-pending"
	mgr.Submit(wl2)

	running := mgr.GetRunningWorkloads()
	if len(running) != 1 {
		t.Errorf("expected 1 running, got %d", len(running))
	}
}

func TestGetPendingWorkloads(t *testing.T) {
	mgr := NewManager(nil)
	wl := sampleWorkload()
	mgr.Submit(wl)

	pending := mgr.GetPendingWorkloads()
	if len(pending) != 1 {
		t.Errorf("expected 1 pending, got %d", len(pending))
	}
}
