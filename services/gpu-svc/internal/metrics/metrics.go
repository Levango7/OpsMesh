package metrics

import (
	"math/rand"
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/models"
)

// Collector handles GPU metrics collection and storage.
type Collector struct {
	mu      sync.RWMutex
	metrics map[string]*models.GPUMetrics
	now     func() time.Time
	rng     *rand.Rand
}

// NewCollector creates a new metrics Collector.
func NewCollector(now func() time.Time) *Collector {
	if now == nil {
		now = time.Now
	}
	return &Collector{
		metrics: make(map[string]*models.GPUMetrics),
		now:     now,
		rng:     rand.New(rand.NewSource(now().UnixNano())),
	}
}

// CollectMetrics simulates collecting GPU metrics for a node.
func (c *Collector) CollectMetrics(nodeID string, gpuCount int) *models.GPUMetrics {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	gpus := make([]models.GPUMetricsPerGPU, gpuCount)
	totalUtil := 0.0
	totalMemUsed := 0
	totalMemTotal := 0
	totalTemp := 0.0
	totalPower := 0.0

	for i := 0; i < gpuCount; i++ {
		utilization := c.rng.Float64() * 100
		memUsed := int(c.rng.Float64() * 81920)
		memTotal := 81920
		temp := 40.0 + c.rng.Float64() * 50
		power := 50.0 + c.rng.Float64() * 300
		eccErrors := 0
		thermalThrottle := false
		if temp > 85 {
			thermalThrottle = true
		}

		gpus[i] = models.GPUMetricsPerGPU{
			Index:           i,
			UtilizationPct:  utilization,
			MemoryUsedMB:    memUsed,
			MemoryTotalMB:   memTotal,
			TemperatureC:    temp,
			PowerDrawW:      power,
			FanSpeedPct:     30.0 + c.rng.Float64() * 70,
			ClockSpeedMHz:   1000 + int(c.rng.Float64()*1500),
			ECCErrorCount:   eccErrors,
			ThermalThrottle: thermalThrottle,
		}

		totalUtil += utilization
		totalMemUsed += memUsed
		totalMemTotal += memTotal
		totalTemp += temp
		totalPower += power
	}

	avgUtil := 0.0
	avgTemp := 0.0
	if gpuCount > 0 {
		avgUtil = totalUtil / float64(gpuCount)
		avgTemp = totalTemp / float64(gpuCount)
	}

	metrics := &models.GPUMetrics{
		NodeID:             nodeID,
		Timestamp:          now,
		GPUs:               gpus,
		AvgUtilization:     avgUtil,
		TotalMemoryUsedMB:  totalMemUsed,
		TotalMemoryTotalMB: totalMemTotal,
		AvgTemperatureC:    avgTemp,
		TotalPowerDrawW:    totalPower,
	}

	c.metrics[nodeID] = metrics
	return metrics
}

// GetMetrics returns the latest metrics for a node.
func (c *Collector) GetMetrics(nodeID string) (*models.GPUMetrics, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m, ok := c.metrics[nodeID]
	if !ok {
		return nil, ErrNoMetrics
	}
	cp := *m
	return &cp, nil
}

// GetAllMetrics returns metrics for all nodes.
func (c *Collector) GetAllMetrics() []*models.GPUMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]*models.GPUMetrics, 0, len(c.metrics))
	for _, m := range c.metrics {
		cp := *m
		out = append(out, &cp)
	}
	return out
}

// ErrNoMetrics is returned when no metrics exist for a node.
var ErrNoMetrics = &MetricsError{"no metrics available for node"}

// MetricsError represents a metrics-related error.
type MetricsError struct {
	Msg string
}

func (e *MetricsError) Error() string {
	return e.Msg
}

// DetectAnomalies checks for GPU health issues in metrics.
func (c *Collector) DetectAnomalies(nodeID string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m, ok := c.metrics[nodeID]
	if !ok {
		return []string{"no metrics available"}
	}

	issues := make([]string, 0)
	for _, gpu := range m.GPUs {
		if gpu.TemperatureC > 90 {
			issues = append(issues, "GPU critically hot")
		}
		if gpu.ThermalThrottle {
			issues = append(issues, "thermal throttling detected")
		}
		if gpu.ECCErrorCount > 0 {
			issues = append(issues, "ECC errors detected")
		}
		if gpu.UtilizationPct > 95 {
			issues = append(issues, "GPU fully saturated")
		}
	}
	return issues
}
