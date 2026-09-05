package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Levango7/OpsMesh/services/runbook-svc/internal/runner"
	"github.com/Levango7/OpsMesh/services/runbook-svc/internal/service"
	"github.com/Levango7/OpsMesh/services/runbook-svc/internal/store"
)

// newTestHandler 构建基于内存实现的完整 handler（防回归锚专用）。
func newTestHandler() *Handler {
	svc := service.NewService(store.NewMemoryStore(), runner.NewRunner())
	return NewHandler(svc)
}

func newTestServer() *http.ServeMux {
	mux := http.NewServeMux()
	newTestHandler().RegisterRoutes(mux)
	return mux
}

func serveJSON(mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestCreateRunbookFrontendContract 防回归：前端契约 POST /api/v1/runbooks
// 发 {name,description,content,triggers}（无 enabled/steps），必须 201 且 enabled=true、content 保留，
// 否则后续 trigger 409。
func TestCreateRunbookFrontendContract(t *testing.T) {
	body := `{"name":"rb-1","description":"d","content":"echo hello\n# comment\n\necho world","triggers":[]}`
	rec := serveJSON(newTestServer(), http.MethodPost, "/api/v1/runbooks", body)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !created.Enabled {
		t.Fatal("expected enabled default true when field absent")
	}
	if created.Content == "" {
		t.Fatal("expected content to be persisted")
	}
}

// TestCreateRunbookExplicitlyDisabled 防回归：显式传 "enabled": false 必须被尊重（保持 201）。
func TestCreateRunbookExplicitlyDisabled(t *testing.T) {
	body := `{"name":"rb-off","content":"echo hi","enabled":false}`
	rec := serveJSON(newTestServer(), http.MethodPost, "/api/v1/runbooks", body)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if created.Enabled {
		t.Fatal("expected enabled false when explicitly set")
	}
}

// TestExecuteAliasAndLogs 防回归：前端契约动词链路
// POST /{id}/execute → GET /{id}/executions → GET /{id}/executions/{eid}/logs。
// content（无结构化 steps）必须降级为 shell 步骤执行并在 logs 中可见输出。
func TestExecuteAliasAndLogs(t *testing.T) {
	mux := newTestServer()

	// 1. create with content only
	body := `{"name":"rb-exec","description":"","content":"echo hello-ops","triggers":[]}`
	rec := serveJSON(mux, http.MethodPost, "/api/v1/runbooks", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// 2. execute alias (frontend contract verb)
	rec = serveJSON(mux, http.MethodPost, "/api/v1/runbooks/"+created.ID+"/execute", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from execute alias, got %d: %s", rec.Code, rec.Body.String())
	}
	var exec struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&exec); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if exec.ID == "" {
		t.Fatal("expected execution ID in response")
	}
	if exec.Status != "success" {
		t.Fatalf("expected success status (content degraded to steps), got %s", exec.Status)
	}

	// 3. executions alias
	rec = serveJSON(mux, http.MethodGet, "/api/v1/runbooks/"+created.ID+"/executions", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from executions alias, got %d: %s", rec.Code, rec.Body.String())
	}
	var list []struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(list) != 1 || list[0].ID != exec.ID {
		t.Fatalf("expected 1 execution with ID %s, got %+v", exec.ID, list)
	}

	// 4. logs endpoint
	rec = serveJSON(mux, http.MethodGet, "/api/v1/runbooks/"+created.ID+"/executions/"+exec.ID+"/logs", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from logs endpoint, got %d: %s", rec.Code, rec.Body.String())
	}
	var logs map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&logs); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if logs["logs"] != "hello-ops" {
		t.Fatalf("expected logs 'hello-ops', got %q", logs["logs"])
	}

	// 5. logs for unknown execution ID must be 404
	rec = serveJSON(mux, http.MethodGet, "/api/v1/runbooks/"+created.ID+"/executions/no-such-eid/logs", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown execution, got %d", rec.Code)
	}
}

// TestTriggerHistoryLegacyRoutes 防回归：原 trigger/history 路径保留兼容。
func TestTriggerHistoryLegacyRoutes(t *testing.T) {
	mux := newTestServer()

	body := `{"name":"rb-legacy","content":"echo legacy"}`
	rec := serveJSON(mux, http.MethodPost, "/api/v1/runbooks", body)
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	rec = serveJSON(mux, http.MethodPost, "/api/v1/runbooks/"+created.ID+"/trigger", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from legacy trigger, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = serveJSON(mux, http.MethodGet, "/api/v1/runbooks/"+created.ID+"/history", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from legacy history, got %d: %s", rec.Code, rec.Body.String())
	}
	var list []map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 history record, got %d", len(list))
	}
}
