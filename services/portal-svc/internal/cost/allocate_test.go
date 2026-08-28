package cost

import (
	"testing"
)

func TestNewAllocator(t *testing.T) {
	a := NewAllocator()
	if a == nil {
		t.Fatal("NewAllocator returned nil")
	}
	if rules := a.GetAllocationRules(); len(rules) != 0 {
		t.Errorf("expected 0 rules, got %d", len(rules))
	}
}

func TestSetAndGetAllocationRules(t *testing.T) {
	a := NewAllocator()
	rules := []AllocationRule{
		{Dimension: DimensionTenant, Weight: 1.0, Tags: []string{"tenant-a", "tenant-b"}},
		{Dimension: DimensionDepartment, Weight: 0.8, Tags: []string{"eng", "ops"}},
	}
	a.SetAllocationRules(rules)

	got := a.GetAllocationRules()
	if len(got) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(got))
	}
	if got[0].Dimension != DimensionDepartment {
		t.Errorf("expected first rule dimension %q, got %q", DimensionDepartment, got[0].Dimension)
	}
	if got[1].Dimension != DimensionTenant {
		t.Errorf("expected second rule dimension %q, got %q", DimensionTenant, got[1].Dimension)
	}
}

func TestAllocateCosts_BasicAllocation(t *testing.T) {
	a := NewAllocator()
	a.SetAllocationRules([]AllocationRule{
		{Dimension: DimensionTenant, Weight: 1.0, Tags: []string{"t1", "t2"}},
	})

	entries := []CostEntry{
		{Target: "t1", Amount: 600.0},
		{Target: "t2", Amount: 400.0},
	}

	report, err := a.AllocateCosts(DimensionTenant, 1000.0, entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.TotalCost != 1000.0 {
		t.Errorf("expected total cost 1000, got %.2f", report.TotalCost)
	}
	if len(report.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(report.Results))
	}
	if report.Results[0].Target != "t1" || report.Results[0].AllocatedAmount != 600.0 {
		t.Errorf("expected t1=600, got %s=%.2f", report.Results[0].Target, report.Results[0].AllocatedAmount)
	}
	if report.Results[1].Target != "t2" || report.Results[1].AllocatedAmount != 400.0 {
		t.Errorf("expected t2=400, got %s=%.2f", report.Results[1].Target, report.Results[1].AllocatedAmount)
	}
}

func TestAllocateCosts_WeightedAllocation(t *testing.T) {
	a := NewAllocator()
	a.SetAllocationRules([]AllocationRule{
		{Dimension: DimensionProject, Weight: 0.5},
	})

	entries := []CostEntry{
		{Target: "p1", Amount: 200.0},
	}

	report, err := a.AllocateCosts(DimensionProject, 200.0, entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Results[0].AllocatedAmount != 100.0 {
		t.Errorf("expected allocated 100 (200*0.5), got %.2f", report.Results[0].AllocatedAmount)
	}
}

func TestAllocateCosts_FilteredByTags(t *testing.T) {
	a := NewAllocator()
	a.SetAllocationRules([]AllocationRule{
		{Dimension: DimensionDevice, Weight: 1.0, Tags: []string{"d1", "d3"}},
	})

	entries := []CostEntry{
		{Target: "d1", Amount: 100.0},
		{Target: "d2", Amount: 200.0},
		{Target: "d3", Amount: 300.0},
	}

	report, err := a.AllocateCosts(DimensionDevice, 600.0, entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Results) != 2 {
		t.Fatalf("expected 2 results (filtered), got %d", len(report.Results))
	}
	if report.Results[0].Target != "d3" {
		t.Errorf("expected first result d3, got %s", report.Results[0].Target)
	}
}

func TestAllocateCosts_NoRuleError(t *testing.T) {
	a := NewAllocator()
	entries := []CostEntry{{Target: "x", Amount: 100.0}}
	_, err := a.AllocateCosts(DimensionTenant, 100.0, entries)
	if err == nil {
		t.Fatal("expected error for missing rule, got nil")
	}
}

func TestAllocateCosts_ZeroCostError(t *testing.T) {
	a := NewAllocator()
	a.SetAllocationRules([]AllocationRule{
		{Dimension: DimensionTenant, Weight: 1.0},
	})
	entries := []CostEntry{{Target: "x", Amount: 0.0}}
	_, err := a.AllocateCosts(DimensionTenant, 0.0, entries)
	if err == nil {
		t.Fatal("expected error for zero cost, got nil")
	}
}

func TestAllocateCosts_EmptyEntriesError(t *testing.T) {
	a := NewAllocator()
	a.SetAllocationRules([]AllocationRule{
		{Dimension: DimensionTenant, Weight: 1.0},
	})
	_, err := a.AllocateCosts(DimensionTenant, 100.0, []CostEntry{})
	if err == nil {
		t.Fatal("expected error for empty entries, got nil")
	}
}

func TestGetAllocationReport(t *testing.T) {
	a := NewAllocator()
	a.SetAllocationRules([]AllocationRule{
		{Dimension: DimensionDepartment, Weight: 1.0},
	})

	entries := []CostEntry{
		{Target: "eng", Amount: 500.0},
		{Target: "ops", Amount: 300.0},
	}

	report, err := a.GetAllocationReport(DimensionDepartment, 800.0, entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(report.Results))
	}
	if report.Results[0].Percentage != 62.5 {
		t.Errorf("expected eng percentage 62.5, got %.2f", report.Results[0].Percentage)
	}
}

func TestSetAllocationRules_DedupeTags(t *testing.T) {
	a := NewAllocator()
	a.SetAllocationRules([]AllocationRule{
		{Dimension: DimensionTenant, Weight: 1.0, Tags: []string{"a", "a", "b"}},
	})

	rules := a.GetAllocationRules()
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if len(rules[0].Tags) != 2 {
		t.Errorf("expected 2 unique tags, got %d", len(rules[0].Tags))
	}
}
