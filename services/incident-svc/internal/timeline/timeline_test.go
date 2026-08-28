package timeline

import (
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/incident-svc/internal/models"
)

func TestBuild(t *testing.T) {
	b := NewBuilder()
	now := time.Now()

	events := []models.TimelineEvent{
		{ID: "3", Timestamp: now.Add(2 * time.Hour), Type: "resolve", Description: "Resolved"},
		{ID: "1", Timestamp: now, Type: "detect", Description: "Detected"},
		{ID: "2", Timestamp: now.Add(1 * time.Hour), Type: "investigate", Description: "Investigating"},
	}

	sorted := b.Build(events)
	if len(sorted) != 3 {
		t.Fatalf("expected 3 events, got %d", len(sorted))
	}
	if sorted[0].Type != "detect" {
		t.Errorf("expected first event 'detect', got %s", sorted[0].Type)
	}
	if sorted[1].Type != "investigate" {
		t.Errorf("expected second event 'investigate', got %s", sorted[1].Type)
	}
	if sorted[2].Type != "resolve" {
		t.Errorf("expected third event 'resolve', got %s", sorted[2].Type)
	}
}

func TestBuildEmpty(t *testing.T) {
	b := NewBuilder()
	sorted := b.Build([]models.TimelineEvent{})
	if len(sorted) != 0 {
		t.Error("expected empty result")
	}
}

func TestAddEvent(t *testing.T) {
	b := NewBuilder()
	now := time.Now()

	events := []models.TimelineEvent{
		{ID: "1", Timestamp: now, Type: "detect", Description: "Detected"},
	}

	newEvent := models.TimelineEvent{
		ID: "2", Timestamp: now.Add(time.Hour), Type: "resolve", Description: "Resolved",
	}

	events = b.AddEvent(events, newEvent)
	if len(events) != 2 {
		t.Errorf("expected 2 events, got %d", len(events))
	}
}

func TestDuration(t *testing.T) {
	b := NewBuilder()
	now := time.Now()

	events := []models.TimelineEvent{
		{ID: "1", Timestamp: now, Type: "detect", Description: "Detected"},
		{ID: "2", Timestamp: now.Add(30 * time.Minute), Type: "resolve", Description: "Resolved"},
	}

	d := b.Duration(events)
	if d != 30*time.Minute {
		t.Errorf("expected 30m duration, got %v", d)
	}
}

func TestDurationSingleEvent(t *testing.T) {
	b := NewBuilder()
	events := []models.TimelineEvent{
		{ID: "1", Timestamp: time.Now(), Type: "detect", Description: "Detected"},
	}

	d := b.Duration(events)
	if d != 0 {
		t.Errorf("expected 0 duration for single event, got %v", d)
	}
}

func TestFilterByType(t *testing.T) {
	b := NewBuilder()
	now := time.Now()

	events := []models.TimelineEvent{
		{ID: "1", Timestamp: now, Type: "detect", Description: "Detected"},
		{ID: "2", Timestamp: now.Add(time.Hour), Type: "comment", Description: "Note"},
		{ID: "3", Timestamp: now.Add(2 * time.Hour), Type: "comment", Description: "Update"},
	}

	filtered := b.FilterByType(events, "comment")
	if len(filtered) != 2 {
		t.Errorf("expected 2 comment events, got %d", len(filtered))
	}
}

func TestSummarize(t *testing.T) {
	b := NewBuilder()
	now := time.Now()

	events := []models.TimelineEvent{
		{ID: "1", Timestamp: now, Type: "detect", Description: "CPU spike detected"},
		{ID: "2", Timestamp: now.Add(time.Hour), Type: "resolve", Description: "Resolved by restart"},
	}

	summary := b.Summarize(events)
	if len(summary) != 2 {
		t.Fatalf("expected 2 summary lines, got %d", len(summary))
	}
	if summary[0] == "" {
		t.Error("expected non-empty summary line")
	}
}
