// Package controlplane iac_ops.go 实现基础设施即代码 (Terraform/Pulumi)
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/pkg/shellquote"
)

// handleIaC 处理 POST /api/v1/iac
func (s *Server) handleIaC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // init/plan/apply/destroy/status
		Tool     string `json:"tool"`   // terraform/pulumi
		WorkDir  string `json:"workDir"`
		Args     string `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	tool := req.Tool
	if tool == "" {
		tool = "terraform"
	}
	if tool != "terraform" && tool != "pulumi" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported tool: " + tool})
		return
	}
	workDir := req.WorkDir
	if workDir == "" {
		workDir = "/tmp/terraform"
	}
	workDirV, err := mqPath("workDir", workDir)
	if err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	argsV, err := opsArgList("args", req.Args)
	if err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "init":
		script = fmt.Sprintf(`cd %s && %s init %s 2>&1 | tail -20`, shellquote.Quote(workDirV), tool, argsV)
	case "plan":
		script = fmt.Sprintf(`cd %s && %s plan %s 2>&1 | tail -30`, shellquote.Quote(workDirV), tool, argsV)
	case "apply":
		script = fmt.Sprintf(`cd %s && %s apply -auto-approve %s 2>&1 | tail -30`, shellquote.Quote(workDirV), tool, argsV)
	case "destroy":
		script = fmt.Sprintf(`cd %s && %s destroy -auto-approve %s 2>&1 | tail -30`, shellquote.Quote(workDirV), tool, argsV)
	case "status":
		script = fmt.Sprintf(`cd %s && %s show 2>&1 | head -30 || echo %s`, shellquote.Quote(workDirV), tool, opsEcho("%s not available", tool))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "iac_"+tool+"_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerIaCRoutes 注册 IaC 路由
func (s *Server) registerIaCRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/iac", s.handleIaC)
}
