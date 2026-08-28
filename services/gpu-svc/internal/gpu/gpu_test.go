package gpu

import (
	"reflect"
	"testing"
)

func TestParseCSVOutput_SingleGPU(t *testing.T) {
	output := "NVIDIA A100-SXM4-80GB, GPU-12345678-1234-1234-1234-123456789abc, 81920, 1024, 15, 1, 42, 72.5, 535.104.05, 8.0"

	gpus := ParseCSVOutput(output)

	if len(gpus) != 1 {
		t.Fatalf("expected 1 GPU, got %d", len(gpus))
	}

	g := gpus[0]
	if g.Name != "NVIDIA A100-SXM4-80GB" {
		t.Errorf("expected name 'NVIDIA A100-SXM4-80GB', got '%s'", g.Name)
	}
	if g.UUID != "GPU-12345678-1234-1234-1234-123456789abc" {
		t.Errorf("expected UUID 'GPU-12345678-1234-1234-1234-123456789abc', got '%s'", g.UUID)
	}
	if g.MemoryTotalMB != 81920 {
		t.Errorf("expected memory total 81920, got %d", g.MemoryTotalMB)
	}
	if g.MemoryUsedMB != 1024 {
		t.Errorf("expected memory used 1024, got %d", g.MemoryUsedMB)
	}
	if g.UtilizationGPU != 15.0 {
		t.Errorf("expected GPU utilization 15.0, got %f", g.UtilizationGPU)
	}
	if g.Temperature != 42.0 {
		t.Errorf("expected temperature 42.0, got %f", g.Temperature)
	}
	if g.PowerDraw != 72.5 {
		t.Errorf("expected power draw 72.5, got %f", g.PowerDraw)
	}
	if g.DriverVersion != "535.104.05" {
		t.Errorf("expected driver version '535.104.05', got '%s'", g.DriverVersion)
	}
	if g.ComputeCapability != "8.0" {
		t.Errorf("expected compute capability '8.0', got '%s'", g.ComputeCapability)
	}
}

func TestParseCSVOutput_MultipleGPUs(t *testing.T) {
	output := `NVIDIA A100-SXM4-80GB, GPU-11111111-1111-1111-1111-111111111111, 81920, 2048, 25, 3, 45, 80.0, 535.104.05, 8.0
NVIDIA H100-80GB, GPU-22222222-2222-2222-2222-222222222222, 81920, 4096, 50, 5, 55, 300.0, 535.104.05, 9.0`

	gpus := ParseCSVOutput(output)

	if len(gpus) != 2 {
		t.Fatalf("expected 2 GPUs, got %d", len(gpus))
	}

	if gpus[0].Name != "NVIDIA A100-SXM4-80GB" {
		t.Errorf("GPU 0: expected name 'NVIDIA A100-SXM4-80GB', got '%s'", gpus[0].Name)
	}
	if gpus[1].Name != "NVIDIA H100-80GB" {
		t.Errorf("GPU 1: expected name 'NVIDIA H100-80GB', got '%s'", gpus[1].Name)
	}
	if gpus[1].ComputeCapability != "9.0" {
		t.Errorf("GPU 1: expected compute capability '9.0', got '%s'", gpus[1].ComputeCapability)
	}
	if gpus[1].PowerDraw != 300.0 {
		t.Errorf("GPU 1: expected power draw 300.0, got %f", gpus[1].PowerDraw)
	}
}

func TestParseCSVOutput_EmptyInput(t *testing.T) {
	gpus := ParseCSVOutput("")
	if len(gpus) != 0 {
		t.Errorf("expected 0 GPUs for empty input, got %d", len(gpus))
	}
}

func TestParseCSVOutput_InsufficientFields(t *testing.T) {
	output := "NVIDIA A100, GPU-1234, 81920"
	gpus := ParseCSVOutput(output)
	if len(gpus) != 0 {
		t.Errorf("expected 0 GPUs for insufficient fields, got %d", len(gpus))
	}
}

func TestParseCSVOutput_WithBlankLines(t *testing.T) {
	output := `
NVIDIA A100-SXM4-80GB, GPU-12345678-1234-1234-1234-123456789abc, 81920, 1024, 15, 1, 42, 72.5, 535.104.05, 8.0

NVIDIA H100-80GB, GPU-abcdef01-abcd-abcd-abcd-abcdef012345, 81920, 2048, 30, 2, 50, 280.0, 535.104.05, 9.0
`

	gpus := ParseCSVOutput(output)
	if len(gpus) != 2 {
		t.Fatalf("expected 2 GPUs, got %d", len(gpus))
	}
}

func TestMockDetector_DetectGPUs(t *testing.T) {
	mock := NewMockDetector()
	gpus, err := mock.DetectGPUs()

	if err != nil {
		t.Fatalf("MockDetector.DetectGPUs returned error: %v", err)
	}
	if len(gpus) != 2 {
		t.Fatalf("expected 2 mock GPUs, got %d", len(gpus))
	}

	expected := GPUInfo{
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
	}

	if !reflect.DeepEqual(gpus[0], expected) {
		t.Errorf("mock GPU[0] mismatch:\n  got:  %+v\n  want: %+v", gpus[0], expected)
	}
}

func TestMockDetector_IsAvailable(t *testing.T) {
	mock := NewMockDetector()
	if !mock.IsAvailable() {
		t.Error("MockDetector.IsAvailable() should always return true")
	}
}

func TestNvidiaDetector_IsAvailable(t *testing.T) {
	d := NewNvidiaDetector()
	// On a machine without nvidia-smi, this should return false.
	// On a machine with nvidia-smi, this should return true.
	// We just verify it doesn't panic and returns a consistent result.
	result1 := d.IsAvailable()
	result2 := d.IsAvailable()
	if result1 != result2 {
		t.Errorf("IsAvailable() returned inconsistent results: %v vs %v", result1, result2)
	}
}

func TestParseCSVOutput_RealWorldOutput(t *testing.T) {
	// Real-world nvidia-smi output with typical values
	output := `NVIDIA GeForce RTX 4090, GPU-a1b2c3d4-e5f6-7890-abcd-ef1234567890, 24576, 5120, 12, 20, 45, 180.25, 545.92, 8.9
NVIDIA GeForce RTX 4090, GPU-f0e1d2c3-b4a5-9687-6543-21fedcba0987, 24576, 10240, 45, 41, 52, 220.50, 545.92, 8.9`

	gpus := ParseCSVOutput(output)

	if len(gpus) != 2 {
		t.Fatalf("expected 2 GPUs, got %d", len(gpus))
	}

	if gpus[0].MemoryTotalMB != 24576 {
		t.Errorf("GPU 0: expected memory total 24576, got %d", gpus[0].MemoryTotalMB)
	}
	if gpus[1].MemoryUsedMB != 10240 {
		t.Errorf("GPU 1: expected memory used 10240, got %d", gpus[1].MemoryUsedMB)
	}
	if gpus[1].UtilizationGPU != 45.0 {
		t.Errorf("GPU 1: expected GPU utilization 45.0, got %f", gpus[1].UtilizationGPU)
	}
	if gpus[0].ComputeCapability != "8.9" {
		t.Errorf("GPU 0: expected compute capability '8.9', got '%s'", gpus[0].ComputeCapability)
	}
}

func TestParseCSVOutput_InvalidNumbers(t *testing.T) {
	// nvidia-smi may return [N/A] for some fields
	output := `NVIDIA A100, GPU-1234, 81920, [N/A], [N/A], [N/A], [N/A], [N/A], 535.104.05, 8.0`

	gpus := ParseCSVOutput(output)

	if len(gpus) != 1 {
		t.Fatalf("expected 1 GPU, got %d", len(gpus))
	}

	// Invalid numbers should default to 0
	if gpus[0].MemoryUsedMB != 0 {
		t.Errorf("expected memory used 0 for [N/A], got %d", gpus[0].MemoryUsedMB)
	}
	if gpus[0].UtilizationGPU != 0.0 {
		t.Errorf("expected GPU utilization 0.0 for [N/A], got %f", gpus[0].UtilizationGPU)
	}
	if gpus[0].MemoryTotalMB != 81920 {
		t.Errorf("expected memory total 81920, got %d", gpus[0].MemoryTotalMB)
	}
}

func TestGPUInfoStruct_Fields(t *testing.T) {
	g := GPUInfo{
		ID:                "gpu-0",
		Name:              "Test GPU",
		UUID:              "GPU-test-uuid",
		MemoryTotalMB:     81920,
		MemoryUsedMB:      4096,
		UtilizationGPU:    25.5,
		UtilizationMemory: 5.0,
		Temperature:       55.0,
		PowerDraw:         150.0,
		DriverVersion:     "535.104.05",
		ComputeCapability: "8.0",
		ECCErrors:         0,
	}

	if g.ID != "gpu-0" || g.Name != "Test GPU" || g.UUID != "GPU-test-uuid" {
		t.Error("GPUInfo basic fields mismatch")
	}
	if g.MemoryTotalMB != 81920 || g.MemoryUsedMB != 4096 {
		t.Error("GPUInfo memory fields mismatch")
	}
	if g.UtilizationGPU != 25.5 || g.UtilizationMemory != 5.0 {
		t.Error("GPUInfo utilization fields mismatch")
	}
	if g.Temperature != 55.0 || g.PowerDraw != 150.0 {
		t.Error("GPUInfo temperature/power fields mismatch")
	}
	if g.DriverVersion != "535.104.05" || g.ComputeCapability != "8.0" {
		t.Error("GPUInfo driver/compute fields mismatch")
	}
}
