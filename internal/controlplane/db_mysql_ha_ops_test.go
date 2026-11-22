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

func newMySQLHATestServer() *Server {
	return &Server{
		store:       store.NewMemoryStore(),
		cfg:         &config.Config{TaskMaxRetries: 1, Demo: false, TrustGatewayHeaders: true},
		requireAuth: false,
	}
}

func mysqlHAPost(t *testing.T, s *Server, path string, h http.HandlerFunc, body map[string]interface{}, tenant string) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenant)
	req.Header.Set("X-User-Id", "audit")
	req.Header.Set("X-User-Roles", "admin")
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func TestMySQLHATable(t *testing.T) {
	s := newMySQLHATestServer()

	rec := mysqlHAPost(t, s, "/api/v1/db/mysql/ha", s.handleMySQLHA, map[string]interface{}{
		"deviceID": "dev-1", "action": "nope",
	}, "default")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown action: got %d, want 400: %s", rec.Code, rec.Body.String())
	}

	rec = mysqlHAPost(t, s, "/api/v1/db/mysql/ha", s.handleMySQLHA, map[string]interface{}{
		"action": "replication-status",
	}, "default")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing deviceID: got %d, want 400: %s", rec.Code, rec.Body.String())
	}

	rec = mysqlHAPost(t, s, "/api/v1/db/mysql/ha", s.handleMySQLHA, map[string]interface{}{
		"deviceID": "dev-1", "action": "replication-status",
	}, "default")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("valid: got %d, want 202: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp["taskID"] == nil || resp["taskID"] == "" {
		t.Fatalf("valid: missing taskID: %s", rec.Body.String())
	}

	for _, p := range []string{`x;id`, `$(id)`, "`id`", "a b", "a/b"} {
		rec = mysqlHAPost(t, s, "/api/v1/db/mysql/ha", s.handleMySQLHA, map[string]interface{}{
			"deviceID": "dev-1", "action": "group-replication-add", "standby": p,
		}, "default")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("injection standby=%q: got %d, want 400: %s", p, rec.Code, rec.Body.String())
		}
	}
	for _, p := range []string{`x;id`, `$(id)`} {
		rec = mysqlHAPost(t, s, "/api/v1/db/mysql/ha", s.handleMySQLHA, map[string]interface{}{
			"deviceID": "dev-1", "action": "replication-status", "db": p,
		}, "default")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("injection db=%q: got %d, want 400: %s", p, rec.Code, rec.Body.String())
		}
	}
	for _, p := range []string{`x;id`, `$(id)`} {
		rec = mysqlHAPost(t, s, "/api/v1/db/mysql/ha", s.handleMySQLHA, map[string]interface{}{
			"deviceID": "dev-1", "action": "replication-status", "user": p,
		}, "default")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("injection user=%q: got %d, want 400: %s", p, rec.Code, rec.Body.String())
		}
	}

	s.store.UpsertDevice(&proto.DeviceInfo{DeviceID: "dev-ct", TenantID: "other", State: "online"})
	rec = mysqlHAPost(t, s, "/api/v1/db/mysql/ha", s.handleMySQLHA, map[string]interface{}{
		"deviceID": "dev-ct", "action": "replication-status",
	}, "default")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant: got %d, want 403: %s", rec.Code, rec.Body.String())
	}
}
