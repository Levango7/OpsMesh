// Package controlplane raid_ext_ops.go 实现 RAID 高级管理
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleRAIDExt 处理 POST /api/v1/os/raid-ext
func (s *Server) handleRAIDExt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID   string `json:"deviceID"`
		Action     string `json:"action"`     // status/rebuild/health/add-spare/remove-disk/mdadm/zfs
		Controller string `json:"controller"` // megacli/storcli/hpssacli/mdadm/zfs
		Target     string `json:"target"`     // array/disk/pool name
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `echo "=== RAID Status ===" && (storcli64 /call show 2>/dev/null || megacli -PDList -aALL 2>/dev/null || hpssacli ctrl all show status 2>/dev/null || mdadm --detail --scan 2>/dev/null || zpool status 2>/dev/null || echo "No RAID controller found")`
	case "rebuild":
		if req.Target == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "target is required"})
			return
		}
		script = fmt.Sprintf(`echo "Rebuilding RAID array: %s" && (storcli64 /c0/v%d start rebuild 2>/dev/null || mdadm --assemble --scan 2>/dev/null || echo "Rebuild not available")`, req.Target, 0)
	case "health":
		script = `echo "=== RAID Health ===" && (storcli64 /call show 2>/dev/null | grep -i "status\|health\|degraded" || mdadm --detail /dev/md0 2>/dev/null | grep -i "state\|active" || echo "RAID health not available")`
	case "add-spare":
		if req.Target == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "target (disk) is required"})
			return
		}
		script = fmt.Sprintf(`echo "Adding spare disk: %s" && (storcli64 /c0/eall/sall add hotsparedrive 2>/dev/null || mdadm --add /dev/md0 %s 2>/dev/null || echo "Add spare not available")`, req.Target, req.Target)
	case "remove-disk":
		if req.Target == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "target (disk) is required"})
			return
		}
		script = fmt.Sprintf(`echo "Removing disk: %s" && (storcli64 /c0/eall/sall remove 2>/dev/null || mdadm --remove /dev/md0 %s 2>/dev/null || echo "Remove disk not available")`, req.Target, req.Target)
	case "mdadm":
		script = `echo "=== Software RAID (mdadm) ===" && (cat /proc/mdstat 2>/dev/null || echo "mdadm not available") && echo "---" && (mdadm --detail --scan 2>/dev/null || echo "No md arrays")`
	case "zfs":
		script = `echo "=== ZFS ===" && (zpool status 2>/dev/null || echo "ZFS not available") && echo "---" && (zfs list 2>/dev/null || echo "No ZFS datasets")`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "raid_ext_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerRAIDExtRoutes 注册 RAID 扩展路由
func (s *Server) registerRAIDExtRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/os/raid-ext", s.handleRAIDExt)
}
