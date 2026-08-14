package approval

import (
	"strings"
	"testing"
	"time"
)

func validStep(id string, order int, mode StepMode, approvers ...string) ApprovalStep {
	return ApprovalStep{ID: id, Name: id, Order: order, Mode: mode, Approvers: approvers}
}

func TestFlowValidateSuccess(t *testing.T) {
	f := &ApprovalFlow{
		ID:          "f1",
		Name:        "flow",
		TenantID:    "t1",
		TriggerType: TriggerShell,
		Steps: []ApprovalStep{
			validStep("s1", 1, StepSequential, "alice", "bob"),
			validStep("s2", 2, StepCountersign, "carol", "dave"),
			validStep("s3", 3, StepAnyOf, "eve"),
		},
		Enabled: true,
	}
	if err := f.Validate(); err != nil {
		t.Fatalf("valid flow: %v", err)
	}
}

func TestFlowValidateErrors(t *testing.T) {
	cases := []struct {
		name       string
		modify     func(*ApprovalFlow)
		wantSubstr string
	}{
		{"missing ID", func(f *ApprovalFlow) { f.ID = "" }, "ID is required"},
		{"missing Name", func(f *ApprovalFlow) { f.Name = "" }, "Name is required"},
		{"missing TenantID", func(f *ApprovalFlow) { f.TenantID = "" }, "TenantID is required"},
		{"missing TriggerType", func(f *ApprovalFlow) { f.TriggerType = "" }, "TriggerType is required"},
		{"no steps", func(f *ApprovalFlow) { f.Steps = nil }, "at least one step"},
		{"empty step ID", func(f *ApprovalFlow) {
			f.Steps = []ApprovalStep{validStep("", 1, StepAnyOf, "alice")}
		}, "step ID is required"},
		{"duplicate step ID", func(f *ApprovalFlow) {
			f.Steps = []ApprovalStep{validStep("s1", 1, StepAnyOf, "alice"), validStep("s1", 2, StepAnyOf, "bob")}
		}, "duplicate step ID"},
		{"zero Order", func(f *ApprovalFlow) {
			f.Steps = []ApprovalStep{validStep("s1", 0, StepAnyOf, "alice")}
		}, "Order must be >= 1"},
		{"duplicate Order", func(f *ApprovalFlow) {
			f.Steps = []ApprovalStep{validStep("s1", 1, StepAnyOf, "alice"), validStep("s2", 1, StepAnyOf, "bob")}
		}, "duplicate step Order"},
		{"invalid Mode", func(f *ApprovalFlow) {
			f.Steps = []ApprovalStep{validStep("s1", 1, "bogus", "alice")}
		}, "invalid step Mode"},
		{"empty Approvers", func(f *ApprovalFlow) {
			f.Steps = []ApprovalStep{validStep("s1", 1, StepAnyOf)}
		}, "Approvers must not be empty"},
		{"unordered Steps", func(f *ApprovalFlow) {
			f.Steps = []ApprovalStep{validStep("s2", 2, StepAnyOf, "alice"), validStep("s1", 1, StepAnyOf, "bob")}
		}, "sorted by Order ascending"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &ApprovalFlow{
				ID: "f1", Name: "n", TenantID: "t1", TriggerType: TriggerShell,
				Steps: []ApprovalStep{validStep("s1", 1, StepAnyOf, "alice")},
			}
			c.modify(f)
			err := f.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", c.wantSubstr)
			}
			if !strings.Contains(err.Error(), c.wantSubstr) {
				t.Errorf("error = %q, want substring %q", err.Error(), c.wantSubstr)
			}
		})
	}
}

func TestFlowStepLookups(t *testing.T) {
	f := &ApprovalFlow{
		Steps: []ApprovalStep{
			validStep("s1", 1, StepAnyOf, "alice"),
			validStep("s2", 2, StepAnyOf, "bob"),
		},
	}
	if got := f.StepByOrder(1); got == nil || got.ID != "s1" {
		t.Errorf("StepByOrder(1) = %+v want s1", got)
	}
	if got := f.StepByOrder(99); got != nil {
		t.Errorf("StepByOrder(99) = %+v want nil", got)
	}
	if got := f.StepByID("s2"); got == nil || got.Order != 2 {
		t.Errorf("StepByID(s2) = %+v want order 2", got)
	}
	if got := f.StepByID("nope"); got != nil {
		t.Errorf("StepByID(nope) = %+v want nil", got)
	}
	if got := f.LastOrder(); got != 2 {
		t.Errorf("LastOrder = %d want 2", got)
	}
	empty := &ApprovalFlow{}
	if got := empty.LastOrder(); got != 0 {
		t.Errorf("empty LastOrder = %d want 0", got)
	}
}

func TestRequestValidate(t *testing.T) {
	good := &ApprovalRequest{
		ID: "r1", FlowID: "f1", TenantID: "t1", TriggerType: TriggerShell,
		Operator: "ops", Status: StatusPending,
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("valid request: %v", err)
	}

	bad := *good
	bad.ID = ""
	if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "ID is required") {
		t.Errorf("missing ID: err=%v", err)
	}

	bad = *good
	bad.Status = "bogus"
	if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "invalid request Status") {
		t.Errorf("invalid status: err=%v", err)
	}
}

func TestRequestStepHelpers(t *testing.T) {
	rs := &RequestStep{
		Decisions: []Decision{{UserID: "alice"}, {UserID: "bob"}},
	}
	if !rs.HasDecided("alice") {
		t.Error("alice has decided")
	}
	if rs.HasDecided("carol") {
		t.Error("carol has not decided")
	}

	r := &ApprovalRequest{
		Steps: []RequestStep{{StepID: "s1", Order: 1}, {StepID: "s2", Order: 2}},
	}
	if got := r.StepByOrder(2); got == nil || got.StepID != "s2" {
		t.Errorf("StepByOrder(2)=%+v", got)
	}
	if got := r.StepByID("s1"); got == nil || got.Order != 1 {
		t.Errorf("StepByID(s1)=%+v", got)
	}

	// IsExpired
	now := time.Now()
	r.ExpireAt = now.Add(time.Hour)
	if r.IsExpired(now) {
		t.Error("should not be expired")
	}
	r.ExpireAt = now.Add(-time.Hour)
	if !r.IsExpired(now) {
		t.Error("should be expired")
	}
	r.ExpireAt = time.Time{}
	if r.IsExpired(now) {
		t.Error("zero ExpireAt should not expire")
	}
}
