package service

// ascend_wiring_test.go — issue #98：昇腾 NPU 采集器接线门禁。

import (
	"context"
	"errors"
	"testing"

	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/metrics"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/node"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/ollama"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/quota"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/scheduler"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/workload"
)

type fakeAscend struct {
	m   *models.GPUMetrics
	err error
	got string // 记录传入的 nodeID
}

func (f *fakeAscend) Collect(ctx context.Context, nodeID string) (*models.GPUMetrics, error) {
	f.got = nodeID
	return f.m, f.err
}

func newAscendTestService() *Service {
	return NewService(
		node.NewManager(nil), scheduler.NewScheduler(nil), workload.NewManager(nil),
		ollama.NewClient("http://localhost:1", 1), quota.NewManager(nil), metrics.NewCollector(nil),
	)
}

func TestGetGPUMetricsUsesAscendWhenWired(t *testing.T) {
	s := newAscendTestService()
	want := &models.GPUMetrics{NodeID: "npu-1", Source: "npu-smi"}
	fake := &fakeAscend{m: want}
	s.SetAscendCollector(fake)

	got, err := s.GetGPUMetrics("npu-1", 4)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("should use ascend result, got %+v", got)
	}
	if fake.got != "npu-1" {
		t.Errorf("ascend called with nodeID=%q, want npu-1", fake.got)
	}
}

func TestGetGPUMetricsFallsBackWhenAscendFails(t *testing.T) {
	s := newAscendTestService()
	fake := &fakeAscend{err: errors.New("npu-smi not available")}
	s.SetAscendCollector(fake)

	got, err := s.GetGPUMetrics("gpu-1", 4)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("fallback should return default metrics, got nil")
	}
	if got.Source == "npu-smi" {
		t.Errorf("should fall back off ascend, got source=%q", got.Source)
	}
}

func TestGetGPUMetricsNoAscendUsesDefault(t *testing.T) {
	s := newAscendTestService()
	got, err := s.GetGPUMetrics("gpu-2", 4)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("default collector should return metrics")
	}
}
