// notify_templates.go M2 通知模板 API（NotifyTemplate CRUD）+ 告警引擎辅助函数。
package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/alertengine"
	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/notify"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

// handleNotifyTemplates 处理 /api/v1/notify-templates：GET 列表 / POST 创建。
func (s *Server) handleNotifyTemplates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listNotifyTemplates(w, r)
	case http.MethodPost:
		s.createNotifyTemplate(w, r)
	default:
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// listNotifyTemplates 返回当前租户的通知模板列表。
func (s *Server) listNotifyTemplates(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:read"); !ok {
		return
	}
	templates := s.store.ListNotifyTemplates(actx.TenantID)
	paginate.WriteJSON(w, http.StatusOK, templates)
}

// createNotifyTemplate 创建一条通知模板。
func (s *Server) createNotifyTemplate(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:write"); !ok {
		return
	}
	var t store.NotifyTemplate
	if err := decodeJSONBody(w, r, &t); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	t.TenantID = actx.TenantID
	created := s.store.CreateNotifyTemplate(&t)
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "create_notify_template", Target: created.ID,
		Detail: sanitizeAuditDetail(fmt.Sprintf("name=%s type=%s format=%s", created.Name, created.Type, created.Format)),
	})
	paginate.WriteJSON(w, http.StatusCreated, created)
}

// handleNotifyTemplateRouting 分派 /api/v1/notify-templates/{id} 子路径：
//   - PUT    /api/v1/notify-templates/{id} — 更新模板
//   - DELETE /api/v1/notify-templates/{id} — 删除模板
func (s *Server) handleNotifyTemplateRouting(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/notify-templates/")
	if id == "" {
		paginate.JSONError(w, http.StatusBadRequest, "template id required")
		return
	}
	switch r.Method {
	case http.MethodPut:
		s.updateNotifyTemplate(w, r, id)
	case http.MethodDelete:
		s.deleteNotifyTemplate(w, r, id)
	default:
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// updateNotifyTemplate 更新一条通知模板。
func (s *Server) updateNotifyTemplate(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:write"); !ok {
		return
	}
	var t store.NotifyTemplate
	if err := decodeJSONBody(w, r, &t); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	t.ID = id
	t.TenantID = actx.TenantID
	if !s.store.UpdateNotifyTemplate(&t) {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "update_notify_template", Target: id,
	})
	paginate.WriteJSON(w, http.StatusOK, t)
}

// deleteNotifyTemplate 删除一条通知模板。
func (s *Server) deleteNotifyTemplate(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:write"); !ok {
		return
	}
	if !s.store.DeleteNotifyTemplate(id, actx.TenantID) {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found or tenant mismatch"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "delete_notify_template", Target: id,
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted", "id": id})
}

// ============================================================================
// 告警引擎辅助函数（异常检测 / 聚合推送 / 事件转换 / ID 生成 / 脱敏）
// ============================================================================

// evaluateAnomalyForDevice 异常检测：对单台设备的指标调用 anomalyEngine.Evaluate。
//
// 从 store 取设备最新指标（DeviceMetrics），提取 cpu_usage/mem_usage 等指标值，
// 调用 anomalyEngine.Evaluate 评估，异常时将 AnomalyAlert 转换为 AlertEvent 返回。
//
// 转换语义：
//   - RuleID：anomalyAlert.RuleID
//   - Severity：anomalyAlert.Severity
//   - Message：包含指标名、值、Z-Score 的人读消息
//   - Labels：ruleID/deviceID/severity/metric（metric 供 AlertInhibitor 标签匹配）
//   - Values：{metricName: value}
//
// 无指标数据（agent 未上报）时返回空切片（跳过该设备）。
func (s *Server) evaluateAnomalyForDevice(deviceID string) []*alertengine.AlertEvent {
	metrics := s.store.DeviceMetrics(deviceID)
	if metrics == nil {
		return nil
	}
	// 提取指标名→值映射。当前覆盖 cpu_usage/mem_usage；后续可扩展 disk/net 等。
	metricValues := map[string]float64{
		"cpu_usage": metrics.CPU.Usage,
		"mem_usage": metrics.Memory.Usage,
	}
	var events []*alertengine.AlertEvent
	for metricName, value := range metricValues {
		alert := s.anomalyEngine.Evaluate(metricName, deviceID, value)
		if alert == nil {
			continue
		}
		ev := &alertengine.AlertEvent{
			RuleID:   alert.RuleID,
			TenantID: "default",
			DeviceID: deviceID,
			Severity: alert.Severity,
			Message: fmt.Sprintf("异常检测触发：指标 %s=%.2f 偏离基线（均值=%.2f, 标准差=%.2f, Z-Score=%.2f）",
				alert.MetricName, alert.Value, alert.Mean, alert.StdDev, alert.ZScore),
			Labels: map[string]string{
				"ruleID":   alert.RuleID,
				"deviceID": deviceID,
				"severity": alert.Severity,
				"metric":   alert.MetricName,
				"tenantID": "default",
			},
			FiredAt: alert.Timestamp,
			Values:  map[string]float64{alert.MetricName: alert.Value},
		}
		events = append(events, ev)
	}
	return events
}

// notifyAlertGroup 推送一个聚合告警组：构造 Message → Notifier.Notify + 写入 store.AddAlert。
func (s *Server) notifyAlertGroup(ctx context.Context, g *alertengine.AlertGroup) {
	if g == nil || len(g.Events) == 0 {
		return
	}
	// 构造消息正文（聚合组内事件摘要）
	var sb strings.Builder
	fmt.Fprintf(&sb, "告警组 %s（共 %d 条）：\n", g.Key, len(g.Events))
	for i, ev := range g.Events {
		if i >= 10 {
			fmt.Fprintf(&sb, "\n... 还有 %d 条", len(g.Events)-10)
			break
		}
		fmt.Fprintf(&sb, "\n- [%s] %s（设备 %s，规则 %s）", ev.Severity, ev.Message, ev.DeviceID, ev.RuleID)
	}
	// 取组内最高严重度作为消息 Severity
	severity := "info"
	for _, ev := range g.Events {
		if ev.Severity == "critical" {
			severity = "critical"
			break
		}
		if ev.Severity == "warning" && severity != "critical" {
			severity = "warning"
		}
	}
	msg := &notify.Message{
		Title:     fmt.Sprintf("[OpsMesh][%s] 告警聚合 %s", severity, g.Key),
		Body:      sb.String(),
		Format:    "markdown",
		Severity:  severity,
		Source:    "alert-engine",
		Timestamp: time.Now(),
	}
	// 通过 Notifier 推送（多渠道 + 去重 + 重试）
	if err := s.alertNotifier.Notify(msg); err != nil {
		logx.Warn(ctx, "告警聚合组推送失败", err, "groupKey", g.Key)
	}
	// 写入 store 使其在 /api/v1/alerts 列表可见
	for _, ev := range g.Events {
		alert := alertEventToAlert(ev)
		s.store.AddAlert(alert)
		// 告警抑制：跟踪活跃告警（供后续抑制判定）。
		// alertInhibitor 为 nil 时跳过（向后兼容）。
		// 仅对未被抑制的 firing 告警跟踪（被抑制的告警在 evaluateAlertsOnce 中已被过滤，不会到达此处）。
		if s.alertInhibitor != nil {
			s.alertInhibitor.TrackActive(alert)
		}
		// 发布 SSE 事件通知前端
		s.publishEvent(ctx, "alert_new", ev.TenantID, map[string]string{
			"alertID":  alert.AlertID,
			"ruleID":   ev.RuleID,
			"deviceID": ev.DeviceID,
			"severity": ev.Severity,
		})
	}
}

// alertEventToAlert 将 alertengine.AlertEvent 转换为 proto.Alert。
//
// 用于：
//   - AlertInhibitor.IsInhibited 检查（需要 proto.Alert 提取标签）。
//   - AlertInhibitor.TrackActive 跟踪活跃告警。
//   - 写入 store.AddAlert 使告警在 /api/v1/alerts 列表可见。
//
// Metric 从 ev.Labels["metric"] 提取（若规则 buildEvent 时注入了 metric 标签）；
// 未注入时 Metric 为空，AlertInhibitor 的 alertLabels 会得到空 metric（不影响 device_id/severity/status 匹配）。
// AlertID 拼接规则与原 notifyAlertGroup 内联构造一致，保持向后兼容。
func alertEventToAlert(ev *alertengine.AlertEvent) *proto.Alert {
	if ev == nil {
		return nil
	}
	return &proto.Alert{
		AlertID:   "alert-eng-" + ev.RuleID + "-" + ev.DeviceID + "-" + ev.FiredAt.Format("20060102150405"),
		TenantID:  ev.TenantID,
		DeviceID:  ev.DeviceID,
		AgentID:   "",
		Severity:  ev.Severity,
		Message:   ev.Message,
		Metric:    ev.Labels["metric"], // 从 Labels 提取 metric（若有），否则为空
		Status:    proto.AlertStatusFiring,
		CreatedAt: ev.FiredAt,
	}
}

// randHex 生成 n 字节随机十六进制串（用于 ID 生成）。
func randHex(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(time.Now().UnixNano() >> uint(i*8))
	}
	return fmt.Sprintf("%x", b)
}

// maskSensitiveConfig 脱敏渠道 Config JSON：将敏感字段（webhook/secret/password/token）替换为 ***。
// 非合法 JSON 原样返回（向后兼容）。
func maskSensitiveConfig(configJSON string) string {
	if configJSON == "" {
		return ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(configJSON), &m); err != nil {
		return configJSON // 非 JSON 原样返回
	}
	sensitiveKeys := []string{"secret", "password", "token", "pass", "apiKey", "api_key"}
	for k := range m {
		lk := strings.ToLower(k)
		for _, sk := range sensitiveKeys {
			if strings.Contains(lk, sk) {
				m[k] = "***"
				break
			}
		}
	}
	out, err := json.Marshal(m)
	if err != nil {
		return configJSON
	}
	return string(out)
}
