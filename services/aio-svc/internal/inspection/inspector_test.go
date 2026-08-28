package inspection

import (
	"strings"
	"testing"
)

func TestRunInspection_EmptyDevices(t *testing.T) {
	in := NewInspector()
	report := in.RunInspection("tenant-1", []string{})
	if report.TotalDevices != 0 {
		t.Errorf("expected 0 devices, got %d", report.TotalDevices)
	}
}

func TestRunInspection_MultipleDevices(t *testing.T) {
	in := NewInspector()
	deviceIds := []string{"srv-01", "srv-02", "srv-03", "srv-04", "srv-05"}
	report := in.RunInspection("tenant-1", deviceIds)
	if report.TotalDevices != 5 {
		t.Errorf("expected 5 devices, got %d", report.TotalDevices)
	}
	if len(report.Devices) != 5 {
		t.Errorf("expected 5 device inspections, got %d", len(report.Devices))
	}
	total := report.HealthyCount + report.WarningCount + report.CriticalCount
	if total != 5 {
		t.Errorf("expected total count 5, got %d", total)
	}
}

func TestRunInspection_DeviceStatus(t *testing.T) {
	in := NewInspector()
	report := in.RunInspection("tenant-1", []string{"critical-device-high-risk"})
	for _, d := range report.Devices {
		if d.Status != "healthy" && d.Status != "warning" && d.Status != "critical" {
			t.Errorf("invalid status: %s", d.Status)
		}
	}
}

func TestGetRiskScore(t *testing.T) {
	in := NewInspector()
	result := in.GetRiskScore("tenant-1", "srv-01")
	if result.TenantId != "tenant-1" {
		t.Errorf("expected tenant-1, got %s", result.TenantId)
	}
	if result.DeviceId != "srv-01" {
		t.Errorf("expected srv-01, got %s", result.DeviceId)
	}
	if result.Score < 0 || result.Score > 100 {
		t.Errorf("expected score 0-100, got %d", result.Score)
	}
	if result.Level == "" {
		t.Error("expected non-empty level")
	}
}

func TestRiskLevel(t *testing.T) {
	tests := []struct {
		score int
		want  string
	}{
		{0, "healthy"},
		{29, "healthy"},
		{30, "warning"},
		{59, "warning"},
		{60, "warning"},
		{79, "warning"},
		{80, "critical"},
		{100, "critical"},
	}
	for _, tt := range tests {
		got := riskLevel(tt.score)
		if got != tt.want {
			t.Errorf("riskLevel(%d) = %s, want %s", tt.score, got, tt.want)
		}
	}
}

func TestGenerateReport(t *testing.T) {
	in := NewInspector()
	deviceIds := []string{"srv-01", "srv-02", "srv-03", "db-01", "db-02"}
	report := in.GenerateReport("tenant-1", deviceIds)
	if report.TenantId != "tenant-1" {
		t.Errorf("expected tenant-1, got %s", report.TenantId)
	}
	if report.TotalDevices != 5 {
		t.Errorf("expected 5 devices, got %d", report.TotalDevices)
	}
	if !strings.Contains(report.Summary, "5 devices") {
		t.Errorf("expected summary to contain '5 devices', got %s", report.Summary)
	}
}

func TestGenerateReport_TopRisks(t *testing.T) {
	in := NewInspector()
	// Create many devices to ensure some have high risk
	deviceIds := make([]string, 20)
	for i := range deviceIds {
		deviceIds[i] = "device-" + string(rune('A'+i)) + "-production-critical-overload"
	}
	report := in.GenerateReport("tenant-1", deviceIds)
	// Top risks should be sorted by score descending
	for i := 1; i < len(report.TopRisks); i++ {
		if report.TopRisks[i].Score > report.TopRisks[i-1].Score {
			t.Error("top risks should be sorted by score descending")
		}
	}
}

func TestComputeDeviceRisk(t *testing.T) {
	score := computeDeviceRisk("test-device")
	if score < 0 || score > 100 {
		t.Errorf("expected score 0-100, got %d", score)
	}
	// Same device should produce same score
	score2 := computeDeviceRisk("test-device")
	if score != score2 {
		t.Errorf("expected deterministic score, got %d vs %d", score, score2)
	}
}

func TestGenerateFindings(t *testing.T) {
	// High risk device
	findings := generateFindings("critical-device", 85)
	if len(findings) == 0 {
		t.Error("expected findings for high risk device")
	}
	hasAvailability := false
	for _, f := range findings {
		if f.Category == "availability" {
			hasAvailability = true
		}
	}
	if !hasAvailability {
		t.Error("expected availability finding for score >= 80")
	}

	// Low risk device
	findingsLow := generateFindings("healthy-device", 20)
	if len(findingsLow) != 0 {
		t.Errorf("expected no findings for low risk, got %d", len(findingsLow))
	}
}

func TestGetRiskFactors(t *testing.T) {
	factors := getRiskFactors("test", 85)
	if len(factors) == 0 {
		t.Error("expected risk factors for high score")
	}
	found := false
	for _, f := range factors {
		if f == "high_risk_score" {
			found = true
		}
	}
	if !found {
		t.Error("expected high_risk_score factor")
	}

	factorsLow := getRiskFactors("test", 20)
	if len(factorsLow) == 0 {
		t.Error("expected at least one factor for low score")
	}
}
