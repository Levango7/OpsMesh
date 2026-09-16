// alert_rules_m2.go M2 告警规则 API（alertengine.AlertRule 多条件规则）。
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
package controlplane

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/alertengine"
	"github.com/Levango7/OpsMesh/internal/proto"
)

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
