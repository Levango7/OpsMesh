// Package controlplane 联邦管理器（M4-4D 控制面联邦）。
//
// 设计目标：把多个独立部署的 OpsMesh 控制面（典型场景：跨网段/跨 IDC/跨 K8s 集群）
// 互联为联邦，使运维人员可从任一控制面：
//   - 查看联邦内所有 peer 的设备视图（GET /api/v1/federation/devices）
//   - 把任务转发到指定 peer 的 agent（POST /api/v1/federation/forward/task）
//   - 查看 peer 列表与在线状态（GET /api/v1/federation/peers）
//
// 通信协议：peer 之间通过 HTTP/JSON 复用现有 REST API（/api/v1/tasks、/api/v1/devices、/healthz），
// 不引入新 gRPC 依赖，保证现有部署/网络策略无需变更。
//
// 容错设计：peer 不可达不影响本地服务，联邦 API 返回可用部分 + 不可达标记。
// 每个 peer 请求 5s 超时，避免一个慢 peer 拖垮联邦视图聚合。
package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"opsmesh/internal/authctx"
	"opsmesh/internal/logx"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// peerRequestTimeout 单个 peer 请求的超时（5s）。
// 取值理由：联邦视图聚合典型 peer 数 ≤ 10，5s × 10 = 50s 上限可接受；
// 单 peer 5s 足够覆盖正常 LAN/WAN RTT + 控制面处理时延。
const peerRequestTimeout = 5 * time.Second

// PeerStatus 描述一个联邦 peer 的地址与当前在线状态。
type PeerStatus struct {
	URL    string `json:"url"`    // peer 控制面 HTTP 地址（如 http://peer1:8080）
	Online bool   `json:"online"` // 是否在线（最近一次健康检查通过）
}

// FederationManager 管理联邦 peer 列表，提供跨网段任务转发与联邦设备视图聚合。
//
// 不持有自己的 goroutine（健康检查按调用即时执行），无状态可变字段，
// 可被多个 HTTP handler 并发调用（http.Client 内部线程安全）。
type FederationManager struct {
	peers      []string       // 不可变 peer 地址列表（构造时确定）
	httpClient *http.Client   // 共享 HTTP 客户端（带超时）
	localStore store.Store    // 本地存储引用（聚合设备视图时取本地数据）
}

// NewFederationManager 构造联邦管理器。peers 为空时返回 nil（调用方据此跳过联邦路由注册）。
// localStore 用于 FederatedDevices 聚合本地设备列表，避免本地数据也走 HTTP 自环。
func NewFederationManager(peers []string, localStore store.Store) *FederationManager {
	if len(peers) == 0 {
		return nil
	}
	return &FederationManager{
		peers: peers,
		httpClient: &http.Client{
			Timeout: peerRequestTimeout,
		},
		localStore: localStore,
	}
}

// Peers 返回每个 peer 的地址与在线状态（即时健康检查，并发可控：调用方按需缓存）。
// 单个 peer 健康检查失败不影响其他 peer 的状态返回（容错）。
func (f *FederationManager) Peers() []PeerStatus {
	out := make([]PeerStatus, 0, len(f.peers))
	for _, p := range f.peers {
		out = append(out, PeerStatus{
			URL:    p,
			Online: f.HealthCheck(context.Background(), p),
		})
	}
	return out
}

// HealthCheck 检查 peer 是否在线（GET <peer>/healthz，2xx 视为在线）。
// 超时/连接拒绝/非 2xx 均视为离线（不返回 error，调用方仅需 bool）。
func (f *FederationManager) HealthCheck(ctx context.Context, peerURL string) bool {
	endpoint := strings.TrimRight(peerURL, "/") + "/healthz"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// ForwardTask 跨网段转发任务到指定 peer（POST <peer>/api/v1/tasks）。
// peer 侧按现有 handleCreateTask 逻辑创建任务并返回完整 Task（含分配的 TaskID）。
// 转发失败（peer 不可达 / 非 2xx）时返回 error，调用方决定如何向用户呈现。
//
// 注意：本方法不做租户隔离校验，由 peer 侧控制面按其网关注入的租户头自行校验。
// 调用方应在 handler 层把当前请求的 X-Tenant-ID 等 identity 头透传给 peer。
func (f *FederationManager) ForwardTask(ctx context.Context, peerURL string, task proto.Task, identityHeaders http.Header) (*proto.Task, error) {
	endpoint := strings.TrimRight(peerURL, "/") + "/api/v1/tasks"
	body, err := json.Marshal(task)
	if err != nil {
		return nil, fmt.Errorf("marshal task: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// 透传 identity 头（X-Tenant-ID / X-User-Id / X-User-Roles），让 peer 侧按网关注入逻辑鉴权。
	for _, h := range []string{"X-Tenant-ID", "X-User-Id", "X-User-Roles"} {
		if v := identityHeaders.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("peer %s unreachable: %w", peerURL, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("peer %s rejected task: status=%d body=%s", peerURL, resp.StatusCode, string(respBody))
	}
	var created proto.Task
	if err := json.Unmarshal(respBody, &created); err != nil {
		return nil, fmt.Errorf("decode peer response: %w", err)
	}
	return &created, nil
}

// FederatedDevices 聚合本地 + 所有 peer 的设备视图。
// 容错：peer 不可达时跳过该 peer（不返回 error），最终返回可用部分。
// 返回结构：{ "local": [...], "peers": { "<peerURL>": {"devices": [...], "error": "..."}, ... } }
// peer 出错时对应 entry 的 error 非空、devices 为 nil，便于前端区分"无设备"与"peer 不可达"。
func (f *FederationManager) FederatedDevices(ctx context.Context, tenantID string) map[string]interface{} {
	// 本地设备：聚合所有 segment 的设备列表。
	localSegments := f.localStore.Snapshot(tenantID)
	localDevices := make([]proto.DeviceInfo, 0)
	for _, devs := range localSegments {
		localDevices = append(localDevices, devs...)
	}

	peerResults := make(map[string]interface{}, len(f.peers))
	for _, p := range f.peers {
		devs, err := f.fetchPeerDevices(ctx, p, tenantID)
		if err != nil {
			peerResults[p] = map[string]interface{}{
				"devices": ([]proto.DeviceInfo)(nil),
				"error":   err.Error(),
			}
			continue
		}
		peerResults[p] = map[string]interface{}{
			"devices": devs,
			"error":   "",
		}
	}

	return map[string]interface{}{
		"local": localDevices,
		"peers": peerResults,
	}
}

// fetchPeerDevices 从单个 peer 拉取设备视图（GET <peer>/api/v1/devices）。
// peer 侧 handleDevices 返回 segment -> []DeviceInfo 的 map，本方法原样返回。
func (f *FederationManager) fetchPeerDevices(ctx context.Context, peerURL, tenantID string) (map[string][]proto.DeviceInfo, error) {
	endpoint := strings.TrimRight(peerURL, "/") + "/api/v1/devices"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	// 透传租户头（若本地 requireAuth，peer 侧也需 identity 头鉴权）。
	if tenantID != "" {
		req.Header.Set("X-Tenant-ID", tenantID)
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("peer %s unreachable: %w", peerURL, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("peer %s status=%d body=%s", peerURL, resp.StatusCode, string(respBody))
	}
	var out map[string][]proto.DeviceInfo
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("decode peer %s devices: %w", peerURL, err)
	}
	return out, nil
}

// --- HTTP handlers（在 federation.go 中定义，避免膨胀 server.go） ---

// handleFederationPeers 处理 GET /api/v1/federation/peers：返回 peer 列表 + 在线状态。
// 响应示例：[{"url":"http://peer1:8080","online":true},{"url":"http://peer2:8080","online":false}]
func (s *Server) handleFederationPeers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.fed == nil {
		// 联邦未启用：返回 404（路由层应已不注册，此处兜底防御）。
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, s.fed.Peers())
}

// handleFederationForwardTask 处理 POST /api/v1/federation/forward/task：转发任务到指定 peer。
// 请求体：{ "peerURL": "http://peer1:8080", "task": { "agentID": "...", "type": "shell", "command": "..." } }
// 调用 ForwardTask 把 task 转发到 peerURL，返回 peer 侧创建的完整 Task（含分配的 TaskID）。
func (s *Server) handleFederationForwardTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.fed == nil {
		http.NotFound(w, r)
		return
	}
	actx := authctx.FromHTTPHeader(r.Header)
	if s.requireAuth && actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing identity context (gateway auth required)"})
		return
	}
	var body struct {
		PeerURL string     `json:"peerURL"`
		Task    proto.Task `json:"task"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.PeerURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "peerURL is required"})
		return
	}
	// 安全校验：peerURL 必须在配置的 peers 列表中，防止 SSRF（攻击者借联邦转发探内网其他服务）。
	if !s.fed.isKnownPeer(body.PeerURL) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "peerURL not in configured federation peers"})
		return
	}
	if body.Task.AgentID == "" || body.Task.Command == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "task.agentID and task.command are required"})
		return
	}
	if body.Task.Type == "" {
		body.Task.Type = "shell"
	}
	// 透传当前请求的 identity 头给 peer，让 peer 侧按其网关鉴权逻辑处理租户归属。
	created, err := s.fed.ForwardTask(r.Context(), body.PeerURL, body.Task, r.Header)
	if err != nil {
		logx.Error(r.Context(), "联邦任务转发失败", err, "peer", body.PeerURL, "agentID", body.Task.AgentID)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	// 本地审计留痕（U-04 等保三级：跨网段操作必须可追溯）。
	s.store.Audit(&proto.AuditEvent{
		TenantID: actx.TenantID,
		UserID:   actx.UserID,
		Action:   "federation_forward_task",
		Target:   created.TaskID,
		Detail:   fmt.Sprintf("forwarded to %s, agentID=%s", body.PeerURL, body.Task.AgentID),
	})
	writeJSON(w, http.StatusCreated, created)
}

// handleFederationDevices 处理 GET /api/v1/federation/devices：聚合本地 + 所有 peer 的设备视图。
// 查询参数 ?tenant 可选（requireAuth 时强制取自身租户）；返回 { "local": [...], "peers": {...} }。
func (s *Server) handleFederationDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.fed == nil {
		http.NotFound(w, r)
		return
	}
	actx := authctx.FromHTTPHeader(r.Header)
	if s.requireAuth && actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing identity context (gateway auth required)"})
		return
	}
	tenant := r.URL.Query().Get("tenant")
	if s.requireAuth {
		tenant = actx.TenantID // 强制租户隔离，忽略客户端伪造
	}
	writeJSON(w, http.StatusOK, s.fed.FederatedDevices(r.Context(), tenant))
}

// isKnownPeer 判断 url 是否在配置的 peers 列表中（SSRF 防护）。
// 用精确匹配（含 scheme 与 host:port），不允通配符或子串。
func (f *FederationManager) isKnownPeer(url string) bool {
	for _, p := range f.peers {
		if p == url {
			return true
		}
	}
	return false
}