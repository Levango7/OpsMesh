package service

import (
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/grafana-bridge/internal/query"
)

func newTestService() *Service {
	provider := NewMockDataProvider()
	return NewService(provider)
}

func TestNewService(t *testing.T) {
	svc := newTestService()
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
	if svc.provider == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestSearch(t *testing.T) {
	svc := newTestService()
	targets := svc.Search()
	if len(targets) == 0 {
		t.Error("expected non-empty search results")
	}

	found := false
	for _, tgt := range targets {
		if tgt.Text == "cpu_usage" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected cpu_usage in search results")
	}
}

func TestSearchContainsTableMetrics(t *testing.T) {
	svc := newTestService()
	targets := svc.Search()

	tableMetrics := []string{"device_list", "alert_summary", "metric_stats"}
	for _, name := range tableMetrics {
		found := false
		for _, tgt := range targets {
			if tgt.Text == name && tgt.Type == "table" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected table metric %q in search results", name)
		}
	}
}

func TestQueryTimeseries(t *testing.T) {
	svc := newTestService()
	now := time.Now()
	pq := &query.ParsedQuery{
		MetricName: "cpu_usage",
		From:       now.Add(-1 * time.Hour),
		To:         now,
		Interval:   5 * time.Minute,
		Format:     "timeseries",
		Tags:       make(map[string]string),
	}

	series, cols, rows := svc.Query(pq)
	if cols != nil {
		t.Error("expected nil columns for timeseries query")
	}
	if rows != nil {
		t.Error("expected nil rows for timeseries query")
	}
	if len(series) == 0 {
		t.Error("expected non-empty time series data")
	}

	for _, s := range series {
		if len(s.Points) == 0 {
			t.Errorf("expected non-empty points for series %s", s.Target)
		}
	}
}

func TestQueryTable(t *testing.T) {
	svc := newTestService()
	now := time.Now()
	pq := &query.ParsedQuery{
		MetricName: "device_list",
		From:       now.Add(-1 * time.Hour),
		To:         now,
		Interval:   5 * time.Minute,
		Format:     "table",
		Tags:       make(map[string]string),
	}

	series, cols, rows := svc.Query(pq)
	if series != nil {
		t.Error("expected nil series for table query")
	}
	if cols == nil {
		t.Error("expected non-nil columns for table query")
	}
	if rows == nil {
		t.Error("expected non-nil rows for table query")
	}
	if len(cols) == 0 {
		t.Error("expected non-empty columns")
	}
	if len(rows) == 0 {
		t.Error("expected non-empty rows")
	}
}

func TestQueryDefaultFormat(t *testing.T) {
	svc := newTestService()
	now := time.Now()
	pq := &query.ParsedQuery{
		MetricName: "memory_usage",
		From:       now.Add(-30 * time.Minute),
		To:         now,
		Interval:   time.Minute,
		Format:     "",
		Tags:       make(map[string]string),
	}

	series, cols, rows := svc.Query(pq)
	if cols != nil || rows != nil {
		t.Error("expected timeseries format for empty query format")
	}
	if len(series) == 0 {
		t.Error("expected non-empty time series data for default format")
	}
}

func TestAnnotations(t *testing.T) {
	svc := newTestService()
	now := time.Now()
	annotations := svc.Annotations(now.Add(-1*time.Hour), now)

	if len(annotations) == 0 {
		t.Error("expected non-empty annotations")
	}

	for _, a := range annotations {
		if a.Title == "" {
			t.Error("expected non-empty annotation title")
		}
		if a.Text == "" {
			t.Error("expected non-empty annotation text")
		}
	}
}

func TestTagKeys(t *testing.T) {
	svc := newTestService()
	keys := svc.TagKeys()
	if len(keys) == 0 {
		t.Error("expected non-empty tag keys")
	}

	found := false
	for _, k := range keys {
		if k.Text == "device" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'device' tag key")
	}
}

func TestTagValuesDevice(t *testing.T) {
	svc := newTestService()
	values := svc.TagValues("device")
	if len(values) == 0 {
		t.Error("expected non-empty tag values for 'device'")
	}
}

func TestTagValuesRegion(t *testing.T) {
	svc := newTestService()
	values := svc.TagValues("region")
	if len(values) == 0 {
		t.Error("expected non-empty tag values for 'region'")
	}
}

func TestTagValuesUnknown(t *testing.T) {
	svc := newTestService()
	values := svc.TagValues("unknown_key")
	if len(values) != 0 {
		t.Error("expected empty tag values for unknown key")
	}
}

func TestMockDataProviderGetTimeSeries(t *testing.T) {
	provider := NewMockDataProvider()
	now := time.Now()
	pq := &query.ParsedQuery{
		MetricName: "cpu_usage",
		From:       now.Add(-1 * time.Hour),
		To:         now,
		Interval:   5 * time.Minute,
		Format:     "timeseries",
		Tags:       make(map[string]string),
	}

	series := provider.GetTimeSeries(pq)
	if len(series) == 0 {
		t.Fatal("expected non-empty series")
	}

	for _, s := range series {
		if s.Target == "" {
			t.Error("expected non-empty target name")
		}
		if len(s.Points) == 0 {
			t.Error("expected non-empty data points")
		}
		for _, p := range s.Points {
			if p.Timestamp.IsZero() {
				t.Error("expected non-zero timestamp")
			}
		}
	}
}

func TestMockDataProviderGetTableData(t *testing.T) {
	provider := NewMockDataProvider()
	now := time.Now()

	testCases := []string{"device_list", "alert_summary", "metric_stats"}
	for _, metric := range testCases {
		pq := &query.ParsedQuery{
			MetricName: metric,
			From:       now.Add(-1 * time.Hour),
			To:         now,
			Interval:   time.Minute,
			Format:     "table",
			Tags:       make(map[string]string),
		}
		cols, rows := provider.GetTableData(pq)
		if len(cols) == 0 {
			t.Errorf("expected non-empty columns for %s", metric)
		}
		if len(rows) == 0 {
			t.Errorf("expected non-empty rows for %s", metric)
		}
	}
}

func TestQueryDiskUsage(t *testing.T) {
	svc := newTestService()
	now := time.Now()
	pq := &query.ParsedQuery{
		MetricName: "disk_usage",
		From:       now.Add(-30 * time.Minute),
		To:         now,
		Interval:   time.Minute,
		Format:     "timeseries",
		Tags:       make(map[string]string),
	}

	series, _, _ := svc.Query(pq)
	if len(series) == 0 {
		t.Error("expected non-empty disk usage series")
	}
}

func TestQueryNetworkTraffic(t *testing.T) {
	svc := newTestService()
	now := time.Now()
	pq := &query.ParsedQuery{
		MetricName: "network_traffic",
		From:       now.Add(-30 * time.Minute),
		To:         now,
		Interval:   time.Minute,
		Format:     "timeseries",
		Tags:       make(map[string]string),
	}

	series, _, _ := svc.Query(pq)
	if len(series) == 0 {
		t.Error("expected non-empty network traffic series")
	}
}

func TestAnnotationsTimeRange(t *testing.T) {
	svc := newTestService()
	now := time.Now()
	from := now.Add(-24 * time.Hour)
	to := now

	annotations := svc.Annotations(from, to)
	for _, a := range annotations {
		if a.Time.Before(from) || a.Time.After(to) {
			t.Errorf("annotation time %v outside range [%v, %v]", a.Time, from, to)
		}
	}
}
