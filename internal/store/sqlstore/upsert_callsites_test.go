package sqlstore

import (
	"testing"
)

// expectedUpsert 描述一处期望的 s.dialect.Upsert 调用点：
// 所在函数、目标表、冲突目标（冲突列顺序敏感，须与 DDL 唯一键一致）。
type expectedUpsert struct {
	Func     string
	Table    string
	Conflict []string
}

// TestUpsertCallsitesLowRiskCRUD 钉死低风险 CRUD 组 15 处
// upsert 调用点的 Func+Table+Conflict 元数据。
// 迁移完成后 collectUpsertCalls 必须恰好命中每一处期望：
// 缺失（未迁移）或重复（重复调用点）都报红。
func TestUpsertCallsitesLowRiskCRUD(t *testing.T) {
	want := []expectedUpsert{
		{"CreateAlertRule", "alert_rules", []string{"id"}},
		{"CreateAPIKey", "api_keys", []string{"id"}},
		{"CreateApp", "argocd_apps", []string{"id"}},
		{"CreateAutomationRule", "automation_rules", []string{"id"}},
		{"CreateAutomationExecution", "automation_executions", []string{"id"}},
		{"CreateBackup", "backup_records", []string{"id"}},
		{"CreateBillingPlan", "billing_plans", []string{"id"}},
		{"CreateSubscription", "subscriptions", []string{"id"}},
		{"CreateInvoice", "invoices", []string{"id"}},
		{"UpsertBudget", "budgets", []string{"tenant_id"}},
		{"CreateCloudAccount", "cloud_accounts", []string{"id"}},
		{"SaveReport", "compliance_reports", []string{"id"}},
		{"CreateScript", "scripts", []string{"id"}},
		{"SaveOSTemplate", "os_templates", []string{"id"}},
		{"SaveMiddlewareTemplate", "middleware_templates", []string{"id"}},
	}
	assertExpectedUpserts(t, want)
}

// assertExpectedUpserts 从 collectUpsertCalls 中按 Func+Table+Conflict
// 精确匹配 want：缺失或重复都失败。
func assertExpectedUpserts(t *testing.T, want []expectedUpsert) {
	t.Helper()
	files := sqlstoreProductionFiles(t)
	calls := collectUpsertCalls(t, files)
	// 顺带守冲突目标：每一处调用点的 Conflict 在 MySQL/PG 双轨
	// 迁移 DDL 中都必须存在相同列序的唯一键。
	assertUpsertConflictParity(t, calls)

	for _, w := range want {
		var matched []upsertCallSpec
		for _, c := range calls {
			if c.Func == w.Func && c.Table == w.Table && equalStrings(c.Conflict, w.Conflict) {
				matched = append(matched, c)
			}
		}
		switch len(matched) {
		case 0:
			t.Errorf("缺少 Upsert 调用点: Func=%s Table=%s Conflict=%v",
				w.Func, w.Table, w.Conflict)
		case 1:
			// 精确命中，无操作。
		default:
			t.Errorf("Upsert 调用点重复: Func=%s Table=%s Conflict=%v 命中 %d 处",
				w.Func, w.Table, w.Conflict, len(matched))
		}
	}
}
