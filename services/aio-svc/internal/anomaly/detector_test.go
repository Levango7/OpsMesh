package anomaly

import (
	"reflect"
	"testing"
)

func TestZScore_EmptyInput(t *testing.T) {
	d := NewDetector()
	indices, scores := d.zscore([]float64{})
	if len(indices) != 0 {
		t.Errorf("expected 0 indices, got %d", len(indices))
	}
	if len(scores) != 0 {
		t.Errorf("expected 0 scores, got %d", len(scores))
	}
}

func TestZScore_NoAnomalies(t *testing.T) {
	d := NewDetector()
	// Normal data within 3 sigma
	values := []float64{10, 11, 9, 10, 11, 9, 10, 10, 11, 9}
	indices, _ := d.zscore(values)
	if len(indices) != 0 {
		t.Errorf("expected no anomalies, got %v", indices)
	}
}

func TestZScore_WithAnomaly(t *testing.T) {
	d := NewDetector()
	// One clear outlier among many normal values
	values := []float64{10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 100}
	indices, scores := d.zscore(values)
	if len(indices) == 0 {
		t.Error("expected at least one anomaly")
	}
	// Index 100 (value=100) should be detected
	found := false
	for _, idx := range indices {
		if idx == 100 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected index 100 to be anomalous, got %v", indices)
	}
	if scores[100] <= 3.0 {
		t.Errorf("expected score > 3.0 for anomaly at index 100, got %f", scores[100])
	}
}

func TestZScore_UniformValues(t *testing.T) {
	d := NewDetector()
	values := []float64{5, 5, 5, 5, 5}
	indices, _ := d.zscore(values)
	if len(indices) != 0 {
		t.Errorf("expected no anomalies for uniform data, got %v", indices)
	}
}

func TestEWMA_DetectsSpike(t *testing.T) {
	d := NewDetector()
	// Stable then spike
	values := []float64{10, 10, 10, 10, 10, 10, 10, 10, 10, 100}
	indices, _ := d.ewma(values)
	if len(indices) == 0 {
		t.Error("expected EWMA to detect spike")
	}
}

func TestEWMA_NoFalsePositive(t *testing.T) {
	d := NewDetector()
	values := []float64{10, 11, 9, 10, 11, 9, 10, 11, 9, 10}
	indices, _ := d.ewma(values)
	if len(indices) != 0 {
		t.Errorf("expected no anomalies for stable data, got %v", indices)
	}
}

func TestIQR_DetectsOutlier(t *testing.T) {
	d := NewDetector()
	// IQR should detect 100 as outlier
	values := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 100}
	indices, _ := d.iqr(values)
	if len(indices) == 0 {
		t.Error("expected IQR to detect outlier")
	}
	found := false
	for _, idx := range indices {
		if idx == 9 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected index 9 to be anomalous, got %v", indices)
	}
}

func TestIQR_NoOutlier(t *testing.T) {
	d := NewDetector()
	values := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	indices, _ := d.iqr(values)
	if len(indices) != 0 {
		t.Errorf("expected no anomalies for uniform data, got %v", indices)
	}
}

func TestDetect_DefaultsToZScore(t *testing.T) {
	d := NewDetector()
	values := []float64{10, 11, 9, 10, 100}
	indices1, _ := d.Detect(values, "unknown")
	indices2, _ := d.Detect(values, "zscore")
	if !reflect.DeepEqual(indices1, indices2) {
		t.Error("unknown method should default to zscore")
	}
}

func TestBatchDetect(t *testing.T) {
	d := NewDetector()
	requests := []BatchRequest{
		{MetricValues: []float64{10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 1000}, Method: "zscore"},
		{MetricValues: []float64{1, 2, 3, 4, 5}, Method: "iqr"},
	}
	results := d.BatchDetect(requests)
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
	if results[0].Method != "zscore" {
		t.Errorf("expected zscore method, got %s", results[0].Method)
	}
	if len(results[0].AnomalyIndices) == 0 {
		t.Error("expected anomalies in first batch")
	}
}

func TestPercentile(t *testing.T) {
	sorted := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	q1 := percentile(sorted, 25)
	q3 := percentile(sorted, 75)
	if q1 != 3.25 {
		t.Errorf("expected Q1=3.25, got %f", q1)
	}
	if q3 != 7.75 {
		t.Errorf("expected Q3=7.75, got %f", q3)
	}
}
