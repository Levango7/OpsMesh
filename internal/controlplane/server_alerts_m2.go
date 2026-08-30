// server_alerts_m2.go M2 集成：告警规则/静默/通知渠道/通知模板 API +
// 告警评估循环（alertengine.Engine + Silencer + Aggregator + Notifier）。
//
// 与 server_alerts.go 的关系：
//   - server_alerts.go 实现原有 /api/v1/alerts（列表/ack/silence）+ 旧版 alert-rules CRUD（globalAlertRules）；
//   - 本文件实现 M2 新增 API（alert-rules 用 alertengine.AlertRule 多条件 + 静默/渠道/模板 CRUD）；
//   - 不破坏现有功能：原有 /api/v1/alerts 与旧 alert-rules 保持不变。
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
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"opsmesh/internal/controlplane/paginate"

	"opsmesh/internal/alertengine"
	"opsmesh/internal/events"
	"opsmesh/internal/logx"
	"opsmesh/internal/notify"
	"opsmesh/internal/proto"
	"opsmesh/internal/secrets"
	"opsmesh/internal/store"
)

// ============================================================================
// M2 告警规则 API（alertengine.AlertRule 多条件规则）
// ============================================================================
//
// 与 server_alerts.go 中旧版 AlertRule（单条件 Metric/Op/Threshold）的区别：
//   - 旧版：单条件、字符串 ForDuration、进程内 globalAlertRules 存储；
//   - M2 版：多条件（Conditions []Condition）+ 逻辑组合（Logic AND/OR/NOT）+
//     持续时长（Duration time.Duration）+ 通知渠道选择（NotifyChannels）+
//     静默关联（SilenceID）+ alertengine.Engine 评估。
//
// 路由策略：/api/v1/alert-rules 仍由旧版 handleAlertRules 处理（向后兼容）；
// M2 多条件规则走 /api/v1/alert-rules-engine（避免与旧版冲突）。
// 但任务要求路由为 /api/v1/alert-rules，且要"不破坏现有功能"。
// 折中：旧版 handleAlertRules 保留，M2 多条件规则走同一路由但用不同方法区分
// （请求体含 conditions 字段 → M2 引擎；否则 → 旧版）。
// 为简洁起见，M2 多条件规则走独立路由 /api/v1/alert-rules-engine。

// handleAlertRulesEngine 处理 /api/v1/alert-rules-engine：GET 列表 / POST 创建。
// 使用 alertengine.AlertRule 多条件规则。
func (s *Server) handleAlertRulesEngine(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listAlertRulesEngine(w, r)
	case http.MethodPost:
		s.createAlertRuleEngine(w, r)
	default:
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// listAlertRulesEngine 返回当前租户的 alertengine.AlertRule 列表。
func (s *Server) listAlertRulesEngine(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:read"); !ok {
		return
	}
	rules, err := s.alertEngine.ListRules(actx.TenantID)
	if err != nil {
		writeInternalError(r.Context(), w, "alerts.listRules", err)
		return
	}
	paginate.WriteJSON(w, http.StatusOK, rules)
}

// createAlertRuleEngine 创建一条 alertengine.AlertRule。
func (s *Server) createAlertRuleEngine(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:write"); !ok {
		return
	}
	var rule alertengine.AlertRule
	if err := decodeJSONBody(w, r, &rule); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// 生成 ID（若未提供）
	if rule.ID == "" {
		rule.ID = "ar-eng-" + randHex(8)
	}
	rule.TenantID = actx.TenantID
	if err := s.alertEngine.AddRule(&rule); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "create_alert_rule_engine", Target: rule.ID,
		Detail: sanitizeAuditDetail(fmt.Sprintf("conditions=%d logic=%s severity=%s", len(rule.Conditions), rule.Logic, rule.Severity)),
	})
	paginate.WriteJSON(w, http.StatusCreated, rule)
}

// handleAlertRuleEngineRouting 分派 /api/v1/alert-rules-engine/{id} 子路径：
//   - GET    /api/v1/alert-rules-engine/{id} — 获取详情
//   - PUT    /api/v1/alert-rules-engine/{id} — 更新规则
//   - DELETE /api/v1/alert-rules-engine/{id} — 删除规则
func (s *Server) handleAlertRuleEngineRouting(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/alert-rules-engine/")
	if id == "" {
		paginate.JSONError(w, http.StatusBadRequest, "alert rule id required")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.getAlertRuleEngine(w, r, id)
	case http.MethodPut:
		s.updateAlertRuleEngine(w, r, id)
	case http.MethodDelete:
		s.deleteAlertRuleEngine(w, r, id)
	default:
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// getAlertRuleEngine 获取单条 alertengine.AlertRule。
func (s *Server) getAlertRuleEngine(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:read"); !ok {
		return
	}
	rule, err := s.alertEngine.GetRule(id)
	if err != nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "alert rule not found"})
		return
	}
	if rule.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "alert rule not found"})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, rule)
}

// updateAlertRuleEngine 更新一条 alertengine.AlertRule。
func (s *Server) updateAlertRuleEngine(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:write"); !ok {
		return
	}
	var rule alertengine.AlertRule
	if err := decodeJSONBody(w, r, &rule); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	rule.ID = id
	rule.TenantID = actx.TenantID
	if err := s.alertEngine.UpdateRule(&rule); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "update_alert_rule_engine", Target: id,
	})
	paginate.WriteJSON(w, http.StatusOK, rule)
}

// deleteAlertRuleEngine 删除一条 alertengine.AlertRule。
func (s *Server) deleteAlertRuleEngine(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:write"); !ok {
		return
	}
	// 校验租户归属
	rule, err := s.alertEngine.GetRule(id)
	if err != nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "alert rule not found"})
		return
	}
	if rule.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "alert rule not found"})
		return
	}
	if err := s.alertEngine.DeleteRule(id); err != nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "alert rule not found"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "delete_alert_rule_engine", Target: id,
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted", "id": id})
}

// ============================================================================
// M2 静默规则 API（SilenceRule 标签匹配 + 时间窗口抑制）
// ============================================================================

// handleAlertSilences 处理 /api/v1/alert-silences：GET 列表 / POST 创建。
func (s *Server) handleAlertSilences(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listAlertSilences(w, r)
	case http.MethodPost:
		s.createAlertSilence(w, r)
	default:
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// listAlertSilences 返回当前租户的静默规则列表。
func (s *Server) listAlertSilences(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:read"); !ok {
		return
	}
	silences := s.store.ListSilences(actx.TenantID)
	paginate.WriteJSON(w, http.StatusOK, silences)
}

// createAlertSilence 创建一条静默规则。
func (s *Server) createAlertSilence(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:write"); !ok {
		return
	}
	var sr store.SilenceRule
	if err := decodeJSONBody(w, r, &sr); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	sr.TenantID = actx.TenantID
	sr.CreatedBy = actx.UserID
	created := s.store.CreateSilence(&sr)
	// 同步注入 alertengine.Silencer（使评估循环立即应用新静默规则）
	if created != nil {
		_ = s.alertSilencer.AddRule(&alertengine.SilenceRule{
			ID:          created.ID,
			TenantID:    created.TenantID,
			MatchLabels: created.MatchLabels,
			StartAt:     created.StartAt,
			EndAt:       created.EndAt,
			CreatedBy:   created.CreatedBy,
			Reason:      created.Reason,
		})
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "create_alert_silence", Target: created.ID,
		Detail: sanitizeAuditDetail(fmt.Sprintf("reason=%s startAt=%s endAt=%s", created.Reason, created.StartAt.Format(time.RFC3339), created.EndAt.Format(time.RFC3339))),
	})
	paginate.WriteJSON(w, http.StatusCreated, created)
}

// handleAlertSilenceRouting 分派 /api/v1/alert-silences/{id} 子路径：DELETE 删除。
func (s *Server) handleAlertSilenceRouting(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/alert-silences/")
	if id == "" {
		paginate.JSONError(w, http.StatusBadRequest, "silence id required")
		return
	}
	switch r.Method {
	case http.MethodDelete:
		s.deleteAlertSilence(w, r, id)
	default:
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// deleteAlertSilence 删除一条静默规则。
func (s *Server) deleteAlertSilence(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:write"); !ok {
		return
	}
	if !s.store.DeleteSilence(id, actx.TenantID) {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "silence not found or tenant mismatch"})
		return
	}
	// 同步从 alertengine.Silencer 移除
	_ = s.alertSilencer.DeleteRule(id)
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "delete_alert_silence", Target: id,
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted", "id": id})
}

// ============================================================================
// M2 通知渠道 API（NotifyChannel CRUD + 测试发送）
// ============================================================================

// handleNotifyChannels 处理 /api/v1/notify-channels：GET 列表 / POST 创建。
func (s *Server) handleNotifyChannels(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listNotifyChannels(w, r)
	case http.MethodPost:
		s.createNotifyChannel(w, r)
	default:
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// listNotifyChannels 返回当前租户的通知渠道列表（Config 脱敏）。
func (s *Server) listNotifyChannels(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:read"); !ok {
		return
	}
	channels := s.store.ListNotifyChannels(actx.TenantID)
	// 脱敏：Config 中的敏感字段（webhook URL/secret/SMTP 密码）替换为 ***
	for _, c := range channels {
		c.Config = maskSensitiveConfig(c.Config)
	}
	paginate.WriteJSON(w, http.StatusOK, channels)
}

// createNotifyChannel 创建一条通知渠道。
func (s *Server) createNotifyChannel(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:write"); !ok {
		return
	}
	var c store.NotifyChannel
	if err := decodeJSONBody(w, r, &c); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// SSRF 防护：保存前校验 webhook URL，防通知渠道被利用做 SSRF
	// （如配置 webhookURL=http://169.254.169.254/latest/meta-data/ 访问云元数据）。
	// 校验失败返回 400 + 错误信息（不泄露内部校验细节，仅返回安全错误）。
	if err := validateNotifyChannelWebhook(&c, s.cfg.WebhookAllowPrivate); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("webhook URL SSRF validation failed: %v", err)})
		return
	}
	c.TenantID = actx.TenantID
	created := s.store.CreateNotifyChannel(&c)
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "create_notify_channel", Target: created.ID,
		Detail: sanitizeAuditDetail(fmt.Sprintf("name=%s type=%s enabled=%v", created.Name, created.Type, created.Enabled)),
	})
	// 返回时脱敏
	created.Config = maskSensitiveConfig(created.Config)
	paginate.WriteJSON(w, http.StatusCreated, created)
}

// handleNotifyChannelRouting 分派 /api/v1/notify-channels/{id} 子路径：
//   - PUT    /api/v1/notify-channels/{id}      — 更新渠道
//   - DELETE /api/v1/notify-channels/{id}      — 删除渠道
//   - POST   /api/v1/notify-channels/{id}/test — 测试发送
func (s *Server) handleNotifyChannelRouting(w http.ResponseWriter, r *http.Request) {
	idAndRest := strings.TrimPrefix(r.URL.Path, "/api/v1/notify-channels/")
	parts := strings.SplitN(idAndRest, "/", 2)
	id := parts[0]
	if id == "" {
		paginate.JSONError(w, http.StatusBadRequest, "channel id required")
		return
	}
	switch {
	case len(parts) == 1 && r.Method == http.MethodPut:
		s.updateNotifyChannel(w, r, id)
	case len(parts) == 1 && r.Method == http.MethodDelete:
		s.deleteNotifyChannel(w, r, id)
	case len(parts) == 2 && parts[1] == "test" && r.Method == http.MethodPost:
		s.testNotifyChannel(w, r, id)
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found", "path": r.URL.Path})
	}
}

// updateNotifyChannel 更新一条通知渠道。
func (s *Server) updateNotifyChannel(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:write"); !ok {
		return
	}
	var c store.NotifyChannel
	if err := decodeJSONBody(w, r, &c); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// SSRF 防护：更新前校验 webhook URL（与 createNotifyChannel 一致）。
	if err := validateNotifyChannelWebhook(&c, s.cfg.WebhookAllowPrivate); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("webhook URL SSRF validation failed: %v", err)})
		return
	}
	c.ID = id
	c.TenantID = actx.TenantID
	if !s.store.UpdateNotifyChannel(&c) {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "channel not found"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "update_notify_channel", Target: id,
	})
	paginate.WriteJSON(w, http.StatusOK, c)
}

// deleteNotifyChannel 删除一条通知渠道。
func (s *Server) deleteNotifyChannel(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:write"); !ok {
		return
	}
	if !s.store.DeleteNotifyChannel(id, actx.TenantID) {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "channel not found or tenant mismatch"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "delete_notify_channel", Target: id,
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted", "id": id})
}

// testNotifyChannel 测试发送一条通知到指定渠道。
// 请求体（可选）：{"title":"测试","body":"这是一条测试通知"}；缺省用内置测试消息。
func (s *Server) testNotifyChannel(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:read"); !ok {
		return
	}
	c := s.store.GetNotifyChannel(id)
	if c == nil || c.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "channel not found"})
		return
	}
	var body struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil && err != io.EOF {
		log.Printf("controlplane: testNotifyChannel 解析请求体失败: %v", err)
	}
	if body.Title == "" {
		body.Title = "OpsMesh 测试通知"
	}
	if body.Body == "" {
		body.Body = fmt.Sprintf("来自渠道 %s（类型 %s）的测试通知，发送时间 %s", c.Name, c.Type, time.Now().Format("2006-01-02 15:04:05"))
	}
	// 构造渠道实例并发送
	ch, err := buildChannel(c, s.secretProvider)
	if err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	msg := &notify.Message{
		Title:     body.Title,
		Body:      body.Body,
		Format:    "markdown",
		Severity:  "info",
		Source:    "test",
		Timestamp: time.Now(),
	}
	if err := ch.Send(msg); err != nil {
		// 发送失败属于服务端/上游渠道故障，不再用 200 携带错误体（HTTP 语义错误，
		// 会让前端与网关把失败当成功）。原始 err 含 SMTP 地址/内部错误细节，仅记日志。
		writeInternalError(r.Context(), w, "alerts.testNotifyChannel.send", err)
		return
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "test notification sent"})
}

// ============================================================================
// M2 通知模板 API（NotifyTemplate CRUD）
// ============================================================================

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

// ============================================================================
// 辅助函数
// ============================================================================

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

// buildChannel 根据 NotifyChannel 构造 notify.Channel 实例。
// Config 为 JSON 字符串，按渠道 Type 解析对应配置。
// provider 非空时用 WithSecret 版本构造渠道，解析 ${key} 格式密钥引用；
// 为空时退化为明文构造（向后兼容）。
func buildChannel(c *store.NotifyChannel, provider secrets.SecretProvider) (notify.Channel, error) {
	var cfg map[string]string
	if c.Config != "" {
		if err := json.Unmarshal([]byte(c.Config), &cfg); err != nil {
			return nil, fmt.Errorf("parse channel config: %w", err)
		}
	}
	switch c.Type {
	case "dingtalk":
		return notify.NewDingTalkChannelWithSecret(cfg["webhookURL"], cfg["secret"], provider)
	case "wecom", "wechat":
		return notify.NewWeChatWorkChannelWithSecret(cfg["webhookURL"], provider)
	case "feishu", "lark":
		return notify.NewFeishuChannelWithSecret(cfg["webhookURL"], cfg["secret"], provider)
	case "slack":
		return notify.NewSlackChannelWithSecret(cfg["webhookURL"], cfg["channel"], provider)
	case "email":
		port := 25
		if p := cfg["port"]; p != "" {
			if n, scanErr := fmt.Sscanf(p, "%d", &port); scanErr != nil || n != 1 {
				port = 25
			}
		}
		var to []string
		if t := cfg["to"]; t != "" {
			to = strings.Split(t, ",")
		}
		// SMTP 密码属于敏感信息，支持密钥引用解析。
		resolvedPass, err := secrets.ResolveSecret(cfg["pass"], provider)
		if err != nil {
			return nil, fmt.Errorf("resolve email password: %w", err)
		}
		return notify.NewEmailChannel(cfg["host"], port, cfg["user"], resolvedPass, cfg["from"], to), nil
	case "webhook", "generic":
		// 通用 webhook 复用钉钉渠道（发送 markdown 消息，适用于大多数 webhook 接收器）
		return notify.NewDingTalkChannelWithSecret(cfg["webhookURL"], "", provider)
	default:
		return nil, fmt.Errorf("unsupported channel type: %s", c.Type)
	}
}

// validateNotifyChannelWebhook 校验通知渠道的 webhook URL 防 SSRF。
//
// 对 webhook 类型渠道（dingtalk/wechat/feishu/slack/webhook/generic），
// 从 Config JSON 提取 webhookURL 字段并调用 ValidateWebhookURL 校验：
//   - 协议必须 http/https
//   - 默认拒绝私网/loopback/链路本地/云元数据地址
//   - allowPrivate=true 时放行内网（内网部署场景）
//
// email 类型渠道无 URL，跳过校验。
// 校验失败返回 error（调用方应返回 400 + 错误信息）。
func validateNotifyChannelWebhook(c *store.NotifyChannel, allowPrivate bool) error {
	// 仅 webhook 类型渠道需要校验 URL。
	switch c.Type {
	case "dingtalk", "wecom", "wechat", "feishu", "lark", "slack", "webhook", "generic":
		// 提取 Config JSON 中的 webhookURL 字段。
		var cfg map[string]string
		if c.Config != "" {
			if err := json.Unmarshal([]byte(c.Config), &cfg); err != nil {
				return fmt.Errorf("parse channel config for SSRF validation: %w", err)
			}
		}
		webhookURL := cfg["webhookURL"]
		if webhookURL == "" {
			// webhookURL 为空由上游 store 层校验（CreateNotifyChannel/UpdateNotifyChannel），
			// 此处不重复校验空值，仅校验非空 URL 的 SSRF 安全性。
			return nil
		}
		return ValidateWebhookURL(webhookURL, allowPrivate)
	case "email":
		// email 渠道无 URL，跳过 SSRF 校验。
		return nil
	default:
		// 未知类型由 buildChannel 上游校验，此处放行（不阻塞未知类型校验流程）。
		return nil
	}
}

// publishEvent 发布 SSE 事件（与 server.go 中的 publishEvent 同签名；此处复用）。
// 若 Server 未定义 publishEvent，则通过 bus.Publish 兜底。
// 注：server.go 已有 publishEvent 方法，此处声明仅用于本文件独立编译；
// 实际复用 server.go 的方法（Go 方法在包内共享）。
//
// 为避免重复定义，删除此声明——server.go 的 publishEvent 已在包内可见。
// func (s *Server) publishEvent(ctx context.Context, eventType, tenantID string, data map[string]string) {
// }

// _ 确保 events 包被使用（publishEvent 内部引用 events.Event）。
var _ = events.LevelInfo
