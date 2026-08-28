package service

import (
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/incident-svc/internal/aggregate"
	"github.com/Levango7/OpsMesh/services/incident-svc/internal/models"
)

func newTestService() *Service {
	store := models.NewMemoryStore()
	eng := aggregate.NewEngine(5 * time.Minute)
	return NewService(store, eng)
}

func TestCreateIncident(t *testing.T) {
	svc := newTestService()

	inc, err := svc.CreateIncident("CPU Spike", "High CPU on server", models.SeverityHigh, []string{"device-1"})
	if err != nil {
		t.Fatalf("CreateIncident failed: %v", err)
	}

	if inc.ID == "" {
		t.Error("expected incident ID to be set")
	}
	if inc.Status != models.StatusDetected {
		t.Errorf("expected status detected, got %s", inc.Status)
	}
	if inc.Title != "CPU Spike" {
		t.Errorf("expected title 'CPU Spike', got %s", inc.Title)
	}
}

func TestCreateIncidentEmptyTitle(t *testing.T) {
	svc := newTestService()

	_, err := svc.CreateIncident("", "desc", models.SeverityHigh, nil)
	if err == nil {
		t.Error("expected error for empty title")
	}
}

func TestGetIncident(t *testing.T) {
	svc := newTestService()

	created, err := svc.CreateIncident("Test", "desc", models.SeverityMedium, nil)
	if err != nil {
		t.Fatalf("CreateIncident failed: %v", err)
	}

	got, err := svc.GetIncident(created.ID)
	if err != nil {
		t.Fatalf("GetIncident failed: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("expected ID %s, got %s", created.ID, got.ID)
	}
}

func TestGetIncidentNotFound(t *testing.T) {
	svc := newTestService()

	_, err := svc.GetIncident("nonexistent")
	if err != ErrIncidentNotFound {
		t.Fatalf("expected ErrIncidentNotFound, got: %v", err)
	}
}

func TestListIncidents(t *testing.T) {
	svc := newTestService()

	for i := 0; i < 3; i++ {
		_, err := svc.CreateIncident("Incident", "desc", models.SeverityHigh, nil)
		if err != nil {
			t.Fatalf("CreateIncident failed: %v", err)
		}
	}

	list := svc.ListIncidents("", "")
	if len(list) != 3 {
		t.Errorf("expected 3 incidents, got %d", len(list))
	}
}

func TestListIncidentsFilterByStatus(t *testing.T) {
	svc := newTestService()

	inc, _ := svc.CreateIncident("Test", "desc", models.SeverityHigh, nil)
	_, _ = svc.ResolveIncident(inc.ID, "tester")

	list := svc.ListIncidents(string(models.StatusResolved), "")
	if len(list) != 1 {
		t.Errorf("expected 1 resolved incident, got %d", len(list))
	}
}

func TestUpdateIncident(t *testing.T) {
	svc := newTestService()

	inc, _ := svc.CreateIncident("Original", "desc", models.SeverityHigh, nil)
	updated, err := svc.UpdateIncident(inc.ID, "Updated", "new desc", "user-1", models.SeverityCritical)
	if err != nil {
		t.Fatalf("UpdateIncident failed: %v", err)
	}

	if updated.Title != "Updated" {
		t.Errorf("expected title 'Updated', got %s", updated.Title)
	}
	if updated.Assignee != "user-1" {
		t.Errorf("expected assignee 'user-1', got %s", updated.Assignee)
	}
	if updated.Severity != models.SeverityCritical {
		t.Errorf("expected severity critical, got %s", updated.Severity)
	}
}

func TestUpdateIncidentNotFound(t *testing.T) {
	svc := newTestService()

	_, err := svc.UpdateIncident("nonexistent", "title", "", "", "")
	if err != ErrIncidentNotFound {
		t.Fatalf("expected ErrIncidentNotFound, got: %v", err)
	}
}

func TestDeleteIncident(t *testing.T) {
	svc := newTestService()

	inc, _ := svc.CreateIncident("ToDelete", "desc", models.SeverityLow, nil)
	err := svc.DeleteIncident(inc.ID)
	if err != nil {
		t.Fatalf("DeleteIncident failed: %v", err)
	}

	_, err = svc.GetIncident(inc.ID)
	if err != ErrIncidentNotFound {
		t.Error("expected incident to be deleted")
	}
}

func TestDeleteIncidentNotFound(t *testing.T) {
	svc := newTestService()

	err := svc.DeleteIncident("nonexistent")
	if err != ErrIncidentNotFound {
		t.Fatalf("expected ErrIncidentNotFound, got: %v", err)
	}
}

func TestResolveIncident(t *testing.T) {
	svc := newTestService()

	inc, _ := svc.CreateIncident("Test", "desc", models.SeverityHigh, nil)
	resolved, err := svc.ResolveIncident(inc.ID, "tester")
	if err != nil {
		t.Fatalf("ResolveIncident failed: %v", err)
	}

	if resolved.Status != models.StatusResolved {
		t.Errorf("expected status resolved, got %s", resolved.Status)
	}
	if resolved.ResolvedAt == nil {
		t.Error("expected ResolvedAt to be set")
	}
}

func TestResolveIncidentNotFound(t *testing.T) {
	svc := newTestService()

	_, err := svc.ResolveIncident("nonexistent", "tester")
	if err != ErrIncidentNotFound {
		t.Fatalf("expected ErrIncidentNotFound, got: %v", err)
	}
}

func TestCloseIncident(t *testing.T) {
	svc := newTestService()

	inc, _ := svc.CreateIncident("Test", "desc", models.SeverityHigh, nil)
	closed, err := svc.CloseIncident(inc.ID, "tester")
	if err != nil {
		t.Fatalf("CloseIncident failed: %v", err)
	}

	if closed.Status != models.StatusClosed {
		t.Errorf("expected status closed, got %s", closed.Status)
	}
	if closed.ClosedAt == nil {
		t.Error("expected ClosedAt to be set")
	}
}

func TestAddTimelineEvent(t *testing.T) {
	svc := newTestService()

	inc, _ := svc.CreateIncident("Test", "desc", models.SeverityHigh, nil)
	ev, err := svc.AddTimelineEvent(inc.ID, "comment", "Investigating issue", "user-1")
	if err != nil {
		t.Fatalf("AddTimelineEvent failed: %v", err)
	}

	if ev.IncidentID != inc.ID {
		t.Errorf("expected incident ID %s, got %s", inc.ID, ev.IncidentID)
	}
	if ev.Type != "comment" {
		t.Errorf("expected type 'comment', got %s", ev.Type)
	}
}

func TestGetTimeline(t *testing.T) {
	svc := newTestService()

	inc, _ := svc.CreateIncident("Test", "desc", models.SeverityHigh, nil)
	_, _ = svc.AddTimelineEvent(inc.ID, "comment", "First", "user-1")
	_, _ = svc.AddTimelineEvent(inc.ID, "comment", "Second", "user-2")

	timeline, err := svc.GetTimeline(inc.ID)
	if err != nil {
		t.Fatalf("GetTimeline failed: %v", err)
	}

	if len(timeline) < 3 {
		t.Errorf("expected at least 3 timeline events, got %d", len(timeline))
	}
}

func TestGeneratePostmortem(t *testing.T) {
	svc := newTestService()

	inc, _ := svc.CreateIncident("Test Incident", "desc", models.SeverityHigh, []string{"device-1"})
	inc.DetectedAt = time.Now().Add(-30 * time.Minute)
	svc.store.UpdateIncident(inc)
	_, _ = svc.ResolveIncident(inc.ID, "tester")

	pm, err := svc.GeneratePostmortem(inc.ID)
	if err != nil {
		t.Fatalf("GeneratePostmortem failed: %v", err)
	}

	if pm.IncidentID != inc.ID {
		t.Errorf("expected incident ID %s, got %s", inc.ID, pm.IncidentID)
	}
	if pm.MTTR <= 0 {
		t.Error("expected positive MTTR")
	}
}

func TestGeneratePostmortemNotFound(t *testing.T) {
	svc := newTestService()

	_, err := svc.GeneratePostmortem("nonexistent")
	if err != ErrIncidentNotFound {
		t.Fatalf("expected ErrIncidentNotFound, got: %v", err)
	}
}

func TestIngestAlert(t *testing.T) {
	svc := newTestService()

	svc.engine.AddRule(&aggregate.AggregationRule{
		ID:             "rule-1",
		DeviceIDs:      []string{"device-1"},
		MetricPatterns: []string{"cpu_usage"},
		Enabled:        true,
	})

	alert := &models.Alert{
		ID:       "alert-1",
		DeviceID: "device-1",
		Metric:   "cpu_usage",
		Severity: models.SeverityHigh,
		Message:  "CPU at 95%",
	}

	inc, err := svc.IngestAlert(alert)
	if err != nil {
		t.Fatalf("IngestAlert failed: %v", err)
	}

	if len(inc.AlertIDs) != 1 {
		t.Errorf("expected 1 alert ID, got %d", len(inc.AlertIDs))
	}
}

func TestIngestAlertNoMatch(t *testing.T) {
	svc := newTestService()

	alert := &models.Alert{
		ID:       "alert-1",
		DeviceID: "device-1",
		Metric:   "cpu_usage",
	}

	_, err := svc.IngestAlert(alert)
	if err == nil {
		t.Error("expected error for unmatched alert")
	}
}

func TestGetResponseMetrics(t *testing.T) {
	svc := newTestService()

	inc, _ := svc.CreateIncident("Test", "desc", models.SeverityHigh, nil)
	_, _ = svc.ResolveIncident(inc.ID, "tester")

	metrics := svc.GetResponseMetrics()
	if metrics.TotalIncidents != 1 {
		t.Errorf("expected 1 total incident, got %d", metrics.TotalIncidents)
	}
	if metrics.ResolvedIncidents != 1 {
		t.Errorf("expected 1 resolved incident, got %d", metrics.ResolvedIncidents)
	}
}
