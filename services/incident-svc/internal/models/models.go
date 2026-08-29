package models

import (
	"sync"
	"time"
)

// IncidentStatus represents the lifecycle stage of an incident.
type IncidentStatus string

const (
	StatusDetected      IncidentStatus = "detected"
	StatusInvestigating IncidentStatus = "investigating"
	StatusMitigating    IncidentStatus = "mitigating"
	StatusResolved      IncidentStatus = "resolved"
	StatusClosed        IncidentStatus = "closed"
)

// Severity represents the severity level of an incident.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityInfo     Severity = "info"
)

// Incident represents a managed incident aggregating related alerts.
type Incident struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Severity    Severity          `json:"severity"`
	Status      IncidentStatus    `json:"status"`
	AlertIDs    []string          `json:"alert_ids"`
	DeviceIDs   []string          `json:"device_ids"`
	Assignee    string            `json:"assignee"`
	Tags        map[string]string `json:"tags"`
	DetectedAt  time.Time         `json:"detected_at"`
	ResolvedAt  *time.Time        `json:"resolved_at,omitempty"`
	ClosedAt    *time.Time        `json:"closed_at,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// TimelineEvent represents a single event in an incident timeline.
type TimelineEvent struct {
	ID          string    `json:"id"`
	IncidentID  string    `json:"incident_id"`
	Timestamp   time.Time `json:"timestamp"`
	Type        string    `json:"type"`
	Description string    `json:"description"`
	Author      string    `json:"author"`
}

// Alert represents an ingested alert that can be aggregated into incidents.
type Alert struct {
	ID        string            `json:"id"`
	Source    string            `json:"source"`
	DeviceID  string            `json:"device_id"`
	Severity  Severity          `json:"severity"`
	Message   string            `json:"message"`
	Metric    string            `json:"metric"`
	Timestamp time.Time         `json:"timestamp"`
	Labels    map[string]string `json:"labels"`
}

// Postmortem represents a generated postmortem document.
type Postmortem struct {
	IncidentID     string          `json:"incident_id"`
	Title          string          `json:"title"`
	Summary        string          `json:"summary"`
	Impact         string          `json:"impact"`
	RootCause      string          `json:"root_cause"`
	Timeline       []TimelineEvent `json:"timeline"`
	LessonsLearned []string        `json:"lessons_learned"`
	ActionItems    []string        `json:"action_items"`
	MTTD           time.Duration   `json:"mttd"`
	MTTR           time.Duration   `json:"mttr"`
	GeneratedAt    time.Time       `json:"generated_at"`
}

// ResponseMetrics holds incident response metrics.
type ResponseMetrics struct {
	TotalIncidents    int           `json:"total_incidents"`
	ActiveIncidents   int           `json:"active_incidents"`
	ResolvedIncidents int           `json:"resolved_incidents"`
	AvgMTTD           time.Duration `json:"avg_mttd"`
	AvgMTTR           time.Duration `json:"avg_mttr"`
	AvgMTTF           time.Duration `json:"avg_mttf"`
}

// IncidentStore is the interface for incident persistence.
type IncidentStore interface {
	CreateIncident(*Incident) *Incident
	GetIncident(id string) *Incident
	UpdateIncident(*Incident) *Incident
	DeleteIncident(id string) bool
	ListIncidents(status string, severity Severity) []*Incident
	AddTimelineEvent(*TimelineEvent) *TimelineEvent
	GetTimeline(incidentID string) []TimelineEvent
	Incidents() []*Incident
}

// MemoryStore is an in-memory implementation of IncidentStore.
type MemoryStore struct {
	mu        sync.RWMutex
	incidents map[string]*Incident
	timeline  map[string][]TimelineEvent
}

// NewMemoryStore creates a new MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		incidents: make(map[string]*Incident),
		timeline:  make(map[string][]TimelineEvent),
	}
}

// CreateIncident stores a new incident.
func (m *MemoryStore) CreateIncident(inc *Incident) *Incident {
	if inc == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if inc.CreatedAt.IsZero() {
		inc.CreatedAt = time.Now()
	}
	if inc.UpdatedAt.IsZero() {
		inc.UpdatedAt = inc.CreatedAt
	}
	m.incidents[inc.ID] = inc
	return inc
}

// GetIncident retrieves an incident by ID.
func (m *MemoryStore) GetIncident(id string) *Incident {
	m.mu.RLock()
	defer m.mu.RUnlock()
	inc, ok := m.incidents[id]
	if !ok {
		return nil
	}
	return inc
}

// UpdateIncident updates an existing incident.
func (m *MemoryStore) UpdateIncident(inc *Incident) *Incident {
	if inc == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.incidents[inc.ID]; !ok {
		return nil
	}
	inc.UpdatedAt = time.Now()
	m.incidents[inc.ID] = inc
	return inc
}

// DeleteIncident removes an incident.
func (m *MemoryStore) DeleteIncident(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.incidents[id]; !ok {
		return false
	}
	delete(m.incidents, id)
	delete(m.timeline, id)
	return true
}

// ListIncidents returns incidents filtered by status and severity.
func (m *MemoryStore) ListIncidents(status string, severity Severity) []*Incident {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Incident, 0, len(m.incidents))
	for _, inc := range m.incidents {
		if status != "" && string(inc.Status) != status {
			continue
		}
		if severity != "" && inc.Severity != severity {
			continue
		}
		out = append(out, inc)
	}
	return out
}

// AddTimelineEvent adds an event to an incident's timeline.
func (m *MemoryStore) AddTimelineEvent(ev *TimelineEvent) *TimelineEvent {
	if ev == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now()
	}
	m.timeline[ev.IncidentID] = append(m.timeline[ev.IncidentID], *ev)
	return ev
}

// GetTimeline returns the timeline for an incident.
func (m *MemoryStore) GetTimeline(incidentID string) []TimelineEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	events := m.timeline[incidentID]
	out := make([]TimelineEvent, len(events))
	copy(out, events)
	return out
}

// Incidents returns all incidents.
func (m *MemoryStore) Incidents() []*Incident {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Incident, 0, len(m.incidents))
	for _, inc := range m.incidents {
		out = append(out, inc)
	}
	return out
}
