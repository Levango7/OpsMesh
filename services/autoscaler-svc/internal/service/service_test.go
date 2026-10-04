package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/evaluator"
	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/k8s"
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
	return 0, fmt.Errorf("metric not found: %s", key)
}

func newTestService() *Service {
	eng := evaluator.NewEvaluator(func() time.Time {
		return time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	})
	scaler := k8s.NewClient()
	reader := &mockMetricsReader{
		values: map[string]float64{
			"web-app/default/cpu_usage": 50.0,
		},
	}
	return NewService(eng, reader, scaler)
}

func TestCreateRule(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	rule, err := svc.CreateRule(ctx, &models.ScaleRule{
		Name:               "High CPU",
		Deployment:         "web-app",
		Namespace:          "default",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		Enabled:            true,
	})
	if err != nil {
		t.Fatalf("CreateRule failed: %v", err)
	}

	if rule.ID == "" {
		t.Error("expected rule ID to be set")
	}
	if rule.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
	if rule.UpdatedAt.IsZero() {
		t.Error("expected UpdatedAt to be set")
	}
}

func TestCreateRuleNil(t *testing.T) {
	svc := newTestService()
	_, err := svc.CreateRule(context.Background(), nil)
	if err != ErrRuleInvalid {
		t.Fatalf("expected ErrRuleInvalid, got: %v", err)
	}
}

func TestGetRule(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.CreateRule(ctx, &models.ScaleRule{
		Name:               "Test",
		Deployment:         "web-app",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		Enabled:            true,
	})
	if err != nil {
		t.Fatalf("CreateRule failed: %v", err)
	}

	got, err := svc.GetRule(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetRule failed: %v", err)
	}

	if got.ID != created.ID {
		t.Errorf("expected ID %s, got %s", created.ID, got.ID)
	}
	if got.Name != "Test" {
		t.Errorf("expected name Test, got %s", got.Name)
	}
}

func TestGetRuleNotFound(t *testing.T) {
	svc := newTestService()
	_, err := svc.GetRule(context.Background(), "nonexistent")
	if err != ErrRuleNotFound {
		t.Fatalf("expected ErrRuleNotFound, got: %v", err)
	}
}

func TestListRules(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, err := svc.CreateRule(ctx, &models.ScaleRule{
			Name:               "Rule",
			Deployment:         "web-app",
			Metric:             "cpu_usage",
			ScaleUpThreshold:   80.0,
			ScaleDownThreshold: 20.0,
			MinReplicas:        1,
			MaxReplicas:        10,
			Enabled:            true,
		})
		if err != nil {
			t.Fatalf("CreateRule failed: %v", err)
		}
	}

	rules, err := svc.ListRules(ctx)
	if err != nil {
		t.Fatalf("ListRules failed: %v", err)
	}

	if len(rules) != 3 {
		t.Errorf("expected 3 rules, got %d", len(rules))
	}
}

func TestUpdateRule(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.CreateRule(ctx, &models.ScaleRule{
		Name:               "Original",
		Deployment:         "web-app",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		Enabled:            true,
	})
	if err != nil {
		t.Fatalf("CreateRule failed: %v", err)
	}

	created.Name = "Updated"
	created.ScaleUpThreshold = 85.0

	updated, err := svc.UpdateRule(ctx, created)
	if err != nil {
		t.Fatalf("UpdateRule failed: %v", err)
	}

	if updated.Name != "Updated" {
		t.Errorf("expected name Updated, got %s", updated.Name)
	}
	if updated.ScaleUpThreshold != 85.0 {
		t.Errorf("expected threshold 85.0, got %f", updated.ScaleUpThreshold)
	}
}

func TestUpdateRuleNotFound(t *testing.T) {
	svc := newTestService()
	_, err := svc.UpdateRule(context.Background(), &models.ScaleRule{
		ID:                 "nonexistent",
		Name:               "Ghost",
		Deployment:         "web-app",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
	})
	if err != ErrRuleNotFound {
		t.Fatalf("expected ErrRuleNotFound, got: %v", err)
	}
}

func TestDeleteRule(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.CreateRule(ctx, &models.ScaleRule{
		Name:               "ToDelete",
		Deployment:         "web-app",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		Enabled:            true,
	})
	if err != nil {
		t.Fatalf("CreateRule failed: %v", err)
	}

	err = svc.DeleteRule(ctx, created.ID)
	if err != nil {
		t.Fatalf("DeleteRule failed: %v", err)
	}

	_, err = svc.GetRule(ctx, created.ID)
	if err != ErrRuleNotFound {
		t.Fatalf("expected ErrRuleNotFound after delete, got: %v", err)
	}
}

func TestDeleteRuleNotFound(t *testing.T) {
	svc := newTestService()
	err := svc.DeleteRule(context.Background(), "nonexistent")
	if err != ErrRuleNotFound {
		t.Fatalf("expected ErrRuleNotFound, got: %v", err)
	}
}

func TestEvaluate(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.CreateRule(ctx, &models.ScaleRule{
		Name:               "High CPU",
		Deployment:         "web-app",
		Namespace:          "default",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		Enabled:            true,
	})
	if err != nil {
		t.Fatalf("CreateRule failed: %v", err)
	}

	// 通过接口播种初始副本数（不再用模拟实现特有的 RegisterDeployment）：
	// 这样同一份用例对任何执行器都成立。
	if err := svc.scaler.SetReplicas("web-app", "default", 3); err != nil {
		t.Fatalf("seed replicas: %v", err)
	}

	resp, err := svc.Evaluate(ctx, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if len(resp.Decisions) != 1 {
		t.Errorf("expected 1 decision, got %d", len(resp.Decisions))
	}
}

func TestGetDecisions(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.CreateRule(ctx, &models.ScaleRule{
		Name:               "Test",
		Deployment:         "web-app",
		Namespace:          "default",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		Enabled:            true,
	})
	if err != nil {
		t.Fatalf("CreateRule failed: %v", err)
	}

	// 通过接口播种初始副本数（不再用模拟实现特有的 RegisterDeployment）：
	// 这样同一份用例对任何执行器都成立。
	if err := svc.scaler.SetReplicas("web-app", "default", 3); err != nil {
		t.Fatalf("seed replicas: %v", err)
	}

	_, err = svc.Evaluate(ctx, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	decisions := svc.GetDecisions(ctx)
	if len(decisions) != 1 {
		t.Errorf("expected 1 decision, got %d", len(decisions))
	}
}

// stubScaler 是可标注身份的执行器替身。
//
// 单测没法连集群，但"响应与决策历史必须带上执行器身份"这件事与连的是不是真集群无关，
// 所以这里用一个只记录调用的替身把身份这一维喂进被测代码。
type stubScaler struct {
	replicas int32
	writes   int
	executor string
}

func (s *stubScaler) GetReplicas(deployment, namespace string) (int32, error) { return s.replicas, nil }

func (s *stubScaler) SetReplicas(deployment, namespace string, replicas int32) error {
	s.replicas = replicas
	s.writes++
	return nil
}

func (s *stubScaler) Executor() string { return s.executor }

func newServiceWithScaler(scaler k8s.Scaler) *Service {
	eng := evaluator.NewEvaluator(nil)
	reader := &mockMetricsReader{values: map[string]float64{"web-app/default/cpu_usage": 50.0}}
	return NewService(eng, reader, scaler)
}

// TestScale_SimulatedExecutorIsLabeledInFullChain 是 #61 造假面之一的回归位：
// 模拟执行器下"已扩容"这句话必须只在响应与决策历史里成立为"模拟"，否则调用方
// （前端 / runbook / 审计）无从区分"集群真的变了"和"进程内存变了个数字"。
func TestScale_SimulatedExecutorIsLabeledInFullChain(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	resp, err := svc.Scale(ctx, &ScaleRequest{Target: "web-app", Replicas: 4, Reason: "test"})
	if err != nil {
		t.Fatalf("Scale: %v", err)
	}
	if resp.Executor != k8s.ExecutorSimulated {
		t.Fatalf("响应 executor = %q, want %q", resp.Executor, k8s.ExecutorSimulated)
	}
	if !resp.Simulated {
		t.Fatal("模拟执行器的响应必须标 simulated=true")
	}
	if !strings.Contains(resp.Message, "未改动任何集群") {
		t.Fatalf("响应 message = %q，期望明说未改动集群", resp.Message)
	}

	ds := svc.GetDecisions(ctx)
	if len(ds) != 1 {
		t.Fatalf("决策历史 %d 条，want 1", len(ds))
	}
	if ds[0].Executor != k8s.ExecutorSimulated {
		t.Fatalf("决策历史 executor = %q, want %q", ds[0].Executor, k8s.ExecutorSimulated)
	}
}

// TestScale_RealExecutorNotMarkedSimulated 反向排除"恒标 simulated"或"恒不标"这两种
// 都会被当成"标签没用"的实现。
func TestScale_RealExecutorNotMarkedSimulated(t *testing.T) {
	stub := &stubScaler{replicas: 1, executor: k8s.ExecutorInCluster}
	svc := newServiceWithScaler(stub)
	ctx := context.Background()

	resp, err := svc.Scale(ctx, &ScaleRequest{Target: "web-app", Replicas: 3, Reason: "manual"})
	if err != nil {
		t.Fatalf("Scale: %v", err)
	}
	if resp.Simulated {
		t.Fatal("真实执行器被标成 simulated")
	}
	if resp.Executor != k8s.ExecutorInCluster {
		t.Fatalf("响应 executor = %q, want %q", resp.Executor, k8s.ExecutorInCluster)
	}
	if strings.Contains(resp.Message, "未改动任何集群") {
		t.Fatalf("真实执行器的 message 不该带模拟免责声明：%q", resp.Message)
	}
	if stub.writes != 1 {
		t.Fatalf("真实执行器被调用 %d 次, want 1", stub.writes)
	}
	if ds := svc.GetDecisions(ctx); len(ds) != 1 || ds[0].Executor != k8s.ExecutorInCluster {
		t.Fatalf("决策历史标注缺失：%+v", ds)
	}
}

// TestEvaluate_DecisionsCarryExecutor 证明自动评估路径同样标注（不只是手工 Scale 有）。
func TestEvaluate_DecisionsCarryExecutor(t *testing.T) {
	stub := &stubScaler{replicas: 2, executor: k8s.ExecutorKubeconfig}
	svc := newServiceWithScaler(stub)
	ctx := context.Background()

	if _, err := svc.CreateRule(ctx, &models.ScaleRule{
		Name: "r1", Deployment: "web-app", Namespace: "default", Metric: "cpu_usage",
		ScaleUpThreshold: 40, ScaleDownThreshold: 20, MinReplicas: 1, MaxReplicas: 10, Enabled: true,
	}); err != nil {
		t.Fatalf("CreateRule: %v", err)
	}
	resp, err := svc.Evaluate(ctx, "")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(resp.Decisions) == 0 {
		t.Fatal("没有产生任何决策")
	}
	for _, d := range resp.Decisions {
		if d.Executor != k8s.ExecutorKubeconfig {
			t.Fatalf("决策 %s 的 executor = %q, want %q", d.ID, d.Executor, k8s.ExecutorKubeconfig)
		}
	}
}
