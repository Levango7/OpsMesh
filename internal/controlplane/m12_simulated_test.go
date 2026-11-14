// m12_simulated_test.go 测试 M12 占位实现 simulated 标记。
//
// 覆盖：
//   - canary_enhance.go handleCanaryMetrics 响应含 simulated:true；
//   - ha.go handleHAFailover 响应含 simulated:false；
//   - compliance/engine.go Scan 返回 Simulated=false。
//
// 说明：backup restore 的 simulated 标记已在 G2 移除（真实备份/恢复，见 backup_api_test.go）。
package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Levango7/OpsMesh/internal/compliance"
	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/store"
)

// newCanarySimTestServer 构造带 batches + 鉴权的测试 Server（供 canary metrics 测试）。
func newCanarySimTestServer() *Server {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{Demo: true, TaskMaxRetries: 3},
		jwtSecret:    []byte("test-jwt-secret-for-canary-sim-32bytes!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
		batches:      newBatchStore(),
	}
}

// TestHandleCanaryMetrics_SimulatedFlag 验证 canary metrics 响应含 simulated:true。
// 审计整改批次1：baseline 与 canary 目前使用同一份租户级指标，必须如实标记为
// simulated（此前误标 false，会让灰度决策建立在无差异的伪对比上）。
func TestHandleCanaryMetrics_SimulatedFlag(t *testing.T) {
	s := newCanarySimTestServer()
	auth := loginAsAdmin(t, s)
	// 构造一个 canary release 供 metrics 查询。
	canaryID := "canary-sim-test"
	s.batches.mu.Lock()
	s.batches.canaries[canaryID] = &canaryRelease{CanaryID: canaryID, TenantID: "default", Percentage: 30}
	s.batches.mu.Unlock()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/canary/"+canaryID+"/metrics", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleCanaryEnhance(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Simulated bool   `json:"simulated"`
		Note      string `json:"note"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Simulated {
		t.Fatal("simulated=false, want true (baseline/canary 共用同一份租户级指标，按批次标签的分组对比尚未接线)")
	}
	if resp.Note == "" {
		t.Fatal("note 为空：simulated 响应必须说明原因")
	}
}

// TestHandleHAFailover_SimulatedFlag 验证 ha failover 不再假成功（审计整改批次1）。
// 手动切主未接线 ⇒ 必须返回 501 + ERR_NOT_IMPLEMENTED，且不得出现
// status:"accepted"/simulated:false 这类"看起来已执行"的字段。
func TestHandleHAFailover_SimulatedFlag(t *testing.T) {
	s := newHATestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ha/failover", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleHAFailover(w, req)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d, want 501 (手动切主未接线必须显式失败); body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Code    string `json:"code"`
		Error   string `json:"error"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Code != "ERR_NOT_IMPLEMENTED" {
		t.Fatalf("code=%q, want ERR_NOT_IMPLEMENTED", resp.Code)
	}
	if resp.Status == "accepted" {
		t.Fatal("501 响应不得带 status:accepted（假成功回归）")
	}
	if resp.Error == "" {
		t.Fatal("error 为空：必须说明未接线原因")
	}
}

// TestComplianceScan_SimulatedFlag 验证 compliance Engine.Scan 返回 Simulated=false（真实扫描编排）。
func TestComplianceScan_SimulatedFlag(t *testing.T) {
	eng := compliance.NewEngine()
	results := []compliance.ComplianceResult{
		{RuleID: "cis-ssh-01", Passed: true, Output: "ok"},
	}
	report := eng.Scan("default", "dev-001", results)
	if report.Simulated {
		t.Fatal("Scan.Simulated=true, want false (real scan orchestration)")
	}
}
