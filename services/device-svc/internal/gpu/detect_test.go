package gpu

import (
	"testing"
)

func TestDetectGPUsReturnsDevices(t *testing.T) {
	d := NewDetector()
	gpus := d.DetectGPUs("node-001")

	if len(gpus) == 0 {
		t.Fatal("expected at least one GPU detected, got 0")
	}

	for _, gpu := range gpus {
		if gpu.ID == "" {
			t.Error("expected GPU ID to be set")
		}
		if gpu.Model == "" {
			t.Error("expected GPU model to be set")
		}
		if gpu.VRAMGB <= 0 {
			t.Errorf("expected positive VRAM, got %d", gpu.VRAMGB)
		}
		if gpu.NodeID != "node-001" {
			t.Errorf("expected nodeID node-001, got %s", gpu.NodeID)
		}
		if gpu.Status != "online" {
			t.Errorf("expected status online, got %s", gpu.Status)
		}
	}
}

func TestDetectGPUsReplacesExisting(t *testing.T) {
	d := NewDetector()

	first := d.DetectGPUs("node-001")
	firstCount := len(first)

	second := d.DetectGPUs("node-001")
	if len(second) != firstCount {
		t.Errorf("expected %d GPUs after re-detection, got %d", firstCount, len(second))
	}

	all := d.ListGPUs("")
	if len(all) != firstCount {
		t.Errorf("expected %d total GPUs after re-detection, got %d", firstCount, len(all))
	}
}

func TestGetGPUInfo(t *testing.T) {
	d := NewDetector()
	gpus := d.DetectGPUs("node-001")

	gpu, err := d.GetGPUInfo(gpus[0].ID)
	if err != nil {
		t.Fatalf("GetGPUInfo failed: %v", err)
	}

	if gpu.ID != gpus[0].ID {
		t.Errorf("expected ID %s, got %s", gpus[0].ID, gpu.ID)
	}
	if gpu.Model != gpus[0].Model {
		t.Errorf("expected model %s, got %s", gpus[0].Model, gpu.Model)
	}
}

func TestGetGPUInfoNotFound(t *testing.T) {
	d := NewDetector()

	_, err := d.GetGPUInfo("nonexistent-gpu")
	if err == nil {
		t.Fatal("expected error for nonexistent GPU, got nil")
	}
}

func TestListGPUsByNode(t *testing.T) {
	d := NewDetector()
	d.DetectGPUs("node-001")
	d.DetectGPUs("node-002")

	node1GPUs := d.ListGPUs("node-002")
	if len(node1GPUs) == 0 {
		t.Fatal("expected GPUs for node-002, got 0")
	}

	for _, gpu := range node1GPUs {
		if gpu.NodeID != "node-002" {
			t.Errorf("expected nodeID node-002, got %s", gpu.NodeID)
		}
	}
}

func TestListGPUsEmptyNode(t *testing.T) {
	d := NewDetector()

	gpus := d.ListGPUs("nonexistent-node")
	if len(gpus) != 0 {
		t.Errorf("expected 0 GPUs for nonexistent node, got %d", len(gpus))
	}
}

func TestGetGPUStats(t *testing.T) {
	d := NewDetector()
	d.DetectGPUs("node-001")
	d.DetectGPUs("node-002")

	stats := d.GetGPUStats()

	if stats.TotalGPUs == 0 {
		t.Fatal("expected non-zero total GPUs in stats")
	}
	if stats.TotalVRAMGB == 0 {
		t.Fatal("expected non-zero total VRAM in stats")
	}
	if stats.OnlineGPUs != stats.TotalGPUs {
		t.Errorf("expected all GPUs online, got %d/%d", stats.OnlineGPUs, stats.TotalGPUs)
	}
	if len(stats.ModelCounts) == 0 {
		t.Error("expected model counts to be populated")
	}
	if stats.AvgVRAMGB <= 0 {
		t.Errorf("expected positive average VRAM, got %f", stats.AvgVRAMGB)
	}
}

func TestGetGPUStatsEmpty(t *testing.T) {
	d := NewDetector()

	stats := d.GetGPUStats()

	if stats.TotalGPUs != 0 {
		t.Errorf("expected 0 total GPUs, got %d", stats.TotalGPUs)
	}
	if stats.TotalVRAMGB != 0 {
		t.Errorf("expected 0 total VRAM, got %d", stats.TotalVRAMGB)
	}
	if len(stats.ModelCounts) != 0 {
		t.Errorf("expected empty model counts, got %v", stats.ModelCounts)
	}
}
