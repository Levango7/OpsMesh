package compliance

import (
	"testing"
)

func TestNewEngine(t *testing.T) {
	e := NewEngine()
	if e == nil {
		t.Fatal("NewEngine returned nil")
	}
	rules := e.ListRules()
	if len(rules) == 0 {
		t.Error("NewEngine should have default CIS rules")
	}
}

func TestGetRule(t *testing.T) {
	e := NewEngine()
	rule, ok := e.GetRule("cis-ssh-01")
	if !ok {
		t.Fatal("GetRule(cis-ssh-01) = false, want true")
	}
	if rule.ID != "cis-ssh-01" {
		t.Errorf("ID = %q, want cis-ssh-01", rule.ID)
	}
	if rule.CheckScript == "" {
		t.Error("CheckScript should not be empty")
	}
	_, ok = e.GetRule("nonexistent")
	if ok {
		t.Error("GetRule(nonexistent) = true, want false")
	}
}

func TestScan_Score(t *testing.T) {
	e := NewEngine()
	results := []ComplianceResult{
		{RuleID: "cis-ssh-01", Passed: true},
		{RuleID: "cis-file-01", Passed: true},
		{RuleID: "cis-password-01", Passed: false},
	}
	report := e.Scan("default", "dev-001", results)
	if report.Simulated {
		t.Error("Simulated = true, want false")
	}
	if report.Score != 66 {
		t.Errorf("Score = %d, want 66", report.Score)
	}
}

func TestScan_EmptyResults(t *testing.T) {
	e := NewEngine()
	report := e.Scan("default", "dev-001", nil)
	if report.Score != 0 {
		t.Errorf("Score = %d, want 0", report.Score)
	}
}

func TestScan_AllPassed(t *testing.T) {
	e := NewEngine()
	results := []ComplianceResult{
		{RuleID: "r1", Passed: true},
		{RuleID: "r2", Passed: true},
	}
	report := e.Scan("default", "dev-001", results)
	if report.Score != 100 {
		t.Errorf("Score = %d, want 100", report.Score)
	}
}

func TestCreateComplianceTasks(t *testing.T) {
	e := NewEngine()
	rules := []ComplianceRule{
		{ID: "r1", CheckScript: "echo test"},
		{ID: "r2", CheckScript: ""},
		{ID: "r3", CheckScript: "ls -la"},
	}
	tasks := e.CreateComplianceTasks("default", "dev-001", rules)
	if len(tasks) != 2 {
		t.Fatalf("CreateComplianceTasks = %d tasks, want 2", len(tasks))
	}
	for _, id := range tasks {
		if id == "" {
			t.Error("task ID should not be empty")
		}
	}
}

func TestCreateComplianceTasks_EmptyRules(t *testing.T) {
	e := NewEngine()
	tasks := e.CreateComplianceTasks("default", "dev-001", nil)
	if len(tasks) != 0 {
		t.Fatalf("CreateComplianceTasks = %d tasks, want 0", len(tasks))
	}
}
