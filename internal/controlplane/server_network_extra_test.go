package controlplane

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"opsmesh/internal/config"
	"opsmesh/internal/store"
)

// 本文件补全 server_network.go 的单元测试。
// 覆盖：NetworkTopologyCache（get/set/peek）、parsePingOutput、buildPingCommand、
// validateDiagnoseTool、buildDiagnoseCommand、handleNetworkTopology、
// handleNetworkTopologyCache、handleNetworkConnectivity、handleNetworkDiagnose。

// newNetworkTestServer 构造测试 Server。
func newNetworkTestServer() *Server {
	st := store.NewMemoryStore()
	return &Server{
		store:                st,
		cfg:                  &config.Config{TaskMaxRetries: 3, Demo: true},
		networkTopologyCache: &NetworkTopologyCache{},
	}
}

// =============================================================================
// NetworkTopologyCache
// =============================================================================

func TestNetworkTopologyCache_GetSet(t *testing.T) {
	c := &NetworkTopologyCache{}
	// 空 cache → get 返回 nil, false
	if data, ok := c.get(); ok || data != nil {
		t.Fatalf("empty get = %v, %v, want nil, false", data, ok)
	}
	// set 后 get
	c.set(&NetworkTopology{TenantID: "t1", GeneratedAt: time.Now()})
	if data, ok := c.get(); !ok || data == nil || data.TenantID != "t1" {
		t.Fatalf("after set get = %v, %v", data, ok)
	}
}

func TestNetworkTopologyCache_Peek(t *testing.T) {
	c := &NetworkTopologyCache{}
	// 空 cache → peek 返回 nil
	if data := c.peek(); data != nil {
		t.Fatalf("empty peek = %v, want nil", data)
	}
	// set 后 peek
	c.set(&NetworkTopology{TenantID: "t2"})
	if data := c.peek(); data == nil || data.TenantID != "t2" {
		t.Fatalf("after set peek = %v", data)
	}
}

func TestNetworkTopologyCache_Expired(t *testing.T) {
	c := &NetworkTopologyCache{}
	// 手动设置过期时间
	c.mu.Lock()
	c.data = &NetworkTopology{TenantID: "expired"}
	c.expiresAt = time.Now().Add(-time.Hour) // 已过期
	c.mu.Unlock()
	if _, ok := c.get(); ok {
		t.Fatal("expired cache should return false")
	}
}

// =============================================================================
// buildPingCommand
// =============================================================================

func TestBuildPingCommand(t *testing.T) {
	// Linux
	got := buildPingCommand("10.0.0.1", 3, 2, "linux")
	if !strings.Contains(got, "ping -c 3") || !strings.Contains(got, "10.0.0.1") {
		t.Fatalf("linux ping = %q", got)
	}
	// Windows
	got = buildPingCommand("10.0.0.1", 3, 2, "windows")
	if !strings.Contains(got, "ping -n 3") || !strings.Contains(got, "-w 2000") {
		t.Fatalf("windows ping = %q", got)
	}
}

// =============================================================================
// parsePingOutput
// =============================================================================

func TestParsePingOutput_Empty(t *testing.T) {
	latency, loss, alive := parsePingOutput("", 1, "linux")
	if latency != -1 || loss != 100 || alive {
		t.Fatalf("empty = %f, %f, %v, want -1, 100, false", latency, loss, alive)
	}
}

func TestParsePingOutput_Linux(t *testing.T) {
	out := `3 packets transmitted, 3 received, 0% packet loss, time 2002ms
rtt min/avg/max/mdev = 0.123/0.234/0.345/0.056 ms`
	latency, loss, alive := parsePingOutput(out, 0, "linux")
	if !alive {
		t.Fatal("should be alive")
	}
	if loss != 0 {
		t.Fatalf("loss = %f, want 0", loss)
	}
	if latency != 0.234 {
		t.Fatalf("latency = %f, want 0.234", latency)
	}
}

func TestParsePingOutput_LinuxPacketLoss(t *testing.T) {
	out := `3 packets transmitted, 1 received, 66% packet loss, time 2002ms
rtt min/avg/max/mdev = 0.123/0.234/0.345/0.056 ms`
	_, loss, alive := parsePingOutput(out, 1, "linux")
	if loss != 66 {
		t.Fatalf("loss = %f, want 66", loss)
	}
	if !alive {
		t.Fatal("loss<100 should be alive")
	}
}

func TestParsePingOutput_LinuxAllLost(t *testing.T) {
	out := `3 packets transmitted, 0 received, 100% packet loss, time 2002ms`
	_, loss, alive := parsePingOutput(out, 1, "linux")
	if loss != 100 {
		t.Fatalf("loss = %f, want 100", loss)
	}
	if alive {
		t.Fatal("100% loss should not be alive")
	}
}

func TestParsePingOutput_Windows(t *testing.T) {
	out := `Ping statistics for 10.0.0.2:
    Packets: Sent = 3, Received = 3, Lost = 0 (0% loss),
Approximate round trip times in milli-seconds:
    Minimum = 1ms, Maximum = 1ms, Average = 1ms`
	latency, loss, alive := parsePingOutput(out, 0, "windows")
	if !alive {
		t.Fatal("should be alive")
	}
	if loss != 0 {
		t.Fatalf("loss = %f, want 0", loss)
	}
	if latency != 1 {
		t.Fatalf("latency = %f, want 1", latency)
	}
}

func TestParsePingOutput_WindowsAllLost(t *testing.T) {
	out := `Ping statistics for 10.0.0.2:
    Packets: Sent = 3, Received = 0, Lost = 3 (100% loss),`
	_, loss, alive := parsePingOutput(out, 1, "windows")
	if loss != 100 {
		t.Fatalf("loss = %f, want 100", loss)
	}
	if alive {
		t.Fatal("100% loss should not be alive")
	}
}

// =============================================================================
// validateDiagnoseTool
// =============================================================================

func TestValidateDiagnoseTool(t *testing.T) {
	valid := []string{"ping", "traceroute", "tcping", "nslookup", "curl"}
	for _, tool := range valid {
		if err := validateDiagnoseTool(tool); err != nil {
			t.Fatalf("validateDiagnoseTool(%q) = %v, want nil", tool, err)
		}
	}
	if err := validateDiagnoseTool("invalid"); err == nil {
		t.Fatal("invalid tool should error")
	}
}

// =============================================================================
// buildDiagnoseCommand
// =============================================================================

func TestBuildDiagnoseCommand(t *testing.T) {
	opts := diagnoseOptions{Count: 3, Timeout: 2, Port: 80}
	// ping
	cmd, err := buildDiagnoseCommand("ping", "10.0.0.1", opts, "linux")
	if err != nil || !strings.Contains(cmd, "ping") {
		t.Fatalf("ping = %q, %v", cmd, err)
	}
	// traceroute linux
	cmd, err = buildDiagnoseCommand("traceroute", "10.0.0.1", opts, "linux")
	if err != nil || !strings.Contains(cmd, "traceroute") {
		t.Fatalf("traceroute = %q, %v", cmd, err)
	}
	// traceroute windows
	cmd, err = buildDiagnoseCommand("traceroute", "10.0.0.1", opts, "windows")
	if err != nil || !strings.Contains(cmd, "tracert") {
		t.Fatalf("tracert = %q, %v", cmd, err)
	}
	// tcping linux
	cmd, err = buildDiagnoseCommand("tcping", "10.0.0.1", opts, "linux")
	if err != nil || !strings.Contains(cmd, "nc") {
		t.Fatalf("tcping linux = %q, %v", cmd, err)
	}
	// tcping windows
	cmd, err = buildDiagnoseCommand("tcping", "10.0.0.1", opts, "windows")
	if err != nil || !strings.Contains(cmd, "Test-NetConnection") {
		t.Fatalf("tcping windows = %q, %v", cmd, err)
	}
	// nslookup
	cmd, err = buildDiagnoseCommand("nslookup", "example.com", opts, "linux")
	if err != nil || !strings.Contains(cmd, "nslookup") {
		t.Fatalf("nslookup = %q, %v", cmd, err)
	}
	// curl
	cmd, err = buildDiagnoseCommand("curl", "http://example.com", opts, "linux")
	if err != nil || !strings.Contains(cmd, "curl") {
		t.Fatalf("curl = %q, %v", cmd, err)
	}
	// unknown
	_, err = buildDiagnoseCommand("unknown", "x", opts, "linux")
	if err == nil {
		t.Fatal("unknown tool should error")
	}
}

// =============================================================================
// handleNetworkTopology / handleNetworkTopologyCache
// =============================================================================

func TestHandleNetworkTopology(t *testing.T) {
	s := newNetworkTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/topology", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleNetworkTopology(rec, req)
	// 无设备 → 返回空拓扑或 200
	if rec.Code != http.StatusOK {
		t.Fatalf("topology = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleNetworkTopologyCache_Empty(t *testing.T) {
	s := newNetworkTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/topology/cache", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleNetworkTopologyCache(rec, req)
	// 空 cache → 404 或 200
	if rec.Code != http.StatusOK && rec.Code != http.StatusNotFound {
		t.Fatalf("cache empty = %d, want 200/404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleNetworkTopologyCache_WithCache(t *testing.T) {
	s := newNetworkTestServer()
	s.networkTopologyCache.set(&NetworkTopology{TenantID: "t1", GeneratedAt: time.Now()})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/topology/cache", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleNetworkTopologyCache(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cache = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// handleNetworkDiagnose
// =============================================================================

func TestHandleNetworkDiagnose_MethodNotAllowed(t *testing.T) {
	s := newNetworkTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/diagnose", nil)
	rec := httptest.NewRecorder()
	s.handleNetworkDiagnose(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("get = %d, want 405", rec.Code)
	}
}

func TestHandleNetworkDiagnose_BadJSON(t *testing.T) {
	s := newNetworkTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/diagnose", strings.NewReader("{bad"))
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleNetworkDiagnose(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json = %d, want 400", rec.Code)
	}
}

func TestHandleNetworkDiagnose_InvalidTool(t *testing.T) {
	s := newNetworkTestServer()
	body := strings.NewReader(`{"agentId":"a1","tool":"invalid","target":"10.0.0.1"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/diagnose", body)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleNetworkDiagnose(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid tool = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleNetworkDiagnose_EmptyTarget(t *testing.T) {
	s := newNetworkTestServer()
	body := strings.NewReader(`{"agentId":"a1","tool":"ping","target":""}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/diagnose", body)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleNetworkDiagnose(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty target = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// handleNetworkConnectivity
// =============================================================================

func TestHandleNetworkConnectivity_MethodNotAllowed(t *testing.T) {
	s := newNetworkTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/connectivity", nil)
	rec := httptest.NewRecorder()
	s.handleNetworkConnectivity(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("get = %d, want 405", rec.Code)
	}
}

func TestHandleNetworkConnectivity_BadJSON(t *testing.T) {
	s := newNetworkTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/connectivity", strings.NewReader("{bad"))
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleNetworkConnectivity(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json = %d, want 400", rec.Code)
	}
}
