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

func newBigDataSecurityTestServer() *Server {
	return &Server{
		store:       store.NewMemoryStore(),
		cfg:         &config.Config{TaskMaxRetries: 1, Demo: false, TrustGatewayHeaders: true},
		requireAuth: false,
	}
}

func bigDataSecurityPost(t *testing.T, s *Server, body map[string]interface{}, tenant string) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/db/bigdata/security", strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenant)
	req.Header.Set("X-User-Id", "audit")
	req.Header.Set("X-User-Roles", "admin")
	rec := httptest.NewRecorder()
	s.handleBigDataSecurity(rec, req)
	return rec
}

func TestBigDataSecurityTable(t *testing.T) {
	s := newBigDataSecurityTestServer()

	cases := []struct {
		name   string
		body   map[string]interface{}
		tenant string
		want   int
	}{
		{"unknown action", map[string]interface{}{"deviceID": "dev-1", "type": "kerberos", "action": "nope"}, "default", http.StatusBadRequest},
		{"missing deviceID", map[string]interface{}{"type": "kerberos", "action": "kdc-status"}, "default", http.StatusBadRequest},
		{"valid", map[string]interface{}{"deviceID": "dev-1", "type": "kerberos", "action": "kdc-status"}, "default", http.StatusAccepted},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := bigDataSecurityPost(t, s, c.body, c.tenant)
			if rec.Code != c.want {
				t.Fatalf("%s: got %d, want %d: %s", c.name, rec.Code, c.want, rec.Body.String())
			}
			if c.want == http.StatusAccepted {
				var resp map[string]interface{}
				if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp["taskID"] == nil || resp["taskID"] == "" {
					t.Fatalf("valid: missing taskID: %s", rec.Body.String())
				}
			}
		})
	}

	for _, p := range []string{`x;id`, `$(id)`, "`id`", "a b", "a/b"} {
		rec := bigDataSecurityPost(t, s, map[string]interface{}{"deviceID": "dev-1", "type": "kerberos", "action": "kdc-status", "name": p}, "default")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("injection name=%q: got %d, want 400: %s", p, rec.Code, rec.Body.String())
		}
	}

	s.store.UpsertDevice(&proto.DeviceInfo{DeviceID: "dev-ct", TenantID: "other", State: "online"})
	rec := bigDataSecurityPost(t, s, map[string]interface{}{"deviceID": "dev-ct", "type": "kerberos", "action": "kdc-status"}, "default")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant: got %d, want 403: %s", rec.Code, rec.Body.String())
	}
}
