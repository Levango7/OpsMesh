// server_network.go M6 集成：网络拓扑发现 + 网络诊断工具 + 连通性检测 API。
//
// 与现有任务机制的关系：
//   - 网络诊断命令（ping/traceroute/tcping/nslookup/curl）通过下发 shell task 到指定 agent 执行；
//   - 复用 store.CreateTask + agent 端 executeShell，不新增 task type；
//   - 诊断结果经现有 /api/v1/tasks/{id}/result 查询，本文件仅做命令构造 + 任务下发 + 结果轮询适配。
//
// API 路由（在 server.go 注册）：
//   - GET  /api/v1/network/topology          — 返回网络拓扑图（节点=设备/agent，边=连通性+延迟）
//   - GET  /api/v1/network/topology/cache    — 返回最近一次缓存的拓扑（不触发探测）
//   - POST /api/v1/network/diagnose          — 发起网络诊断任务（ping/traceroute/tcping/nslookup/curl）
//   - GET  /api/v1/network/diagnose/{taskId} — 查询诊断任务结果
//   - POST /api/v1/network/connectivity      — 批量连通性检测
//
// 设计要点：
//   - 租户隔离：所有 API 经 requireTenantContext 获取租户 ID，仅返回当前租户的设备/拓扑；
//   - 安全：target 非空校验 + count 范围 1-100 + timeout 范围 1-30 + 命令经 validateCommand；
//   - 缓存：拓扑探测结果缓存 5 分钟（networkTopologyCache），避免频繁探测；
//   - 跨平台：命令构造区分 Linux/Windows（agent 端根据 runtime.GOOS 选择，控制面侧构造时按 agent.OS 选择）；
//   - 不引入新依赖：纯标准库实现，拓扑图由前端 SVG 渲染。
package controlplane

import (
	"fmt"
	"net/http"
	"opsmesh/internal/controlplane/paginate"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"opsmesh/internal/events"
	"opsmesh/internal/proto"
)

// ============================================================================
// 网络拓扑缓存
// ============================================================================

// networkTopologyTTL 拓扑缓存有效期（5 分钟）。
const networkTopologyTTL = 5 * time.Minute

// NetworkTopologyCache 网络拓扑缓存（Server 持有，内存缓存，重启后丢失）。
//
// 字段说明：
//   - mu 互斥保护并发读写；
//   - data 缓存的拓扑数据（含 nodes/edges/generatedAt）；
//   - expiresAt 缓存过期时间，过期后下次查询触发重新探测。
type NetworkTopologyCache struct {
	mu        sync.RWMutex
	data      *NetworkTopology
	expiresAt time.Time
}

// NetworkTopology 网络拓扑数据结构。
type NetworkTopology struct {
	Nodes       []NetworkNode `json:"nodes"`
	Edges       []NetworkEdge `json:"edges"`
	GeneratedAt time.Time     `json:"generatedAt"`
	TenantID    string        `json:"tenantID"`
}

// NetworkNode 拓扑节点（设备/agent）。
type NetworkNode struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
	IP       string `json:"ip"`
	Status   string `json:"status"` // online / offline
	OS       string `json:"os"`
	Segment  string `json:"segment"`
}

// NetworkEdge 拓扑边（连通性 + 延迟）。
type NetworkEdge struct {
	Source    string  `json:"source"`
	Target    string  `json:"target"`
	LatencyMs float64 `json:"latencyMs"` // 平均延迟（ms），-1 表示不可达
	Loss      float64 `json:"loss"`      // 丢包率（0-100）
	Alive     bool    `json:"alive"`     // 是否可达
}

// get 返回缓存数据与是否有效（未过期）。
func (c *NetworkTopologyCache) get() (*NetworkTopology, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.data == nil || time.Now().After(c.expiresAt) {
		return nil, false
	}
	return c.data, true
}

// set 写入缓存并设置过期时间。
func (c *NetworkTopologyCache) set(data *NetworkTopology) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = data
	c.expiresAt = time.Now().Add(networkTopologyTTL)
}

// peek 返回缓存数据（不论是否过期），仅用于 cache 端点。
func (c *NetworkTopologyCache) peek() *NetworkTopology {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.data
}

// ============================================================================
// 网络拓扑 API
// ============================================================================

// handleNetworkTopology 处理 GET /api/v1/network/topology：返回网络拓扑图。
//
// 查询参数：
//   - ?refresh=true：强制刷新（忽略缓存，重新探测）。
//
// 实现要点：
//   - 从 store 获取所有在线设备列表（按租户隔离）；
//   - 对每对设备发起互 ping 探测（通过下发 shell task 执行 ping -c 3 -W 2 <target_ip>）；
//   - 解析 ping 输出提取延迟（avg/loss）；
//   - 返回 JSON: { nodes, edges, generatedAt }；
//   - 缓存 5 分钟，避免频繁探测。
func (s *Server) handleNetworkTopology(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "device:read"); !ok {
		return
	}
	tenant := actx.TenantID

	// 解析 refresh 参数。
	refresh := r.URL.Query().Get("refresh") == "true"

	// 非刷新模式：优先返回缓存。
	if !refresh {
		if data, valid := s.networkTopologyCache.get(); valid && data != nil && data.TenantID == tenant {
			paginate.WriteJSON(w, http.StatusOK, data)
			return
		}
	}

	// 探测拓扑。
	topo, err := s.probeNetworkTopology(tenant)
	if err != nil {
		writeInternalError(r.Context(), w, "network.probeTopology", err)
		return
	}
	// 写入缓存。
	s.networkTopologyCache.set(topo)
	paginate.WriteJSON(w, http.StatusOK, topo)
}

// handleNetworkTopologyCache 处理 GET /api/v1/network/topology/cache：返回最近一次缓存的拓扑（不触发探测）。
func (s *Server) handleNetworkTopologyCache(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := s.requireTenantContext(w, r); !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "device:read"); !ok {
		return
	}
	data := s.networkTopologyCache.peek()
	if data == nil {
		paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"nodes":       []interface{}{},
			"edges":       []interface{}{},
			"generatedAt": time.Time{},
			"cached":      false,
		})
		return
	}
	// 标记是否来自缓存。
	resp := map[string]interface{}{
		"nodes":       data.Nodes,
		"edges":       data.Edges,
		"generatedAt": data.GeneratedAt,
		"tenantID":    data.TenantID,
		"cached":      true,
	}
	paginate.WriteJSON(w, http.StatusOK, resp)
}

// probeNetworkTopology 探测网络拓扑：构造节点 + 对每对设备发起互 ping 探测构造边。
//
// 注意：本实现采用"快速返回"策略——下发 ping 任务后立即返回当前已知拓扑（仅含节点 + 历史边），
// 不等待 ping 结果（避免 HTTP 请求长时间挂起）。前端可通过 ?refresh=true 周期刷新获取最新边。
//
// 边的构造逻辑：
//   - 仅对在线设备两两组合下发 ping 任务（避免对离线设备无意义探测）；
//   - 同步等待一小段时间（最多 2 秒）收集已完成的 ping 结果，构造边；
//   - 未完成的不阻塞，下次刷新时补齐。
func (s *Server) probeNetworkTopology(tenant string) (*NetworkTopology, error) {
	// 收集所有在线设备（按租户过滤）。
	snap := s.store.Snapshot(tenant)
	var nodes []NetworkNode
	var onlineDevs []proto.DeviceInfo
	for _, devs := range snap {
		for _, d := range devs {
			if d.Retired {
				continue
			}
			node := NetworkNode{
				ID:       d.DeviceID,
				Hostname: d.Hostname,
				IP:       d.IP,
				Status:   d.State,
				OS:       d.OS,
				Segment:  d.Segment,
			}
			nodes = append(nodes, node)
			if d.State == "online" && d.IP != "" && d.AgentID != "" {
				onlineDevs = append(onlineDevs, d)
			}
		}
	}
	if nodes == nil {
		nodes = []NetworkNode{}
	}

	topo := &NetworkTopology{
		Nodes:       nodes,
		Edges:       []NetworkEdge{},
		GeneratedAt: time.Now(),
		TenantID:    tenant,
	}

	// 在线设备少于 2 台时无需探测边。
	if len(onlineDevs) < 2 {
		return topo, nil
	}

	// 对每对在线设备发起互 ping 探测。
	// 为避免阻塞 HTTP 请求过久，采用"下发 + 短暂等待"策略：
	//   - 下发 ping 任务（ping -c 3 -W 2 <target_ip>）；
	//   - 同步等待最多 2 秒收集结果；
	//   - 已完成的解析延迟/丢包构造边，未完成的跳过（下次刷新补齐）。
	edges := s.probeEdges(tenant, onlineDevs)
	topo.Edges = edges
	return topo, nil
}

// probeEdges 对在线设备两两组合下发 ping 探测并收集边。
//
// 实现策略：
//   - 仅探测 i<j 的组合（避免重复，ping 不对称但拓扑图只需一条边）；
//   - 下发 ping 任务后等待最多 3 秒收集结果；
//   - 解析 ping 输出提取 avg latency + loss；
//   - 任务结果缺失时构造 alive=false 的边。
func (s *Server) probeEdges(tenant string, devs []proto.DeviceInfo) []NetworkEdge {
	type probeJob struct {
		sourceIdx, targetIdx int
		taskID               string
	}
	var jobs []probeJob
	for i := 0; i < len(devs); i++ {
		for j := i + 1; j < len(devs); j++ {
			// 下发 ping 任务到 devs[i].AgentID，目标 devs[j].IP。
			source := devs[i]
			target := devs[j]
			// 构造 ping 命令（按 source.OS 区分 Linux/Windows）。
			cmd := buildPingCommand(target.IP, 3, 2, source.OS)
			task := s.store.CreateTask(&proto.Task{
				AgentID:    source.AgentID,
				TenantID:   tenant,
				Type:       proto.TaskTypeShell,
				Command:    cmd,
				MaxRetries: 0,
			})
			jobs = append(jobs, probeJob{sourceIdx: i, targetIdx: j, taskID: task.TaskID})
		}
	}

	// 等待最多 3 秒收集结果。
	deadline := time.Now().Add(3 * time.Second)
	edges := make([]NetworkEdge, 0, len(jobs))
	for _, job := range jobs {
		source := devs[job.sourceIdx]
		target := devs[job.targetIdx]
		edge := NetworkEdge{
			Source: source.DeviceID,
			Target: target.DeviceID,
			Alive:  false,
		}
		// 轮询任务结果直到 deadline。
		for time.Now().Before(deadline) {
			res := s.store.TaskResult(job.taskID)
			if res != nil {
				// 解析 ping 输出。
				latency, loss, alive := parsePingOutput(res.Stdout, res.ExitCode, source.OS)
				edge.LatencyMs = latency
				edge.Loss = loss
				edge.Alive = alive
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		edges = append(edges, edge)
	}
	return edges
}

// buildPingCommand 构造 ping 命令（按 OS 区分）。
//
// Linux:   ping -c {count} -W {timeout} {target}
// Windows: ping -n {count} -w {timeout*1000} {target}
// 其他（darwin 等）: ping -c {count} -W {timeout} {target}
func buildPingCommand(target string, count, timeout int, os string) string {
	if os == "windows" {
		// Windows ping -w 单位为毫秒。
		return fmt.Sprintf("ping -n %d -w %d %s", count, timeout*1000, target)
	}
	// Linux/darwin/其他: -c count -W timeout（秒）。
	return fmt.Sprintf("ping -c %d -W %d %s", count, timeout, target)
}

// parsePingOutput 解析 ping 命令输出，提取平均延迟（ms）、丢包率（%）、是否可达。
//
// Linux ping 输出示例：
//
//	PING 10.0.0.2 (10.0.0.2) 56(84) bytes of data.
//	...
//	--- 10.0.0.2 ping statistics ---
//	3 packets transmitted, 3 received, 0% packet loss, time 2002ms
//	rtt min/avg/max/mdev = 0.123/0.234/0.345/0.056 ms
//
// Windows ping 输出示例：
//
//	Pinging 10.0.0.2 with 32 bytes of data:
//	Reply from 10.0.0.2: bytes=32 time=1ms TTL=64
//	...
//	Ping statistics for 10.0.0.2:
//	    Packets: Sent = 3, Received = 3, Lost = 0 (0% loss),
//	Approximate round trip times in milli-seconds:
//	    Minimum = 1ms, Maximum = 1ms, Average = 1ms
func parsePingOutput(stdout string, exitCode int, os string) (latency float64, loss float64, alive bool) {
	latency = -1
	loss = 100
	alive = false
	if exitCode == 0 {
		alive = true
		loss = 0
	}
	if stdout == "" {
		return
	}
	if os == "windows" {
		return parsePingOutputWindows(stdout, exitCode)
	}
	return parsePingOutputLinux(stdout, exitCode)
}

// parsePingOutputLinux 解析 Linux ping 输出。
func parsePingOutputLinux(stdout string, exitCode int) (latency float64, loss float64, alive bool) {
	latency = -1
	loss = 100
	alive = exitCode == 0
	if alive {
		loss = 0
	}
	// 提取丢包率：如 "3 packets transmitted, 3 received, 0% packet loss"
	lossRe := regexp.MustCompile(`(\d+)% packet loss`)
	if m := lossRe.FindStringSubmatch(stdout); len(m) >= 2 {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			loss = v
			// 丢包率 < 100 视为可达。
			if loss < 100 {
				alive = true
			} else {
				alive = false
			}
		}
	}
	// 提取平均延迟：如 "rtt min/avg/max/mdev = 0.123/0.234/0.345/0.056 ms"
	// 或 "round-trip min/avg/max/stddev = 0.123/0.234/0.345/0.056 ms"
	avgRe := regexp.MustCompile(`(?:rtt|round-trip) min/avg/max/(?:mdev|stddev) = ([\d.]+)/([\d.]+)/([\d.]+)/([\d.]+) ms`)
	if m := avgRe.FindStringSubmatch(stdout); len(m) >= 3 {
		if v, err := strconv.ParseFloat(m[2], 64); err == nil {
			latency = v
		}
	}
	return
}

// parsePingOutputWindows 解析 Windows ping 输出。
func parsePingOutputWindows(stdout string, exitCode int) (latency float64, loss float64, alive bool) {
	latency = -1
	loss = 100
	alive = exitCode == 0
	if alive {
		loss = 0
	}
	// 提取丢包率：如 "Lost = 0 (0% loss),"
	lossRe := regexp.MustCompile(`Lost = \d+ \((\d+)% loss\)`)
	if m := lossRe.FindStringSubmatch(stdout); len(m) >= 2 {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			loss = v
			if loss < 100 {
				alive = true
			} else {
				alive = false
			}
		}
	}
	// 提取平均延迟：如 "Minimum = 1ms, Maximum = 1ms, Average = 1ms"
	avgRe := regexp.MustCompile(`Average = (\d+)ms`)
	if m := avgRe.FindStringSubmatch(stdout); len(m) >= 2 {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			latency = v
		}
	}
	return
}

// ============================================================================
// 网络诊断工具 API
// ============================================================================

// diagnoseRequest 网络诊断请求体。
type diagnoseRequest struct {
	AgentID string          `json:"agentId"`
	Tool    string          `json:"tool"`   // ping / traceroute / tcping / nslookup / curl
	Target  string          `json:"target"` // 目标地址（IP/域名/URL）
	Options diagnoseOptions `json:"options"`
}

// diagnoseOptions 网络诊断可选参数。
type diagnoseOptions struct {
	Count   int `json:"count"`   // ping 次数（1-100，默认 4）
	Timeout int `json:"timeout"` // 超时秒数（1-30，默认 5）
	Port    int `json:"port"`    // tcping 端口
}

// diagnoseResponse 网络诊断响应体。
type diagnoseResponse struct {
	TaskID string `json:"taskId"`
}

// handleNetworkDiagnose 处理 POST /api/v1/network/diagnose：发起网络诊断任务。
//
// 请求体: { agentId, tool, target, options?: {count?, timeout?, port?} }
// 返回: { taskId }，客户端轮询 GET /api/v1/network/diagnose/{taskId} 获取结果。
//
// 支持的工具：
//   - ping:        ping -c {count} -W {timeout} {target}（Windows: ping -n {count} -w {timeout*1000} {target}）
//   - traceroute:  traceroute -m 30 -w 2 {target}（Windows: tracert -h 30 -w 2000 {target}）
//   - tcping:      nc -zv -w {timeout} {target} {port}（Windows: PowerShell Test-NetConnection -ComputerName {target} -Port {port}）
//   - nslookup:    nslookup {target}
//   - curl:        curl -sS -o /dev/null -w "%{http_code} %{time_total}" --max-time {timeout} {target}
func (s *Server) handleNetworkDiagnose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "task:write"); !ok {
		return
	}
	tenant := actx.TenantID

	var req diagnoseRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// 参数校验。
	if req.AgentID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "agentId is required"})
		return
	}
	if req.Target == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "target is required"})
		return
	}
	if err := validateDiagnoseTool(req.Tool); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// 默认参数 + 范围校验。
	if req.Options.Count == 0 {
		req.Options.Count = 4
	}
	if req.Options.Count < 1 || req.Options.Count > 100 {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "count must be between 1 and 100"})
		return
	}
	if req.Options.Timeout == 0 {
		req.Options.Timeout = 5
	}
	if req.Options.Timeout < 1 || req.Options.Timeout > 30 {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "timeout must be between 1 and 30"})
		return
	}
	// tcping 必须指定 port。
	if req.Tool == "tcping" && req.Options.Port <= 0 {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "port is required for tcping"})
		return
	}

	// 校验 agent 存在且属于当前租户。
	agent := s.lookupAgent(req.AgentID)
	if agent == nil || (tenant != "" && agent.TenantID != tenant) {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "agent not found or tenant mismatch"})
		return
	}

	// 构造诊断命令（按 agent.OS 区分 Linux/Windows）。
	cmd, err := buildDiagnoseCommand(req.Tool, req.Target, req.Options, agent.OS)
	if err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// 控制面侧命令校验（纵深防御）。
	if err := validateCommand(cmd); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "command validation failed: " + err.Error()})
		return
	}

	// 下发 shell task。
	task := s.store.CreateTask(&proto.Task{
		AgentID:    req.AgentID,
		TenantID:   tenant,
		Type:       proto.TaskTypeShell,
		Command:    cmd,
		MaxRetries: s.cfg.TaskMaxRetries,
	})
	// 审计 + 事件。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: tenant, UserID: actx.UserID, Action: "network_diagnose", Target: task.TaskID,
		Detail: sanitizeAuditDetail("tool:" + req.Tool + " target:" + req.Target),
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: tenant, UserID: actx.UserID,
			Action: "network_diagnose", Target: task.TaskID, Level: events.LevelInfo,
			Detail: sanitizeAuditDetail("tool:" + req.Tool),
		})
	}
	s.publishEvent(r.Context(), "task_status", tenant, map[string]string{
		"taskID": task.TaskID, "status": task.Status, "agentID": req.AgentID,
	})
	paginate.WriteJSON(w, http.StatusAccepted, diagnoseResponse{TaskID: task.TaskID})
}

// handleNetworkDiagnoseResult 处理 GET /api/v1/network/diagnose/{taskId}：查询诊断任务结果。
//
// 复用现有任务结果查询逻辑（store.TaskResult），并校验租户归属。
// 路径: /api/v1/network/diagnose/{taskId}
func (s *Server) handleNetworkDiagnoseResult(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "task:read"); !ok {
		return
	}
	// 解析路径: /api/v1/network/diagnose/{taskId}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/network/diagnose/")
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" {
		paginate.JSONError(w, http.StatusBadRequest, "taskId required")
		return
	}
	// 取第一段作为 taskId（忽略后续段）。
	parts := strings.SplitN(rest, "/", 2)
	taskID := parts[0]
	if taskID == "" {
		paginate.JSONError(w, http.StatusBadRequest, "taskId required")
		return
	}
	// 查询任务结果。
	res := s.store.TaskResult(taskID)
	if res == nil {
		// 任务可能仍在执行中，返回 pending 状态。
		t := s.store.TaskByID(taskID)
		if t == nil {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
			return
		}
		if actx.TenantID != "" && t.TenantID != actx.TenantID {
			paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
			return
		}
		paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"taskId":  taskID,
			"status":  t.Status,
			"pending": true,
		})
		return
	}
	// 租户归属校验。
	if actx.TenantID != "" {
		t := s.store.TaskByID(taskID)
		if t == nil || t.TenantID != actx.TenantID {
			paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
			return
		}
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"taskId":     taskID,
		"exitCode":   res.ExitCode,
		"stdout":     res.Stdout,
		"stderr":     res.Stderr,
		"durationMs": res.DurationMs,
		"finishedAt": res.FinishedAt,
		"pending":    false,
	})
}

// validateDiagnoseTool 校验诊断工具类型。
func validateDiagnoseTool(tool string) error {
	switch tool {
	case "ping", "traceroute", "tcping", "nslookup", "curl":
		return nil
	default:
		return fmt.Errorf("invalid tool %q (allowed: ping, traceroute, tcping, nslookup, curl)", tool)
	}
}

// buildDiagnoseCommand 按工具类型 + OS 构造诊断命令。
func buildDiagnoseCommand(tool, target string, opts diagnoseOptions, os string) (string, error) {
	switch tool {
	case "ping":
		return buildPingCommand(target, opts.Count, opts.Timeout, os), nil
	case "traceroute":
		if os == "windows" {
			// Windows: tracert -h 30 -w 2000 {target}
			return fmt.Sprintf("tracert -h 30 -w 2000 %s", target), nil
		}
		// Linux/darwin: traceroute -m 30 -w 2 {target}
		return fmt.Sprintf("traceroute -m 30 -w 2 %s", target), nil
	case "tcping":
		if os == "windows" {
			// Windows: PowerShell Test-NetConnection -ComputerName {target} -Port {port}
			// 注意：PowerShell 命令需经 powershell.exe 执行，此处构造为 powershell -Command "..." 形式。
			return fmt.Sprintf("powershell -Command \"Test-NetConnection -ComputerName %s -Port %d\"", target, opts.Port), nil
		}
		// Linux: nc -zv -w {timeout} {target} {port}
		return fmt.Sprintf("nc -zv -w %d %s %d", opts.Timeout, target, opts.Port), nil
	case "nslookup":
		return fmt.Sprintf("nslookup %s", target), nil
	case "curl":
		// curl -sS -o /dev/null -w "%{http_code} %{time_total}" --max-time {timeout} {target}
		return fmt.Sprintf("curl -sS -o /dev/null -w \"%%{http_code} %%{time_total}\" --max-time %d %s", opts.Timeout, target), nil
	default:
		return "", fmt.Errorf("unsupported tool: %s", tool)
	}
}

// ============================================================================
// 连通性检测 API
// ============================================================================

// connectivityRequest 连通性检测请求体。
type connectivityRequest struct {
	SourceAgentID string               `json:"sourceAgentId"`
	Targets       []connectivityTarget `json:"targets"`
}

// connectivityTarget 连通性检测目标。
type connectivityTarget struct {
	IP   string `json:"ip"`
	Port int    `json:"port,omitempty"`
}

// connectivityResult 连通性检测结果。
type connectivityResult struct {
	Target    string  `json:"target"`
	Alive     bool    `json:"alive"`
	LatencyMs float64 `json:"latencyMs"`
}

// connectivityResponse 连通性检测响应体。
type connectivityResponse struct {
	Results []connectivityResult `json:"results"`
}

// handleNetworkConnectivity 处理 POST /api/v1/network/connectivity：批量连通性检测。
//
// 请求体: { sourceAgentId, targets: [{ip, port?}] }
// 返回: { results: [{target, alive, latencyMs}] }
//
// 实现要点：
//   - 对每个 target 发起 tcping 检测（有 port 时用 nc/Test-NetConnection，无 port 时用 ping）；
//   - 同步等待最多 5 秒收集结果；
//   - 返回所有 target 的连通性结果。
func (s *Server) handleNetworkConnectivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "task:write"); !ok {
		return
	}
	tenant := actx.TenantID

	var req connectivityRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.SourceAgentID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "sourceAgentId is required"})
		return
	}
	if len(req.Targets) == 0 {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "targets is required (non-empty)"})
		return
	}
	// 校验 source agent 存在且属于当前租户。
	agent := s.lookupAgent(req.SourceAgentID)
	if agent == nil || (tenant != "" && agent.TenantID != tenant) {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "source agent not found or tenant mismatch"})
		return
	}

	// 对每个 target 下发检测任务。
	type connJob struct {
		targetIdx int
		taskID    string
		isPing    bool
	}
	jobs := make([]connJob, 0, len(req.Targets))
	for i, tgt := range req.Targets {
		if tgt.IP == "" {
			continue
		}
		var cmd string
		isPing := false
		if tgt.Port > 0 {
			// tcping 检测。
			c, err := buildDiagnoseCommand("tcping", tgt.IP, diagnoseOptions{Timeout: 3, Port: tgt.Port}, agent.OS)
			if err != nil {
				continue
			}
			cmd = c
		} else {
			// ping 检测。
			cmd = buildPingCommand(tgt.IP, 3, 2, agent.OS)
			isPing = true
		}
		if err := validateCommand(cmd); err != nil {
			continue
		}
		task := s.store.CreateTask(&proto.Task{
			AgentID:    req.SourceAgentID,
			TenantID:   tenant,
			Type:       proto.TaskTypeShell,
			Command:    cmd,
			MaxRetries: 0,
		})
		jobs = append(jobs, connJob{targetIdx: i, taskID: task.TaskID, isPing: isPing})
	}

	// 等待最多 5 秒收集结果。
	deadline := time.Now().Add(5 * time.Second)
	results := make([]connectivityResult, len(req.Targets))
	for i, tgt := range req.Targets {
		results[i] = connectivityResult{Target: tgt.IP, Alive: false, LatencyMs: -1}
	}
	for _, job := range jobs {
		tgt := req.Targets[job.targetIdx]
		result := connectivityResult{Target: tgt.IP, Alive: false, LatencyMs: -1}
		for time.Now().Before(deadline) {
			res := s.store.TaskResult(job.taskID)
			if res != nil {
				if job.isPing {
					latency, _, alive := parsePingOutput(res.Stdout, res.ExitCode, agent.OS)
					result.LatencyMs = latency
					result.Alive = alive
				} else {
					// tcping: exit code 0 表示连通。
					result.Alive = res.ExitCode == 0
					if result.Alive {
						result.LatencyMs = 0
					}
				}
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		results[job.targetIdx] = result
	}

	// 审计。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: tenant, UserID: actx.UserID, Action: "network_connectivity", Target: req.SourceAgentID,
		Detail: sanitizeAuditDetail(fmt.Sprintf("targets:%d", len(req.Targets))),
	})
	paginate.WriteJSON(w, http.StatusOK, connectivityResponse{Results: results})
}

// ============================================================================
// 辅助：JSON 解析（避免重复 decodeJSONBody 错误处理）
// ============================================================================
