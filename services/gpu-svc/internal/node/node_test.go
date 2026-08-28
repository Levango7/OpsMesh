package node

import (
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/models"
)

func sampleNode() *models.GPUNode {
	return &models.GPUNode{
		Name:    "gpu-node-1",
		Address: "192.168.1.100",
		GPUs: []models.GPUInfo{
			{
				Index:             0,
				Model:             "A100",
				VRAMMB:            81920,
				ComputeCapability: "8.0",
				DriverVersion:     "535.104.05",
				Vendor:            models.GPUVendorNVIDIA,
			},
			{
				Index:             1,
				Model:             "A100",
				VRAMMB:            81920,
				ComputeCapability: "8.0",
				DriverVersion:     "535.104.05",
				Vendor:            models.GPUVendorNVIDIA,
			},
		},
		Labels: map[string]string{"zone": "us-east-1a"},
	}
}

func TestRegister(t *testing.T) {
	mgr := NewManager(nil)
	node := sampleNode()

	err := mgr.Register(node)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if node.ID == "" {
		t.Error("expected node ID to be set")
	}
	if node.Status != models.NodeStatusOnline {
		t.Errorf("expected status online, got %s", node.Status)
	}
	if node.TotalVRAMMB != 163840 {
		t.Errorf("expected total VRAM 163840, got %d", node.TotalVRAMMB)
	}
}

func TestRegisterDuplicate(t *testing.T) {
	mgr := NewManager(nil)
	node := sampleNode()

	if err := mgr.Register(node); err != nil {
		t.Fatalf("first Register failed: %v", err)
	}

	node2 := sampleNode()
	node2.ID = node.ID
	err := mgr.Register(node2)
	if err != ErrNodeAlreadyExists {
		t.Fatalf("expected ErrNodeAlreadyExists, got: %v", err)
	}
}

func TestRegisterInvalid(t *testing.T) {
	mgr := NewManager(nil)
	err := mgr.Register(nil)
	if err != ErrNodeInvalid {
		t.Fatalf("expected ErrNodeInvalid, got: %v", err)
	}

	err = mgr.Register(&models.GPUNode{})
	if err != ErrNodeInvalid {
		t.Fatalf("expected ErrNodeInvalid for empty name, got: %v", err)
	}
}

func TestGet(t *testing.T) {
	mgr := NewManager(nil)
	node := sampleNode()
	if err := mgr.Register(node); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	got, err := mgr.Get(node.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Name != "gpu-node-1" {
		t.Errorf("expected name gpu-node-1, got %s", got.Name)
	}
}

func TestGetNotFound(t *testing.T) {
	mgr := NewManager(nil)
	_, err := mgr.Get("nonexistent")
	if err != ErrNodeNotFound {
		t.Fatalf("expected ErrNodeNotFound, got: %v", err)
	}
}

func TestList(t *testing.T) {
	mgr := NewManager(nil)
	for i := 0; i < 3; i++ {
		node := sampleNode()
		node.Name = "node-" + string(rune('A'+i))
		if err := mgr.Register(node); err != nil {
			t.Fatalf("Register failed: %v", err)
		}
	}

	nodes := mgr.List()
	if len(nodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(nodes))
	}
}

func TestUpdate(t *testing.T) {
	mgr := NewManager(nil)
	node := sampleNode()
	if err := mgr.Register(node); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	node.Name = "updated-node"
	node.GPUErrors = 5
	if err := mgr.Update(node); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	got, err := mgr.Get(node.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Name != "updated-node" {
		t.Errorf("expected updated name, got %s", got.Name)
	}
	if got.GPUErrors != 5 {
		t.Errorf("expected 5 errors, got %d", got.GPUErrors)
	}
}

func TestUpdateNotFound(t *testing.T) {
	mgr := NewManager(nil)
	err := mgr.Update(&models.GPUNode{ID: "nonexistent"})
	if err != ErrNodeNotFound {
		t.Fatalf("expected ErrNodeNotFound, got: %v", err)
	}
}

func TestUnregister(t *testing.T) {
	mgr := NewManager(nil)
	node := sampleNode()
	if err := mgr.Register(node); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if err := mgr.Unregister(node.ID); err != nil {
		t.Fatalf("Unregister failed: %v", err)
	}

	_, err := mgr.Get(node.ID)
	if err != ErrNodeNotFound {
		t.Fatalf("expected ErrNodeNotFound after unregister, got: %v", err)
	}
}

func TestUnregisterNotFound(t *testing.T) {
	mgr := NewManager(nil)
	err := mgr.Unregister("nonexistent")
	if err != ErrNodeNotFound {
		t.Fatalf("expected ErrNodeNotFound, got: %v", err)
	}
}

func TestUpdateHeartbeat(t *testing.T) {
	fixedNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	called := 0
	now := func() time.Time {
		called++
		return fixedNow
	}

	mgr := NewManager(now)
	node := sampleNode()
	if err := mgr.Register(node); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	later := fixedNow.Add(30 * time.Second)
	mgr.now = func() time.Time { return later }
	if err := mgr.UpdateHeartbeat(node.ID); err != nil {
		t.Fatalf("UpdateHeartbeat failed: %v", err)
	}

	got, _ := mgr.Get(node.ID)
	if !got.LastHeartbeat.Equal(later) {
		t.Errorf("expected heartbeat %v, got %v", later, got.LastHeartbeat)
	}
}

func TestGetHealth(t *testing.T) {
	mgr := NewManager(nil)
	node := sampleNode()
	if err := mgr.Register(node); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	health, err := mgr.GetHealth(node.ID)
	if err != nil {
		t.Fatalf("GetHealth failed: %v", err)
	}
	if health.NodeID != node.ID {
		t.Errorf("expected node ID %s, got %s", node.ID, health.NodeID)
	}
	if health.Status != models.NodeStatusOnline {
		t.Errorf("expected status online, got %s", health.Status)
	}
}

func TestGetHealthNotFound(t *testing.T) {
	mgr := NewManager(nil)
	_, err := mgr.GetHealth("nonexistent")
	if err != ErrNodeNotFound {
		t.Fatalf("expected ErrNodeNotFound, got: %v", err)
	}
}

func TestSetNodeStatus(t *testing.T) {
	mgr := NewManager(nil)
	node := sampleNode()
	if err := mgr.Register(node); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if err := mgr.SetNodeStatus(node.ID, models.NodeStatusDraining); err != nil {
		t.Fatalf("SetNodeStatus failed: %v", err)
	}

	got, _ := mgr.Get(node.ID)
	if got.Status != models.NodeStatusDraining {
		t.Errorf("expected draining status, got %s", got.Status)
	}
}

func TestGetOnlineNodes(t *testing.T) {
	mgr := NewManager(nil)
	node1 := sampleNode()
	node1.Name = "node-1"
	node2 := sampleNode()
	node2.Name = "node-2"
	mgr.Register(node1)
	mgr.Register(node2)
	mgr.SetNodeStatus(node2.ID, models.NodeStatusOffline)

	online := mgr.GetOnlineNodes()
	if len(online) != 1 {
		t.Errorf("expected 1 online node, got %d", len(online))
	}
}
