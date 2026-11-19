package evaluator

import (
	"errors"
	"testing"

	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/store"
)

// store_wiring_test.go —— issue #48 裁决①（持久化接线）的回归钉：
//   ① SetStore 装载存量规则（重启不丢的**前半**）；
//   ② AddRule/DeleteRule 写穿到 store（重启不丢的**后半**）；
//   ③ store 写失败 ⇒ fail-open：内存态保留、不向调用方报错（响亮记 Error 由 logx 承担）。
//
// 用内存 store 即可（不需要真库）：钉的是**接线语义**，不是 MySQL 方言
// （方言由 store 包的 DSN 门控测试负责）。

func TestSetStoreLoadsExistingRules(t *testing.T) {
	ms := store.NewMemoryStore()
	if _, err := ms.CreateRule(&models.ScaleRule{
		ID: "r-existing", TenantID: "A", Name: "存量规则",
		Deployment: "web", Namespace: "default", Metric: "cpu",
		ScaleUpThreshold: 80, ScaleDownThreshold: 20,
		MinReplicas: 1, MaxReplicas: 10, Enabled: true,
	}); err != nil {
		t.Fatalf("seed CreateRule: %v", err)
	}

	eng := NewEvaluator(nil)
	if err := eng.SetStore(ms); err != nil {
		t.Fatalf("SetStore: %v", err)
	}

	got, err := eng.GetRule("r-existing", "A")
	if err != nil || got == nil {
		t.Fatalf("存量规则未被装载：GetRule = %v, %v", got, err)
	}
	if got.Name != "存量规则" {
		t.Fatalf("装载内容不符: %+v", got)
	}
}

func TestEvaluatorWriteThrough(t *testing.T) {
	ms := store.NewMemoryStore()
	eng := NewEvaluator(nil)
	if err := eng.SetStore(ms); err != nil {
		t.Fatalf("SetStore: %v", err)
	}

	rule := &models.ScaleRule{
		ID: "r-wt", TenantID: "A", Name: "写穿", Deployment: "web", Namespace: "default",
		Metric: "cpu", ScaleUpThreshold: 80, ScaleDownThreshold: 20,
		MinReplicas: 1, MaxReplicas: 10, Enabled: true,
	}
	if err := eng.AddRule(rule); err != nil {
		t.Fatalf("AddRule: %v", err)
	}
	if _, ok := ms.GetRule("r-wt", "A"); !ok {
		t.Fatal("AddRule 未写穿到 store——重启即丢（接线失效）")
	}

	if err := eng.DeleteRule("r-wt", "A"); err != nil {
		t.Fatalf("DeleteRule: %v", err)
	}
	if _, ok := ms.GetRule("r-wt", "A"); ok {
		t.Fatal("DeleteRule 未写穿到 store——库里还在的规则会在下次装载时复活")
	}
}

// failingStore 全部写路径失败：钉 fail-open 语义（内存是求值权威，不因落库失败拒绝请求）。
type failingStore struct{ store.AutoscalerStore }

func (f failingStore) ListAllRules() []*models.ScaleRule { return nil }
func (f failingStore) CreateRule(*models.ScaleRule) (*models.ScaleRule, error) {
	return nil, errors.New("boom")
}
func (f failingStore) UpdateRule(*models.ScaleRule, string) error { return errors.New("boom") }
func (f failingStore) DeleteRule(string, string) bool             { return false }

func TestEvaluatorStoreFailureFailsOpen(t *testing.T) {
	eng := NewEvaluator(nil)
	if err := eng.SetStore(failingStore{}); err != nil {
		t.Fatalf("SetStore(failingStore): %v", err)
	}

	rule := &models.ScaleRule{
		ID: "r-fail", TenantID: "A", Name: "n", Deployment: "d", Namespace: "ns",
		Metric: "cpu", ScaleUpThreshold: 80, ScaleDownThreshold: 20,
		MinReplicas: 1, MaxReplicas: 5, Enabled: true,
	}
	// store 写失败 ⇒ **不**向调用方报错（fail-open），内存态保留；
	// 响亮记 Error 由 logx 承担（本断言钉"不拒绝请求"这一半）。
	if err := eng.AddRule(rule); err != nil {
		t.Fatalf("AddRule 在 store 故障时不应拒绝请求（fail-open）: %v", err)
	}
	got, err := eng.GetRule("r-fail", "A")
	if err != nil || got == nil {
		t.Fatalf("store 故障时内存态丢失: %v, %v", got, err)
	}
}
