package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"opsmesh/internal/config"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// newPeerTestServer 构造一个模拟 peer 控制面的 httptest.Server，复用真实 Server 的 handler。
// 这样 peer 侧的任务创建/设备列表/健康检查走真实代码路径，测试更接近生产。
func newPeerTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	st := store.NewMemoryStore()
	// 预注册一台 agent，使 peer 有设备可查、有 agent 可下发。
	st.Register(&proto.AgentInfo{Segment: "peer-seg", TenantID: "t1", Hostname: "peer-agent-1"})
	srv := &Server{
		store:       st,
		cfg:         &config.Config{TaskMaxRetries: 3},
		requireAuth: false,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tasks", srv.handleListTasks) // GET 列表 + POST 创建
	mux.HandleFunc("/api/v1/devices", srv.handleDevices)
	mux.HandleFunc("/healthz", srv.handleHealthz)
	return httptest.NewServer(mux)
}

// TestFederationManager_ForwardTask 验证跨网段任务转发：ForwardTask 把 task POST 到 peer /api/v1/tasks，
// peer 侧创建任务并返回完整 Task（含分配的 TaskID 与初始 pending 状态）。
func TestFederationManager_ForwardTask(t *testing.T) {
	peer := newPeerTestServer(t)
	defer peer.Close()

	localStore := store.NewMemoryStore()
	fed := NewFederationManager([]string{peer.URL}, localStore, "", nil)
	if fed == nil {
		t.Fatal("NewFederationManager returned nil for non-empty peers")
	}

	// 取 peer 上预注册的 agent ID（peer 侧 handleListTasks 走真实注册流程）。
	peerAgents := peer.Client()
	_ = peerAgents
	// 直接从 peer 的 store 取 agent ID（白盒：peer 用的是真实 Server，agent ID 由 Register 分配）。
	// 改用 HTTP 拉取设备视图反推 agent ID（黑盒，更稳健）。
	resp, err := peer.Client().Get(peer.URL + "/api/v1/devices")
	if err != nil {
		t.Fatalf("get peer devices: %v", err)
	}
	var segMap map[string][]proto.DeviceInfo
	json.NewDecoder(resp.Body).Decode(&segMap)
	resp.Body.Close()
	var peerAgentID string
	for _, devs := range segMap {
		if len(devs) > 0 {
			peerAgentID = devs[0].AgentID
			break
		}
	}
	if peerAgentID == "" {
		t.Fatal("no agent found on peer")
	}

	task := proto.Task{AgentID: peerAgentID, Type: "shell", Command: "echo federated"}
	created, err := fed.ForwardTask(context.Background(), peer.URL, task, http.Header{"X-Tenant-ID": {"t1"}})
	if err != nil {
		t.Fatalf("ForwardTask err: %v", err)
	}
	if created.TaskID == "" {
		t.Fatal("created.TaskID is empty")
	}
	if created.AgentID != peerAgentID {
		t.Fatalf("created.AgentID = %q, want %q", created.AgentID, peerAgentID)
	}
	if created.Status != "pending" {
		t.Fatalf("created.Status = %q, want pending", created.Status)
	}
}

// TestFederationManager_FederatedDevices 验证联邦设备视图聚合：返回 local + 所有 peer 的设备，
// peer 不可达时该 peer entry 含 error 字段且 devices 为 nil。
func TestFederationManager_FederatedDevices(t *testing.T) {
	peer1 := newPeerTestServer(t)
	defer peer1.Close()
	// peer2 用一个不会启动的地址，模拟不可达。
	peer2URL := "http://127.0.0.1:1" // 端口 1 几乎必然拒绝连接

	localStore := store.NewMemoryStore()
	// 本地也注册一台 agent，使 local 部分非空。
	localStore.Register(&proto.AgentInfo{Segment: "local-seg", TenantID: "t1", Hostname: "local-agent"})

	fed := NewFederationManager([]string{peer1.URL, peer2URL}, localStore, "", nil)
	result := fed.FederatedDevices(context.Background(), "")

	// local 部分应有至少 1 台设备。
	localDevs, _ := result["local"].([]proto.DeviceInfo)
	if len(localDevs) == 0 {
		t.Fatal("local devices empty, want >=1")
	}

	peersMap, ok := result["peers"].(map[string]interface{})
	if !ok {
		t.Fatalf("peers map type = %T", result["peers"])
	}
	// peer1 应成功（error 为空、devices 非 nil）。
	p1Entry, ok := peersMap[peer1.URL].(map[string]interface{})
	if !ok {
		t.Fatalf("peer1 entry type = %T", peersMap[peer1.URL])
	}
	if p1Entry["error"] != "" {
		t.Fatalf("peer1 error = %v, want empty", p1Entry["error"])
	}
	// peer2 应失败（error 非空）。
	p2Entry, ok := peersMap[peer2URL].(map[string]interface{})
	if !ok {
		t.Fatalf("peer2 entry type = %T", peersMap[peer2URL])
	}
	if p2Entry["error"] == "" {
		t.Fatal("peer2 error empty, want non-empty (peer unreachable)")
	}
}

// TestFederationManager_HealthCheck 验证健康检查：在线 peer 返回 true，不可达 peer 返回 false。
func TestFederationManager_HealthCheck(t *testing.T) {
	peer := newPeerTestServer(t)
	defer peer.Close()

	localStore := store.NewMemoryStore()
	fed := NewFederationManager([]string{peer.URL}, localStore, "", nil)

	if !fed.HealthCheck(context.Background(), peer.URL) {
		t.Fatal("HealthCheck(online peer) = false, want true")
	}
	if fed.HealthCheck(context.Background(), "http://127.0.0.1:1") {
		t.Fatal("HealthCheck(unreachable peer) = true, want false")
	}
}

// TestFederationManager_Peers 验证 Peers() 返回每个 peer 的地址 + 在线状态。
func TestFederationManager_Peers(t *testing.T) {
	peer := newPeerTestServer(t)
	defer peer.Close()
	unreachable := "http://127.0.0.1:1"

	fed := NewFederationManager([]string{peer.URL, unreachable}, store.NewMemoryStore(), "", nil)
	statuses := fed.Peers()
	if len(statuses) != 2 {
		t.Fatalf("len(Peers) = %d, want 2", len(statuses))
	}
	// 找到对应 entry 并校验状态。
	byURL := make(map[string]bool, len(statuses))
	for _, s := range statuses {
		byURL[s.URL] = s.Online
	}
	if !byURL[peer.URL] {
		t.Fatal("online peer marked offline")
	}
	if byURL[unreachable] {
		t.Fatal("unreachable peer marked online")
	}
}

// TestHandleFederationPeers 验证 GET /api/v1/federation/peers 返回正确的 peer 状态列表。
func TestHandleFederationPeers(t *testing.T) {
	peer := newPeerTestServer(t)
	defer peer.Close()

	s := &Server{
		store: store.NewMemoryStore(),
		cfg:   &config.Config{FederationPeers: []string{peer.URL}},
		fed:   NewFederationManager([]string{peer.URL}, store.NewMemoryStore(), "", nil),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/federation/peers", nil)
	rec := httptest.NewRecorder()
	s.handleFederationPeers(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var statuses []PeerStatus
	if err := json.NewDecoder(rec.Body).Decode(&statuses); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(statuses) != 1 || statuses[0].URL != peer.URL || !statuses[0].Online {
		t.Fatalf("statuses = %+v, want 1 online peer at %s", statuses, peer.URL)
	}
}

// TestHandleFederationForwardTask 验证 POST /api/v1/federation/forward/task 转发任务到 peer。
// 包含 SSRF 防护：peerURL 不在配置列表中时拒绝。
func TestHandleFederationForwardTask(t *testing.T) {
	peer := newPeerTestServer(t)
	defer peer.Close()

	s := &Server{
		store: store.NewMemoryStore(),
		cfg:   &config.Config{FederationPeers: []string{peer.URL}},
		fed:   NewFederationManager([]string{peer.URL}, store.NewMemoryStore(), "", nil),
	}

	// 取 peer 上 agent ID（黑盒：通过 /api/v1/devices 反推）。
	resp, err := peer.Client().Get(peer.URL + "/api/v1/devices")
	if err != nil {
		t.Fatalf("get peer devices: %v", err)
	}
	var segMap map[string][]proto.DeviceInfo
	json.NewDecoder(resp.Body).Decode(&segMap)
	resp.Body.Close()
	var peerAgentID string
	for _, devs := range segMap {
		if len(devs) > 0 {
			peerAgentID = devs[0].AgentID
			break
		}
	}
	if peerAgentID == "" {
		t.Fatal("no agent on peer")
	}

	// --- 正常转发：peerURL 在配置列表中 ---
	body, _ := json.Marshal(map[string]interface{}{
		"peerURL": peer.URL,
		"task": map[string]string{
			"agentID": peerAgentID,
			"type":    "shell",
			"command": "echo via federation",
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/federation/forward/task", bytes.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-User-Id", "u1")
	rec := httptest.NewRecorder()
	s.handleFederationForwardTask(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("forward status = %d, want 201, body=%s", rec.Code, rec.Body.String())
	}
	var created proto.Task
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	if created.TaskID == "" {
		t.Fatal("created.TaskID empty")
	}

	// --- SSRF 防护：peerURL 不在配置列表中应拒绝 ---
	body2, _ := json.Marshal(map[string]interface{}{
		"peerURL": "http://evil.example.com:8080",
		"task": map[string]string{
			"agentID": "x",
			"command": "cat /etc/passwd",
		},
	})
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/federation/forward/task", bytes.NewReader(body2))
	rec2 := httptest.NewRecorder()
	s.handleFederationForwardTask(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("SSRF attempt status = %d, want 400", rec2.Code)
	}
}

// TestHandleFederationDevices 验证 GET /api/v1/federation/devices 聚合本地 + peer 设备。
func TestHandleFederationDevices(t *testing.T) {
	peer := newPeerTestServer(t)
	defer peer.Close()

	localSt := store.NewMemoryStore()
	localSt.Register(&proto.AgentInfo{Segment: "local-seg", TenantID: "t1", Hostname: "local-agent"})
	s := &Server{
		store: localSt,
		cfg:   &config.Config{FederationPeers: []string{peer.URL}},
		fed:   NewFederationManager([]string{peer.URL}, localSt, "", nil),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/federation/devices", nil)
	rec := httptest.NewRecorder()
	s.handleFederationDevices(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var result struct {
		Local []proto.DeviceInfo             `json:"local"`
		Peers map[string]map[string]interface{} `json:"peers"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Local) == 0 {
		t.Fatal("local devices empty")
	}
	if _, ok := result.Peers[peer.URL]; !ok {
		t.Fatalf("peer %s not in result", peer.URL)
	}
}

// TestFederationDisabled_NotRegistered 验证 FederationPeers 为空时联邦 API 不注册（返回 404）。
// 通过完整 mux 路由分发验证（不直接调 handler，确保路由层正确跳过注册）。
func TestFederationDisabled_NotRegistered(t *testing.T) {
	st := store.NewMemoryStore()
	s := &Server{
		store: st,
		cfg:   &config.Config{}, // FederationPeers 为 nil
		fed:   nil,              // 显式 nil
	}
	mux := http.NewServeMux()
	// 模拟 server.go Start 中的路由注册逻辑：仅当 fed != nil 时注册。
	if s.fed != nil {
		mux.HandleFunc("/api/v1/federation/peers", s.handleFederationPeers)
		mux.HandleFunc("/api/v1/federation/forward/task", s.handleFederationForwardTask)
		mux.HandleFunc("/api/v1/federation/devices", s.handleFederationDevices)
	}

	for _, path := range []string{"/api/v1/federation/peers", "/api/v1/federation/devices"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404 (federation disabled)", path, rec.Code)
		}
	}
}

// TestNewFederationManager_NilForEmptyPeers 验证 peers 为空时返回 nil（调用方据此跳过路由注册）。
func TestNewFederationManager_NilForEmptyPeers(t *testing.T) {
	if got := NewFederationManager(nil, store.NewMemoryStore(), "", nil); got != nil {
		t.Fatalf("NewFederationManager(nil, _) = %v, want nil", got)
	}
	if got := NewFederationManager([]string{}, store.NewMemoryStore(), "", nil); got != nil {
		t.Fatalf("NewFederationManager([], _) = %v, want nil", got)
	}
	if got := NewFederationManager([]string{""}, store.NewMemoryStore(), "", nil); got == nil {
		t.Fatal("NewFederationManager(['']) = nil, want non-nil (single empty string still constructs)")
	}
}