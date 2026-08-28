package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	alertv1 "github.com/Levango7/OpsMesh/services/alert-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/alert-svc/internal/engine"
	"github.com/Levango7/OpsMesh/services/alert-svc/internal/notify"
	"github.com/Levango7/OpsMesh/services/alert-svc/internal/store"
	"opsmesh/pkg/circuit"
	"opsmesh/pkg/metrics"
)

// Errors returned by the service.
var (
	ErrRuleNotFound  = errors.New("alert rule not found")
	ErrRuleInvalid   = errors.New("alert rule invalid")
	ErrAlertNotFound = errors.New("alert not found")
)

// Service implements the alert service business logic.
type Service struct {
	engine  *engine.Engine
	store   store.AlertStore
	notifier notify.Notifier
	breaker  *circuit.Breaker
}

// NewService creates a new Service.
func NewService(eng *engine.Engine, s store.AlertStore) *Service {
	return &Service{
		engine: eng,
		store:  s,
	}
}

// SetNotifier sets the notifier for the service (optional).
func (s *Service) SetNotifier(n notify.Notifier) {
	s.notifier = n
}

// SetCircuitBreaker sets the circuit breaker for notifications (optional).
func (s *Service) SetCircuitBreaker(cb *circuit.Breaker) {
	s.breaker = cb
}

// CreateRule creates a new alert rule.
func (s *Service) CreateRule(ctx context.Context, req *alertv1.CreateRuleRequest) (*alertv1.AlertRule, error) {
	if req.Rule == nil {
		return nil, ErrRuleInvalid
	}

	now := timestamppb.Now()
	rule := req.Rule
	if rule.Id == "" {
		rule.Id = uuid.New().String()
	}
	rule.CreatedAt = now
	rule.UpdatedAt = now
	if rule.Severity == "" {
		rule.Severity = "warning"
	}

	engineRule := protoToEngineRule(rule)
	if err := s.engine.AddRule(engineRule); err != nil {
		return nil, fmt.Errorf("failed to add rule to engine: %w", err)
	}

	storeRule := protoToStoreRule(rule)
	s.store.CreateAlertRule(storeRule)

	return rule, nil
}

// GetRule retrieves a rule by ID.
func (s *Service) GetRule(ctx context.Context, req *alertv1.GetRuleRequest) (*alertv1.AlertRule, error) {
	rule, err := s.engine.GetRule(req.Id)
	if err != nil {
		if errors.Is(err, engine.ErrRuleNotFound) {
			return nil, ErrRuleNotFound
		}
		return nil, err
	}
	return engineToProtoRule(rule), nil
}

// ListRules lists all rules.
func (s *Service) ListRules(ctx context.Context) (*alertv1.ListRulesResponse, error) {
	rules, err := s.engine.ListRules("")
	if err != nil {
		return nil, err
	}
	out := make([]*alertv1.AlertRule, 0, len(rules))
	for _, r := range rules {
		out = append(out, engineToProtoRule(r))
	}
	return &alertv1.ListRulesResponse{Rules: out}, nil
}

// UpdateRule updates an existing rule.
func (s *Service) UpdateRule(ctx context.Context, req *alertv1.UpdateRuleRequest) (*alertv1.AlertRule, error) {
	if req.Rule == nil {
		return nil, ErrRuleInvalid
	}

	now := timestamppb.Now()
	rule := req.Rule
	rule.UpdatedAt = now

	engineRule := protoToEngineRule(rule)
	if err := s.engine.UpdateRule(engineRule); err != nil {
		if errors.Is(err, engine.ErrRuleNotFound) {
			return nil, ErrRuleNotFound
		}
		return nil, fmt.Errorf("failed to update rule in engine: %w", err)
	}

	storeRule := protoToStoreRule(rule)
	s.store.UpdateAlertRule(storeRule)

	return rule, nil
}

// DeleteRule deletes a rule by ID.
func (s *Service) DeleteRule(ctx context.Context, req *alertv1.DeleteRuleRequest) error {
	if err := s.engine.DeleteRule(req.Id); err != nil {
		if errors.Is(err, engine.ErrRuleNotFound) {
			return ErrRuleNotFound
		}
		return err
	}
	s.store.DeleteAlertRule(req.Id)
	return nil
}

// Evaluate evaluates metrics against rules and returns triggered alerts.
func (s *Service) Evaluate(ctx context.Context, req *alertv1.EvaluateRequest) (*alertv1.EvaluateResponse, error) {
	events, err := s.engine.Evaluate(req.DeviceId)
	if err != nil {
		return nil, err
	}

	out := make([]*alertv1.Alert, 0, len(events))
	for _, ev := range events {
		alert := &alertv1.Alert{
			Id:       uuid.New().String(),
			TenantId: ev.TenantID,
			RuleId:   ev.RuleID,
			Severity: ev.Severity,
			Message:  ev.Message,
			Values:   ev.Values,
			Status:   "firing",
			FiredAt:  timestamppb.New(ev.FiredAt),
		}
		out = append(out, alert)

		s.store.AddAlert(&store.Alert{
			AlertID:  alert.Id,
			TenantID: ev.TenantID,
			DeviceID: ev.DeviceID,
			Severity: ev.Severity,
			Message:  ev.Message,
			Status:   "firing",
			Metric:   ev.RuleID,
			CreatedAt: ev.FiredAt,
		})

		if s.notifier != nil && s.notifier.IsEnabled() {
			details := make(map[string]interface{}, len(ev.Values))
			for k, v := range ev.Values {
				details[k] = v
			}
			if s.breaker != nil {
				err := s.breaker.Execute(func() error {
					return s.notifier.TriggerEvent(
						ev.DeviceID,
						ev.Message,
						ev.Severity,
						alert.Id,
						details,
					)
				})
				if err != nil {
					metrics.RecordBusinessMetric("alert_notification_failures", 1, map[string]string{"tenant_id": ev.TenantID})
				} else {
					metrics.RecordBusinessMetric("alert_notifications_total", 1, map[string]string{"tenant_id": ev.TenantID})
				}
			} else {
				_ = s.notifier.TriggerEvent(
					ev.DeviceID,
					ev.Message,
					ev.Severity,
					alert.Id,
					details,
				)
			}
		}
	}
	return &alertv1.EvaluateResponse{Alerts: out}, nil
}

// GetAlert retrieves an alert by ID.
func (s *Service) GetAlert(ctx context.Context, req *alertv1.GetAlertRequest) (*alertv1.Alert, error) {
	a := s.store.Alert(req.Id)
	if a == nil {
		return nil, ErrAlertNotFound
	}
	return storeToProtoAlert(a), nil
}

// ListAlerts lists alerts with optional filtering.
func (s *Service) ListAlerts(ctx context.Context, req *alertv1.ListAlertsRequest) (*alertv1.ListAlertsResponse, error) {
	alerts := s.store.Alerts(req.TenantId)
	out := make([]*alertv1.Alert, 0, len(alerts))
	for _, a := range alerts {
		if req.Status != "" && a.Status != req.Status {
			continue
		}
		out = append(out, storeToProtoAlert(a))
		if req.Limit > 0 && int32(len(out)) >= req.Limit {
			break
		}
	}
	return &alertv1.ListAlertsResponse{Alerts: out}, nil
}

// AckAlert acknowledges an alert.
func (s *Service) AckAlert(ctx context.Context, req *alertv1.AckAlertRequest) error {
	ok := s.store.AckAlert(req.Id, "", "system")
	if !ok {
		return ErrAlertNotFound
	}

	if s.notifier != nil && s.notifier.IsEnabled() {
		a := s.store.Alert(req.Id)
		source := ""
		if a != nil {
			source = a.DeviceID
		}
		if s.breaker != nil {
			_ = s.breaker.Execute(func() error {
				return s.notifier.AcknowledgeEvent(source, "alert acknowledged", req.Id, nil)
			})
		} else {
			_ = s.notifier.AcknowledgeEvent(source, "alert acknowledged", req.Id, nil)
		}
	}
	return nil
}

// ResolveAlert resolves an alert.
func (s *Service) ResolveAlert(ctx context.Context, req *alertv1.ResolveAlertRequest) error {
	ok := s.store.ResolveAlert(req.Id, "", "system")
	if !ok {
		return ErrAlertNotFound
	}

	if s.notifier != nil && s.notifier.IsEnabled() {
		a := s.store.Alert(req.Id)
		source := ""
		if a != nil {
			source = a.DeviceID
		}
		if s.breaker != nil {
			_ = s.breaker.Execute(func() error {
				return s.notifier.ResolveEvent(source, "alert resolved", req.Id, nil)
			})
		} else {
			_ = s.notifier.ResolveEvent(source, "alert resolved", req.Id, nil)
		}
	}
	return nil
}

// SilenceAlert silences an alert.
func (s *Service) SilenceAlert(ctx context.Context, req *alertv1.SilenceAlertRequest) error {
	until := time.Now().Add(time.Duration(req.DurationMinutes) * time.Minute)
	ok := s.store.SilenceAlert(req.Id, "", "system", until, req.Comment)
	if !ok {
		return ErrAlertNotFound
	}
	return nil
}

// Mapping functions

func protoToEngineRule(r *alertv1.AlertRule) *engine.AlertRule {
	return &engine.AlertRule{
		ID:             r.Id,
		Name:           r.Name,
		TenantID:       r.TenantId,
		Enabled:        r.Enabled,
		Conditions:     []engine.Condition{{Metric: r.Metric, Operator: r.Op, Threshold: r.Threshold}},
		Logic:          engine.LogicAnd,
		Duration:       time.Duration(r.Duration) * time.Second,
		Severity:       r.Severity,
		NotifyChannels: r.Channels,
	}
}

func engineToProtoRule(r *engine.AlertRule) *alertv1.AlertRule {
	out := &alertv1.AlertRule{
		Id:        r.ID,
		Name:      r.Name,
		TenantId:  r.TenantID,
		Enabled:   r.Enabled,
		Severity:  r.Severity,
		Channels:  r.NotifyChannels,
		Duration:  int32(r.Duration.Seconds()),
		CreatedAt: timestamppb.New(r.CreatedAt),
		UpdatedAt: timestamppb.New(r.UpdatedAt),
	}
	if len(r.Conditions) > 0 {
		out.Metric = r.Conditions[0].Metric
		out.Op = r.Conditions[0].Operator
		out.Threshold = r.Conditions[0].Threshold
	}
	return out
}

func protoToStoreRule(r *alertv1.AlertRule) *store.AlertRule {
	return &store.AlertRule{
		ID:          r.Id,
		TenantID:    r.TenantId,
		Metric:      r.Metric,
		Op:          r.Op,
		Threshold:   r.Threshold,
		ForDuration: int(r.Duration),
		Severity:    r.Severity,
		Enabled:     r.Enabled,
		CreatedAt:   r.CreatedAt.AsTime(),
	}
}

func storeToProtoAlert(a *store.Alert) *alertv1.Alert {
	return &alertv1.Alert{
		Id:        a.AlertID,
		TenantId:  a.TenantID,
		Severity:  a.Severity,
		Message:   a.Message,
		Status:    a.Status,
		FiredAt:   timestamppb.New(a.CreatedAt),
		UpdatedAt: timestamppb.New(a.UpdatedAt),
	}
}
