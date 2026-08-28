package aiworkload

import (
	"strings"
	"testing"
)

func TestValidateValidWorkload(t *testing.T) {
	w := &AIWorkload{
		Name:      "llama-inference",
		Type:      WorkloadTypeInference,
		ModelName: "llama-3.1-70b",
		GPURequirements: GPURequirements{
			MinVRAMGB:         80,
			ComputeCapability: "8.0",
			GPUCount:          2,
			GPUModel:          "A100",
			MultiGPU:          true,
		},
		Replicas:    1,
		MaxReplicas: 4,
	}
	if err := w.Validate(); err != nil {
		t.Fatalf("expected valid workload, got: %v", err)
	}
}

func TestValidateMissingName(t *testing.T) {
	w := &AIWorkload{
		Type:      WorkloadTypeInference,
		ModelName: "gpt-4",
		GPURequirements: GPURequirements{
			MinVRAMGB: 40,
			GPUCount:  1,
			GPUModel:  "A100",
		},
	}
	err := w.Validate()
	if err == nil || !strings.Contains(err.Error(), "name required") {
		t.Fatalf("expected 'name required' error, got: %v", err)
	}
}

func TestValidateInvalidType(t *testing.T) {
	w := &AIWorkload{
		Name:      "test",
		Type:      "invalid-type",
		ModelName: "model",
		GPURequirements: GPURequirements{
			MinVRAMGB: 40,
			GPUCount:  1,
		},
	}
	err := w.Validate()
	if err == nil || !strings.Contains(err.Error(), "invalid workload type") {
		t.Fatalf("expected 'invalid workload type' error, got: %v", err)
	}
}

func TestValidateMissingModelName(t *testing.T) {
	w := &AIWorkload{
		Name: "test",
		Type: WorkloadTypeTraining,
		GPURequirements: GPURequirements{
			MinVRAMGB: 40,
			GPUCount:  1,
		},
	}
	err := w.Validate()
	if err == nil || !strings.Contains(err.Error(), "model_name required") {
		t.Fatalf("expected 'model_name required' error, got: %v", err)
	}
}

func TestValidateInvalidGPURequirements(t *testing.T) {
	w := &AIWorkload{
		Name:      "test",
		Type:      WorkloadTypeInference,
		ModelName: "model",
		GPURequirements: GPURequirements{
			MinVRAMGB: 0,
			GPUCount:  0,
		},
	}
	err := w.Validate()
	if err == nil {
		t.Fatal("expected GPU validation error, got nil")
	}
}

func TestValidateMultiGPUNotEnough(t *testing.T) {
	w := &AIWorkload{
		Name:      "test",
		Type:      WorkloadTypeTraining,
		ModelName: "model",
		GPURequirements: GPURequirements{
			MinVRAMGB: 40,
			GPUCount:  1,
			MultiGPU:  true,
		},
	}
	err := w.Validate()
	if err == nil || !strings.Contains(err.Error(), "multi-GPU requires gpu_count >= 2") {
		t.Fatalf("expected multi-GPU error, got: %v", err)
	}
}

func TestValidateNVLinkWithoutMultiGPU(t *testing.T) {
	w := &AIWorkload{
		Name:      "test",
		Type:      WorkloadTypeInference,
		ModelName: "model",
		GPURequirements: GPURequirements{
			MinVRAMGB: 40,
			GPUCount:  1,
			NVLink:    true,
		},
	}
	err := w.Validate()
	if err == nil || !strings.Contains(err.Error(), "NVLink requires multi-GPU") {
		t.Fatalf("expected NVLink error, got: %v", err)
	}
}

func TestDeployAndStatus(t *testing.T) {
	m := NewManager()
	w := &AIWorkload{
		TenantID:  "tenant-1",
		Name:      "bert-inference",
		Type:      WorkloadTypeInference,
		ModelName: "bert-large",
		GPURequirements: GPURequirements{
			MinVRAMGB:         16,
			ComputeCapability: "7.0",
			GPUCount:          1,
			GPUModel:          "T4",
		},
		Replicas:    1,
		MaxReplicas: 2,
	}

	deployed, err := m.Deploy(w)
	if err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}
	if deployed.ID == "" {
		t.Fatal("expected non-empty workload ID")
	}
	if deployed.Status != WorkloadStatusRunning {
		t.Fatalf("expected status %s, got %s", WorkloadStatusRunning, deployed.Status)
	}

	got, err := m.GetStatus(deployed.ID, "tenant-1")
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if got.Name != "bert-inference" {
		t.Fatalf("expected name bert-inference, got %s", got.Name)
	}
}

func TestDeployInvalidWorkload(t *testing.T) {
	m := NewManager()
	w := &AIWorkload{
		Name: "",
		Type: WorkloadTypeInference,
	}
	_, err := m.Deploy(w)
	if err == nil {
		t.Fatal("expected error for invalid workload")
	}
}

func TestScale(t *testing.T) {
	m := NewManager()
	w := &AIWorkload{
		TenantID:  "tenant-1",
		Name:      "scale-test",
		Type:      WorkloadTypeInference,
		ModelName: "model",
		GPURequirements: GPURequirements{
			MinVRAMGB: 40,
			GPUCount:  1,
			GPUModel:  "A100",
		},
		Replicas:    1,
		MaxReplicas: 4,
	}

	deployed, err := m.Deploy(w)
	if err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}

	scaled, err := m.Scale(deployed.ID, "tenant-1", 3)
	if err != nil {
		t.Fatalf("Scale failed: %v", err)
	}
	if scaled.Replicas != 3 {
		t.Fatalf("expected 3 replicas, got %d", scaled.Replicas)
	}
	if scaled.ReplicasBefore != 1 {
		t.Fatalf("expected replicas_before=1, got %d", scaled.ReplicasBefore)
	}
}

func TestScaleExceedsMax(t *testing.T) {
	m := NewManager()
	w := &AIWorkload{
		TenantID:  "tenant-1",
		Name:      "scale-test",
		Type:      WorkloadTypeInference,
		ModelName: "model",
		GPURequirements: GPURequirements{
			MinVRAMGB: 40,
			GPUCount:  1,
			GPUModel:  "A100",
		},
		Replicas:    1,
		MaxReplicas: 2,
	}

	deployed, err := m.Deploy(w)
	if err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}

	_, err = m.Scale(deployed.ID, "tenant-1", 5)
	if err == nil || !strings.Contains(err.Error(), "exceeds max_replicas") {
		t.Fatalf("expected max_replicas error, got: %v", err)
	}
}

func TestRollback(t *testing.T) {
	m := NewManager()
	w := &AIWorkload{
		TenantID:  "tenant-1",
		Name:      "rollback-test",
		Type:      WorkloadTypeTraining,
		ModelName: "gpt-2",
		GPURequirements: GPURequirements{
			MinVRAMGB: 80,
			GPUCount:  4,
			GPUModel:  "A100",
			MultiGPU:  true,
		},
		Replicas:    1,
		MaxReplicas: 8,
	}

	deployed, err := m.Deploy(w)
	if err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}

	_, err = m.Scale(deployed.ID, "tenant-1", 4)
	if err != nil {
		t.Fatalf("Scale failed: %v", err)
	}

	rolledBack, err := m.Rollback(deployed.ID, "tenant-1")
	if err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}
	if rolledBack.Status != WorkloadStatusRolledBack {
		t.Fatalf("expected status %s, got %s", WorkloadStatusRolledBack, rolledBack.Status)
	}
	if rolledBack.Replicas != 1 {
		t.Fatalf("expected 1 replica after rollback, got %d", rolledBack.Replicas)
	}
}

func TestStop(t *testing.T) {
	m := NewManager()
	w := &AIWorkload{
		TenantID:  "tenant-1",
		Name:      "stop-test",
		Type:      WorkloadTypeInference,
		ModelName: "model",
		GPURequirements: GPURequirements{
			MinVRAMGB: 16,
			GPUCount:  1,
			GPUModel:  "T4",
		},
		Replicas:    1,
		MaxReplicas: 2,
	}

	deployed, err := m.Deploy(w)
	if err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}

	stopped, err := m.Stop(deployed.ID, "tenant-1")
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if stopped.Status != WorkloadStatusStopped {
		t.Fatalf("expected status %s, got %s", WorkloadStatusStopped, stopped.Status)
	}
}

func TestTenantIsolation(t *testing.T) {
	m := NewManager()
	w := &AIWorkload{
		TenantID:  "tenant-1",
		Name:      "isolated",
		Type:      WorkloadTypeInference,
		ModelName: "model",
		GPURequirements: GPURequirements{
			MinVRAMGB: 40,
			GPUCount:  1,
			GPUModel:  "A100",
		},
		Replicas:    1,
		MaxReplicas: 2,
	}

	deployed, err := m.Deploy(w)
	if err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}

	_, err = m.GetStatus(deployed.ID, "tenant-2")
	if err == nil || !strings.Contains(err.Error(), "tenant mismatch") {
		t.Fatalf("expected tenant mismatch error, got: %v", err)
	}
}

func TestList(t *testing.T) {
	m := NewManager()

	for i := 0; i < 3; i++ {
		_, err := m.Deploy(&AIWorkload{
			TenantID:  "tenant-1",
			Name:      "workload",
			Type:      WorkloadTypeInference,
			ModelName: "model",
			GPURequirements: GPURequirements{
				MinVRAMGB: 40,
				GPUCount:  1,
				GPUModel:  "A100",
			},
			Replicas:    1,
			MaxReplicas: 2,
		})
		if err != nil {
			t.Fatalf("Deploy failed: %v", err)
		}
	}

	list := m.List("tenant-1", "")
	if len(list) != 3 {
		t.Fatalf("expected 3 workloads, got %d", len(list))
	}
}

func TestGetLogs(t *testing.T) {
	m := NewManager()
	w := &AIWorkload{
		TenantID:  "tenant-1",
		Name:      "log-test",
		Type:      WorkloadTypeFineTuning,
		ModelName: "llama-3-8b",
		GPURequirements: GPURequirements{
			MinVRAMGB: 80,
			GPUCount:  2,
			GPUModel:  "A100",
			MultiGPU:  true,
		},
		Replicas:    1,
		MaxReplicas: 4,
	}

	deployed, err := m.Deploy(w)
	if err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}

	logs, err := m.GetLogs(deployed.ID, "tenant-1")
	if err != nil {
		t.Fatalf("GetLogs failed: %v", err)
	}
	if !strings.Contains(logs, "llama-3-8b") {
		t.Fatalf("expected logs to contain model name, got: %s", logs)
	}
	if !strings.Contains(logs, "A100") {
		t.Fatalf("expected logs to contain GPU model, got: %s", logs)
	}
}

func TestIsValidWorkloadType(t *testing.T) {
	tests := []struct {
		wt   string
		want bool
	}{
		{WorkloadTypeInference, true},
		{WorkloadTypeTraining, true},
		{WorkloadTypeFineTuning, true},
		{"invalid", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsValidWorkloadType(tt.wt); got != tt.want {
			t.Errorf("IsValidWorkloadType(%q) = %v, want %v", tt.wt, got, tt.want)
		}
	}
}

func TestIsTerminalStatus(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{WorkloadStatusFailed, true},
		{WorkloadStatusStopped, true},
		{WorkloadStatusRolledBack, true},
		{WorkloadStatusRunning, false},
		{WorkloadStatusPending, false},
		{WorkloadStatusDeploying, false},
	}
	for _, tt := range tests {
		if got := IsTerminalStatus(tt.status); got != tt.want {
			t.Errorf("IsTerminalStatus(%q) = %v, want %v", tt.status, got, tt.want)
		}
	}
}
