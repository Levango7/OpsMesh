package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Levango7/OpsMesh/services/workflow-svc/internal/dag"
	"github.com/Levango7/OpsMesh/services/workflow-svc/internal/models"
)

// Engine executes workflow DAGs.
type Engine struct {
	mu         sync.RWMutex
	executions map[string]*models.Execution
	approvals  map[string]*models.Approval
	webhooks   map[string]*models.WebhookCallback
}

// NewEngine creates a new Engine.
func NewEngine() *Engine {
	return &Engine{
		executions: make(map[string]*models.Execution),
		approvals:  make(map[string]*models.Approval),
		webhooks:   make(map[string]*models.WebhookCallback),
	}
}

// StartExecution begins executing a workflow and returns the execution record.
func (e *Engine) StartExecution(ctx context.Context, wf *models.Workflow) (*models.Execution, error) {
	g := dag.NewGraph(wf.Nodes, wf.Edges)
	if err := g.Validate(); err != nil {
		return nil, fmt.Errorf("invalid workflow: %w", err)
	}

	execution := &models.Execution{
		ID:         uuid.New().String(),
		WorkflowID: wf.ID,
		Status:     models.ExecutionStatusRunning,
		NodeStates: make(map[string]models.NodeStatus),
		Context:    make(map[string]string),
		StartedAt:  time.Now(),
	}

	for _, node := range wf.Nodes {
		execution.NodeStates[node.ID] = models.NodeStatusPending
	}

	e.mu.Lock()
	e.executions[execution.ID] = execution
	e.mu.Unlock()

	go e.run(context.Background(), g, wf, execution)

	return execution, nil
}

// GetExecution retrieves an execution by ID.
func (e *Engine) GetExecution(id string) *models.Execution {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.executions[id]
}

// GetApproval retrieves an approval by ID.
func (e *Engine) GetApproval(id string) *models.Approval {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.approvals[id]
}

// ApproveApproval marks an approval as approved and resumes execution.
func (e *Engine) ApproveApproval(approvalID, resolvedBy, comment string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	approval, ok := e.approvals[approvalID]
	if !ok {
		return fmt.Errorf("approval not found: %s", approvalID)
	}
	if approval.Status != "pending" {
		return fmt.Errorf("approval already resolved: %s", approval.Status)
	}

	approval.Status = "approved"
	approval.ResolvedBy = resolvedBy
	approval.Comment = comment
	approval.ResolvedAt = time.Now()

	return nil
}

// RejectApproval marks an approval as rejected.
func (e *Engine) RejectApproval(approvalID, resolvedBy, comment string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	approval, ok := e.approvals[approvalID]
	if !ok {
		return fmt.Errorf("approval not found: %s", approvalID)
	}
	if approval.Status != "pending" {
		return fmt.Errorf("approval already resolved: %s", approval.Status)
	}

	approval.Status = "rejected"
	approval.ResolvedBy = resolvedBy
	approval.Comment = comment
	approval.ResolvedAt = time.Now()

	return nil
}

// HandleWebhookCallback processes an incoming webhook callback.
func (e *Engine) HandleWebhookCallback(executionID, nodeID string, payload map[string]string) (*models.WebhookCallback, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	execution, ok := e.executions[executionID]
	if !ok {
		return nil, fmt.Errorf("execution not found: %s", executionID)
	}

	cb := &models.WebhookCallback{
		ID:          uuid.New().String(),
		ExecutionID: executionID,
		NodeID:      nodeID,
		Payload:     payload,
		ReceivedAt:  time.Now(),
	}
	e.webhooks[cb.ID] = cb

	if state, ok := execution.NodeStates[nodeID]; ok && state == models.NodeStatusRunning {
		execution.NodeStates[nodeID] = models.NodeStatusCompleted
	}

	return cb, nil
}

// run executes the DAG asynchronously.
func (e *Engine) run(ctx context.Context, g *dag.Graph, wf *models.Workflow, execution *models.Execution) {
	roots := g.GetRoots()
	leaves := g.GetLeaves()
	_ = leaves

	ready := make([]string, len(roots))
	copy(ready, roots)

	for len(ready) > 0 {
		nodeID := ready[0]
		ready = ready[1:]

		node := g.Nodes[nodeID]
		e.mu.Lock()
		execution.NodeStates[nodeID] = models.NodeStatusRunning
		e.mu.Unlock()

		result := e.executeNode(ctx, node, execution)

		e.mu.Lock()
		if result == models.NodeStatusFailed {
			execution.NodeStates[nodeID] = models.NodeStatusFailed
			execution.Status = models.ExecutionStatusFailed
			execution.ErrorMessage = fmt.Sprintf("node '%s' failed", node.Name)
			execution.CompletedAt = time.Now()
			e.mu.Unlock()
			return
		}

		execution.NodeStates[nodeID] = result
		e.mu.Unlock()

		if node.Type == models.NodeTypeApproval && result == models.NodeStatusRunning {
			return
		}

		if node.Type == models.NodeTypeWebhook && result == models.NodeStatusRunning {
			e.mu.Lock()
			execution.Status = models.ExecutionStatusWaitingApproval
			e.mu.Unlock()
			return
		}

		for _, childID := range g.GetChildren(nodeID) {
			parents := g.GetParents(childID)
			allDone := true
			for _, p := range parents {
				e.mu.RLock()
				state := execution.NodeStates[p]
				e.mu.RUnlock()
				if state != models.NodeStatusCompleted && state != models.NodeStatusSkipped {
					allDone = false
					break
				}
			}
			if allDone {
				ready = append(ready, childID)
			}
		}
	}

	e.mu.Lock()
	allCompleted := true
	hasApprovalPending := false
	for _, node := range wf.Nodes {
		state := execution.NodeStates[node.ID]
		if state == models.NodeStatusRunning {
			hasApprovalPending = true
		}
		if state != models.NodeStatusCompleted && state != models.NodeStatusSkipped {
			allCompleted = false
		}
	}

	if hasApprovalPending {
		execution.Status = models.ExecutionStatusWaitingApproval
	} else if allCompleted {
		execution.Status = models.ExecutionStatusCompleted
		execution.CompletedAt = time.Now()
	} else {
		execution.Status = models.ExecutionStatusFailed
		execution.CompletedAt = time.Now()
	}
	e.mu.Unlock()
}

func (e *Engine) executeNode(ctx context.Context, node *models.Node, execution *models.Execution) models.NodeStatus {
	switch node.Type {
	case models.NodeTypeTask:
		return models.NodeStatusCompleted
	case models.NodeTypeApproval:
		e.mu.Lock()
		approval := &models.Approval{
			ID:          uuid.New().String(),
			ExecutionID: execution.ID,
			NodeID:      node.ID,
			WorkflowID:  execution.WorkflowID,
			Status:      "pending",
			RequestedAt: time.Now(),
		}
		e.approvals[approval.ID] = approval
		execution.Status = models.ExecutionStatusWaitingApproval
		e.mu.Unlock()
		return models.NodeStatusRunning
	case models.NodeTypeCondition:
		return models.NodeStatusCompleted
	case models.NodeTypeParallel:
		return models.NodeStatusCompleted
	case models.NodeTypeWebhook:
		e.mu.Lock()
		execution.Status = models.ExecutionStatusWaitingApproval
		e.mu.Unlock()
		return models.NodeStatusRunning
	case models.NodeTypeTimer:
		duration := 100 * time.Millisecond
		if t, ok := node.Config["duration"]; ok {
			if d, err := time.ParseDuration(t); err == nil {
				duration = d
			}
		}
		select {
		case <-ctx.Done():
			return models.NodeStatusFailed
		case <-time.After(duration):
			return models.NodeStatusCompleted
		}
	default:
		return models.NodeStatusFailed
	}
}

// ResumeExecution resumes a waiting execution after approval/webhook.
func (e *Engine) ResumeExecution(ctx context.Context, executionID string) error {
	e.mu.Lock()
	execution, ok := e.executions[executionID]
	if !ok {
		e.mu.Unlock()
		return fmt.Errorf("execution not found: %s", executionID)
	}
	execution.Status = models.ExecutionStatusRunning
	e.mu.Unlock()

	return nil
}

// ListExecutions returns all executions.
func (e *Engine) ListExecutions() []*models.Execution {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make([]*models.Execution, 0, len(e.executions))
	for _, ex := range e.executions {
		result = append(result, ex)
	}
	return result
}

// ListApprovals returns all approvals.
func (e *Engine) ListApprovals() []*models.Approval {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make([]*models.Approval, 0, len(e.approvals))
	for _, a := range e.approvals {
		result = append(result, a)
	}
	return result
}
