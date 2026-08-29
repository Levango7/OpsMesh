package scheduler

import (
	"testing"

	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/models"
)

type mockNodeProvider struct {
	nodes []*models.GPUNode
}

func (m *mockNodeProvider) GetOnlineNodes() []*models.GPUNode {
	return m.nodes
}

func (m *mockNodeProvider) Get(id string) (*models.GPUNode, error) {
	for _, n := range m.nodes {
		if n.ID == id {
			return n, nil
		}
	}
	return nil, ErrNoSuitableNode
}

func sampleGPUNode(id string, gpuCount int, usedVRAM int) *models.GPUNode {
	gpus := make([]models.GPUInfo, gpuCount)
	for i := 0; i < gpuCount; i++ {
		gpus[i] = models.GPUInfo{
			Index:  i,
			Model:  "A100",
			VRAMMB: 81920,
			Vendor: models.GPUVendorNVIDIA,
		}
	}
	return &models.GPUNode{
		ID:          id,
		Name:        "node-" + id,
		GPUs:        gpus,
		Status:      models.NodeStatusOnline,
		TotalVRAMMB: gpuCount * 81920,
		UsedVRAMMB:  usedVRAM,
	}
}

func TestSchedule(t *testing.T) {
	s := NewScheduler(nil)
	provider := &mockNodeProvider{
		nodes: []*models.GPUNode{
			sampleGPUNode("node-1", 4, 0),
		},
	}

	workload := &models.Workload{
		ID:         "wl-1",
		GPURequest: models.GPURequest{Count: 2, MinVRAMMB: 4096},
		Priority:   5,
	}

	result, err := s.Schedule(workload, provider)
	if err != nil {
		t.Fatalf("Schedule failed: %v", err)
	}
	if !result.Assigned {
		t.Error("expected workload to be assigned")
	}
	if len(result.NodeIDs) == 0 {
		t.Error("expected at least one node assigned")
	}
}

func TestScheduleNoNodes(t *testing.T) {
	s := NewScheduler(nil)
	provider := &mockNodeProvider{nodes: []*models.GPUNode{}}

	workload := &models.Workload{
		ID:         "wl-1",
		GPURequest: models.GPURequest{Count: 1},
	}

	_, err := s.Schedule(workload, provider)
	if err != ErrNoSuitableNode {
		t.Fatalf("expected ErrNoSuitableNode, got: %v", err)
	}
}

func TestScheduleInsufficientGPUs(t *testing.T) {
	s := NewScheduler(nil)
	provider := &mockNodeProvider{
		nodes: []*models.GPUNode{
			sampleGPUNode("node-1", 1, 0),
		},
	}

	workload := &models.Workload{
		ID:         "wl-1",
		GPURequest: models.GPURequest{Count: 4},
	}

	_, err := s.Schedule(workload, provider)
	if err != ErrNoSuitableNode {
		t.Fatalf("expected ErrNoSuitableNode, got: %v", err)
	}
}

func TestScheduleNilWorkload(t *testing.T) {
	s := NewScheduler(nil)
	provider := &mockNodeProvider{nodes: []*models.GPUNode{}}

	_, err := s.Schedule(nil, provider)
	if err == nil {
		t.Fatal("expected error for nil workload")
	}
}

func TestGetPolicies(t *testing.T) {
	s := NewScheduler(nil)
	policies := s.GetPolicies()
	if len(policies) == 0 {
		t.Error("expected default policies")
	}
}

func TestSetPolicies(t *testing.T) {
	s := NewScheduler(nil)
	newPolicies := []models.SchedulingPolicy{
		{Name: "spreading", Type: "spreading", Enabled: true, PriorityWeight: 10},
	}
	if err := s.SetPolicies(newPolicies); err != nil {
		t.Fatalf("SetPolicies failed: %v", err)
	}
	policies := s.GetPolicies()
	if len(policies) != 1 {
		t.Errorf("expected 1 policy, got %d", len(policies))
	}
	if policies[0].Type != "spreading" {
		t.Errorf("expected spreading policy, got %s", policies[0].Type)
	}
}

func TestSetPoliciesInvalid(t *testing.T) {
	s := NewScheduler(nil)
	err := s.SetPolicies([]models.SchedulingPolicy{
		{Name: "bad", Type: "invalid_type"},
	})
	if err != ErrInvalidPolicy {
		t.Fatalf("expected ErrInvalidPolicy, got: %v", err)
	}
}

func TestGetQueue(t *testing.T) {
	s := NewScheduler(nil)
	queue := s.GetQueue()
	if queue == nil {
		t.Error("expected non-nil queue")
	}
}

func TestBinPackingSort(t *testing.T) {
	s := NewScheduler(nil)
	s.SetPolicies([]models.SchedulingPolicy{
		{Name: "bin-packing", Type: "bin_packing", Enabled: true, PriorityWeight: 10},
	})

	provider := &mockNodeProvider{
		nodes: []*models.GPUNode{
			sampleGPUNode("node-1", 4, 100000),
			sampleGPUNode("node-2", 4, 50000),
		},
	}

	workload := &models.Workload{
		ID:         "wl-1",
		GPURequest: models.GPURequest{Count: 1},
	}

	result, err := s.Schedule(workload, provider)
	if err != nil {
		t.Fatalf("Schedule failed: %v", err)
	}
	// bin-packing should prefer node with more used VRAM (node-1)
	if result.NodeIDs[0] != "node-1" {
		t.Errorf("expected node-1 first (bin-packing), got %s", result.NodeIDs[0])
	}
}

func TestSpreadingSort(t *testing.T) {
	s := NewScheduler(nil)
	s.SetPolicies([]models.SchedulingPolicy{
		{Name: "spreading", Type: "spreading", Enabled: true, PriorityWeight: 10},
	})

	provider := &mockNodeProvider{
		nodes: []*models.GPUNode{
			sampleGPUNode("node-1", 4, 100000),
			sampleGPUNode("node-2", 4, 50000),
		},
	}

	workload := &models.Workload{
		ID:         "wl-1",
		GPURequest: models.GPURequest{Count: 1},
	}

	result, err := s.Schedule(workload, provider)
	if err != nil {
		t.Fatalf("Schedule failed: %v", err)
	}
	// spreading should prefer node with less used VRAM (node-2)
	if result.NodeIDs[0] != "node-2" {
		t.Errorf("expected node-2 first (spreading), got %s", result.NodeIDs[0])
	}
}
