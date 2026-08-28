package timeline

import (
	"sort"
	"time"

	"github.com/Levango7/OpsMesh/services/incident-svc/internal/models"
)

// Builder constructs incident timelines.
type Builder struct{}

// NewBuilder creates a new timeline Builder.
func NewBuilder() *Builder {
	return &Builder{}
}

// Build constructs a timeline from a set of events, sorted chronologically.
func (b *Builder) Build(events []models.TimelineEvent) []models.TimelineEvent {
	sorted := make([]models.TimelineEvent, len(events))
	copy(sorted, events)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Timestamp.Before(sorted[j].Timestamp)
	})
	return sorted
}

// AddEvent appends a new event to the timeline and returns the sorted result.
func (b *Builder) AddEvent(events []models.TimelineEvent, ev models.TimelineEvent) []models.TimelineEvent {
	events = append(events, ev)
	return b.Build(events)
}

// Duration returns the total duration from first to last event.
func (b *Builder) Duration(events []models.TimelineEvent) time.Duration {
	if len(events) < 2 {
		return 0
	}
	sorted := b.Build(events)
	return sorted[len(sorted)-1].Timestamp.Sub(sorted[0].Timestamp)
}

// FilterByType returns events matching the given type.
func (b *Builder) FilterByType(events []models.TimelineEvent, eventType string) []models.TimelineEvent {
	out := make([]models.TimelineEvent, 0)
	for _, ev := range events {
		if ev.Type == eventType {
			out = append(out, ev)
		}
	}
	return out
}

// Summarize generates a human-readable timeline summary.
func (b *Builder) Summarize(events []models.TimelineEvent) []string {
	sorted := b.Build(events)
	summary := make([]string, 0, len(sorted))
	for _, ev := range sorted {
		summary = append(summary, ev.Timestamp.Format(time.RFC3339)+" ["+ev.Type+"] "+ev.Description)
	}
	return summary
}
