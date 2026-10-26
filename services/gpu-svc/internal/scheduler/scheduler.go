package scheduler

import (
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/models"
)

// ErrNoSuitableNode is returned when no node can satisfy the workload.
var ErrNoSuitableNode = errors.New("no suitable node found for scheduling")

// ErrInvalidPolicy is returned when a scheduling policy is invalid.
var ErrInvalidPolicy = errors.New("invalid scheduling policy")

// NodeProvider provides node information to the scheduler.
type NodeProvider interface {
	GetOnlineNodes() []*models.GPUNode
	Get(id string) (*models.GPUNode, error)
}

// WorkloadProvider provides workload queue information to the scheduler.
type WorkloadProvider interface {
	GetPendingWorkloads() []*models.Workload
}

// Scheduler handles workload scheduling across GPU nodes.
type Scheduler struct {
	mu              sync.RWMutex
	policies        []models.SchedulingPolicy
	now             func() time.Time
	workloadProvider WorkloadProvider
}

// NewScheduler creates a new Scheduler.
func NewScheduler(now func() time.Time) *Scheduler {
	if now == nil {
		now = time.Now
	}
	return &Scheduler{
		policies: defaultPolicies(),
		now:      now,
	}
}

// NewSchedulerWithWorkloads creates a new Scheduler with a workload provider.
func NewSchedulerWithWorkloads(now func() time.Time, wp WorkloadProvider) *Scheduler {
	s := NewScheduler(now)
	s.workloadProvider = wp
	return s
}

// SetWorkloadProvider sets the workload provider for queue management.
func (s *Scheduler) SetWorkloadProvider(wp WorkloadProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workloadProvider = wp
}

// defaultPolicies returns the default scheduling policies.
func defaultPolicies() []models.SchedulingPolicy {
	return []models.SchedulingPolicy{
		{
			Name:           "bin-packing",
			Type:           "bin_packing",
			Enabled:        true,
			PriorityWeight: 10,
			MaxGPUsPerNode: 8,
		},
		{
			Name:           "priority-scheduling",
			Type:           "priority",
			Enabled:        true,
			PriorityWeight: 5,
			MaxGPUsPerNode: 8,
		},
	}
}

// Schedule attempts to assign a workload to GPU nodes.
func (s *Scheduler) Schedule(workload *models.Workload, provider NodeProvider) (*models.ScheduleResult, error) {
	if workload == nil {
		return nil, errors.New("workload is nil")
	}

	result := &models.ScheduleResult{
		WorkloadID: workload.ID,
		Assigned:   false,
	}

	nodes := provider.GetOnlineNodes()
	if len(nodes) == 0 {
		result.Reason = "no online nodes available"
		return result, ErrNoSuitableNode
	}

	// Sort nodes based on active policy
	policy := s.activePolicy()
	eligible := s.filterAndSortNodes(nodes, workload, policy)

	if len(eligible) == 0 {
		result.Reason = "no nodes with sufficient GPU resources"
		return result, ErrNoSuitableNode
	}

	// Assign to the best node(s)
	selected := s.selectNodes(eligible, workload, policy)
	if len(selected) == 0 {
		result.Reason = "could not allocate GPUs on any node"
		return result, ErrNoSuitableNode
	}

	result.Assigned = true
	result.NodeIDs = selected
	return result, nil
}

// GetPolicies returns all scheduling policies.
func (s *Scheduler) GetPolicies() []models.SchedulingPolicy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]models.SchedulingPolicy, len(s.policies))
	copy(out, s.policies)
	return out
}

// SetPolicies replaces all scheduling policies.
func (s *Scheduler) SetPolicies(policies []models.SchedulingPolicy) error {
	for _, p := range policies {
		if p.Type != "bin_packing" && p.Type != "spreading" && p.Type != "affinity" && p.Type != "anti-affinity" && p.Type != "priority" {
			return ErrInvalidPolicy
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policies = make([]models.SchedulingPolicy, len(policies))
	copy(s.policies, policies)
	return nil
}

// GetQueue returns pending workloads sorted by priority (highest first).
func (s *Scheduler) GetQueue() []*models.Workload {
	s.mu.RLock()
	wp := s.workloadProvider
	s.mu.RUnlock()

	if wp == nil {
		return []*models.Workload{}
	}

	pending := wp.GetPendingWorkloads()
	if pending == nil {
		return []*models.Workload{}
	}

	// Copy to avoid mutation
	out := make([]*models.Workload, len(pending))
	copy(out, pending)

	// Sort by priority (descending), then by creation time (ascending)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})

	return out
}

// AssignNodeResources updates node VRAM allocation after scheduling.
// Returns true if allocation succeeded, false if insufficient resources.
func (s *Scheduler) AssignNodeResources(node *models.GPUNode, vramMB int) bool {
	if node == nil || vramMB <= 0 {
		return false
	}
	if node.UsedVRAMMB+vramMB > node.TotalVRAMMB {
		return false
	}
	node.UsedVRAMMB += vramMB
	node.UpdatedAt = s.now()
	return true
}

// ReleaseNodeResources releases VRAM allocation when a workload is cancelled/completed.
func (s *Scheduler) ReleaseNodeResources(node *models.GPUNode, vramMB int) {
	if node == nil || vramMB <= 0 {
		return
	}
	node.UsedVRAMMB -= vramMB
	if node.UsedVRAMMB < 0 {
		node.UsedVRAMMB = 0
	}
	node.UpdatedAt = s.now()
}

// activePolicy returns the highest priority enabled policy.
func (s *Scheduler) activePolicy() *models.SchedulingPolicy {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var best *models.SchedulingPolicy
	for i := range s.policies {
		if s.policies[i].Enabled {
			if best == nil || s.policies[i].PriorityWeight > best.PriorityWeight {
				best = &s.policies[i]
			}
		}
	}
	return best
}

// filterAndSortNodes returns nodes that can satisfy the workload, sorted by policy.
func (s *Scheduler) filterAndSortNodes(nodes []*models.GPUNode, workload *models.Workload, policy *models.SchedulingPolicy) []*models.GPUNode {
	eligible := make([]*models.GPUNode, 0)
	for _, n := range nodes {
		availableGPUs := len(n.GPUs) - countAllocated(n)
		if availableGPUs < workload.GPURequest.Count {
			continue
		}
		if workload.GPURequest.MinVRAMMB > 0 {
			if !hasSufficientVRAM(n, workload.GPURequest.MinVRAMMB) {
				continue
			}
		}
		if policy != nil && policy.MaxGPUsPerNode > 0 && workload.GPURequest.Count > policy.MaxGPUsPerNode {
			continue
		}

		// Affinity check: if policy has labels, node must match all of them
		if policy != nil && len(policy.Labels) > 0 {
			if !matchLabels(n.Labels, policy.Labels) {
				continue
			}
		}

		// Affinity policy type: workload requires specific node labels
		if policy != nil && policy.Type == "affinity" && len(policy.Labels) > 0 {
			if !matchLabels(n.Labels, policy.Labels) {
				continue
			}
		}

		// Anti-affinity policy type: workload must NOT run on nodes with matching labels
		if policy != nil && policy.Type == "anti-affinity" && len(policy.Labels) > 0 {
			if matchLabels(n.Labels, policy.Labels) {
				continue
			}
		}

		// GPU model preference
		if workload.GPURequest.GPUModel != "" {
			if !nodeHasGPUModel(n, workload.GPURequest.GPUModel) {
				continue
			}
		}

		eligible = append(eligible, n)
	}

	// Sort based on policy type
	if policy != nil && policy.Type == "bin_packing" {
		sort.Slice(eligible, func(i, j int) bool {
			return eligible[i].UsedVRAMMB > eligible[j].UsedVRAMMB
		})
	} else if policy != nil && policy.Type == "spreading" {
		sort.Slice(eligible, func(i, j int) bool {
			return eligible[i].UsedVRAMMB < eligible[j].UsedVRAMMB
		})
	} else if policy != nil && policy.Type == "priority" {
		// Priority: prefer nodes with more available resources for high-priority workloads
		sort.Slice(eligible, func(i, j int) bool {
			availI := eligible[i].TotalVRAMMB - eligible[i].UsedVRAMMB
			availJ := eligible[j].TotalVRAMMB - eligible[j].UsedVRAMMB
			return availI > availJ
		})
	} else {
		sort.Slice(eligible, func(i, j int) bool {
			return len(eligible[i].GPUs) > len(eligible[j].GPUs)
		})
	}

	return eligible
}

// selectNodes picks the best nodes for the workload.
func (s *Scheduler) selectNodes(nodes []*models.GPUNode, workload *models.Workload, policy *models.SchedulingPolicy) []string {
	if len(nodes) == 0 {
		return nil
	}

	gpusNeeded := workload.GPURequest.Count
	selected := make([]string, 0)

	for _, n := range nodes {
		available := len(n.GPUs) - countAllocated(n)
		if available <= 0 {
			continue
		}
		selected = append(selected, n.ID)
		gpusNeeded -= available
		if gpusNeeded <= 0 {
			break
		}
	}

	if gpusNeeded > 0 {
		return nil
	}
	return selected
}

func countAllocated(n *models.GPUNode) int {
	if len(n.GPUs) == 0 {
		return 0
	}
	vramPerGPU := n.TotalVRAMMB / len(n.GPUs)
	if vramPerGPU == 0 {
		return 0
	}
	allocated := n.UsedVRAMMB / vramPerGPU
	if allocated > len(n.GPUs) {
		return len(n.GPUs)
	}
	return allocated
}

func hasSufficientVRAM(n *models.GPUNode, minVRAMMB int) bool {
	availableVRAM := n.TotalVRAMMB - n.UsedVRAMMB
	return availableVRAM >= minVRAMMB
}

// matchLabels checks if node labels contain all policy labels.
func matchLabels(nodeLabels, policyLabels map[string]string) bool {
	if len(policyLabels) == 0 {
		return true
	}
	if nodeLabels == nil {
		return false
	}
	for k, v := range policyLabels {
		if nodeLabels[k] != v {
			return false
		}
	}
	return true
}

// nodeHasGPUModel checks if a node has at least one GPU matching the requested model.
func nodeHasGPUModel(n *models.GPUNode, model string) bool {
	for _, g := range n.GPUs {
		if g.Model == model {
			return true
		}
	}
	return false
}
