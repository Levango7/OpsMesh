// notify_templates.go M2 通知模板 API（NotifyTemplate CRUD）+ 告警引擎辅助函数。
package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/internal/aiops"
	"github.com/Levango7/OpsMesh/internal/alertengine"
	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/logx"
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
	if created == nil {
		// 同 createNotifyChannel：落库失败返回 nil 时不得裸用 created.ID
		// （nil 解引用 panic 掐断连接，客户端连 500 都收不到）。
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "create notify template failed"})
		return
	}
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

// notifyAlertGroup 落一个聚合告警组：逐事件写入 store.AddAlert（+ 抑制跟踪 + SSE）。
//
// TD-92 收口：**不再直接调 alertNotifier.Notify**。此前这段"构造 Message → Notify"
// 在 Notifier 无渠道时静默丢弃（TD-92 根因），且与真实外发路径重复——同一批事件经
// store.AddAlert 落库后，由 M7 notifyLoop 统一外发（CLI 单通道 + 文件多渠道，
// 见 deliverAlert）。两条路径同时发会造成重复投递，故自动告警外发只保留
// notifyLoop 这一个发送点；聚合组消息随之删除（它只为那条死调用服务）。
func (s *Server) notifyAlertGroup(ctx context.Context, g *alertengine.AlertGroup) {
	if g == nil || len(g.Events) == 0 {
		return
	}
	// 写入 store 使其在 /api/v1/alerts 列表可见，并经 notifyLoop 外发（deliverAlert）
	for _, ev := range g.Events {
		alert := alertEventToAlert(ev)
		s.store.AddAlert(alert)
		// 按规则选渠道（TD-92 第二批）：规则显式配了 NotifyChannels 时，把选择意图
		// 按 AlertID 登记（deliverAlert 据此过滤 UI 渠道）。proto.Alert 没有 RuleID
		// 字段，选择意图只能随告警 ID 走内存；空/未配 = 不过滤（送全部 enabled 渠道）。
		if s.alertEngine != nil && ev.RuleID != "" {
			if rule, err := s.alertEngine.GetRule(ev.RuleID); err == nil && rule != nil && len(rule.NotifyChannels) > 0 {
				s.noteAlertChannelSelection(alert.AlertID, rule.NotifyChannels)
			}
		}
		// 告警抑制：跟踪活跃告警（供后续抑制判定）。
		// alertInhibitor 为 nil 时跳过（向后兼容）。
		// 仅对未被抑制的 firing 告警跟踪（被抑制的告警在 evaluateAlertsOnce 中已被过滤，不会到达此处）。
		if s.alertInhibitor != nil {
			s.alertInhibitor.TrackActive(alert)
		}

		// 自动调优：基础设施预警触发且 autoRemediateEnabled 为 true 时，
		// 自动下发对应 OS 优化模板任务到受影响设备（一键自愈）。
		if s.autoRemediateEnabled {
			s.autoRemediateOnAlert(ctx, ev.RuleID, ev.DeviceID)
		}

		// 发布 SSE 事件通知前端；若 AIOps 启用，先做根因分析并附带结果
		rootCauseJSON := ""
		rootCauseEvidence := ""
		if s.aiopsEngine != nil && s.aiopsEngine.IsEnabled() {
			// 构造最小告警上下文（alert + 设备指标），用于 AIOps 根因分析
			alertCtx := &aiops.AlertContext{
				Alert: alert,
			}
			if dm := s.store.DeviceMetrics(ev.DeviceID); dm != nil {
				alertCtx.Metrics = &aiops.DeviceContext{
					DeviceInfo: &proto.DeviceInfo{
						DeviceID: dm.DeviceID,
						Hostname: dm.Hostname,
						OS:       dm.OS,
						Arch:     dm.Arch,
						State:    "online",
					},
					Metrics: &proto.DeviceMetrics{
						CPU:          dm.CPU,
						Memory:       dm.Memory,
						Disks:        dm.Disks,
						Network:      dm.Network,
						ProcessCount: dm.ProcessCount,
						Uptime:       dm.Uptime,
					},
					Uptime: dm.Uptime,
				}
			}
			if rc, err := s.aiopsEngine.Analyze(ctx, alertCtx); err == nil {
				rootCauseJSON = fmt.Sprintf(`{"category":"%s","component":"%s","description":"%s","confidence":%.2f}`,
					rc.Category, rc.Component, rc.Description, rc.Confidence)
				rootCauseEvidence = strings.Join(rc.Evidence, " || ")
			}
			// AIOps 分析失败不阻断告警处理，仅跳过附带根因
		}
		s.publishEvent(ctx, "alert_new", ev.TenantID, map[string]string{
			"alertID":           alert.AlertID,
			"ruleID":            ev.RuleID,
			"deviceID":          ev.DeviceID,
			"severity":          ev.Severity,
			"rootCause":         rootCauseJSON,
			"rootCauseEvidence": rootCauseEvidence,
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

// autoRemediateTemplate 规则 ID → OS 优化模板 ID 映射（基础设施预警自愈）。
var autoRemediateTemplate = map[string]string{
	"infra-cpu-high":  "kernel-tune", // CPU 过高 → 内核参数调优（somaxconn/backlog/keepalive 等）
	"infra-mem-high":  "kernel-tune", // 内存过高 → 内核参数调优（vm.swappiness 等）
	"infra-disk-high": "",            // 磁盘过高：无直接对应模板，跳过（建议人工清理）
}

// autoRemediateOnAlert 基础设施预警触发时自动下发 OS 优化模板任务（一键自愈）。
// 仅当 s.autoRemediateEnabled 为 true 且规则有对应模板时才下发；下发失败不阻断告警流程。
func (s *Server) autoRemediateOnAlert(ctx context.Context, ruleID, deviceID string) {
	tplID, ok := autoRemediateTemplate[ruleID]
	if !ok || tplID == "" {
		return // 无对应模板，跳过
	}
	if s.store == nil {
		return
	}

	// 查找设备的 agentID 和 租户 ID（从所有 segment 中匹配 deviceID）。
	var agentID, tenantID string
	for _, devices := range s.store.Snapshot("") {
		for _, d := range devices {
			if d.DeviceID == deviceID && d.AgentID != "" {
				agentID = d.AgentID
				tenantID = d.TenantID
				if tenantID == "" {
					tenantID = "default"
				}
				break
			}
		}
		if agentID != "" {
			break
		}
	}
	if agentID == "" {
		logx.Warn(ctx, "自动调优：未找到设备 agentID，跳过下发", "deviceID", deviceID, "ruleID", ruleID)
		return
	}

	// 获取模板内容。
	tpl := s.getOSTemplateByID(tplID)
	if tpl == nil {
		logx.Warn(ctx, "自动调优：模板不存在，跳过下发", "templateID", tplID, "deviceID", deviceID)
		return
	}

	// 下发 shell 任务（模板 Commands 作为命令）。
	task := &proto.Task{
		AgentID:    agentID,
		TenantID:   tenantID,
		Type:       proto.TaskTypeShell,
		Command:    tpl.Commands,
		Status:     "pending",
		Timeout:    300, // 5 分钟超时
		MaxRetries: 1,
		CreatedAt:  time.Now(),
	}
	if _, err := s.store.CreateTask(task); err != nil {
		logx.Warn(ctx, "自动调优：下发任务失败", "deviceID", deviceID, "templateID", tplID, "error", err)
		return
	}
	logx.Info(ctx, "自动调优：已下发优化模板任务", "deviceID", deviceID, "agentID", agentID, "templateID", tplID, "ruleID", ruleID)
}
