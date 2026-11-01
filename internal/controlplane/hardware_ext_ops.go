// Package controlplane hardware_ext_ops.go 实现扩展硬件运维支持
package controlplane

import (
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

func (s *Server) handleRAID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `echo "=== RAID Status ===" && (mdadm --detail 2>/dev/null || true) && echo "--- megacli ---" && (megacli -LDInfo -Lall -aALL 2>/dev/null || true) && echo "--- storcli ---" && (storcli /c0 show 2>/dev/null || true)`
	case "info":
		script = `echo "=== mdstat ===" && (cat /proc/mdstat 2>/dev/null || true) && echo "--- AdpAllInfo ---" && (megacli -AdpAllInfo -aALL 2>/dev/null || true)`
	case "create":
		script = `echo "=== Create RAID ===" && (mdadm --create 2>/dev/null || true) && echo "--- megacli ---" && (megacli -CfgLdAdd 2>/dev/null || true)`
	case "delete":
		script = `echo "=== Delete RAID ===" && (mdadm --stop 2>/dev/null || true) && echo "--- megacli ---" && (megacli -CfgLdDel 2>/dev/null || true)`
	case "rebuild":
		script = `echo "=== Rebuild RAID ===" && (mdadm --manage --add 2>/dev/null || true) && echo "--- megacli ---" && (megacli -PdReplaceMissing 2>/dev/null || true)`
	case "monitor":
		script = `echo "=== Monitor RAID ===" && (mdadm --monitor 2>/dev/null || true) && echo "--- megacli ---" && (megacli -AdpEventLog -GetEvents -f /tmp/megacli_events.log -aALL 2>/dev/null || true)`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "hardware_raid_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

func (s *Server) handleBIOS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "info":
		script = `echo "=== BIOS Info ===" && (dmidecode -t bios 2>/dev/null || true) && echo "--- efibootmgr ---" && (efibootmgr 2>/dev/null || true)`
	case "backup":
		script = `echo "=== BIOS Backup ===" && (flashrom -r bios_backup.bin 2>/dev/null || true)`
	case "restore":
		script = `echo "=== BIOS Restore ===" && (flashrom -w bios_backup.bin 2>/dev/null || true)`
	case "settings":
		script = `echo "=== BIOS Settings (OEM Strings) ===" && (dmidecode -t 11 2>/dev/null || true)`
	case "boot-order":
		script = `echo "=== Boot Order ===" && (efibootmgr -o 2>/dev/null || true)`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "hardware_bios_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

func (s *Server) handleFirmware(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "list":
		script = `echo "=== Firmware Devices ===" && (fwupdmgr get-devices 2>/dev/null || true) && echo "=== Updates ===" && (fwupdmgr get-updates 2>/dev/null || true)`
	case "update":
		script = `echo "=== Firmware Update ===" && (fwupdmgr update 2>/dev/null || true)`
	case "check":
		script = `echo "=== Firmware Check ===" && (fwupdmgr refresh 2>/dev/null || true)`
	case "rollback":
		script = `echo "=== Firmware Rollback ===" && (fwupdmgr downgrade 2>/dev/null || true)`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "hardware_firmware_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

func (s *Server) handleThermal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `echo "=== Thermal Status ===" && (sensors 2>/dev/null || true) && echo "--- IPMI Temp ---" && (ipmitool sdr type Temperature 2>/dev/null || true)`
	case "monitor":
		script = `echo "=== Thermal Monitor ===" && (watch sensors 2>/dev/null || sensors 2>/dev/null || true)`
	case "fan-control":
		script = `echo "=== Fan Control ===" && (ipmitool raw 0x30 0x30 0x01 0x00 2>/dev/null || true)`
	case "alert":
		script = `echo "=== Thermal Alert ===" && (sensors -A 2>/dev/null || true)`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "hardware_thermal_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

func (s *Server) handlePDU(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `echo "=== PDU Status ===" && (snmpwalk -v2c -c public localhost 2>/dev/null | head -20 || true) && echo "--- IPMI Power ---" && (ipmitool power status 2>/dev/null || true)`
	case "power-on":
		script = `echo "=== Power ON ===" && (ipmitool power on 2>/dev/null || true)`
	case "power-off":
		script = `echo "=== Power OFF ===" && (ipmitool power off 2>/dev/null || true)`
	case "power-cycle":
		script = `echo "=== Power Cycle ===" && (ipmitool power cycle 2>/dev/null || true)`
	case "outlet-list":
		script = `echo "=== Outlet List ===" && (snmpwalk -v2c -c public localhost 2>/dev/null | grep -i outlet || true)`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "hardware_pdu_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

func (s *Server) handleKVM(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `echo "=== KVM Status ===" && (ipmitool sol info 2>/dev/null || true)`
	case "connect":
		script = `echo "=== KVM Connect ===" && (ipmitool sol activate 2>/dev/null || true)`
	case "disconnect":
		script = `echo "=== KVM Disconnect ===" && (ipmitool sol deactivate 2>/dev/null || true)`
	case "redirect":
		script = `echo "=== KVM Redirect ===" && (ipmitool sol payload enable 2>/dev/null || true)`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "hardware_kvm_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

func (s *Server) registerHardwareExtRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/hardware/raid", s.handleRAID)
	mux.HandleFunc("/api/v1/hardware/bios", s.handleBIOS)
	mux.HandleFunc("/api/v1/hardware/firmware", s.handleFirmware)
	mux.HandleFunc("/api/v1/hardware/thermal", s.handleThermal)
	mux.HandleFunc("/api/v1/hardware/pdu", s.handlePDU)
	mux.HandleFunc("/api/v1/hardware/kvm", s.handleKVM)
}
