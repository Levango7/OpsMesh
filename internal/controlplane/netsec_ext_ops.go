// Package controlplane netsec_ext_ops.go 实现扩展网络安全运维
// 覆盖：IPS/IDS、SIEM、蜜罐、流量整形、VPN、零信任
package controlplane

import (
	"encoding/json"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleIPSIDS 处理 POST /api/v1/netsec/ips
func (s *Server) handleIPSIDS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/deploy/enable/disable/rules/update
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `echo "=== IPS/IDS Status ===" && (suricata --version 2>/dev/null || echo "suricata not available") && (snort -V 2>/dev/null || echo "snort not available") && (zeek --version 2>/dev/null || echo "zeek not available")`
	case "deploy":
		script = `# deploy: apt install suricata/snort/zeek
apt-get install -y suricata snort zeek 2>/dev/null || apt install -y suricata snort zeek`
	case "enable":
		script = `systemctl start suricata && echo "suricata enabled"`
	case "disable":
		script = `systemctl stop suricata && echo "suricata disabled"`
	case "rules":
		script = `suricata-update && echo "rules updated"`
	case "update":
		script = `suricata-update && echo "updated"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "ips_ids_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleSIEM 处理 POST /api/v1/netsec/siem
func (s *Server) handleSIEM(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/deploy/enable/disable/rules
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `echo "=== SIEM Status ===" && (wazuh-agent status 2>/dev/null || echo "wazuh-agent not available") && (ossec status 2>/dev/null || echo "ossec not available")`
	case "deploy":
		script = `# deploy: apt install wazuh-agent
apt-get install -y wazuh-agent 2>/dev/null || apt install -y wazuh-agent`
	case "enable":
		script = `systemctl start wazuh-agent && echo "wazuh-agent enabled"`
	case "disable":
		script = `systemctl stop wazuh-agent && echo "wazuh-agent disabled"`
	case "rules":
		script = `ossec-control enable && echo "ossec rules enabled"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "siem_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleHoneypot 处理 POST /api/v1/netsec/honeypot
func (s *Server) handleHoneypot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/deploy/enable/disable/list
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `echo "=== Honeypot Status ===" && (cowrie status 2>/dev/null || echo "cowrie not available") && (honeypot status 2>/dev/null || echo "honeypot not available")`
	case "deploy":
		script = `# deploy: apt install cowrie
apt-get install -y cowrie 2>/dev/null || apt install -y cowrie`
	case "enable":
		script = `systemctl start cowrie && echo "cowrie enabled"`
	case "disable":
		script = `systemctl stop cowrie && echo "cowrie disabled"`
	case "list":
		script = `ls /opt/cowrie/var/log/ 2>/dev/null || echo "cowrie log dir not found"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "honeypot_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleTrafficShaping 处理 POST /api/v1/netsec/traffic
func (s *Server) handleTrafficShaping(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/limit/shape/priority/qos
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `echo "=== Traffic Shaping Status ===" && (tc qdisc show 2>/dev/null || echo "tc not available") && (iptables -L 2>/dev/null || echo "iptables not available")`
	case "limit":
		script = `# limit: tc qdisc add ... tbf
tc qdisc add dev eth0 root tbf rate 1mbit burst 32kbit latency 400ms && echo "tbf limit applied"`
	case "shape":
		script = `# shape: tc qdisc add ... htb
tc qdisc add dev eth0 root htb default 12 && echo "htb shaping applied"`
	case "priority":
		script = `# priority: tc filter add ... prio
tc filter add dev eth0 protocol ip parent 1:0 prio 1 u32 match ip dport 80 0xffff flowid 1:10 && echo "prio filter applied"`
	case "qos":
		script = `# qos: tc qdisc add ... cake
tc qdisc add dev eth0 root cake bandwidth 10mbit && echo "cake qos applied"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "traffic_shaping_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleVPN 处理 POST /api/v1/netsec/vpn
func (s *Server) handleVPN(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/deploy/enable/disable/config
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `echo "=== VPN Status ===" && (wg show 2>/dev/null || echo "wireguard not available") && (openvpn --version 2>/dev/null | head -3 || echo "openvpn not available") && (ipsec status 2>/dev/null || echo "ipsec not available")`
	case "deploy":
		script = `# deploy: apt install wireguard/openvpn/strongswan
apt-get install -y wireguard openvpn strongswan 2>/dev/null || apt install -y wireguard openvpn strongswan`
	case "enable":
		script = `systemctl start wg-quick@wg0 && echo "wg-quick@wg0 enabled"`
	case "disable":
		script = `systemctl stop wg-quick@wg0 && echo "wg-quick@wg0 disabled"`
	case "config":
		script = `wg showconf wg0 2>/dev/null || echo "wg0 not found"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "vpn_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleZeroTrust 处理 POST /api/v1/netsec/zerotrust
func (s *Server) handleZeroTrust(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/deploy/enable/disable/policy
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `echo "=== Zero Trust Status ===" && (spire-agent status 2>/dev/null || echo "spire-agent not available") && (step-ca status 2>/dev/null || echo "step-ca not available")`
	case "deploy":
		script = `# deploy: apt install spire-agent
apt-get install -y spire-agent 2>/dev/null || apt install -y spire-agent`
	case "enable":
		script = `systemctl start spire-agent && echo "spire-agent enabled"`
	case "disable":
		script = `systemctl stop spire-agent && echo "spire-agent disabled"`
	case "policy":
		script = `spire-agent api fetch jwt 2>/dev/null || echo "spire-agent api not available"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "zerotrust_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
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
