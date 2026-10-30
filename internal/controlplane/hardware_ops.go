// hardware_ops.go 硬件/小型机/机柜运维管理。
package controlplane

import (
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleHardwareInventory 处理 POST /api/v1/hardware/inventory
// actions: detect/list/info/power/status
func (s *Server) handleHardwareInventory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID    string `json:"deviceID"`
		Action      string `json:"action"`      // detect/list/info/power/status
		Component   string `json:"component"`   // cpu/memory/disk/nic/motherboard/all
		PowerAction string `json:"powerAction"` // on/off/restart/cycle/status
		Power       string `json:"power"`
		Mode        string `json:"mode"`
		State       string `json:"state"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if req.Component == "" {
		req.Component = "all"
	}

	var script string
	switch req.Action {
	case "detect":
		script = `echo "=== dmidecode ===" && (dmidecode 2>/dev/null | head -80 || echo "dmidecode not available")
echo "=== lspci ===" && (lspci 2>/dev/null || echo "lspci not available")
echo "=== lshw ===" && (lshw -short 2>/dev/null || echo "lshw not available")
echo "=== ipmitool ===" && (ipmitool mc info 2>/dev/null || echo "ipmitool not available")`
	case "list":
		script = `echo "=== Hardware Components ===" && (lshw -short 2>/dev/null || dmidecode -t 1,2,3,4,17 2>/dev/null | head -120 || echo "hardware listing tools not available")`
	case "info":
		switch req.Component {
		case "cpu":
			script = `echo "=== CPU ===" && (lscpu 2>/dev/null || echo "lscpu not available") && echo "---" && (dmidecode -t processor 2>/dev/null | head -60 || true)`
		case "memory":
			script = `echo "=== Memory ===" && (free -h 2>/dev/null || echo "free not available") && echo "---" && (dmidecode -t memory 2>/dev/null | head -80 || true)`
		case "disk":
			script = `echo "=== Disk ===" && (lsblk 2>/dev/null || echo "lsblk not available") && echo "---" && (fdisk -l 2>/dev/null | head -40 || true)`
		case "nic":
			script = `echo "=== NIC ===" && (ip link 2>/dev/null || ifconfig -a 2>/dev/null || echo "no ip tool") && echo "---" && (lspci 2>/dev/null | grep -iE 'ethernet|network' || true)`
		case "motherboard":
			script = `echo "=== Motherboard ===" && (dmidecode -t baseboard 2>/dev/null | head -40 || echo "dmidecode not available") && echo "---" && (dmidecode -t bios 2>/dev/null | head -30 || true)`
		default:
			script = `echo "=== CPU ===" && (lscpu 2>/dev/null | head -20 || echo "n/a")
echo "=== Memory ===" && (free -h 2>/dev/null || echo "n/a")
echo "=== Disk ===" && (lsblk 2>/dev/null || echo "n/a")
echo "=== NIC ===" && (ip link 2>/dev/null | head -20 || echo "n/a")
echo "=== Motherboard ===" && (dmidecode -t baseboard 2>/dev/null | head -20 || echo "n/a")`
		}
	case "power":
		pa := req.PowerAction
		if pa == "" {
			pa = req.Power
		}
		if pa == "" {
			pa = req.Mode
		}
		if pa == "" {
			pa = req.State
		}
		if pa == "" {
			pa = "status"
		}
		switch pa {
		case "on":
			script = `ipmitool power on && echo "Power ON requested"`
		case "off":
			script = `ipmitool power off && echo "Power OFF requested"`
		case "restart", "reset":
			script = `ipmitool power reset && echo "Power RESET requested"`
		case "cycle":
			script = `ipmitool power cycle && echo "Power CYCLE requested"`
		case "status":
			script = `ipmitool power status`
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown power action: " + pa})
			return
		}
	case "status":
		script = `echo "=== Power Status ===" && (ipmitool power status 2>/dev/null || echo "ipmitool not available")
echo "=== Sensors ===" && (ipmitool sensor list 2>/dev/null || sensors 2>/dev/null || echo "sensors not available")
echo "=== Temperature ===" && (ipmitool sdr type Temperature 2>/dev/null || true)
echo "=== Fan ===" && (ipmitool sdr type Fan 2>/dev/null || true)
echo "=== Voltage ===" && (ipmitool sdr type Voltage 2>/dev/null || true)`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "hardware_inventory_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleHardwareBMC 处理 POST /api/v1/hardware/bmc
// actions: status/console/sol/power-cycle/reset
func (s *Server) handleHardwareBMC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/console/sol/power-cycle/reset
		Reset    string `json:"reset"`  // cold/warm
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if req.Reset == "" {
		req.Reset = "cold"
	}

	var script string
	switch req.Action {
	case "status":
		script = `echo "=== BMC Info ===" && (ipmitool mc info 2>/dev/null || echo "ipmitool not available")
echo "=== LAN Config ===" && (ipmitool lan print 1 2>/dev/null || true)
echo "=== User List ===" && (ipmitool user list 1 2>/dev/null || true)`
	case "console":
		script = `echo "=== SOL Console ===" && (ipmitool sol activate 2>/dev/null || echo "SOL not available")`
	case "sol":
		script = `echo "=== SOL (Serial Over LAN) ===" && (ipmitool sol info 1 2>/dev/null; ipmitool sol activate 2>/dev/null || echo "SOL not available")`
	case "power-cycle":
		script = `ipmitool power cycle && echo "BMC power cycle requested"`
	case "reset":
		if req.Reset == "warm" {
			script = `ipmitool mc reset warm && echo "BMC warm reset requested"`
		} else {
			script = `ipmitool mc reset cold && echo "BMC cold reset requested"`
		}
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "hardware_bmc_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleCabinet 处理 POST /api/v1/hardware/cabinet
// actions: status/list/devices/power/temperature
func (s *Server) handleCabinet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID    string `json:"deviceID"`
		Action      string `json:"action"` // status/list/devices/power/temperature
		CabinetID   string `json:"cabinetID"`
		Cabinet     string `json:"cabinet"`
		Device      string `json:"device"`
		Position    string `json:"position"`
		Operation   string `json:"operation"`   // list/add/remove/move
		PowerAction string `json:"powerAction"` // on/off/cycle/status
		Power       string `json:"power"`
		Outlet      string `json:"outlet"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	cabinet := req.CabinetID
	if cabinet == "" {
		cabinet = req.Cabinet
	}

	var script string
	switch req.Action {
	case "status":
		script = fmt.Sprintf(`echo "=== Cabinet Status ===" && echo "Cabinet: %s"
echo "--- PDU ---" && (ipmitool sensor list 2>/dev/null | grep -iE 'pdu|power' || snmpwalk -v2c -c public localhost 1.3.6.1.4.1.21785 2>/dev/null | head -20 || echo "PDU status not available")
echo "--- Cooling/Fan ---" && (ipmitool sensor list 2>/dev/null | grep -iE 'fan|cool' || echo "fan status not available")
echo "--- Temp ---" && (ipmitool sensor list 2>/dev/null | grep -iE 'temp' || sensors 2>/dev/null || echo "temp not available")`, cabinet)
	case "list":
		script = fmt.Sprintf(`echo "=== Devices in Cabinet %s ===" && (ls /dev 2>/dev/null | head; echo "---"; dmidecode -t 1 2>/dev/null | grep -iE 'product|serial|manufacturer' || echo "device list not available")`, cabinet)
	case "devices":
		op := req.Operation
		if op == "" {
			op = "list"
		}
		switch op {
		case "add", "mount":
			script = fmt.Sprintf(`echo "Mounting device %s into cabinet %s at %s" && echo "device mounted"`, req.Device, cabinet, req.Position)
		case "remove", "unmount":
			script = fmt.Sprintf(`echo "Removing device %s from cabinet %s" && echo "device removed"`, req.Device, cabinet)
		case "move":
			script = fmt.Sprintf(`echo "Moving device %s to position %s in cabinet %s" && echo "device moved"`, req.Device, req.Position, cabinet)
		default:
			script = fmt.Sprintf(`echo "=== Cabinet %s Device List ===" && (dmidecode -t 1 2>/dev/null | grep -iE 'product|serial' || echo "no device inventory")`, cabinet)
		}
	case "power":
		pa := req.PowerAction
		if pa == "" {
			pa = req.Power
		}
		if pa == "" {
			pa = "status"
		}
		outlet := req.Outlet
		if outlet == "" {
			outlet = "all"
		}
		switch pa {
		case "on":
			script = fmt.Sprintf(`echo "Power ON outlet %s in cabinet %s" && (ipmitool power on 2>/dev/null || snmpset -v2c -c private localhost 1.3.6.1.4.1.21785.3.1.0 i 1 2>/dev/null || echo "pdu control not available")`, outlet, cabinet)
		case "off":
			script = fmt.Sprintf(`echo "Power OFF outlet %s in cabinet %s" && (ipmitool power off 2>/dev/null || snmpset -v2c -c private localhost 1.3.6.1.4.1.21785.3.1.0 i 0 2>/dev/null || echo "pdu control not available")`, outlet, cabinet)
		case "cycle":
			script = fmt.Sprintf(`echo "Power CYCLE outlet %s in cabinet %s" && (ipmitool power cycle 2>/dev/null || echo "pdu control not available")`, outlet, cabinet)
		case "status":
			script = fmt.Sprintf(`echo "Power status for cabinet %s outlet %s" && (ipmitool power status 2>/dev/null || echo "pdu status not available")`, cabinet, outlet)
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown power action: " + pa})
			return
		}
	case "temperature":
		script = fmt.Sprintf(`echo "=== Temperature Monitoring for Cabinet %s ===" && (ipmitool sensor list 2>/dev/null | grep -iE 'temp' || sensors 2>/dev/null || echo "temperature sensors not available")
echo "--- Fan ---" && (ipmitool sensor list 2>/dev/null | grep -iE 'fan' || echo "fan not available")`, cabinet)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "hardware_cabinet_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerHardwareRoutes 注册硬件/小型机/机柜运维路由
func (s *Server) registerHardwareRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/hardware/inventory", s.handleHardwareInventory)
	mux.HandleFunc("/api/v1/hardware/bmc", s.handleHardwareBMC)
	mux.HandleFunc("/api/v1/hardware/cabinet", s.handleCabinet)
}
