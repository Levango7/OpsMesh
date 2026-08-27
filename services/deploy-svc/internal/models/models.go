package models

import "time"

// Deployment status constants.
const (
	DeploymentStatusPending   = "pending"
	DeploymentStatusRunning   = "running"
	DeploymentStatusSuccess   = "success"
	DeploymentStatusFailed    = "failed"
	DeploymentStatusRollback  = "rollback"
	DeploymentStatusCancelled = "cancelled"
)

// Deployment type constants.
const (
	DeploymentTypeScript = "script"
	DeploymentTypeFile   = "file"
	DeploymentTypeK8s    = "k8s"
)

// Deployment strategy constants.
const (
	StrategyRolling   = "rolling"
	StrategyCanary    = "canary"
	StrategyBlueGreen = "bluegreen"
)

// Canary status constants.
const (
	CanaryStatusPending   = "pending"
	CanaryStatusRunning   = "running"
	CanaryStatusAnalyzing = "analyzing"
	CanaryStatusPromoted  = "promoted"
	CanaryStatusRollback  = "rollback"
)

// Deployment represents a deployment task.
type Deployment struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	Name         string    `json:"name"`
	Type         string    `json:"type"`
	RepoURL      string    `json:"repo_url"`
	Content      string    `json:"content"`
	Path         string    `json:"path"`
	TargetIDs    []string  `json:"target_ids"`
	Status       string    `json:"status"`
	Strategy     string    `json:"strategy"`
	CanaryWeight int       `json:"canary_weight"`
	AutoRollback bool      `json:"auto_rollback"`
	CreatedBy    string    `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	ErrorMessage string    `json:"error_message"`
}

// Template represents a deployment template.
type Template struct {
	ID          string            `json:"id"`
	TenantID    string            `json:"tenant_id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Type        string            `json:"type"`
	RepoURL     string            `json:"repo_url"`
	Content     string            `json:"content"`
	Path        string            `json:"path"`
	Parameters  map[string]string `json:"parameters"`
	CreatedBy   string            `json:"created_by"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// Strategy represents a deployment strategy.
type Strategy struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	Type           string    `json:"type"`
	CanaryWeight   int       `json:"canary_weight"`
	MaxUnavailable int       `json:"max_unavailable"`
	MaxSurge       int       `json:"max_surge"`
	AutoRollback   bool      `json:"auto_rollback"`
	TimeoutSeconds int       `json:"timeout_seconds"`
	CreatedBy      string    `json:"created_by"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Canary represents a canary deployment.
type Canary struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	DeploymentID string    `json:"deployment_id"`
	Name         string    `json:"name"`
	Weight       int       `json:"weight"`
	Status       string    `json:"status"`
	SuccessCount int       `json:"success_count"`
	FailureCount int       `json:"failure_count"`
	CreatedBy    string    `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// IsValidDeploymentType checks if the deployment type is valid.
func IsValidDeploymentType(t string) bool {
	switch t {
	case DeploymentTypeScript, DeploymentTypeFile, DeploymentTypeK8s:
		return true
	}
	return false
}

// IsValidStrategy checks if the strategy type is valid.
func IsValidStrategy(s string) bool {
	switch s {
	case StrategyRolling, StrategyCanary, StrategyBlueGreen:
		return true
	}
	return false
}

// IsTerminalStatus checks if the deployment status is terminal.
func IsTerminalStatus(status string) bool {
	switch status {
	case DeploymentStatusSuccess, DeploymentStatusFailed, DeploymentStatusRollback, DeploymentStatusCancelled:
		return true
	}
	return false
}
