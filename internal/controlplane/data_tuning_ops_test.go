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

func newDataTuningTestServer() *Server {
	return &Server{
		store:       store.NewMemoryStore(),
		cfg:         &config.Config{TaskMaxRetries: 1, Demo: false, TrustGatewayHeaders: true},
		requireAuth: false,
	}
}

func dataTuningRequest(t *testing.T, s *Server, h http.HandlerFunc, method, path string, body interface{}, tenant string) *httptest.ResponseRecorder {
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

func validDataTuningSpec() map[string]interface{} {
	return map[string]interface{}{
		"count":       3,
		"cores":       16,
		"ramGB":       64,
		"diskGB":      2000,
		"workload":    "batch",
		"dataScaleTB": 2,
		"replication": 3,
	}
}

func TestDataTuningProfiles(t *testing.T) {
	s := newDataTuningTestServer()
	rec := dataTuningRequest(t, s, s.handleDataTuningProfiles, http.MethodGet, "/api/v1/data/tuning/profiles", nil, "default")
	if rec.Code != http.StatusOK {
		t.Fatalf("profiles: got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Profiles   []string `json:"profiles"`
		Components []string `json:"components"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode profiles: %v", err)
	}
	if len(resp.Profiles) != 5 {
		t.Fatalf("profiles = %v, want 5 workloads", resp.Profiles)
	}
	found := false
	for _, p := range resp.Profiles {
		if p == "batch" {
			found = true
		}
	}
	if !found {
		t.Fatalf("profiles = %v, missing batch", resp.Profiles)
	}
	if len(resp.Components) != 6 {
		t.Fatalf("components = %v, want 6", resp.Components)
	}
}

func TestDataTuningRecommendInvalidWorkload(t *testing.T) {
	s := newDataTuningTestServer()
	spec := validDataTuningSpec()
	spec["workload"] = "foo"
	rec := dataTuningRequest(t, s, s.handleDataTuningRecommend, http.MethodPost, "/api/v1/data/tuning/recommend",
		spec, "default")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid workload: got %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestDataTuningRecommendYAML(t *testing.T) {
	s := newDataTuningTestServer()
	body := validDataTuningSpec()
	rec := dataTuningRequest(t, s, s.handleDataTuningRecommend, http.MethodPost, "/api/v1/data/tuning/recommend", body, "default")
	if rec.Code != http.StatusOK {
		t.Fatalf("recommend: got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Profile    string                       `json:"profile"`
		Components map[string]map[string]string `json:"components"`
		Rendered   string                       `json:"rendered"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode recommend: %v", err)
	}
	if resp.Profile != "batch-3n" {
		t.Fatalf("profile = %q, want batch-3n", resp.Profile)
	}
	if resp.Rendered == "" || !strings.Contains(resp.Rendered, "yarn:") || !strings.Contains(resp.Rendered, "kafka:") {
		t.Fatalf("rendered YAML missing sections:\n%s", resp.Rendered)
	}
	if len(resp.Components["yarn"]) == 0 {
		t.Fatal("components.yarn is empty")
	}
}

func TestDataTuningApplyMissingDeviceID(t *testing.T) {
	s := newDataTuningTestServer()
	rec := dataTuningRequest(t, s, s.handleDataTuningApply, http.MethodPost, "/api/v1/data/tuning/apply",
		map[string]interface{}{"component": "yarn", "spec": validDataTuningSpec()}, "default")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing deviceID: got %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestDataTuningApplyInvalidSpec(t *testing.T) {
	s := newDataTuningTestServer()
	spec := validDataTuningSpec()
	spec["count"] = 0
	rec := dataTuningRequest(t, s, s.handleDataTuningApply, http.MethodPost, "/api/v1/data/tuning/apply",
		map[string]interface{}{"deviceID": "dev-1", "component": "yarn", "spec": spec, "dryRun": true}, "default")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid spec: got %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestDataTuningApplyCrossTenant(t *testing.T) {
	s := newDataTuningTestServer()
	s.store.UpsertDevice(&proto.DeviceInfo{DeviceID: "dev-ct", TenantID: "other", State: "online"})
	rec := dataTuningRequest(t, s, s.handleDataTuningApply, http.MethodPost, "/api/v1/data/tuning/apply",
		map[string]interface{}{"deviceID": "dev-ct", "component": "yarn", "spec": validDataTuningSpec(), "dryRun": true}, "default")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant: got %d, want 403: %s", rec.Code, rec.Body.String())
	}
}

func TestDataTuningApplyDryRun(t *testing.T) {
	s := newDataTuningTestServer()
	rec := dataTuningRequest(t, s, s.handleDataTuningApply, http.MethodPost, "/api/v1/data/tuning/apply",
		map[string]interface{}{"deviceID": "dev-1", "component": "hdfs", "spec": validDataTuningSpec(), "dryRun": true}, "default")
	if rec.Code != http.StatusOK {
		t.Fatalf("dryRun: got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode dryRun: %v", err)
	}
	if _, ok := resp["taskID"]; ok {
		t.Fatalf("dryRun must not return taskID: %s", rec.Body.String())
	}
	rendered, _ := resp["rendered"].(string)
	if !strings.Contains(rendered, "dfs.blocksize=128m") || !strings.Contains(rendered, "dfs.replication=3") {
		t.Fatalf("dryRun rendered text missing HDFS params:\n%s", rendered)
	}
}

func TestDataTuningApplyDispatchesTask(t *testing.T) {
	s := newDataTuningTestServer()
	rec := dataTuningRequest(t, s, s.handleDataTuningApply, http.MethodPost, "/api/v1/data/tuning/apply",
		map[string]interface{}{"deviceID": "dev-1", "component": "kafka", "spec": validDataTuningSpec()}, "default")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("apply: got %d, want 202: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode apply: %v", err)
	}
	if id, _ := resp["taskID"].(string); id == "" {
		t.Fatalf("apply: missing taskID: %s", rec.Body.String())
	}
	tasks := s.store.AllTasks("default")
	found := false
	for _, task := range tasks {
		if strings.Contains(task.Command, "/etc/kafka/conf/opsmesh-tuning.properties") &&
			strings.Contains(task.Command, "cp -a") &&
			strings.Contains(task.Command, "log.retention.hours=168") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("apply task not found in store (n=%d)", len(tasks))
	}
}

func TestDataTuningApplyRejectsInjection(t *testing.T) {
	s := newDataTuningTestServer()
	for _, component := range []string{"yarn;rm -rf /", "../../etc/yarn", "yarn\nx", "unknown"} {
		rec := dataTuningRequest(t, s, s.handleDataTuningApply, http.MethodPost, "/api/v1/data/tuning/apply",
			map[string]interface{}{"deviceID": "dev-1", "component": component, "spec": validDataTuningSpec(), "dryRun": true}, "default")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("component %q: got %d, want 400: %s", component, rec.Code, rec.Body.String())
		}
	}
}
