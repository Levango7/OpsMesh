package controlplane

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opsmesh/internal/approval"
	"opsmesh/internal/config"
	"opsmesh/internal/cron"
	"opsmesh/internal/store"
)

// newM5TestServer 构造一个注入了 M5 字段的测试控制面。
func newM5TestServer(t *testing.T) *Server {
	t.Helper()
	st := store.NewMemoryStore().WithDemo(true)
	s := &Server{
		store:          st,
		cfg:            &config.Config{Demo: true},
		requireAuth:    false,
		batches:        newBatchStore(),
		scheduleMgr:    cron.NewManager(),
		approvalEngine: approval.New(),
	}
	// 注入预置审批流（实例化为 default 租户）。
	for _, f := range approval.DefaultFlows {
		cp := *f
		cp.TenantID = "default"
		_ = s.approvalEngine.CreateFlow(&cp)
	}
	return s
}

// TestM5_ScheduleManagerIntegration 验证 Server.scheduleMgr 已注入且可 CRUD。
func TestM5_ScheduleManagerIntegration(t *testing.T) {
	s := newM5TestServer(t)
	if s.scheduleMgr == nil {
		t.Fatal("scheduleMgr should be initialized")
	}
	e, err := s.scheduleMgr.Create(&cron.ScheduleEntry{
		TaskID:   "tpl-test",
		CronExpr: "*/5 * * * *",
		Name:     "test schedule",
	})
	if err != nil {
		t.Fatal(err)
	}
	if e.ID == "" {
		t.Fatal("entry ID should be assigned")
	}
	got, err := s.scheduleMgr.Get(e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CronExpr != "*/5 * * * *" {
		t.Errorf("CronExpr = %s", got.CronExpr)
	}
}

// TestM5_ApprovalEngineIntegration 验证 Server.approvalEngine 已注入且预置流已加载。
func TestM5_ApprovalEngineIntegration(t *testing.T) {
	s := newM5TestServer(t)
	if s.approvalEngine == nil {
		t.Fatal("approvalEngine should be initialized")
	}
	// 预置流应已加载。
	f, err := s.approvalEngine.GetFlow("preset-shell")
	if err != nil {
		t.Fatalf("preset-shell flow should be loaded: %v", err)
	}
	if f.TriggerType != approval.TriggerShell {
		t.Errorf("TriggerType = %s, want shell", f.TriggerType)
	}
}

// TestM5_BatchStoreIntegration 验证 Server.batches 已注入。
func TestM5_BatchStoreIntegration(t *testing.T) {
	s := newM5TestServer(t)
	if s.batches == nil {
		t.Fatal("batches should be initialized")
	}
}

// TestM5_ApprovalFlowsAPI 验证 GET /api/v1/approval/flows 返回预置流列表。
func TestM5_ApprovalFlowsAPI(t *testing.T) {
	s := newM5TestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/approval/flows", nil)
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-ID", "demo")
	w := httptest.NewRecorder()
	s.handleApprovalFlows(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Flows []*approval.ApprovalFlow `json:"flows"`
		Total int                      `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Total == 0 {
		t.Error("expect preset flows, got 0")
	}
}

// TestM5_SchedulesAPI 验证 GET /api/v1/schedules 返回空列表。
func TestM5_SchedulesAPI(t *testing.T) {
	s := newM5TestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/schedules", nil)
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-ID", "demo")
	w := httptest.NewRecorder()
	s.handleSchedules(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// TestM5_ScheduleCreateAPI 验证 POST /api/v1/schedules 创建定时任务。
func TestM5_ScheduleCreateAPI(t *testing.T) {
	s := newM5TestServer(t)
	body := `{"taskID":"tpl-1","name":"test","cronExpr":"*/5 * * * *"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/schedules", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-ID", "demo")
	w := httptest.NewRecorder()
	s.handleSchedules(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
}

// TestM5_BatchExecAPI 验证 POST /api/v1/tasks/batch-exec 参数校验。
func TestM5_BatchExecAPI(t *testing.T) {
	s := newM5TestServer(t)
	// 空设备列表应返回 400。
	body := `{"deviceIDs":[],"command":"echo"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/batch-exec", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-ID", "demo")
	w := httptest.NewRecorder()
	s.handleBatchExec(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty deviceIDs: status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestM5_CanaryCreateAPI_InvalidStrategy 验证非法 strategy 返回 400。
func TestM5_CanaryCreateAPI_InvalidStrategy(t *testing.T) {
	s := newM5TestServer(t)
	body := `{"deviceIDs":["d1"],"command":"echo","strategy":"invalid"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/canary", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-ID", "demo")
	w := httptest.NewRecorder()
	s.handleCanaryCreate(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid strategy: status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestM5_ApprovalPendingAPI 验证 GET /api/v1/approval/pending 返回 200。
func TestM5_ApprovalPendingAPI(t *testing.T) {
	s := newM5TestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/approval/pending", nil)
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-ID", "demo")
	w := httptest.NewRecorder()
	s.handleApprovalPending(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
}
