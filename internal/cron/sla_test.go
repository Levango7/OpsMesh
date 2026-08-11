package cron

import (
	"testing"
	"time"
)

func TestSLAMonitor_WarnBeforeBreach(t *testing.T) {
	var events []SLABreachEvent
	m := NewSLAMonitor(func(e SLABreachEvent) { events = append(events, e) })
	fixedNow := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	m.SetNow(func() time.Time { return fixedNow })

	m.Track(&SLAConfig{
		TaskID:   "task-1",
		Deadline: 10 * time.Minute,
		WarnAt:   5 * time.Minute,
	})

	// 4 分钟：未触发。
	evs := m.Check(fixedNow.Add(4 * time.Minute))
	if len(evs) != 0 {
		t.Fatalf("expect 0 events at 4min, got %d", len(evs))
	}
	// 6 分钟：触发 warn。
	evs = m.Check(fixedNow.Add(6 * time.Minute))
	if len(evs) != 1 || evs[0].Kind != "warn" {
		t.Fatalf("expect 1 warn event at 6min, got %v", evs)
	}
	// 12 分钟：触发 breach（warn 已触发，不再重复）。
	evs = m.Check(fixedNow.Add(12 * time.Minute))
	if len(evs) != 1 || evs[0].Kind != "breach" {
		t.Fatalf("expect 1 breach event at 12min, got %v", evs)
	}
	// 13 分钟：breach 已触发，不再重复。
	evs = m.Check(fixedNow.Add(13 * time.Minute))
	if len(evs) != 0 {
		t.Fatalf("expect 0 events at 13min (already breached), got %d", len(evs))
	}
	if len(events) != 2 {
		t.Errorf("callback invoked %d times, want 2", len(events))
	}
}

func TestSLAMonitor_CompleteStopsTracking(t *testing.T) {
	m := NewSLAMonitor(nil)
	fixedNow := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	m.SetNow(func() time.Time { return fixedNow })

	m.Track(&SLAConfig{TaskID: "task-2", Deadline: time.Minute})
	m.Complete("task-2")

	tracked := m.Tracked()
	if len(tracked) != 0 {
		t.Fatalf("after Complete, tracked = %v, want empty", tracked)
	}
}

func TestSLAMonitor_CheckNoConfig(t *testing.T) {
	m := NewSLAMonitor(nil)
	now := time.Now()
	m.Track(&SLAConfig{TaskID: "task-3"}) // Deadline=0, WarnAt=0
	evs := m.Check(now.Add(time.Hour))
	if len(evs) != 0 {
		t.Fatalf("expect 0 events for no-SLA task, got %d", len(evs))
	}
}