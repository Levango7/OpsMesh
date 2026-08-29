package models

import (
	"time"
)

// RequestStatus represents the lifecycle status of a resource request.
type RequestStatus string

const (
	StatusDraft     RequestStatus = "draft"
	StatusPending   RequestStatus = "pending"
	StatusApproved  RequestStatus = "approved"
	StatusRejected  RequestStatus = "rejected"
	StatusFulfilled RequestStatus = "fulfilled"
	StatusCancelled RequestStatus = "cancelled"
)

// IsTerminal returns true if the status is a final state.
func (s RequestStatus) IsTerminal() bool {
	switch s {
	case StatusApproved, StatusRejected, StatusFulfilled, StatusCancelled:
		return true
	}
	return false
}

// ResourceRequest represents a resource request in the self-service portal.
type ResourceRequest struct {
	ID           string        `json:"id"`
	TenantID     string        `json:"tenant_id"`
	Requester    string        `json:"requester"`
	Title        string        `json:"title"`
	Description  string        `json:"description"`
	ResourceType string        `json:"resource_type"`
	CPU          int           `json:"cpu"`
	MemoryGB     int           `json:"memory_gb"`
	StorageGB    int           `json:"storage_gd"`
	CostEstimate float64       `json:"cost_estimate"`
	Status       RequestStatus `json:"status"`
	Approver     string        `json:"approver,omitempty"`
	ApprovalNote string        `json:"approval_note,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

// Quota represents a tenant's resource quota.
type Quota struct {
	TenantID     string `json:"tenant_id"`
	MaxCPU       int    `json:"max_cpu"`
	MaxMemoryGB  int    `json:"max_memory_gb"`
	MaxStorageGB int    `json:"max_storage_gb"`
	MaxRequests  int    `json:"max_requests"`
}

// QuotaUsage represents current quota usage for a tenant.
type QuotaUsage struct {
	TenantID      string `json:"tenant_id"`
	UsedCPU       int    `json:"used_cpu"`
	UsedMemoryGB  int    `json:"used_memory_gb"`
	UsedStorageGB int    `json:"used_storage_gd"`
	UsedRequests  int    `json:"used_requests"`
	Quota         *Quota `json:"quota"`
}

// Budget represents a tenant's budget configuration.
type Budget struct {
	TenantID       string  `json:"tenant_id"`
	MonthlyLimit   float64 `json:"monthly_limit"`
	CurrentSpend   float64 `json:"current_spend"`
	AlertThreshold float64 `json:"alert_threshold"`
}

// CostRecommendation represents a cost optimization recommendation.
type CostRecommendation struct {
	ID          string  `json:"id"`
	TenantID    string  `json:"tenant_id"`
	Category    string  `json:"category"`
	ResourceID  string  `json:"resource_id"`
	Description string  `json:"description"`
	Savings     float64 `json:"savings"`
	Priority    string  `json:"priority"`
}

// Utilization represents resource utilization metrics.
type Utilization struct {
	TenantID     string  `json:"tenant_id"`
	CPUUsage     float64 `json:"cpu_usage"`
	MemoryUsage  float64 `json:"memory_usage"`
	StorageUsage float64 `json:"storage_usage"`
	IdleCount    int     `json:"idle_count"`
}

// SavingsAnalysis represents potential savings analysis.
type SavingsAnalysis struct {
	TenantID          string  `json:"tenant_id"`
	TotalSpend        float64 `json:"total_spend"`
	PotentialSavings  float64 `json:"potential_savings"`
	IdleResourcesCost float64 `json:"idle_resources_cost"`
	OverProvCost      float64 `json:"over_provisioning_cost"`
}

// ActivityEvent represents an audit log entry.
type ActivityEvent struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	UserID    string    `json:"user_id"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	Detail    string    `json:"detail"`
	Timestamp time.Time `json:"timestamp"`
}

// DashboardStats represents dashboard statistics.
type DashboardStats struct {
	TotalRequests    int     `json:"total_requests"`
	PendingRequests  int     `json:"pending_requests"`
	ApprovedRequests int     `json:"approved_requests"`
	RejectedRequests int     `json:"rejected_requests"`
	TotalSavings     float64 `json:"total_savings"`
	MonthlySpend     float64 `json:"monthly_spend"`
	ActiveQuotas     int     `json:"active_quotas"`
}
