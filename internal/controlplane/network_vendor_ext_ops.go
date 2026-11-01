// Package controlplane network_vendor_ext_ops.go 实现扩展网络设备厂商运维支持
// 覆盖：F5、Citrix NetScaler、Palo Alto、Fortinet、pfSense
package controlplane

import (
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleNetworkF5 处理 POST /api/v1/network/vendor/f5
func (s *Server) handleNetworkF5(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/vs/pool/node/monitor/irule
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `tmsh show sys version && tmsh show ltm virtual`
	case "vs":
		script = `tmsh list ltm virtual`
	case "pool":
		script = `tmsh list ltm pool`
	case "node":
		script = `tmsh list ltm node`
	case "monitor":
		script = `tmsh list ltm monitor`
	case "irule":
		script = `tmsh list ltm rule`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "network_f5_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleNetworkCitrix 处理 POST /api/v1/network/vendor/citrix
func (s *Server) handleNetworkCitrix(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/vs/service/lb/vserver
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `show version && show ns runningConfig`
	case "vs":
		script = `show lb vserver`
	case "service":
		script = `show service`
	case "lb":
		script = `show lb monitor`
	case "vserver":
		script = `show cs vserver`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "network_citrix_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleNetworkPaloAlto 处理 POST /api/v1/network/vendor/paloalto
func (s *Server) handleNetworkPaloAlto(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/policy/nat/security-zone/address
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `show system info && show system resources`
	case "policy":
		script = `show running security-policy`
	case "nat":
		script = `show running nat-policy`
	case "security-zone":
		script = `show zone`
	case "address":
		script = `show address`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "network_paloalto_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleNetworkFortinet 处理 POST /api/v1/network/vendor/fortinet
func (s *Server) handleNetworkFortinet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/policy/vdom/interface/vpn
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `get system status && get system performance`
	case "policy":
		script = `show firewall policy`
	case "vdom":
		script = `show vdom`
	case "interface":
		script = `show system interface`
	case "vpn":
		script = `diagnose vpn tunnel list`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "network_fortinet_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleNetworkPfSense 处理 POST /api/v1/network/vendor/pfsense
func (s *Server) handleNetworkPfSense(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/firewall/nat/interface/vpn
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `pfctl -s info && pfctl -s states`
	case "firewall":
		script = `pfctl -sr`
	case "nat":
		script = `pfctl -sn`
	case "interface":
		script = `ifconfig`
	case "vpn":
		script = `openvpn --status`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "network_pfsense_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerNetworkVendorExtRoutes 注册网络设备厂商扩展运维路由
func (s *Server) registerNetworkVendorExtRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/network/vendor/f5", s.handleNetworkF5)
	mux.HandleFunc("/api/v1/network/vendor/citrix", s.handleNetworkCitrix)
	mux.HandleFunc("/api/v1/network/vendor/paloalto", s.handleNetworkPaloAlto)
	mux.HandleFunc("/api/v1/network/vendor/fortinet", s.handleNetworkFortinet)
	mux.HandleFunc("/api/v1/network/vendor/pfsense", s.handleNetworkPfSense)
}
