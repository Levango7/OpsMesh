package anomaly

import (
	"math"
	"sort"
)

// Detector provides anomaly detection algorithms for time-series metrics.
type Detector struct{}

// NewDetector creates a new Detector.
func NewDetector() *Detector {
	return &Detector{}
}

// Detect finds anomaly indices in metric_values using the specified method.
// Supported methods: "zscore", "ewma", "iqr".
func (d *Detector) Detect(metricValues []float64, method string) ([]int, []float64) {
	switch method {
	case "zscore":
		return d.zscore(metricValues)
	case "ewma":
		return d.ewma(metricValues)
	case "iqr":
		return d.iqr(metricValues)
	default:
		return d.zscore(metricValues)
	}
}

// zscore detects anomalies using standard deviation.
// A point is anomalous if |z| > 3.
func (d *Detector) zscore(values []float64) ([]int, []float64) {
	if len(values) == 0 {
		return []int{}, []float64{}
	}

	mean := computeMean(values)
	std := computeStd(values, mean)
	if std == 0 {
		return []int{}, make([]float64, len(values))
	}

	indices := make([]int, 0)
	scores := make([]float64, len(values))
	for i, v := range values {
		z := (v - mean) / std
		scores[i] = math.Abs(z)
		if math.Abs(z) > 3.0 {
			indices = append(indices, i)
		}
	}
	return indices, scores
}

// ewma detects anomalies using exponentially weighted moving average.
// Alpha = 0.3. A point is anomalous if it deviates more than 3 stddev from EWMA.
func (d *Detector) ewma(values []float64) ([]int, []float64) {
	if len(values) == 0 {
		return []int{}, []float64{}
	}

	const alpha = 0.3
	ewmaVals := make([]float64, len(values))
	ewmaVals[0] = values[0]
	for i := 1; i < len(values); i++ {
		ewmaVals[i] = alpha*values[i] + (1-alpha)*ewmaVals[i-1]
	}

	// Compute stddev of residuals
	residuals := make([]float64, len(values))
	for i := range values {
		residuals[i] = values[i] - ewmaVals[i]
	}
	std := computeStd(residuals, computeMean(residuals))
	if std == 0 {
		return []int{}, make([]float64, len(values))
	}

	indices := make([]int, 0)
	scores := make([]float64, len(values))
	for i := range values {
		dev := math.Abs(residuals[i]) / std
		scores[i] = dev
		if dev > 3.0 {
			indices = append(indices, i)
		}
	}
	return indices, scores
}

// iqr detects anomalies using interquartile range.
// A point is anomalous if it falls below Q1-1.5*IQR or above Q3+1.5*IQR.
func (d *Detector) iqr(values []float64) ([]int, []float64) {
	if len(values) == 0 {
		return []int{}, []float64{}
	}

	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	q1 := percentile(sorted, 25)
	q3 := percentile(sorted, 75)
	iqr := q3 - q1
	lower := q1 - 1.5*iqr
	upper := q3 + 1.5*iqr

	indices := make([]int, 0)
	scores := make([]float64, len(values))
	for i, v := range values {
		if v < lower {
			scores[i] = (lower - v) / iqr
			indices = append(indices, i)
		} else if v > upper {
			scores[i] = (v - upper) / iqr
			indices = append(indices, i)
		}
	}
	return indices, scores
}

// BatchDetect runs detection on multiple requests.
func (d *Detector) BatchDetect(requests []BatchRequest) []BatchResult {
	results := make([]BatchResult, 0, len(requests))
	for _, req := range requests {
		indices, scores := d.Detect(req.MetricValues, req.Method)
		results = append(results, BatchResult{
			AnomalyIndices: indices,
			Scores:         scores,
			Method:         req.Method,
			TotalAnalyzed:  int32(len(req.MetricValues)),
		})
	}
	return results
}

// BatchRequest represents a single detection request in a batch.
type BatchRequest struct {
	MetricValues []float64
	Method       string
}

// BatchResult represents the result of a single detection in a batch.
type BatchResult struct {
	AnomalyIndices []int
	Scores         []float64
	Method         string
	TotalAnalyzed  int32
}

// Helper functions

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

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	rank := (p / 100.0) * float64(len(sorted)-1)
	lower := int(math.Floor(rank))
	upper := int(math.Ceil(rank))
	if lower == upper {
		return sorted[lower]
	}
	frac := rank - float64(lower)
	return sorted[lower] + frac*(sorted[upper]-sorted[lower])
}
