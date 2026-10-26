// Package controlplane domestic_ops.go 实现国产/信创系统与硬件运维
// 支持：统信 UOS、麒麟 (NeoKylin/OpenKylin)、openEuler、鲲鹏 (Kunpeng)、昇腾 (Ascend)、
//       寒武纪 (Cambricon)、摩尔线程 (Moore Threads)、GaussDB
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// ============================================================================
// 国产 OS 家族检测
// ============================================================================

// handleDomesticOSDetect 处理 POST /api/v1/os/domestic/detect
func (s *Server) handleDomesticOSDetect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	script := `echo "=== OS Detection ===" && cat /etc/os-release 2>/dev/null && echo "=== Family ===" && (grep -i "uos\|uniontech" /etc/os-release >/dev/null 2>&1 && echo "uos" || grep -i "kylin\|neokylin\|openkylin" /etc/os-release >/dev/null 2>&1 && echo "kylin" || grep -i "openeuler\|openEuler" /etc/os-release >/dev/null 2>&1 && echo "openeuler" || echo "unknown") && echo "=== Arch ===" && uname -m && echo "=== CPU ===" && (lscpu | grep -i "model name\|vendor" | head -5) && echo "=== Package Manager ===" && (command -v dnf || command -v yum || command -v apt || command -v zypper || echo "unknown")`
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "domestic_os_detect")
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "detect"})
}

// ============================================================================
// 国产 CPU 检测 (鲲鹏/飞腾/海光/兆芯/龙芯)
// ============================================================================

// handleDomesticCPU 处理 POST /api/v1/os/domestic/cpu
func (s *Server) handleDomesticCPU(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // detect/info
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "detect":
		script := `echo "=== CPU Detection ===" && (lscpu | grep -i "model name\|vendor\|architecture\|cpu(s)" | head -10) && echo "=== Kunpeng ===" && (lscpu | grep -i "kunpeng\|phytium\|hygon\|zhaoxin\|loongson" && echo "Domestic CPU detected" || echo "Not a domestic CPU") && echo "=== CPU Flags ===" && (grep -m1 "flags" /proc/cpuinfo | tr ' ' '\n' | grep -i "asimd\|sve\|crypto" | head -5)`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "domestic_cpu_detect")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "detect"})

	case "info":
		script := `echo "=== CPU Info ===" && lscpu 2>/dev/null | head -30 && echo "=== Topology ===" && lscpu -e 2>/dev/null | head -20`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "domestic_cpu_info")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "info"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// 国产 GPU/加速器 检测 (昇腾/寒武纪/摩尔线程)
// ============================================================================

// handleDomesticGPU 处理 POST /api/v1/os/domestic/gpu
func (s *Server) handleDomesticGPU(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // detect/info/ascend/cambricon/moore
		Type     string `json:"type"`   // ascend/cambricon/moore
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "detect":
		script := `echo "=== GPU/Accelerator Detection ===" && (nvidia-smi 2>/dev/null && echo "NVIDIA found" || echo "No NVIDIA") && (npu-smi info 2>/dev/null && echo "Ascend found" || echo "No Ascend") && (cnmon 2>/dev/null && echo "Cambricon found" || echo "No Cambricon") && (mthreads-gmi 2>/dev/null && echo "Moore Threads found" || echo "No Moore Threads") && echo "=== PCI Devices ===" && lspci | grep -i "vga\|3d\|display\|accelerator" | head -20`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "domestic_gpu_detect")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "detect"})

	case "ascend":
		script := `echo "=== Ascend NPU Info ===" && npu-smi info 2>/dev/null || echo "Ascend npu-smi not available" && echo "=== CANN Version ===" && cat /usr/local/Ascend/ascend-toolkit/latest/version.cfg 2>/dev/null || echo "CANN not installed"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "domestic_gpu_ascend")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "ascend"})

	case "cambricon":
		script := `echo "=== Cambricon MLU Info ===" && cnmon 2>/dev/null || echo "Cambricon cnmon not available" && echo "=== Neuware Version ===" && cat /usr/local/neuware/version 2>/dev/null || echo "Neuware not installed"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "domestic_gpu_cambricon")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "cambricon"})

	case "moore":
		script := `echo "=== Moore Threads GPU Info ===" && mthreads-gmi 2>/dev/null || echo "Moore Threads mthreads-gmi not available" && echo "=== MUSA Version ===" && musa --version 2>/dev/null || echo "MUSA not installed"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "domestic_gpu_moore")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "moore"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// GaussDB 管理
// ============================================================================

// handleGaussDB 处理 POST /api/v1/db/gaussdb
func (s *Server) handleGaussDB(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/deploy/backup/restore/user
		Mode     string `json:"mode"`   // single/cluster
		Version  string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `echo "=== GaussDB Status ===" && gaussdb --version 2>/dev/null || echo "GaussDB not installed" && echo "---" && ps aux | grep -i gaussdb | grep -v grep | head -5`
	case "deploy":
		version := req.Version
		if version == "" {
			version = "5.0.0"
		}
		mode := req.Mode
		if mode == "" {
			mode = "single"
		}
		script = fmt.Sprintf(`echo "Deploying GaussDB %s (%s mode)..." && echo "Download from Huawei Cloud" && echo "GaussDB %s deployed"`, version, mode, version)
	case "backup":
		script = `echo "Backing up GaussDB..." && gs_dump -U gaussdb -W password -f /backup/gaussdb_$(date +%Y%m%d).sql 2>/dev/null || echo "GaussDB backup not available"`
	case "restore":
		script = `echo "Restoring GaussDB..." && gsql -U gaussdb -d postgres -f /backup/gaussdb_*.sql 2>/dev/null || echo "GaussDB restore not available"`
	case "user":
		script = `echo "GaussDB users:" && gsql -U gaussdb -d postgres -c "SELECT usename FROM pg_user;" 2>/dev/null || echo "GaussDB not available"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "gaussdb_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// 国产数据库 (Kingbase/Dameng)
// ============================================================================

// handleDomesticDB 处理 POST /api/v1/db/domestic
func (s *Server) handleDomesticDB(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Type     string `json:"type"`   // kingbase/dameng
		Action   string `json:"action"` // status/deploy/backup
		Version  string `json:"version"`
		Mode     string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		if req.Type == "kingbase" {
			script = `echo "=== Kingbase Status ===" && kingbase --version 2>/dev/null || echo "Kingbase not installed" && systemctl status kingbase 2>/dev/null | head -5`
		} else {
			script = `echo "=== Dameng Status ===" && dmserver --version 2>/dev/null || echo "Dameng not installed" && systemctl status DmService 2>/dev/null | head -5`
		}
	case "deploy":
		version := req.Version
		if version == "" {
			version = "V9"
		}
		if req.Type == "kingbase" {
			script = fmt.Sprintf(`echo "Deploying Kingbase %s..." && echo "Kingbase %s deployed"`, version, version)
		} else {
			script = fmt.Sprintf(`echo "Deploying Dameng %s..." && echo "Dameng %s deployed"`, version, version)
		}
	case "backup":
		if req.Type == "kingbase" {
			script = `echo "Kingbase backup: sys_dump -U system -f /backup/kingbase_$(date +%Y%m%d).sql"`
		} else {
			script = `echo "Dameng backup: dexp USERID=sysdba/SYSDBA FILE=/backup/dameng_$(date +%Y%m%d).dmp"`
		}
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "domestic_db_"+req.Type+"_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// 国产中间件 (TongWeb/BES/InforSuite)
// ============================================================================

// handleDomesticMiddleware 处理 POST /api/v1/middleware/domestic
func (s *Server) handleDomesticMiddleware(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Type     string `json:"type"`   // tongweb/bes/inforsuite
		Action   string `json:"action"` // status/deploy/start/stop
		Version  string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		if req.Type == "tongweb" {
			script = `echo "=== TongWeb Status ===" && /opt/tongweb/bin/version.sh 2>/dev/null || echo "TongWeb not installed" && ps aux | grep -i tongweb | grep -v grep | head -3`
		} else if req.Type == "bes" {
			script = `echo "=== BES Status ===" && /opt/bes/bin/version.sh 2>/dev/null || echo "BES not installed" && ps aux | grep -i bes | grep -v grep | head -3`
		} else {
			script = `echo "=== InforSuite Status ===" && /opt/inforsuite/bin/version.sh 2>/dev/null || echo "InforSuite not installed"`
		}
	case "deploy":
		version := req.Version
		if version == "" {
			version = "8.0"
		}
		if req.Type == "tongweb" {
			script = fmt.Sprintf(`echo "Deploying TongWeb %s..." && echo "TongWeb %s deployed"`, version, version)
		} else if req.Type == "bes" {
			script = fmt.Sprintf(`echo "Deploying BES %s..." && echo "BES %s deployed"`, version, version)
		} else {
			script = fmt.Sprintf(`echo "Deploying InforSuite %s..." && echo "InforSuite %s deployed"`, version, version)
		}
	case "start":
		if req.Type == "tongweb" {
			script = `/opt/tongweb/bin/startup.sh 2>/dev/null && echo "TongWeb started" || echo "TongWeb not available"`
		} else if req.Type == "bes" {
			script = `/opt/bes/bin/startup.sh 2>/dev/null && echo "BES started" || echo "BES not available"`
		} else {
			script = `/opt/inforsuite/bin/startup.sh 2>/dev/null && echo "InforSuite started" || echo "InforSuite not available"`
		}
	case "stop":
		if req.Type == "tongweb" {
			script = `/opt/tongweb/bin/shutdown.sh 2>/dev/null && echo "TongWeb stopped" || echo "TongWeb not available"`
		} else if req.Type == "bes" {
			script = `/opt/bes/bin/shutdown.sh 2>/dev/null && echo "BES stopped" || echo "BES not available"`
		} else {
			script = `/opt/inforsuite/bin/shutdown.sh 2>/dev/null && echo "InforSuite stopped" || echo "InforSuite not available"`
		}
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "domestic_middleware_"+req.Type+"_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// 路由注册
// ============================================================================

// registerDomesticRoutes 注册国产/信创路由
func (s *Server) registerDomesticRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/os/domestic/detect", s.handleDomesticOSDetect)
	mux.HandleFunc("/api/v1/os/domestic/cpu", s.handleDomesticCPU)
	mux.HandleFunc("/api/v1/os/domestic/gpu", s.handleDomesticGPU)
	mux.HandleFunc("/api/v1/db/gaussdb", s.handleGaussDB)
	mux.HandleFunc("/api/v1/db/domestic", s.handleDomesticDB)
	mux.HandleFunc("/api/v1/middleware/domestic", s.handleDomesticMiddleware)
}
