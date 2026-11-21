package controlplane

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

func newDBPGGISTestServer() *Server {
	return &Server{
		store:       store.NewMemoryStore(),
		cfg:         &config.Config{TaskMaxRetries: 1, Demo: false, TrustGatewayHeaders: true},
		requireAuth: false,
	}
}

func TestDBPGGIS(t *testing.T) {
	s := newDBPGGISTestServer()

	rec := dbpgPost(t, s, "/api/v1/db/postgresql/gis", s.handleDBPGGIS, map[string]interface{}{
		"deviceID": "dev-1", "action": "nope",
	}, "default")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown action: got %d, want 400: %s", rec.Code, rec.Body.String())
	}

	rec = dbpgPost(t, s, "/api/v1/db/postgresql/gis", s.handleDBPGGIS, map[string]interface{}{
		"action": "postgis-check",
	}, "default")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing deviceID: got %d, want 400: %s", rec.Code, rec.Body.String())
	}

	rec = dbpgPost(t, s, "/api/v1/db/postgresql/gis", s.handleDBPGGIS, map[string]interface{}{
		"deviceID": "dev-1", "action": "postgis-check",
	}, "default")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("valid: got %d, want 202: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp["taskID"] == nil || resp["taskID"] == "" {
		t.Fatalf("valid: missing taskID: %s", rec.Body.String())
	}

	for _, p := range []string{`x;id`, `$(id)`, "`id`", "a b", "a/b"} {
		rec = dbpgPost(t, s, "/api/v1/db/postgresql/gis", s.handleDBPGGIS, map[string]interface{}{
			"deviceID": "dev-1", "action": "spatial-index-create", "tableName": p,
		}, "default")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("injection tableName=%q: got %d, want 400: %s", p, rec.Code, rec.Body.String())
		}
	}
	for _, p := range []string{"nope", "invalid"} {
		rec = dbpgPost(t, s, "/api/v1/db/postgresql/gis", s.handleDBPGGIS, map[string]interface{}{
			"deviceID": "dev-1", "action": "spatial-index-create", "tableName": "t1", "indexType": p,
		}, "default")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("invalid indexType=%q: got %d, want 400", p, rec.Code)
		}
	}

	s.store.UpsertDevice(&proto.DeviceInfo{DeviceID: "dev-ct", TenantID: "other", State: "online"})
	rec = dbpgPost(t, s, "/api/v1/db/postgresql/gis", s.handleDBPGGIS, map[string]interface{}{
		"deviceID": "dev-ct", "action": "postgis-check",
	}, "default")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant: got %d, want 403: %s", rec.Code, rec.Body.String())
	}
}
