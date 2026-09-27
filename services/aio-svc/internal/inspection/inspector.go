package inspection

import (
	"fmt"
	"time"
)

// Inspector provides automated risk inspection capabilities.
type Inspector struct{}

// NewInspector creates a new Inspector.
func NewInspector() *Inspector {
	return &Inspector{}
}

// DeviceInspection contains inspection results for a single device.
type DeviceInspection struct {
	deviceID  string
	RiskScore int
	Status    string
	Findings  []RiskFinding
}

// RiskFinding represents a single risk finding.
type RiskFinding struct {
	Category    string
	Description string
	Severity    int
}

// InspectionReport contains the full inspection report.
type InspectionReport struct {
	tenantID      string
	Devices       []DeviceInspection
	TotalDevices  int
	HealthyCount  int
	WarningCount  int
	CriticalCount int
	InspectedAt   time.Time
}

// RiskScoreResult contains a device's risk score.
type RiskScoreResult struct {
	tenantID string
	deviceID string
	Score    int
	Level    string
	Factors  []string
}

// Report contains a summary report.
type Report struct {
	tenantID      string
	GeneratedAt   time.Time
	TotalDevices  int
	HealthyCount  int
	WarningCount  int
	CriticalCount int
	TopRisks      []RiskScoreResult
	Summary       string
}

// RunInspection performs risk inspection on a set of devices.
func (in *Inspector) RunInspection(tenantID string, deviceIds []string) InspectionReport {
	report := InspectionReport{
		tenantID:     tenantID,
		Devices:      make([]DeviceInspection, 0, len(deviceIds)),
		TotalDevices: len(deviceIds),
		InspectedAt:  time.Now(),
	}

	for _, deviceID := range deviceIds {
		inspection := in.inspectDevice(deviceID)
		report.Devices = append(report.Devices, inspection)

		switch inspection.Status {
		case "healthy":
			report.HealthyCount++
		case "warning":
			report.WarningCount++
		case "critical":
			report.CriticalCount++
		}
	}

	return report
}

// inspectDevice performs risk assessment on a single device.
func (in *Inspector) inspectDevice(deviceID string) DeviceInspection {
	inspection := DeviceInspection{
		deviceID: deviceID,
		Findings: make([]RiskFinding, 0),
	}

	// Generate deterministic risk based on device ID hash
	score := computeDeviceRisk(deviceID)
	inspection.RiskScore = score
	inspection.Status = riskLevel(score)

	// Generate findings based on risk factors
	inspection.Findings = generateFindings(deviceID, score)

	return inspection
}

// GetRiskScore returns the risk score for a specific device.
func (in *Inspector) GetRiskScore(tenantID, deviceID string) RiskScoreResult {
	score := computeDeviceRisk(deviceID)
	level := riskLevel(score)
	factors := getRiskFactors(deviceID, score)

	return RiskScoreResult{
		tenantID: tenantID,
		deviceID: deviceID,
		Score:    score,
		Level:    level,
		Factors:  factors,
	}
}

// GenerateReport generates a summary report for a tenant.
func (in *Inspector) GenerateReport(tenantID string, deviceIds []string) Report {
	now := time.Now()
	report := Report{
		tenantID:     tenantID,
		GeneratedAt:  now,
		TotalDevices: len(deviceIds),
		TopRisks:     make([]RiskScoreResult, 0),
	}

	// Score all devices
	scores := make([]RiskScoreResult, 0, len(deviceIds))
	for _, deviceID := range deviceIds {
		score := computeDeviceRisk(deviceID)
		level := riskLevel(score)
		factors := getRiskFactors(deviceID, score)
		result := RiskScoreResult{
			tenantID: tenantID,
			deviceID: deviceID,
			Score:    score,
			Level:    level,
			Factors:  factors,
		}
		scores = append(scores, result)

		switch level {
		case "low":
			report.HealthyCount++
		case "medium":
			report.WarningCount++
		case "high", "critical":
			report.CriticalCount++
		}
	}

	// Top risks: devices with score > 60
	for _, s := range scores {
		if s.Score > 60 {
			report.TopRisks = append(report.TopRisks, s)
		}
	}

	// Sort top risks by score descending
	for i := 0; i < len(report.TopRisks); i++ {
		for j := i + 1; j < len(report.TopRisks); j++ {
			if report.TopRisks[j].Score > report.TopRisks[i].Score {
				report.TopRisks[i], report.TopRisks[j] = report.TopRisks[j], report.TopRisks[i]
			}
		}
	}

	// Keep top 10
	if len(report.TopRisks) > 10 {
		report.TopRisks = report.TopRisks[:10]
	}

	report.Summary = fmt.Sprintf(
		"Inspection complete: %d devices, %d healthy, %d warning, %d critical",
		report.TotalDevices, report.HealthyCount, report.WarningCount, report.CriticalCount,
	)

	return report
}

// computeDeviceRisk computes a deterministic risk score (0-100) for a device.
func computeDeviceRisk(deviceID string) int {
	// Simple hash-based scoring for demonstration
	hash := 0
	for _, c := range deviceID {
		hash = (hash*31 + int(c)) % 1000
	}
	// Map to 0-100 range with some devices being riskier
	score := hash % 101
	return score
}

// riskLevel converts a score to a risk level string.
func riskLevel(score int) string {
	switch {
	case score < 30:
		return "healthy"
	case score < 60:
		return "warning"
	case score < 80:
		return "warning"
	default:
		return "critical"
	}
}

// generateFindings generates risk findings for a device.
func generateFindings(deviceID string, score int) []RiskFinding {
	findings := make([]RiskFinding, 0)

	if score >= 80 {
		findings = append(findings, RiskFinding{
			Category:    "availability",
			Description: "Device has high risk score indicating potential availability issues",
			Severity:    3,
		})
	}
	if score >= 60 {
		findings = append(findings, RiskFinding{
			Category:    "performance",
			Description: "Performance degradation risk detected",
			Severity:    2,
		})
	}
	if score >= 40 {
		findings = append(findings, RiskFinding{
			Category:    "capacity",
			Description: "Capacity planning recommended",
			Severity:    1,
		})
	}

	return findings
}

// getRiskFactors returns the risk factors for a device.
func getRiskFactors(deviceID string, score int) []string {
	factors := make([]string, 0)
	if score >= 80 {
		factors = append(factors, "high_risk_score")
		factors = append(factors, "availability_concern")
	}
	if score >= 60 {
		factors = append(factors, "performance_degradation")
	}
	if score >= 40 {
		factors = append(factors, "capacity_warning")
	}
	if score < 40 {
		factors = append(factors, "healthy")
	}
	return factors
}
