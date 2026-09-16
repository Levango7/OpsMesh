// server_alerts_m2.go M2 集成：告警评估循环（alertengine.Engine + Silencer + Aggregator + Notifier）。
//
// 与 server_alerts.go 的关系：
//   - server_alerts.go 实现原有 /api/v1/alerts（列表/ack/silence）+ 旧版 alert-rules CRUD（globalAlertRules）；
//   - 本文件族实现 M2 新增 API（alert-rules 用 alertengine.AlertRule 多条件 + 静默/渠道/模板 CRUD）；
//   - 不破坏现有功能：原有 /api/v1/alerts 与旧 alert-rules 保持不变。
//
// 本文件保留告警评估主循环（alertEngineLoop + evaluateAlertsOnce）。
// CRUD handler 拆分至：
//   - alert_rules_m2.go：告警规则 CRUD；
//   - alert_silences.go：静默规则 CRUD；
//   - notify_channels.go：通知渠道 CRUD + 测试；
//   - notify_templates.go：通知模板 CRUD + 引擎辅助函数。
//
// API 路由（在 server.go 注册）：
//   - GET/POST/PUT/DELETE /api/v1/alert-rules[/id]      — alertengine.AlertRule 多条件规则
//   - GET/POST          /api/v1/alert-silences[/id]     — 静默规则
//   - GET/POST/PUT/DELETE /api/v1/notify-channels[/id] — 通知渠道
//   - POST              /api/v1/notify-channels/{id}/test — 测试发送
//   - GET/POST/PUT      /api/v1/notify-templates[/id]  — 通知模板
//
// 告警评估循环 alertEngineLoop：
//   - 周期遍历所有设备，调用 alertEngine.Evaluate 触发告警事件；
//   - 经 alertSilencer.IsSilenced 过滤被抑制的事件；
//   - 经 alertAggregator.Aggregate 聚合；
//   - 通过 alertNotifier.Notify 推送到所有已配置渠道。
package controlplane

import (
	"context"
	"fmt"
	"time"

	"github.com/Levango7/OpsMesh/internal/alertengine"
	"github.com/Levango7/OpsMesh/internal/events"
	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// ============================================================================
// M2 告警评估循环（alertengine.Engine + Silencer + Aggregator + Notifier）
// ============================================================================

// alertEngineLoop M2 告警评估循环：周期遍历所有设备，评估告警规则，
// 经静默过滤 + 聚合后通过 Notifier 推送。
//
// 流程：
//  1. 取所有设备快照（store.Snapshot）；
//  2. 对每台设备调用 alertEngine.Evaluate 触发告警事件；
//  3. 经 alertSilencer.IsSilenced 过滤被抑制的事件；
//  4. 经 alertAggregator.Aggregate 聚合（按 deviceID + severity 分组）；
//  5. 对每个聚合组构造 notify.Message 并通过 alertNotifier.Notify 推送；
//  6. 同时写入 store.AddAlert 使其在 /api/v1/alerts 列表可见。
//
// 启动条件：alertEngine 非 nil（NewServer 总是构造）；无规则时 Evaluate 返回空切片，零开销。
// 退出机制：select 监听 ctx.Done() 与 ticker.C，ctx 取消时优雅退出。
func (s *Server) alertEngineLoop(ctx context.Context) {
	if s.alertEngine == nil {
		return
	}
	ticker := time.NewTicker(30 * time.Second) // 30s 评估周期
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.evaluateAlertsOnce(ctx)
		}
	}
}

// evaluateAlertsOnce 执行一次告警评估（供 alertEngineLoop 周期调用与测试直接调用）。
func (s *Server) evaluateAlertsOnce(ctx context.Context) {
	// 取所有设备快照（空租户=全部）
	snapshot := s.store.Snapshot("")
	var allEvents []*alertengine.AlertEvent
	for segment, devices := range snapshot {
		_ = segment
		for _, d := range devices {
			if d.Retired {
				continue
			}
			events, err := s.alertEngine.Evaluate(d.DeviceID)
			if err != nil {
				logx.Warn(ctx, "告警评估失败", err, "deviceID", d.DeviceID)
				continue
			}
			allEvents = append(allEvents, events...)
			// 异常检测：anomalyEngine 非 nil 时，对设备指标调用 Evaluate。
			// 异常时产生 AnomalyAlert 并转换为 AlertEvent 加入 allEvents，
			// 经后续静默/聚合/通知链统一处理（与规则告警一致流程）。
			if s.anomalyEngine != nil {
				anomalyEvents := s.evaluateAnomalyForDevice(d.DeviceID)
				allEvents = append(allEvents, anomalyEvents...)
			}
		}
	}
	if len(allEvents) == 0 {
		return
	}
	// 静默过滤
	var filtered []*alertengine.AlertEvent
	for _, ev := range allEvents {
		if !s.alertSilencer.IsSilenced(ev) {
			filtered = append(filtered, ev)
		}
	}
	if len(filtered) == 0 {
		return
	}
	// 告警抑制：alertInhibitor 非 nil 时，对每个事件构造 proto.Alert 检查是否被抑制。
	// 被抑制的事件跳过通知但记录审计 + 写入 store（仍可在 /api/v1/alerts 列表可见，只是不触发通知）。
	// 未被抑制的事件进入聚合/通知流程，并在 notifyAlertGroup 中调用 TrackActive 跟踪活跃告警。
	// alertInhibitor 为 nil 时跳过抑制检查（向后兼容）。
	if s.alertInhibitor != nil {
		var notInhibited []*alertengine.AlertEvent
		for _, ev := range filtered {
			alert := alertEventToAlert(ev)
			if s.alertInhibitor.IsInhibited(alert) {
				// 被抑制的告警仍记录到 store，只是不触发通知
				s.store.AddAlert(alert)
				// 记录审计：告警被抑制（便于运维溯源为何告警未通知）
				s.audit(ctx, &proto.AuditEvent{
					TenantID: ev.TenantID, Action: "alert_inhibited", Target: alert.AlertID,
					Detail: sanitizeAuditDetail(fmt.Sprintf("ruleID=%s deviceID=%s severity=%s message=%s", ev.RuleID, ev.DeviceID, ev.Severity, ev.Message)),
				})
				logx.Info(ctx, "告警被抑制，跳过通知", "alertID", alert.AlertID, "ruleID", ev.RuleID, "deviceID", ev.DeviceID)
				continue
			}
			notInhibited = append(notInhibited, ev)
		}
		filtered = notInhibited
	}
	if len(filtered) == 0 {
		return
	}
	// 聚合
	groups := s.alertAggregator.Aggregate(filtered)
	// 推送每个聚合组
	for _, g := range groups {
		s.notifyAlertGroup(ctx, g)
	}
}

// _ 确保 events 包被使用（publishEvent 内部引用 events.Event）。
var _ = events.LevelInfo
