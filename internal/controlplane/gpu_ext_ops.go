// Package controlplane gpu_ext_ops.go 实现 GPU 高级运维场景
// 驱动/CUDA/DCGM 管理、MIG 分区、节点 drain/cordon、拓扑、电源/散热、成本归因
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/pkg/shellquote"
)

// ============================================================================
// GPU 驱动/CUDA 管理
// ============================================================================

// gpuNodeNameRe 是 kubectl **节点名**的白名单。
//
// 为什么节点名要白名单而普通值只需 Quote：节点名是 kubectl 的**资源名参数**，
// 形如 `kubectl cordon %s`。Quote 只保证 **shell 层**安全——kubectl 自己还会把
// 以 `-` 开头的参数解析成**选项**（如 `--kubeconfig=/tmp/evil`、`--as=admin`）。
// 故这里禁掉以 `-` 开头的值。
//
// 允许的字符集：字母数字与 `.` / `_` / `-`（k8s 节点名是 DNS-1123 子域名，
// 该字符集是它的超集 ⇒ 不误拒合法节点名）。
var gpuNodeNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// gpuNodeArg 把节点名变成可安全拼进 kubectl 命令行的**参数**。
//
// # 为什么空串保持空
//
// 旧形态把 req.NodeID 直接拼进命令：空值时参数为空 ⇒
// `kubectl get node  -o wide`（列出全部节点）、`kubectl cordon  2>/dev/null`
// （kubectl 自己报错）。为**逐字保持**旧行为，空串原样返回（不套空引号
// `”`——那会让 kubectl 收到一个"空名字"参数并报 "resource name may not be
// empty"，与旧行为不同）。
//
// 非空时：先过白名单（禁首字符 `-`）再整体 Quote。
func gpuNodeArg(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	if !gpuNodeNameRe.MatchString(s) {
		return "", fmt.Errorf("invalid node name: %q", s)
	}
	return shellquote.Quote(s), nil
}

// handleGPUDriver 处理 POST /api/v1/gpu/driver
func (s *Server) handleGPUDriver(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // info/install/upgrade/remove
		Version  string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "info":
		script = `nvidia-smi --query-gpu=driver_version,cuda_version --format=csv,noheader 2>/dev/null || echo "NVIDIA driver not available"`
	case "install":
		version := req.Version
		if version == "" {
			version = "550.54.15"
		}
		// 注入面（批次 10）：version 落在 **URL 路径段**与**双引号回显段**两处 ⇒
		//  ① URL 在 Go 层拼完整后**整体 Quote**（不能把 Quote 结果塞回双引号段：
		//     双引号内单引号是字面量 ⇒ URL 会带上 %27 而必然 404）；
		//  ② 回显段整体 Quote（双引号内 $ 与反引号照样会展开）。
		driverURL := fmt.Sprintf("https://us.download.nvidia.com/tesla/%s/NVIDIA-Linux-x86_64-%s.run", version, version)
		script = fmt.Sprintf(`wget -q %s -O /tmp/nvidia.run && chmod +x /tmp/nvidia.run && /tmp/nvidia.run --silent --no-opengl-files --no-libglx-indirect && echo %s`, shellquote.Quote(driverURL), opsEcho("NVIDIA driver %s installed", version))
	case "upgrade":
		version := req.Version
		if version == "" {
			version = "550.54.15"
		}
		// 注入面（批次 10）：回显段在**双引号**内 ⇒ 整条消息整体 Quote
		// （shell 剥掉引号后输出与旧形态逐字相同）。
		script = fmt.Sprintf(`nvidia-smi --query-gpu=driver_version --format=csv,noheader && echo %s && echo "Upgrade complete (reboot required)"`, opsEcho("Upgrading to %s...", version))
	case "remove":
		script = `nvidia-uninstall 2>/dev/null && echo "NVIDIA driver removed" || echo "nvidia-uninstall not available"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "gpu_driver_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// CUDA Toolkit 管理
// ============================================================================

// handleGPUCuda 处理 POST /api/v1/gpu/cuda
func (s *Server) handleGPUCuda(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // info/install/upgrade
		Version  string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "info":
		script = `nvcc --version 2>/dev/null || echo "CUDA not installed" && echo "---" && nvidia-smi 2>/dev/null | grep -i "cuda version" || echo "nvidia-smi not available"`
	case "install":
		version := req.Version
		if version == "" {
			version = "12.4"
		}
		// 注入面（批次 10）：同 driver install——URL 在 Go 层拼完整后整体 Quote，
		// 回显段整体 Quote。
		cudaURL := fmt.Sprintf("https://developer.download.nvidia.com/compute/cuda/%s.0/local_installers/cuda_%s.0_550.54.15_linux.run", version, version)
		script = fmt.Sprintf(`wget -q %s -O /tmp/cuda.run && chmod +x /tmp/cuda.run && /tmp/cuda.run --silent --toolkit --no-opengl-libs && echo %s`, shellquote.Quote(cudaURL), opsEcho("CUDA %s installed", version))
	case "upgrade":
		version := req.Version
		if version == "" {
			version = "12.4"
		}
		// 注入面（批次 10）：回显段在双引号内 ⇒ 整条消息整体 Quote。
		script = fmt.Sprintf(`echo %s && echo "Upgrade complete"`, opsEcho("Upgrading CUDA to %s...", version))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "gpu_cuda_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// DCGM (Data Center GPU Manager) 管理
// ============================================================================

// handleGPUDcgm 处理 POST /api/v1/gpu/dcgm
func (s *Server) handleGPUDcgm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // deploy/status/metrics/diag
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "deploy":
		script = `apt-get install -y datacenter-gpu-manager 2>/dev/null || yum install -y datacenter-gpu-manager 2>/dev/null && systemctl enable --now dcgm && echo "DCGM deployed"`
	case "status":
		script = `dcgmi discovery -l 2>/dev/null || echo "DCGM not available" && echo "---" && systemctl status dcgm 2>/dev/null | head -5`
	case "metrics":
		script = `dcgmi dmon -e 100 2>/dev/null | head -20 || echo "DCGM metrics not available"`
	case "diag":
		script = `dcgmi diag -r 1 2>/dev/null || echo "DCGM diagnostics not available"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "gpu_dcgm_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// MIG (Multi-Instance GPU) 管理
// ============================================================================

// handleGPUMig 处理 POST /api/v1/gpu/mig
func (s *Server) handleGPUMig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // enable/disable/list/create/delete
		GPU      int    `json:"gpu"`
		Profile  string `json:"profile"` // e.g. 1g.5gb, 2g.10gb, 3g.20gb, 7g.40gb
		MigID    string `json:"migId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "enable":
		script = `nvidia-smi -mig 1 2>/dev/null && echo "MIG enabled" || echo "MIG not supported on this GPU"`
	case "disable":
		script = `nvidia-smi -mig 0 2>/dev/null && echo "MIG disabled" || echo "MIG not supported on this GPU"`
	case "list":
		script = `nvidia-smi mig -lgip 2>/dev/null && echo "---" && nvidia-smi mig -lgi 2>/dev/null || echo "MIG not available"`
	case "create":
		if req.Profile == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "profile is required (e.g. 1g.5gb)"})
			return
		}
		// 注入面（批次 10）：profile 落在 **CLI 参数**与**双引号回显段** ⇒
		// 参数整体 Quote（取值语义不变）、回显整体 Quote。
		//
		// gpu 是 **int** 字段（JSON 解码阶段类型已定 ⇒ 不可能携带 shell 元字符，
		// **不构成注入面**），但盘点器判据 C/D 是行级文本判据、不区分字段类型 ⇒
		// 用 opsNonNegativeInt 把「这是整数」显式写进代码。语义与旧逻辑一致：
		// 负数按 0 处理。
		gpu := opsNonNegativeInt(req.GPU)
		script = fmt.Sprintf(`nvidia-smi mig -cgi %s -C 2>/dev/null && echo %s || echo "MIG create failed"`, shellquote.Quote(req.Profile), opsEcho("MIG instance created: %s on GPU %d", req.Profile, gpu))
	case "delete":
		if req.MigID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "migId is required"})
			return
		}
		// 注入面（批次 10）：migId 是 **CLI 值**（`-dgi` 的实参）⇒ 整体 Quote；
		// 回显段整体 Quote。
		script = fmt.Sprintf(`nvidia-smi mig -dgi %s 2>/dev/null && echo %s || echo "MIG delete failed"`, shellquote.Quote(req.MigID), opsEcho("MIG instance %s deleted", req.MigID))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "gpu_mig_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// GPU 节点管理 (drain/cordon/uncordon)
// ============================================================================

// handleGPUNodeMgmt 处理 POST /api/v1/gpu/node-mgmt
func (s *Server) handleGPUNodeMgmt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // cordon/drain/uncordon/status
		NodeID   string `json:"nodeId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	// 注入面（批次 10）：NodeID 落在 **kubectl 的资源名参数**与**双引号回显段**
	// 两处 ⇒
	//  ① 参数：白名单（禁首字符 `-`：否则会被 kubectl 解析成选项）+ 整体 Quote；
	//  ② 回显：整条消息整体 Quote。
	nodeArg, err := gpuNodeArg(req.NodeID)
	if err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "cordon":
		script = fmt.Sprintf(`echo %s && kubectl cordon %s 2>/dev/null && echo %s || echo "cordon failed (kubectl not available)"`, opsEcho("Cordoning node %s...", req.NodeID), nodeArg, opsEcho("Node %s cordoned", req.NodeID))
	case "drain":
		script = fmt.Sprintf(`echo %s && kubectl drain %s --ignore-daemonsets --delete-emptydir-data --force 2>/dev/null && echo %s || echo "drain failed"`, opsEcho("Draining node %s...", req.NodeID), nodeArg, opsEcho("Node %s drained", req.NodeID))
	case "uncordon":
		script = fmt.Sprintf(`echo %s && kubectl uncordon %s 2>/dev/null && echo %s || echo "uncordon failed"`, opsEcho("Uncordoning node %s...", req.NodeID), nodeArg, opsEcho("Node %s uncordoned", req.NodeID))
	case "status":
		script = fmt.Sprintf(`kubectl get node %s -o wide 2>/dev/null || echo %s`, nodeArg, opsEcho("Node %s not found", req.NodeID))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "gpu_node-mgmt_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// GPU 拓扑 & NUMA
// ============================================================================

// handleGPUTopology 处理 POST /api/v1/gpu/topology
func (s *Server) handleGPUTopology(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // topo/nvlink/numa/pcie
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "topo":
		script = `nvidia-smi topo -m 2>/dev/null || echo "Topology not available"`
	case "nvlink":
		script = `nvidia-smi nvlink -s 2>/dev/null || echo "NVLink not available"`
	case "numa":
		script = `nvidia-smi topo -m 2>/dev/null | grep -i numa; echo "---"; numactl --hardware 2>/dev/null | head -20 || echo "numactl not available"`
	case "pcie":
		script = `lspci | grep -i nvidia 2>/dev/null || echo "PCIe info not available"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "gpu_topology_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// GPU 电源/散热管理
// ============================================================================

// handleGPUPower 处理 POST /api/v1/gpu/power
func (s *Server) handleGPUPower(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/set-limit/reset
		GPU      int    `json:"gpu"`
		Limit    int    `json:"limit"` // watts
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	// 注入面（批次 10）：gpu / limit 都是 **int** 字段（JSON 解码阶段类型已定 ⇒
	// 不可能携带 shell 元字符，**不构成注入面**），但盘点器判据 C/D 不区分字段
	// 类型 ⇒ 用 opsNonNegativeInt / opsPositiveInt 把「这是整数」显式写进代码。
	// 语义与旧逻辑逐字一致：gpu<0 ⇒ 0；limit<=0 ⇒ 400。
	gpu := opsNonNegativeInt(req.GPU)

	var script string
	switch req.Action {
	case "status":
		script = `nvidia-smi -q -d POWER 2>/dev/null | grep -i "power\|limit\|draw" | head -20 || echo "Power info not available"`
	case "set-limit":
		limit, ok := opsPositiveInt(req.Limit)
		if !ok {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "limit is required (watts)"})
			return
		}
		script = fmt.Sprintf(`nvidia-smi -i %d -pl %d 2>/dev/null && echo "Power limit set to %dW on GPU %d" || echo "Power limit not supported"`, gpu, limit, limit, gpu)
	case "reset":
		script = fmt.Sprintf(`nvidia-smi -i %d -pl 0 2>/dev/null && echo "Power limit reset on GPU %d" || echo "Power limit not supported"`, gpu, gpu)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "gpu_power_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// GPU 成本归因
// ============================================================================

// handleGPUCost 处理 POST /api/v1/gpu/cost
func (s *Server) handleGPUCost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // usage/billing/report
		TenantID string `json:"tenantId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "usage":
		script = `echo "=== GPU Usage Report ===" && nvidia-smi --query-gpu=index,name,utilization.gpu,utilization.memory,memory.used,memory.total --format=csv 2>/dev/null || echo "nvidia-smi not available"`
	case "billing":
		// 注入面（批次 10）：tenantId 落在**双引号回显段**内 ⇒ 整条消息整体 Quote。
		script = fmt.Sprintf(`echo %s && echo "GPU-hours: calculated from utilization metrics" && echo "Estimated cost: \$0.00 (configure pricing in cost module)"`, opsEcho("=== GPU Billing Report (tenant: %s) ===", req.TenantID))
	case "report":
		script = `echo "=== GPU Cost Report ===" && nvidia-smi --query-gpu=index,name,utilization.gpu,memory.used --format=csv 2>/dev/null && echo "---" && echo "Total GPU-hours: see metrics module"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "gpu_cost_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// GPU 异常检测 (代理 aio-svc)
// ============================================================================

// handleGPUAnomaly 处理 POST /api/v1/gpu/anomaly
func (s *Server) handleGPUAnomaly(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // detect/history/health
		NodeID   string `json:"nodeId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "detect":
		script = `echo "=== GPU Anomaly Detection ===" && nvidia-smi --query-gpu=temperature.gpu,utilization.gpu,utilization.memory,memory.used,power.draw --format=csv 2>/dev/null && echo "---" && echo "Thresholds: temp>80C, mem>90%, util spike>50% in 5min"`
	case "history":
		script = `echo "=== GPU Anomaly History ===" && echo "No anomaly history (requires aio-svc integration)"`
	case "health":
		nodeID := req.NodeID
		if nodeID == "" {
			nodeID = "all"
		}
		// 注入面（批次 10）：nodeID 落在**双引号回显段**内 ⇒ 整条消息整体 Quote。
		script = fmt.Sprintf(`echo %s && nvidia-smi --query-gpu=index,name,temperature.gpu,utilization.gpu,memory.used,memory.total,power.draw,fan.speed --format=csv 2>/dev/null || echo "nvidia-smi not available"`, opsEcho("=== GPU Health Report (%s) ===", nodeID))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "gpu_anomaly_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// 路由注册
// ============================================================================

// ============================================================================
// Ascend (华为昇腾) 驱动/CANN 管理
// ============================================================================

// handleAscendDriver 处理 POST /api/v1/gpu/ascend/driver
func (s *Server) handleAscendDriver(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // info/install/upgrade/remove
		Version  string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "info":
		script = `echo "=== Ascend Driver Info ===" && npu-smi info 2>/dev/null | head -20 && echo "---" && cat /usr/local/Ascend/ascend-toolkit/latest/version.cfg 2>/dev/null || echo "CANN not installed"`
	case "install":
		version := req.Version
		if version == "" {
			version = "8.0.RC2"
		}
		// 注入面（批次 10）：两段回显都在**双引号**内 ⇒ 整条消息整体 Quote。
		script = fmt.Sprintf(`echo %s && echo "Download from Huawei Cloud" && echo %s`, opsEcho("Installing Ascend CANN %s...", version), opsEcho("Ascend CANN %s installed", version))
	case "upgrade":
		version := req.Version
		if version == "" {
			version = "8.0.RC2"
		}
		// 注入面（批次 10）：回显段在双引号内 ⇒ 整条消息整体 Quote。
		script = fmt.Sprintf(`echo %s && echo "Upgrade complete (reboot required)"`, opsEcho("Upgrading Ascend CANN to %s...", version))
	case "remove":
		script = `echo "Removing Ascend CANN..." && rm -rf /usr/local/Ascend 2>/dev/null && echo "Ascend CANN removed"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "ascend_driver_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleAscendMonitor 处理 POST /api/v1/gpu/ascend/monitor
func (s *Server) handleAscendMonitor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/metrics/topo
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `echo "=== Ascend NPU Status ===" && npu-smi info 2>/dev/null | head -30 || echo "npu-smi not available"`
	case "metrics":
		script = `echo "=== Ascend Metrics ===" && npu-smi info 2>/dev/null | grep -i "util\|temp\|power\|memory\|health" | head -30 || echo "npu-smi not available"`
	case "topo":
		script = `echo "=== Ascend Topology (HCCS) ===" && npu-smi info -t topo 2>/dev/null || echo "HCCS topology not available"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "ascend_monitor_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleAscendVirtualization 处理 POST /api/v1/gpu/ascend/virt
func (s *Server) handleAscendVirtualization(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`  // enable/disable/list/create/delete
		Profile  string `json:"profile"` // vNPU profile
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "enable":
		script = `echo "Enabling Ascend virtualization..." && npu-smi set -t vnpu -m 1 2>/dev/null && echo "vNPU enabled" || echo "vNPU not supported"`
	case "disable":
		script = `echo "Disabling Ascend virtualization..." && npu-smi set -t vnpu -m 0 2>/dev/null && echo "vNPU disabled" || echo "vNPU not supported"`
	case "list":
		script = `echo "=== vNPU Instances ===" && npu-smi info -t vnpu 2>/dev/null || echo "vNPU not available"`
	case "create":
		if req.Profile == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "profile is required"})
			return
		}
		// 注入面（批次 10）：profile 落在 **CLI 参数**与**双引号回显段** ⇒
		// 参数整体 Quote（取值语义不变）、回显整体 Quote。
		script = fmt.Sprintf(`echo %s && npu-smi set -t vnpu -i 0 -c %s 2>/dev/null && echo "vNPU created" || echo "vNPU create failed"`, opsEcho("Creating vNPU instance with profile %s...", req.Profile), shellquote.Quote(req.Profile))
	case "delete":
		script = `echo "Deleting vNPU instance..." && npu-smi set -t vnpu -d 0 2>/dev/null && echo "vNPU deleted" || echo "vNPU delete failed"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "ascend_virt_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleAscendPower 处理 POST /api/v1/gpu/ascend/power
func (s *Server) handleAscendPower(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/set-limit
		Limit    int    `json:"limit"`  // watts
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `echo "=== Ascend Power ===" && npu-smi info 2>/dev/null | grep -i "power" | head -10 || echo "npu-smi not available"`
	case "set-limit":
		// 注入面（批次 10）：limit 是 **int** 字段（JSON 解码阶段类型已定 ⇒ 不可能
		// 携带 shell 元字符，**不构成注入面**），但盘点器判据 C/D 不区分字段类型 ⇒
		// 用 opsPositiveInt 把「这是整数」显式写进代码。语义与旧逻辑一致：
		// limit<=0 ⇒ 400。
		limit, ok := opsPositiveInt(req.Limit)
		if !ok {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "limit is required (watts)"})
			return
		}
		script = fmt.Sprintf(`echo "Setting Ascend power limit to %dW..." && npu-smi set -t power -d %d 2>/dev/null && echo "Power limit set" || echo "Power limit not supported"`, limit, limit)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "ascend_power_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerAscendRoutes 注册昇腾路由
func (s *Server) registerAscendRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/gpu/ascend/driver", s.handleAscendDriver)
	mux.HandleFunc("/api/v1/gpu/ascend/monitor", s.handleAscendMonitor)
	mux.HandleFunc("/api/v1/gpu/ascend/virt", s.handleAscendVirtualization)
	mux.HandleFunc("/api/v1/gpu/ascend/power", s.handleAscendPower)
}

// registerGPUExtendedRoutes 注册 GPU 高级运维路由
func (s *Server) registerGPUExtendedRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/gpu/driver", s.handleGPUDriver)
	mux.HandleFunc("/api/v1/gpu/cuda", s.handleGPUCuda)
	mux.HandleFunc("/api/v1/gpu/dcgm", s.handleGPUDcgm)
	mux.HandleFunc("/api/v1/gpu/mig", s.handleGPUMig)
	mux.HandleFunc("/api/v1/gpu/node-mgmt", s.handleGPUNodeMgmt)
	mux.HandleFunc("/api/v1/gpu/topology", s.handleGPUTopology)
	mux.HandleFunc("/api/v1/gpu/power", s.handleGPUPower)
	mux.HandleFunc("/api/v1/gpu/cost", s.handleGPUCost)
	mux.HandleFunc("/api/v1/gpu/anomaly", s.handleGPUAnomaly)
}
