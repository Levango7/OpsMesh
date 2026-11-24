package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/governance"
	"github.com/Levango7/OpsMesh/internal/store"
)

func newGovernanceTestServer() *Server {
	return &Server{
		store:           store.NewMemoryStore(),
		governanceStore: governance.NewMemoryStore(),
		cfg:             &config.Config{TaskMaxRetries: 1, Demo: false, TrustGatewayHeaders: true},
		requireAuth:     false,
	}
}

func governanceRequest(t *testing.T, s *Server, h http.HandlerFunc, method, path string, body interface{}, tenant string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = strings.NewReader(string(b))
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenant)
	req.Header.Set("X-User-Id", "audit")
	req.Header.Set("X-User-Roles", "admin")
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func TestGovernanceRulesInvalidAction(t *testing.T) {
	s := newGovernanceTestServer()
	rec := governanceRequest(t, s, s.handleGovernanceRules, http.MethodPost, "/api/v1/governance/rules", map[string]interface{}{
		"action": "foobar",
	}, "default")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestGovernanceRulesCrossTenant(t *testing.T) {
	s := newGovernanceTestServer()
	// create as tenant A
	rec := governanceRequest(t, s, s.handleGovernanceRules, http.MethodPost, "/api/v1/governance/rules", map[string]interface{}{
		"action": "create",
		"rule": map[string]interface{}{
			"dataset": "users", "column": "email", "kind": "null", "severity": "warn", "enabled": true,
		},
	}, "tenantA")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: got %d: %s", rec.Code, rec.Body.String())
	}
	var created map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &created)
	id := created["id"].(string)

	// tenant B tries to get it
	rec2 := governanceRequest(t, s, s.handleGovernanceRules, http.MethodPost, "/api/v1/governance/rules", map[string]interface{}{
		"action": "evaluate",
		"rule":   map[string]interface{}{"id": id},
	}, "tenantB")
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant evaluate: got %d, want 404: %s", rec2.Code, rec2.Body.String())
	}
}

func TestGovernanceRulesCreateListRoundtrip(t *testing.T) {
	s := newGovernanceTestServer()
	rec := governanceRequest(t, s, s.handleGovernanceRules, http.MethodPost, "/api/v1/governance/rules", map[string]interface{}{
		"action": "create",
		"rule": map[string]interface{}{
			"dataset": "users", "column": "email", "kind": "null", "severity": "warn", "enabled": true,
		},
	}, "default")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: got %d: %s", rec.Code, rec.Body.String())
	}
	rec2 := governanceRequest(t, s, s.handleGovernanceRules, http.MethodPost, "/api/v1/governance/rules", map[string]interface{}{
		"action": "list",
	}, "default")
	if rec2.Code != http.StatusOK {
		t.Fatalf("list: got %d: %s", rec2.Code, rec2.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec2.Body.Bytes(), &resp)
	rules := resp["rules"].([]interface{})
	if len(rules) != 1 {
		t.Fatalf("list rules = %d, want 1", len(rules))
	}
}

func TestGovernanceRulesEvaluatePersists(t *testing.T) {
	s := newGovernanceTestServer()
	rec := governanceRequest(t, s, s.handleGovernanceRules, http.MethodPost, "/api/v1/governance/rules", map[string]interface{}{
		"action": "create",
		"rule": map[string]interface{}{
			"dataset": "users", "column": "email", "kind": "null", "severity": "warn", "enabled": true,
		},
	}, "default")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: got %d: %s", rec.Code, rec.Body.String())
	}
	var created map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &created)
	id := created["id"].(string)

	rec2 := governanceRequest(t, s, s.handleGovernanceRules, http.MethodPost, "/api/v1/governance/rules", map[string]interface{}{
		"action": "evaluate",
		"rule":   map[string]interface{}{"id": id},
		"rows":   []map[string]interface{}{{"email": "a@b.com"}},
	}, "default")
	if rec2.Code != http.StatusOK {
		t.Fatalf("evaluate: got %d: %s", rec2.Code, rec2.Body.String())
	}
	var res map[string]interface{}
	json.Unmarshal(rec2.Body.Bytes(), &res)
	if res["passed"] != true {
		t.Fatalf("expected passed=true, got %v", res["passed"])
	}
}

func TestGovernancePIIMaskedPreview(t *testing.T) {
	s := newGovernanceTestServer()
	rec := governanceRequest(t, s, s.handleGovernancePII, http.MethodPost, "/api/v1/governance/pii", map[string]interface{}{
		"action": "scan",
		"policy": map[string]interface{}{
			"dataset": "users", "column": "email", "kind": "email",
		},
		"rows": []map[string]interface{}{
			{"email": "alice@example.com", "name": "Alice"},
		},
	}, "default")
	if rec.Code != http.StatusOK {
		t.Fatalf("scan: got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	preview := resp["preview"].([]interface{})
	if len(preview) != 1 {
		t.Fatalf("preview len = %d", len(preview))
	}
	m := preview[0].(map[string]interface{})
	if m["email_masked"] != "a***@example.com" {
		t.Fatalf("masked = %v", m["email_masked"])
	}
}

func TestGovernanceRetentionDryRun(t *testing.T) {
	s := newGovernanceTestServer()
	rec := governanceRequest(t, s, s.handleGovernanceRetention, http.MethodPost, "/api/v1/governance/retention", map[string]interface{}{
		"action": "apply",
		"policy": map[string]interface{}{
			"dataset": "events", "retentionDays": 30, "action": "delete",
		},
		"dryRun": true,
	}, "default")
	if rec.Code != http.StatusOK {
		t.Fatalf("dryRun: got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["dryRun"] != true {
		t.Fatalf("dryRun = %v", resp["dryRun"])
	}
	if !strings.Contains(resp["sql"].(string), "DELETE FROM events") {
		t.Fatalf("sql = %v", resp["sql"])
	}
}

func TestGovernanceRetentionApplyRequiresDevice(t *testing.T) {
	s := newGovernanceTestServer()
	rec := governanceRequest(t, s, s.handleGovernanceRetention, http.MethodPost, "/api/v1/governance/retention", map[string]interface{}{
		"action": "apply",
		"policy": map[string]interface{}{
			"dataset": "events", "retentionDays": 30, "action": "delete",
		},
	}, "default")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("apply without device: got %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestGovernanceCatalogSearch(t *testing.T) {
	s := newGovernanceTestServer()
	s.governanceStore.CreateCatalogAsset(governance.CatalogAsset{TenantID: "default", Kind: "table", Name: "users", Owner: "admin", Tags: []string{"pii"}, PIILevel: "high"})
	rec := governanceRequest(t, s, s.handleGovernanceCatalog, http.MethodGet, "/api/v1/governance/catalog?q=users", nil, "default")
	if rec.Code != http.StatusOK {
		t.Fatalf("catalog: got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	assets := resp["assets"].([]interface{})
	if len(assets) != 1 {
		t.Fatalf("assets = %d, want 1", len(assets))
	}
}

func TestGovernanceLineageNotConfigured(t *testing.T) {
	s := newGovernanceTestServer()
	rec := governanceRequest(t, s, s.handleGovernanceLineage, http.MethodPost, "/api/v1/governance/lineage", map[string]interface{}{
		"query": "users",
	}, "default")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("lineage: got %d, want 503: %s", rec.Code, rec.Body.String())
	}
}

func TestGovernanceRetentionInvalidDataset(t *testing.T) {
	s := newGovernanceTestServer()
	rec := governanceRequest(t, s, s.handleGovernanceRetention, http.MethodPost, "/api/v1/governance/retention", map[string]interface{}{
		"action": "create",
		"policy": map[string]interface{}{
			"dataset": "bad table", "retentionDays": 30, "action": "delete",
		},
	}, "default")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid dataset: got %d, want 400: %s", rec.Code, rec.Body.String())
	}
}
