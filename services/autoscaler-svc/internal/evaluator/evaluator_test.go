package evaluator

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/pkg/metrics"
	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/models"
)

// mockMetricsReader is a mock implementation of MetricsReader.
type mockMetricsReader struct {
	values map[string]float64
	err    error
}

func (m *mockMetricsReader) ReadMetric(deployment, namespace, metric string) (float64, error) {
	if m.err != nil {
		return 0, m.err
	}
	key := deployment + "/" + namespace + "/" + metric
	if v, ok := m.values[key]; ok {
		return v, nil
	}
	return 0, nil
}

// mockK8sScaler is a mock implementation of K8sScaler.
type mockK8sScaler struct {
	replicas map[string]int32
	err      error
}

func (m *mockK8sScaler) GetReplicas(deployment, namespace string) (int32, error) {
	if m.err != nil {
		return 0, m.err
	}
	key := namespace + "/" + deployment
	return m.replicas[key], nil
}

func (m *mockK8sScaler) SetReplicas(deployment, namespace string, replicas int32) error {
	if m.err != nil {
		return m.err
	}
	key := namespace + "/" + deployment
	m.replicas[key] = replicas
	return nil
}

func newTestEvaluator() *Evaluator {
	return NewEvaluator(func() time.Time {
		return time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	})
}

func TestAddRule(t *testing.T) {
	e := newTestEvaluator()
	rule := &models.ScaleRule{
		ID:                 "rule-1",
		Name:               "High CPU Scale Up",
		Deployment:         "web-app",
		Namespace:          "default",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		Enabled:            true,
	}

	if err := e.AddRule(rule); err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}

	rules := e.ListRules()
	if len(rules) != 1 {
		t.Errorf("expected 1 rule, got %d", len(rules))
	}
}

func TestAddRuleInvalid(t *testing.T) {
	e := newTestEvaluator()

	tests := []struct {
		name string
		rule *models.ScaleRule
	}{
		{"nil rule", nil},
		{"empty ID", &models.ScaleRule{ID: ""}},
		{"negative min", &models.ScaleRule{ID: "r1", MinReplicas: -1, MaxReplicas: 5, ScaleUpThreshold: 80, ScaleDownThreshold: 20}},
		{"max <= min", &models.ScaleRule{ID: "r1", MinReplicas: 5, MaxReplicas: 5, ScaleUpThreshold: 80, ScaleDownThreshold: 20}},
		{"up <= down", &models.ScaleRule{ID: "r1", MinReplicas: 1, MaxReplicas: 10, ScaleUpThreshold: 20, ScaleDownThreshold: 80}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := e.AddRule(tt.rule); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestUpdateRule(t *testing.T) {
	e := newTestEvaluator()
	rule := &models.ScaleRule{
		ID:                 "rule-1",
		Name:               "Original",
		Deployment:         "web-app",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		Enabled:            true,
	}

	if err := e.AddRule(rule); err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}

	rule.Name = "Updated"
	rule.ScaleUpThreshold = 85.0
	if err := e.UpdateRule(rule); err != nil {
		t.Fatalf("UpdateRule failed: %v", err)
	}

	updated, err := e.GetRule("rule-1")
	if err != nil {
		t.Fatalf("GetRule failed: %v", err)
	}
	if updated.Name != "Updated" {
		t.Errorf("expected name Updated, got %s", updated.Name)
	}
	if updated.ScaleUpThreshold != 85.0 {
		t.Errorf("expected threshold 85.0, got %f", updated.ScaleUpThreshold)
	}
}

func TestUpdateRuleNotFound(t *testing.T) {
	e := newTestEvaluator()
	err := e.UpdateRule(&models.ScaleRule{ID: "nonexistent"})
	if err == nil {
		t.Error("expected error for nonexistent rule")
	}
}

func TestDeleteRule(t *testing.T) {
	e := newTestEvaluator()
	rule := &models.ScaleRule{
		ID:                 "rule-1",
		Name:               "ToDelete",
		Deployment:         "web-app",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		Enabled:            true,
	}

	if err := e.AddRule(rule); err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}

	if err := e.DeleteRule("rule-1"); err != nil {
		t.Fatalf("DeleteRule failed: %v", err)
	}

	_, err := e.GetRule("rule-1")
	if err == nil {
		t.Error("expected error after delete")
	}
}

func TestDeleteRuleNotFound(t *testing.T) {
	e := newTestEvaluator()
	err := e.DeleteRule("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent rule")
	}
}

func TestEvaluateScaleUp(t *testing.T) {
	e := newTestEvaluator()
	rule := &models.ScaleRule{
		ID:                 "rule-1",
		Name:               "High CPU",
		Deployment:         "web-app",
		Namespace:          "default",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		Enabled:            true,
	}

	if err := e.AddRule(rule); err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}

	reader := &mockMetricsReader{
		values: map[string]float64{"web-app/default/cpu_usage": 95.0},
	}
	scaler := &mockK8sScaler{
		replicas: map[string]int32{"default/web-app": 3},
	}

	decisions, err := e.Evaluate(reader, scaler, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(decisions))
	}

	d := decisions[0]
	if d.Action != "scale_up" {
		t.Errorf("expected scale_up, got %s", d.Action)
	}
	if d.FromReplicas != 3 {
		t.Errorf("expected from 3, got %d", d.FromReplicas)
	}
	if d.ToReplicas != 4 {
		t.Errorf("expected to 4, got %d", d.ToReplicas)
	}
}

func TestEvaluateScaleDown(t *testing.T) {
	e := newTestEvaluator()
	rule := &models.ScaleRule{
		ID:                 "rule-1",
		Name:               "Low CPU",
		Deployment:         "web-app",
		Namespace:          "default",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		Enabled:            true,
	}

	if err := e.AddRule(rule); err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}

	reader := &mockMetricsReader{
		values: map[string]float64{"web-app/default/cpu_usage": 10.0},
	}
	scaler := &mockK8sScaler{
		replicas: map[string]int32{"default/web-app": 5},
	}

	decisions, err := e.Evaluate(reader, scaler, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(decisions))
	}

	d := decisions[0]
	if d.Action != "scale_down" {
		t.Errorf("expected scale_down, got %s", d.Action)
	}
	if d.FromReplicas != 5 {
		t.Errorf("expected from 5, got %d", d.FromReplicas)
	}
	if d.ToReplicas != 4 {
		t.Errorf("expected to 4, got %d", d.ToReplicas)
	}
}

func TestEvaluateNoAction(t *testing.T) {
	e := newTestEvaluator()
	rule := &models.ScaleRule{
		ID:                 "rule-1",
		Name:               "Normal",
		Deployment:         "web-app",
		Namespace:          "default",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		Enabled:            true,
	}

	if err := e.AddRule(rule); err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}

	reader := &mockMetricsReader{
		values: map[string]float64{"web-app/default/cpu_usage": 50.0},
	}
	scaler := &mockK8sScaler{
		replicas: map[string]int32{"default/web-app": 3},
	}

	decisions, err := e.Evaluate(reader, scaler, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(decisions))
	}

	if decisions[0].Action != "no_action" {
		t.Errorf("expected no_action, got %s", decisions[0].Action)
	}
}

func TestEvaluateMaxReplicasLimit(t *testing.T) {
	e := newTestEvaluator()
	rule := &models.ScaleRule{
		ID:                 "rule-1",
		Name:               "At Max",
		Deployment:         "web-app",
		Namespace:          "default",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        5,
		Enabled:            true,
	}

	if err := e.AddRule(rule); err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}

	reader := &mockMetricsReader{
		values: map[string]float64{"web-app/default/cpu_usage": 95.0},
	}
	scaler := &mockK8sScaler{
		replicas: map[string]int32{"default/web-app": 5},
	}

	decisions, err := e.Evaluate(reader, scaler, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if decisions[0].Action != "no_action" {
		t.Errorf("expected no_action at max, got %s", decisions[0].Action)
	}
	if decisions[0].ToReplicas != 5 {
		t.Errorf("expected to remain at 5, got %d", decisions[0].ToReplicas)
	}
}

func TestEvaluateMinReplicasLimit(t *testing.T) {
	e := newTestEvaluator()
	rule := &models.ScaleRule{
		ID:                 "rule-1",
		Name:               "At Min",
		Deployment:         "web-app",
		Namespace:          "default",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        2,
		MaxReplicas:        10,
		Enabled:            true,
	}

	if err := e.AddRule(rule); err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}

	reader := &mockMetricsReader{
		values: map[string]float64{"web-app/default/cpu_usage": 5.0},
	}
	scaler := &mockK8sScaler{
		replicas: map[string]int32{"default/web-app": 2},
	}

	decisions, err := e.Evaluate(reader, scaler, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if decisions[0].Action != "no_action" {
		t.Errorf("expected no_action at min, got %s", decisions[0].Action)
	}
	if decisions[0].ToReplicas != 2 {
		t.Errorf("expected to remain at 2, got %d", decisions[0].ToReplicas)
	}
}

func TestEvaluateDisabledRule(t *testing.T) {
	e := newTestEvaluator()
	rule := &models.ScaleRule{
		ID:                 "rule-1",
		Name:               "Disabled",
		Deployment:         "web-app",
		Namespace:          "default",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		Enabled:            false,
	}

	if err := e.AddRule(rule); err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}

	reader := &mockMetricsReader{
		values: map[string]float64{"web-app/default/cpu_usage": 95.0},
	}
	scaler := &mockK8sScaler{
		replicas: map[string]int32{"default/web-app": 3},
	}

	decisions, err := e.Evaluate(reader, scaler, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if len(decisions) != 0 {
		t.Errorf("expected 0 decisions for disabled rule, got %d", len(decisions))
	}
}

func TestEvaluateCooldown(t *testing.T) {
	e := newTestEvaluator()
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	e.now = func() time.Time { return now }

	rule := &models.ScaleRule{
		ID:                 "rule-1",
		Name:               "Cooldown Test",
		Deployment:         "web-app",
		Namespace:          "default",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		CooldownUp:         60 * time.Second,
		Enabled:            true,
	}

	if err := e.AddRule(rule); err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}

	reader := &mockMetricsReader{
		values: map[string]float64{"web-app/default/cpu_usage": 95.0},
	}
	scaler := &mockK8sScaler{
		replicas: map[string]int32{"default/web-app": 3},
	}

	decisions, err := e.Evaluate(reader, scaler, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if decisions[0].Action != "scale_up" {
		t.Fatalf("expected first scale_up, got %s", decisions[0].Action)
	}

	scaler.replicas["default/web-app"] = 4

	decisions, err = e.Evaluate(reader, scaler, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if decisions[0].Action != "no_action" {
		t.Errorf("expected no_action during cooldown, got %s", decisions[0].Action)
	}
	if decisions[0].Reason != "scale up in cooldown period" {
		t.Errorf("expected cooldown reason, got %s", decisions[0].Reason)
	}
}

func TestDecisionsHistory(t *testing.T) {
	e := newTestEvaluator()
	rule := &models.ScaleRule{
		ID:                 "rule-1",
		Name:               "History Test",
		Deployment:         "web-app",
		Namespace:          "default",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		Enabled:            true,
	}

	if err := e.AddRule(rule); err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}

	reader := &mockMetricsReader{
		values: map[string]float64{"web-app/default/cpu_usage": 95.0},
	}
	scaler := &mockK8sScaler{
		replicas: map[string]int32{"default/web-app": 3},
	}

	_, err := e.Evaluate(reader, scaler, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	decisions := e.Decisions()
	if len(decisions) != 1 {
		t.Errorf("expected 1 decision in history, got %d", len(decisions))
	}
}

// renderMetrics 抓取 /metrics 文本（与 device-svc 的指标断言同一口径），
// 断言"真实暴露出来的那一行"而不是内部 map 的值——后者会放过家族写错的用例。
func renderMetrics(t *testing.T) string {
	t.Helper()
	rec := httptest.NewRecorder()
	metrics.GetHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	return rec.Body.String()
}

// TestDecisionHistoryTrimmedToCap 验证历史上限：走真实 Evaluate 路径灌入 上限+200 条
// 之后，长度必须停在上限（缺陷是每轮只 append、从不裁剪，进程生命周期内单调增长）。
// 顺带断言占用量落在 **gauge** 家族：Set/Add 配错在渲染面上只差一个 _total 后缀，
// 语义却是"当前值"与"累计量"两种，后者会让 rate() 恒为无意义。
func TestDecisionHistoryTrimmedToCap(t *testing.T) {
	metrics.Init("autoscaler-svc-test")
	e := newTestEvaluator()
	rule := &models.ScaleRule{
		ID:                 "rule-1",
		Name:               "History Cap",
		Deployment:         "web-app",
		Namespace:          "default",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		Enabled:            true,
	}
	if err := e.AddRule(rule); err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}
	// 指标落在阈值区间内：每轮产出一条 no_action 决策，不触碰副本数，
	// 于是灌入量完全由 Evaluate 的历史写入路径决定。
	reader := &mockMetricsReader{values: map[string]float64{"web-app/default/cpu_usage": 50.0}}
	scaler := &mockK8sScaler{replicas: map[string]int32{"default/web-app": 3}}

	const inserted = maxDecisionHistory + 200
	for i := 0; i < inserted; i++ {
		if _, err := e.Evaluate(reader, scaler, ""); err != nil {
			t.Fatalf("Evaluate #%d failed: %v", i, err)
		}
	}

	if got := e.DecisionHistoryLen(); got != maxDecisionHistory {
		t.Errorf("灌入 %d 条后 DecisionHistoryLen=%d，期望上限 %d", inserted, got, maxDecisionHistory)
	}
	if got := len(e.Decisions()); got != maxDecisionHistory {
		t.Errorf("Decisions() 返回 %d 条，期望 %d", got, maxDecisionHistory)
	}

	body := renderMetrics(t)
	// 期望值从常量派生：改上限时这条断言跟着走，而不是留下一个只会误报的硬编码 500。
	wantGauge := fmt.Sprintf(`business_metrics{name="autoscaler_decision_history_entries"} %d`, maxDecisionHistory)
	if !strings.Contains(body, wantGauge) {
		t.Errorf("占用量 gauge 缺失或不为 %d\n---输出:\n%s", maxDecisionHistory, body)
	}
	if strings.Contains(body, `business_metrics_total{name="autoscaler_decision_history_entries"}`) {
		t.Errorf("占用量被写进了 counter 家族 business_metrics_total（gauge 用了 Add 语义）")
	}
}

// TestDecisionHistoryKeepsMostRecent 验证裁剪丢的是**最旧**：保留段必须正好是
// 最后 maxDecisionHistory 条且顺序不变。方向写反（丢尾部）时长度断言依然通过，
// 但冷却判定读的是尾部——所以这条用例单独盯住内容而不是条数。
func TestDecisionHistoryKeepsMostRecent(t *testing.T) {
	e := newTestEvaluator()

	const inserted = maxDecisionHistory + 200
	ids := make([]string, inserted)
	for i := 0; i < inserted; i++ {
		ids[i] = fmt.Sprintf("rule-%04d", i)
		e.RecordDecision(&models.ScaleDecision{
			RuleID:     ids[i],
			Deployment: "web-app",
			Action:     "no_action",
		})
	}

	got := e.Decisions()
	if len(got) != maxDecisionHistory {
		t.Fatalf("长度 %d，期望 %d", len(got), maxDecisionHistory)
	}
	if last := got[len(got)-1].RuleID; last != ids[inserted-1] {
		t.Errorf("尾部是 %s，期望最新一条 %s", last, ids[inserted-1])
	}
	oldestKept := inserted - maxDecisionHistory
	if first := got[0].RuleID; first != ids[oldestKept] {
		t.Errorf("头部是 %s，期望最早保留的 %s", first, ids[oldestKept])
	}
	if got[0].RuleID == ids[0] {
		t.Error("最早一条决策仍在历史里，说明没丢最旧")
	}
	for i, d := range got {
		if want := ids[oldestKept+i]; d.RuleID != want {
			t.Fatalf("第 %d 位是 %s，期望 %s（保留段顺序被打乱）", i, d.RuleID, want)
		}
	}
}

// TestCooldownStillWorksAfterHistoryWrap 是裁剪方向的守护用例：缓冲区绕回之后，
// 仍在 60s 冷却窗口里的那条 scale_up 必须还读得到。丢了它，症状不是报错而是
// "同一规则在冷却期内被反复扩容"——静默且难从日志回溯。
func TestCooldownStillWorksAfterHistoryWrap(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	e := NewEvaluator(func() time.Time { return now })

	// 先把历史灌满 filler：与 rule-1 无关（ruleID 不同、action=no_action 不参与冷却判定），
	// 但会占满容量，使随后的任何写入都触发一次绕回。
	for i := 0; i < maxDecisionHistory; i++ {
		e.RecordDecision(&models.ScaleDecision{
			RuleID:     "filler",
			Deployment: "other-app",
			Action:     "no_action",
			Timestamp:  now.Add(-time.Duration(maxDecisionHistory-i) * time.Second),
		})
	}

	rule := &models.ScaleRule{
		ID:                 "rule-1",
		Name:               "Wrap Cooldown",
		Deployment:         "web-app",
		Namespace:          "default",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		CooldownUp:         60 * time.Second,
		Enabled:            true,
	}
	if err := e.AddRule(rule); err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}
	reader := &mockMetricsReader{values: map[string]float64{"web-app/default/cpu_usage": 95.0}}
	scaler := &mockK8sScaler{replicas: map[string]int32{"default/web-app": 3}}

	decisions, err := e.Evaluate(reader, scaler, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if len(decisions) != 1 || decisions[0].Action != "scale_up" {
		t.Fatalf("绕回前首轮应扩容，got %+v", decisions)
	}

	// 绕回继续发生：scale_up 被挤到缓冲区中段（既不在头也不在尾）。
	for i := 0; i < 5; i++ {
		e.RecordDecision(&models.ScaleDecision{
			RuleID:     "filler",
			Deployment: "other-app",
			Action:     "no_action",
		})
	}
	if got := e.DecisionHistoryLen(); got != maxDecisionHistory {
		t.Fatalf("绕回后长度 %d，期望 %d", got, maxDecisionHistory)
	}

	scaler.replicas["default/web-app"] = 4
	decisions, err = e.Evaluate(reader, scaler, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if decisions[0].Action != "no_action" {
		t.Errorf("绕回后冷却失效：期望 no_action，got %s", decisions[0].Action)
	}
	if decisions[0].Reason != "scale up in cooldown period" {
		t.Errorf("绕回后冷却原因缺失，got %q", decisions[0].Reason)
	}
}
