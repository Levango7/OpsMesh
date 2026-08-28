package gpu

import (
	"fmt"
	"sync"
	"time"
)

// GPUDevice represents a GPU device detected on a node.
type GPUDevice struct {
	ID                string    `json:"id"`
	Model             string    `json:"model"`
	VRAMGB            int       `json:"vramGb"`
	DriverVersion     string    `json:"driverVersion"`
	ComputeCapability string    `json:"computeCapability"`
	NodeID            string    `json:"nodeID"`
	Status            string    `json:"status"`
	DetectedAt        time.Time `json:"detectedAt"`
}

// GPUStats holds aggregate statistics across all GPUs.
type GPUStats struct {
	TotalGPUs      int            `json:"totalGpus"`
	TotalVRAMGB    int            `json:"totalVramGb"`
	OnlineGPUs     int            `json:"onlineGPUs"`
	OfflineGPUs    int            `json:"offlineGPUs"`
	ModelCounts    map[string]int `json:"modelCounts"`
	AvgVRAMGB      float64        `json:"avgVramGb"`
}

// Detector manages GPU detection and caching.
type Detector struct {
	mu    sync.RWMutex
	gpus  map[string]*GPUDevice
	nodes map[string][]string // nodeID -> []gpuID
}

// NewDetector creates a new GPU Detector.
func NewDetector() *Detector {
	return &Detector{
		gpus:  make(map[string]*GPUDevice),
		nodes: make(map[string][]string),
	}
}

// DetectGPUs simulates GPU detection for a given node.
// In production, this would call nvidia-smi or similar.
func (d *Detector) DetectGPUs(nodeID string) []*GPUDevice {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Remove existing GPUs for this node
	if oldIDs, ok := d.nodes[nodeID]; ok {
		for _, id := range oldIDs {
			delete(d.gpus, id)
		}
	}

	// Simulate detection of 1-3 GPUs per node
	models := []struct {
		model             string
		vram              int
		computeCapability string
	}{
		{"NVIDIA A100", 80, "8.0"},
		{"NVIDIA H100", 80, "9.0"},
		{"NVIDIA V100", 32, "7.0"},
		{"NVIDIA T4", 16, "7.5"},
	}

	now := time.Now()
	detected := make([]*GPUDevice, 0)
	d.nodes[nodeID] = make([]string, 0)

	count := 1
	if nodeID != "" {
		count = 2
	}

	for i := 0; i < count; i++ {
		m := models[i%len(models)]
		gpuID := fmt.Sprintf("gpu-%s-%d", nodeID, i)
		gpu := &GPUDevice{
			ID:                gpuID,
			Model:             m.model,
			VRAMGB:            m.vram,
			DriverVersion:     "535.129.03",
			ComputeCapability: m.computeCapability,
			NodeID:            nodeID,
			Status:            "online",
			DetectedAt:        now,
		}
		d.gpus[gpuID] = gpu
		d.nodes[nodeID] = append(d.nodes[nodeID], gpuID)
		detected = append(detected, gpu)
	}

	return detected
}

// GetGPUInfo returns a single GPU by ID.
func (d *Detector) GetGPUInfo(gpuID string) (*GPUDevice, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	gpu, ok := d.gpus[gpuID]
	if !ok {
		return nil, fmt.Errorf("GPU not found: %s", gpuID)
	}
	cp := *gpu
	return &cp, nil
}

// ListGPUs returns all GPUs, optionally filtered by nodeID.
func (d *Detector) ListGPUs(nodeID string) []*GPUDevice {
	d.mu.RLock()
	defer d.mu.RUnlock()

	out := make([]*GPUDevice, 0)
	if nodeID != "" {
		if ids, ok := d.nodes[nodeID]; ok {
			for _, id := range ids {
				if gpu, ok := d.gpus[id]; ok {
					cp := *gpu
					out = append(out, &cp)
				}
			}
		}
		return out
	}

	for _, gpu := range d.gpus {
		cp := *gpu
		out = append(out, &cp)
	}
	return out
}

// GetGPUStats returns aggregate statistics across all detected GPUs.
func (d *Detector) GetGPUStats() *GPUStats {
	d.mu.RLock()
	defer d.mu.RUnlock()

	stats := &GPUStats{
		ModelCounts: make(map[string]int),
	}

	if len(d.gpus) == 0 {
		return stats
	}

	var totalVRAM int
	for _, gpu := range d.gpus {
		stats.TotalGPUs++
		totalVRAM += gpu.VRAMGB
		stats.ModelCounts[gpu.Model]++
		if gpu.Status == "online" {
			stats.OnlineGPUs++
		} else {
			stats.OfflineGPUs++
		}
	}

	stats.TotalVRAMGB = totalVRAM
	stats.AvgVRAMGB = float64(totalVRAM) / float64(stats.TotalGPUs)
	return stats
}
