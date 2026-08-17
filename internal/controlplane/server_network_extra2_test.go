package controlplane

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"opsmesh/internal/config"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// 本文件补全 server_network.go 中 0% 覆盖的 probeEdges 和 handleNetworkDiagnoseResult。

// =============================================================================
// probeEdges
// =============================================================================

func TestProbeEdges_EmptyDevs_Extra(t *testing.T) {
	s := &Server{store: store.NewMemoryStore(), cfg: &config.Config{Demo: true}}
	edges := s.probeEdges("t1", nil)
	if len(edges) != 0 {
		t.Errorf("empty devs: got %d edges, want 0", len(edges))
	}
}

func TestProbeEdges_SingleDev_Extra(t *testing.T) {
	s := &Server{store: store.NewMemoryStore(), cfg: &config.Config{Demo: true}}
	devs := []proto.DeviceInfo{{DeviceID: "d1", AgentID: "a1", IP: "10.0.0.1"}}
	edges := s.probeEdges("t1", devs)
	if len(edges) != 0 {
		t.Errorf("single dev: got %d edges, want 0", len(edges))
	}
}

func TestProbeEdges_TwoDevs_Extra(t *testing.T) {
	s := &Server{store: store.NewMemoryStore(), cfg: &config.Config{Demo: true}}
	devs := []proto.DeviceInfo{
		{DeviceID: "d1", AgentID: "a1", IP: "10.0.0.1"},
		{DeviceID: "d2", AgentID: "a2", IP: "10.0.0.29"},
	}
	edges := s.probeEdges("t1", devs)
	if len(edges) != 1 {
		t.Errorf("two devs: got %d edges, want 1", len(edges))
	}
}

// =============================================================================
// handleNetworkDiagnoseResult
// =============================================================================

func TestNetworkDiagnoseResult_MethodNotAllowed_Extra(t *testing.T) {
	s := &Server{store: store.NewMemoryStore(), cfg: &config.Config{Demo: true}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/diagnose/x", nil)
	rec := httptest.NewRecorder()
	s.handleNetworkDiagnoseResult(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

func TestNetworkDiagnoseResult_EmptyTaskID_Extra(t *testing.T) {
	s := &Server{store: store.NewMemoryStore(), cfg: &config.Config{Demo: true}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/diagnose/", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleNetworkDiagnoseResult(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestNetworkDiagnoseResult_TaskNotFound_Extra(t *testing.T) {
	s := &Server{store: store.NewMemoryStore(), cfg: &config.Config{Demo: true}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/diagnose/nope", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleNetworkDiagnoseResult(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

func TestNetworkDiagnoseResult_Pending_Extra(t *testing.T) {
	s := &Server{store: store.NewMemoryStore(), cfg: &config.Config{Demo: true}}
	a := s.store.Register(&proto.AgentInfo{Segment: "seg", TenantID: "default"})
	tk := s.store.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "default", Type: "shell", Command: "ping x"})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/diagnose/"+tk.TaskID, nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleNetworkDiagnoseResult(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestNetworkDiagnoseResult_WithResult_Extra(t *testing.T) {
	s := &Server{store: store.NewMemoryStore(), cfg: &config.Config{Demo: true}}
	a := s.store.Register(&proto.AgentInfo{Segment: "seg", TenantID: "default"})
	tk := s.store.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "default", Type: "shell", Command: "ping x"})
	s.store.ClaimTask(a.AgentID)
	s.store.SubmitResult(&proto.TaskResult{TaskID: tk.TaskID, AgentID: a.AgentID, ExitCode: 0, Stdout: "ok"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/diagnose/"+tk.TaskID, nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleNetworkDiagnoseResult(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestNetworkDiagnoseResult_TenantMismatch_Extra(t *testing.T) {
	s := &Server{store: store.NewMemoryStore(), cfg: &config.Config{Demo: true}}
	a := s.store.Register(&proto.AgentInfo{Segment: "seg", TenantID: "default"})
	tk := s.store.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "default", Type: "shell", Command: "ping x"})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/diagnose/"+tk.TaskID, nil)
	req.Header.Set("X-Tenant-ID", "other")
	rec := httptest.NewRecorder()
	s.handleNetworkDiagnoseResult(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("tenant mismatch: %d, want 403", rec.Code)
	}
}
