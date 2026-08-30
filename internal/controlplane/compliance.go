package controlplane

// compliance.go 实现 Phase 3 安全合规 HTTP handler。
//
// API 端点：
//   - GET  /api/v1/compliance/rules           列出合规规则
//   - GET  /api/v1/compliance/rules/{id}      规则详情
//   - POST /api/v1/compliance/scan            扫描指定设备合规状态
//   - GET  /api/v1/compliance/reports         列出合规报告
//   - GET  /api/v1/compliance/reports/{id}    报告详情
//
// 设计要点（与 traffic.go 风格一致）：
//   - 用 s.requireTenantContext(w, r) 提取租户；
//   - 错误响应统一 {"error": "message"} 格式；
//   - 用 decodeJSONBody 解析请求体；
//   - 鉴权：需 compliance:read/compliance:write 权限。
//   - 合规引擎在 handler 内即时构造（NewEngine 很轻，规则只读）。

import (
	"net/http"
	"strings"
	"time"

	"opsmesh/internal/controlplane/paginate"

	"opsmesh/internal/compliance"
	"opsmesh/internal/store"
)

// complianceEngine 返回合规引擎（MVP：即时构造，规则只读无并发风险）。
func (s *Server) complianceEngine() *compliance.Engine {
	return compliance.NewEngine()
}

// handleComplianceRules 统一处理 /api/v1/compliance/rules：
//   - GET：列出合规规则
func (s *Server) handleComplianceRules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListComplianceRules(w, r)
	default:
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListComplianceRules 处理 GET /api/v1/compliance/rules：列出规则。
func (s *Server) handleListComplianceRules(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "compliance:read"); !ok {
		return
	}
	eng := s.complianceEngine()
	rules := eng.ListRules()
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"rules": rules})
}

// handleComplianceRuleRouting 分派 /api/v1/compliance/rules/{id} 子路径：
//   - GET /api/v1/compliance/rules/{id}  获取规则详情
func (s *Server) handleComplianceRuleRouting(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/compliance/rules/")
	if rest == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "rule id required"})
		return
	}
	id := rest
	if id == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "rule id required"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleGetComplianceRule(w, r, id)
	default:
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleGetComplianceRule 处理 GET /api/v1/compliance/rules/{id}：获取规则详情。
func (s *Server) handleGetComplianceRule(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requirePermission(w, r, "compliance:read"); !ok {
		return
	}
	eng := s.complianceEngine()
	rule, ok := eng.GetRule(id)
	if !ok || rule == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "rule not found"})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, rule)
}

// handleComplianceScan 处理 POST /api/v1/compliance/scan：扫描指定设备合规状态。
//
// 请求体：{"deviceID": "...", "results": [{"ruleId":"...", "passed":true, "output":"..."}]}
// MVP：接受 agent 上报的检查结果，聚合成 ComplianceReport 落库。
// 若 results 为空，则用引擎规则生成全 failed 占位结果（供测试/演示）。
func (s *Server) handleComplianceScan(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "compliance:write"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	var body struct {
		DeviceID string                        `json:"deviceID"`
		Results  []compliance.ComplianceResult `json:"results"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	// 结果为空时用引擎规则生成占位结果（全 failed，供测试/演示）。
	results := body.Results
	if len(results) == 0 {
		eng := s.complianceEngine()
		now := time.Now()
		for _, rule := range eng.ListRules() {
			results = append(results, compliance.ComplianceResult{
				RuleID:    rule.ID,
				Passed:    false,
				Output:    "not checked",
				CheckedAt: now,
			})
		}
	}
	// 用引擎计算分数并生成报告。
	eng := s.complianceEngine()
	report := eng.Scan(actx.TenantID, body.DeviceID, results)
	// 转换为 store 模型落库。
	storeReport := &store.ComplianceReport{
		TenantID:  report.TenantID,
		DeviceID:  report.DeviceID,
		Score:     report.Score,
		CreatedAt: report.CreatedAt,
	}
	for _, r := range report.Results {
		storeReport.Results = append(storeReport.Results, store.ComplianceResult{
			RuleID:    r.RuleID,
			Passed:    r.Passed,
			Output:    r.Output,
			CheckedAt: r.CheckedAt,
		})
	}
	saved := s.store.SaveReport(actx.TenantID, storeReport)
	if saved == nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "save report failed"})
		return
	}
	paginate.WriteJSON(w, http.StatusCreated, saved)
}

// handleComplianceReports 统一处理 /api/v1/compliance/reports：
//   - GET：列出合规报告
func (s *Server) handleComplianceReports(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListComplianceReports(w, r)
	default:
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListComplianceReports 处理 GET /api/v1/compliance/reports：列出报告。
func (s *Server) handleListComplianceReports(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "compliance:read"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	reports := s.store.ListReports(actx.TenantID)
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"reports": reports})
}

// handleComplianceReportRouting 分派 /api/v1/compliance/reports/{id} 子路径：
//   - GET /api/v1/compliance/reports/{id}  获取报告详情
func (s *Server) handleComplianceReportRouting(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/compliance/reports/")
	if rest == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "report id required"})
		return
	}
	id := rest
	if id == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "report id required"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleGetComplianceReport(w, r, id)
	default:
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleGetComplianceReport 处理 GET /api/v1/compliance/reports/{id}：获取报告详情。
func (s *Server) handleGetComplianceReport(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requirePermission(w, r, "compliance:read"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	report, ok := s.store.GetReport(actx.TenantID, id)
	if !ok || report == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "report not found"})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, report)
}
