// Package controlplane network_ext_ops.go 实现网络管理运维
// VPN/WireGuard/OpenVPN, CDN Purge, DNS 管理, SD-WAN
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleNetworkExt 处理 POST /api/v1/network/ext
func (s *Server) handleNetworkExt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // vpn-status/vpn-config/cdn-purge/dns-flush/sdwan-status
		VPN      string `json:"vpn"`    // wireguard/openvpn/ipsec
		CDN      string `json:"cdn"`    // cloudflare/akamai/fastly
		URL      string `json:"url"`    // URL to purge
		Zone     string `json:"zone"`   // DNS zone
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "vpn-status":
		script = `echo "=== WireGuard ===" && (wg show 2>/dev/null || echo "WireGuard not available") && echo "=== OpenVPN ===" && (systemctl status openvpn 2>/dev/null | head -3 || echo "OpenVPN not available") && echo "=== IPsec ===" && (ipsec status 2>/dev/null || echo "IPsec not available")`
	case "vpn-config":
		if req.VPN == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "vpn type is required"})
			return
		}
		script = fmt.Sprintf(`echo "VPN config for %s:" && case %s in wireguard) wg showconf wg0 2>/dev/null || echo "wg0 not found";; openvpn) cat /etc/openvpn/server.conf 2>/dev/null || echo "OpenVPN config not found";; ipsec) cat /etc/ipsec.conf 2>/dev/null || echo "IPsec config not found";; esac`, req.VPN, req.VPN)
	case "cdn-purge":
		if req.URL == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "url is required"})
			return
		}
		script = fmt.Sprintf(`echo "Purging CDN cache for %s..." && curl -X POST "https://api.cloudflare.com/client/v4/zones/ZONE_ID/purge_cache" -H "Authorization: Bearer TOKEN" -d '{"files":["%s"]}' 2>/dev/null || echo "CDN purge not configured"`, req.URL, req.URL)
	case "dns-flush":
		script = `systemctl restart systemd-resolved 2>/dev/null && echo "DNS cache flushed" || (service nscd restart 2>/dev/null && echo "NSCD restarted") || echo "DNS flush not available"`
	case "sdwan-status":
		script = `echo "=== SD-WAN Status ===" && (ip route show 2>/dev/null | head -10) && echo "---" && (curl -s http://localhost:8080/api/sdwan/status 2>/dev/null || echo "SD-WAN not available")`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "network_ext_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleNetworkVendorSwitch 处理 POST /api/v1/network/vendor/switch：厂商特定命令脚本。
func (s *Server) handleNetworkVendorSwitch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Vendor   string `json:"vendor"` // cisco/huawei/juniper/h3c/arista
		Action   string `json:"action"` // 可选：version/config/vlan/interface/all
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	vendor := strings.ToLower(strings.TrimSpace(req.Vendor))
	var commands []string
	switch vendor {
	case "cisco":
		commands = []string{"show version", "show running-config", "show vlan", "show interface"}
	case "huawei":
		commands = []string{"display version", "display current-configuration", "display vlan", "display interface"}
	case "juniper":
		commands = []string{"show version", "show configuration", "show vlans", "show interfaces"}
	case "h3c":
		commands = []string{"display version", "display current-configuration", "display vlan"}
	case "arista":
		commands = []string{"show version", "show running-config", "show vlan", "show interfaces"}
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported vendor: " + req.Vendor})
		return
	}

	// 可选按 action 过滤命令。
	if req.Action != "" && !strings.EqualFold(req.Action, "all") {
		filtered := make([]string, 0, len(commands))
		key := strings.ToLower(req.Action)
		for _, c := range commands {
			if strings.Contains(strings.ToLower(c), key) {
				filtered = append(filtered, c)
			}
		}
		if len(filtered) > 0 {
			commands = filtered
		}
	}

	var b strings.Builder
	b.WriteString("set -e\n")
	fmt.Fprintf(&b, "echo '=== Network Vendor Switch: %s ==='\n", vendor)
	for _, c := range commands {
		fmt.Fprintf(&b, "echo '--- %s ---'\n", c)
		fmt.Fprintf(&b, "%s\n", c)
	}
	script := b.String()

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "network_vendor_switch_"+vendor)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "vendor": vendor, "action": req.Action})
}

// registerNetworkExtRoutes 注册网络扩展路由
func (s *Server) registerNetworkExtRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/network/ext", s.handleNetworkExt)
	mux.HandleFunc("/api/v1/network/vendor/switch", s.handleNetworkVendorSwitch)
}
