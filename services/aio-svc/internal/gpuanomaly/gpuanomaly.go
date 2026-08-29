package gpuanomaly

import (
	"math"
	"sync"
	"time"
)

// AnomalyType represents the type of GPU anomaly detected.
type AnomalyType string

const (
	HighTemperature   AnomalyType = "high_temperature"
	MemoryLeak        AnomalyType = "memory_leak"
	UtilizationSpike  AnomalyType = "utilization_spike"
	PowerAnomaly      AnomalyType = "power_anomaly"
	ECCError          AnomalyType = "ecc_error"
	ThermalThrottling AnomalyType = "thermal_throttling"
)

// GPUMetric represents a single GPU measurement at a point in time.
type GPUMetric struct {
	NodeID      string    `json:"node_id"`
	GPUID       string    `json:"gpu_id"`
	Utilization float64   `json:"utilization"`
	MemoryUsed  float64   `json:"memory_used"`
	MemoryTotal float64   `json:"memory_total"`
	Temperature float64   `json:"temperature"`
	PowerDraw   float64   `json:"power_draw"`
	Timestamp   time.Time `json:"timestamp"`
}

// Anomaly represents a detected GPU anomaly.
type Anomaly struct {
	NodeID    string      `json:"node_id"`
	GPUID     string      `json:"gpu_id"`
	Type      AnomalyType `json:"type"`
	Severity  string      `json:"severity"`
	Message   string      `json:"message"`
	Timestamp time.Time   `json:"timestamp"`
}

// GPUHealthReport summarizes the health of a node's GPUs.
type GPUHealthReport struct {
	NodeID          string    `json:"node_id"`
	GPUCount        int       `json:"gpu_count"`
	HealthyGPUs     []string  `json:"healthy_gpus"`
	UnhealthyGPUs   []string  `json:"unhealthy_gpus"`
	AnomalyCount    int       `json:"anomaly_count"`
	LatestAnomalies []Anomaly `json:"latest_anomalies"`
}

// Thresholds for anomaly detection.
const (
	TemperatureCriticalThreshold = 85.0
	MemoryLeakGrowthRate         = 0.10
	UtilizationSpikeThreshold    = 95.0
	PowerAnomalyStdDevMultiplier = 3.0
	ECCErrorThreshold            = 1.0
	ThermalThrottlingTemp        = 80.0
)

// Detector provides GPU anomaly detection capabilities.
type Detector struct {
	mu      sync.RWMutex
	history []Anomaly
	metrics map[string][]GPUMetric // key: node_id:gpu_id
}

// NewDetector creates a new GPU anomaly Detector.
func NewDetector() *Detector {
	return &Detector{
		history: make([]Anomaly, 0),
		metrics: make(map[string][]GPUMetric),
	}
}

// DetectTemperatureAnomaly checks if GPU temperature exceeds critical threshold.
func (d *Detector) DetectTemperatureAnomaly(metric GPUMetric) *Anomaly {
	if metric.Temperature > TemperatureCriticalThreshold {
		return &Anomaly{
			NodeID:    metric.NodeID,
			GPUID:     metric.GPUID,
			Type:      HighTemperature,
			Severity:  severityForTemp(metric.Temperature),
			Message:   "GPU temperature exceeds critical threshold",
			Timestamp: metric.Timestamp,
		}
	}
	if metric.Temperature > ThermalThrottlingTemp {
		return &Anomaly{
			NodeID:    metric.NodeID,
			GPUID:     metric.GPUID,
			Type:      ThermalThrottling,
			Severity:  "warning",
			Message:   "GPU temperature approaching throttling range",
			Timestamp: metric.Timestamp,
		}
	}
	return nil
}

// DetectMemoryLeak analyzes memory usage history to detect leak patterns.
// A growth rate > 10%/hour sustained over the window indicates a leak.
func (d *Detector) DetectMemoryLeak(metrics []GPUMetric) *Anomaly {
	if len(metrics) < 2 {
		return nil
	}

	first := metrics[0]
	last := metrics[len(metrics)-1]

	if first.MemoryTotal == 0 {
		return nil
	}

	duration := last.Timestamp.Sub(first.Timestamp).Hours()
	if duration <= 0 {
		return nil
	}

	memUsedRatio := first.MemoryUsed / first.MemoryTotal
	memUsedRatioLast := last.MemoryUsed / last.MemoryTotal

	if memUsedRatioLast <= memUsedRatio {
		return nil
	}

	growthRate := (memUsedRatioLast - memUsedRatio) / duration

	if growthRate > MemoryLeakGrowthRate {
		return &Anomaly{
			NodeID:    last.NodeID,
			GPUID:     last.GPUID,
			Type:      MemoryLeak,
			Severity:  "critical",
			Message:   "Memory leak detected: growth rate exceeds 10%/hour",
			Timestamp: last.Timestamp,
		}
	}
	return nil
}

// DetectUtilizationSpike checks if GPU utilization is sustained above 95%.
func (d *Detector) DetectUtilizationSpike(metrics []GPUMetric) *Anomaly {
	if len(metrics) == 0 {
		return nil
	}

	sustainedCount := 0
	for _, m := range metrics {
		if m.Utilization > UtilizationSpikeThreshold {
			sustainedCount++
		}
	}

	if sustainedCount >= 3 {
		last := metrics[len(metrics)-1]
		return &Anomaly{
			NodeID:    last.NodeID,
			GPUID:     last.GPUID,
			Type:      UtilizationSpike,
			Severity:  "warning",
			Message:   "GPU utilization sustained above 95%",
			Timestamp: last.Timestamp,
		}
	}
	return nil
}

// DetectPowerAnomaly identifies power draw anomalies using statistical analysis.
func (d *Detector) DetectPowerAnomaly(metrics []GPUMetric) *Anomaly {
	if len(metrics) < 3 {
		return nil
	}

	values := make([]float64, len(metrics))
	for i, m := range metrics {
		values[i] = m.PowerDraw
	}

	mean := computeMean(values)
	std := computeStd(values, mean)

	if std == 0 {
		return nil
	}

	last := metrics[len(metrics)-1]
	deviation := math.Abs(last.PowerDraw-mean) / std

	if deviation > PowerAnomalyStdDevMultiplier {
		return &Anomaly{
			NodeID:    last.NodeID,
			GPUID:     last.GPUID,
			Type:      PowerAnomaly,
			Severity:  "warning",
			Message:   "Power draw anomaly detected",
			Timestamp: last.Timestamp,
		}
	}
	return nil
}

// FullGPUScan runs all detection methods on a set of GPU metrics.
func (d *Detector) FullGPUScan(metrics []GPUMetric) []Anomaly {
	d.mu.Lock()
	defer d.mu.Unlock()

	anomalies := make([]Anomaly, 0)

	// Group metrics by node_id:gpu_id
	grouped := make(map[string][]GPUMetric)
	for _, m := range metrics {
		key := m.NodeID + ":" + m.GPUID
		grouped[key] = append(grouped[key], m)
	}

	for _, group := range grouped {
		// Temperature check on latest metric
		latest := group[len(group)-1]
		if a := d.DetectTemperatureAnomaly(latest); a != nil {
			anomalies = append(anomalies, *a)
		}

		// Memory leak detection
		if a := d.DetectMemoryLeak(group); a != nil {
			anomalies = append(anomalies, *a)
		}

		// Utilization spike
		if a := d.DetectUtilizationSpike(group); a != nil {
			anomalies = append(anomalies, *a)
		}

		// Power anomaly
		if a := d.DetectPowerAnomaly(group); a != nil {
			anomalies = append(anomalies, *a)
		}
	}

	// Store anomalies in history
	d.history = append(d.history, anomalies...)

	return anomalies
}

// IngestMetrics stores GPU metrics for later analysis.
func (d *Detector) IngestMetrics(metrics []GPUMetric) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, m := range metrics {
		key := m.NodeID + ":" + m.GPUID
		d.metrics[key] = append(d.metrics[key], m)
	}
}

// GetHistory returns all detected anomalies.
func (d *Detector) GetHistory() []Anomaly {
	d.mu.RLock()
	defer d.mu.RUnlock()

	result := make([]Anomaly, len(d.history))
	copy(result, d.history)
	return result
}

// GetHealthReport generates a health report for a specific node.
func (d *Detector) GetHealthReport(nodeID string) GPUHealthReport {
	d.mu.RLock()
	defer d.mu.RUnlock()

	report := GPUHealthReport{
		NodeID:          nodeID,
		HealthyGPUs:     make([]string, 0),
		UnhealthyGPUs:   make([]string, 0),
		LatestAnomalies: make([]Anomaly, 0),
	}

	gpuSet := make(map[string]bool)
	for key, group := range d.metrics {
		parts := splitKey(key)
		if len(parts) != 2 || parts[0] != nodeID {
			continue
		}
		gpuID := parts[1]
		gpuSet[gpuID] = true

		hasAnomaly := false
		latest := group[len(group)-1]

		if a := d.DetectTemperatureAnomaly(latest); a != nil {
			hasAnomaly = true
			report.LatestAnomalies = append(report.LatestAnomalies, *a)
		}
		if a := d.DetectMemoryLeak(group); a != nil {
			hasAnomaly = true
			report.LatestAnomalies = append(report.LatestAnomalies, *a)
		}
		if a := d.DetectUtilizationSpike(group); a != nil {
			hasAnomaly = true
			report.LatestAnomalies = append(report.LatestAnomalies, *a)
		}
		if a := d.DetectPowerAnomaly(group); a != nil {
			hasAnomaly = true
			report.LatestAnomalies = append(report.LatestAnomalies, *a)
		}

		if hasAnomaly {
			report.UnhealthyGPUs = append(report.UnhealthyGPUs, gpuID)
		} else {
			report.HealthyGPUs = append(report.HealthyGPUs, gpuID)
		}
	}

	report.GPUCount = len(gpuSet)
	report.AnomalyCount = len(report.LatestAnomalies)

	return report
}

func severityForTemp(temp float64) string {
	if temp > 95 {
		return "critical"
	}
	if temp > 90 {
		return "high"
	}
	return "warning"
}

func computeMean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func computeStd(values []float64, mean float64) float64 {
	if len(values) < 2 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		diff := v - mean
		sum += diff * diff
	}
	return math.Sqrt(sum / float64(len(values)-1))
}

func splitKey(key string) []string {
	for i := 0; i < len(key); i++ {
		if key[i] == ':' {
			return []string{key[:i], key[i+1:]}
		}
	}
	return []string{key}
}
