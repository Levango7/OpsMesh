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

	"opsmesh/internal/compliance"
	"opsmesh/internal/config"
	"opsmesh/internal/store"
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

// TestHandleCanaryMetrics_SimulatedFlag 验证 canary metrics 响应含 simulated:false（真实指标）。
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
		Simulated bool `json:"simulated"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Simulated {
		t.Fatal("simulated=true, want false (real metrics)")
	}
}

// TestHandleHAFailover_SimulatedFlag 验证 ha failover 响应含 simulated:false（真实状态）。
func TestHandleHAFailover_SimulatedFlag(t *testing.T) {
	s := newHATestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ha/failover", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleHAFailover(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Simulated bool `json:"simulated"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Simulated {
		t.Fatal("simulated=true, want false (real HA status)")
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
