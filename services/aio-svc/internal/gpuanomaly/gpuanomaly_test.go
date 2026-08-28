package gpuanomaly

import (
	"testing"
	"time"
)

func TestDetectTemperatureAnomaly_Critical(t *testing.T) {
	d := NewDetector()
	metric := GPUMetric{
		NodeID:      "node-1",
		GPUID:       "gpu-0",
		Temperature: 92.0,
		Timestamp:   time.Now(),
	}
	a := d.DetectTemperatureAnomaly(metric)
	if a == nil {
		t.Fatal("expected temperature anomaly, got nil")
	}
	if a.Type != HighTemperature {
		t.Errorf("expected type high_temperature, got %s", a.Type)
	}
	if a.Severity != "high" {
		t.Errorf("expected severity high, got %s", a.Severity)
	}
}

func TestDetectTemperatureAnomaly_Warning(t *testing.T) {
	d := NewDetector()
	metric := GPUMetric{
		NodeID:      "node-1",
		GPUID:       "gpu-0",
		Temperature: 82.0,
		Timestamp:   time.Now(),
	}
	a := d.DetectTemperatureAnomaly(metric)
	if a == nil {
		t.Fatal("expected thermal throttling warning, got nil")
	}
	if a.Type != ThermalThrottling {
		t.Errorf("expected type thermal_throttling, got %s", a.Type)
	}
}

func TestDetectTemperatureAnomaly_Normal(t *testing.T) {
	d := NewDetector()
	metric := GPUMetric{
		NodeID:      "node-1",
		GPUID:       "gpu-0",
		Temperature: 65.0,
		Timestamp:   time.Now(),
	}
	a := d.DetectTemperatureAnomaly(metric)
	if a != nil {
		t.Errorf("expected no anomaly for normal temp, got %v", a)
	}
}

func TestDetectMemoryLeak_Positive(t *testing.T) {
	d := NewDetector()
	now := time.Now()
	metrics := []GPUMetric{
		{NodeID: "node-1", GPUID: "gpu-0", MemoryUsed: 1000, MemoryTotal: 10000, Timestamp: now.Add(-2 * time.Hour)},
		{NodeID: "node-1", GPUID: "gpu-0", MemoryUsed: 3500, MemoryTotal: 10000, Timestamp: now},
	}
	a := d.DetectMemoryLeak(metrics)
	if a == nil {
		t.Fatal("expected memory leak anomaly, got nil")
	}
	if a.Type != MemoryLeak {
		t.Errorf("expected type memory_leak, got %s", a.Type)
	}
}

func TestDetectMemoryLeak_Negative(t *testing.T) {
	d := NewDetector()
	now := time.Now()
	metrics := []GPUMetric{
		{NodeID: "node-1", GPUID: "gpu-0", MemoryUsed: 5000, MemoryTotal: 10000, Timestamp: now.Add(-2 * time.Hour)},
		{NodeID: "node-1", GPUID: "gpu-0", MemoryUsed: 5000, MemoryTotal: 10000, Timestamp: now},
	}
	a := d.DetectMemoryLeak(metrics)
	if a != nil {
		t.Errorf("expected no anomaly for stable memory, got %v", a)
	}
}

func TestDetectUtilizationSpike_Sustained(t *testing.T) {
	d := NewDetector()
	now := time.Now()
	metrics := []GPUMetric{
		{NodeID: "node-1", GPUID: "gpu-0", Utilization: 97, Timestamp: now.Add(-3 * time.Minute)},
		{NodeID: "node-1", GPUID: "gpu-0", Utilization: 98, Timestamp: now.Add(-2 * time.Minute)},
		{NodeID: "node-1", GPUID: "gpu-0", Utilization: 96, Timestamp: now.Add(-1 * time.Minute)},
		{NodeID: "node-1", GPUID: "gpu-0", Utilization: 99, Timestamp: now},
	}
	a := d.DetectUtilizationSpike(metrics)
	if a == nil {
		t.Fatal("expected utilization spike anomaly, got nil")
	}
	if a.Type != UtilizationSpike {
		t.Errorf("expected type utilization_spike, got %s", a.Type)
	}
}

func TestDetectUtilizationSpike_NotSustained(t *testing.T) {
	d := NewDetector()
	now := time.Now()
	metrics := []GPUMetric{
		{NodeID: "node-1", GPUID: "gpu-0", Utilization: 50, Timestamp: now.Add(-2 * time.Minute)},
		{NodeID: "node-1", GPUID: "gpu-0", Utilization: 99, Timestamp: now.Add(-1 * time.Minute)},
		{NodeID: "node-1", GPUID: "gpu-0", Utilization: 60, Timestamp: now},
	}
	a := d.DetectUtilizationSpike(metrics)
	if a != nil {
		t.Errorf("expected no anomaly for brief spike, got %v", a)
	}
}

func TestDetectPowerAnomaly_Outlier(t *testing.T) {
	d := NewDetector()
	now := time.Now()
	metrics := []GPUMetric{
		{NodeID: "node-1", GPUID: "gpu-0", PowerDraw: 250, Timestamp: now.Add(-19 * time.Minute)},
		{NodeID: "node-1", GPUID: "gpu-0", PowerDraw: 250, Timestamp: now.Add(-18 * time.Minute)},
		{NodeID: "node-1", GPUID: "gpu-0", PowerDraw: 250, Timestamp: now.Add(-17 * time.Minute)},
		{NodeID: "node-1", GPUID: "gpu-0", PowerDraw: 250, Timestamp: now.Add(-16 * time.Minute)},
		{NodeID: "node-1", GPUID: "gpu-0", PowerDraw: 250, Timestamp: now.Add(-15 * time.Minute)},
		{NodeID: "node-1", GPUID: "gpu-0", PowerDraw: 250, Timestamp: now.Add(-14 * time.Minute)},
		{NodeID: "node-1", GPUID: "gpu-0", PowerDraw: 250, Timestamp: now.Add(-13 * time.Minute)},
		{NodeID: "node-1", GPUID: "gpu-0", PowerDraw: 250, Timestamp: now.Add(-12 * time.Minute)},
		{NodeID: "node-1", GPUID: "gpu-0", PowerDraw: 250, Timestamp: now.Add(-11 * time.Minute)},
		{NodeID: "node-1", GPUID: "gpu-0", PowerDraw: 250, Timestamp: now.Add(-10 * time.Minute)},
		{NodeID: "node-1", GPUID: "gpu-0", PowerDraw: 250, Timestamp: now.Add(-9 * time.Minute)},
		{NodeID: "node-1", GPUID: "gpu-0", PowerDraw: 250, Timestamp: now.Add(-8 * time.Minute)},
		{NodeID: "node-1", GPUID: "gpu-0", PowerDraw: 250, Timestamp: now.Add(-7 * time.Minute)},
		{NodeID: "node-1", GPUID: "gpu-0", PowerDraw: 250, Timestamp: now.Add(-6 * time.Minute)},
		{NodeID: "node-1", GPUID: "gpu-0", PowerDraw: 250, Timestamp: now.Add(-5 * time.Minute)},
		{NodeID: "node-1", GPUID: "gpu-0", PowerDraw: 250, Timestamp: now.Add(-4 * time.Minute)},
		{NodeID: "node-1", GPUID: "gpu-0", PowerDraw: 250, Timestamp: now.Add(-3 * time.Minute)},
		{NodeID: "node-1", GPUID: "gpu-0", PowerDraw: 250, Timestamp: now.Add(-2 * time.Minute)},
		{NodeID: "node-1", GPUID: "gpu-0", PowerDraw: 250, Timestamp: now.Add(-1 * time.Minute)},
		{NodeID: "node-1", GPUID: "gpu-0", PowerDraw: 1000, Timestamp: now},
	}
	a := d.DetectPowerAnomaly(metrics)
	if a == nil {
		t.Fatal("expected power anomaly, got nil")
	}
	if a.Type != PowerAnomaly {
		t.Errorf("expected type power_anomaly, got %s", a.Type)
	}
}

func TestDetectPowerAnomaly_Normal(t *testing.T) {
	d := NewDetector()
	now := time.Now()
	metrics := []GPUMetric{
		{NodeID: "node-1", GPUID: "gpu-0", PowerDraw: 250, Timestamp: now.Add(-2 * time.Minute)},
		{NodeID: "node-1", GPUID: "gpu-0", PowerDraw: 248, Timestamp: now.Add(-1 * time.Minute)},
		{NodeID: "node-1", GPUID: "gpu-0", PowerDraw: 252, Timestamp: now},
	}
	a := d.DetectPowerAnomaly(metrics)
	if a != nil {
		t.Errorf("expected no anomaly for stable power, got %v", a)
	}
}

func TestFullGPUScan(t *testing.T) {
	d := NewDetector()
	now := time.Now()
	metrics := []GPUMetric{
		{NodeID: "node-1", GPUID: "gpu-0", Utilization: 97, MemoryUsed: 1000, MemoryTotal: 10000, Temperature: 92, PowerDraw: 250, Timestamp: now.Add(-2 * time.Minute)},
		{NodeID: "node-1", GPUID: "gpu-0", Utilization: 98, MemoryUsed: 3500, MemoryTotal: 10000, Temperature: 93, PowerDraw: 500, Timestamp: now},
	}
	anomalies := d.FullGPUScan(metrics)
	if len(anomalies) == 0 {
		t.Error("expected anomalies from full scan, got none")
	}
}

func TestGetHistory(t *testing.T) {
	d := NewDetector()
	now := time.Now()
	metrics := []GPUMetric{
		{NodeID: "node-1", GPUID: "gpu-0", Temperature: 92, Timestamp: now},
	}
	d.FullGPUScan(metrics)
	history := d.GetHistory()
	if len(history) == 0 {
		t.Error("expected history entries, got none")
	}
}

func TestGetHealthReport(t *testing.T) {
	d := NewDetector()
	now := time.Now()
	metrics := []GPUMetric{
		{NodeID: "node-1", GPUID: "gpu-0", Utilization: 50, MemoryUsed: 2000, MemoryTotal: 10000, Temperature: 65, PowerDraw: 250, Timestamp: now},
	}
	d.IngestMetrics(metrics)
	report := d.GetHealthReport("node-1")
	if report.GPUCount != 1 {
		t.Errorf("expected 1 GPU, got %d", report.GPUCount)
	}
	if len(report.HealthyGPUs) != 1 {
		t.Errorf("expected 1 healthy GPU, got %d", len(report.HealthyGPUs))
	}
}

func TestIngestMetrics(t *testing.T) {
	d := NewDetector()
	now := time.Now()
	metrics := []GPUMetric{
		{NodeID: "node-1", GPUID: "gpu-0", Temperature: 65, Timestamp: now},
		{NodeID: "node-1", GPUID: "gpu-0", Temperature: 66, Timestamp: now.Add(time.Minute)},
	}
	d.IngestMetrics(metrics)
	report := d.GetHealthReport("node-1")
	if report.GPUCount != 1 {
		t.Errorf("expected 1 GPU after ingest, got %d", report.GPUCount)
	}
}
