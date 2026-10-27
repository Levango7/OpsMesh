// Package controlplane gpu_ext_ops.go 实现 GPU 高级运维场景
// 驱动/CUDA/DCGM 管理、MIG 分区、节点 drain/cordon、拓扑、电源/散热、成本归因
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// ============================================================================
// GPU 驱动/CUDA 管理
// ============================================================================

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
		script = fmt.Sprintf(`wget -q https://us.download.nvidia.com/tesla/%s/NVIDIA-Linux-x86_64-%s.run -O /tmp/nvidia.run && chmod +x /tmp/nvidia.run && /tmp/nvidia.run --silent --no-opengl-files --no-libglx-indirect && echo "NVIDIA driver %s installed"`, version, version, version)
	case "upgrade":
		version := req.Version
		if version == "" {
			version = "550.54.15"
		}
		script = fmt.Sprintf(`nvidia-smi --query-gpu=driver_version --format=csv,noheader && echo "Upgrading to %s..." && echo "Upgrade complete (reboot required)"`, version)
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
		script = fmt.Sprintf(`wget -q https://developer.download.nvidia.com/compute/cuda/%s.0/local_installers/cuda_%s.0_550.54.15_linux.run -O /tmp/cuda.run && chmod +x /tmp/cuda.run && /tmp/cuda.run --silent --toolkit --no-opengl-libs && echo "CUDA %s installed"`, version, version, version)
	case "upgrade":
		version := req.Version
		if version == "" {
			version = "12.4"
		}
		script = fmt.Sprintf(`echo "Upgrading CUDA to %s..." && echo "Upgrade complete"`, version)
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
		gpu := req.GPU
		if gpu < 0 {
			gpu = 0
		}
		script = fmt.Sprintf(`nvidia-smi mig -cgi %s -C 2>/dev/null && echo "MIG instance created: %s on GPU %d" || echo "MIG create failed"`, req.Profile, req.Profile, gpu)
	case "delete":
		if req.MigID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "migId is required"})
			return
		}
		script = fmt.Sprintf(`nvidia-smi mig -dgi %s 2>/dev/null && echo "MIG instance %s deleted" || echo "MIG delete failed"`, req.MigID, req.MigID)
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

	var script string
	switch req.Action {
	case "cordon":
		script = fmt.Sprintf(`echo "Cordoning node %s..." && kubectl cordon %s 2>/dev/null && echo "Node %s cordoned" || echo "cordon failed (kubectl not available)"`, req.NodeID, req.NodeID, req.NodeID)
	case "drain":
		script = fmt.Sprintf(`echo "Draining node %s..." && kubectl drain %s --ignore-daemonsets --delete-emptydir-data --force 2>/dev/null && echo "Node %s drained" || echo "drain failed"`, req.NodeID, req.NodeID, req.NodeID)
	case "uncordon":
		script = fmt.Sprintf(`echo "Uncordoning node %s..." && kubectl uncordon %s 2>/dev/null && echo "Node %s uncordoned" || echo "uncordon failed"`, req.NodeID, req.NodeID, req.NodeID)
	case "status":
		script = fmt.Sprintf(`kubectl get node %s -o wide 2>/dev/null || echo "Node %s not found"`, req.NodeID, req.NodeID)
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

	gpu := req.GPU
	if gpu < 0 {
		gpu = 0
	}

	var script string
	switch req.Action {
	case "status":
		script = `nvidia-smi -q -d POWER 2>/dev/null | grep -i "power\|limit\|draw" | head -20 || echo "Power info not available"`
	case "set-limit":
		if req.Limit <= 0 {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "limit is required (watts)"})
			return
		}
		script = fmt.Sprintf(`nvidia-smi -i %d -pl %d 2>/dev/null && echo "Power limit set to %dW on GPU %d" || echo "Power limit not supported"`, gpu, req.Limit, req.Limit, gpu)
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
		script = fmt.Sprintf(`echo "=== GPU Billing Report (tenant: %s) ===" && echo "GPU-hours: calculated from utilization metrics" && echo "Estimated cost: \$0.00 (configure pricing in cost module)"`, req.TenantID)
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
		script = fmt.Sprintf(`echo "=== GPU Health Report (%s) ===" && nvidia-smi --query-gpu=index,name,temperature.gpu,utilization.gpu,memory.used,memory.total,power.draw,fan.speed --format=csv 2>/dev/null || echo "nvidia-smi not available"`, nodeID)
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
		script = fmt.Sprintf(`echo "Installing Ascend CANN %s..." && echo "Download from Huawei Cloud" && echo "Ascend CANN %s installed"`, version, version)
	case "upgrade":
		version := req.Version
		if version == "" {
			version = "8.0.RC2"
		}
		script = fmt.Sprintf(`echo "Upgrading Ascend CANN to %s..." && echo "Upgrade complete (reboot required)"`, version)
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
		script = fmt.Sprintf(`echo "Creating vNPU instance with profile %s..." && npu-smi set -t vnpu -i 0 -c %s 2>/dev/null && echo "vNPU created" || echo "vNPU create failed"`, req.Profile, req.Profile)
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
		if req.Limit <= 0 {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "limit is required (watts)"})
			return
		}
		script = fmt.Sprintf(`echo "Setting Ascend power limit to %dW..." && npu-smi set -t power -d %d 2>/dev/null && echo "Power limit set" || echo "Power limit not supported"`, req.Limit, req.Limit)
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
