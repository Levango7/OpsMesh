package engine

import (
	"context"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/workflow-svc/internal/models"
)

func TestEngine_StartExecutionSimpleWorkflow(t *testing.T) {
	e := NewEngine()
	ctx := context.Background()

	wf := &models.Workflow{
		ID:     "wf-1",
		Name:   "Simple Workflow",
		Status: models.WorkflowStatusActive,
		Nodes: []models.Node{
			{ID: "n1", Type: models.NodeTypeTask, Name: "Start"},
			{ID: "n2", Type: models.NodeTypeTask, Name: "End"},
		},
		Edges: []models.Edge{
			{From: "n1", To: "n2"},
		},
	}

	exec, err := e.StartExecution(ctx, wf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exec.Status != models.ExecutionStatusRunning && exec.Status != models.ExecutionStatusCompleted {
		t.Fatalf("unexpected status: %s", exec.Status)
	}

	time.Sleep(200 * time.Millisecond)

	result := e.GetExecution(exec.ID)
	if result == nil {
		t.Fatal("execution not found")
	}
	if result.Status != models.ExecutionStatusCompleted {
		t.Fatalf("expected completed, got %s", result.Status)
	}
	if result.NodeStates["n1"] != models.NodeStatusCompleted {
		t.Fatalf("n1 not completed: %s", result.NodeStates["n1"])
	}
	if result.NodeStates["n2"] != models.NodeStatusCompleted {
		t.Fatalf("n2 not completed: %s", result.NodeStates["n2"])
	}
}

func TestEngine_StartExecutionInvalidWorkflow(t *testing.T) {
	e := NewEngine()
	ctx := context.Background()

	wf := &models.Workflow{
		ID:     "wf-bad",
		Name:   "Bad Workflow",
		Status: models.WorkflowStatusActive,
		Nodes: []models.Node{
			{ID: "n1", Type: models.NodeTypeTask, Name: "A"},
			{ID: "n2", Type: models.NodeTypeTask, Name: "B"},
		},
		Edges: []models.Edge{
			{From: "n1", To: "n2"},
			{From: "n2", To: "n1"},
		},
	}

	_, err := e.StartExecution(ctx, wf)
	if err == nil {
		t.Fatal("expected error for cyclic workflow")
	}
}

func TestEngine_StartExecutionApprovalNode(t *testing.T) {
	e := NewEngine()
	ctx := context.Background()

	wf := &models.Workflow{
		ID:     "wf-approval",
		Name:   "Approval Workflow",
		Status: models.WorkflowStatusActive,
		Nodes: []models.Node{
			{ID: "n1", Type: models.NodeTypeTask, Name: "Start"},
			{ID: "n2", Type: models.NodeTypeApproval, Name: "Approve"},
			{ID: "n3", Type: models.NodeTypeTask, Name: "End"},
		},
		Edges: []models.Edge{
			{From: "n1", To: "n2"},
			{From: "n2", To: "n3"},
		},
	}

	exec, err := e.StartExecution(ctx, wf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	result := e.GetExecution(exec.ID)
	if result.Status != models.ExecutionStatusWaitingApproval {
		t.Fatalf("expected waiting_approval, got %s", result.Status)
	}

	approvals := e.ListApprovals()
	if len(approvals) != 1 {
		t.Fatalf("expected 1 approval, got %d", len(approvals))
	}
	if approvals[0].Status != "pending" {
		t.Fatalf("expected pending approval, got %s", approvals[0].Status)
	}
}

func TestEngine_ApproveApproval(t *testing.T) {
	e := NewEngine()
	ctx := context.Background()

	wf := &models.Workflow{
		ID:     "wf-approve-test",
		Name:   "Approve Test",
		Status: models.WorkflowStatusActive,
		Nodes: []models.Node{
			{ID: "n1", Type: models.NodeTypeTask, Name: "Start"},
			{ID: "n2", Type: models.NodeTypeApproval, Name: "Approve"},
			{ID: "n3", Type: models.NodeTypeTask, Name: "End"},
		},
		Edges: []models.Edge{
			{From: "n1", To: "n2"},
			{From: "n2", To: "n3"},
		},
	}

	_, err := e.StartExecution(ctx, wf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	approvals := e.ListApprovals()
	if len(approvals) == 0 {
		t.Fatal("expected at least one approval")
	}

	approvalID := approvals[0].ID
	err = e.ApproveApproval(approvalID, "test-user", "looks good")
	if err != nil {
		t.Fatalf("unexpected error approving: %v", err)
	}

	approval := e.GetApproval(approvalID)
	if approval.Status != "approved" {
		t.Fatalf("expected approved, got %s", approval.Status)
	}
	if approval.ResolvedBy != "test-user" {
		t.Fatalf("expected resolved by test-user, got %s", approval.ResolvedBy)
	}
}

func TestEngine_RejectApproval(t *testing.T) {
	e := NewEngine()
	ctx := context.Background()

	wf := &models.Workflow{
		ID:     "wf-reject-test",
		Name:   "Reject Test",
		Status: models.WorkflowStatusActive,
		Nodes: []models.Node{
			{ID: "n1", Type: models.NodeTypeApproval, Name: "Approve"},
		},
		Edges: []models.Edge{},
	}

	_, err := e.StartExecution(ctx, wf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	approvals := e.ListApprovals()
	if len(approvals) == 0 {
		t.Fatal("expected at least one approval")
	}

	approvalID := approvals[0].ID
	err = e.RejectApproval(approvalID, "admin", "not allowed")
	if err != nil {
		t.Fatalf("unexpected error rejecting: %v", err)
	}

	approval := e.GetApproval(approvalID)
	if approval.Status != "rejected" {
		t.Fatalf("expected rejected, got %s", approval.Status)
	}
}

func TestEngine_ApproveAlreadyResolved(t *testing.T) {
	e := NewEngine()
	ctx := context.Background()

	wf := &models.Workflow{
		ID:     "wf-double",
		Name:   "Double Approve",
		Status: models.WorkflowStatusActive,
		Nodes: []models.Node{
			{ID: "n1", Type: models.NodeTypeApproval, Name: "Approve"},
		},
	}

	_, err := e.StartExecution(ctx, wf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	approvals := e.ListApprovals()
	approvalID := approvals[0].ID

	err = e.ApproveApproval(approvalID, "user1", "ok")
	if err != nil {
		t.Fatalf("first approve should succeed: %v", err)
	}

	err = e.ApproveApproval(approvalID, "user2", "also ok")
	if err == nil {
		t.Fatal("expected error on second approve")
	}
}

func TestEngine_HandleWebhookCallback(t *testing.T) {
	e := NewEngine()
	ctx := context.Background()

	wf := &models.Workflow{
		ID:     "wf-webhook",
		Name:   "Webhook Test",
		Status: models.WorkflowStatusActive,
		Nodes: []models.Node{
			{ID: "n1", Type: models.NodeTypeTask, Name: "Start"},
			{ID: "n2", Type: models.NodeTypeWebhook, Name: "Wait for callback"},
			{ID: "n3", Type: models.NodeTypeTask, Name: "End"},
		},
		Edges: []models.Edge{
			{From: "n1", To: "n2"},
			{From: "n2", To: "n3"},
		},
	}

	exec, err := e.StartExecution(ctx, wf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	result := e.GetExecution(exec.ID)
	if result.Status != models.ExecutionStatusWaitingApproval {
		t.Fatalf("expected waiting_approval, got %s", result.Status)
	}

	cb, err := e.HandleWebhookCallback(exec.ID, "n2", map[string]string{"status": "done"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cb.ExecutionID != exec.ID {
		t.Fatalf("wrong execution ID: %s", cb.ExecutionID)
	}
}

func TestEngine_HandleWebhookInvalidExecution(t *testing.T) {
	e := NewEngine()

	_, err := e.HandleWebhookCallback("nonexistent", "n1", map[string]string{"status": "done"})
	if err == nil {
		t.Fatal("expected error for invalid execution")
	}
}

func TestEngine_TimerNode(t *testing.T) {
	e := NewEngine()
	ctx := context.Background()

	wf := &models.Workflow{
		ID:     "wf-timer",
		Name:   "Timer Test",
		Status: models.WorkflowStatusActive,
		Nodes: []models.Node{
			{ID: "n1", Type: models.NodeTypeTimer, Name: "Wait", Config: map[string]string{"duration": "50ms"}},
			{ID: "n2", Type: models.NodeTypeTask, Name: "After Timer"},
		},
		Edges: []models.Edge{
			{From: "n1", To: "n2"},
		},
	}

	exec, err := e.StartExecution(ctx, wf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	result := e.GetExecution(exec.ID)
	if result.Status != models.ExecutionStatusCompleted {
		t.Fatalf("expected completed, got %s", result.Status)
	}
}

func TestEngine_EmptyWorkflow(t *testing.T) {
	e := NewEngine()
	ctx := context.Background()

	wf := &models.Workflow{
		ID:     "wf-empty",
		Name:   "Empty",
		Status: models.WorkflowStatusActive,
		Nodes:  []models.Node{},
		Edges:  []models.Edge{},
	}

	_, err := e.StartExecution(ctx, wf)
	if err == nil {
		t.Fatal("expected error for empty workflow")
	}
}

func TestEngine_ResumeExecution(t *testing.T) {
	e := NewEngine()
	ctx := context.Background()

	wf := &models.Workflow{
		ID:     "wf-resume",
		Name:   "Resume Test",
		Status: models.WorkflowStatusActive,
		Nodes: []models.Node{
			{ID: "n1", Type: models.NodeTypeTask, Name: "Done"},
		},
	}

	exec, err := e.StartExecution(ctx, wf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	err = e.ResumeExecution(ctx, exec.ID)
	if err != nil {
		t.Fatalf("unexpected error resuming: %v", err)
	}

	result := e.GetExecution(exec.ID)
	if result.Status != models.ExecutionStatusRunning && result.Status != models.ExecutionStatusCompleted {
		t.Fatalf("unexpected status after resume: %s", result.Status)
	}
}

func TestEngine_ResumeExecutionNotFound(t *testing.T) {
	e := NewEngine()
	ctx := context.Background()

	err := e.ResumeExecution(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent execution")
	}
}

func TestEngine_MultiBranchWorkflow(t *testing.T) {
	e := NewEngine()
	ctx := context.Background()

	wf := &models.Workflow{
		ID:     "wf-multi",
		Name:   "Multi Branch",
		Status: models.WorkflowStatusActive,
		Nodes: []models.Node{
			{ID: "start", Type: models.NodeTypeTask, Name: "Start"},
			{ID: "branch1", Type: models.NodeTypeTask, Name: "Branch 1"},
			{ID: "branch2", Type: models.NodeTypeTask, Name: "Branch 2"},
			{ID: "end", Type: models.NodeTypeTask, Name: "End"},
		},
		Edges: []models.Edge{
			{From: "start", To: "branch1"},
			{From: "start", To: "branch2"},
			{From: "branch1", To: "end"},
			{From: "branch2", To: "end"},
		},
	}

	exec, err := e.StartExecution(ctx, wf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	result := e.GetExecution(exec.ID)
	if result.Status != models.ExecutionStatusCompleted {
		t.Fatalf("expected completed, got %s: %s", result.Status, result.ErrorMessage)
	}
}

func TestEngine_GetExecutionNotFound(t *testing.T) {
	e := NewEngine()
	result := e.GetExecution("nonexistent")
	if result != nil {
		t.Fatal("expected nil for nonexistent execution")
	}
}

func TestEngine_ListExecutions(t *testing.T) {
	e := NewEngine()
	ctx := context.Background()

	wf := &models.Workflow{
		ID:     "wf-list",
		Name:   "List Test",
		Status: models.WorkflowStatusActive,
		Nodes: []models.Node{
			{ID: "n1", Type: models.NodeTypeTask, Name: "Task"},
		},
	}

	_, err := e.StartExecution(ctx, wf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	execs := e.ListExecutions()
	if len(execs) < 1 {
		t.Fatalf("expected at least 1 execution, got %d", len(execs))
	}
}
