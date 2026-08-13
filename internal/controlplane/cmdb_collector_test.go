package controlplane

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"opsmesh/internal/cmdb"
	"opsmesh/internal/config"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// newTestCollector 构造测试用 CMDBCollector + MemoryStore + MemoryCiStore。
//
// 返回 collector、底层 store（用于注册设备/存指标）、ciStore（用于断言 CI）。
// tenantID 留空（跨租户采集），interval 用 50ms 便于测试 Run 循环。
func newTestCollector() (*CMDBCollector, *store.MemoryStore, *cmdb.MemoryCiStore) {
	st := store.NewMemoryStore()
	ci := cmdb.NewMemoryCiStore()
	c := NewCMDBCollector(st, ci, 50*time.Millisecond, "")
	return c, st, ci
}

// makeTestMetrics 构造测试用 DeviceMetrics（含主机元信息 + 2 个服务）。
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

// --- TestCMDBCollector_Collect ---

// TestCMDBCollector_Collect 验证给定 DeviceMetrics，CMDB 配置项被正确创建：
//   - 主机 CI（machine 类型）含 hostname/os_type/os_version/kernel/cpu_cores/memory_total/disk_total；
//   - 每个服务一个 service CI；
//   - 审计日志已产出（action=cmdb_collect）。
func TestCMDBCollector_Collect(t *testing.T) {
	c, st, ci := newTestCollector()
	deviceID := "dev-001"

	before := len(st.Audits())
	if err := c.Collect(deviceID, makeTestMetrics(deviceID)); err != nil {
		t.Fatalf("Collect err: %v", err)
	}

	// 验证主机 CI 创建。
	hostCI, err := ci.GetCI(context.Background(), "ci-host-"+deviceID, "default")
	if err != nil {
		t.Fatalf("GetCI host err: %v", err)
	}
	if hostCI.CiType != "machine" {
		t.Errorf("host CI type = %q, want machine", hostCI.CiType)
	}
	if hostCI.Name != "host-dev-001" {
		t.Errorf("host CI name = %q, want host-dev-001", hostCI.Name)
	}
	if hostCI.Source != "agent" {
		t.Errorf("host CI source = %q, want agent", hostCI.Source)
	}
	if hostCI.ApprovalStatus != cmdb.ApprovalApproved {
		t.Errorf("host CI approval = %q, want approved", hostCI.ApprovalStatus)
	}
	// 验证属性。
	wantAttrs := map[string]string{
		"hostname":     "host-dev-001",
		"os_type":      "linux",
		"os_version":   "Ubuntu 22.04 LTS",
		"kernel":       "5.15.0-91-generic",
		"arch":         "amd64",
		"cpu_cores":    "8",
		"cpu_model":    "Intel Xeon E5",
		"memory_total": "16384",
		"disk_total":   "600", // 100 + 500
	}
	for k, want := range wantAttrs {
		if got := hostCI.Attrs[k]; got != want {
			t.Errorf("host CI attr %q = %q, want %q", k, got, want)
		}
	}

	// 验证服务 CI 创建（3 个服务）。
	svcNames := []string{"nginx", "docker", "mysql"}
	for _, name := range svcNames {
		svcID := "ci-svc-" + deviceID + "-" + name
		svcCI, err := ci.GetCI(context.Background(), svcID, "default")
		if err != nil {
			t.Errorf("GetCI service %s err: %v", name, err)
			continue
		}
		if svcCI.CiType != "service" {
			t.Errorf("service CI %s type = %q, want service", name, svcCI.CiType)
		}
		if svcCI.Attrs["name"] != name {
			t.Errorf("service CI %s attr name = %q, want %q", name, svcCI.Attrs["name"], name)
		}
	}

	// 验证审计日志产出。
	after := len(st.Audits())
	if after-before != 1 {
		t.Fatalf("audit events delta = %d, want 1", after-before)
	}
	lastAudit := st.Audits()[after-1]
	if lastAudit.Action != "cmdb_collect" {
		t.Errorf("audit action = %q, want cmdb_collect", lastAudit.Action)
	}
	if lastAudit.Target != deviceID {
		t.Errorf("audit target = %q, want %q", lastAudit.Target, deviceID)
	}
}

// TestCMDBCollector_Collect_UpdateExisting 验证对已存在 CI 的更新（幂等 upsert）：
//   - 第二次 Collect 应更新属性而非创建重复 CI；
//   - 版本号递增（UpdateCI 产生新版本）。
func TestCMDBCollector_Collect_UpdateExisting(t *testing.T) {
	c, _, ci := newTestCollector()
	deviceID := "dev-002"

	// 第一次采集。
	m1 := makeTestMetrics(deviceID)
	if err := c.Collect(deviceID, m1); err != nil {
		t.Fatalf("Collect #1 err: %v", err)
	}
	hostCI1, _ := ci.GetCI(context.Background(), "ci-host-"+deviceID, "default")
	v1 := hostCI1.Version

	// 第二次采集（修改 CPU 核数）。
	m2 := makeTestMetrics(deviceID)
	m2.CPU.Cores = 16
	if err := c.Collect(deviceID, m2); err != nil {
		t.Fatalf("Collect #2 err: %v", err)
	}
	hostCI2, _ := ci.GetCI(context.Background(), "ci-host-"+deviceID, "default")
	if hostCI2.Version != v1+1 {
		t.Errorf("version = %d, want %d (递增)", hostCI2.Version, v1+1)
	}
	if hostCI2.Attrs["cpu_cores"] != "16" {
		t.Errorf("cpu_cores after update = %q, want 16", hostCI2.Attrs["cpu_cores"])
	}

	// 验证 CI 总数未翻倍（仍是 1 host + 3 service = 4）。
	items, _ := ci.GetCIs(context.Background(), "", "active", "default")
	if len(items) != 4 {
		t.Errorf("CI count = %d, want 4 (1 host + 3 service)", len(items))
	}
}

// --- TestCMDBCollector_CollectAll ---

// TestCMDBCollector_CollectAll 验证多设备采集：
//   - 注册 3 台设备 + 存指标，CollectAll 返回 collected=3；
//   - 每台设备都有对应的主机 CI。
func TestCMDBCollector_CollectAll(t *testing.T) {
	c, st, ci := newTestCollector()
	devices := []string{"dev-a", "dev-b", "dev-c"}
	for _, id := range devices {
		st.UpsertDevice(&proto.DeviceInfo{
			DeviceID: id, Segment: "seg-1", TenantID: "default", State: "online", Managed: true,
		})
		st.StoreDeviceMetrics(id, makeTestMetrics(id))
	}

	collected, failed, err := c.CollectAll()
	if err != nil {
		t.Fatalf("CollectAll err: %v", err)
	}
	if collected != 3 {
		t.Errorf("collected = %d, want 3", collected)
	}
	if failed != 0 {
		t.Errorf("failed = %d, want 0", failed)
	}

	// 验证每台设备都有主机 CI。
	for _, id := range devices {
		if _, err := ci.GetCI(context.Background(), "ci-host-"+id, "default"); err != nil {
			t.Errorf("device %s host CI not created: %v", id, err)
		}
	}
}

// TestCMDBCollector_CollectAll_SkipNoMetrics 验证无指标的设备被跳过（不计 failed）。
func TestCMDBCollector_CollectAll_SkipNoMetrics(t *testing.T) {
	c, st, _ := newTestCollector()
	// 注册设备但不存指标。
	st.UpsertDevice(&proto.DeviceInfo{DeviceID: "dev-no-metrics", Segment: "seg-1", TenantID: "default"})

	collected, failed, err := c.CollectAll()
	if err != nil {
		t.Fatalf("CollectAll err: %v", err)
	}
	if collected != 0 {
		t.Errorf("collected = %d, want 0 (no metrics)", collected)
	}
	if failed != 0 {
		t.Errorf("failed = %d, want 0 (skipped, not failed)", failed)
	}
}

// TestCMDBCollector_CollectAll_SkipRetired 验证 retired 设备被跳过。
func TestCMDBCollector_CollectAll_SkipRetired(t *testing.T) {
	c, st, _ := newTestCollector()
	st.UpsertDevice(&proto.DeviceInfo{DeviceID: "dev-retired", Segment: "seg-1", TenantID: "default", Retired: true})
	st.StoreDeviceMetrics("dev-retired", makeTestMetrics("dev-retired"))

	collected, _, err := c.CollectAll()
	if err != nil {
		t.Fatalf("CollectAll err: %v", err)
	}
	if collected != 0 {
		t.Errorf("collected = %d, want 0 (retired skipped)", collected)
	}
}

// --- TestCMDBCollector_EmptyMetrics ---

// TestCMDBCollector_EmptyMetrics 验证空指标不报错（deviceID 空或 metrics nil）。
func TestCMDBCollector_EmptyMetrics(t *testing.T) {
	c, _, _ := newTestCollector()

	// deviceID 空串。
	if err := c.Collect("", makeTestMetrics("x")); err != nil {
		t.Errorf("Collect(empty deviceID) err: %v", err)
	}
	// metrics nil。
	if err := c.Collect("dev-x", nil); err != nil {
		t.Errorf("Collect(nil metrics) err: %v", err)
	}
	// 两者都空。
	if err := c.Collect("", nil); err != nil {
		t.Errorf("Collect(both empty) err: %v", err)
	}
}

// --- TestCMDBCollector_Run ---

// TestCMDBCollector_Run 验证 Run 循环启动/停止：
//   - 启动后首次立即执行一次采集；
//   - context cancel 时正常退出（不泄漏 goroutine）。
func TestCMDBCollector_Run(t *testing.T) {
	c, st, ci := newTestCollector()
	// 准备一台设备 + 指标，验证首次立即采集。
	st.UpsertDevice(&proto.DeviceInfo{DeviceID: "dev-run", Segment: "seg-1", TenantID: "default"})
	st.StoreDeviceMetrics("dev-run", makeTestMetrics("dev-run"))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Run(ctx)
	}()

	// 等待首次采集完成（Run 启动后立即执行一次）。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := ci.GetCI(context.Background(), "ci-host-dev-run", "default"); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := ci.GetCI(context.Background(), "ci-host-dev-run", "default"); err != nil {
		t.Fatalf("Run 首次采集未在 2s 内创建 host CI: %v", err)
	}

	// cancel 并验证 Run 在 5s 内退出。
	cancel()
	select {
	case <-done:
		// 正常退出
	case <-time.After(5 * time.Second):
		t.Fatal("Run 未在 5s 内退出，疑似 goroutine 泄漏")
	}
}

// TestCMDBCollector_Run_PeriodicCollect 验证 Run 周期性采集：
// 用极短 interval（50ms）+ 计数器验证多次采集发生。
func TestCMDBCollector_Run_PeriodicCollect(t *testing.T) {
	st := store.NewMemoryStore()
	ci := cmdb.NewMemoryCiStore()
	// 用自定义 collector 计数采集次数（通过审计事件数推断）。
	c := NewCMDBCollector(st, ci, 50*time.Millisecond, "")

	st.UpsertDevice(&proto.DeviceInfo{DeviceID: "dev-periodic", Segment: "seg-1", TenantID: "default"})
	st.StoreDeviceMetrics("dev-periodic", makeTestMetrics("dev-periodic"))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Run(ctx)
	}()

	// 等待至少 3 次采集（每次产 1 条审计，首次立即 + 2 次 ticker）。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(st.Audits()) >= 3 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	<-done // 等 goroutine 退出避免 race。

	if got := len(st.Audits()); got < 3 {
		t.Errorf("periodic collect count = %d, want >= 3", got)
	}
}

// --- TestHandleCMDBCollect ---

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
func TestNewCMDBCollector_DefaultInterval(t *testing.T) {
	st := store.NewMemoryStore()
	ci := cmdb.NewMemoryCiStore()

	c1 := NewCMDBCollector(st, ci, 0, "")
	if c1.interval != 5*time.Minute {
		t.Errorf("interval=0 → %v, want 5m", c1.interval)
	}
	c2 := NewCMDBCollector(st, ci, -1, "")
	if c2.interval != 5*time.Minute {
		t.Errorf("interval=-1 → %v, want 5m", c2.interval)
	}
	c3 := NewCMDBCollector(st, ci, 10*time.Second, "")
	if c3.interval != 10*time.Second {
		t.Errorf("interval=10s → %v, want 10s", c3.interval)
	}
}

// --- 并发安全 ---

// TestCMDBCollector_ConcurrentCollect 验证并发采集不 panic（-race 下验证无数据竞态）。
func TestCMDBCollector_ConcurrentCollect(t *testing.T) {
	c, st, _ := newTestCollector()
	st.UpsertDevice(&proto.DeviceInfo{DeviceID: "dev-concurrent", Segment: "seg-1", TenantID: "default"})
	st.StoreDeviceMetrics("dev-concurrent", makeTestMetrics("dev-concurrent"))

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.Collect("dev-concurrent", makeTestMetrics("dev-concurrent"))
		}()
	}
	wg.Wait()
}
