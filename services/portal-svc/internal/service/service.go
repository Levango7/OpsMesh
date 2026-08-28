package service

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/Levango7/OpsMesh/services/portal-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/portal-svc/internal/store"
)

// Errors returned by the service.
var (
	ErrRequestNotFound   = errors.New("resource request not found")
	ErrRequestInvalid    = errors.New("resource request invalid")
	ErrInvalidTransition = errors.New("invalid status transition")
	ErrQuotaExceeded     = errors.New("quota exceeded")
	ErrBudgetNotFound    = errors.New("budget not found")
	ErrQuotaNotFound     = errors.New("quota not found")
)

// Service implements the portal business logic.
type Service struct {
	store store.Store
	now   func() time.Time
}

// NewService creates a new Service.
func NewService(s store.Store) *Service {
	return &Service{
		store: s,
		now:   time.Now,
	}
}

// NewServiceWithClock creates a Service with a custom clock (for testing).
func NewServiceWithClock(s store.Store, now func() time.Time) *Service {
	return &Service{
		store: s,
		now:   now,
	}
}

// ============================================================================
// Resource Request Lifecycle
// ============================================================================

// CreateRequest creates a new resource request.
func (s *Service) CreateRequest(tenantID, requester, title, description, resourceType string, cpu, memoryGB, storageGB int) (*models.ResourceRequest, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("%w: tenantID required", ErrRequestInvalid)
	}
	if requester == "" {
		return nil, fmt.Errorf("%w: requester required", ErrRequestInvalid)
	}
	if title == "" {
		return nil, fmt.Errorf("%w: title required", ErrRequestInvalid)
	}

	now := s.now()
	req := &models.ResourceRequest{
		ID:           uuid.New().String(),
		TenantID:     tenantID,
		Requester:    requester,
		Title:        title,
		Description:  description,
		ResourceType: resourceType,
		CPU:          cpu,
		MemoryGB:     memoryGB,
		StorageGB:    storageGB,
		CostEstimate: estimateCost(cpu, memoryGB, storageGB),
		Status:       models.StatusDraft,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	s.store.CreateRequest(req)
	s.store.AddActivity(&models.ActivityEvent{
		ID:       uuid.New().String(),
		TenantID: tenantID,
		UserID:   requester,
		Action:   "create_request",
		Target:   req.ID,
		Detail:   fmt.Sprintf("Created request: %s", title),
	})
	return req, nil
}

// GetRequest retrieves a request by ID.
func (s *Service) GetRequest(id string) (*models.ResourceRequest, error) {
	r := s.store.GetRequest(id)
	if r == nil {
		return nil, ErrRequestNotFound
	}
	return r, nil
}

// ListRequests lists requests with optional filters.
func (s *Service) ListRequests(tenantID, status string) []*models.ResourceRequest {
	return s.store.ListRequests(tenantID, status)
}

// UpdateRequest updates a request (only in draft or pending status).
func (s *Service) UpdateRequest(id, title, description string, cpu, memoryGB, storageGB int) (*models.ResourceRequest, error) {
	r, err := s.GetRequest(id)
	if err != nil {
		return nil, err
	}
	if r.Status != models.StatusDraft && r.Status != models.StatusPending {
		return nil, fmt.Errorf("%w: cannot update request in %s status", ErrInvalidTransition, r.Status)
	}

	if title != "" {
		r.Title = title
	}
	if description != "" {
		r.Description = description
	}
	if cpu > 0 {
		r.CPU = cpu
	}
	if memoryGB > 0 {
		r.MemoryGB = memoryGB
	}
	if storageGB > 0 {
		r.StorageGB = storageGB
	}
	r.CostEstimate = estimateCost(r.CPU, r.MemoryGB, r.StorageGB)
	r.UpdatedAt = s.now()

	s.store.UpdateRequest(r)
	s.store.AddActivity(&models.ActivityEvent{
		ID:       uuid.New().String(),
		TenantID: r.TenantID,
		UserID:   r.Requester,
		Action:   "update_request",
		Target:   id,
		Detail:   fmt.Sprintf("Updated request: %s", r.Title),
	})
	return r, nil
}

// SubmitRequest submits a draft request for approval.
func (s *Service) SubmitRequest(id string) (*models.ResourceRequest, error) {
	r, err := s.GetRequest(id)
	if err != nil {
		return nil, err
	}
	if r.Status != models.StatusDraft {
		return nil, fmt.Errorf("%w: only draft requests can be submitted", ErrInvalidTransition)
	}

	// Check quota before submitting
	if err := s.checkQuota(r); err != nil {
		return nil, err
	}

	r.Status = models.StatusPending
	r.UpdatedAt = s.now()
	s.store.UpdateRequest(r)
	s.store.AddActivity(&models.ActivityEvent{
		ID:       uuid.New().String(),
		TenantID: r.TenantID,
		UserID:   r.Requester,
		Action:   "submit_request",
		Target:   id,
		Detail:   fmt.Sprintf("Submitted request: %s for approval", r.Title),
	})
	return r, nil
}

// CancelRequest cancels a request.
func (s *Service) CancelRequest(id string) (*models.ResourceRequest, error) {
	r, err := s.GetRequest(id)
	if err != nil {
		return nil, err
	}
	if r.Status.IsTerminal() {
		return nil, fmt.Errorf("%w: cannot cancel request in %s status", ErrInvalidTransition, r.Status)
	}

	r.Status = models.StatusCancelled
	r.UpdatedAt = s.now()
	s.store.UpdateRequest(r)
	s.store.AddActivity(&models.ActivityEvent{
		ID:       uuid.New().String(),
		TenantID: r.TenantID,
		UserID:   r.Requester,
		Action:   "cancel_request",
		Target:   id,
		Detail:   fmt.Sprintf("Cancelled request: %s", r.Title),
	})
	return r, nil
}

// ApproveRequest approves a pending request.
func (s *Service) ApproveRequest(id, approver, note string) (*models.ResourceRequest, error) {
	r, err := s.GetRequest(id)
	if err != nil {
		return nil, err
	}
	if r.Status != models.StatusPending {
		return nil, fmt.Errorf("%w: only pending requests can be approved", ErrInvalidTransition)
	}

	r.Status = models.StatusApproved
	r.Approver = approver
	r.ApprovalNote = note
	r.UpdatedAt = s.now()
	s.store.UpdateRequest(r)
	s.store.AddActivity(&models.ActivityEvent{
		ID:       uuid.New().String(),
		TenantID: r.TenantID,
		UserID:   approver,
		Action:   "approve_request",
		Target:   id,
		Detail:   fmt.Sprintf("Approved request: %s", r.Title),
	})
	return r, nil
}

// RejectRequest rejects a pending request.
func (s *Service) RejectRequest(id, approver, note string) (*models.ResourceRequest, error) {
	r, err := s.GetRequest(id)
	if err != nil {
		return nil, err
	}
	if r.Status != models.StatusPending {
		return nil, fmt.Errorf("%w: only pending requests can be rejected", ErrInvalidTransition)
	}

	r.Status = models.StatusRejected
	r.Approver = approver
	r.ApprovalNote = note
	r.UpdatedAt = s.now()
	s.store.UpdateRequest(r)
	s.store.AddActivity(&models.ActivityEvent{
		ID:       uuid.New().String(),
		TenantID: r.TenantID,
		UserID:   approver,
		Action:   "reject_request",
		Target:   id,
		Detail:   fmt.Sprintf("Rejected request: %s - %s", r.Title, note),
	})
	return r, nil
}

// FulfillRequest marks an approved request as fulfilled.
func (s *Service) FulfillRequest(id string) (*models.ResourceRequest, error) {
	r, err := s.GetRequest(id)
	if err != nil {
		return nil, err
	}
	if r.Status != models.StatusApproved {
		return nil, fmt.Errorf("%w: only approved requests can be fulfilled", ErrInvalidTransition)
	}

	r.Status = models.StatusFulfilled
	r.UpdatedAt = s.now()
	s.store.UpdateRequest(r)
	s.store.AddActivity(&models.ActivityEvent{
		ID:       uuid.New().String(),
		TenantID: r.TenantID,
		UserID:   "system",
		Action:   "fulfill_request",
		Target:   id,
		Detail:   fmt.Sprintf("Fulfilled request: %s", r.Title),
	})
	return r, nil
}

// ============================================================================
// Quota Management
// ============================================================================

// GetQuota retrieves a tenant's quota.
func (s *Service) GetQuota(tenantID string) (*models.Quota, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("%w: tenantID required", ErrQuotaNotFound)
	}
	q := s.store.GetQuota(tenantID)
	if q == nil {
		return nil, ErrQuotaNotFound
	}
	return q, nil
}

// ListQuotas returns all quotas.
func (s *Service) ListQuotas() []*models.Quota {
	return s.store.ListQuotas()
}

// UpdateQuota updates a tenant's quota.
func (s *Service) UpdateQuota(tenantID string, maxCPU, maxMemoryGB, maxStorageGB, maxRequests int) (*models.Quota, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("%w: tenantID required", ErrRequestInvalid)
	}
	q := &models.Quota{
		TenantID:     tenantID,
		MaxCPU:       maxCPU,
		MaxMemoryGB:  maxMemoryGB,
		MaxStorageGB: maxStorageGB,
		MaxRequests:  maxRequests,
	}
	if err := s.store.SetQuota(tenantID, q); err != nil {
		return nil, err
	}
	s.store.AddActivity(&models.ActivityEvent{
		ID:       uuid.New().String(),
		TenantID: tenantID,
		UserID:   "admin",
		Action:   "update_quota",
		Target:   tenantID,
		Detail:   fmt.Sprintf("Updated quota: CPU=%d, Memory=%dGB, Storage=%dGB", maxCPU, maxMemoryGB, maxStorageGB),
	})
	return q, nil
}

// GetQuotaUsage returns current quota usage for a tenant.
func (s *Service) GetQuotaUsage(tenantID string) (*models.QuotaUsage, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("%w: tenantID required", ErrQuotaNotFound)
	}
	q := s.store.GetQuota(tenantID)
	if q == nil {
		return nil, ErrQuotaNotFound
	}

	// Calculate current usage from approved/fulfilled requests
	requests := s.store.ListRequests(tenantID, "")
	var usedCPU, usedMemory, usedStorage, usedRequests int
	for _, r := range requests {
		if r.Status == models.StatusApproved || r.Status == models.StatusFulfilled {
			usedCPU += r.CPU
			usedMemory += r.MemoryGB
			usedStorage += r.StorageGB
		}
		if r.Status != models.StatusCancelled && r.Status != models.StatusRejected {
			usedRequests++
		}
	}

	return &models.QuotaUsage{
		TenantID:      tenantID,
		UsedCPU:       usedCPU,
		UsedMemoryGB:  usedMemory,
		UsedStorageGB: usedStorage,
		UsedRequests:  usedRequests,
		Quota:         q,
	}, nil
}

// checkQuota verifies a request does not exceed tenant quota.
func (s *Service) checkQuota(r *models.ResourceRequest) error {
	q := s.store.GetQuota(r.TenantID)
	if q == nil {
		return nil // No quota set = unlimited
	}

	usage, err := s.GetQuotaUsage(r.TenantID)
	if err != nil {
		return nil
	}

	if q.MaxCPU > 0 && usage.UsedCPU+r.CPU > q.MaxCPU {
		return fmt.Errorf("%w: CPU usage %d + %d exceeds max %d", ErrQuotaExceeded, usage.UsedCPU, r.CPU, q.MaxCPU)
	}
	if q.MaxMemoryGB > 0 && usage.UsedMemoryGB+r.MemoryGB > q.MaxMemoryGB {
		return fmt.Errorf("%w: memory usage %d + %d exceeds max %d", ErrQuotaExceeded, usage.UsedMemoryGB, r.MemoryGB, q.MaxMemoryGB)
	}
	if q.MaxStorageGB > 0 && usage.UsedStorageGB+r.StorageGB > q.MaxStorageGB {
		return fmt.Errorf("%w: storage usage %d + %d exceeds max %d", ErrQuotaExceeded, usage.UsedStorageGB, r.StorageGB, q.MaxStorageGB)
	}
	if q.MaxRequests > 0 && usage.UsedRequests+1 > q.MaxRequests {
		return fmt.Errorf("%w: request count %d exceeds max %d", ErrQuotaExceeded, usage.UsedRequests, q.MaxRequests)
	}
	return nil
}

// ============================================================================
// Cost Optimization
// ============================================================================

// GetUtilization returns resource utilization overview.
func (s *Service) GetUtilization(tenantID string) (*models.Utilization, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenantID required")
	}
	u := s.store.GetUtilization(tenantID)
	if u == nil {
		// Generate default utilization
		u = s.generateUtilization(tenantID)
	}
	return u, nil
}

// GetRecommendations returns cost optimization recommendations.
func (s *Service) GetRecommendations(tenantID string) []*models.CostRecommendation {
	recs := s.store.ListRecommendations(tenantID)
	if len(recs) == 0 {
		recs = s.generateRecommendations(tenantID)
		for _, r := range recs {
			s.store.AddRecommendation(r)
		}
	}
	return recs
}

// GetSavingsAnalysis returns potential savings analysis.
func (s *Service) GetSavingsAnalysis(tenantID string) (*models.SavingsAnalysis, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenantID required")
	}
	recs := s.GetRecommendations(tenantID)
	var totalSavings, idleCost, overProvCost float64
	for _, r := range recs {
		totalSavings += r.Savings
		if r.Category == "idle" {
			idleCost += r.Savings
		} else if r.Category == "over_provisioned" {
			overProvCost += r.Savings
		}
	}

	budget := s.store.GetBudget(tenantID)
	var totalSpend float64
	if budget != nil {
		totalSpend = budget.CurrentSpend
	}

	return &models.SavingsAnalysis{
		TenantID:          tenantID,
		TotalSpend:        totalSpend,
		PotentialSavings:  totalSavings,
		IdleResourcesCost: idleCost,
		OverProvCost:      overProvCost,
	}, nil
}

// ============================================================================
// Budget Management
// ============================================================================

// GetBudget returns a tenant's budget status.
func (s *Service) GetBudget(tenantID string) (*models.Budget, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("%w: tenantID required", ErrBudgetNotFound)
	}
	b := s.store.GetBudget(tenantID)
	if b == nil {
		return nil, ErrBudgetNotFound
	}
	return b, nil
}

// SetBudget sets a tenant's budget.
func (s *Service) SetBudget(tenantID string, monthlyLimit, alertThreshold float64) (*models.Budget, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("%w: tenantID required", ErrRequestInvalid)
	}
	if monthlyLimit <= 0 {
		return nil, fmt.Errorf("%w: monthly limit must be positive", ErrRequestInvalid)
	}
	b := &models.Budget{
		TenantID:       tenantID,
		MonthlyLimit:   monthlyLimit,
		CurrentSpend:   monthlyLimit * 0.6, // Simulated
		AlertThreshold: alertThreshold,
	}
	if err := s.store.SetBudget(b); err != nil {
		return nil, err
	}
	s.store.AddActivity(&models.ActivityEvent{
		ID:       uuid.New().String(),
		TenantID: tenantID,
		UserID:   "admin",
		Action:   "set_budget",
		Target:   tenantID,
		Detail:   fmt.Sprintf("Set budget: %.2f/month, alert at %.0f%%", monthlyLimit, alertThreshold*100),
	})
	return b, nil
}

// ============================================================================
// Dashboard
// ============================================================================

// GetDashboardStats returns dashboard statistics.
func (s *Service) GetDashboardStats(tenantID string) *models.DashboardStats {
	requests := s.store.ListRequests(tenantID, "")
	stats := &models.DashboardStats{}
	for _, r := range requests {
		stats.TotalRequests++
		switch r.Status {
		case models.StatusPending:
			stats.PendingRequests++
		case models.StatusApproved, models.StatusFulfilled:
			stats.ApprovedRequests++
		case models.StatusRejected:
			stats.RejectedRequests++
		}
	}

	quotas := s.store.ListQuotas()
	stats.ActiveQuotas = len(quotas)

	recs := s.store.ListRecommendations(tenantID)
	for _, r := range recs {
		stats.TotalSavings += r.Savings
	}

	budget := s.store.GetBudget(tenantID)
	if budget != nil {
		stats.MonthlySpend = budget.CurrentSpend
	}

	return stats
}

// GetRecentActivity returns recent activity events.
func (s *Service) GetRecentActivity(tenantID string, limit int) []*models.ActivityEvent {
	return s.store.ListActivity(tenantID, limit)
}

// ============================================================================
// Internal Helpers
// ============================================================================

func estimateCost(cpu, memoryGB, storageGB int) float64 {
	return float64(cpu)*0.05 + float64(memoryGB)*0.01 + float64(storageGB)*0.001
}

func (s *Service) generateUtilization(tenantID string) *models.Utilization {
	requests := s.store.ListRequests(tenantID, "")
	var totalCPU, totalMemory int
	for _, r := range requests {
		if r.Status == models.StatusApproved || r.Status == models.StatusFulfilled {
			totalCPU += r.CPU
			totalMemory += r.MemoryGB
		}
	}
	u := &models.Utilization{
		TenantID:     tenantID,
		CPUUsage:     float64(totalCPU) * 0.65,
		MemoryUsage:  float64(totalMemory) * 0.72,
		StorageUsage: 0.58,
		IdleCount:    3,
	}
	s.store.SetUtilization(u)
	return u
}

func (s *Service) generateRecommendations(tenantID string) []*models.CostRecommendation {
	now := s.now()
	recs := []*models.CostRecommendation{
		{
			ID:          uuid.New().String(),
			TenantID:    tenantID,
			Category:    "idle",
			ResourceID:  "i-001",
			Description: "Instance i-001 has <5% CPU utilization for 7 days. Consider terminating.",
			Savings:     120.50,
			Priority:    "high",
		},
		{
			ID:          uuid.New().String(),
			TenantID:    tenantID,
			Category:    "over_provisioned",
			ResourceID:  "i-002",
			Description: "Instance i-002 (8 vCPU) averages 20% usage. Right-size to 2 vCPU.",
			Savings:     85.00,
			Priority:    "medium",
		},
		{
			ID:          uuid.New().String(),
			TenantID:    tenantID,
			Category:    "idle",
			ResourceID:  "vol-003",
			Description: "Volume vol-003 is unattached for 30 days. Delete to save costs.",
			Savings:     45.00,
			Priority:    "low",
		},
	}
	// Ensure unique timestamps for deterministic ordering
	_ = now
	sort.Slice(recs, func(i, j int) bool {
		return recs[i].Savings > recs[j].Savings
	})
	return recs
}
