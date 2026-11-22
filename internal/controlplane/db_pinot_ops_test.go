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

func newPinotExtTestServer() *Server {
	return &Server{
		store:       store.NewMemoryStore(),
		cfg:         &config.Config{TaskMaxRetries: 1, Demo: false, TrustGatewayHeaders: true},
		requireAuth: false,
	}
}

func pinotExtPost(t *testing.T, s *Server, path string, h http.HandlerFunc, body map[string]interface{}, tenant string) *httptest.ResponseRecorder {
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

func TestPinotExtSmoke(t *testing.T) {
	s := newPinotExtTestServer()

	rec := pinotExtPost(t, s, "/api/v1/db/pinot/ext", s.handlePinotExt, map[string]interface{}{
		"deviceID": "dev-1", "action": "nope",
	}, "default")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown action: got %d, want 400: %s", rec.Code, rec.Body.String())
	}

	rec = pinotExtPost(t, s, "/api/v1/db/pinot/ext", s.handlePinotExt, map[string]interface{}{
		"action": "status",
	}, "default")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing deviceID: got %d, want 400: %s", rec.Code, rec.Body.String())
	}

	rec = pinotExtPost(t, s, "/api/v1/db/pinot/ext", s.handlePinotExt, map[string]interface{}{
		"deviceID": "dev-1", "action": "status",
	}, "default")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("valid: got %d, want 202: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp["taskID"] == nil || resp["taskID"] == "" {
		t.Fatalf("valid: missing taskID: %s", rec.Body.String())
	}

	for _, p := range []string{`x;id`, `$(id)`, "`id`", "a b", "a/b"} {
		rec = pinotExtPost(t, s, "/api/v1/db/pinot/ext", s.handlePinotExt, map[string]interface{}{
			"deviceID": "dev-1", "action": "status", "table": p,
		}, "default")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("injection table=%q: got %d, want 400: %s", p, rec.Code, rec.Body.String())
		}
	}

	s.store.UpsertDevice(&proto.DeviceInfo{DeviceID: "dev-ct", TenantID: "other", State: "online"})
	rec = pinotExtPost(t, s, "/api/v1/db/pinot/ext", s.handlePinotExt, map[string]interface{}{
		"deviceID": "dev-ct", "action": "status",
	}, "default")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant: got %d, want 403: %s", rec.Code, rec.Body.String())
	}
}
