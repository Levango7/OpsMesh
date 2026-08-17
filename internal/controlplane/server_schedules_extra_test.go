package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opsmesh/internal/config"
	"opsmesh/internal/cron"
	"opsmesh/internal/store"
)

// 本文件补全 server_schedules.go 中 0% 覆盖的定时任务 handler：
//   - handleScheduleRouting / scheduleGet / scheduleUpdate / scheduleDelete
//   - schedulePause / scheduleResume
//
// 注：handleSchedules / scheduleList / scheduleCreate 已在 integration_m5_test.go 有覆盖。

// newScheduleTestServer 构造带 scheduleMgr 的测试控制面。
func newScheduleTestServer() *Server {
	return &Server{
		store:       store.NewMemoryStore(),
		cfg:         &config.Config{Demo: true},
		requireAuth: false,
		scheduleMgr: cron.NewManager(),
	}
}

// createSchedule 通过 API 创建一个定时任务，返回其 ID。
func createSchedule(t *testing.T, s *Server) string {
	t.Helper()
	body := `{"taskID":"tpl-1","name":"test","cronExpr":"*/5 * * * *"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/schedules", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-ID", "u1")
	rec := httptest.NewRecorder()
	s.handleSchedules(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup schedule: %d body=%s", rec.Code, rec.Body.String())
	}
	var e cron.ScheduleEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return e.ID
}

// =============================================================================
// handleScheduleRouting / scheduleGet / scheduleUpdate / scheduleDelete
// =============================================================================

func TestScheduleRouting_GetUpdateDelete(t *testing.T) {
	s := newScheduleTestServer()
	id := createSchedule(t, s)

	// GET
	gReq := httptest.NewRequest(http.MethodGet, "/api/v1/schedules/"+id, nil)
	gReq.Header.Set("X-Tenant-ID", "default")
	gRec := httptest.NewRecorder()
	s.handleScheduleRouting(gRec, gReq)
	if gRec.Code != http.StatusOK {
		t.Fatalf("get: %d body=%s", gRec.Code, gRec.Body.String())
	}

	// PUT
	updBody := `{"name":"updated","cronExpr":"*/10 * * * *","status":"active"}`
	pReq := httptest.NewRequest(http.MethodPut, "/api/v1/schedules/"+id, strings.NewReader(updBody))
	pReq.Header.Set("X-Tenant-ID", "default")
	pRec := httptest.NewRecorder()
	s.handleScheduleRouting(pRec, pReq)
	if pRec.Code != http.StatusOK {
		t.Fatalf("update: %d body=%s", pRec.Code, pRec.Body.String())
	}

	// DELETE
	dReq := httptest.NewRequest(http.MethodDelete, "/api/v1/schedules/"+id, nil)
	dReq.Header.Set("X-Tenant-ID", "default")
	dRec := httptest.NewRecorder()
	s.handleScheduleRouting(dRec, dReq)
	if dRec.Code != http.StatusOK {
		t.Fatalf("delete: %d body=%s", dRec.Code, dRec.Body.String())
	}
}

func TestScheduleRouting_EmptyID(t *testing.T) {
	s := newScheduleTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/schedules/", nil)
	rec := httptest.NewRecorder()
	s.handleScheduleRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestScheduleRouting_MethodNotAllowed(t *testing.T) {
	s := newScheduleTestServer()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/schedules/x", nil)
	rec := httptest.NewRecorder()
	s.handleScheduleRouting(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

func TestScheduleRouting_UnknownSubPath(t *testing.T) {
	s := newScheduleTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/schedules/x/unknown", nil)
	rec := httptest.NewRecorder()
	s.handleScheduleRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

func TestScheduleGet_NotFound(t *testing.T) {
	s := newScheduleTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/schedules/nope", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleScheduleRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

func TestScheduleGet_TenantMismatch(t *testing.T) {
	s := newScheduleTestServer()
	id := createSchedule(t, s)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/schedules/"+id, nil)
	req.Header.Set("X-Tenant-ID", "other")
	rec := httptest.NewRecorder()
	s.handleScheduleRouting(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("tenant mismatch: %d, want 403", rec.Code)
	}
}

func TestScheduleUpdate_BadJSON(t *testing.T) {
	s := newScheduleTestServer()
	id := createSchedule(t, s)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/schedules/"+id, strings.NewReader("not json"))
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleScheduleRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestScheduleUpdate_NotFound(t *testing.T) {
	s := newScheduleTestServer()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/schedules/nope", strings.NewReader(`{}`))
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleScheduleRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestScheduleDelete_NotFound(t *testing.T) {
	s := newScheduleTestServer()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/schedules/nope", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleScheduleRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

// =============================================================================
// schedulePause / scheduleResume
// =============================================================================

func TestSchedulePause_Happy(t *testing.T) {
	s := newScheduleTestServer()
	id := createSchedule(t, s)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/schedules/"+id+"/pause", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleScheduleRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("pause: %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSchedulePause_NotFound(t *testing.T) {
	s := newScheduleTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/schedules/nope/pause", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleScheduleRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

func TestSchedulePause_MethodNotAllowed(t *testing.T) {
	s := newScheduleTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/schedules/x/pause", nil)
	rec := httptest.NewRecorder()
	s.handleScheduleRouting(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

func TestScheduleResume_Happy(t *testing.T) {
	s := newScheduleTestServer()
	id := createSchedule(t, s)
	// 先暂停
	pReq := httptest.NewRequest(http.MethodPost, "/api/v1/schedules/"+id+"/pause", nil)
	pReq.Header.Set("X-Tenant-ID", "default")
	s.handleScheduleRouting(httptest.NewRecorder(), pReq)
	// 再恢复
	req := httptest.NewRequest(http.MethodPost, "/api/v1/schedules/"+id+"/resume", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleScheduleRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("resume: %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestScheduleResume_NotFound(t *testing.T) {
	s := newScheduleTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/schedules/nope/resume", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleScheduleRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

func TestScheduleResume_MethodNotAllowed(t *testing.T) {
	s := newScheduleTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/schedules/x/resume", nil)
	rec := httptest.NewRecorder()
	s.handleScheduleRouting(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}
