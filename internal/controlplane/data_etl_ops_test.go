package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/dataetl"
	"github.com/Levango7/OpsMesh/internal/store"
)

func newETLTestServer() *Server {
	return &Server{
		store:           store.NewMemoryStore(),
		governanceStore: nil,
		cfg:             &config.Config{TaskMaxRetries: 1, Demo: false, TrustGatewayHeaders: true},
		requireAuth:     false,
	}
}

func etlRequest(t *testing.T, s *Server, h http.HandlerFunc, method, path string, body interface{}, tenant string) *httptest.ResponseRecorder {
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

func TestETLJobsInvalidEngine(t *testing.T) {
	s := newETLTestServer()
	rec := etlRequest(t, s, s.handleETLJobs, http.MethodPost, "/api/v1/etl/jobs", map[string]interface{}{
		"action": "create",
		"job": map[string]interface{}{
			"name": "test-job", "engine": "foobar", "source": "src", "sink": "sink",
		},
	}, "default")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestETLJobsCreateListRoundtrip(t *testing.T) {
	s := newETLTestServer()
	rec := etlRequest(t, s, s.handleETLJobs, http.MethodPost, "/api/v1/etl/jobs", map[string]interface{}{
		"action": "create",
		"job": map[string]interface{}{
			"name": "test-job", "engine": "spark", "source": "src", "sink": "sink",
		},
	}, "default")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: got %d: %s", rec.Code, rec.Body.String())
	}
	var created map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &created)
	if created["id"] == nil || created["id"] == "" {
		t.Fatalf("id not generated")
	}

	// list
	rec2 := etlRequest(t, s, s.handleETLJobs, http.MethodGet, "/api/v1/etl/jobs", nil, "default")
	if rec2.Code != http.StatusOK {
		t.Fatalf("list: got %d: %s", rec2.Code, rec2.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec2.Body.Bytes(), &resp)
	jobs := resp["jobs"].([]interface{})
	if len(jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(jobs))
	}
}

func TestETLJobsRunRequiresDevice(t *testing.T) {
	s := newETLTestServer()
	// create job first
	rec := etlRequest(t, s, s.handleETLJobs, http.MethodPost, "/api/v1/etl/jobs", map[string]interface{}{
		"action": "create",
		"job": map[string]interface{}{
			"name": "test-job", "engine": "spark", "source": "src", "sink": "sink",
		},
	}, "default")
	var created map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &created)
	id := created["id"].(string)

	// run without deviceID
	rec2 := etlRequest(t, s, s.handleETLJobs, http.MethodPost, "/api/v1/etl/jobs", map[string]interface{}{
		"action": "run",
		"job":    map[string]interface{}{"id": id},
	}, "default")
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("run without device: got %d, want 400: %s", rec2.Code, rec2.Body.String())
	}
}

func TestETLJobsCrossTenant(t *testing.T) {
	s := newETLTestServer()
	rec := etlRequest(t, s, s.handleETLJobs, http.MethodPost, "/api/v1/etl/jobs", map[string]interface{}{
		"action": "create",
		"job": map[string]interface{}{
			"name": "test-job", "engine": "spark", "source": "src", "sink": "sink",
		},
	}, "tenantA")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: got %d: %s", rec.Code, rec.Body.String())
	}
	var created map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &created)
	id := created["id"].(string)

	// tenant B tries to get it
	rec2 := etlRequest(t, s, s.handleETLJobs, http.MethodPost, "/api/v1/etl/jobs", map[string]interface{}{
		"action": "get",
		"job":    map[string]interface{}{"id": id},
	}, "tenantB")
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant get: got %d, want 404: %s", rec2.Code, rec2.Body.String())
	}
}

func TestETLJobsDelete(t *testing.T) {
	s := newETLTestServer()
	rec := etlRequest(t, s, s.handleETLJobs, http.MethodPost, "/api/v1/etl/jobs", map[string]interface{}{
		"action": "create",
		"job": map[string]interface{}{
			"name": "test-job", "engine": "spark", "source": "src", "sink": "sink",
		},
	}, "default")
	var created map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &created)
	id := created["id"].(string)

	rec2 := etlRequest(t, s, s.handleETLJobs, http.MethodPost, "/api/v1/etl/jobs", map[string]interface{}{
		"action": "delete",
		"job":    map[string]interface{}{"id": id},
	}, "default")
	if rec2.Code != http.StatusOK {
		t.Fatalf("delete: got %d: %s", rec2.Code, rec2.Body.String())
	}
}

func TestETLRunsList(t *testing.T) {
	s := newETLTestServer()
	rec := etlRequest(t, s, s.handleETLRuns, http.MethodGet, "/api/v1/etl/runs", nil, "default")
	if rec.Code != http.StatusOK {
		t.Fatalf("list runs: got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestETLEngines(t *testing.T) {
	names := dataetl.EtlEngineNames()
	if len(names) != 8 {
		t.Fatalf("engines = %d, want 8", len(names))
	}
	if !dataetl.IsValidEngine("spark") {
		t.Fatalf("spark should be valid")
	}
	if dataetl.IsValidEngine("foobar") {
		t.Fatalf("foobar should be invalid")
	}
}
