package service

import (
	"fmt"
	"math"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/services/grafana-bridge/internal/models"
	"github.com/Levango7/OpsMesh/services/grafana-bridge/internal/query"
)

// DataProvider defines the interface for fetching metrics data.
type DataProvider interface {
	GetAvailableMetrics() []models.SearchTarget
	GetTimeSeries(pq *query.ParsedQuery) []models.TimeSeries
	GetTableData(pq *query.ParsedQuery) ([]string, [][]string)
	GetAnnotationsList(from, to time.Time) []models.Annotation
	GetTagKeys() []models.TagKey
	GetTagValues(key string) []models.TagValue
}

// MockDataProvider generates sample metrics data for testing.
type MockDataProvider struct {
	mu       sync.RWMutex
	devices  []string
	metrics  []string
	regions  []string
	statuses []string
}

// NewMockDataProvider creates a new MockDataProvider with default data.
func NewMockDataProvider() *MockDataProvider {
	return &MockDataProvider{
		devices:  []string{"device-001", "device-002", "device-003", "device-004", "device-005"},
		metrics:  []string{"cpu_usage", "memory_usage", "disk_usage", "network_traffic"},
		regions:  []string{"us-east-1", "us-west-2", "eu-central-1", "ap-southeast-1"},
		statuses: []string{"healthy", "warning", "critical"},
	}
}

// GetAvailableMetrics returns all available metric names for the search endpoint.
func (m *MockDataProvider) GetAvailableMetrics() []models.SearchTarget {
	m.mu.RLock()
	defer m.mu.RUnlock()

	targets := make([]models.SearchTarget, 0)
	for _, metric := range m.metrics {
		targets = append(targets, models.SearchTarget{
			Text: metric,
			Type: "timeseries",
		})
	}
	targets = append(targets, models.SearchTarget{Text: "device_list", Type: "table"})
	targets = append(targets, models.SearchTarget{Text: "alert_summary", Type: "table"})
	targets = append(targets, models.SearchTarget{Text: "metric_stats", Type: "table"})
	return targets
}

// GetTimeSeries generates time-series data for the given query.
func (m *MockDataProvider) GetTimeSeries(pq *query.ParsedQuery) []models.TimeSeries {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if pq.Interval <= 0 {
		pq.Interval = time.Minute
	}

	metricName := pq.MetricName
	if metricName == "" {
		metricName = "cpu_usage"
	}

	series := make([]models.TimeSeries, 0)
	devices := m.filterDevices(pq.Tags)

	for _, device := range devices {
		points := m.generateDataPoints(pq.From, pq.To, pq.Interval, metricName, device)
		tags := map[string]string{"device": device}
		for k, v := range pq.Tags {
			tags[k] = v
		}
		series = append(series, models.TimeSeries{
			Target: fmt.Sprintf("%s{%s}", metricName, formatTags(tags)),
			Tags:   tags,
			Points: points,
		})
	}

	return series
}

// GetTableData generates table data for the given query.
func (m *MockDataProvider) GetTableData(pq *query.ParsedQuery) ([]string, [][]string) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	metricName := pq.MetricName

	switch metricName {
	case "device_list":
		return m.deviceListTable()
	case "alert_summary":
		return m.alertSummaryTable()
	case "metric_stats":
		return m.metricStatsTable(pq)
	default:
		return m.metricStatsTable(pq)
	}
}

// GetAnnotationsList returns alert annotations for the given time range.
func (m *MockDataProvider) GetAnnotationsList(from, to time.Time) []models.Annotation {
	m.mu.RLock()
	defer m.mu.RUnlock()

	annotations := make([]models.Annotation, 0)
	rng := rand.New(rand.NewSource(from.Unix()))

	numAlerts := rng.Intn(5) + 1
	for i := 0; i < numAlerts; i++ {
		alertTime := from.Add(time.Duration(rng.Int63n(int64(to.Sub(from)))))
		device := m.devices[rng.Intn(len(m.devices))]
		severity := m.statuses[rng.Intn(len(m.statuses))]

		annotations = append(annotations, models.Annotation{
			Time:  alertTime,
			Title: fmt.Sprintf("Alert on %s", device),
			Text:  fmt.Sprintf("Metric threshold exceeded on device %s (severity: %s)", device, severity),
			Tags:  []string{"alert", severity, device},
		})
	}

	return annotations
}

// GetTagKeys returns available tag keys.
func (m *MockDataProvider) GetTagKeys() []models.TagKey {
	return []models.TagKey{
		{Type: "string", Text: "device"},
		{Type: "string", Text: "region"},
		{Type: "string", Text: "status"},
	}
}

// GetTagValues returns values for a given tag key.
func (m *MockDataProvider) GetTagValues(key string) []models.TagValue {
	m.mu.RLock()
	defer m.mu.RUnlock()

	switch key {
	case "device":
		values := make([]models.TagValue, 0, len(m.devices))
		for _, d := range m.devices {
			values = append(values, models.TagValue{Text: d})
		}
		return values
	case "region":
		values := make([]models.TagValue, 0, len(m.regions))
		for _, r := range m.regions {
			values = append(values, models.TagValue{Text: r})
		}
		return values
	case "status":
		values := make([]models.TagValue, 0, len(m.statuses))
		for _, s := range m.statuses {
			values = append(values, models.TagValue{Text: s})
		}
		return values
	default:
		return []models.TagValue{}
	}
}

// filterDevices returns devices matching the given tags filter.
func (m *MockDataProvider) filterDevices(tags map[string]string) []string {
	if len(tags) == 0 {
		return m.devices
	}

	filtered := make([]string, 0)
	for _, d := range m.devices {
		match := true
		for k, v := range tags {
			if k == "device" && d != v {
				match = false
				break
			}
		}
		if match {
			filtered = append(filtered, d)
		}
	}

	if len(filtered) == 0 {
		return m.devices
	}
	return filtered
}

// generateDataPoints creates time-series data points for a metric.
func (m *MockDataProvider) generateDataPoints(from, to time.Time, interval time.Duration, metric, device string) []models.DataPoint {
	rng := rand.New(rand.NewSource(int64(len(device)) + from.Unix()))
	points := make([]models.DataPoint, 0)

	baseValue := m.getBaseValue(metric)
	amplitude := m.getAmplitude(metric)

	for t := from; t.Before(to) || t.Equal(to); t = t.Add(interval) {
		elapsed := t.Sub(from).Seconds()
		trend := math.Sin(elapsed/300.0) * amplitude
		noise := (rng.Float64() - 0.5) * amplitude * 0.3
		value := baseValue + trend + noise

		value = math.Max(0, math.Min(100, value))
		if metric == "network_traffic" {
			value = math.Max(0, value*10)
		}

		points = append(points, models.DataPoint{
			Timestamp: t,
			Value:     math.Round(value*100) / 100,
		})
	}

	return points
}

func (m *MockDataProvider) getBaseValue(metric string) float64 {
	switch metric {
	case "cpu_usage":
		return 45.0
	case "memory_usage":
		return 60.0
	case "disk_usage":
		return 55.0
	case "network_traffic":
		return 50.0
	default:
		return 50.0
	}
}

func (m *MockDataProvider) getAmplitude(metric string) float64 {
	switch metric {
	case "cpu_usage":
		return 25.0
	case "memory_usage":
		return 15.0
	case "disk_usage":
		return 10.0
	case "network_traffic":
		return 30.0
	default:
		return 20.0
	}
}

func (m *MockDataProvider) deviceListTable() ([]string, [][]string) {
	columns := []string{"Device", "Region", "Status", "CPU %", "Memory %"}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	rows := make([][]string, 0, len(m.devices))

	for _, d := range m.devices {
		region := m.regions[rng.Intn(len(m.regions))]
		status := m.statuses[rng.Intn(len(m.statuses))]
		cpu := fmt.Sprintf("%.1f", rng.Float64()*100)
		mem := fmt.Sprintf("%.1f", rng.Float64()*100)
		rows = append(rows, []string{d, region, status, cpu, mem})
	}

	return columns, rows
}

func (m *MockDataProvider) alertSummaryTable() ([]string, [][]string) {
	columns := []string{"Alert", "Severity", "Count", "Last Fired"}
	rows := [][]string{
		{"High CPU", "critical", "12", "2 min ago"},
		{"High Memory", "warning", "5", "15 min ago"},
		{"Disk Full", "critical", "2", "1 hour ago"},
		{"Network Spike", "warning", "8", "30 min ago"},
	}
	return columns, rows
}

func (m *MockDataProvider) metricStatsTable(pq *query.ParsedQuery) ([]string, [][]string) {
	columns := []string{"Metric", "Min", "Max", "Avg", "Current"}
	metricName := pq.MetricName
	if metricName == "" {
		metricName = "cpu_usage"
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	min := fmt.Sprintf("%.1f", rng.Float64()*30)
	max := fmt.Sprintf("%.1f", 70+rng.Float64()*30)
	avg := fmt.Sprintf("%.1f", 30+rng.Float64()*40)
	current := fmt.Sprintf("%.1f", rng.Float64()*100)

	rows := [][]string{{metricName, min, max, avg, current}}
	return columns, rows
}

// Service implements the grafana-bridge business logic.
type Service struct {
	provider DataProvider
}

// NewService creates a new Service with the given data provider.
func NewService(provider DataProvider) *Service {
	return &Service{provider: provider}
}

// Search returns available metric names.
func (s *Service) Search() []models.SearchTarget {
	return s.provider.GetAvailableMetrics()
}

// Query executes a query and returns time-series or table data.
func (s *Service) Query(pq *query.ParsedQuery) ([]models.TimeSeries, []string, [][]string) {
	switch pq.Format {
	case "table":
		cols, rows := s.provider.GetTableData(pq)
		return nil, cols, rows
	default:
		series := s.provider.GetTimeSeries(pq)
		return series, nil, nil
	}
}

// Annotations returns annotations for the given time range.
func (s *Service) Annotations(from, to time.Time) []models.Annotation {
	return s.provider.GetAnnotationsList(from, to)
}

// TagKeys returns available tag keys.
func (s *Service) TagKeys() []models.TagKey {
	return s.provider.GetTagKeys()
}

// TagValues returns values for a tag key.
func (s *Service) TagValues(key string) []models.TagValue {
	return s.provider.GetTagValues(key)
}

func formatTags(tags map[string]string) string {
	parts := make([]string, 0, len(tags))
	for k, v := range tags {
		parts = append(parts, fmt.Sprintf(`%s="%s"`, k, v))
	}
	return strings.Join(parts, ", ")
}
