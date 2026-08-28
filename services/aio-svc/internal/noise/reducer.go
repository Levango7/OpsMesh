package noise

import (
	"hash/fnv"
	"sort"
	"strings"
	"time"
)

// Reducer provides alert noise reduction capabilities.
type Reducer struct{}

// NewReducer creates a new Reducer.
func NewReducer() *Reducer {
	return &Reducer{}
}

// Alert represents an alert for noise reduction operations.
type Alert struct {
	Id        string
	TenantId  string
	RuleId    string
	RuleName  string
	DeviceId  string
	Severity  string
	Message   string
	FiredAt   time.Time
	Status    string
}

// AlertCluster represents a group of similar alerts.
type AlertCluster struct {
	ClusterKey string
	Alerts     []Alert
	Count      int32
	FirstFired time.Time
	LastFired  time.Time
}

// FlappingResult contains flapping detection output.
type FlappingResult struct {
	AlertId      string
	IsFlapping   bool
	StateChanges int32
	Frequency    float64
}

// ClusterKey generates a clustering key for an alert.
func clusterKey(a Alert) string {
	return a.RuleId + ":" + devicePrefix(a.DeviceId) + ":" + a.Severity
}

// devicePrefix extracts the prefix of a device ID for grouping.
func devicePrefix(deviceId string) string {
	parts := strings.Split(deviceId, "-")
	if len(parts) >= 2 {
		return parts[0] + "-" + parts[1]
	}
	return deviceId
}

// ClusterAlerts groups similar alerts together.
// Similarity: same rule, same device prefix, same severity.
func (r *Reducer) ClusterAlerts(alerts []Alert) []AlertCluster {
	if len(alerts) == 0 {
		return []AlertCluster{}
	}

	groups := make(map[string][]Alert)
	for _, a := range alerts {
		key := clusterKey(a)
		groups[key] = append(groups[key], a)
	}

	clusters := make([]AlertCluster, 0, len(groups))
	for key, group := range groups {
		first := group[0].FiredAt
		last := group[0].FiredAt
		for _, a := range group {
			if a.FiredAt.Before(first) {
				first = a.FiredAt
			}
			if a.FiredAt.After(last) {
				last = a.FiredAt
			}
		}
		clusters = append(clusters, AlertCluster{
			ClusterKey: key,
			Alerts:     group,
			Count:      int32(len(group)),
			FirstFired: first,
			LastFired:  last,
		})
	}

	// Sort clusters by count descending
	sort.Slice(clusters, func(i, j int) bool {
		return clusters[i].Count > clusters[j].Count
	})

	return clusters
}

// DetectFlapping detects if an alert is flapping (repeatedly firing/resolving).
// An alert is flapping if it changes state more than 3 times within the window.
func (r *Reducer) DetectFlapping(alertId string, window time.Duration, states []AlertState) FlappingResult {
	result := FlappingResult{
		AlertId: alertId,
	}

	if len(states) < 2 {
		return result
	}

	// Sort states by timestamp
	sorted := make([]AlertState, len(states))
	copy(sorted, states)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Timestamp.Before(sorted[j].Timestamp)
	})

	// Filter states within window
	now := sorted[len(sorted)-1].Timestamp
	cutoff := now.Add(-window)
	windowStates := make([]AlertState, 0)
	for _, s := range sorted {
		if s.Timestamp.After(cutoff) || s.Timestamp.Equal(cutoff) {
			windowStates = append(windowStates, s)
		}
	}

	if len(windowStates) < 2 {
		return result
	}

	// Count state changes
	changes := int32(0)
	for i := 1; i < len(windowStates); i++ {
		if windowStates[i].Status != windowStates[i-1].Status {
			changes++
		}
	}

	result.StateChanges = changes
	result.IsFlapping = changes > 3

	// Frequency: changes per hour
	if window > 0 {
		result.Frequency = float64(changes) / window.Hours()
	}

	return result
}

// CompressAlerts deduplicates and compresses alerts.
// Alerts with the same rule+device+message within 1 minute are compressed.
func (r *Reducer) CompressAlerts(alerts []Alert) ([]Alert, int, int, error) {
	if len(alerts) == 0 {
		return []Alert{}, 0, 0, nil
	}

	// Sort by fired time
	sorted := make([]Alert, len(alerts))
	copy(sorted, alerts)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].FiredAt.Before(sorted[j].FiredAt)
	})

	compressed := make([]Alert, 0, len(sorted))
	seen := make(map[uint64]bool)

	for _, a := range sorted {
		key := compressKey(a)
		if seen[key] {
			continue
		}
		seen[key] = true
		compressed = append(compressed, a)
	}

	return compressed, len(alerts), len(compressed), nil
}

// AlertState represents the state of an alert at a point in time.
type AlertState struct {
	Status    string
	Timestamp time.Time
}

// compressKey generates a deduplication key for an alert.
func compressKey(a Alert) uint64 {
	h := fnv.New64a()
	h.Write([]byte(a.RuleId))
	h.Write([]byte(a.DeviceId))
	h.Write([]byte(a.Message))
	// Round to 1-minute bucket
	bucket := a.FiredAt.Unix() / 60
	h.Write([]byte(string(rune(bucket))))
	return h.Sum64()
}
