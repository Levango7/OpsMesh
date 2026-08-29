package service

import (
	"testing"

	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/metrics"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/node"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/ollama"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/quota"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/scheduler"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/workload"
)

func newTestService() *Service {
	return NewService(
		node.NewManager(nil),
		scheduler.NewScheduler(nil),
		workload.NewManager(nil),
		ollama.NewClient("http://localhost:11434", 0),
		quota.NewManager(nil),
		metrics.NewCollector(nil),
	)
}

func TestRegisterNode(t *testing.T) {
	svc := newTestService()
	n := &models.GPUNode{
		Name: "test-node",
		GPUs: []models.GPUInfo{{Index: 0, Model: "A100", VRAMMB: 81920, Vendor: models.GPUVendorNVIDIA}},
	}

	registered, err := svc.RegisterNode(n)
	if err != nil {
		t.Fatalf("RegisterNode failed: %v", err)
	}
	if registered.ID == "" {
		t.Error("expected node ID to be set")
	}
}

func TestGetNode(t *testing.T) {
	svc := newTestService()
	n := &models.GPUNode{Name: "n1", GPUs: []models.GPUInfo{{Index: 0, VRAMMB: 81920}}}
	svc.RegisterNode(n)

	got, err := svc.GetNode(n.ID)
	if err != nil {
		t.Fatalf("GetNode failed: %v", err)
	}
	if got.Name != "n1" {
		t.Errorf("expected n1, got %s", got.Name)
	}
}

func TestListNodes(t *testing.T) {
	svc := newTestService()
	svc.RegisterNode(&models.GPUNode{Name: "n1", GPUs: []models.GPUInfo{{Index: 0}}})
	svc.RegisterNode(&models.GPUNode{Name: "n2", GPUs: []models.GPUInfo{{Index: 0}}})

	nodes := svc.ListNodes()
	if len(nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(nodes))
	}
}

func TestUnregisterNode(t *testing.T) {
	svc := newTestService()
	n := &models.GPUNode{Name: "n1", GPUs: []models.GPUInfo{{Index: 0}}}
	svc.RegisterNode(n)

	if err := svc.UnregisterNode(n.ID); err != nil {
		t.Fatalf("UnregisterNode failed: %v", err)
	}

	_, err := svc.GetNode(n.ID)
	if err == nil {
		t.Error("expected error after unregister")
	}
}

func TestGetResourceSummary(t *testing.T) {
	svc := newTestService()
	n := &models.GPUNode{
		Name: "n1",
		GPUs: []models.GPUInfo{
			{Index: 0, VRAMMB: 81920},
			{Index: 1, VRAMMB: 81920},
		},
	}
	svc.RegisterNode(n)

	summary := svc.GetResourceSummary()
	if summary.TotalGPUs != 2 {
		t.Errorf("expected 2 total GPUs, got %d", summary.TotalGPUs)
	}
	if summary.OnlineNodes != 1 {
		t.Errorf("expected 1 online node, got %d", summary.OnlineNodes)
	}
}

func TestSubmitWorkload(t *testing.T) {
	svc := newTestService()
	wl := &models.Workload{
		Name:       "train",
		TenantID:   "t1",
		GPURequest: models.GPURequest{Count: 2, MinVRAMMB: 40960},
	}

	submitted, err := svc.SubmitWorkload(wl)
	if err != nil {
		t.Fatalf("SubmitWorkload failed: %v", err)
	}
	if submitted.ID == "" {
		t.Error("expected workload ID to be set")
	}
}

func TestCancelWorkload(t *testing.T) {
	svc := newTestService()
	wl := &models.Workload{
		Name:       "train",
		TenantID:   "t1",
		GPURequest: models.GPURequest{Count: 2},
	}
	svc.SubmitWorkload(wl)

	if err := svc.CancelWorkload(wl.ID); err != nil {
		t.Fatalf("CancelWorkload failed: %v", err)
	}

	got, _ := svc.GetWorkload(wl.ID)
	if got.Status != models.WorkloadStatusCancelled {
		t.Errorf("expected cancelled, got %s", got.Status)
	}
}

func TestListWorkloads(t *testing.T) {
	svc := newTestService()
	svc.SubmitWorkload(&models.Workload{Name: "w1", TenantID: "t1", GPURequest: models.GPURequest{Count: 1}})
	svc.SubmitWorkload(&models.Workload{Name: "w2", TenantID: "t1", GPURequest: models.GPURequest{Count: 1}})

	workloads := svc.ListWorkloads("")
	if len(workloads) != 2 {
		t.Errorf("expected 2 workloads, got %d", len(workloads))
	}
}

func TestTriggerScheduling(t *testing.T) {
	svc := newTestService()
	n := &models.GPUNode{
		Name: "n1",
		GPUs: []models.GPUInfo{{Index: 0, VRAMMB: 81920}, {Index: 1, VRAMMB: 81920}},
	}
	svc.RegisterNode(n)

	wl := &models.Workload{
		Name:       "wl-1",
		TenantID:   "t1",
		GPURequest: models.GPURequest{Count: 1},
	}
	svc.SubmitWorkload(wl)

	results := svc.TriggerScheduling()
	if len(results) != 1 {
		t.Errorf("expected 1 schedule result, got %d", len(results))
	}
	if !results[0].Assigned {
		t.Error("expected workload to be assigned")
	}
}

func TestSchedulingPolicies(t *testing.T) {
	svc := newTestService()
	policies := svc.GetSchedulingPolicies()
	if len(policies) == 0 {
		t.Error("expected default policies")
	}

	err := svc.SetSchedulingPolicies([]models.SchedulingPolicy{
		{Name: "spreading", Type: "spreading", Enabled: true},
	})
	if err != nil {
		t.Fatalf("SetSchedulingPolicies failed: %v", err)
	}
}

func TestModelManagement(t *testing.T) {
	svc := newTestService()
	model, err := svc.PullModel("llama3:8b")
	if err != nil {
		t.Fatalf("PullModel failed: %v", err)
	}
	if model.Name != "llama3:8b" {
		t.Errorf("expected llama3:8b, got %s", model.Name)
	}

	models := svc.ListModels()
	if len(models) != 1 {
		t.Errorf("expected 1 model, got %d", len(models))
	}

	if err := svc.RemoveModel("llama3:8b"); err != nil {
		t.Fatalf("RemoveModel failed: %v", err)
	}
}

func TestQuotaManagement(t *testing.T) {
	svc := newTestService()
	err := svc.SetQuota(&models.GPUQuota{
		TenantID: "t1", MaxGPUs: 8, MaxVRAMMB: 655360, MaxWorkloads: 5,
	})
	if err != nil {
		t.Fatalf("SetQuota failed: %v", err)
	}

	usage, err := svc.GetQuotaUsage("t1")
	if err != nil {
		t.Fatalf("GetQuotaUsage failed: %v", err)
	}
	if usage.MaxGPUs != 8 {
		t.Errorf("expected max 8 GPUs, got %d", usage.MaxGPUs)
	}

	quotas := svc.ListQuotas()
	if len(quotas) != 1 {
		t.Errorf("expected 1 quota, got %d", len(quotas))
	}
}

func TestGetGPUMetrics(t *testing.T) {
	svc := newTestService()
	m, err := svc.GetGPUMetrics("node-1", 2)
	if err != nil {
		t.Fatalf("GetGPUMetrics failed: %v", err)
	}
	if m.NodeID != "node-1" {
		t.Errorf("expected node-1, got %s", m.NodeID)
	}
}

func TestGetNodeResources(t *testing.T) {
	svc := newTestService()
	n := &models.GPUNode{
		Name: "n1",
		GPUs: []models.GPUInfo{{Index: 0, VRAMMB: 81920}},
	}
	svc.RegisterNode(n)

	resources := svc.GetNodeResources()
	if len(resources) != 1 {
		t.Errorf("expected 1 node resource, got %d", len(resources))
	}
}
