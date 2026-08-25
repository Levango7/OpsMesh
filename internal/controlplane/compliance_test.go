// compliance_test.go 测试 Phase 3 安全合规 HTTP handler（compliance.go）。
//
// 覆盖范围：
//   - handleListComplianceRules：列出规则
//   - handleGetComplianceRule：规则详情、不存在
//   - handleComplianceScan：扫描设备、缺 deviceID
//   - handleListComplianceReports：空列表、扫描后列表
//   - handleGetComplianceReport：报告详情、不存在
//   - 鉴权：无 token 返回 401
//
// 测试策略（与 ticket_test.go 风格一致）：
//   - 白盒（package controlplane），直接装配 Server{store: MemoryStore, jwtSecret: 固定}；
//   - 鉴权用例通过 admin 登录获取 token（requirePermission 校验 compliance:read/write）；
//   - 用 httptest.NewRequest + httptest.NewRecorder 直接调用 handler。
package controlplane

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"opsmesh/internal/compliance"
	"opsmesh/internal/config"
	"opsmesh/internal/store"
)

// newComplianceTestServer 构造合规 API 测试用 Server。
func newComplianceTestServer() *Server {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3},
		jwtSecret:    []byte("test-jwt-secret-for-compliance-test-32bytes!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
}

// TestHandleListComplianceRules 验证列出规则返回 200 + 非空规则列表。
func TestHandleListComplianceRules(t *testing.T) {
	s := newComplianceTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/rules", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleComplianceRules(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Rules []compliance.ComplianceRule `json:"rules"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Rules) < 10 {
		t.Fatalf("rules=%d, want >= 10 (CIS baseline)", len(resp.Rules))
	}
}

// TestHandleGetComplianceRule 验证获取规则详情。
func TestHandleGetComplianceRule(t *testing.T) {
	s := newComplianceTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/rules/cis-ssh-01", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleComplianceRuleRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var rule compliance.ComplianceRule
	if err := json.Unmarshal(w.Body.Bytes(), &rule); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rule.ID != "cis-ssh-01" {
		t.Fatalf("rule ID=%s, want cis-ssh-01", rule.ID)
	}
}

// TestHandleGetComplianceRule_NotFound 验证不存在的规则返回 404。
func TestHandleGetComplianceRule_NotFound(t *testing.T) {
	s := newComplianceTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/rules/nonexistent", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleComplianceRuleRouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// TestHandleComplianceScan 验证扫描设备生成报告。
func TestHandleComplianceScan(t *testing.T) {
	s := newComplianceTestServer()
	auth := loginAsAdmin(t, s)

	body, _ := json.Marshal(map[string]interface{}{
		"deviceID": "dev-001",
		"results": []map[string]interface{}{
			{"ruleId": "cis-ssh-01", "passed": true, "output": "ok"},
			{"ruleId": "cis-firewall-01", "passed": false, "output": "firewalld inactive"},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/compliance/scan", bytes.NewReader(body))
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleComplianceScan(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var report store.ComplianceReport
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if report.DeviceID != "dev-001" {
		t.Fatalf("deviceID=%s, want dev-001", report.DeviceID)
	}
	if report.Score != 50 {
		t.Fatalf("score=%d, want 50 (1/2 passed)", report.Score)
	}
	if len(report.Results) != 2 {
		t.Fatalf("results=%d, want 2", len(report.Results))
	}
}

// TestHandleComplianceScan_NoDeviceID 验证缺 deviceID 返回 400。
func TestHandleComplianceScan_NoDeviceID(t *testing.T) {
	s := newComplianceTestServer()
	auth := loginAsAdmin(t, s)

	body, _ := json.Marshal(map[string]interface{}{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/compliance/scan", bytes.NewReader(body))
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleComplianceScan(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

// TestHandleListComplianceReports_AfterScan 验证扫描后列表含报告。
func TestHandleListComplianceReports_AfterScan(t *testing.T) {
	s := newComplianceTestServer()
	auth := loginAsAdmin(t, s)

	// 先直接写一条报告到 store。
	s.store.SaveReport("default", &store.ComplianceReport{
		DeviceID: "dev-002",
		Score:    80,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/reports", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleComplianceReports(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Reports []*store.ComplianceReport `json:"reports"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Reports) != 1 {
		t.Fatalf("reports=%d, want 1", len(resp.Reports))
	}
}

// TestHandleGetComplianceReport 验证获取报告详情。
func TestHandleGetComplianceReport(t *testing.T) {
	s := newComplianceTestServer()
	auth := loginAsAdmin(t, s)

	saved := s.store.SaveReport("default", &store.ComplianceReport{
		DeviceID: "dev-003",
		Score:    90,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/reports/"+saved.ID, nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleComplianceReportRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var report store.ComplianceReport
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if report.ID != saved.ID {
		t.Fatalf("report ID=%s, want %s", report.ID, saved.ID)
	}
}

// TestHandleListComplianceRules_NoToken 验证无 token 返回 401。
func TestHandleListComplianceRules_NoToken(t *testing.T) {
	s := newComplianceTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/rules", nil)
	w := httptest.NewRecorder()
	s.handleComplianceRules(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", w.Code)
	}
}
