// cmdb_collector_test.go CMDB 采集 handler 的用例（TD-87 批 2 第三批：采集器实现已下沉
// internal/controlplane/cmdbcollector，其语义用例按函数迁入；本文件保留 Server 端接线用例）。
package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/cmdb"
	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

// TestHandleCMDBCollect 验证 POST /api/v1/cmdb/collect 手动触发采集：
//   - demo 模式放行 RBAC；
//   - 返回 {collected: N, failed: M}。
func TestHandleCMDBCollect(t *testing.T) {
	st := store.NewMemoryStore()
	ci := cmdb.NewMemoryCiStore()
	// 准备 2 台设备 + 指标。
	for _, id := range []string{"dev-h1", "dev-h2"} {
		st.UpsertDevice(&proto.DeviceInfo{DeviceID: id, Segment: "seg-1", TenantID: "default"})
		st.StoreDeviceMetrics(id, makeTestMetrics(id))
	}
	s := &Server{
		store:         st,
		requireAuth:   false,
		cfg:           &config.Config{Demo: true}, // demo 放行 RBAC
		cmdbCollector: NewCMDBCollector(st, ci, 5*time.Minute, ""),
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/collect", nil)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleCMDBCollect(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Collected int `json:"collected"`
		Failed    int `json:"failed"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response err: %v", err)
	}
	if resp.Collected != 2 {
		t.Errorf("collected = %d, want 2", resp.Collected)
	}
	if resp.Failed != 0 {
		t.Errorf("failed = %d, want 0", resp.Failed)
	}
}

// TestHandleCMDBCollect_MethodNotAllowed 验证非 POST 方法返回 405。

// TestHandleCMDBCollect_MethodNotAllowed 验证非 POST 方法返回 405。
func TestHandleCMDBCollect_MethodNotAllowed(t *testing.T) {
	s := &Server{store: store.NewMemoryStore(), cfg: &config.Config{Demo: true}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/collect", nil)
	rec := httptest.NewRecorder()
	s.handleCMDBCollect(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET status = %d, want 405", rec.Code)
	}
}

// TestHandleCMDBCollect_NilCollector 验证 cmdbCollector 未初始化时返回 503。

// TestHandleCMDBCollect_NilCollector 验证 cmdbCollector 未初始化时返回 503。
func TestHandleCMDBCollect_NilCollector(t *testing.T) {
	s := &Server{store: store.NewMemoryStore(), cfg: &config.Config{Demo: true}}
	// cmdbCollector 留 nil。
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cmdb/collect", nil)
	rec := httptest.NewRecorder()
	s.handleCMDBCollect(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("nil collector status = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not initialized") {
		t.Errorf("body = %q, want contains 'not initialized'", rec.Body.String())
	}
}

// --- TestNewCMDBCollector_DefaultInterval ---

// TestNewCMDBCollector_DefaultInterval 验证 interval<=0 时回退 5 分钟。

// makeTestMetrics 构造测试指标（父包副本——迁移后两个包各自持有最小测试助手，
// 同 recordingBus/countDevices 的既有惯例；新增字段时两侧同步）。
func makeTestMetrics(deviceID string) *proto.DeviceMetrics {
	return &proto.DeviceMetrics{
		DeviceID:  deviceID,
		Hostname:  "host-" + deviceID,
		OS:        "linux",
		OSVersion: "Ubuntu 22.04 LTS",
		Kernel:    "5.15.0-91-generic",
		Arch:      "amd64",
		CPU:       proto.CPUMetrics{Cores: 8, Usage: 12.5, Model: "Intel Xeon E5"},
		Memory:    proto.MemMetrics{Total: 16384, Used: 4096, Available: 12288, Usage: 25.0},
		Disks: []proto.DiskMetrics{
			{Mount: "/", Total: 100, Used: 30, Free: 70, Usage: 30.0, Type: "ext4"},
			{Mount: "/data", Total: 500, Used: 100, Free: 400, Usage: 20.0, Type: "ext4"},
		},
		Services: []proto.ServiceInfo{
			{Name: "nginx", Status: "running", Enabled: true},
			{Name: "docker", Status: "running", Enabled: true},
			{Name: "mysql", Status: "stopped", Enabled: false},
		},
		CollectedAt: time.Now(),
	}
}
