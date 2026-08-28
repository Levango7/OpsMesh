package service

import (
	"context"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/workflow-svc/internal/engine"
	"github.com/Levango7/OpsMesh/services/workflow-svc/internal/models"
)

// memoryStore is a simple in-memory store for testing.
type memoryStore struct {
	workflows map[string]*models.Workflow
}

func newMemoryStore() *memoryStore {
	return &memoryStore{workflows: make(map[string]*models.Workflow)}
}

func (m *memoryStore) CreateWorkflow(wf *models.Workflow) *models.Workflow {
	m.workflows[wf.ID] = wf
	return wf
}

func (m *memoryStore) GetWorkflow(id string) *models.Workflow {
	return m.workflows[id]
}

func (m *memoryStore) ListWorkflows() []*models.Workflow {
	result := make([]*models.Workflow, 0, len(m.workflows))
	for _, wf := range m.workflows {
		result = append(result, wf)
	}
	return result
}

func (m *memoryStore) UpdateWorkflow(wf *models.Workflow) bool {
	if _, ok := m.workflows[wf.ID]; !ok {
		return false
	}
	wf.UpdatedAt = time.Now()
	m.workflows[wf.ID] = wf
	return true
}

func (m *memoryStore) DeleteWorkflow(id string) bool {
	if _, ok := m.workflows[id]; !ok {
		return false
	}
	delete(m.workflows, id)
	return true
}

func TestService_CreateWorkflow(t *testing.T) {
	svc := NewService(newMemoryStore(), engine.NewEngine())
	ctx := context.Background()

	wf := &models.Workflow{
		Name:        "Test Workflow",
		Description: "A test",
		Nodes: []models.Node{
			{ID: "n1", Type: models.NodeTypeTask, Name: "Task"},
		},
	}

	created, err := svc.CreateWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected ID to be set")
	}
	if created.Status != models.WorkflowStatusDraft {
		t.Fatalf("expected draft status, got %s", created.Status)
	}
}

func TestService_CreateWorkflowInvalid(t *testing.T) {
	svc := NewService(newMemoryStore(), engine.NewEngine())
	ctx := context.Background()

	_, err := svc.CreateWorkflow(ctx, &models.Workflow{Name: ""})
	if err != ErrWorkflowInvalid {
		t.Fatalf("expected ErrWorkflowInvalid, got %v", err)
	}

	_, err = svc.CreateWorkflow(ctx, nil)
	if err != ErrWorkflowInvalid {
		t.Fatalf("expected ErrWorkflowInvalid, got %v", err)
	}
}

func TestService_GetWorkflow(t *testing.T) {
	svc := NewService(newMemoryStore(), engine.NewEngine())
	ctx := context.Background()

	wf := &models.Workflow{Name: "Test", Nodes: []models.Node{{ID: "n1", Type: models.NodeTypeTask, Name: "Task"}}}
	created, _ := svc.CreateWorkflow(ctx, wf)

	got, err := svc.GetWorkflow(ctx, created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "Test" {
		t.Fatalf("expected Test, got %s", got.Name)
	}
}

func TestService_GetWorkflowNotFound(t *testing.T) {
	svc := NewService(newMemoryStore(), engine.NewEngine())
	ctx := context.Background()

	_, err := svc.GetWorkflow(ctx, "nonexistent")
	if err != ErrWorkflowNotFound {
		t.Fatalf("expected ErrWorkflowNotFound, got %v", err)
	}
}

func TestService_ListWorkflows(t *testing.T) {
	svc := NewService(newMemoryStore(), engine.NewEngine())
	ctx := context.Background()

	svc.CreateWorkflow(ctx, &models.Workflow{Name: "W1", Nodes: []models.Node{{ID: "n1", Type: models.NodeTypeTask, Name: "Task"}}})
	svc.CreateWorkflow(ctx, &models.Workflow{Name: "W2", Nodes: []models.Node{{ID: "n2", Type: models.NodeTypeTask, Name: "Task"}}})

	list, err := svc.ListWorkflows(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 workflows, got %d", len(list))
	}
}

func TestService_UpdateWorkflow(t *testing.T) {
	svc := NewService(newMemoryStore(), engine.NewEngine())
	ctx := context.Background()

	wf := &models.Workflow{Name: "Original", Nodes: []models.Node{{ID: "n1", Type: models.NodeTypeTask, Name: "Task"}}}
	created, _ := svc.CreateWorkflow(ctx, wf)

	created.Name = "Updated"
	updated, err := svc.UpdateWorkflow(ctx, created)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Name != "Updated" {
		t.Fatalf("expected Updated, got %s", updated.Name)
	}
}

func TestService_UpdateWorkflowNotFound(t *testing.T) {
	svc := NewService(newMemoryStore(), engine.NewEngine())
	ctx := context.Background()

	_, err := svc.UpdateWorkflow(ctx, &models.Workflow{ID: "nonexistent", Name: "X"})
	if err != ErrWorkflowNotFound {
		t.Fatalf("expected ErrWorkflowNotFound, got %v", err)
	}
}

func TestService_DeleteWorkflow(t *testing.T) {
	svc := NewService(newMemoryStore(), engine.NewEngine())
	ctx := context.Background()

	wf := &models.Workflow{Name: "Delete Me", Nodes: []models.Node{{ID: "n1", Type: models.NodeTypeTask, Name: "Task"}}}
	created, _ := svc.CreateWorkflow(ctx, wf)

	err := svc.DeleteWorkflow(ctx, created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.GetWorkflow(ctx, created.ID)
	if err != ErrWorkflowNotFound {
		t.Fatal("expected workflow to be deleted")
	}
}

func TestService_DeleteWorkflowNotFound(t *testing.T) {
	svc := NewService(newMemoryStore(), engine.NewEngine())
	ctx := context.Background()

	err := svc.DeleteWorkflow(ctx, "nonexistent")
	if err != ErrWorkflowNotFound {
		t.Fatalf("expected ErrWorkflowNotFound, got %v", err)
	}
}

func TestService_ExecuteWorkflow(t *testing.T) {
	svc := NewService(newMemoryStore(), engine.NewEngine())
	ctx := context.Background()

	wf := &models.Workflow{
		Name:   "Exec Test",
		Status: models.WorkflowStatusActive,
		Nodes: []models.Node{
			{ID: "n1", Type: models.NodeTypeTask, Name: "Task 1"},
			{ID: "n2", Type: models.NodeTypeTask, Name: "Task 2"},
		},
		Edges: []models.Edge{
			{From: "n1", To: "n2"},
		},
	}
	created, _ := svc.CreateWorkflow(ctx, wf)

	exec, err := svc.ExecuteWorkflow(ctx, created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exec.WorkflowID != created.ID {
		t.Fatalf("wrong workflow ID: %s", exec.WorkflowID)
	}
}

func TestService_ExecuteWorkflowNotFound(t *testing.T) {
	svc := NewService(newMemoryStore(), engine.NewEngine())
	ctx := context.Background()

	_, err := svc.ExecuteWorkflow(ctx, "nonexistent")
	if err != ErrWorkflowNotFound {
		t.Fatalf("expected ErrWorkflowNotFound, got %v", err)
	}
}

func TestService_ExecuteWorkflowNotActive(t *testing.T) {
	svc := NewService(newMemoryStore(), engine.NewEngine())
	ctx := context.Background()

	wf := &models.Workflow{
		Name:   "Draft Workflow",
		Status: models.WorkflowStatusDraft,
		Nodes: []models.Node{
			{ID: "n1", Type: models.NodeTypeTask, Name: "Task"},
		},
	}
	created, _ := svc.CreateWorkflow(ctx, wf)

	_, err := svc.ExecuteWorkflow(ctx, created.ID)
	if err != ErrWorkflowInvalid {
		t.Fatalf("expected ErrWorkflowInvalid, got %v", err)
	}
}

func TestService_GetExecutions(t *testing.T) {
	svc := NewService(newMemoryStore(), engine.NewEngine())
	ctx := context.Background()

	wf := &models.Workflow{
		Name:   "Exec List Test",
		Status: models.WorkflowStatusActive,
		Nodes: []models.Node{
			{ID: "n1", Type: models.NodeTypeTask, Name: "Task"},
		},
	}
	created, _ := svc.CreateWorkflow(ctx, wf)

	svc.ExecuteWorkflow(ctx, created.ID)
	time.Sleep(200 * time.Millisecond)

	execs, err := svc.GetExecutions(ctx, created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(execs) < 1 {
		t.Fatalf("expected at least 1 execution, got %d", len(execs))
	}
}

func TestService_ApproveApprovalNotFound(t *testing.T) {
	svc := NewService(newMemoryStore(), engine.NewEngine())
	ctx := context.Background()

	err := svc.ApproveApproval(ctx, "nonexistent", "user", "ok")
	if err != ErrApprovalNotFound {
		t.Fatalf("expected ErrApprovalNotFound, got %v", err)
	}
}

func TestService_RejectApprovalNotFound(t *testing.T) {
	svc := NewService(newMemoryStore(), engine.NewEngine())
	ctx := context.Background()

	err := svc.RejectApproval(ctx, "nonexistent", "user", "bad")
	if err != ErrApprovalNotFound {
		t.Fatalf("expected ErrApprovalNotFound, got %v", err)
	}
}

func TestService_HandleWebhookCallback(t *testing.T) {
	svc := NewService(newMemoryStore(), engine.NewEngine())
	ctx := context.Background()

	wf := &models.Workflow{
		Name:   "Webhook Test",
		Status: models.WorkflowStatusActive,
		Nodes: []models.Node{
			{ID: "n1", Type: models.NodeTypeTask, Name: "Task"},
			{ID: "n2", Type: models.NodeTypeWebhook, Name: "Webhook"},
			{ID: "n3", Type: models.NodeTypeTask, Name: "End"},
		},
		Edges: []models.Edge{
			{From: "n1", To: "n2"},
			{From: "n2", To: "n3"},
		},
	}
	created, _ := svc.CreateWorkflow(ctx, wf)

	exec, err := svc.ExecuteWorkflow(ctx, created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	cb, err := svc.HandleWebhookCallback(ctx, exec.ID, "n2", map[string]string{"result": "ok"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cb.NodeID != "n2" {
		t.Fatalf("wrong node ID: %s", cb.NodeID)
	}
}
