package service

import (
	"fmt"
	"time"

	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/metrics"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/node"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/ollama"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/quota"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/scheduler"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/workload"
)

// Service implements the GPU service business logic.
type Service struct {
	nodes     *node.Manager
	scheduler *scheduler.Scheduler
	workloads *workload.Manager
	ollama    *ollama.Client
	quotas    *quota.Manager
	metrics   *metrics.Collector
}

// NewService creates a new Service.
func NewService(nodeMgr *node.Manager, sched *scheduler.Scheduler, wlMgr *workload.Manager, oc *ollama.Client, qMgr *quota.Manager, collector *metrics.Collector) *Service {
	return &Service{
		nodes:     nodeMgr,
		scheduler: sched,
		workloads: wlMgr,
		ollama:    oc,
		quotas:    qMgr,
		metrics:   collector,
	}
}

// RegisterNode registers a new GPU node.
func (s *Service) RegisterNode(n *models.GPUNode) (*models.GPUNode, error) {
	if err := s.nodes.Register(n); err != nil {
		return nil, fmt.Errorf("failed to register node: %w", err)
	}
	return n, nil
}

// GetNode retrieves a node by ID.
func (s *Service) GetNode(id string) (*models.GPUNode, error) {
	return s.nodes.Get(id)
}

// ListNodes returns all nodes.
func (s *Service) ListNodes() []*models.GPUNode {
	return s.nodes.List()
}

// UpdateNode updates a node.
func (s *Service) UpdateNode(n *models.GPUNode) (*models.GPUNode, error) {
	if err := s.nodes.Update(n); err != nil {
		return nil, fmt.Errorf("failed to update node: %w", err)
	}
	return n, nil
}

// UnregisterNode removes a node.
func (s *Service) UnregisterNode(id string) error {
	return s.nodes.Unregister(id)
}

// GetNodeHealth returns node health.
func (s *Service) GetNodeHealth(id string) (*models.NodeHealth, error) {
	return s.nodes.GetHealth(id)
}

// GetResourceSummary returns total GPU resource summary.
func (s *Service) GetResourceSummary() *models.ResourceSummary {
	nodes := s.nodes.List()
	summary := &models.ResourceSummary{
		NodeResources: make([]models.NodeResource, 0, len(nodes)),
	}

	for _, n := range nodes {
		gpuCount := len(n.GPUs)
		available := gpuCount - int(n.UsedVRAMMB)/(81920+1)
		if available < 0 {
			available = 0
		}
		allocated := gpuCount - available

		summary.TotalGPUs += gpuCount
		summary.AvailableGPUs += available
		summary.AllocatedGPUs += allocated
		summary.TotalVRAMMB += n.TotalVRAMMB
		summary.AvailableVRAMMB += n.TotalVRAMMB - n.UsedVRAMMB
		summary.AllocatedVRAMMB += n.UsedVRAMMB

		if n.Status == models.NodeStatusOnline {
			summary.OnlineNodes++
		} else {
			summary.OfflineNodes++
		}

		summary.NodeResources = append(summary.NodeResources, models.NodeResource{
			NodeID:          n.ID,
			Status:          n.Status,
			TotalGPUs:       gpuCount,
			AvailableGPUs:   available,
			AllocatedGPUs:   allocated,
			TotalVRAMMB:     n.TotalVRAMMB,
			AvailableVRAMMB: n.TotalVRAMMB - n.UsedVRAMMB,
			AllocatedVRAMMB: n.UsedVRAMMB,
		})
	}
	return summary
}

// GetNodeResources returns per-node resource breakdown.
func (s *Service) GetNodeResources() []models.NodeResource {
	summary := s.GetResourceSummary()
	return summary.NodeResources
}

// SubmitWorkload submits a new AI workload.
func (s *Service) SubmitWorkload(wl *models.Workload) (*models.Workload, error) {
	// Check quota
	if err := s.quotas.CheckAllocation(wl.TenantID, wl.GPURequest.Count, wl.GPURequest.MinVRAMMB, 1); err != nil {
		return nil, fmt.Errorf("quota exceeded: %w", err)
	}

	if err := s.workloads.Submit(wl); err != nil {
		return nil, fmt.Errorf("failed to submit workload: %w", err)
	}

	return wl, nil
}

// GetWorkload retrieves a workload by ID.
func (s *Service) GetWorkload(id string) (*models.Workload, error) {
	return s.workloads.Get(id)
}

// ListWorkloads returns workloads, optionally filtered by status.
func (s *Service) ListWorkloads(status string) []*models.Workload {
	var ws models.WorkloadStatus
	if status != "" {
		ws = models.WorkloadStatus(status)
	}
	return s.workloads.List(ws)
}

// UpdateWorkload updates a workload.
func (s *Service) UpdateWorkload(wl *models.Workload) (*models.Workload, error) {
	if err := s.workloads.Update(wl); err != nil {
		return nil, fmt.Errorf("failed to update workload: %w", err)
	}
	return wl, nil
}

// CancelWorkload cancels a workload.
func (s *Service) CancelWorkload(id string) error {
	wl, err := s.workloads.Get(id)
	if err != nil {
		return err
	}

	if err := s.workloads.Cancel(id); err != nil {
		return err
	}

	// Release quota
	_ = s.quotas.ReleaseAllocation(wl.TenantID, wl.GPURequest.Count, wl.GPURequest.MinVRAMMB, 1)
	return nil
}

// ScaleWorkload scales a workload's replicas.
func (s *Service) ScaleWorkload(id string, replicas int) error {
	return s.workloads.Scale(id, replicas)
}

// TriggerScheduling attempts to schedule all pending workloads.
func (s *Service) TriggerScheduling() []*models.ScheduleResult {
	results := make([]*models.ScheduleResult, 0)
	pending := s.workloads.GetPendingWorkloads()

	for _, wl := range pending {
		result, _ := s.scheduler.Schedule(wl, s.nodes)
		if result.Assigned {
			_ = s.workloads.AssignNode(wl.ID, result.NodeIDs)
			_ = s.quotas.RecordAllocation(wl.TenantID, wl.GPURequest.Count, wl.GPURequest.MinVRAMMB, 1)
		}
		results = append(results, result)
	}
	return results
}

// GetSchedulingPolicies returns scheduling policies.
func (s *Service) GetSchedulingPolicies() []models.SchedulingPolicy {
	return s.scheduler.GetPolicies()
}

// SetSchedulingPolicies sets scheduling policies.
func (s *Service) SetSchedulingPolicies(policies []models.SchedulingPolicy) error {
	return s.scheduler.SetPolicies(policies)
}

// GetScheduleQueue returns the pending queue.
func (s *Service) GetScheduleQueue() []*models.Workload {
	return s.scheduler.GetQueue()
}

// PullModel pulls/syncs an Ollama model.
func (s *Service) PullModel(name string) (*models.GPUModel, error) {
	if err := s.ollama.PullModel(name); err != nil {
		return nil, err
	}
	return &models.GPUModel{
		Name:         name,
		SizeBytes:    ollama.EstimateModelSize(name),
		ParameterCount: ollama.EstimateParams(name),
		Quantized:    true,
		LastPulled:   time.Now(),
	}, nil
}

// ListModels returns available models.
func (s *Service) ListModels() []*models.GPUModel {
	ollamaModels, err := s.ollama.ListModels()
	if err != nil {
		return nil
	}
	result := make([]*models.GPUModel, 0, len(ollamaModels))
	for _, m := range ollamaModels {
		result = append(result, &models.GPUModel{
			Name:           m.Name,
			SizeBytes:      m.SizeBytes,
			ParameterCount: m.ParameterCount,
			Quantized:      m.Quantized,
			Serving:        m.Serving,
			Port:           m.Port,
			NodeID:         m.NodeID,
			Replicas:       m.Replicas,
			LastPulled:     m.LastPulled,
		})
	}
	return result
}

// RemoveModel removes a model.
func (s *Service) RemoveModel(name string) error {
	return s.ollama.RemoveModel(name)
}

// ServeModel starts serving a model.
func (s *Service) ServeModel(name, nodeID string, port, replicas int) (*models.GPUModel, error) {
	if err := s.ollama.ServeModel(name, port); err != nil {
		return nil, err
	}
	return &models.GPUModel{
		Name:         name,
		Serving:      true,
		Port:         port,
		NodeID:       nodeID,
		Replicas:     replicas,
		LastPulled:   time.Now(),
	}, nil
}

// GetModelStatus returns model serving status.
func (s *Service) GetModelStatus(name string) (*models.GPUModel, error) {
	status, err := s.ollama.GetModelStatus(name)
	if err != nil {
		return nil, err
	}
	return &models.GPUModel{
		Name:    status.Name,
		Serving: status.Serving,
	}, nil
}

// ListQuotas returns all quotas.
func (s *Service) ListQuotas() []*models.GPUQuota {
	return s.quotas.ListQuotas()
}

// SetQuota sets a quota.
func (s *Service) SetQuota(q *models.GPUQuota) error {
	return s.quotas.SetQuota(q)
}

// GetQuotaUsage returns quota usage.
func (s *Service) GetQuotaUsage(tenantID string) (*models.QuotaUsage, error) {
	return s.quotas.GetUsage(tenantID)
}

// GetGPUMetrics returns GPU metrics for a node.
func (s *Service) GetGPUMetrics(nodeID string, gpuCount int) (*models.GPUMetrics, error) {
	m, err := s.metrics.GetMetrics(nodeID)
	if err != nil {
		m = s.metrics.CollectMetrics(nodeID, gpuCount)
	}
	return m, nil
}
