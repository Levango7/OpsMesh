package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opsmesh/internal/cmdb"
	"opsmesh/internal/config"
	"opsmesh/internal/store"
)

// 本文件补全 cmdb_approval.go 中 0% 覆盖的 handler：
//   - handleCMDBChanges / cmdbChangeList / cmdbChangeSubmit
//   - handleCMDBChangeRouting / cmdbChangeGet / cmdbChangeApprove / cmdbChangeReject

// newCMDBApprovalTestServer 构造带 CMDB 审批管理器的测试控制面。
func newCMDBApprovalTestServer() *Server {
	st := store.NewMemoryStore()
	ci := cmdb.NewMemoryCiStore()
	return &Server{
		store:           st,
		cfg:             &config.Config{Demo: true},
		requireAuth:     false,
		cmdbApprovalMgr: NewCMDBApprovalManager(st, ci),
	}
}

// =============================================================================
// handleCMDBChanges / cmdbChangeList / cmdbChangeSubmit
// =============================================================================

func TestCMDBChangeSubmit_Happy(t *testing.T) {
	s := newCMDBApprovalTestServer()
	body := `{"action":"create","ciType":"machine","changes":{"name":"host1"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/changes", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-ID", "u1")
	rec := httptest.NewRecorder()
	s.handleCMDBChanges(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit: %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCMDBChangeSubmit_BadJSON(t *testing.T) {
	s := newCMDBApprovalTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/changes", strings.NewReader("not json"))
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleCMDBChanges(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestCMDBChangeList_Happy(t *testing.T) {
	s := newCMDBApprovalTestServer()
	// 先提交一个变更
	body := `{"action":"create","ciType":"machine","changes":{"name":"host1"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/changes", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-ID", "u1")
	s.handleCMDBChanges(httptest.NewRecorder(), req)

	// 列表
	lReq := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/changes", nil)
	lReq.Header.Set("X-Tenant-ID", "default")
	lRec := httptest.NewRecorder()
	s.handleCMDBChanges(lRec, lReq)
	if lRec.Code != http.StatusOK {
		t.Fatalf("list: %d body=%s", lRec.Code, lRec.Body.String())
	}
	var resp struct {
		Changes []*CMDBChangeRequest `json:"changes"`
		Total   int                  `json:"total"`
	}
	if err := json.Unmarshal(lRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total == 0 {
		t.Error("expect changes, got 0")
	}
}

func TestCMDBChangeList_PendingFilter(t *testing.T) {
	s := newCMDBApprovalTestServer()
	body := `{"action":"create","ciType":"machine","changes":{"name":"host1"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/changes", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-ID", "u1")
	s.handleCMDBChanges(httptest.NewRecorder(), req)

	lReq := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/changes?status=pending", nil)
	lReq.Header.Set("X-Tenant-ID", "default")
	lRec := httptest.NewRecorder()
	s.handleCMDBChanges(lRec, lReq)
	if lRec.Code != http.StatusOK {
		t.Fatalf("list pending: %d", lRec.Code)
	}
}

func TestHandleCMDBChanges_MethodNotAllowed(t *testing.T) {
	s := newCMDBApprovalTestServer()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/cmdb/changes", nil)
	rec := httptest.NewRecorder()
	s.handleCMDBChanges(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

// =============================================================================
// handleCMDBChangeRouting / cmdbChangeGet / cmdbChangeApprove / cmdbChangeReject
// =============================================================================

// setupCMDBChange 提交一个变更并返回其 ID。
func setupCMDBChange(t *testing.T) (*Server, string) {
	t.Helper()
	s := newCMDBApprovalTestServer()
	body := `{"action":"create","ciType":"machine","changes":{"name":"host1"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/changes", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-ID", "u1")
	rec := httptest.NewRecorder()
	s.handleCMDBChanges(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup: %d body=%s", rec.Code, rec.Body.String())
	}
	var chg CMDBChangeRequest
	if err := json.Unmarshal(rec.Body.Bytes(), &chg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return s, chg.ID
}

func TestCMDBChangeGet_Happy(t *testing.T) {
	s, id := setupCMDBChange(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/changes/"+id, nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleCMDBChangeRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCMDBChangeGet_NotFound(t *testing.T) {
	s := newCMDBApprovalTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/changes/nope", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleCMDBChangeRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

func TestCMDBChangeGet_TenantMismatch(t *testing.T) {
	s, id := setupCMDBChange(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/changes/"+id, nil)
	req.Header.Set("X-Tenant-ID", "other")
	rec := httptest.NewRecorder()
	s.handleCMDBChangeRouting(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("tenant mismatch: %d, want 403", rec.Code)
	}
}

func TestCMDBChangeApprove_Happy(t *testing.T) {
	s, id := setupCMDBChange(t)
	body := `{"comment":"lgtm"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/changes/"+id+"/approve", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-ID", "u2")
	rec := httptest.NewRecorder()
	s.handleCMDBChangeRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve: %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCMDBChangeApprove_NotFound(t *testing.T) {
	s := newCMDBApprovalTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/changes/nope/approve", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleCMDBChangeRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

func TestCMDBChangeApprove_TenantMismatch(t *testing.T) {
	s, id := setupCMDBChange(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/changes/"+id+"/approve", nil)
	req.Header.Set("X-Tenant-ID", "other")
	rec := httptest.NewRecorder()
	s.handleCMDBChangeRouting(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("tenant mismatch: %d, want 403", rec.Code)
	}
}

func TestCMDBChangeReject_Happy(t *testing.T) {
	s, id := setupCMDBChange(t)
	body := `{"comment":"nope"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/changes/"+id+"/reject", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-ID", "u2")
	rec := httptest.NewRecorder()
	s.handleCMDBChangeRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("reject: %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCMDBChangeReject_NotFound(t *testing.T) {
	s := newCMDBApprovalTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/changes/nope/reject", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleCMDBChangeRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

func TestCMDBChangeReject_TenantMismatch(t *testing.T) {
	s, id := setupCMDBChange(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/changes/"+id+"/reject", nil)
	req.Header.Set("X-Tenant-ID", "other")
	rec := httptest.NewRecorder()
	s.handleCMDBChangeRouting(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("tenant mismatch: %d, want 403", rec.Code)
	}
}

func TestCMDBChangeRouting_EmptyID(t *testing.T) {
	s := newCMDBApprovalTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/changes/", nil)
	rec := httptest.NewRecorder()
	s.handleCMDBChangeRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestCMDBChangeRouting_UnknownSubPath(t *testing.T) {
	s := newCMDBApprovalTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/changes/x/unknown", nil)
	rec := httptest.NewRecorder()
	s.handleCMDBChangeRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

func TestCMDBChangeRouting_GetMethodNotAllowed(t *testing.T) {
	s := newCMDBApprovalTestServer()
	// POST on /changes/{id} (no sub) should be 405
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/changes/x", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleCMDBChangeRouting(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

func TestCMDBChangeRouting_ApproveMethodNotAllowed(t *testing.T) {
	s := newCMDBApprovalTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/changes/x/approve", nil)
	rec := httptest.NewRecorder()
	s.handleCMDBChangeRouting(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}
