package gpu

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// GPUInfo describes a single GPU device detected from nvidia-smi.
type GPUInfo struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	UUID              string  `json:"uuid"`
	MemoryTotalMB     int     `json:"memory_total_mb"`
	MemoryUsedMB      int     `json:"memory_used_mb"`
	UtilizationGPU    float64 `json:"utilization_gpu"`
	UtilizationMemory float64 `json:"utilization_memory"`
	Temperature       float64 `json:"temperature"`
	PowerDraw         float64 `json:"power_draw"`
	DriverVersion     string  `json:"driver_version"`
	ComputeCapability string  `json:"compute_capability"`
	ECCErrors         int     `json:"ecc_errors"`
}

// Detector is the interface for GPU detection implementations.
type Detector interface {
	DetectGPUs() ([]GPUInfo, error)
	IsAvailable() bool
}

// NvidiaDetector runs nvidia-smi to detect NVIDIA GPUs.
type NvidiaDetector struct {
	mu       sync.Mutex
	nvidiaSMI string
	available *bool
}

// NewNvidiaDetector creates a new NvidiaDetector.
func NewNvidiaDetector() *NvidiaDetector {
	return &NvidiaDetector{
		nvidiaSMI: "nvidia-smi",
	}
}

// IsAvailable checks if nvidia-smi is available on the system.
func (d *NvidiaDetector) IsAvailable() bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.available != nil {
		return *d.available
	}

	_, err := exec.LookPath(d.nvidiaSMI)
	avail := err == nil
	d.available = &avail
	return avail
}

// DetectGPUs runs nvidia-smi and parses the CSV output.
func (d *NvidiaDetector) DetectGPUs() ([]GPUInfo, error) {
	if !d.IsAvailable() {
		return nil, fmt.Errorf("nvidia-smi not found in PATH")
	}

	cmd := exec.Command(d.nvidiaSMI,
		"--query-gpu=gpu_name,uuid,memory.total,memory.used,utilization.gpu,utilization.memory,temperature.gpu,power.draw,driver_version,compute_cap",
		"--format=csv,noheader,nounits",
	)

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := strings.TrimSpace(string(exitErr.Stderr))
			if strings.Contains(stderr, "driver/library version mismatch") {
				return nil, fmt.Errorf("NVIDIA driver not loaded: %s", stderr)
			}
			if strings.Contains(stderr, "Permission denied") {
				return nil, fmt.Errorf("permission denied accessing NVIDIA driver: %s", stderr)
			}
			if strings.Contains(stderr, "NVIDIA-SMI has failed") {
				return nil, fmt.Errorf("no NVIDIA GPU found: %s", stderr)
			}
			return nil, fmt.Errorf("nvidia-smi failed: %s", stderr)
		}
		return nil, fmt.Errorf("failed to execute nvidia-smi: %w", err)
	}

	return ParseCSVOutput(string(output)), nil
}

// ParseCSVOutput parses the CSV output from nvidia-smi into GPUInfo structs.
func ParseCSVOutput(output string) []GPUInfo {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	gpus := make([]GPUInfo, 0, len(lines))

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Split(line, ",")
		if len(fields) < 10 {
			continue
		}

		for j := range fields {
			fields[j] = strings.TrimSpace(fields[j])
		}

		gpu := GPUInfo{
			ID:                fmt.Sprintf("gpu-%d", i),
			Name:              fields[0],
			UUID:              fields[1],
			DriverVersion:     fields[8],
			ComputeCapability: fields[9],
		}

		gpu.MemoryTotalMB, _ = strconv.Atoi(fields[2])
		gpu.MemoryUsedMB, _ = strconv.Atoi(fields[3])
		gpu.UtilizationGPU, _ = strconv.ParseFloat(fields[4], 64)
		gpu.UtilizationMemory, _ = strconv.ParseFloat(fields[5], 64)
		gpu.Temperature, _ = strconv.ParseFloat(fields[6], 64)
		gpu.PowerDraw, _ = strconv.ParseFloat(fields[7], 64)


		gpus = append(gpus, gpu)
	}

	return gpus
}

// MockDetector provides simulated GPU data for testing and fallback.
type MockDetector struct{}

// NewMockDetector creates a new MockDetector.
func NewMockDetector() *MockDetector {
	return &MockDetector{}
}

// DetectGPUs returns simulated GPU data.
func (d *MockDetector) DetectGPUs() ([]GPUInfo, error) {
	return []GPUInfo{
		{
			ID:                "gpu-0",
			Name:              "NVIDIA A100-SXM4-80GB",
			UUID:              "GPU-12345678-1234-1234-1234-123456789abc",
			MemoryTotalMB:     81920,
			MemoryUsedMB:      1024,
			UtilizationGPU:    15.0,
			UtilizationMemory: 1.2,
			Temperature:       42.0,
			PowerDraw:         72.5,
			DriverVersion:     "535.104.05",
			ComputeCapability: "8.0",
			ECCErrors:         0,
		},
		{
			ID:                "gpu-1",
			Name:              "NVIDIA A100-SXM4-80GB",
			UUID:              "GPU-abcdef01-abcd-abcd-abcd-abcdef012345",
			MemoryTotalMB:     81920,
			MemoryUsedMB:      2048,
			UtilizationGPU:    5.0,
			UtilizationMemory: 2.5,
			Temperature:       38.0,
			PowerDraw:         65.0,
			DriverVersion:     "535.104.05",
			ComputeCapability: "8.0",
			ECCErrors:         0,
		},
	}, nil
}

// IsAvailable always returns true for the mock detector.
func (d *MockDetector) IsAvailable() bool {
	return true
}

// AutoDetect returns real GPU data if nvidia-smi is available, otherwise falls back to mock data.
func AutoDetect() ([]GPUInfo, Detector, error) {
	nvidia := NewNvidiaDetector()
	if nvidia.IsAvailable() {
		gpus, err := nvidia.DetectGPUs()
		if err != nil {
			mock := NewMockDetector()
			gpus, mockErr := mock.DetectGPUs()
			if mockErr != nil {
				return nil, nil, fmt.Errorf("real detection failed: %v; mock fallback also failed: %v", err, mockErr)
			}
			return gpus, mock, nil
		}
		return gpus, nvidia, nil
	}

	mock := NewMockDetector()
	gpus, err := mock.DetectGPUs()
	return gpus, mock, err
}
