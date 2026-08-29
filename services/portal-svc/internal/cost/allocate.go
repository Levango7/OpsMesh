package cost

import (
	"fmt"
	"sort"
	"sync"
)

// Dimension represents the cost allocation dimension.
type Dimension string

const (
	DimensionDevice     Dimension = "device"
	DimensionTenant     Dimension = "tenant"
	DimensionDepartment Dimension = "department"
	DimensionProject    Dimension = "project"
)

// AllocationRule defines how costs should be allocated across a dimension.
type AllocationRule struct {
	Dimension Dimension `json:"dimension"`
	Weight    float64   `json:"weight"`
	Tags      []string  `json:"targets"`
}

// AllocationResult represents the result of a cost allocation for a single target.
type AllocationResult struct {
	Dimension       string  `json:"dimension"`
	Target          string  `json:"target"`
	AllocatedAmount float64 `json:"allocated_amount"`
	Percentage      float64 `json:"percentage"`
	Details         string  `json:"details"`
}

// AllocationReport contains the full results of a cost allocation run.
type AllocationReport struct {
	TotalCost float64            `json:"total_cost"`
	Rule      AllocationRule     `json:"rule"`
	Results   []AllocationResult `json:"results"`
}

// CostEntry represents a single cost item to be allocated.
type CostEntry struct {
	Target string  `json:"target"`
	Amount float64 `json:"amount"`
}

// Allocator manages allocation rules and performs cost allocation.
type Allocator struct {
	mu    sync.RWMutex
	rules map[Dimension]AllocationRule
}

// NewAllocator creates a new Allocator.
func NewAllocator() *Allocator {
	return &Allocator{
		rules: make(map[Dimension]AllocationRule),
	}
}

// SetAllocationRules stores one or more allocation rules.
func (a *Allocator) SetAllocationRules(rules []AllocationRule) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, rule := range rules {
		rule.Tags = dedupeStrings(rule.Tags)
		a.rules[rule.Dimension] = rule
	}
}

// GetAllocationRules returns all stored allocation rules.
func (a *Allocator) GetAllocationRules() []AllocationRule {
	a.mu.RLock()
	defer a.mu.RUnlock()
	rules := make([]AllocationRule, 0, len(a.rules))
	for _, rule := range a.rules {
		rules = append(rules, rule)
	}
	sort.Slice(rules, func(i, j int) bool {
		return string(rules[i].Dimension) < string(rules[j].Dimension)
	})
	return rules
}

// AllocateCosts distributes totalCost across the targets defined in the rule for the given dimension.
func (a *Allocator) AllocateCosts(dimension Dimension, totalCost float64, entries []CostEntry) (*AllocationReport, error) {
	if totalCost <= 0 {
		return nil, fmt.Errorf("total cost must be positive, got %.2f", totalCost)
	}

	a.mu.RLock()
	rule, ok := a.rules[dimension]
	a.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no allocation rule found for dimension %q", dimension)
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("no cost entries provided for allocation")
	}

	results := make([]AllocationResult, 0, len(entries))
	var totalAllocated float64

	for _, entry := range entries {
		var share float64
		if len(rule.Tags) > 0 {
			matched := false
			for _, tag := range rule.Tags {
				if tag == entry.Target {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		share = entry.Amount * rule.Weight
		totalAllocated += share

		pct := 0.0
		if entry.Amount > 0 {
			pct = (share / totalCost) * 100
		}

		results = append(results, AllocationResult{
			Dimension:       string(dimension),
			Target:          entry.Target,
			AllocatedAmount: round2(share),
			Percentage:      round2(pct),
			Details:         fmt.Sprintf("allocated %.2f%% of %.2f at weight %.2f", pct, entry.Amount, rule.Weight),
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].AllocatedAmount > results[j].AllocatedAmount
	})

	return &AllocationReport{
		TotalCost: totalCost,
		Rule:      rule,
		Results:   results,
	}, nil
}

// GetAllocationReport runs allocation and returns the report (convenience wrapper).
func (a *Allocator) GetAllocationReport(dimension Dimension, totalCost float64, entries []CostEntry) (*AllocationReport, error) {
	return a.AllocateCosts(dimension, totalCost, entries)
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}
