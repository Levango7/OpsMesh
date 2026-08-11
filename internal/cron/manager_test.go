package cron

import (
	"testing"
	"time"
)

func TestManager_CreateAndGet(t *testing.T) {
	m := NewManager()
	fixedNow := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	m.SetNow(func() time.Time { return fixedNow })

	e, err := m.Create(&ScheduleEntry{
		TaskID:   "task-1",
		TenantID: "default",
		Name:     "每5分钟健康检查",
		CronExpr: "*/5 * * * *",
		CreatedBy: "alice",
	})
	if err != nil {
		t.Fatal(err)
	}
	if e.ID == "" {
		t.Fatal("entry ID should be assigned")
	}
	if e.Status != EntryActive {
		t.Errorf("Status = %s, want active", e.Status)
	}
	if e.NextRunAt.IsZero() {
		t.Error("NextRunAt should be computed")
	}

	got, err := m.Get(e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.TaskID != "task-1" {
		t.Errorf("Get TaskID = %s", got.TaskID)
	}
}

func TestManager_InvalidCronExpr(t *testing.T) {
	m := NewManager()
	_, err := m.Create(&ScheduleEntry{
		TaskID:   "task-1",
		CronExpr: "invalid",
	})
	if err == nil {
		t.Fatal("expect error for invalid cron expr")
	}
}

func TestManager_PauseResume(t *testing.T) {
	m := NewManager()
	m.SetNow(func() time.Time { return time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC) })

	e, _ := m.Create(&ScheduleEntry{TaskID: "t1", CronExpr: "*/5 * * * *"})

	paused, err := m.Pause(e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if paused.Status != EntryPaused {
		t.Errorf("Status = %s, want paused", paused.Status)
	}
	if !paused.NextRunAt.IsZero() {
		t.Error("paused NextRunAt should be zero")
	}

	resumed, err := m.Resume(e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Status != EntryActive {
		t.Errorf("Status = %s, want active", resumed.Status)
	}
	if resumed.NextRunAt.IsZero() {
		t.Error("resumed NextRunAt should be computed")
	}
}

func TestManager_Delete(t *testing.T) {
	m := NewManager()
	e, _ := m.Create(&ScheduleEntry{TaskID: "t1", CronExpr: "*/5 * * * *"})
	if err := m.Delete(e.ID); err != nil {
		t.Fatal(err)
	}
	got, err := m.Get(e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != EntryDeleted {
		t.Errorf("Status = %s, want deleted", got.Status)
	}
}

func TestManager_ListByTenant(t *testing.T) {
	m := NewManager()
	_, _ = m.Create(&ScheduleEntry{TaskID: "t1", TenantID: "tenant-a", CronExpr: "*/5 * * * *"})
	_, _ = m.Create(&ScheduleEntry{TaskID: "t2", TenantID: "tenant-b", CronExpr: "0 * * * *"})
	_, _ = m.Create(&ScheduleEntry{TaskID: "t3", TenantID: "tenant-a", CronExpr: "0 2 * * *"})

	listA := m.List("tenant-a", "")
	if len(listA) != 2 {
		t.Fatalf("tenant-a list = %d, want 2", len(listA))
	}
	listAll := m.List("", "")
	if len(listAll) != 3 {
		t.Fatalf("all list = %d, want 3", len(listAll))
	}
	listActive := m.List("", EntryActive)
	if len(listActive) != 3 {
		t.Fatalf("active list = %d, want 3", len(listActive))
	}
}

func TestManager_Update(t *testing.T) {
	m := NewManager()
	e, _ := m.Create(&ScheduleEntry{TaskID: "t1", CronExpr: "*/5 * * * *", Name: "old"})

	updated, err := m.Update(e.ID, &ScheduleEntry{Name: "new", CronExpr: "0 * * * *"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "new" {
		t.Errorf("Name = %s, want new", updated.Name)
	}
	if updated.CronExpr != "0 * * * *" {
		t.Errorf("CronExpr = %s", updated.CronExpr)
	}
}

func TestManager_NotFound(t *testing.T) {
	m := NewManager()
	if _, err := m.Get("nonexistent"); err != ErrEntryNotFound {
		t.Errorf("Get nonexistent = %v, want ErrEntryNotFound", err)
	}
	if _, err := m.Pause("nonexistent"); err != ErrEntryNotFound {
		t.Errorf("Pause nonexistent = %v, want ErrEntryNotFound", err)
	}
	if err := m.Delete("nonexistent"); err != ErrEntryNotFound {
		t.Errorf("Delete nonexistent = %v, want ErrEntryNotFound", err)
	}
}