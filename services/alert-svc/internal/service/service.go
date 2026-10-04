package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Levango7/OpsMesh/pkg/circuit"
	"github.com/Levango7/OpsMesh/pkg/metrics"
	alertv1 "github.com/Levango7/OpsMesh/services/alert-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/alert-svc/internal/engine"
	"github.com/Levango7/OpsMesh/services/alert-svc/internal/notify"
	"github.com/Levango7/OpsMesh/services/alert-svc/internal/store"
)

// Errors returned by the service.
var (
	ErrRuleNotFound  = errors.New("alert rule not found")
	ErrRuleInvalid   = errors.New("alert rule invalid")
	ErrAlertNotFound = errors.New("alert not found")
)

// externalNotifyFailureMetric 记 ack/resolve 侧"本地已落库、外部通知失败"的累计次数。
// 与触发侧已有的 alert_notifications / alert_notification_failures 不重复计：那两个
// 只覆盖 TriggerEvent，本指标补的正是此前完全无人记录的两条 ack/resolve 路径。
// action 标签只取 "ack"/"resolve" 两个字面量（本文件控制），不会引入无界基数。
const externalNotifyFailureMetric = "alert_external_notify_failures"

// Service implements the alert service business logic.
type Service struct {
	engine   *engine.Engine
	store    store.AlertStore
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

// recordExternalNotifyFailure 把 ack/resolve 的外部通知失败变成可见的信号。
//
// 缺陷背景：调用点原先写成 `_ = s.breaker.Execute(...)`，本地状态已经改成功、请求返回
// OK，而 PagerDuty 侧根本没收到——运维据此以为 on-call 已被通知，比直接报错更危险。
//
// 刻意不把 err 返回给调用方：ack/resolve 的事实来源是本地告警状态，让远端故障把它
// 升级成 5xx 会诱导操作者反复点击，且熔断器打开期间会彻底无法确认告警。
//
// 日志走标准库 log（服务名 [alert-svc] 前缀）：cmd/alert-svc/main.go 的
// applog.Init 已把标准库 logger 接管成 JSON，因此不 import 根的 internal/logx
// ——微服务只依赖 pkg/*，这条边界一旦破，服务镜像就会跟着控制面的内部包一起变。
func (s *Service) recordExternalNotifyFailure(action, alertID string, err error) {
	// 措辞按动作区分：resolve 记成"确认"会把排障线索指错方向。
	verb := "解决"
	if action == "ack" {
		verb = "确认"
	}
	log.Printf("[alert-svc] WARN 告警%s已落库但外部通知失败: action=%s alertID=%s err=%v", verb, action, alertID, err)
	metrics.AddBusinessMetric(externalNotifyFailureMetric, 1, map[string]string{"action": action})
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

// Evaluate 用请求携带的读数评估规则，返回触发的告警**与评估面本身的状态**。
//
// 为什么响应里不只有 alerts（#60）：修前引擎根本不读指标（`operator==">" && threshold<100`
// 就算命中），空 alerts 既可能是"读数都正常"也可能是"你根本没给读数"。
// 这两种情况对客户是两个事实，混在一起就等于把"监控没数据"卖成"系统健康"。
func (s *Service) Evaluate(ctx context.Context, req *alertv1.EvaluateRequest) (*alertv1.EvaluateResponse, error) {
	report, err := s.engine.Evaluate(req.GetTenantId(), req.GetDeviceId(), req.GetMetrics())
	if err != nil {
		return nil, err
	}
	if len(req.GetMetrics()) == 0 && len(report.Evaluations) > 0 {
		log.Printf("[alert-svc] 评估未携带任何读数：device=%s tenant=%s 规则=%d 条，全部按 no_data 处理",
			req.GetDeviceId(), req.GetTenantId(), len(report.Evaluations))
	}

	out := make([]*alertv1.Alert, 0, len(report.Events))
	for _, ev := range report.Events {
		alert := &alertv1.Alert{
			Id:       uuid.New().String(),
			TenantId: ev.TenantID,
			RuleId:   ev.RuleID,
			Severity: ev.Severity,
			Message:  ev.Message,
			Values:   ev.Values,
			Status:   "firing",
			FiredAt:  timestamppb.New(ev.FiredAt),
			DeviceId: ev.DeviceID,
			Metric:   ev.Metric,
		}
		out = append(out, alert)

		s.store.AddAlert(&store.Alert{
			AlertID:   alert.Id,
			TenantID:  ev.TenantID,
			DeviceID:  ev.DeviceID,
			Severity:  ev.Severity,
			Message:   ev.Message,
			Status:    "firing",
			Metric:    ev.Metric,
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
					metrics.AddBusinessMetric("alert_notification_failures", 1, map[string]string{"tenant_id": ev.TenantID})
				} else {
					metrics.AddBusinessMetric("alert_notifications", 1, map[string]string{"tenant_id": ev.TenantID})
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
	return &alertv1.EvaluateResponse{
		Alerts:          out,
		EvaluatedRules:  int32(report.Evaluated()),
		PendingRules:    report.IDs(engine.StatePending),
		NoDataRules:     report.IDs(engine.StateNoData),
		InvalidRules:    report.IDs(engine.StateInvalid),
		MetricsSupplied: int32(len(req.GetMetrics())),
	}, nil
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
			if err := s.breaker.Execute(func() error {
				return s.notifier.AcknowledgeEvent(source, "alert acknowledged", req.Id, nil)
			}); err != nil {
				s.recordExternalNotifyFailure("ack", req.Id, err)
			}
		} else if err := s.notifier.AcknowledgeEvent(source, "alert acknowledged", req.Id, nil); err != nil {
			s.recordExternalNotifyFailure("ack", req.Id, err)
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
			if err := s.breaker.Execute(func() error {
				return s.notifier.ResolveEvent(source, "alert resolved", req.Id, nil)
			}); err != nil {
				s.recordExternalNotifyFailure("resolve", req.Id, err)
			}
		} else if err := s.notifier.ResolveEvent(source, "alert resolved", req.Id, nil); err != nil {
			s.recordExternalNotifyFailure("resolve", req.Id, err)
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
		DeviceId:  a.DeviceID,
		// ⚠️ RuleId 在这里只能留空：alert-svc 自己的 alerts 表（internal/store/mysql.go
		// 的 CREATE TABLE）没有 rule_id 列，store.Alert 也没有该字段。
		// 修前这里把 Metric 当 RuleID 用，于是"按指标查告警"整条口径都是错的；
		// 现在 Metric 存回真指标，rule_id 的缺口就显式留在这里（补列属 schema 迁移，
		// 已登记为待办，不用错字段糊过去）。
		Metric: a.Metric,
	}
}
