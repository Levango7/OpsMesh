package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

func newDBPGMVCCTestServer() *Server {
	return &Server{
		store:       store.NewMemoryStore(),
		cfg:         &config.Config{TaskMaxRetries: 1, Demo: false, TrustGatewayHeaders: true},
		requireAuth: false,
	}
}

func dbpgmvccPost(t *testing.T, s *Server, body map[string]interface{}, tenant string) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/db/postgresql/mvcc", strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenant)
	req.Header.Set("X-User-Id", "audit")
	req.Header.Set("X-User-Roles", "admin")
	rec := httptest.NewRecorder()
	s.handleDBPGMVCC(rec, req)
	return rec
}

func TestDBPGMVCC(t *testing.T) {
	s := newDBPGMVCCTestServer()

	// unknown action
	rec := dbpgmvccPost(t, s, map[string]interface{}{"deviceID": "dev-1", "action": "nope"}, "default")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown action: got %d, want 400: %s", rec.Code, rec.Body.String())
	}

	// missing deviceID
	rec = dbpgmvccPost(t, s, map[string]interface{}{"action": "dead-tuples"}, "default")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing deviceID: got %d, want 400: %s", rec.Code, rec.Body.String())
	}

	// valid
	rec = dbpgmvccPost(t, s, map[string]interface{}{"deviceID": "dev-1", "action": "dead-tuples"}, "default")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("valid: got %d, want 202: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp["taskID"] == nil || resp["taskID"] == "" {
		t.Fatalf("valid: missing taskID: %s", rec.Body.String())
	}

	// injection attempts rejected (table)
	for _, p := range []string{`x;id`, `$(id)`, "`id`", "a b", "a/b"} {
		rec = dbpgmvccPost(t, s, map[string]interface{}{"deviceID": "dev-1", "action": "vacuum", "table": p}, "default")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("injection table=%q: got %d, want 400: %s", p, rec.Code, rec.Body.String())
		}
	}

	// cross-tenant
	s.store.UpsertDevice(&proto.DeviceInfo{DeviceID: "dev-ct", TenantID: "other", State: "online"})
	rec = dbpgmvccPost(t, s, map[string]interface{}{"deviceID": "dev-ct", "action": "dead-tuples"}, "default")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant: got %d, want 403: %s", rec.Code, rec.Body.String())
	}
}
