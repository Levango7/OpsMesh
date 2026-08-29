package models

import "time"

// QueryType represents the type of Grafana query.
type QueryType string

const (
	QueryTypeTimeseries  QueryType = "timeseries"
	QueryTypeTable       QueryType = "table"
	QueryTypeAnnotations QueryType = "annotations"
)

// MetricType represents a specific metric being queried.
type MetricType string

const (
	MetricCPUUsage       MetricType = "cpu_usage"
	MetricMemoryUsage    MetricType = "memory_usage"
	MetricDiskUsage      MetricType = "disk_usage"
	MetricNetworkTraffic MetricType = "network_traffic"
)

// DataPoint represents a single time-series data point.
type DataPoint struct {
	Timestamp time.Time
	Value     float64
}

// TimeSeries represents a named series of data points.
type TimeSeries struct {
	Target string
	Tags   map[string]string
	Points []DataPoint
}

// TableRow represents a row in a table response.
type TableRow struct {
	Columns []TableColumn
}

// TableColumn represents a column value in a table row.
type TableColumn struct {
	Text string
}

// Annotation represents a Grafana annotation event.
type Annotation struct {
	Time  time.Time
	Title string
	Text  string
	Tags  []string
}

// TagKey represents an available tag key.
type TagKey struct {
	Type string
	Text string
}

// TagValue represents a value for a tag key.
type TagValue struct {
	Text string
}

// SearchTarget represents a metric available for querying.
type SearchTarget struct {
	Text string
	Type string
}
