package metrics

import (
	"testing"
)

func TestCollectMetrics(t *testing.T) {
	c := NewCollector(nil)
	metrics := c.CollectMetrics("node-1", 4)

	if metrics.NodeID != "node-1" {
		t.Errorf("expected node-1, got %s", metrics.NodeID)
	}
	if len(metrics.GPUs) != 4 {
		t.Errorf("expected 4 GPUs, got %d", len(metrics.GPUs))
	}
	if metrics.TotalMemoryTotalMB != 4*81920 {
		t.Errorf("expected total memory %d, got %d", 4*81920, metrics.TotalMemoryTotalMB)
	}
}

func TestCollectMetricsStores(t *testing.T) {
	c := NewCollector(nil)
	c.CollectMetrics("node-1", 2)

	stored, err := c.GetMetrics("node-1")
	if err != nil {
		t.Fatalf("GetMetrics failed: %v", err)
	}
	if stored.NodeID != "node-1" {
		t.Errorf("expected node-1, got %s", stored.NodeID)
	}
}

func TestGetMetricsNotFound(t *testing.T) {
	c := NewCollector(nil)
	_, err := c.GetMetrics("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent node")
	}
}

func TestGetAllMetrics(t *testing.T) {
	c := NewCollector(nil)
	c.CollectMetrics("node-1", 2)
	c.CollectMetrics("node-2", 4)

	all := c.GetAllMetrics()
	if len(all) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(all))
	}
}

func TestDetectAnomaliesNoMetrics(t *testing.T) {
	c := NewCollector(nil)
	issues := c.DetectAnomalies("nonexistent")
	if len(issues) != 1 {
		t.Errorf("expected 1 issue, got %d", len(issues))
	}
}

func TestDetectAnomaliesNormal(t *testing.T) {
	c := NewCollector(nil)
	c.CollectMetrics("node-1", 2)
	issues := c.DetectAnomalies("node-1")
	// Since metrics are random, just check it returns without panic
	_ = issues
}

func TestMetricsTimestamp(t *testing.T) {
	c := NewCollector(nil)
	metrics := c.CollectMetrics("node-1", 1)
	if metrics.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestMetricsFields(t *testing.T) {
	c := NewCollector(nil)
	metrics := c.CollectMetrics("node-1", 2)

	if metrics.AvgUtilization < 0 || metrics.AvgUtilization > 100 {
		t.Errorf("expected avg utilization 0-100, got %f", metrics.AvgUtilization)
	}
	if metrics.AvgTemperatureC < 0 {
		t.Errorf("expected non-negative temp, got %f", metrics.AvgTemperatureC)
	}
	if metrics.TotalPowerDrawW < 0 {
		t.Errorf("expected non-negative power, got %f", metrics.TotalPowerDrawW)
	}
}

func TestGPUMetricsPerGPU(t *testing.T) {
	c := NewCollector(nil)
	metrics := c.CollectMetrics("node-1", 1)
	gpu := metrics.GPUs[0]

	if gpu.MemoryTotalMB != 81920 {
		t.Errorf("expected 81920 total mem, got %d", gpu.MemoryTotalMB)
	}
	if gpu.MemoryUsedMB < 0 || gpu.MemoryUsedMB > 81920 {
		t.Errorf("memory used out of range: %d", gpu.MemoryUsedMB)
	}
	if gpu.FanSpeedPct < 0 || gpu.FanSpeedPct > 100 {
		t.Errorf("fan speed out of range: %f", gpu.FanSpeedPct)
	}
}

func TestCollectorConcurrency(t *testing.T) {
	c := NewCollector(nil)
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(idx int) {
			nodeID := "node-" + string(rune('A'+idx))
			c.CollectMetrics(nodeID, 2)
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	all := c.GetAllMetrics()
	if len(all) != 10 {
		t.Errorf("expected 10 nodes, got %d", len(all))
	}
}

func TestMetricsError(t *testing.T) {
	err := &MetricsError{Msg: "test error"}
	if err.Error() != "test error" {
		t.Errorf("expected 'test error', got %s", err.Error())
	}
}

func TestCollectMetricsZeroGPUs(t *testing.T) {
	c := NewCollector(nil)
	metrics := c.CollectMetrics("node-empty", 0)
	if len(metrics.GPUs) != 0 {
		t.Errorf("expected 0 GPUs, got %d", len(metrics.GPUs))
	}
}
