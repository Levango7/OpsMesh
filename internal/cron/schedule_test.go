package cron

import (
	"sync"
	"testing"
	"time"

	"opsmesh/internal/proto"
)

// fakeStore 是 TaskStore 的内存实现，供调度器单测。
type fakeStore struct {
	mu      sync.Mutex
	all     []*proto.Task
	created []*proto.Task
	updated []*proto.Task
}

func (f *fakeStore) AllTasks(_ string) []*proto.Task {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*proto.Task, len(f.all))
	copy(out, f.all)
	return out
}

func (f *fakeStore) CreateTask(t *proto.Task) *proto.Task {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, t)
	f.all = append(f.all, t)
	return t
}

func (f *fakeStore) UpdateTask(t *proto.Task) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updated = append(f.updated, t)
}

func TestSchedulerDerivesInstanceOnMatch(t *testing.T) {
	store := &fakeStore{
		all: []*proto.Task{
			{TaskID: "tpl-1", Type: proto.TaskTypeShell, Command: "echo hi", Schedule: "* * * * *", LastFiredAt: time.Time{}},
		},
	}
	s := NewScheduler(store)
	s.interval = time.Minute
	s.tick()
	if len(store.created) != 1 {
		t.Fatalf("expect 1 derived task, got %d", len(store.created))
	}
	d := store.created[0]
	if d.ParentID != "tpl-1" {
		t.Errorf("derived ParentID wrong: %s", d.ParentID)
	}
	if d.Command != "echo hi" {
		t.Errorf("derived Command not cloned: %s", d.Command)
	}
	if len(store.updated) != 1 {
		t.Errorf("template LastFiredAt not updated")
	}
}

func TestSchedulerSkipsNonScheduled(t *testing.T) {
	store := &fakeStore{
		all: []*proto.Task{
			{TaskID: "tpl-2", Type: proto.TaskTypeShell, Command: "x", Schedule: ""},
		},
	}
	s := NewScheduler(store)
	s.interval = time.Minute
	s.tick()
	if len(store.created) != 0 {
		t.Fatalf("non-scheduled task should not derive, got %d", len(store.created))
	}
}

func TestSchedulerNoReentryWithinInterval(t *testing.T) {
	now := time.Now()
	store := &fakeStore{
		all: []*proto.Task{
			{TaskID: "tpl-3", Type: proto.TaskTypeShell, Command: "y", Schedule: "* * * * *", LastFiredAt: now.Add(-10 * time.Second)},
		},
	}
	s := NewScheduler(store)
	s.interval = time.Minute
	s.tick() // 距上次触发仅 10s < 60s interval，应跳过
	if len(store.created) != 0 {
		t.Fatalf("re-entry guard broke, derived %d", len(store.created))
	}
}

func TestSchedulerDerivesAfterInterval(t *testing.T) {
	now := time.Now()
	store := &fakeStore{
		all: []*proto.Task{
			{TaskID: "tpl-4", Type: proto.TaskTypeShell, Command: "z", Schedule: "* * * * *", LastFiredAt: now.Add(-2 * time.Minute)},
		},
	}
	s := NewScheduler(store)
	s.interval = time.Minute
	s.tick() // 距上次 2min > 60s，应派生
	if len(store.created) != 1 {
		t.Fatalf("expect derive after interval, got %d", len(store.created))
	}
}
