package controlplane

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"opsmesh/internal/config"
	"opsmesh/internal/domain"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// newTestServer 构造一个无总线/无指标的测试控制面（白盒，直接装配 Registry）。
func newTestServer() *Server {
	st := store.NewMemoryStore().WithDemo(true)
	return &Server{
		reg:         NewRegistryWithStore(st),
		cfg:         &config.Config{},
		requireAuth: false,
	}
}

// TestHandleListTasks 验证 GET /api/v1/tasks 返回全部任务（含预置与下发）。
func TestHandleListTasks(t *testing.T) {
	s := newTestServer()
	a := s.reg.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	s.reg.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "echo hi"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
	w := httptest.NewRecorder()
	s.handleListTasks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var tasks []*domain.Task
	if err := json.NewDecoder(w.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(tasks) != 2 { // 预置 1 + 下发 1
		t.Fatalf("tasks=%d, want 2", len(tasks))
	}
}

// TestHandleListTasks_StatusFilter 验证 ?status=done 过滤。
func TestHandleListTasks_StatusFilter(t *testing.T) {
	s := newTestServer()
	a := s.reg.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	s.reg.SubmitResult(&proto.TaskResult{TaskID: "task-" + a.AgentID + "-1", AgentID: a.AgentID, ExitCode: 0})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks?status=pending", nil)
	w := httptest.NewRecorder()
	s.handleListTasks(w, req)
	var tasks []*domain.Task
	json.NewDecoder(w.Body).Decode(&tasks)
	if len(tasks) != 0 {
		t.Fatalf("status=pending 应过滤掉 done 任务，得到 %d", len(tasks))
	}
}

// TestHandleDeviceDetail 验证 GET /api/v1/devices/{id} 返回设备详情 + 任务 + 结果。
func TestHandleDeviceDetail(t *testing.T) {
	s := newTestServer()
	a := s.reg.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	devID := "dev-" + a.AgentID
	s.reg.SubmitResult(&proto.TaskResult{TaskID: "task-" + a.AgentID + "-1", AgentID: a.AgentID, ExitCode: 0, Stdout: "ok"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices/"+devID, nil)
	w := httptest.NewRecorder()
	s.handleDeviceDetail(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var dd struct {
		Device  *domain.Device      `json:"device"`
		Tasks   []*domain.Task      `json:"tasks"`
		Results []*domain.TaskResult `json:"results"`
	}
	if err := json.NewDecoder(w.Body).Decode(&dd); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dd.Device == nil || dd.Device.DeviceID != devID {
		t.Fatalf("device 详情错误: %+v", dd.Device)
	}
	if len(dd.Results) != 1 || dd.Results[0].Stdout != "ok" {
		t.Fatalf("results 详情错误: %+v", dd.Results)
	}
}

// TestHandleDeviceDetail_TenantMismatch 验证设备详情的租户隔离：跨租户应 403。
func TestHandleDeviceDetail_TenantMismatch(t *testing.T) {
	s := newTestServer()
	a := s.reg.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	devID := "dev-" + a.AgentID
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices/"+devID, nil)
	req.Header.Set("X-Tenant-ID", "t2") // 冒充另一租户
	w := httptest.NewRecorder()
	s.handleDeviceDetail(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("跨租户应 403，得到 %d", w.Code)
	}
}

// TestHandleDeviceDetail_NotFound 验证未知设备返回 404。
func TestHandleDeviceDetail_NotFound(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices/nope", nil)
	w := httptest.NewRecorder()
	s.handleDeviceDetail(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("未知设备应 404，得到 %d", w.Code)
	}
}

// TestHandleBatchCreateTasks 验证 P0-3 批量下发：一次请求向多台 agent 下发同一任务模板。
func TestHandleBatchCreateTasks(t *testing.T) {
	s := newTestServer()
	a1 := s.reg.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	a2 := s.reg.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})

	body, _ := json.Marshal(map[string]interface{}{
		"targets": []string{a1.AgentID, a2.AgentID},
		"type":    "shell",
		"command": "echo batch",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/batch", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleBatchCreateTasks(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Count   int      `json:"count"`
		Created []string `json:"created"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Count != 2 || len(resp.Created) != 2 {
		t.Fatalf("batch count=%d created=%d, want 2/2", resp.Count, len(resp.Created))
	}
	// 两台各应多一条 pending 任务（演示开启：各含 1 预置 + 本次批量 1 = 2）
	if got := s.reg.GetTasks(a1.AgentID); len(got) != 2 {
		t.Fatalf("a1 tasks=%d, want 2 (preset+batch)", len(got))
	}
}

// TestHandleAudits 验证 P0-4 审计检索：按 action 过滤返回审计事件。
func TestHandleAudits(t *testing.T) {
	s := newTestServer()
	s.reg.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	// Register 自身会产 register 审计；此处额外写一条 create_task 以验证动作过滤。
	s.reg.Audit(&proto.AuditEvent{TenantID: "t1", Action: "create_task", Target: "x"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audits?action=create_task", nil)
	w := httptest.NewRecorder()
	s.handleAudits(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var evs []*proto.AuditEvent
	if err := json.NewDecoder(w.Body).Decode(&evs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// 过滤 create_task 应只命中显式写入的那 1 条（register 被滤掉）
	if len(evs) != 1 || evs[0].Action != "create_task" {
		t.Fatalf("audits=%+v, want 1 create_task", evs)
	}
}
