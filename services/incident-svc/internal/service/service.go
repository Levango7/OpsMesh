package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Levango7/OpsMesh/services/incident-svc/internal/aggregate"
	"github.com/Levango7/OpsMesh/services/incident-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/incident-svc/internal/timeline"
)

// Errors returned by the service.
var (
	ErrIncidentNotFound = errors.New("incident not found")
	ErrInvalidStatus    = errors.New("invalid status transition")
	ErrIncidentResolved = errors.New("incident already resolved")
)

// Service implements the incident management business logic.
type Service struct {
	store    models.IncidentStore
	engine   *aggregate.Engine
	timeline *timeline.Builder
}

// NewService creates a new Service.
func NewService(store models.IncidentStore, eng *aggregate.Engine) *Service {
	return &Service{
		store:    store,
		engine:   eng,
		timeline: timeline.NewBuilder(),
	}
}

// CreateIncident creates a new incident.
func (s *Service) CreateIncident(title, description string, severity models.Severity, deviceIDs []string) (*models.Incident, error) {
	if title == "" {
		return nil, errors.New("incident title is required")
	}

	now := time.Now()
	inc := &models.Incident{
		ID:          uuid.New().String(),
		Title:       title,
		Description: description,
		Severity:    severity,
		Status:      models.StatusDetected,
		DeviceIDs:   deviceIDs,
		DetectedAt:  now,
		CreatedAt:   now,
		UpdatedAt:   now,
		Tags:        make(map[string]string),
	}

	s.store.CreateIncident(inc)

	_ = s.store.AddTimelineEvent(&models.TimelineEvent{
		ID:          uuid.New().String(),
		IncidentID:  inc.ID,
		Timestamp:   now,
		Type:        "created",
		Description: "Incident created: " + title,
		Author:      "system",
	})

	return inc, nil
}

// GetIncident retrieves an incident by ID.
func (s *Service) GetIncident(id string) (*models.Incident, error) {
	inc := s.store.GetIncident(id)
	if inc == nil {
		return nil, ErrIncidentNotFound
	}
	return inc, nil
}

// ListIncidents lists incidents filtered by status and severity.
func (s *Service) ListIncidents(status string, severity models.Severity) []*models.Incident {
	return s.store.ListIncidents(status, severity)
}

// UpdateIncident updates an existing incident.
func (s *Service) UpdateIncident(id, title, description, assignee string, severity models.Severity) (*models.Incident, error) {
	inc, err := s.GetIncident(id)
	if err != nil {
		return nil, err
	}

	if inc.Status == models.StatusResolved || inc.Status == models.StatusClosed {
		return nil, ErrIncidentResolved
	}

	if title != "" {
		inc.Title = title
	}
	if description != "" {
		inc.Description = description
	}
	if assignee != "" {
		inc.Assignee = assignee
	}
	if severity != "" {
		inc.Severity = severity
	}

	s.store.UpdateIncident(inc)
	return inc, nil
}

// DeleteIncident deletes an incident.
func (s *Service) DeleteIncident(id string) error {
	if !s.store.DeleteIncident(id) {
		return ErrIncidentNotFound
	}
	return nil
}

// AddTimelineEvent adds an event to an incident's timeline.
func (s *Service) AddTimelineEvent(incidentID, eventType, description, author string) (*models.TimelineEvent, error) {
	inc, err := s.GetIncident(incidentID)
	if err != nil {
		return nil, err
	}

	ev := &models.TimelineEvent{
		ID:          uuid.New().String(),
		IncidentID:  inc.ID,
		Timestamp:   time.Now(),
		Type:        eventType,
		Description: description,
		Author:      author,
	}

	s.store.AddTimelineEvent(ev)
	return ev, nil
}

// GetTimeline returns the full timeline for an incident.
func (s *Service) GetTimeline(incidentID string) ([]models.TimelineEvent, error) {
	_, err := s.GetIncident(incidentID)
	if err != nil {
		return nil, err
	}

	events := s.store.GetTimeline(incidentID)
	return s.timeline.Build(events), nil
}

// ResolveIncident transitions an incident to resolved.
func (s *Service) ResolveIncident(id, author string) (*models.Incident, error) {
	inc, err := s.GetIncident(id)
	if err != nil {
		return nil, err
	}

	if inc.Status == models.StatusResolved || inc.Status == models.StatusClosed {
		return nil, ErrIncidentResolved
	}

	now := time.Now()
	inc.Status = models.StatusResolved
	inc.ResolvedAt = &now
	s.store.UpdateIncident(inc)

	_ = s.store.AddTimelineEvent(&models.TimelineEvent{
		ID:          uuid.New().String(),
		IncidentID:  inc.ID,
		Timestamp:   now,
		Type:        "resolved",
		Description: "Incident resolved",
		Author:      author,
	})

	return inc, nil
}

// CloseIncident transitions an incident to closed.
func (s *Service) CloseIncident(id, author string) (*models.Incident, error) {
	inc, err := s.GetIncident(id)
	if err != nil {
		return nil, err
	}

	if inc.Status == models.StatusClosed {
		return nil, ErrInvalidStatus
	}

	now := time.Now()
	inc.Status = models.StatusClosed
	inc.ClosedAt = &now
	s.store.UpdateIncident(inc)

	_ = s.store.AddTimelineEvent(&models.TimelineEvent{
		ID:          uuid.New().String(),
		IncidentID:  inc.ID,
		Timestamp:   now,
		Type:        "closed",
		Description: "Incident closed",
		Author:      author,
	})

	return inc, nil
}

// GeneratePostmortem generates a postmortem document for a resolved incident.
func (s *Service) GeneratePostmortem(incidentID string) (*models.Postmortem, error) {
	inc, err := s.GetIncident(incidentID)
	if err != nil {
		return nil, err
	}

	events := s.store.GetTimeline(incidentID)
	sortedEvents := s.timeline.Build(events)

	var mttd, mttr time.Duration
	if !inc.DetectedAt.IsZero() && inc.ResolvedAt != nil {
		mttr = inc.ResolvedAt.Sub(inc.DetectedAt)
	}

	pm := &models.Postmortem{
		IncidentID:  inc.ID,
		Title:       "Postmortem: " + inc.Title,
		Summary:     fmt.Sprintf("Incident %s occurred on %s with severity %s.", inc.ID, inc.DetectedAt.Format(time.RFC3339), inc.Severity),
		Impact:      fmt.Sprintf("Affected devices: %v", inc.DeviceIDs),
		RootCause:   "Pending root cause analysis",
		Timeline:    sortedEvents,
		MTTD:        mttd,
		MTTR:        mttr,
		GeneratedAt: time.Now(),
		LessonsLearned: []string{
			"Review monitoring coverage",
			"Validate alert thresholds",
		},
		ActionItems: []string{
			"Update runbooks",
			"Improve detection time",
		},
	}

	return pm, nil
}

// IngestAlert ingests an alert and auto-aggregates it into an incident.
func (s *Service) IngestAlert(alert *models.Alert) (*models.Incident, error) {
	if alert == nil {
		return nil, errors.New("alert is nil")
	}

	if alert.ID == "" {
		alert.ID = uuid.New().String()
	}
	if alert.Timestamp.IsZero() {
		alert.Timestamp = time.Now()
	}

	result := s.engine.Aggregate(alert)
	if !result.Matched {
		return nil, errors.New("alert did not match any aggregation rule")
	}

	inc, err := s.GetIncident(result.IncidentID)
	if err != nil {
		title := fmt.Sprintf("Incident for %s", alert.DeviceID)
		inc, err = s.CreateIncident(title, alert.Message, alert.Severity, []string{alert.DeviceID})
		if err != nil {
			return nil, fmt.Errorf("failed to create incident: %w", err)
		}
	}

	inc.AlertIDs = append(inc.AlertIDs, alert.ID)
	s.store.UpdateIncident(inc)

	_ = s.store.AddTimelineEvent(&models.TimelineEvent{
		ID:          uuid.New().String(),
		IncidentID:  inc.ID,
		Timestamp:   time.Now(),
		Type:        "alert_ingested",
		Description: "Alert ingested: " + alert.Message,
		Author:      "system",
	})

	return inc, nil
}

// GetResponseMetrics calculates response metrics across all incidents.
func (s *Service) GetResponseMetrics() *models.ResponseMetrics {
	incidents := s.store.Incidents()

	metrics := &models.ResponseMetrics{
		TotalIncidents: len(incidents),
	}

	var totalMTTD, totalMTTR time.Duration
	var mttdCount, mttrCount int

	for _, inc := range incidents {
		switch inc.Status {
		case models.StatusDetected, models.StatusInvestigating, models.StatusMitigating:
			metrics.ActiveIncidents++
		case models.StatusResolved, models.StatusClosed:
			metrics.ResolvedIncidents++
		}

		if inc.ResolvedAt != nil {
			mttr := inc.ResolvedAt.Sub(inc.DetectedAt)
			if mttr > 0 {
				totalMTTR += mttr
				mttrCount++
			}
		}
	}

	if mttrCount > 0 {
		metrics.AvgMTTR = totalMTTR / time.Duration(mttrCount)
	}
	if mttdCount > 0 {
		metrics.AvgMTTD = totalMTTD / time.Duration(mttdCount)
	}
	_ = totalMTTD

	return metrics
}
