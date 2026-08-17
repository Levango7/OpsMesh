package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opsmesh/internal/config"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// 本文件补全 server_batch.go 中 0% 覆盖的批量/灰度 handler：
//   - handleBatchStatus / handleCanaryCreate / planCanaryPhases / execCanaryPhase
//   - itoaPhase / handleCanaryStatus / handleCanaryAdvance
//   - handleBatchRouting / handleCanaryRouting
//
// 测试模式：构造 Server{batches: newBatchStore(), Demo: true}，用 httptest 发请求。

// newBatchTestServer 构造带批量索引的测试控制面。
func newBatchTestServer() *Server {
	return &Server{
		store:       store.NewMemoryStore(),
		cfg:         &config.Config{Demo: true, TaskMaxRetries: 3},
		requireAuth: false,
		batches:     newBatchStore(),
	}
}

// =============================================================================
// handleBatchExec / handleBatchStatus / handleBatchRouting
// =============================================================================

func TestBatchExec_HappyWithAgent(t *testing.T) {
	s := newBatchTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	body := `{"deviceIDs":["` + a.AgentID + `"],"command":"echo hi","taskType":"shell"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/batch-exec", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-ID", "u1")
	rec := httptest.NewRecorder()
	s.handleBatchExec(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		BatchID string          `json:"batchID"`
		Tasks   []batchTaskItem `json:"tasks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.BatchID == "" {
		t.Error("batchID empty")
	}
	if len(resp.Tasks) != 1 || resp.Tasks[0].Status != "pending" {
		t.Errorf("tasks=%+v", resp.Tasks)
	}
}

func TestBatchExec_AgentNotFound(t *testing.T) {
	s := newBatchTestServer()
	body := `{"deviceIDs":["nope"],"command":"echo hi"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/batch-exec", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleBatchExec(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Tasks []batchTaskItem `json:"tasks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Tasks) != 1 || resp.Tasks[0].Error == "" {
		t.Errorf("expect error for missing agent, got %+v", resp.Tasks)
	}
}

func TestBatchExec_BadJSON(t *testing.T) {
	s := newBatchTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/batch-exec", strings.NewReader("not json"))
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleBatchExec(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestBatchExec_EmptyDevices(t *testing.T) {
	s := newBatchTestServer()
	body := `{"deviceIDs":[],"command":"echo"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/batch-exec", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleBatchExec(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestBatchExec_EmptyCommand(t *testing.T) {
	s := newBatchTestServer()
	body := `{"deviceIDs":["d1"],"command":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/batch-exec", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleBatchExec(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestBatchExec_MethodNotAllowed(t *testing.T) {
	s := newBatchTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/batch-exec", nil)
	rec := httptest.NewRecorder()
	s.handleBatchExec(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

func TestBatchStatus_Happy(t *testing.T) {
	s := newBatchTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	body := `{"deviceIDs":["` + a.AgentID + `"],"command":"echo hi"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/batch-exec", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleBatchExec(rec, req)
	var create struct {
		BatchID string `json:"batchID"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &create); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	// 查询状态
	sReq := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/batch/"+create.BatchID, nil)
	sReq.Header.Set("X-Tenant-ID", "default")
	sRec := httptest.NewRecorder()
	s.handleBatchStatus(sRec, sReq, create.BatchID)
	if sRec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", sRec.Code, sRec.Body.String())
	}
}

func TestBatchStatus_NotFound(t *testing.T) {
	s := newBatchTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/batch/nope", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleBatchStatus(rec, req, "nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

func TestBatchStatus_TenantMismatch(t *testing.T) {
	s := newBatchTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	body := `{"deviceIDs":["` + a.AgentID + `"],"command":"echo hi"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/batch-exec", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleBatchExec(rec, req)
	var create struct {
		BatchID string `json:"batchID"`
	}
	json.Unmarshal(rec.Body.Bytes(), &create)

	sReq := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/batch/"+create.BatchID, nil)
	sReq.Header.Set("X-Tenant-ID", "other")
	sRec := httptest.NewRecorder()
	s.handleBatchStatus(sRec, sReq, create.BatchID)
	if sRec.Code != http.StatusForbidden {
		t.Fatalf("tenant mismatch: %d, want 403", sRec.Code)
	}
}

func TestBatchStatus_MethodNotAllowed(t *testing.T) {
	s := newBatchTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/batch/x", nil)
	rec := httptest.NewRecorder()
	s.handleBatchStatus(rec, req, "x")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

func TestBatchRouting_Happy(t *testing.T) {
	s := newBatchTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	body := `{"deviceIDs":["` + a.AgentID + `"],"command":"echo hi"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/batch-exec", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleBatchExec(rec, req)
	var create struct {
		BatchID string `json:"batchID"`
	}
	json.Unmarshal(rec.Body.Bytes(), &create)

	rReq := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/batch/"+create.BatchID, nil)
	rReq.Header.Set("X-Tenant-ID", "default")
	rRec := httptest.NewRecorder()
	s.handleBatchRouting(rRec, rReq)
	if rRec.Code != http.StatusOK {
		t.Fatalf("routing: %d body=%s", rRec.Code, rRec.Body.String())
	}
}

func TestBatchRouting_EmptyID(t *testing.T) {
	s := newBatchTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/batch/", nil)
	rec := httptest.NewRecorder()
	s.handleBatchRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

// =============================================================================
// handleCanaryCreate / planCanaryPhases / handleCanaryStatus / handleCanaryAdvance
// =============================================================================

func TestCanaryCreate_Percentage(t *testing.T) {
	s := newBatchTestServer()
	s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	body := `{"deviceIDs":["d1","d2","d3","d4"],"command":"echo hi","strategy":"percentage","percentage":50}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/canary", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-ID", "u1")
	rec := httptest.NewRecorder()
	s.handleCanaryCreate(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		CanaryID string                   `json:"canaryID"`
		Phases   []map[string]interface{} `json:"phases"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.CanaryID == "" || len(resp.Phases) == 0 {
		t.Errorf("canary=%+v", resp)
	}
}

func TestCanaryCreate_Group(t *testing.T) {
	s := newBatchTestServer()
	body := `{"deviceIDs":["d1","d2","d3","d4"],"command":"echo hi","strategy":"group","groups":["g1","g2"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/canary", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleCanaryCreate(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestCanaryCreate_Label(t *testing.T) {
	s := newBatchTestServer()
	body := `{"deviceIDs":["d1","d2"],"command":"echo hi","strategy":"label","labels":{"env":"canary"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/canary", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleCanaryCreate(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestCanaryCreate_BadJSON(t *testing.T) {
	s := newBatchTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/canary", strings.NewReader("not json"))
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleCanaryCreate(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestCanaryCreate_EmptyDevices(t *testing.T) {
	s := newBatchTestServer()
	body := `{"deviceIDs":[],"command":"echo","strategy":"percentage"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/canary", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleCanaryCreate(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestCanaryCreate_EmptyCommand(t *testing.T) {
	s := newBatchTestServer()
	body := `{"deviceIDs":["d1"],"command":"","strategy":"percentage"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/canary", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleCanaryCreate(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestCanaryCreate_InvalidStrategy(t *testing.T) {
	s := newBatchTestServer()
	body := `{"deviceIDs":["d1"],"command":"echo","strategy":"invalid"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/canary", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleCanaryCreate(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestCanaryCreate_MethodNotAllowed(t *testing.T) {
	s := newBatchTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/canary", nil)
	rec := httptest.NewRecorder()
	s.handleCanaryCreate(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

// setupCanary 创建一个灰度发布并返回 canaryID。
func setupCanary(t *testing.T) *Server {
	t.Helper()
	s := newBatchTestServer()
	s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	body := `{"deviceIDs":["d1","d2"],"command":"echo hi","strategy":"percentage","percentage":50}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/canary", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-ID", "u1")
	rec := httptest.NewRecorder()
	s.handleCanaryCreate(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup canary: %d body=%s", rec.Code, rec.Body.String())
	}
	return s
}

func TestCanaryStatus_NotFound(t *testing.T) {
	s := newBatchTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/canary/nope", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleCanaryStatus(rec, req, "nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

func TestCanaryStatus_MethodNotAllowed(t *testing.T) {
	s := newBatchTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/canary/x", nil)
	rec := httptest.NewRecorder()
	s.handleCanaryStatus(rec, req, "x")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

func TestCanaryAdvance_NotFound(t *testing.T) {
	s := newBatchTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/canary/nope/advance", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleCanaryAdvance(rec, req, "nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

func TestCanaryAdvance_MethodNotAllowed(t *testing.T) {
	s := newBatchTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/canary/x/advance", nil)
	rec := httptest.NewRecorder()
	s.handleCanaryAdvance(rec, req, "x")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

func TestCanaryRouting_EmptyID(t *testing.T) {
	s := newBatchTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/canary/", nil)
	rec := httptest.NewRecorder()
	s.handleCanaryRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

func TestCanaryRouting_UnknownSubPath(t *testing.T) {
	s := newBatchTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/canary/x/unknown", nil)
	rec := httptest.NewRecorder()
	s.handleCanaryRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

// =============================================================================
// planCanaryPhases / itoaPhase 单元测试
// =============================================================================

func TestPlanCanaryPhases_Percentage(t *testing.T) {
	phases := planCanaryPhases([]string{"d1", "d2", "d3", "d4"}, "percentage", 50, nil, nil)
	if len(phases) != 2 {
		t.Errorf("percentage 50: expect 2 phases, got %d", len(phases))
	}
}

func TestPlanCanaryPhases_PercentageFull(t *testing.T) {
	phases := planCanaryPhases([]string{"d1", "d2"}, "percentage", 100, nil, nil)
	if len(phases) != 1 {
		t.Errorf("percentage 100: expect 1 phase, got %d", len(phases))
	}
}

func TestPlanCanaryPhases_PercentageDefault(t *testing.T) {
	phases := planCanaryPhases([]string{"d1", "d2", "d3"}, "percentage", 0, nil, nil)
	if len(phases) < 1 {
		t.Errorf("percentage 0 default: expect phases, got %d", len(phases))
	}
}

func TestPlanCanaryPhases_Group(t *testing.T) {
	phases := planCanaryPhases([]string{"d1", "d2", "d3", "d4"}, "group", 0, []string{"g1", "g2"}, nil)
	if len(phases) != 2 {
		t.Errorf("group 2: expect 2 phases, got %d", len(phases))
	}
}

func TestPlanCanaryPhases_Label(t *testing.T) {
	phases := planCanaryPhases([]string{"d1", "d2"}, "label", 0, nil, map[string]string{"env": "canary"})
	if len(phases) != 1 {
		t.Errorf("label: expect 1 phase, got %d", len(phases))
	}
}

func TestPlanCanaryPhases_UnknownStrategy(t *testing.T) {
	phases := planCanaryPhases([]string{"d1"}, "unknown", 0, nil, nil)
	if len(phases) != 1 {
		t.Errorf("unknown: expect 1 phase fallback, got %d", len(phases))
	}
}

func TestItoaPhase(t *testing.T) {
	tests := []struct {
		in   int
		want string
	}{
		{0, "0"}, {1, "1"}, {12, "12"}, {123, "123"}, {-5, "-5"},
	}
	for _, tt := range tests {
		got := itoaPhase(tt.in)
		if got != tt.want {
			t.Errorf("itoaPhase(%d)=%q, want %q", tt.in, got, tt.want)
		}
	}
}
