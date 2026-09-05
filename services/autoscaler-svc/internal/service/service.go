package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/evaluator"
	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/k8s"
	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/models"
)

// Errors returned by the service.
var (
	ErrRuleNotFound = fmt.Errorf("scaling rule not found")
	ErrRuleInvalid  = fmt.Errorf("scaling rule invalid")
)

// Service implements the autoscaler business logic.
type Service struct {
	evaluator *evaluator.Evaluator
	reader    evaluator.MetricsReader
	scaler    *k8s.Client
}

// NewService creates a new Service.
func NewService(eng *evaluator.Evaluator, reader evaluator.MetricsReader, scaler *k8s.Client) *Service {
	return &Service{
		evaluator: eng,
		reader:    reader,
		scaler:    scaler,
	}
}

// CreateRule creates a new scaling rule.
func (s *Service) CreateRule(ctx context.Context, rule *models.ScaleRule) (*models.ScaleRule, error) {
	if rule == nil {
		return nil, ErrRuleInvalid
	}
	if rule.ID == "" {
		rule.ID = uuid.New().String()
	}
	if rule.ScaleUpThreshold <= 0 {
		return nil, fmt.Errorf("%w: scaleUpThreshold must be positive", ErrRuleInvalid)
	}
	if rule.MaxReplicas <= 0 {
		rule.MaxReplicas = 10
	}
	if rule.MinReplicas < 0 {
		rule.MinReplicas = 1
	}
	now := time.Now()
	rule.CreatedAt = now
	rule.UpdatedAt = now
	if rule.CooldownUp == 0 {
		rule.CooldownUp = 60 * time.Second
	}
	if rule.CooldownDown == 0 {
		rule.CooldownDown = 300 * time.Second
	}

	if err := s.evaluator.AddRule(rule); err != nil {
		return nil, fmt.Errorf("failed to add rule: %w", err)
	}
	return rule, nil
}

// GetRule retrieves a rule by ID.
func (s *Service) GetRule(ctx context.Context, id string) (*models.ScaleRule, error) {
	rule, err := s.evaluator.GetRule(id)
	if err != nil {
		return nil, ErrRuleNotFound
	}
	return rule, nil
}

// ListRules lists all rules.
func (s *Service) ListRules(ctx context.Context) ([]*models.ScaleRule, error) {
	return s.evaluator.ListRules(), nil
}

// UpdateRule updates an existing rule.
func (s *Service) UpdateRule(ctx context.Context, rule *models.ScaleRule) (*models.ScaleRule, error) {
	if rule == nil || rule.ID == "" {
		return nil, ErrRuleInvalid
	}
	rule.UpdatedAt = time.Now()
	if err := s.evaluator.UpdateRule(rule); err != nil {
		if err.Error() == fmt.Sprintf("rule not found: %s", rule.ID) {
			return nil, ErrRuleNotFound
		}
		return nil, fmt.Errorf("failed to update rule: %w", err)
	}
	return rule, nil
}

// DeleteRule deletes a rule by ID.
func (s *Service) DeleteRule(ctx context.Context, id string) error {
	if err := s.evaluator.DeleteRule(id); err != nil {
		if err.Error() == fmt.Sprintf("rule not found: %s", id) {
			return ErrRuleNotFound
		}
		return err
	}
	return nil
}

// Evaluate triggers evaluation of scaling rules.
func (s *Service) Evaluate(ctx context.Context, ruleID string) (*models.EvaluateResponse, error) {
	decisions, err := s.evaluator.Evaluate(s.reader, s.scaler, ruleID)
	if err != nil {
		return nil, fmt.Errorf("evaluation failed: %w", err)
	}

	out := make([]models.ScaleDecision, 0, len(decisions))
	for _, d := range decisions {
		d.ID = uuid.New().String()
		out = append(out, *d)
	}

	return &models.EvaluateResponse{
		Decisions: out,
		Timestamp: time.Now(),
	}, nil
}

// GetDecisions returns the scaling decision history.
func (s *Service) GetDecisions(ctx context.Context) []*models.ScaleDecision {
	return s.evaluator.Decisions()
}

// ScaleRequest is the body for manual scale (frontend contract: {target,replicas,reason},
// where target is in the form "namespace/deployment" or "deployment", the default namespace is default).
type ScaleRequest struct {
	Target   string `json:"target"`
	Replicas int32  `json:"replicas"`
	Reason   string `json:"reason"`
}

// ScaleResponse is the reply for manual scale (frontend contract: {status,message}).
type ScaleResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// parseScaleTarget parses target in the form "namespace/deployment" or bare "deployment",
// missing namespace falls back to default.
func parseScaleTarget(target string) (deployment, namespace string) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", ""
	}
	if i := strings.IndexByte(target, '/'); i >= 0 {
		ns, dep := target[:i], target[i+1:]
		if ns == "" {
			ns = "default"
		}
		return dep, ns
	}
	return target, "default"
}

// Scale manually sets the target deployment's replica count to the specified value
// and records a manual decision (backend contract: POST /api/v1/scale).
func (s *Service) Scale(ctx context.Context, req *ScaleRequest) (*ScaleResponse, error) {
	if req == nil {
		return nil, ErrRuleInvalid
	}
	deployment, namespace := parseScaleTarget(req.Target)
	if deployment == "" {
		return nil, fmt.Errorf("%w: target is required", ErrRuleInvalid)
	}
	if req.Replicas < 0 {
		return nil, fmt.Errorf("%w: replicas cannot be negative", ErrRuleInvalid)
	}

	fromReplicas, err := s.scaler.GetReplicas(deployment, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get replicas: %w", err)
	}
	if err := s.scaler.SetReplicas(deployment, namespace, req.Replicas); err != nil {
		return nil, fmt.Errorf("failed to set replicas: %w", err)
	}

	s.evaluator.RecordDecision(&models.ScaleDecision{
		ID:           uuid.New().String(),
		RuleID:       "",
		Deployment:   deployment,
		Namespace:    namespace,
		Action:       "manual",
		FromReplicas: fromReplicas,
		ToReplicas:   req.Replicas,
		Reason:       req.Reason,
		Timestamp:    time.Now(),
	})

	return &ScaleResponse{
		Status:  "ok",
		Message: fmt.Sprintf("scaled %s/%s to %d replicas", namespace, deployment, req.Replicas),
	}, nil
}

// CooldownStatus is an alias of evaluator.CooldownStatus exported for handler consumption.
type CooldownStatus = evaluator.CooldownStatus

// GetCooldowns returns per-rule cooldown remaining seconds (frontend contract:
// GET /api/v1/cooldowns → {cooldowns:[{ruleId,ruleName,remaining,expiresAt}]}).
func (s *Service) GetCooldowns(ctx context.Context) []CooldownStatus {
	return s.evaluator.Cooldowns()
}
