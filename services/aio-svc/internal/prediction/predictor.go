package prediction

import (
	"math"
)

// Predictor provides time-series prediction capabilities.
type Predictor struct{}

// NewPredictor creates a new Predictor.
func NewPredictor() *Predictor {
	return &Predictor{}
}

// CapacityPrediction contains linear regression prediction results.
type CapacityPrediction struct {
	PredictedValues []float64
	Slope           float64
	Intercept       float64
	Rsquared        float64
	Horizon         int
}

// TrendResult contains trend analysis results.
type TrendResult struct {
	Trend      string
	Slope      float64
	Confidence float64
}

// PredictCapacity uses linear regression to predict future values.
// It fits y = slope*x + intercept using least squares, then extrapolates.
func (p *Predictor) PredictCapacity(values []float64, horizon int) CapacityPrediction {
	result := CapacityPrediction{
		Horizon: horizon,
	}

	if len(values) < 2 || horizon <= 0 {
		return result
	}

	// Prepare x values (0, 1, 2, ...)
	n := float64(len(values))
	x := make([]float64, len(values))
	for i := range values {
		x[i] = float64(i)
	}

	// Compute means
	xMean := (n - 1) / 2
	yMean := 0.0
	for _, v := range values {
		yMean += v
	}
	yMean /= n

	// Compute slope and intercept using least squares
	var ssXY, ssXX float64
	for i := range values {
		dx := x[i] - xMean
		dy := values[i] - yMean
		ssXY += dx * dy
		ssXX += dx * dx
	}

	if ssXX == 0 {
		result.Slope = 0
		result.Intercept = yMean
		result.Rsquared = 0
		result.PredictedValues = make([]float64, horizon)
		for i := range result.PredictedValues {
			result.PredictedValues[i] = yMean
		}
		return result
	}

	slope := ssXY / ssXX
	intercept := yMean - slope*xMean

	result.Slope = slope
	result.Intercept = intercept

	// Compute R-squared
	var ssTot, ssRes float64
	for i, v := range values {
		predicted := slope*x[i] + intercept
		res := v - predicted
		diff := v - yMean
		ssRes += res * res
		ssTot += diff * diff
	}
	if ssTot > 0 {
		result.Rsquared = 1 - ssRes/ssTot
	}

	// Predict future values
	result.PredictedValues = make([]float64, horizon)
	for i := 0; i < horizon; i++ {
		futureX := float64(len(values) + i)
		result.PredictedValues[i] = slope*futureX + intercept
	}

	return result
}

// PredictTrend determines if the metric is increasing, decreasing, or stable.
func (p *Predictor) PredictTrend(values []float64) TrendResult {
	result := TrendResult{Trend: "stable"}

	if len(values) < 2 {
		return result
	}

	// Fit linear regression
	n := float64(len(values))
	xMean := (n - 1) / 2
	yMean := 0.0
	for _, v := range values {
		yMean += v
	}
	yMean /= n

	var ssXY, ssXX, ssTot float64
	for i, v := range values {
		dx := float64(i) - xMean
		dy := v - yMean
		ssXY += dx * dy
		ssXX += dx * dx
		ssTot += dy * dy
	}

	if ssXX == 0 {
		return result
	}

	slope := ssXY / ssXX
	result.Slope = slope

	// Compute R-squared as confidence
	var ssRes float64
	for i, v := range values {
		predicted := slope*float64(i) + (yMean - slope*xMean)
		res := v - predicted
		ssRes += res * res
	}
	if ssTot > 0 {
		result.Confidence = 1 - ssRes/ssTot
	}

	// Determine trend based on slope magnitude
	threshold := math.Abs(yMean) * 0.01
	if threshold < 0.001 {
		threshold = 0.001
	}

	if slope > threshold {
		result.Trend = "increasing"
	} else if slope < -threshold {
		result.Trend = "decreasing"
	} else {
		result.Trend = "stable"
	}

	return result
}

// Forecast generates a forecast for a device metric.
func (p *Predictor) Forecast(values []float64, hours int) ForecastResult {
	pred := p.PredictCapacity(values, hours)
	trend := p.PredictTrend(values)

	return ForecastResult{
		Values: pred.PredictedValues,
		Hours:  hours,
		Trend:  trend.Trend,
	}
}

// ForecastResult contains forecast output.
type ForecastResult struct {
	Values []float64
	Hours  int
	Trend  string
}
