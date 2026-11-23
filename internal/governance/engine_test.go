package governance

import (
	"testing"
	"time"
)

func TestEvaluateRuleNull(t *testing.T) {
	rule := DataRule{ID: "r1", Kind: RuleKindNull, Column: "name", Severity: "warn"}
	rows := []Row{{"name": "alice"}, {"name": ""}, {"name": nil}}
	res, err := EvaluateRule(rule, rows)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Passed {
		t.Fatalf("expected fail, got pass")
	}
	if res.Observed != "nulls=2" {
		t.Fatalf("observed = %q, want nulls=2", res.Observed)
	}
	// maxNulls=2 allows 2 nulls
	rule2 := DataRule{ID: "r2", Kind: RuleKindNull, Column: "name", Expr: "maxNulls=2"}
	res2, err := EvaluateRule(rule2, rows)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !res2.Passed {
		t.Fatalf("expected pass with maxNulls=2")
	}
}

func TestEvaluateRuleUnique(t *testing.T) {
	rule := DataRule{ID: "r1", Kind: RuleKindUnique, Column: "email"}
	rows := []Row{{"email": "a@x.com"}, {"email": "a@x.com"}, {"email": "b@x.com"}}
	res, err := EvaluateRule(rule, rows)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Passed {
		t.Fatalf("expected fail on duplicate")
	}
	if res.Observed != "duplicates=1" {
		t.Fatalf("observed = %q, want duplicates=1", res.Observed)
	}
	// all unique
	rows2 := []Row{{"email": "a@x.com"}, {"email": "b@x.com"}}
	res2, _ := EvaluateRule(rule, rows2)
	if !res2.Passed {
		t.Fatalf("expected pass")
	}
}

func TestEvaluateRuleRange(t *testing.T) {
	rule := DataRule{ID: "r1", Kind: RuleKindRange, Column: "age", Expr: "min=18,max=65"}
	rows := []Row{{"age": 25.0}, {"age": 10.0}, {"age": "abc"}, {"age": 70.0}}
	res, err := EvaluateRule(rule, rows)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Passed {
		t.Fatalf("expected fail")
	}
	if res.Observed != "out_of_range=3" {
		t.Fatalf("observed = %q, want out_of_range=3", res.Observed)
	}
	// comma syntax
	rule2 := DataRule{ID: "r2", Kind: RuleKindRange, Column: "age", Expr: "18,65"}
	res2, _ := EvaluateRule(rule2, []Row{{"age": 30.0}})
	if !res2.Passed {
		t.Fatalf("expected pass with comma syntax")
	}
	// min only
	rule3 := DataRule{ID: "r3", Kind: RuleKindRange, Column: "age", Expr: "min=0"}
	res3, _ := EvaluateRule(rule3, []Row{{"age": -1.0}})
	if res3.Passed {
		t.Fatalf("expected fail with min only")
	}
}

func TestEvaluateRuleRegex(t *testing.T) {
	rule := DataRule{ID: "r1", Kind: RuleKindRegex, Column: "email", Expr: `^[^@]+@[^@]+\.[^@]+$`}
	rows := []Row{{"email": "a@b.com"}, {"email": "not-email"}, {"email": "c@d.org"}}
	res, err := EvaluateRule(rule, rows)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Passed {
		t.Fatalf("expected fail")
	}
	if res.Observed != "non_matching=1" {
		t.Fatalf("observed = %q, want non_matching=1", res.Observed)
	}
	// empty expr error
	rule2 := DataRule{ID: "r2", Kind: RuleKindRegex, Column: "email", Expr: ""}
	_, err2 := EvaluateRule(rule2, rows)
	if err2 == nil {
		t.Fatalf("expected error for empty regex")
	}
	// invalid regex
	rule3 := DataRule{ID: "r3", Kind: RuleKindRegex, Column: "email", Expr: "[invalid"}
	_, err3 := EvaluateRule(rule3, rows)
	if err3 == nil {
		t.Fatalf("expected error for invalid regex")
	}
}

func TestEvaluateRuleFreshness(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	rule := DataRule{ID: "r1", Kind: RuleKindFreshness, Column: "ts", Expr: "maxAgeHours=24"}
	rows := []Row{
		{"ts": "2026-01-01T10:00:00Z"}, // 2h ago, OK
		{"ts": "2025-12-30T10:00:00Z"}, // ~50h ago, stale
		{"ts": "not-a-time"},           // invalid
	}
	res, err := EvaluateRuleAt(rule, rows, now)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Passed {
		t.Fatalf("expected fail (stale + invalid)")
	}
	if res.Observed != "max_age_hours=50.00,violations=2" {
		t.Fatalf("observed = %q", res.Observed)
	}
}

func TestEvaluateRuleFreshnessColumnExpr(t *testing.T) {
	now := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	rule := DataRule{ID: "r1", Kind: RuleKindFreshness, Column: "updated_at", Expr: "maxAgeHours=48"}
	rows := []Row{{"updated_at": "2026-06-14T00:00:00Z"}} // 24h ago
	res, err := EvaluateRuleAt(rule, rows, now)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !res.Passed {
		t.Fatalf("expected pass: %s", res.Detail)
	}
}

func TestEvaluateRuleRowCount(t *testing.T) {
	rule := DataRule{ID: "r1", Kind: RuleKindRowCount, Column: "", Expr: "min=1,max=3"}
	rows := []Row{{"a": 1}, {"a": 2}}
	res, err := EvaluateRule(rule, rows)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !res.Passed {
		t.Fatalf("expected pass: 2 in [1,3]")
	}
	rows2 := []Row{{"a": 1}, {"a": 2}, {"a": 3}, {"a": 4}}
	res2, _ := EvaluateRule(rule, rows2)
	if res2.Passed {
		t.Fatalf("expected fail: 4 > 3")
	}
	// no bounds
	rule2 := DataRule{ID: "r2", Kind: RuleKindRowCount, Expr: ""}
	_, err2 := EvaluateRule(rule2, rows)
	if err2 == nil {
		t.Fatalf("expected error for no bounds")
	}
}

func TestEvaluateRuleUnknownKind(t *testing.T) {
	rule := DataRule{ID: "r1", Kind: "foobar", Column: "x"}
	_, err := EvaluateRule(rule, nil)
	if err == nil {
		t.Fatalf("expected error for unknown kind")
	}
}

func TestEvaluateRuleMissingColumn(t *testing.T) {
	rule := DataRule{ID: "r1", Kind: RuleKindNull, Column: ""}
	_, err := EvaluateRule(rule, nil)
	if err == nil {
		t.Fatalf("expected error for missing column")
	}
}

func TestSuggestMask(t *testing.T) {
	cases := []struct {
		kind, value, want string
	}{
		{PIIKindEmail, "alice@example.com", "a***@example.com"},
		{PIIKindPhone, "13812345678", "138****5678"},
		{PIIKindIDCard, "110101199001011234", "110101********1234"},
		{PIIKindName, "张三", "张**"},
		{PIIKindName, "张", "*"},
		{PIIKindCustom, "secret123", "s***"},
		{PIIKindEmail, "no-at-sign", "***"},
		{PIIKindPhone, "123", "***"},
		{"unknown", "anything", "********"},
	}
	for _, c := range cases {
		got := SuggestMask(c.kind, c.value)
		if got != c.want {
			t.Errorf("SuggestMask(%q, %q) = %q, want %q", c.kind, c.value, got, c.want)
		}
	}
	if SuggestMask(PIIKindEmail, "") != "" {
		t.Errorf("empty email should mask to empty")
	}
}

func TestBuildRetentionSQL(t *testing.T) {
	cases := []struct {
		policy RetentionPolicy
		want   string
		err    bool
	}{
		{RetentionPolicy{Dataset: "events", RetentionDays: 30, Action: RetentionActionDelete},
			"DELETE FROM events WHERE created_at < NOW() - INTERVAL 30 DAY;", false},
		{RetentionPolicy{Dataset: "events", RetentionDays: 7, Action: RetentionActionArchive},
			"INSERT INTO events_archive SELECT * FROM events WHERE created_at < NOW() - INTERVAL 7 DAY;", false},
		{RetentionPolicy{Dataset: "events.email", RetentionDays: 14, Action: RetentionActionAnonymize},
			"UPDATE events SET email = '***' WHERE created_at < NOW() - INTERVAL 14 DAY;", false},
		{RetentionPolicy{Dataset: "events", RetentionDays: 0, Action: RetentionActionDelete}, "", true},
		{RetentionPolicy{Dataset: "bad table", RetentionDays: 1, Action: RetentionActionDelete}, "", true},
		{RetentionPolicy{Dataset: "events", RetentionDays: 1, Action: "purge"}, "", true},
		{RetentionPolicy{Dataset: "events", RetentionDays: 1, Action: RetentionActionAnonymize}, "", true}, // no dot
	}
	for _, c := range cases {
		got, err := BuildRetentionSQL(c.policy)
		if c.err {
			if err == nil {
				t.Errorf("BuildRetentionSQL(%+v) = %q, want error", c.policy, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("BuildRetentionSQL(%+v) unexpected err: %v", c.policy, err)
			continue
		}
		if got != c.want {
			t.Errorf("BuildRetentionSQL(%+v) = %q, want %q", c.policy, got, c.want)
		}
	}
}

func TestValidateIdentifier(t *testing.T) {
	cases := []struct {
		s    string
		want bool
	}{
		{"users", true},
		{"public.users", true},
		{"_tmp", true},
		{"", false},
		{"1table", false},
		{"a b", false},
		{"a;b", false},
		{"a--b", false},
	}
	for _, c := range cases {
		err := ValidateIdentifier(c.s)
		got := err == nil
		if got != c.want {
			t.Errorf("ValidateIdentifier(%q) = %v, want %v", c.s, got, c.want)
		}
	}
}

func TestNewID(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := newID()
		if len(id) != 32 {
			t.Fatalf("newID len = %d, want 32", len(id))
		}
		if ids[id] {
			t.Fatalf("duplicate ID: %s", id)
		}
		ids[id] = true
	}
}

func TestRuleKinds(t *testing.T) {
	kinds := []string{RuleKindNull, RuleKindUnique, RuleKindRange, RuleKindRegex, RuleKindFreshness, RuleKindRowCount}
	if len(kinds) != 6 {
		t.Fatalf("rule kinds = %d, want 6", len(kinds))
	}
	// each should be non-empty
	for _, k := range kinds {
		if k == "" {
			t.Fatalf("empty rule kind")
		}
	}
}

func TestMemoryStoreBasic(t *testing.T) {
	store := NewMemoryStore()
	now := time.Now().UTC()
	rule := DataRule{TenantID: "t1", Dataset: "users", Column: "email", Kind: RuleKindUnique, Severity: "warn", Enabled: true}
	created, err := store.CreateRule(rule)
	if err != nil {
		t.Fatalf("CreateRule: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("ID not generated")
	}
	got, err := store.GetRule("t1", created.ID)
	if err != nil {
		t.Fatalf("GetRule: %v", err)
	}
	if got.Dataset != "users" {
		t.Fatalf("Dataset = %q, want users", got.Dataset)
	}
	// cross-tenant
	_, err = store.GetRule("t2", created.ID)
	if err != ErrTenantMismatch {
		t.Fatalf("cross-tenant GetRule: want ErrTenantMismatch, got %v", err)
	}
	// update
	created.Severity = "critical"
	updated, err := store.UpdateRule(created)
	if err != nil {
		t.Fatalf("UpdateRule: %v", err)
	}
	if updated.Severity != "critical" {
		t.Fatalf("Severity = %q, want critical", updated.Severity)
	}
	if updated.CreatedAt != created.CreatedAt {
		t.Fatalf("CreatedAt changed")
	}
	// delete
	if err := store.DeleteRule("t1", created.ID); err != nil {
		t.Fatalf("DeleteRule: %v", err)
	}
	if _, err := store.GetRule("t1", created.ID); err != ErrNotFound {
		t.Fatalf("after delete: want ErrNotFound, got %v", err)
	}
	// delete unknown
	if err := store.DeleteRule("t1", "nonexistent"); err != ErrNotFound {
		t.Fatalf("delete unknown: want ErrNotFound, got %v", err)
	}
	// list
	rule2 := DataRule{TenantID: "t1", Dataset: "orders", Kind: RuleKindNull, Column: "id", Severity: "info", Enabled: true}
	store.CreateRule(rule2)
	list := store.ListRules("t1")
	if len(list) != 1 {
		t.Fatalf("ListRules = %d, want 1", len(list))
	}
	// pii
	pii := PiiPolicy{TenantID: "t1", Dataset: "users", Column: "email", Kind: PIIKindEmail, Enabled: true}
	createdPii, _ := store.CreatePIIPolicy(pii)
	if createdPii.ID == "" {
		t.Fatalf("PII ID not generated")
	}
	if len(store.ListPIIPolicies("t1")) != 1 {
		t.Fatalf("ListPIIPolicies = %d", len(store.ListPIIPolicies("t1")))
	}
	// retention
	ret := RetentionPolicy{TenantID: "t1", Dataset: "logs", RetentionDays: 30, Action: RetentionActionDelete, Enabled: true}
	createdRet, _ := store.CreateRetentionPolicy(ret)
	if createdRet.ID == "" {
		t.Fatalf("Retention ID not generated")
	}
	// catalog
	asset := CatalogAsset{TenantID: "t1", Kind: AssetKindTable, Name: "users", Owner: "admin", Tags: []string{"pii"}, PIILevel: PIILevelHigh}
	createdAsset, _ := store.CreateCatalogAsset(asset)
	if createdAsset.ID == "" {
		t.Fatalf("Asset ID not generated")
	}
	listed := store.ListCatalogAssets("t1")
	if len(listed) != 1 {
		t.Fatalf("ListCatalogAssets = %d", len(listed))
	}
	// rule result
	res, _ := EvaluateRule(rule, []Row{{"name": "x"}})
	saved := store.AppendRuleResult(res)
	if saved.ID == "" {
		t.Fatalf("RuleResult ID not generated")
	}
	results := store.ListRuleResults("t1", rule.ID)
	_ = results
	_ = now
}

func TestMemoryStoreCrossTenant(t *testing.T) {
	store := NewMemoryStore()
	store.CreateRule(DataRule{TenantID: "t1", Dataset: "users", Kind: RuleKindNull, Column: "id", Severity: "info", Enabled: true})
	store.CreateRule(DataRule{TenantID: "t2", Dataset: "orders", Kind: RuleKindNull, Column: "id", Severity: "info", Enabled: true})
	if len(store.ListRules("t1")) != 1 {
		t.Fatalf("t1 rules = %d", len(store.ListRules("t1")))
	}
	if len(store.ListRules("t2")) != 1 {
		t.Fatalf("t2 rules = %d", len(store.ListRules("t2")))
	}
	if len(store.ListRules("t1")) == len(store.ListRules("t2")) && store.ListRules("t1")[0].Dataset == store.ListRules("t2")[0].Dataset {
		t.Fatalf("cross-tenant data leak")
	}
}
