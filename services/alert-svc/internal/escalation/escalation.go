package escalation

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// EscalationLevel represents a single escalation step within a policy.
type EscalationLevel struct {
	Level    int           `json:"level"`
	Timeout  time.Duration `json:"timeout"`
	Channels []string      `json:"channels"`
}

// EscalationPolicy defines how an alert escalates through notification levels.
type EscalationPolicy struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	TenantID  string            `json:"tenant_id"`
	Levels    []EscalationLevel `json:"levels"`
	Enabled   bool              `json:"enabled"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// RotationType defines how on-call rotations repeat.
type RotationType string

const (
	RotationDaily  RotationType = "daily"
	RotationWeekly RotationType = "weekly"
)

// OnCallEntry represents a single user's on-call window.
type OnCallEntry struct {
	UserID    string    `json:"user_id"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
}

// OnCallSchedule defines an on-call rotation for a team.
type OnCallSchedule struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	TenantID  string        `json:"tenant_id"`
	Entries   []OnCallEntry `json:"entries"`
	Rotation  RotationType  `json:"rotation"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

// EscalationState tracks the current escalation status of an alert.
type EscalationState struct {
	ID             string            `json:"id"`
	AlertID        string            `json:"alert_id"`
	PolicyID       string            `json:"policy_id"`
	CurrentLevel   int               `json:"current_level"`
	Status         string            `json:"status"`
	StartedAt      time.Time         `json:"started_at"`
	AcknowledgedAt *time.Time        `json:"acknowledged_at,omitempty"`
	ResolvedAt     *time.Time        `json:"resolved_at,omitempty"`
	NotifiedAt     map[int]time.Time `json:"notified_at"`
	ChannelsUsed   map[int][]string  `json:"channels_used"`
}

// NotificationFunc is called when escalation needs to notify a channel.
type NotificationFunc func(alertID string, level int, channels []string) error

// Escalator manages escalation policies, on-call schedules, and active escalations.
type Escalator struct {
	mu          sync.RWMutex
	policies    map[string]*EscalationPolicy
	schedules   map[string]*OnCallSchedule
	escalations map[string]*EscalationState
	notifyFn    NotificationFunc
	ticker      *time.Ticker
	stopCh      chan struct{}
	wg          sync.WaitGroup
	interval    time.Duration
	now         func() time.Time
}

// NewEscalator creates a new Escalator with default settings.
func NewEscalator() *Escalator {
	return &Escalator{
		policies:    make(map[string]*EscalationPolicy),
		schedules:   make(map[string]*OnCallSchedule),
		escalations: make(map[string]*EscalationState),
		stopCh:      make(chan struct{}),
		interval:    10 * time.Second,
		now:         time.Now,
	}
}

// SetNotificationFunc sets the callback function for sending notifications.
func (e *Escalator) SetNotificationFunc(fn NotificationFunc) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.notifyFn = fn
}

// SetInterval sets the background check interval (for testing).
func (e *Escalator) SetInterval(d time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if d > 0 {
		e.interval = d
	}
}

// SetNow sets the time function (for testing).
func (e *Escalator) SetNow(fn func() time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.now = fn
}

// AddPolicy adds or updates an escalation policy.
func (e *Escalator) AddPolicy(policy *EscalationPolicy) error {
	if policy == nil {
		return fmt.Errorf("escalation: policy is nil")
	}
	if policy.ID == "" {
		return fmt.Errorf("escalation: policy ID is required")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()
	if policy.CreatedAt.IsZero() {
		policy.CreatedAt = now
	}
	policy.UpdatedAt = now
	cp := *policy
	e.policies[policy.ID] = &cp
	return nil
}

// GetPolicy retrieves an escalation policy by ID.
func (e *Escalator) GetPolicy(id string) *EscalationPolicy {
	e.mu.RLock()
	defer e.mu.RUnlock()
	p, ok := e.policies[id]
	if !ok {
		return nil
	}
	cp := *p
	return &cp
}

// ListPolicies returns all escalation policies, optionally filtered by tenant.
func (e *Escalator) ListPolicies(tenantID string) []*EscalationPolicy {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]*EscalationPolicy, 0, len(e.policies))
	for _, p := range e.policies {
		if tenantID != "" && p.TenantID != tenantID {
			continue
		}
		cp := *p
		out = append(out, &cp)
	}
	return out
}

// DeletePolicy removes an escalation policy.
func (e *Escalator) DeletePolicy(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.policies[id]; !ok {
		return false
	}
	delete(e.policies, id)
	return true
}

// AddSchedule adds or updates an on-call schedule.
func (e *Escalator) AddSchedule(schedule *OnCallSchedule) error {
	if schedule == nil {
		return fmt.Errorf("escalation: schedule is nil")
	}
	if schedule.ID == "" {
		return fmt.Errorf("escalation: schedule ID is required")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()
	if schedule.CreatedAt.IsZero() {
		schedule.CreatedAt = now
	}
	schedule.UpdatedAt = now
	cp := *schedule
	e.schedules[schedule.ID] = &cp
	return nil
}

// GetSchedule retrieves an on-call schedule by ID.
func (e *Escalator) GetSchedule(id string) *OnCallSchedule {
	e.mu.RLock()
	defer e.mu.RUnlock()
	s, ok := e.schedules[id]
	if !ok {
		return nil
	}
	cp := *s
	return &cp
}

// ListSchedules returns all on-call schedules, optionally filtered by tenant.
func (e *Escalator) ListSchedules(tenantID string) []*OnCallSchedule {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]*OnCallSchedule, 0, len(e.schedules))
	for _, s := range e.schedules {
		if tenantID != "" && s.TenantID != tenantID {
			continue
		}
		cp := *s
		out = append(out, &cp)
	}
	return out
}

// DeleteSchedule removes an on-call schedule.
func (e *Escalator) DeleteSchedule(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.schedules[id]; !ok {
		return false
	}
	delete(e.schedules, id)
	return true
}

// StartEscalation begins escalation tracking for an alert.
func (e *Escalator) StartEscalation(alertID, policyID string) (*EscalationState, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	policy, ok := e.policies[policyID]
	if !ok {
		return nil, fmt.Errorf("escalation: policy %s not found", policyID)
	}
	if !policy.Enabled {
		return nil, fmt.Errorf("escalation: policy %s is disabled", policyID)
	}

	state := &EscalationState{
		ID:           escID(alertID),
		AlertID:      alertID,
		PolicyID:     policyID,
		CurrentLevel: -1,
		Status:       "active",
		StartedAt:    e.now(),
		NotifiedAt:   make(map[int]time.Time),
		ChannelsUsed: make(map[int][]string),
	}
	e.escalations[state.ID] = state
	return state, nil
}

// Acknowledge marks an escalation as acknowledged, stopping further escalation.
func (e *Escalator) Acknowledge(alertID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	state, ok := e.escalations[escID(alertID)]
	if !ok {
		return fmt.Errorf("escalation: alert %s not found", alertID)
	}
	if state.Status != "active" {
		return fmt.Errorf("escalation: alert %s is not active (status: %s)", alertID, state.Status)
	}

	now := e.now()
	state.Status = "acknowledged"
	state.AcknowledgedAt = &now
	return nil
}

// Escalate manually moves an escalation to the next level.
func (e *Escalator) Escalate(alertID string) (*EscalationState, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	state, ok := e.escalations[escID(alertID)]
	if !ok {
		return nil, fmt.Errorf("escalation: alert %s not found", alertID)
	}
	if state.Status != "active" {
		return nil, fmt.Errorf("escalation: alert %s is not active", alertID)
	}

	policy, ok := e.policies[state.PolicyID]
	if !ok {
		return nil, fmt.Errorf("escalation: policy %s not found", state.PolicyID)
	}

	nextLevel := state.CurrentLevel + 1
	if nextLevel >= len(policy.Levels) {
		return state, nil
	}

	state.CurrentLevel = nextLevel
	level := policy.Levels[nextLevel]
	state.NotifiedAt[level.Level] = e.now()
	state.ChannelsUsed[level.Level] = level.Channels

	if e.notifyFn != nil {
		_ = e.notifyFn(alertID, level.Level, level.Channels)
	}

	return state, nil
}

// Resolve marks an escalation as resolved.
func (e *Escalator) Resolve(alertID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	state, ok := e.escalations[escID(alertID)]
	if !ok {
		return fmt.Errorf("escalation: alert %s not found", alertID)
	}

	now := e.now()
	state.Status = "resolved"
	state.ResolvedAt = &now
	return nil
}

// GetActiveEscalation returns the escalation state for a given alert.
func (e *Escalator) GetActiveEscalation(alertID string) *EscalationState {
	e.mu.RLock()
	defer e.mu.RUnlock()
	s, ok := e.escalations[escID(alertID)]
	if !ok {
		return nil
	}
	cp := *s
	return &cp
}

// ListActiveEscalations returns all active escalations.
func (e *Escalator) ListActiveEscalations() []*EscalationState {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]*EscalationState, 0)
	for _, s := range e.escalations {
		if s.Status == "active" {
			cp := *s
			out = append(out, &cp)
		}
	}
	return out
}

// GetOnCall returns the current on-call user for a schedule.
func (e *Escalator) GetOnCall(scheduleID string, at time.Time) *OnCallEntry {
	e.mu.RLock()
	schedule, ok := e.schedules[scheduleID]
	e.mu.RUnlock()
	if !ok {
		return nil
	}
	return findOnCall(schedule, at)
}

// GetOnCallForTenant returns the first matching on-call entry for a tenant.
func (e *Escalator) GetOnCallForTenant(tenantID string, at time.Time) *OnCallEntry {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, s := range e.schedules {
		if s.TenantID == tenantID {
			entry := findOnCall(s, at)
			if entry != nil {
				return entry
			}
		}
	}
	return nil
}

// Start begins the background escalation goroutine.
func (e *Escalator) Start(ctx context.Context) {
	e.mu.Lock()
	e.ticker = time.NewTicker(e.interval)
	e.mu.Unlock()

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-e.stopCh:
				return
			case <-e.ticker.C:
				e.checkEscalations()
			}
		}
	}()
}

// Stop gracefully shuts down the background goroutine.
func (e *Escalator) Stop() {
	close(e.stopCh)
	e.wg.Wait()
}

// checkEscalations evaluates all active escalations and promotes those past timeout.
func (e *Escalator) checkEscalations() {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := e.now()
	for _, state := range e.escalations {
		if state.Status != "active" {
			continue
		}

		policy, ok := e.policies[state.PolicyID]
		if !ok {
			continue
		}

		currentLevel := state.CurrentLevel

		// If never notified (level -1), check first level timeout from start.
		if currentLevel < 0 {
			if len(policy.Levels) > 0 {
				firstTimeout := policy.Levels[0].Timeout
				if now.Sub(state.StartedAt) >= firstTimeout {
					state.CurrentLevel = 0
					level := policy.Levels[0]
					state.NotifiedAt[0] = now
					state.ChannelsUsed[0] = level.Channels
					if e.notifyFn != nil {
						_ = e.notifyFn(state.AlertID, 0, level.Channels)
					}
				}
			}
			continue
		}

		// Check if current level has a next level and timeout elapsed.
		nextLevel := currentLevel + 1
		if nextLevel >= len(policy.Levels) {
			continue
		}

		lastNotified, hasNotified := state.NotifiedAt[currentLevel]
		if !hasNotified {
			lastNotified = state.StartedAt
		}

		nextTimeout := policy.Levels[nextLevel].Timeout
		if now.Sub(lastNotified) >= nextTimeout {
			state.CurrentLevel = nextLevel
			level := policy.Levels[nextLevel]
			state.NotifiedAt[nextLevel] = now
			state.ChannelsUsed[nextLevel] = level.Channels
			if e.notifyFn != nil {
				_ = e.notifyFn(state.AlertID, nextLevel, level.Channels)
			}
		}
	}
}

// findOnCall finds the on-call entry active at the given time, considering rotation.
func findOnCall(schedule *OnCallSchedule, at time.Time) *OnCallEntry {
	if len(schedule.Entries) == 0 {
		return nil
	}

	for _, entry := range schedule.Entries {
		if isEntryActive(&entry, at, schedule.Rotation) {
			cp := entry
			return &cp
		}
	}
	return nil
}

// isEntryActive checks if an on-call entry is active at a given time considering rotation.
func isEntryActive(entry *OnCallEntry, at time.Time, rotation RotationType) bool {
	start := entry.StartTime
	end := entry.EndTime

	switch rotation {
	case RotationDaily:
		// Compare only time-of-day components.
		atTime := at.Hour()*3600 + at.Minute()*60 + at.Second()
		startTime := start.Hour()*3600 + start.Minute()*60 + start.Second()
		endTime := end.Hour()*3600 + end.Minute()*60 + end.Second()
		if startTime <= endTime {
			return atTime >= startTime && atTime <= endTime
		}
		return atTime >= startTime || atTime <= endTime
	case RotationWeekly:
		// Compare day-of-week and time.
		atWeekday := int(at.Weekday())
		startWeekday := int(start.Weekday())
		endWeekday := int(end.Weekday())
		atSec := at.Hour()*3600 + at.Minute()*60 + at.Second()
		startSec := start.Hour()*3600 + start.Minute()*60 + start.Second()
		endSec := end.Hour()*3600 + end.Minute()*60 + end.Second()

		if startWeekday == endWeekday {
			return atWeekday == startWeekday && atSec >= startSec && atSec <= endSec
		}
		if startWeekday < endWeekday {
			if atWeekday > startWeekday && atWeekday < endWeekday {
				return true
			}
			if atWeekday == startWeekday && atSec >= startSec {
				return true
			}
			if atWeekday == endWeekday && atSec <= endSec {
				return true
			}
			return false
		}
		// Wraps around the week (e.g., Friday to Monday).
		if atWeekday > startWeekday || atWeekday < endWeekday {
			return true
		}
		if atWeekday == startWeekday && atSec >= startSec {
			return true
		}
		if atWeekday == endWeekday && atSec <= endSec {
			return true
		}
		return false
	default:
		// No rotation: direct time comparison.
		return !at.Before(start) && !at.After(end)
	}
}

// escID generates a stable escalation ID from an alert ID.
func escID(alertID string) string {
	return "esc-" + alertID
}

// MarshalJSON for EscalationState ensures maps are serialized correctly.
func (s *EscalationState) MarshalJSON() ([]byte, error) {
	type Alias EscalationState
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(s),
	})
}
