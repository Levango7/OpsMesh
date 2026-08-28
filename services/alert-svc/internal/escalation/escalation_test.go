package escalation

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestEscalatorAddAndGetPolicy(t *testing.T) {
	e := NewEscalator()
	policy := &EscalationPolicy{
		ID:       "policy-1",
		Name:     "Critical Escalation",
		TenantID: "tenant-1",
		Levels: []EscalationLevel{
			{Level: 0, Timeout: 5 * time.Minute, Channels: []string{"email"}},
			{Level: 1, Timeout: 10 * time.Minute, Channels: []string{"sms", "pagerduty"}},
		},
		Enabled: true,
	}

	if err := e.AddPolicy(policy); err != nil {
		t.Fatalf("AddPolicy failed: %v", err)
	}

	got := e.GetPolicy("policy-1")
	if got == nil {
		t.Fatal("expected policy to be found")
	}
	if got.Name != "Critical Escalation" {
		t.Errorf("expected name 'Critical Escalation', got %q", got.Name)
	}
	if len(got.Levels) != 2 {
		t.Errorf("expected 2 levels, got %d", len(got.Levels))
	}
}

func TestEscalatorListPolicies(t *testing.T) {
	e := NewEscalator()
	_ = e.AddPolicy(&EscalationPolicy{ID: "p1", TenantID: "t1", Enabled: true})
	_ = e.AddPolicy(&EscalationPolicy{ID: "p2", TenantID: "t1", Enabled: true})
	_ = e.AddPolicy(&EscalationPolicy{ID: "p3", TenantID: "t2", Enabled: true})

	all := e.ListPolicies("")
	if len(all) != 3 {
		t.Errorf("expected 3 policies, got %d", len(all))
	}

	filtered := e.ListPolicies("t1")
	if len(filtered) != 2 {
		t.Errorf("expected 2 policies for tenant t1, got %d", len(filtered))
	}
}

func TestEscalatorAddPolicyNil(t *testing.T) {
	e := NewEscalator()
	err := e.AddPolicy(nil)
	if err == nil {
		t.Error("expected error for nil policy")
	}
}

func TestEscalatorAddPolicyNoID(t *testing.T) {
	e := NewEscalator()
	err := e.AddPolicy(&EscalationPolicy{Name: "No ID"})
	if err == nil {
		t.Error("expected error for policy without ID")
	}
}

func TestEscalationFlow(t *testing.T) {
	e := NewEscalator()

	var notifications []int
	e.SetNotificationFunc(func(alertID string, level int, channels []string) error {
		notifications = append(notifications, level)
		return nil
	})

	policy := &EscalationPolicy{
		ID:       "policy-1",
		Name:     "Test Policy",
		TenantID: "tenant-1",
		Levels: []EscalationLevel{
			{Level: 0, Timeout: 0, Channels: []string{"email"}},
			{Level: 1, Timeout: 0, Channels: []string{"sms"}},
			{Level: 2, Timeout: 0, Channels: []string{"pagerduty"}},
		},
		Enabled: true,
	}
	_ = e.AddPolicy(policy)

	state, err := e.StartEscalation("alert-1", "policy-1")
	if err != nil {
		t.Fatalf("StartEscalation failed: %v", err)
	}
	if state.Status != "active" {
		t.Errorf("expected status 'active', got %q", state.Status)
	}

	// Escalate to level 0
	state, err = e.Escalate("alert-1")
	if err != nil {
		t.Fatalf("Escalate to level 0 failed: %v", err)
	}
	if state.CurrentLevel != 0 {
		t.Errorf("expected level 0, got %d", state.CurrentLevel)
	}

	// Escalate to level 1
	state, err = e.Escalate("alert-1")
	if err != nil {
		t.Fatalf("Escalate to level 1 failed: %v", err)
	}
	if state.CurrentLevel != 1 {
		t.Errorf("expected level 1, got %d", state.CurrentLevel)
	}

	// Escalate to level 2 (max)
	state, err = e.Escalate("alert-1")
	if err != nil {
		t.Fatalf("Escalate to level 2 failed: %v", err)
	}
	if state.CurrentLevel != 2 {
		t.Errorf("expected level 2, got %d", state.CurrentLevel)
	}

	// Escalate beyond max - should stay at level 2
	state, err = e.Escalate("alert-1")
	if err != nil {
		t.Fatalf("Escalate beyond max failed: %v", err)
	}
	if state.CurrentLevel != 2 {
		t.Errorf("expected level 2 (max), got %d", state.CurrentLevel)
	}

	if len(notifications) != 3 {
		t.Errorf("expected 3 notifications, got %d", len(notifications))
	}
}

func TestEscalationAckStopsEscalation(t *testing.T) {
	e := NewEscalator()

	block := int32(0)
	e.SetNotificationFunc(func(alertID string, level int, channels []string) error {
		if atomic.LoadInt32(&block) == 1 {
			t.Errorf("notification should not fire after acknowledgment (level %d)", level)
		}
		return nil
	})

	policy := &EscalationPolicy{
		ID:       "policy-1",
		Name:     "Test Policy",
		TenantID: "tenant-1",
		Levels: []EscalationLevel{
			{Level: 0, Timeout: 0, Channels: []string{"email"}},
			{Level: 1, Timeout: 0, Channels: []string{"sms"}},
		},
		Enabled: true,
	}
	_ = e.AddPolicy(policy)

	_, err := e.StartEscalation("alert-1", "policy-1")
	if err != nil {
		t.Fatalf("StartEscalation failed: %v", err)
	}

	// Escalate to level 0
	_, _ = e.Escalate("alert-1")

	// Acknowledge
	err = e.Acknowledge("alert-1")
	if err != nil {
		t.Fatalf("Acknowledge failed: %v", err)
	}

	atomic.StoreInt32(&block, 1)

	// Try to escalate after acknowledgment - should fail
	_, err = e.Escalate("alert-1")
	if err == nil {
		t.Error("expected error escalating acknowledged alert")
	}

	state := e.GetActiveEscalation("alert-1")
	if state.Status != "acknowledged" {
		t.Errorf("expected status 'acknowledged', got %q", state.Status)
	}
	if state.AcknowledgedAt == nil {
		t.Error("expected AcknowledgedAt to be set")
	}
}

func TestEscalationResolve(t *testing.T) {
	e := NewEscalator()
	policy := &EscalationPolicy{
		ID:       "policy-1",
		Levels:   []EscalationLevel{{Level: 0, Timeout: 0, Channels: []string{"email"}}},
		Enabled:  true,
	}
	_ = e.AddPolicy(policy)

	_, err := e.StartEscalation("alert-1", "policy-1")
	if err != nil {
		t.Fatalf("StartEscalation failed: %v", err)
	}

	err = e.Resolve("alert-1")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	state := e.GetActiveEscalation("alert-1")
	if state.Status != "resolved" {
		t.Errorf("expected status 'resolved', got %q", state.Status)
	}
	if state.ResolvedAt == nil {
		t.Error("expected ResolvedAt to be set")
	}
}

func TestBackgroundEscalation(t *testing.T) {
	e := NewEscalator()
	e.SetInterval(50 * time.Millisecond)

	var notifications []int
	e.SetNotificationFunc(func(alertID string, level int, channels []string) error {
		notifications = append(notifications, level)
		return nil
	})

	frozen := time.Now()
	counter := int64(0)
	e.SetNow(func() time.Time {
		return frozen.Add(time.Duration(atomic.AddInt64(&counter, 1)) * 10 * time.Millisecond)
	})

	policy := &EscalationPolicy{
		ID:       "policy-1",
		Name:     "BG Test",
		TenantID: "t1",
		Levels: []EscalationLevel{
			{Level: 0, Timeout: 25 * time.Millisecond, Channels: []string{"email"}},
			{Level: 1, Timeout: 25 * time.Millisecond, Channels: []string{"sms"}},
		},
		Enabled: true,
	}
	_ = e.AddPolicy(policy)

	_, err := e.StartEscalation("alert-bg", "policy-1")
	if err != nil {
		t.Fatalf("StartEscalation failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	e.Start(ctx)

	time.Sleep(250 * time.Millisecond)

	cancel()
	e.Stop()

	if len(notifications) < 1 {
		t.Errorf("expected at least 1 background notification, got %d", len(notifications))
	}
}

func TestOnCallRotationDaily(t *testing.T) {
	e := NewEscalator()
	schedule := &OnCallSchedule{
		ID:       "sched-1",
		Name:     "Daily On-Call",
		TenantID: "tenant-1",
		Entries: []OnCallEntry{
			{UserID: "alice", StartTime: time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC), EndTime: time.Date(2024, 1, 1, 17, 0, 0, 0, time.UTC)},
			{UserID: "bob", StartTime: time.Date(2024, 1, 1, 17, 0, 0, 0, time.UTC), EndTime: time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC)},
		},
		Rotation: RotationDaily,
	}
	_ = e.AddSchedule(schedule)

	// 12:00 - should be alice
	at := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	entry := e.GetOnCall("sched-1", at)
	if entry == nil {
		t.Fatal("expected on-call entry at 12:00")
	}
	if entry.UserID != "alice" {
		t.Errorf("expected alice at 12:00, got %s", entry.UserID)
	}

	// 20:00 - should be bob
	at = time.Date(2024, 6, 15, 20, 0, 0, 0, time.UTC)
	entry = e.GetOnCall("sched-1", at)
	if entry == nil {
		t.Fatal("expected on-call entry at 20:00")
	}
	if entry.UserID != "bob" {
		t.Errorf("expected bob at 20:00, got %s", entry.UserID)
	}
}

func TestOnCallRotationWeekly(t *testing.T) {
	e := NewEscalator()
	schedule := &OnCallSchedule{
		ID:       "sched-weekly",
		Name:     "Weekly On-Call",
		TenantID: "tenant-1",
		Entries: []OnCallEntry{
			{UserID: "carol", StartTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), EndTime: time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)},
			{UserID: "dave", StartTime: time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC), EndTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
		},
		Rotation: RotationWeekly,
	}
	_ = e.AddSchedule(schedule)

	// Wednesday (Jan 3, 2024) - should be carol
	at := time.Date(2024, 1, 3, 12, 0, 0, 0, time.UTC)
	entry := e.GetOnCall("sched-weekly", at)
	if entry == nil {
		t.Fatal("expected on-call entry on Wednesday")
	}
	if entry.UserID != "carol" {
		t.Errorf("expected carol on Wednesday, got %s", entry.UserID)
	}

	// Saturday (Jan 6, 2024) - should be dave
	at = time.Date(2024, 1, 6, 12, 0, 0, 0, time.UTC)
	entry = e.GetOnCall("sched-weekly", at)
	if entry == nil {
		t.Fatal("expected on-call entry on Saturday")
	}
	if entry.UserID != "dave" {
		t.Errorf("expected dave on Saturday, got %s", entry.UserID)
	}
}

func TestEscalatorDeletePolicy(t *testing.T) {
	e := NewEscalator()
	_ = e.AddPolicy(&EscalationPolicy{ID: "p1", Enabled: true})

	if !e.DeletePolicy("p1") {
		t.Error("expected DeletePolicy to return true")
	}
	if e.GetPolicy("p1") != nil {
		t.Error("expected policy to be deleted")
	}
	if e.DeletePolicy("p1") {
		t.Error("expected DeletePolicy to return false for non-existent policy")
	}
}

func TestEscalatorDeleteSchedule(t *testing.T) {
	e := NewEscalator()
	_ = e.AddSchedule(&OnCallSchedule{ID: "s1", Rotation: RotationDaily})

	if !e.DeleteSchedule("s1") {
		t.Error("expected DeleteSchedule to return true")
	}
	if e.GetSchedule("s1") != nil {
		t.Error("expected schedule to be deleted")
	}
	if e.DeleteSchedule("s1") {
		t.Error("expected DeleteSchedule to return false for non-existent schedule")
	}
}

func TestListActiveEscalations(t *testing.T) {
	e := NewEscalator()
	policy := &EscalationPolicy{
		ID:      "policy-1",
		Levels:  []EscalationLevel{{Level: 0, Timeout: 0, Channels: []string{"email"}}},
		Enabled: true,
	}
	_ = e.AddPolicy(policy)

	_, _ = e.StartEscalation("alert-1", "policy-1")
	_, _ = e.StartEscalation("alert-2", "policy-1")
	_ = e.Resolve("alert-1")

	active := e.ListActiveEscalations()
	if len(active) != 1 {
		t.Errorf("expected 1 active escalation, got %d", len(active))
	}
	if active[0].AlertID != "alert-2" {
		t.Errorf("expected active escalation for alert-2, got %s", active[0].AlertID)
	}
}

func TestEscalatorStartDisabledPolicy(t *testing.T) {
	e := NewEscalator()
	policy := &EscalationPolicy{
		ID:      "policy-1",
		Levels:  []EscalationLevel{{Level: 0, Timeout: 0, Channels: []string{"email"}}},
		Enabled: false,
	}
	_ = e.AddPolicy(policy)

	_, err := e.StartEscalation("alert-1", "policy-1")
	if err == nil {
		t.Error("expected error starting escalation with disabled policy")
	}
}

func TestEscalatorAcknowledgeNotFound(t *testing.T) {
	e := NewEscalator()
	err := e.Acknowledge("nonexistent")
	if err == nil {
		t.Error("expected error acknowledging non-existent escalation")
	}
}

func TestEscalatorResolveNotFound(t *testing.T) {
	e := NewEscalator()
	err := e.Resolve("nonexistent")
	if err == nil {
		t.Error("expected error resolving non-existent escalation")
	}
}
