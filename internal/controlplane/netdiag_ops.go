// Package controlplane netdiag_ops.go 实现网络诊断工具
// 覆盖：tcpdump, mtr, nmap, dig, host, arp, route, iftop, nethogs, ss, netstat, traceroute, ping
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/pkg/shellquote"
)

// handleNetDiag 处理 POST /api/v1/netdiag
//
// 注入面（批次 7）：target 是用户可控字段，一律经 shellquote.Quote 变成 POSIX
// 单引号字面量后再入脚本。原先的裸拼与双引号拼法都可被 `$(id)` / 反引号执行——
// 双引号段内 `$` 与反引号同样会展开，故双引号不是防护。
func (s *Server) handleNetDiag(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // tcpdump/mtr/nmap/dig/host/arp/route/iftop/nethogs/ss/netstat/traceroute/ping/status-code
		Target   string `json:"target"`
		Count    int    `json:"count"`
		Port     int    `json:"port"`
		Protocol string `json:"protocol"` // tcp/udp/icmp
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "tcpdump":
		if req.Target == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "target (host or interface) is required"})
			return
		}
		count := req.Count
		if count <= 0 {
			count = 100
		}
		script = fmt.Sprintf(`timeout 30 tcpdump -i any -nn -c %d host %s 2>/dev/null || echo "tcpdump not available"`, count, shellquote.Quote(req.Target))
	case "mtr":
		if req.Target == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "target is required"})
			return
		}
		count := req.Count
		if count <= 0 {
			count = 10
		}
		script = fmt.Sprintf(`mtr -r -c %d %s 2>/dev/null || traceroute %s 2>/dev/null || echo "mtr/traceroute not available"`, count, shellquote.Quote(req.Target), shellquote.Quote(req.Target))
	case "nmap":
		if req.Target == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "target is required"})
			return
		}
		script = fmt.Sprintf(`nmap -sT -p 1-1000 %s 2>/dev/null | head -40 || echo "nmap not available"`, shellquote.Quote(req.Target))
	case "dig":
		if req.Target == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "target (domain) is required"})
			return
		}
		script = fmt.Sprintf(`dig %s +short 2>/dev/null && echo "---" && dig %s 2>/dev/null | grep -A2 "ANSWER SECTION"`, shellquote.Quote(req.Target), shellquote.Quote(req.Target))
	case "host":
		if req.Target == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "target (domain) is required"})
			return
		}
		script = fmt.Sprintf(`host %s 2>/dev/null || echo "host not available"`, shellquote.Quote(req.Target))
	case "arp":
		script = `arp -a 2>/dev/null || ip neigh 2>/dev/null || echo "arp not available"`
	case "route":
		script = `ip route show 2>/dev/null || route -n 2>/dev/null || echo "route not available"`
	case "iftop":
		script = `iftop -t -s 5 2>/dev/null | head -20 || echo "iftop not available"`
	case "nethogs":
		script = `nethogs -t 5 2>/dev/null | head -20 || echo "nethogs not available"`
	case "ss":
		script = `ss -tlnp 2>/dev/null | head -40 || echo "ss not available"`
	case "netstat":
		script = `netstat -tlnp 2>/dev/null | head -40 || echo "netstat not available"`
	case "traceroute":
		if req.Target == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "target is required"})
			return
		}
		script = fmt.Sprintf(`traceroute %s 2>/dev/null || tracepath %s 2>/dev/null || echo "traceroute not available"`, shellquote.Quote(req.Target), shellquote.Quote(req.Target))
	case "ping":
		if req.Target == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "target is required"})
			return
		}
		count := req.Count
		if count <= 0 {
			count = 4
		}
		script = fmt.Sprintf(`ping -c %d %s 2>/dev/null || echo "ping not available"`, count, shellquote.Quote(req.Target))
	case "status-code":
		if req.Target == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "target (URL) is required"})
			return
		}
		script = fmt.Sprintf(`echo "=== HTTP Status Code ===" && curl -s -o /dev/null -w "HTTP %%{http_code} (time: %%{time_total}s, size: %%{size_download} bytes)" --max-time 10 %s 2>/dev/null && echo "" && echo "=== Redirect Chain ===" && curl -sIL %s 2>/dev/null | grep -iE "HTTP/|Location:" | head -10`, shellquote.Quote(req.Target), shellquote.Quote(req.Target))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "netdiag_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerNetDiagRoutes 注册网络诊断路由
func (s *Server) registerNetDiagRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/netdiag", s.handleNetDiag)
}
