package prediction

import (
	"math"
	"testing"
)

func TestPredictCapacity_Increasing(t *testing.T) {
	p := NewPredictor()
	values := []float64{10, 20, 30, 40, 50}
	result := p.PredictCapacity(values, 3)
	if len(result.PredictedValues) != 3 {
		t.Errorf("expected 3 predicted values, got %d", len(result.PredictedValues))
	}
	if result.Slope <= 0 {
		t.Errorf("expected positive slope for increasing data, got %f", result.Slope)
	}
	// Predicted values should be > 50
	for i, v := range result.PredictedValues {
		if v <= 50 {
			t.Errorf("predicted[%d] = %f, expected > 50", i, v)
		}
	}
}

func TestPredictCapacity_Decreasing(t *testing.T) {
	p := NewPredictor()
	values := []float64{50, 40, 30, 20, 10}
	result := p.PredictCapacity(values, 2)
	if result.Slope >= 0 {
		t.Errorf("expected negative slope for decreasing data, got %f", result.Slope)
	}
}

func TestPredictCapacity_InsufficientData(t *testing.T) {
	p := NewPredictor()
	result := p.PredictCapacity([]float64{10}, 3)
	if len(result.PredictedValues) != 0 {
		t.Error("expected no predictions for single data point")
	}
}

func TestPredictCapacity_Rsquared(t *testing.T) {
	p := NewPredictor()
	// Perfect linear data
	values := []float64{0, 10, 20, 30, 40}
	result := p.PredictCapacity(values, 1)
	if result.Rsquared < 0.99 {
		t.Errorf("expected R^2 ~1.0 for perfect linear data, got %f", result.Rsquared)
	}
}

func TestPredictTrend_Increasing(t *testing.T) {
	p := NewPredictor()
	values := []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	result := p.PredictTrend(values)
	if result.Trend != "increasing" {
		t.Errorf("expected 'increasing', got '%s'", result.Trend)
	}
	if result.Slope <= 0 {
		t.Errorf("expected positive slope, got %f", result.Slope)
	}
}

func TestPredictTrend_Decreasing(t *testing.T) {
	p := NewPredictor()
	values := []float64{100, 90, 80, 70, 60, 50, 40, 30, 20, 10}
	result := p.PredictTrend(values)
	if result.Trend != "decreasing" {
		t.Errorf("expected 'decreasing', got '%s'", result.Trend)
	}
}

func TestPredictTrend_Stable(t *testing.T) {
	p := NewPredictor()
	values := []float64{50, 51, 49, 50, 51, 49, 50, 51, 49, 50}
	result := p.PredictTrend(values)
	if result.Trend != "stable" {
		t.Errorf("expected 'stable', got '%s'", result.Trend)
	}
}

func TestPredictTrend_InsufficientData(t *testing.T) {
	p := NewPredictor()
	result := p.PredictTrend([]float64{42})
	if result.Trend != "stable" {
		t.Errorf("expected 'stable' for single point, got '%s'", result.Trend)
	}
}

func TestForecast(t *testing.T) {
	p := NewPredictor()
	values := []float64{10, 20, 30, 40, 50}
	fc := p.Forecast(values, 5)
	if len(fc.Values) != 5 {
		t.Errorf("expected 5 forecast values, got %d", len(fc.Values))
	}
	if fc.Hours != 5 {
		t.Errorf("expected 5 hours, got %d", fc.Hours)
	}
	if fc.Trend != "increasing" {
		t.Errorf("expected 'increasing' trend, got '%s'", fc.Trend)
	}
}

func TestPredictCapacity_ZeroHorizon(t *testing.T) {
	p := NewPredictor()
	values := []float64{10, 20, 30}
	result := p.PredictCapacity(values, 0)
	if len(result.PredictedValues) != 0 {
		t.Error("expected no predictions for zero horizon")
	}
}

func TestPredictCapacity_ConstantValues(t *testing.T) {
	p := NewPredictor()
	values := []float64{50, 50, 50, 50, 50}
	result := p.PredictCapacity(values, 3)
	if math.Abs(result.Slope) > 0.001 {
		t.Errorf("expected ~0 slope for constant data, got %f", result.Slope)
	}
	for _, v := range result.PredictedValues {
		if math.Abs(v-50) > 0.001 {
			t.Errorf("expected predicted ~50 for constant data, got %f", v)
		}
	}
}
