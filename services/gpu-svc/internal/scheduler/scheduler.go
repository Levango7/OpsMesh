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

// Scheduler handles workload scheduling across GPU nodes.
type Scheduler struct {
	mu       sync.RWMutex
	policies []models.SchedulingPolicy
	now      func() time.Time
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

// GetQueue returns pending workloads (placeholder for queue management).
func (s *Scheduler) GetQueue() []*models.Workload {
	return []*models.Workload{}
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
