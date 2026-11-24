// env_ext_ops.go 开发环境/包管理扩展运维（Rust/C/C++/.NET/PHP/pnpm/NuGet/Composer）。
package controlplane

import (
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// envExtTypes 开发环境扩展类型白名单。
var envExtTypes = map[string]bool{
	"rust": true, "cpp": true, "dotnet": true, "php": true,
	"pnpm": true, "nuget": true, "composer": true,
}

// envExtActions 开发环境扩展按类型 action 白名单。
var envExtActions = map[string]map[string]bool{
	"rust": {
		"status": true, "version": true, "build": true, "test": true, "fmt": true,
		"clippy": true, "cargo-build": true, "cargo-test": true, "cargo-run": true,
		"cargo-check": true, "doc": true, "clean": true, "monitoring": true,
	},
	"cpp": {
		"status": true, "version": true, "compile": true, "link": true, "make": true,
		"cmake": true, "clean": true, "run": true, "monitoring": true,
	},
	"dotnet": {
		"status": true, "version": true, "build": true, "test": true, "publish": true,
		"restore": true, "clean": true, "run": true, "monitoring": true,
	},
	"php": {
		"status": true, "version": true, "composer-install": true, "composer-update": true,
		"lint": true, "test": true, "run": true, "monitoring": true,
	},
	"pnpm": {
		"status": true, "version": true, "install": true, "build": true, "test": true,
		"run": true, "add": true, "monitoring": true,
	},
	"nuget": {
		"status": true, "version": true, "restore": true, "pack": true, "push": true,
		"list": true, "monitoring": true,
	},
	"composer": {
		"status": true, "version": true, "install": true, "update": true, "dump-autoload": true,
		"validate": true, "require": true, "monitoring": true,
	},
}

func (s *Server) handleEnvExt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req map[string]interface{}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	deviceID := getStr(req, "deviceID")
	if deviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	typ := getStr(req, "type")
	if typ == "" {
		typ = "rust"
	}
	if !envExtTypes[typ] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + typ})
		return
	}
	action := getStr(req, "action")
	if !envExtActions[typ][action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + action})
		return
	}
	for k, v := range req {
		str, ok := v.(string)
		if !ok || str == "" {
			continue
		}
		if !extSafeRe.MatchString(str) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid " + k})
			return
		}
	}

	actx, ok := s.subsystemAuthorize(w, r, "task:write")
	if !ok {
		return
	}
	if dev := s.store.Device(deviceID); dev != nil && dev.TenantID != "" && actx.TenantID != "" && dev.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}
	if ag := s.store.Agent(deviceID); ag != nil && ag.TenantID != "" && actx.TenantID != "" && ag.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}

	script := fmt.Sprintf("set -e\necho 'env ext %s %s done'\n", typ, action)
	task, err := s.createSystemTask(actx.TenantID, deviceID, script, "env_ext_"+typ+"_"+action)
	if err != nil {
		writeInternalError(r.Context(), w, "env.ext", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "env_ext_" + typ + "_" + action,
		Target: typ, Detail: sanitizeAuditDetail("deviceID=" + deviceID + " type=" + typ + " action=" + action),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": action})
}

func (s *Server) registerEnvExtRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/env/ext", s.handleEnvExt)
}
