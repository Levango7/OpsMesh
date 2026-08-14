// server_alerts.go 告警相关 HTTP handler、后台推送循环与告警规则 CRUD。
//
// 从 server.go 拆分而来（task 114：按路由域拆分巨型 server.go）。
// 包含告警列表/确认/静默端点、notifyLoop 推送循环、告警规则 API 与进程内规则存储，
// 逻辑未做任何修改。
package controlplane

import (
	"context"
	cryptoRand "crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"opsmesh/internal/events"
	"opsmesh/internal/logx"
	"opsmesh/internal/notify"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// notifyLoop M7/B7 告警通知推送：每 10s 检查是否有新的 firing 告警（critical + warning 均推送），
// 经聚合/抑制后通过多通道（Webhook/Email/Slack/企业微信）推送。
// 启动条件：WebhookURL 非空 或 邮件通道已配置；两者皆空时不启动。
// 防重复：通过 lastAlertSent 时间戳追踪；只推送 CreatedAt 晚于该时间戳的告警。
// B7 聚合/抑制：相同 metric+device 在 5 分钟窗口内只推送一次；critical 已触发时抑制同源 warning。
// B1 修复 7：启动时对 webhook URL 做 SSRF 校验，拒绝私网/元数据地址。
func (s *Server) notifyLoop(ctx context.Context) {
	// B7：启动条件放宽——Webhook 或 Email 任一配置即启动。
	webhookConfigured := s.cfg.AlertWebhookURL != ""
	emailConfigured := s.alertChannels != nil && s.alertChannels.Email != nil && s.alertChannels.Email.Enabled()
	if !webhookConfigured && !emailConfigured {
		return // 无任何告警通道配置，不启动 notifyLoop
	}
	// B1 修复 7：SSRF 校验 webhook URL，防告警推送被利用做 SSRF（访问云元数据/内网服务）。
	if webhookConfigured {
		if err := validateURLSSRF(s.cfg.AlertWebhookURL); err != nil {
			logx.Error(ctx, "告警 Webhook URL SSRF 校验失败，不启动 notifyLoop", err, "url", s.cfg.AlertWebhookURL)
			return
		}
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			// B7：周期清理聚合器过期条目（防止内存泄漏；保留 2 倍窗口内的条目）。
			s.alertAggr.Cleanup(now.Add(-2 * notify.AggregateWindow))
			alerts := s.store.Alerts("")
			for _, a := range alerts {
				// B7：推送所有 firing 状态告警（critical + warning）。
				// 非 firing（acknowledged/silenced）跳过；Status 为空视为 firing（兼容旧告警）。
				if a.Status != "" && a.Status != proto.AlertStatusFiring {
					continue
				}
				if !a.CreatedAt.After(s.lastAlertSent) {
					continue // 已推送过（防重复）
				}
				// B7：聚合/抑制——同源 5 分钟窗口内只推一次，critical 抑制同源 warning。
				if !s.alertAggr.Allow(a, now) {
					continue
				}
				// B7：多通道推送（Webhook + Email；Slack/企业微信由 URL 域名自动识别）。
				if err := s.alertChannels.Push(a); err != nil {
					logx.Error(ctx, "告警推送失败", err, "alertID", a.AlertID)
				} else {
					logx.Info(ctx, "告警推送成功", "alertID", a.AlertID, "severity", a.Severity)
				}
				if a.CreatedAt.After(s.lastAlertSent) {
					s.lastAlertSent = a.CreatedAt
				}
			}
		}
	}
}

// handleAlerts 处理 GET /api/v1/alerts：返回活跃告警（M7 监控告警最小数据源）。
// 租户隔离：requireAuth 时仅返回本租户告警。
// B1 修复 3：支持 page/pageSize 分页（向后兼容：不传 page 返回全量）。
func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:read"); !ok {
		return
	}
	alerts := s.store.Alerts(actx.TenantID)
	// B1 修复 3：分页（向后兼容：不传 page 返回全量）。
	page, pageSize := parsePagination(r.URL.Query())
	if page == 0 {
		writeJSON(w, http.StatusOK, alerts)
		return
	}
	total := len(alerts)
	start := (page - 1) * pageSize
	if start >= total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	writeJSON(w, http.StatusOK, paginateResult{
		Data: alerts[start:end], Total: total, Page: page, PageSize: pageSize, HasMore: end < total,
	})
}

// handleAlertRouting 统一分派 /api/v1/alerts/{id}/... 子路径：
//   - POST /api/v1/alerts/{id}/ack：确认告警（M7）
//   - POST /api/v1/alerts/{id}/silence：静默告警（M7）
func (s *Server) handleAlertRouting(w http.ResponseWriter, r *http.Request) {
	idAndRest := strings.TrimPrefix(r.URL.Path, "/api/v1/alerts/")
	parts := strings.SplitN(idAndRest, "/", 2)
	id := parts[0]
	if id == "" {
		jsonError(w, http.StatusBadRequest, "alert id required")
		return
	}
	switch {
	case len(parts) == 2 && parts[1] == "ack" && r.Method == http.MethodPost:
		s.handleAckAlert(w, r, id)
	case len(parts) == 2 && parts[1] == "silence" && r.Method == http.MethodPost:
		s.handleSilenceAlert(w, r, id)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found", "path": r.URL.Path})
	}
}

// handleAckAlert 处理 POST /api/v1/alerts/{id}/ack：确认告警（M7）。
// 租户隔离：requireAuth 时强制网关注入租户，禁止越权确认他租户告警。
func (s *Server) handleAckAlert(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:ack"); !ok {
		return
	}
	if s.store.Alert(id) == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "alert not found"})
		return
	}
	if !s.store.AckAlert(id, actx.TenantID, actx.UserID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "alert not found or tenant mismatch"})
		return
	}
	// M1-4：携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{TenantID: actx.TenantID, UserID: actx.UserID, Action: "ack_alert", Target: id, Detail: "acknowledged via HTTP"})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{TenantID: actx.TenantID, UserID: actx.UserID, Action: "ack_alert", Target: id, Level: events.LevelInfo})
	}
	// M3-2B SSE：通知前端告警状态已变更（告警面板刷新）
	// H6 租户隔离：携带 actx.TenantID，仅同租户订阅者收到。
	// M1-4：携带 ctx 的 trace_id，使 SSE 事件与链路追踪关联。
	s.publishEvent(r.Context(), "alert_new", actx.TenantID, map[string]string{
		"alertID": id,
		"action":  "ack",
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "acknowledged", "alertID": id})
}

// handleSilenceAlert 处理 POST /api/v1/alerts/{id}/silence：静默告警（M7）。
// 请求体（可选）：{"durationMinutes":1440,"comment":"..."}；缺省静默 24h。
func (s *Server) handleSilenceAlert(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:silence"); !ok {
		return
	}
	var body struct {
		DurationMinutes int    `json:"durationMinutes"`
		Comment         string `json:"comment"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil && err != io.EOF {
		log.Printf("controlplane: handleSilenceAlert 解析请求体失败: %v", err)
	}
	if s.store.Alert(id) == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "alert not found"})
		return
	}
	// 缺省静默 24h（与注释一致）；显式指定 DurationMinutes>0 时覆盖。
	until := time.Now().Add(24 * time.Hour)
	if body.DurationMinutes > 0 {
		until = time.Now().Add(time.Duration(body.DurationMinutes) * time.Minute)
	}
	if !s.store.SilenceAlert(id, actx.TenantID, actx.UserID, until, body.Comment) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "alert not found or tenant mismatch"})
		return
	}
	// M1-4：携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{TenantID: actx.TenantID, UserID: actx.UserID, Action: "silence_alert", Target: id, Detail: sanitizeAuditDetail(fmt.Sprintf("silenced %dm: %s", body.DurationMinutes, body.Comment))})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{TenantID: actx.TenantID, UserID: actx.UserID, Action: "silence_alert", Target: id, Level: events.LevelInfo})
	}
	// M3-2B SSE：通知前端告警已静默（告警面板刷新）
	// H6 租户隔离：携带 actx.TenantID，仅同租户订阅者收到。
	// M1-4：携带 ctx 的 trace_id，使 SSE 事件与链路追踪关联。
	s.publishEvent(r.Context(), "alert_new", actx.TenantID, map[string]string{
		"alertID": id,
		"action":  "silence",
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "silenced", "alertID": id})
}

// ============================================================================
// B1 修复 9：告警规则 API
// ============================================================================

// AlertRule 告警规则定义（M7 扩展：可配置的告警触发规则）。
// 用户可定义基于指标的告警规则，由后台评估循环检查并触发告警。
type AlertRule struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenantID"`
	Metric      string    `json:"metric"`      // 指标名（如 cpu.usage、mem.usage）
	Op          string    `json:"op"`          // 比较运算符：> >= < <= ==
	Threshold   float64   `json:"threshold"`   // 阈值
	ForDuration string    `json:"forDuration"` // 持续时间（如 5m），持续满足条件才触发
	Severity    string    `json:"severity"`    // warning / critical
	Message     string    `json:"message"`     // 告警消息模板
	Enabled     bool      `json:"enabled"`     // 是否启用
	CreatedAt   time.Time `json:"createdAt"`
	CreatedBy   string    `json:"createdBy"`
}

// handleAlertRules 处理 /api/v1/alert-rules：GET 列表 / POST 创建。
func (s *Server) handleAlertRules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listAlertRules(w, r)
	case http.MethodPost:
		s.createAlertRule(w, r)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// listAlertRules 返回当前租户的告警规则列表。
func (s *Server) listAlertRules(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:read"); !ok {
		return
	}
	rules := s.listAlertRulesForTenant(actx.TenantID)
	// 分页（向后兼容：不传 page 返回全量）。
	page, pageSize := parsePagination(r.URL.Query())
	if page == 0 {
		writeJSON(w, http.StatusOK, rules)
		return
	}
	total := len(rules)
	start := (page - 1) * pageSize
	if start >= total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	writeJSON(w, http.StatusOK, paginateResult{
		Data: rules[start:end], Total: total, Page: page, PageSize: pageSize, HasMore: end < total,
	})
}

// createAlertRule 创建一条告警规则。
func (s *Server) createAlertRule(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:write"); !ok {
		return
	}
	var rule AlertRule
	if err := decodeJSONBody(w, r, &rule); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if rule.Metric == "" || rule.Op == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "metric and op are required"})
		return
	}
	// 校验 op 合法。
	switch rule.Op {
	case ">", ">=", "<", "<=", "==":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "op must be one of >, >=, <, <=, =="})
		return
	}
	if rule.Severity == "" {
		rule.Severity = "warning"
	}
	ruleBytes := make([]byte, 8)
	if _, err := cryptoRand.Read(ruleBytes); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate rule ID"})
		return
	}
	rule.ID = "ar-" + hex.EncodeToString(ruleBytes)
	rule.TenantID = actx.TenantID
	rule.CreatedAt = time.Now()
	rule.CreatedBy = actx.UserID
	s.saveAlertRule(&rule)
	// M1-4：携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "create_alert_rule", Target: rule.ID,
		Detail: sanitizeAuditDetail(fmt.Sprintf("metric=%s op=%s threshold=%g severity=%s", rule.Metric, rule.Op, rule.Threshold, rule.Severity)),
	})
	writeJSON(w, http.StatusCreated, rule)
}

// handleAlertRuleRouting 分派 /api/v1/alert-rules/{id} 子路径：DELETE 删除。
func (s *Server) handleAlertRuleRouting(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/alert-rules/")
	if id == "" {
		jsonError(w, http.StatusBadRequest, "alert rule id required")
		return
	}
	switch r.Method {
	case http.MethodDelete:
		s.deleteAlertRule(w, r, id)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// deleteAlertRule 删除一条告警规则。
func (s *Server) deleteAlertRule(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:write"); !ok {
		return
	}
	if !s.removeAlertRule(id, actx.TenantID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "alert rule not found or tenant mismatch"})
		return
	}
	// M1-4：携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "delete_alert_rule", Target: id,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "id": id})
}

// parseForDurationSecs 把 controlplane.AlertRule.ForDuration 字符串（如 "5m"、"30s"、"1h"）
// 解析为秒数，存入 store.AlertRule.ForDuration（int）。解析失败返回 0（立即触发）。
func parseForDurationSecs(s string) int {
	if s == "" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0
	}
	return int(d.Seconds())
}

// formatForDurationSecs 把 store.AlertRule.ForDuration（秒）格式化为 controlplane.AlertRule.ForDuration
// 字符串（如 "300s"）。保留秒级精度，避免 "5m0s" 等非简洁格式。
func formatForDurationSecs(n int) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("%ds", n)
}

// cpAlertRuleToStore 把 controlplane.AlertRule 转换为 store.AlertRule。
// ForDuration 字符串 → 秒数；CreatedBy 透传。
func cpAlertRuleToStore(r *AlertRule) *store.AlertRule {
	if r == nil {
		return nil
	}
	return &store.AlertRule{
		ID:          r.ID,
		TenantID:    r.TenantID,
		Metric:      r.Metric,
		Op:          r.Op,
		Threshold:   r.Threshold,
		ForDuration: parseForDurationSecs(r.ForDuration),
		Severity:    r.Severity,
		Message:     r.Message,
		Enabled:     r.Enabled,
		CreatedAt:   r.CreatedAt,
		CreatedBy:   r.CreatedBy,
	}
}

// storeAlertRuleToCP 把 store.AlertRule 转换为 controlplane.AlertRule。
// ForDuration 秒数 → 字符串；CreatedBy 透传。
func storeAlertRuleToCP(r *store.AlertRule) *AlertRule {
	if r == nil {
		return nil
	}
	return &AlertRule{
		ID:          r.ID,
		TenantID:    r.TenantID,
		Metric:      r.Metric,
		Op:          r.Op,
		Threshold:   r.Threshold,
		ForDuration: formatForDurationSecs(r.ForDuration),
		Severity:    r.Severity,
		Message:     r.Message,
		Enabled:     r.Enabled,
		CreatedAt:   r.CreatedAt,
		CreatedBy:   r.CreatedBy,
	}
}

// listAlertRulesForTenant 返回指定租户的告警规则列表。
// task 246 M2 持久化：通过 store.ListAlertRules 读取（MemoryStore 内存 / SQLStore MySQL）。
func (s *Server) listAlertRulesForTenant(tenantID string) []*AlertRule {
	storeRules := s.store.ListAlertRules(tenantID)
	out := make([]*AlertRule, 0, len(storeRules))
	for _, sr := range storeRules {
		out = append(out, storeAlertRuleToCP(sr))
	}
	return out
}

// saveAlertRule 保存告警规则。
// task 246 M2 持久化：通过 store.CreateAlertRule 写入（MemoryStore 内存 / SQLStore MySQL）。
func (s *Server) saveAlertRule(rule *AlertRule) {
	s.store.CreateAlertRule(cpAlertRuleToStore(rule))
}

// removeAlertRule 删除告警规则（校验租户归属）。
// task 246 M2 持久化：通过 store.DeleteAlertRule 删除。
// 注意：store.DeleteAlertRule 不校验租户归属，需先 GetAlertRule 校验。
func (s *Server) removeAlertRule(id, tenantID string) bool {
	existing := s.store.GetAlertRule(id)
	if existing == nil {
		return false
	}
	if tenantID != "" && existing.TenantID != tenantID {
		return false
	}
	return s.store.DeleteAlertRule(id)
}
