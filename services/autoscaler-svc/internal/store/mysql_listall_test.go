package store

import (
	"os"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/models"
)

// mysql_listall_test.go —— ListAllRules 的真库往返钉（issue #48 裁决① 接线）。
//
// ListAllRules 是「启动时装载存量规则」的唯一入口（evaluator.SetStore 调用）：
// 它漏一条，重启就静默丢一条规则。真库判据：写两条不同租户的规则 ⇒
// ListAllRules 必须两条都返回（跨租户、不过滤）。
// 未设置 OPSMESH_TEST_MYSQL_DSN 时跳过；CI 的 services job 会设置 ⇒ 在 CI 真跑。
func TestMySQLListAllRulesCrossTenant(t *testing.T) {
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("SKIP reason=missing OPSMESH_TEST_MYSQL_DSN; ListAllRules 真库往返需设置该环境变量")
	}
	ms, err := NewMySQLStore(dsn)
	if err != nil {
		t.Fatalf("NewMySQLStore: %v（建表/迁移不通过）", err)
	}

	suffix := time.Now().UTC().Format("150405")
	a := &models.ScaleRule{ID: "la-a-" + suffix, TenantID: "t-a", Name: "n-a",
		Deployment: "d", Namespace: "ns", Metric: "cpu",
		ScaleUpThreshold: 80, ScaleDownThreshold: 20, MinReplicas: 1, MaxReplicas: 5, Enabled: true}
	b := &models.ScaleRule{ID: "la-b-" + suffix, TenantID: "t-b", Name: "n-b",
		Deployment: "d", Namespace: "ns", Metric: "cpu",
		ScaleUpThreshold: 80, ScaleDownThreshold: 20, MinReplicas: 1, MaxReplicas: 5, Enabled: true}
	if _, err := ms.CreateRule(a); err != nil {
		t.Fatalf("CreateRule(a): %v", err)
	}
	if _, err := ms.CreateRule(b); err != nil {
		t.Fatalf("CreateRule(b): %v", err)
	}
	defer func() {
		ms.DeleteRule(a.ID, a.TenantID)
		ms.DeleteRule(b.ID, b.TenantID)
	}()

	all := ms.ListAllRules()
	found := map[string]bool{}
	for _, r := range all {
		if r.ID == a.ID || r.ID == b.ID {
			found[r.ID] = true
		}
	}
	if !found[a.ID] || !found[b.ID] {
		t.Fatalf("ListAllRules 跨租户漏规则：want 2 got %v 条（含 la-a=%v / la-b=%v）",
			len(all), found[a.ID], found[b.ID])
	}
}
