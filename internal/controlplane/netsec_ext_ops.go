// Package controlplane netsec_ext_ops.go 实现扩展网络安全运维
// 覆盖：IPS/IDS、SIEM、蜜罐、流量整形、VPN、零信任
//
// TD-60 device 域下沉：脚本构建已抽至 pkg/netsec（纯函数），本文件只保留
// HTTP 解析与任务下发。device-svc 侧复用同一 pkg，避免双份实现漂移。
package controlplane

import (
	"encoding/json"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/pkg/netsec"
)

// handleNetSec 是六个 netsec 端点共用的处理骨架：
// 解析 {deviceID, action} → pkg/netsec 取脚本 → dispatchKafkaTask 下发。
// cap 标识能力，供脚本选择与任务类型标签使用。
func (s *Server) handleNetSec(w http.ResponseWriter, r *http.Request, cap netsec.Capability) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req netsec.ActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	script, ok := netsec.Build(cap, req.Action)
	if !ok {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, netsec.TaskAction(cap, req.Action))
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleIPSIDS 处理 POST /api/v1/netsec/ips
func (s *Server) handleIPSIDS(w http.ResponseWriter, r *http.Request) {
	s.handleNetSec(w, r, netsec.CapIPSIDS)
}

// handleSIEM 处理 POST /api/v1/netsec/siem
func (s *Server) handleSIEM(w http.ResponseWriter, r *http.Request) {
	s.handleNetSec(w, r, netsec.CapSIEM)
}

// handleHoneypot 处理 POST /api/v1/netsec/honeypot
func (s *Server) handleHoneypot(w http.ResponseWriter, r *http.Request) {
	s.handleNetSec(w, r, netsec.CapHoneypot)
}

// handleTrafficShaping 处理 POST /api/v1/netsec/traffic
func (s *Server) handleTrafficShaping(w http.ResponseWriter, r *http.Request) {
	s.handleNetSec(w, r, netsec.CapTrafficShaping)
}

// handleVPN 处理 POST /api/v1/netsec/vpn
func (s *Server) handleVPN(w http.ResponseWriter, r *http.Request) {
	s.handleNetSec(w, r, netsec.CapVPN)
}

// handleZeroTrust 处理 POST /api/v1/netsec/zerotrust
func (s *Server) handleZeroTrust(w http.ResponseWriter, r *http.Request) {
	s.handleNetSec(w, r, netsec.CapZeroTrust)
}

// registerNetSecExtRoutes 注册网络安全扩展路由
func (s *Server) registerNetSecExtRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/netsec/ips", s.handleIPSIDS)
	mux.HandleFunc("/api/v1/netsec/siem", s.handleSIEM)
	mux.HandleFunc("/api/v1/netsec/honeypot", s.handleHoneypot)
	mux.HandleFunc("/api/v1/netsec/traffic", s.handleTrafficShaping)
	mux.HandleFunc("/api/v1/netsec/vpn", s.handleVPN)
	mux.HandleFunc("/api/v1/netsec/zerotrust", s.handleZeroTrust)
}
