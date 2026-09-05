package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/evaluator"
	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/k8s"
	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/service"
)

// mockMetricsReader 是 MetricsReader 的固定值实现。
type mockMetricsReader struct {
	values map[string]float64
}

func (m *mockMetricsReader) ReadMetric(deployment, namespace, metric string) (float64, error) {
	key := deployment + "/" + namespace + "/" + metric
	if v, ok := m.values[key]; ok {
		return v, nil
	}
	return 0, fmt.Errorf("metric not found: %s", key)
}

// newTestServer 组装带全部路由的测试服务器，并暴露底层组件供断言。
func newTestServer(t *testing.T) (*httptest.Server, *evaluator.Evaluator, *k8s.Client) {
	t.Helper()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	eng := evaluator.NewEvaluator(func() time.Time { return now })
	scaler := k8s.NewClient()
	reader := &mockMetricsReader{values: map[string]float64{}}
	svc := service.NewService(eng, reader, scaler)
	h := NewHandler(svc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, eng, scaler
}

// doJSON 发送 JSON 请求并解析 JSON 响应。
func doJSON(t *testing.T, ts *httptest.Server, method, path string, body interface{}, out interface{}) int {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode request body: %v", err)
		}
	}
	req, err := http.NewRequest(method, ts.URL+path, &buf)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode response of %s %s: %v", method, path, err)
		}
	}
	return resp.StatusCode
}

// ---- 修复点：POST /rules 兼容前端简化结构（threshold/cooldown）----

func TestCreateRuleSimplifiedPayload(t *testing.T) {
	// 前端 createScalingRule 的真实请求体形态。
	payload := map[string]interface{}{
		"name":        "cpu-rule",
		"metric":      "cpu",
		"threshold":   80,
		"minReplicas": 1,
		"maxReplicas": 10,
		"cooldown":    300,
	}

	ts, _, _ := newTestServer(t)
	var rule models.ScaleRule
	status := doJSON(t, ts, http.MethodPost, "/api/v1/rules", payload, &rule)
	if status != http.StatusCreated {
		t.Fatalf("create simplified rule: got status %d, want %d", status, http.StatusCreated)
	}
	if rule.Name != "cpu-rule" || rule.Metric != "cpu" {
		t.Errorf("name/metric = %q/%q, want cpu-rule/cpu", rule.Name, rule.Metric)
	}
	if rule.ScaleUpThreshold != 80 {
		t.Errorf("scaleUpThreshold = %v, want 80 (threshold 必须映射)", rule.ScaleUpThreshold)
	}
	if rule.ScaleDownThreshold <= 0 || rule.ScaleDownThreshold >= rule.ScaleUpThreshold {
		t.Errorf("scaleDownThreshold = %v, want 0 < down < up=%v", rule.ScaleDownThreshold, rule.ScaleUpThreshold)
	}
	if rule.CooldownUp != 300*time.Second || rule.CooldownDown != 300*time.Second {
		t.Errorf("cooldowns = %v/%v, want 300s/300s (cooldown 秒必须映射)", rule.CooldownUp, rule.CooldownDown)
	}
	if rule.Deployment != "default" || rule.Namespace != "default" {
		t.Errorf("deployment/namespace = %q/%q, want default/default", rule.Deployment, rule.Namespace)
	}
	if rule.MinReplicas != 1 || rule.MaxReplicas != 10 {
		t.Errorf("min/max replicas = %d/%d, want 1/10", rule.MinReplicas, rule.MaxReplicas)
	}
	if !rule.Enabled {
		t.Error("simplified rule must default to enabled")
	}
}

func TestCreateRuleFullPayloadStillWorks(t *testing.T) {
	// 旧结构照常支持（回归）。
	payload := map[string]interface{}{
		"name":               "full-rule",
		"deployment":         "web-app",
		"namespace":          "prod",
		"metric":             "cpu_usage",
		"scaleUpThreshold":   80.0,
		"scaleDownThreshold": 20.0,
		"minReplicas":        1,
		"maxReplicas":        5,
		"cooldownUp":         60000000000,
		"cooldownDown":       120000000000,
		"enabled":            true,
	}

	ts, _, _ := newTestServer(t)
	var rule models.ScaleRule
	status := doJSON(t, ts, http.MethodPost, "/api/v1/rules", payload, &rule)
	if status != http.StatusCreated {
		t.Fatalf("create full rule: got status %d, want %d", status, http.StatusCreated)
	}
	if rule.Deployment != "web-app" || rule.Namespace != "prod" {
		t.Errorf("deployment/namespace = %q/%q, want web-app/prod", rule.Deployment, rule.Namespace)
	}
	if rule.ScaleUpThreshold != 80 || rule.ScaleDownThreshold != 20 {
		t.Errorf("thresholds = %v/%v, want 80/20", rule.ScaleUpThreshold, rule.ScaleDownThreshold)
	}
	if rule.CooldownUp != 60*time.Second || rule.CooldownDown != 120*time.Second {
		t.Errorf("cooldowns = %v/%v, want 60s/120s", rule.CooldownUp, rule.CooldownDown)
	}
}

func TestCreateRuleInvalidPayloadStillRejected(t *testing.T) {
	ts, _, _ := newTestServer(t)
	var errResp struct {
		Error string `json:"error"`
	}
	// threshold=0 违反 ScaleUpThreshold>0 校验，必须 400。
	status := doJSON(t, ts, http.MethodPost, "/api/v1/rules", map[string]interface{}{
		"name": "bad", "metric": "cpu", "threshold": 0,
		"minReplicas": 1, "maxReplicas": 10, "cooldown": 60,
	}, &errResp)
	if status != http.StatusBadRequest {
		t.Fatalf("create invalid rule: got status %d, want %d", status, http.StatusBadRequest)
	}
}

// ---- 修复点：POST /api/v1/scale ----

func TestManualScale(t *testing.T) {
	ts, _, scaler := newTestServer(t)
	scaler.RegisterDeployment("web-app", "default", 3)

	var resp service.ScaleResponse
	status := doJSON(t, ts, http.MethodPost, "/api/v1/scale", map[string]interface{}{
		"target":   "web-app",
		"replicas": 5,
		"reason":   "manual test",
	}, &resp)
	if status != http.StatusOK {
		t.Fatalf("manual scale: got status %d, want %d", status, http.StatusOK)
	}
	if resp.Status != "ok" {
		t.Errorf("Status = %q, want ok", resp.Status)
	}
	if resp.Message == "" {
		t.Error("Message 不能为空")
	}

	got, err := scaler.GetReplicas("web-app", "default")
	if err != nil {
		t.Fatalf("GetReplicas: %v", err)
	}
	if got != 5 {
		t.Errorf("replicas = %d, want 5 (SetReplicas 必须生效)", got)
	}

	// 决策历史应记录一条 manual 决策。
	var decisions []*models.ScaleDecision
	status = doJSON(t, ts, http.MethodGet, "/api/v1/decisions", nil, &decisions)
	if status != http.StatusOK {
		t.Fatalf("get decisions: got status %d, want %d", status, http.StatusOK)
	}
	if len(decisions) != 1 {
		t.Fatalf("decisions count = %d, want 1", len(decisions))
	}
	d := decisions[0]
	if d.Action != "manual" || d.FromReplicas != 3 || d.ToReplicas != 5 || d.Reason != "manual test" {
		t.Errorf("decision = %+v, want action=manual from=3 to=5 reason=manual test", *d)
	}
}

func TestManualScaleNamespacedTarget(t *testing.T) {
	ts, _, scaler := newTestServer(t)
	scaler.RegisterDeployment("api", "prod", 2)

	var resp service.ScaleResponse
	status := doJSON(t, ts, http.MethodPost, "/api/v1/scale", map[string]interface{}{
		"target":   "prod/api",
		"replicas": 4,
	}, &resp)
	if status != http.StatusOK {
		t.Fatalf("manual scale namespaced: got status %d, want %d", status, http.StatusOK)
	}
	if got, _ := scaler.GetReplicas("api", "prod"); got != 4 {
		t.Errorf("replicas(api/prod) = %d, want 4", got)
	}
	// 默认命名空间不能被误写。
	if got, _ := scaler.GetReplicas("api", "default"); got != 0 {
		t.Errorf("replicas(api/default) = %d, want 0 (target=ns/dep 不应落到 default)", got)
	}
}

func TestManualScaleValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		body map[string]interface{}
	}{
		{"missing target", map[string]interface{}{"replicas": 3}},
		{"empty target", map[string]interface{}{"target": "", "replicas": 3}},
		{"negative replicas", map[string]interface{}{"target": "web", "replicas": -1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts, _, _ := newTestServer(t)
			var errResp struct {
				Error string `json:"error"`
			}
			status := doJSON(t, ts, http.MethodPost, "/api/v1/scale", tc.body, &errResp)
			if status != http.StatusBadRequest {
				t.Fatalf("scale %v: got status %d, want %d", tc.body, status, http.StatusBadRequest)
			}
		})
	}
}

// ---- 修复点：GET /api/v1/cooldowns ----

func TestCooldownsRoute(t *testing.T) {
	ts, _, _ := newTestServer(t)

	// 无规则时返回空列表结构（前端读 data.cooldowns）。
	var empty struct {
		Cooldowns []evaluator.CooldownStatus `json:"cooldowns"`
	}
	status := doJSON(t, ts, http.MethodGet, "/api/v1/cooldowns", nil, &empty)
	if status != http.StatusOK {
		t.Fatalf("get cooldowns: got status %d, want %d", status, http.StatusOK)
	}
	if empty.Cooldowns == nil {
		t.Error("cooldowns 字段缺失（前端读 data.cooldowns）")
	}
	if len(empty.Cooldowns) != 0 {
		t.Errorf("cooldowns length = %d, want 0", len(empty.Cooldowns))
	}
}

func TestCooldownsAfterScalingDecision(t *testing.T) {
	ts, eng, _ := newTestServer(t)

	// 规则 + 最近一次 scale_up 决策 → cooldowns 应报告剩余秒数。
	rule := &models.ScaleRule{
		ID:                 "r1",
		Name:               "cpu-rule",
		Deployment:         "web-app",
		Namespace:          "default",
		Metric:             "cpu",
		ScaleUpThreshold:   80,
		ScaleDownThreshold: 20,
		MinReplicas:        1,
		MaxReplicas:        10,
		CooldownUp:         120 * time.Second,
		CooldownDown:       300 * time.Second,
		Enabled:            true,
	}
	if err := eng.AddRule(rule); err != nil {
		t.Fatalf("AddRule: %v", err)
	}
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	eng.RecordDecision(&models.ScaleDecision{
		RuleID:       "r1",
		Deployment:   "web-app",
		Namespace:    "default",
		Action:       "scale_up",
		FromReplicas: 2,
		ToReplicas:   3,
		Timestamp:    now.Add(-30 * time.Second), // 120s 窗口内已过 30s
	})

	var resp struct {
		Cooldowns []evaluator.CooldownStatus `json:"cooldowns"`
	}
	status := doJSON(t, ts, http.MethodGet, "/api/v1/cooldowns", nil, &resp)
	if status != http.StatusOK {
		t.Fatalf("get cooldowns: got status %d, want %d", status, http.StatusOK)
	}
	if len(resp.Cooldowns) != 1 {
		t.Fatalf("cooldowns length = %d, want 1", len(resp.Cooldowns))
	}
	c := resp.Cooldowns[0]
	if c.RuleID != "r1" || c.RuleName != "cpu-rule" {
		t.Errorf("ruleId/ruleName = %q/%q, want r1/cpu-rule", c.RuleID, c.RuleName)
	}
	// now 是 evaluator 固定时钟；决策发生于 now-30s，窗口 120s，
	// 到期时刻 = 决策时刻 + 120s = now + 90s，剩余 90s。
	decisionAt := now.Add(-30 * time.Second)
	if c.Remaining != 90 {
		t.Errorf("remaining = %d, want 90", c.Remaining)
	}
	if c.ExpiresAt != decisionAt.Add(120*time.Second).Unix() {
		t.Errorf("expiresAt = %d, want %d", c.ExpiresAt, decisionAt.Add(120*time.Second).Unix())
	}
}

func TestCooldownsExpiredAfterWindow(t *testing.T) {
	ts, eng, _ := newTestServer(t)

	rule := &models.ScaleRule{
		ID:                 "r2",
		Name:               "mem-rule",
		Deployment:         "api",
		Namespace:          "default",
		Metric:             "memory",
		ScaleUpThreshold:   80,
		ScaleDownThreshold: 20,
		MinReplicas:        1,
		MaxReplicas:        10,
		CooldownUp:         60 * time.Second,
		Enabled:            true,
	}
	if err := eng.AddRule(rule); err != nil {
		t.Fatalf("AddRule: %v", err)
	}
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	eng.RecordDecision(&models.ScaleDecision{
		RuleID:    "r2",
		Action:    "scale_up",
		Timestamp: now.Add(-120 * time.Second), // 60s 窗口早已过期
	})

	var resp struct {
		Cooldowns []evaluator.CooldownStatus `json:"cooldowns"`
	}
	status := doJSON(t, ts, http.MethodGet, "/api/v1/cooldowns", nil, &resp)
	if status != http.StatusOK {
		t.Fatalf("get cooldowns: got status %d, want %d", status, http.StatusOK)
	}
	if len(resp.Cooldowns) != 1 {
		t.Fatalf("cooldowns length = %d, want 1", len(resp.Cooldowns))
	}
	if resp.Cooldowns[0].Remaining != 0 {
		t.Errorf("remaining = %d, want 0 (过期冷却必须归零)", resp.Cooldowns[0].Remaining)
	}
}

// ---- 回归：方法不允许 ----

func TestScaleMethodNotAllowed(t *testing.T) {
	ts, _, _ := newTestServer(t)
	status := doJSON(t, ts, http.MethodGet, "/api/v1/scale", nil, nil)
	if status != http.StatusMethodNotAllowed {
		t.Errorf("GET /api/v1/scale: got status %d, want %d", status, http.StatusMethodNotAllowed)
	}
}

func TestCooldownsMethodNotAllowed(t *testing.T) {
	ts, _, _ := newTestServer(t)
	status := doJSON(t, ts, http.MethodPost, "/api/v1/cooldowns", map[string]string{}, nil)
	if status != http.StatusMethodNotAllowed {
		t.Errorf("POST /api/v1/cooldowns: got status %d, want %d", status, http.StatusMethodNotAllowed)
	}
}
