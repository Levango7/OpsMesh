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

	inc, err := svc.CreateIncident(CreateIncidentInput{Title: "CPU Spike", Description: "High CPU on server", Severity: models.SeverityHigh, DeviceIDs: []string{"device-1"}})
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

	_, err := svc.CreateIncident(CreateIncidentInput{Title: "", Description: "desc", Severity: models.SeverityHigh, DeviceIDs: nil})
	if err == nil {
		t.Error("expected error for empty title")
	}
}

func TestGetIncident(t *testing.T) {
	svc := newTestService()

	created, err := svc.CreateIncident(CreateIncidentInput{Title: "Test", Description: "desc", Severity: models.SeverityMedium, DeviceIDs: nil})
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
		_, err := svc.CreateIncident(CreateIncidentInput{Title: "Incident", Description: "desc", Severity: models.SeverityHigh, DeviceIDs: nil})
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

	inc, _ := svc.CreateIncident(CreateIncidentInput{Title: "Test", Description: "desc", Severity: models.SeverityHigh, DeviceIDs: nil})
	_, _ = svc.ResolveIncident(inc.ID, "tester")

	list := svc.ListIncidents(string(models.StatusResolved), "")
	if len(list) != 1 {
		t.Errorf("expected 1 resolved incident, got %d", len(list))
	}
}

func TestUpdateIncident(t *testing.T) {
	svc := newTestService()

	inc, _ := svc.CreateIncident(CreateIncidentInput{Title: "Original", Description: "desc", Severity: models.SeverityHigh, DeviceIDs: nil})
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

	inc, _ := svc.CreateIncident(CreateIncidentInput{Title: "ToDelete", Description: "desc", Severity: models.SeverityLow, DeviceIDs: nil})
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

	inc, _ := svc.CreateIncident(CreateIncidentInput{Title: "Test", Description: "desc", Severity: models.SeverityHigh, DeviceIDs: nil})
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

	inc, _ := svc.CreateIncident(CreateIncidentInput{Title: "Test", Description: "desc", Severity: models.SeverityHigh, DeviceIDs: nil})
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

	inc, _ := svc.CreateIncident(CreateIncidentInput{Title: "Test", Description: "desc", Severity: models.SeverityHigh, DeviceIDs: nil})
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

	inc, _ := svc.CreateIncident(CreateIncidentInput{Title: "Test", Description: "desc", Severity: models.SeverityHigh, DeviceIDs: nil})
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

	inc, _ := svc.CreateIncident(CreateIncidentInput{Title: "Test Incident", Description: "desc", Severity: models.SeverityHigh, DeviceIDs: []string{"device-1"}})
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

	inc, _ := svc.CreateIncident(CreateIncidentInput{Title: "Test", Description: "desc", Severity: models.SeverityHigh, DeviceIDs: nil})
	_, _ = svc.ResolveIncident(inc.ID, "tester")

	metrics := svc.GetResponseMetrics()
	if metrics.TotalIncidents != 1 {
		t.Errorf("expected 1 total incident, got %d", metrics.TotalIncidents)
	}
	if metrics.ResolvedIncidents != 1 {
		t.Errorf("expected 1 resolved incident, got %d", metrics.ResolvedIncidents)
	}
}

// TestIngestAlert_EscalatesByAlertCount 是 #62 那条"ShouldEscalate 从未被调用"的回归位：
// 事故级别必须随后续告警上升，而不是永远停在第一条告警的级别上。
func TestIngestAlert_EscalatesByAlertCount(t *testing.T) {
	svc := newTestService()
	svc.engine.AddRule(&aggregate.AggregationRule{
		ID: "rule-1", DeviceIDs: []string{"device-1"}, MetricPatterns: []string{"cpu_usage"}, Enabled: true,
	})

	var last *models.Incident
	for i := 1; i <= 5; i++ {
		inc, err := svc.IngestAlert(&models.Alert{
			ID: "alert-" + string(rune('a'+i)), DeviceID: "device-1", Metric: "cpu_usage",
			Severity: models.SeverityLow, Message: "cpu high",
		})
		if err != nil {
			t.Fatalf("IngestAlert(%d): %v", i, err)
		}
		last = inc
		switch i {
		case 3:
			// 第 3 条起 ShouldEscalate 判 medium（低级别 + 计数≥3）。
			if inc.Severity != models.SeverityMedium {
				t.Fatalf("第 3 条后 severity = %s, want medium", inc.Severity)
			}
		case 5:
			if inc.Severity != models.SeverityHigh {
				t.Fatalf("第 5 条后 severity = %s, want high", inc.Severity)
			}
		}
	}

	// 升级必须留时间线痕迹（值班与复盘要看得到"什么时候被抬过级别"）。
	events, err := svc.GetTimeline(last.ID)
	if err != nil {
		t.Fatalf("GetTimeline: %v", err)
	}
	var escalated int
	for _, e := range events {
		if e.Type == "escalated" {
			escalated++
		}
	}
	if escalated != 2 {
		t.Fatalf("escalated 事件 %d 条，期望 2（low→medium、medium→high）：%+v", escalated, events)
	}

	// 第 10 条 ⇒ critical。
	for i := 6; i <= 10; i++ {
		inc, err := svc.IngestAlert(&models.Alert{
			ID: "alert-x" + string(rune('a'+i)), DeviceID: "device-1", Metric: "cpu_usage",
			Severity: models.SeverityLow, Message: "cpu high",
		})
		if err != nil {
			t.Fatalf("IngestAlert(%d): %v", i, err)
		}
		if i == 10 && inc.Severity != models.SeverityCritical {
			t.Fatalf("第 10 条后 severity = %s, want critical", inc.Severity)
		}
	}
}

// TestIngestAlert_NeverDowngrades 钉住"只升不降"：
// 一条 critical 事故不会因为后续进来低级别告警被拉回 low。
func TestIngestAlert_NeverDowngrades(t *testing.T) {
	svc := newTestService()
	svc.engine.AddRule(&aggregate.AggregationRule{
		ID: "rule-1", DeviceIDs: []string{"device-1"}, MetricPatterns: []string{"cpu_usage"}, Enabled: true,
	})

	inc, err := svc.IngestAlert(&models.Alert{
		ID: "a-1", DeviceID: "device-1", Metric: "cpu_usage", Severity: models.SeverityCritical, Message: "down",
	})
	if err != nil {
		t.Fatalf("IngestAlert: %v", err)
	}
	if inc.Severity != models.SeverityCritical {
		t.Fatalf("首条 critical 事故 severity = %s", inc.Severity)
	}

	inc2, err := svc.IngestAlert(&models.Alert{
		ID: "a-2", DeviceID: "device-1", Metric: "cpu_usage", Severity: models.SeverityInfo, Message: "noise",
	})
	if err != nil {
		t.Fatalf("IngestAlert(2): %v", err)
	}
	if inc2.Severity != models.SeverityCritical {
		t.Fatalf("critical 事故被降级成 %s", inc2.Severity)
	}
}

// TestIngestAlert_EscalatesOnIncomingHigherSeverity 验证"进来的告警比事故更严重"这条
// 也会被抬级别（修前只按事故自己的级别算，升级条件永远追不上现场）。
func TestIngestAlert_EscalatesOnIncomingHigherSeverity(t *testing.T) {
	svc := newTestService()
	svc.engine.AddRule(&aggregate.AggregationRule{
		ID: "rule-1", DeviceIDs: []string{"device-1"}, MetricPatterns: []string{"cpu_usage"}, Enabled: true,
	})
	if _, err := svc.IngestAlert(&models.Alert{
		ID: "b-1", DeviceID: "device-1", Metric: "cpu_usage", Severity: models.SeverityLow, Message: "warm",
	}); err != nil {
		t.Fatalf("IngestAlert: %v", err)
	}
	inc, err := svc.IngestAlert(&models.Alert{
		ID: "b-2", DeviceID: "device-1", Metric: "cpu_usage", Severity: models.SeverityCritical, Message: "on fire",
	})
	if err != nil {
		t.Fatalf("IngestAlert(2): %v", err)
	}
	if inc.Severity != models.SeverityCritical {
		t.Fatalf("收到 critical 告警后事故仍是 %s", inc.Severity)
	}
	events, _ := svc.GetTimeline(inc.ID)
	var found bool
	for _, e := range events {
		if e.Type == "escalated" {
			found = true
		}
	}
	if !found {
		t.Fatalf("升级没有留时间线痕迹：%+v", events)
	}
}
