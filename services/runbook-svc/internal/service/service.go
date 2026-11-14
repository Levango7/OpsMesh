package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Levango7/OpsMesh/services/runbook-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/runbook-svc/internal/runner"
	"github.com/Levango7/OpsMesh/services/runbook-svc/internal/store"
)

// Errors returned by the service.
var (
	ErrRunbookNotFound = errors.New("runbook not found")
	ErrRunbookInvalid  = errors.New("runbook invalid")
	ErrRunbookDisabled = errors.New("runbook is disabled")
	// ErrTenantRequired：租户缺失时拒绝写入（D4；handler 层应提前以 401/400 拦掉）。
	ErrTenantRequired = errors.New("tenant id is required")
)

// Service implements the runbook service business logic.
type Service struct {
	store  store.RunbookStore
	runner *runner.Runner
}

// NewService creates a new Service.
func NewService(s store.RunbookStore, r *runner.Runner) *Service {
	return &Service{
		store:  s,
		runner: r,
	}
}

// CreateRunbook creates a new runbook.
func (s *Service) CreateRunbook(ctx context.Context, rb *models.Runbook) (*models.Runbook, error) {
	if rb == nil {
		return nil, ErrRunbookInvalid // nil 仍按"非法输入"报（保持既有契约）
	}
	if rb.TenantID == "" {
		return nil, ErrTenantRequired // 租户是写入前置条件（D4）
	}
	if rb.Name == "" {
		return nil, ErrRunbookInvalid
	}

	if rb.ID == "" {
		rb.ID = uuid.New().String()
	}
	now := time.Now()
	rb.CreatedAt = now
	rb.UpdatedAt = now
	if rb.Enabled && len(rb.Steps) == 0 && contentToSteps(rb.Content) == nil {
		return nil, ErrRunbookInvalid
	}

	s.store.CreateRunbook(rb)
	return rb, nil
}

// GetRunbook retrieves a runbook by ID.
func (s *Service) GetRunbook(ctx context.Context, id, tenantID string) (*models.Runbook, error) {
	rb := s.store.GetRunbook(id, tenantID)
	if rb == nil {
		return nil, ErrRunbookNotFound
	}
	return rb, nil
}

// ListRunbooks returns all runbooks.
func (s *Service) ListRunbooks(ctx context.Context, tenantID string) ([]*models.Runbook, error) {
	return s.store.ListRunbooks(tenantID), nil
}

// UpdateRunbook updates an existing runbook.
func (s *Service) UpdateRunbook(ctx context.Context, rb *models.Runbook, tenantID string) (*models.Runbook, error) {
	if rb == nil || rb.ID == "" {
		return nil, ErrRunbookInvalid
	}
	if !s.store.UpdateRunbook(rb, tenantID) {
		return nil, ErrRunbookNotFound
	}
	return s.store.GetRunbook(rb.ID, tenantID), nil
}

// DeleteRunbook removes a runbook by ID.
func (s *Service) DeleteRunbook(ctx context.Context, id, tenantID string) error {
	if !s.store.DeleteRunbook(id, tenantID) {
		return ErrRunbookNotFound
	}
	return nil
}

// TriggerRunbook executes a runbook by ID.
func (s *Service) TriggerRunbook(ctx context.Context, id string, triggeredBy, tenantID string) (*models.ExecutionRecord, error) {
	rb := s.store.GetRunbook(id, tenantID)
	if rb == nil {
		return nil, ErrRunbookNotFound
	}
	if !rb.Enabled {
		return nil, ErrRunbookDisabled
	}
	// 约束：前端契约只发 content 文本（无结构化 steps）；Steps 为空且 Content 非空时，
	// 降级为逐行 shell 步骤执行，禁止零步骤直接返回 success。
	if len(rb.Steps) == 0 {
		if steps := contentToSteps(rb.Content); steps != nil {
			rb.Steps = steps
		}
	}

	record := s.runner.Runbook(ctx, rb, triggeredBy)
	// 约束：执行记录必须有 ID，供前端 GET /runbooks/{id}/executions/{eid}/logs 定位。
	if record.ID == "" {
		record.ID = uuid.New().String()
	}
	record.TenantID = tenantID // 归属由触发租户钉住（AddExecution 会再校验父 runbook）
	s.store.AddExecution(record)
	return record, nil
}

// contentToSteps parses editor text content into shell steps (one per non-empty line).
// Returns nil when content yields no executable steps.
func contentToSteps(content string) []models.Step {
	lines := strings.Split(content, "\n")
	steps := make([]models.Step, 0, len(lines))
	for i, line := range lines {
		cmd := strings.TrimSpace(line)
		if cmd == "" || strings.HasPrefix(cmd, "#") {
			continue
		}
		steps = append(steps, models.Step{
			Name:    fmt.Sprintf("step-%d", i+1),
			Action:  "shell",
			Command: cmd,
			OnError: "stop",
		})
	}
	if len(steps) == 0 {
		return nil
	}
	return steps
}

// GetHistory returns execution history for a runbook.
func (s *Service) GetHistory(ctx context.Context, runbookID, tenantID string) ([]*models.ExecutionRecord, error) {
	return s.store.GetExecutions(runbookID, tenantID), nil
}

// HandleWebhook processes a webhook payload and triggers matching runbooks.
// HandleWebhook：租户口径 = **上游客体携带**（webhook 是外部触发，租户只能由调用方声明；
// handler 缺失即 400）。触发范围因此只限该租户的 runbook，不会跨租户触发。
func (s *Service) HandleWebhook(ctx context.Context, payload *models.WebhookPayload, tenantID string) ([]*models.ExecutionRecord, error) {
	if payload == nil {
		return nil, ErrRunbookInvalid
	}

	runbooks := s.store.ListRunbooks(tenantID)
	records := make([]*models.ExecutionRecord, 0)

	for _, rb := range runbooks {
		if !rb.Enabled {
			continue
		}
		if matchesTriggers(rb, payload) {
			record := s.runner.Runbook(ctx, rb, "webhook:"+payload.Source)
			if record.ID == "" {
				record.ID = uuid.New().String()
			}
			record.TenantID = tenantID
			s.store.AddExecution(record)
			records = append(records, record)
		}
	}

	return records, nil
}

func matchesTriggers(rb *models.Runbook, payload *models.WebhookPayload) bool {
	for _, trigger := range rb.Triggers {
		if trigger.EventType != payload.EventType {
			continue
		}
		allMatch := true
		for k, v := range trigger.Filters {
			if payload.Data[k] != v {
				allMatch = false
				break
			}
		}
		if allMatch {
			return true
		}
	}
	return false
}
