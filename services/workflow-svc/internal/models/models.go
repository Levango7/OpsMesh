package models

import "time"

// WorkflowStatus represents the state of a workflow definition.
type WorkflowStatus string

const (
	WorkflowStatusDraft    WorkflowStatus = "draft"
	WorkflowStatusActive   WorkflowStatus = "active"
	WorkflowStatusArchived WorkflowStatus = "archived"
)

// ExecutionStatus represents the state of a workflow execution.
type ExecutionStatus string

const (
	ExecutionStatusRunning         ExecutionStatus = "running"
	ExecutionStatusWaitingApproval ExecutionStatus = "waiting_approval"
	ExecutionStatusCompleted       ExecutionStatus = "completed"
	ExecutionStatusFailed          ExecutionStatus = "failed"
)

// NodeType identifies the type of a workflow node.
type NodeType string

const (
	NodeTypeTask      NodeType = "task"
	NodeTypeApproval  NodeType = "approval"
	NodeTypeCondition NodeType = "condition"
	NodeTypeParallel  NodeType = "parallel"
	NodeTypeWebhook   NodeType = "webhook"
	NodeTypeTimer     NodeType = "timer"
)

// NodeStatus represents the status of an individual node during execution.
type NodeStatus string

const (
	NodeStatusPending   NodeStatus = "pending"
	NodeStatusRunning   NodeStatus = "running"
	NodeStatusCompleted NodeStatus = "completed"
	NodeStatusFailed    NodeStatus = "failed"
	NodeStatusSkipped   NodeStatus = "skipped"
)

// Workflow represents a DAG-based workflow definition.
type Workflow struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Status      WorkflowStatus `json:"status"`
	Nodes       []Node         `json:"nodes"`
	Edges       []Edge         `json:"edges"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// Node is a single unit of work in the workflow DAG.
type Node struct {
	ID      string            `json:"id"`
	Type    NodeType          `json:"type"`
	Name    string            `json:"name"`
	Config  map[string]string `json:"config,omitempty"`
	Timeout time.Duration     `json:"timeout,omitempty"`
}

// Edge defines a dependency between two nodes.
type Edge struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Condition string `json:"condition,omitempty"`
}

// Execution represents a single run of a workflow.
type Execution struct {
	ID           string                `json:"id"`
	WorkflowID   string                `json:"workflow_id"`
	Status       ExecutionStatus       `json:"status"`
	NodeStates   map[string]NodeStatus `json:"node_states"`
	Context      map[string]string     `json:"context,omitempty"`
	StartedAt    time.Time             `json:"started_at"`
	CompletedAt  time.Time             `json:"completed_at,omitempty"`
	ErrorMessage string                `json:"error_message,omitempty"`
}

// Approval represents a pending human approval.
type Approval struct {
	ID          string    `json:"id"`
	ExecutionID string    `json:"execution_id"`
	NodeID      string    `json:"node_id"`
	WorkflowID  string    `json:"workflow_id"`
	Status      string    `json:"status"` // pending/approved/rejected
	RequestedAt time.Time `json:"requested_at"`
	ResolvedAt  time.Time `json:"resolved_at,omitempty"`
	ResolvedBy  string    `json:"resolved_by,omitempty"`
	Comment     string    `json:"comment,omitempty"`
}

// WebhookCallback represents an incoming webhook callback.
type WebhookCallback struct {
	ID          string            `json:"id"`
	ExecutionID string            `json:"execution_id"`
	NodeID      string            `json:"node_id"`
	Payload     map[string]string `json:"payload"`
	ReceivedAt  time.Time         `json:"received_at"`
}
