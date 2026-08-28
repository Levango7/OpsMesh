package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Levango7/OpsMesh/services/workflow-svc/internal/engine"
	"github.com/Levango7/OpsMesh/services/workflow-svc/internal/models"
)

// Errors returned by the service.
var (
	ErrWorkflowNotFound      = errors.New("workflow not found")
	ErrWorkflowInvalid       = errors.New("workflow invalid")
	ErrExecutionNotFound     = errors.New("execution not found")
	ErrApprovalNotFound      = errors.New("approval not found")
	ErrApprovalNotResolvable = errors.New("approval not in pending state")
)

// WorkflowStore is the interface for workflow persistence.
type WorkflowStore interface {
	CreateWorkflow(*models.Workflow) *models.Workflow
	GetWorkflow(id string) *models.Workflow
	ListWorkflows() []*models.Workflow
	UpdateWorkflow(*models.Workflow) bool
	DeleteWorkflow(id string) bool
}

// Service implements the workflow service business logic.
type Service struct {
	mu      sync.RWMutex
	store   WorkflowStore
	engine  *engine.Engine
}

// NewService creates a new Service.
func NewService(s WorkflowStore, e *engine.Engine) *Service {
	return &Service{
		store:  s,
		engine: e,
	}
}

// CreateWorkflow creates a new workflow definition.
func (s *Service) CreateWorkflow(ctx context.Context, wf *models.Workflow) (*models.Workflow, error) {
	if wf == nil || wf.Name == "" {
		return nil, ErrWorkflowInvalid
	}

	if wf.ID == "" {
		wf.ID = uuid.New().String()
	}
	now := time.Now()
	wf.CreatedAt = now
	wf.UpdatedAt = now
	if wf.Status == "" {
		wf.Status = models.WorkflowStatusDraft
	}

	s.store.CreateWorkflow(wf)
	return wf, nil
}

// GetWorkflow retrieves a workflow by ID.
func (s *Service) GetWorkflow(ctx context.Context, id string) (*models.Workflow, error) {
	wf := s.store.GetWorkflow(id)
	if wf == nil {
		return nil, ErrWorkflowNotFound
	}
	return wf, nil
}

// ListWorkflows returns all workflows.
func (s *Service) ListWorkflows(ctx context.Context) ([]*models.Workflow, error) {
	return s.store.ListWorkflows(), nil
}

// UpdateWorkflow updates an existing workflow.
func (s *Service) UpdateWorkflow(ctx context.Context, wf *models.Workflow) (*models.Workflow, error) {
	if wf == nil || wf.ID == "" {
		return nil, ErrWorkflowInvalid
	}
	if !s.store.UpdateWorkflow(wf) {
		return nil, ErrWorkflowNotFound
	}
	return s.store.GetWorkflow(wf.ID), nil
}

// DeleteWorkflow removes a workflow by ID.
func (s *Service) DeleteWorkflow(ctx context.Context, id string) error {
	if !s.store.DeleteWorkflow(id) {
		return ErrWorkflowNotFound
	}
	return nil
}

// ExecuteWorkflow starts execution of a workflow.
func (s *Service) ExecuteWorkflow(ctx context.Context, id string) (*models.Execution, error) {
	wf := s.store.GetWorkflow(id)
	if wf == nil {
		return nil, ErrWorkflowNotFound
	}
	if wf.Status != models.WorkflowStatusActive {
		return nil, ErrWorkflowInvalid
	}

	return s.engine.StartExecution(ctx, wf)
}

// GetExecutions returns all executions for a workflow.
func (s *Service) GetExecutions(ctx context.Context, workflowID string) ([]*models.Execution, error) {
	all := s.engine.ListExecutions()
	result := make([]*models.Execution, 0)
	for _, ex := range all {
		if ex.WorkflowID == workflowID {
			result = append(result, ex)
		}
	}
	return result, nil
}

// ApproveApproval approves a pending approval.
func (s *Service) ApproveApproval(ctx context.Context, approvalID, resolvedBy, comment string) error {
	if err := s.engine.ApproveApproval(approvalID, resolvedBy, comment); err != nil {
		if err.Error() == "approval not found: "+approvalID {
			return ErrApprovalNotFound
		}
		return ErrApprovalNotResolvable
	}
	return nil
}

// RejectApproval rejects a pending approval.
func (s *Service) RejectApproval(ctx context.Context, approvalID, resolvedBy, comment string) error {
	if err := s.engine.RejectApproval(approvalID, resolvedBy, comment); err != nil {
		if err.Error() == "approval not found: "+approvalID {
			return ErrApprovalNotFound
		}
		return ErrApprovalNotResolvable
	}
	return nil
}

// HandleWebhookCallback processes an incoming webhook callback.
func (s *Service) HandleWebhookCallback(ctx context.Context, executionID, nodeID string, payload map[string]string) (*models.WebhookCallback, error) {
	return s.engine.HandleWebhookCallback(executionID, nodeID, payload)
}

// GetApproval retrieves an approval by ID.
func (s *Service) GetApproval(id string) *models.Approval {
	return s.engine.GetApproval(id)
}
